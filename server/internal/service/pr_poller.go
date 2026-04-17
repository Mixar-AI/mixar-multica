package service

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GitHubClient is the interface the poller uses to reach GitHub, making it
// easy to substitute a fake in unit tests.
type GitHubClient interface {
	FetchPR(ctx context.Context, owner, repo string, number int) (*GHPullRequest, error)
	FetchReviewComments(ctx context.Context, owner, repo string, number int, since time.Time) ([]GHReviewComment, error)
}

// TokenProvider resolves a GitHub access token for a given workspace.
// Implementations should try the workspace_integration table first and fall
// back to the GITHUB_TOKEN env var when no row is found.
type TokenProvider interface {
	GetGitHubToken(ctx context.Context, workspaceID uuid.UUID) (string, error)
}

// GitHubClientFactory creates a GitHubClient for a given token. Used by the
// poller to build a per-workspace client when a TokenProvider is configured.
type GitHubClientFactory func(token string) GitHubClient

// GHPullRequest mirrors github.PR without importing the package, keeping
// the service layer decoupled from the github package.
type GHPullRequest struct {
	Number    int
	State     string
	Draft     bool
	Merged    bool
	Title     string
	HeadRef   string
	BaseRef   string
	MergedAt  *time.Time
	ClosedAt  *time.Time
	UpdatedAt time.Time
}

// GHReviewComment mirrors github.ReviewComment without importing the package.
type GHReviewComment struct {
	ID        int64
	Body      string
	User      string
	CreatedAt time.Time
	HTMLURL   string
}

// PRPoller polls GitHub for changes to open pull requests and syncs them
// back to the database. When review activity is detected, it dispatches a
// follow-up agent task so the agent can address reviewer feedback.
type PRPoller struct {
	Queries       *db.Queries
	GitHub        GitHubClient         // used when TokenProvider is nil (legacy single-token mode)
	TokenProvider TokenProvider        // optional; enables per-workspace tokens
	ClientFactory GitHubClientFactory  // required when TokenProvider is set
	TaskSvc       *TaskService
	Logger        *slog.Logger
}

// NewPRPoller creates a new PRPoller using a shared GitHubClient.
// Use NewPRPollerWithTokenProvider to enable per-workspace token lookup.
func NewPRPoller(q *db.Queries, gh GitHubClient, taskSvc *TaskService) *PRPoller {
	return &PRPoller{
		Queries: q,
		GitHub:  gh,
		TaskSvc: taskSvc,
		Logger:  slog.Default(),
	}
}

// NewPRPollerWithTokenProvider creates a PRPoller that resolves tokens
// per-workspace using the given TokenProvider and builds per-PR clients via
// the factory. Falls back to legacy GitHub env var token when provider returns
// an error.
func NewPRPollerWithTokenProvider(q *db.Queries, tp TokenProvider, factory GitHubClientFactory, taskSvc *TaskService) *PRPoller {
	return &PRPoller{
		Queries:       q,
		TokenProvider: tp,
		ClientFactory: factory,
		TaskSvc:       taskSvc,
		Logger:        slog.Default(),
	}
}

// resolveClientForPR returns the GitHubClient to use for the given PR.
// When a TokenProvider is configured, it resolves a per-workspace token.
// Falls back to the shared GitHub client (or a GITHUB_TOKEN env var client).
func (p *PRPoller) resolveClientForPR(ctx context.Context, pr db.PullRequest) GitHubClient {
	if p.TokenProvider == nil || p.ClientFactory == nil {
		return p.GitHub
	}

	wsUUID, err := uuid.Parse(util.UUIDToString(pr.WorkspaceID))
	if err != nil {
		return p.GitHub
	}

	token, err := p.TokenProvider.GetGitHubToken(ctx, wsUUID)
	if err != nil || token == "" {
		p.Logger.Debug("pr poller: no workspace token, using fallback", "workspace_id", wsUUID, "error", err)
		return p.GitHub
	}
	return p.ClientFactory(token)
}

// Run starts the poll loop. It blocks until ctx is cancelled.
func (p *PRPoller) Run(ctx context.Context, interval time.Duration) {
	p.Logger.Info("pr poller starting", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.Logger.Info("pr poller stopped")
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

const pollBatchSize = 50

func (p *PRPoller) tick(ctx context.Context) {
	prs, err := p.Queries.ListOpenPullRequests(ctx, pollBatchSize)
	if err != nil {
		p.Logger.Warn("pr poller: list open PRs failed", "error", err)
		return
	}
	if len(prs) == 0 {
		return
	}
	p.Logger.Debug("pr poller: syncing PRs", "count", len(prs))

	for _, pr := range prs {
		p.syncPR(ctx, pr)
	}
}

var githubRepoPath = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

func parseGitHubPRURL(rawURL string) (owner, repo string, number int, ok bool) {
	m := githubRepoPath.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return "", "", 0, false
	}
	return m[1], m[2], n, true
}

func (p *PRPoller) syncPR(ctx context.Context, pr db.PullRequest) {
	owner, repo, number, ok := parseGitHubPRURL(pr.PrUrl)
	if !ok {
		// Not a GitHub PR (e.g. GitLab): skip silently.
		return
	}

	ghClient := p.resolveClientForPR(ctx, pr)
	ghPR, err := ghClient.FetchPR(ctx, owner, repo, number)
	if err != nil {
		p.Logger.Warn("pr poller: fetch PR failed",
			"pr_id", util.UUIDToString(pr.ID),
			"url", pr.PrUrl,
			"error", err,
		)
		// Still update last_synced_at by performing a no-op update so we
		// don't hammer a broken PR on every tick.
		p.Queries.UpdatePullRequestState(ctx, db.UpdatePullRequestStateParams{ID: pr.ID})
		return
	}

	// Map GitHub state to our internal state value.
	newState := ghPR.State // "open" | "closed"
	if ghPR.Draft {
		newState = "draft"
	} else if ghPR.Merged {
		newState = "merged"
	}

	params := db.UpdatePullRequestStateParams{
		ID:    pr.ID,
		State: pgtype.Text{String: newState, Valid: true},
		Title: pgtype.Text{String: ghPR.Title, Valid: true},
	}
	if ghPR.MergedAt != nil {
		params.MergedAt = pgtype.Timestamptz{Time: *ghPR.MergedAt, Valid: true}
	}
	if ghPR.ClosedAt != nil {
		params.ClosedAt = pgtype.Timestamptz{Time: *ghPR.ClosedAt, Valid: true}
	}

	updated, err := p.Queries.UpdatePullRequestState(ctx, params)
	if err != nil {
		p.Logger.Warn("pr poller: update PR state failed",
			"pr_id", util.UUIDToString(pr.ID),
			"error", err,
		)
		return
	}

	p.Logger.Debug("pr poller: synced",
		"pr_id", util.UUIDToString(updated.ID),
		"state", updated.State,
	)

	// Detect review activity: PR is still open and was updated since our last sync.
	sinceTime := pr.LastSyncedAt.Time
	if pr.LastSyncedAt.Valid &&
		(newState == "open" || newState == "draft") &&
		ghPR.UpdatedAt.After(sinceTime) {
		p.checkReviewFeedback(ctx, updated, owner, repo, number, sinceTime)
	}
}

// checkReviewFeedback fetches new review comments and, if any exist, dispatches
// a follow-up agent task so the assigned agent can address reviewer feedback.
func (p *PRPoller) checkReviewFeedback(ctx context.Context, pr db.PullRequest, owner, repo string, number int, since time.Time) {
	ghClient := p.resolveClientForPR(ctx, pr)
	comments, err := ghClient.FetchReviewComments(ctx, owner, repo, number, since)
	if err != nil {
		p.Logger.Warn("pr poller: fetch review comments failed",
			"pr_id", util.UUIDToString(pr.ID),
			"error", err,
		)
		return
	}
	if len(comments) == 0 {
		return
	}

	// Pick the most recent comment as the trigger.
	latest := comments[0]
	for _, c := range comments[1:] {
		if c.CreatedAt.After(latest.CreatedAt) {
			latest = c
		}
	}

	p.Logger.Info("pr poller: review feedback detected",
		"pr_id", util.UUIDToString(pr.ID),
		"pr_url", pr.PrUrl,
		"comment_user", latest.User,
		"comment_url", latest.HTMLURL,
		"new_comments", len(comments),
	)

	if !pr.IssueID.Valid {
		p.Logger.Debug("pr poller: PR has no linked issue, skipping follow-up task",
			"pr_id", util.UUIDToString(pr.ID),
		)
		return
	}

	issue, err := p.Queries.GetIssue(ctx, pr.IssueID)
	if err != nil {
		p.Logger.Warn("pr poller: load issue for follow-up failed",
			"pr_id", util.UUIDToString(pr.ID),
			"issue_id", util.UUIDToString(pr.IssueID),
			"error", err,
		)
		return
	}

	// Create a Multica comment from the review feedback so the agent gets
	// it via the standard TriggerCommentID flow.
	body := "[Review feedback from " + latest.User + "](" + latest.HTMLURL + ")\n\n" + latest.Body
	triggerComment, err := p.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     pr.IssueID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{},
		Content:     body,
		Type:        "review_feedback",
	})
	if err != nil {
		p.Logger.Warn("pr poller: create trigger comment failed",
			"pr_id", util.UUIDToString(pr.ID),
			"error", err,
		)
		return
	}

	// Dispatch follow-up task with reuse_worktree=true so the agent
	// continues work in its existing worktree. We use EnqueueTaskForIssueWithPicker
	// so the agent's runtime ID is resolved automatically.
	task, err := p.TaskSvc.EnqueueTaskForIssueWithPicker(ctx, issue, PickerFields{
		BaseBranch:       pgtype.Text{String: pr.BaseBranch, Valid: pr.BaseBranch != ""},
		ReuseWorktree:    true,
		TriggerCommentID: triggerComment.ID,
	})
	if err != nil {
		p.Logger.Warn("pr poller: dispatch follow-up task failed",
			"pr_id", util.UUIDToString(pr.ID),
			"error", err,
		)
		return
	}

	p.Logger.Info("pr poller: follow-up task dispatched",
		"task_id", util.UUIDToString(task.ID),
		"pr_id", util.UUIDToString(pr.ID),
		"issue_id", util.UUIDToString(pr.IssueID),
	)
}
