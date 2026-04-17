package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// capturePullRequest parses req.PRURL, looks up the linked repository, and
// inserts a row in pull_request. Errors are logged but not propagated — PR
// capture is best-effort; task completion must not fail because of it.
func (h *Handler) capturePullRequest(ctx context.Context, workspaceID pgtype.UUID, taskID pgtype.UUID, req TaskCompleteRequest) {
	parsed, err := service.ParsePRURL(req.PRURL)
	if err != nil {
		slog.Warn("pr capture: parse failed", "url", req.PRURL, "error", err)
		return
	}
	repo, err := h.Queries.GetRepositoryByURL(ctx, db.GetRepositoryByURLParams{
		WorkspaceID: workspaceID,
		Url:         parsed.RepoURL,
	})
	if err != nil {
		slog.Warn("pr capture: repo not registered", "url", parsed.RepoURL, "error", err)
		return
	}

	// Resolve optional issue ID and title from the task.
	var issueID pgtype.UUID
	var title string
	task, err := h.Queries.GetAgentTask(ctx, taskID)
	if err == nil && task.IssueID.Valid {
		issueID = task.IssueID
		if issue, err := h.Queries.GetIssue(ctx, task.IssueID); err == nil {
			title = issue.Title
		}
	}

	// Resolve head/base branch from worktree if available.
	headBranch := ""
	baseBranch := "main"

	if _, err := h.Queries.InsertPullRequest(ctx, db.InsertPullRequestParams{
		WorkspaceID:      workspaceID,
		RepositoryID:     repo.ID,
		IssueID:          issueID,
		TaskID:           taskID,
		PrUrl:            req.PRURL,
		PrNumber:         int32(parsed.Number),
		HeadBranch:       headBranch,
		BaseBranch:       baseBranch,
		State:            "open",
		Title:            title,
		CreatedByAgentID: pgtype.UUID{},
	}); err != nil {
		slog.Error("pr capture: insert failed", "error", err)
	}
}

// PullRequestResponse is the JSON shape for pull request endpoints.
type PullRequestResponse struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	RepositoryID     string `json:"repository_id"`
	IssueID          string `json:"issue_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	PrURL            string `json:"pr_url"`
	PrNumber         int32  `json:"pr_number"`
	HeadBranch       string `json:"head_branch"`
	BaseBranch       string `json:"base_branch"`
	State            string `json:"state"`
	Title            string `json:"title"`
	CreatedByAgentID string `json:"created_by_agent_id,omitempty"`
	CreatedAt        string `json:"created_at"`
	LastSyncedAt     string `json:"last_synced_at,omitempty"`
}

func pullRequestToResponse(pr db.PullRequest) PullRequestResponse {
	resp := PullRequestResponse{
		ID:           uuidToString(pr.ID),
		WorkspaceID:  uuidToString(pr.WorkspaceID),
		RepositoryID: uuidToString(pr.RepositoryID),
		PrURL:        pr.PrUrl,
		PrNumber:     pr.PrNumber,
		HeadBranch:   pr.HeadBranch,
		BaseBranch:   pr.BaseBranch,
		State:        pr.State,
		Title:        pr.Title,
		CreatedAt:    pr.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if pr.IssueID.Valid {
		resp.IssueID = uuidToString(pr.IssueID)
	}
	if pr.TaskID.Valid {
		resp.TaskID = uuidToString(pr.TaskID)
	}
	if pr.CreatedByAgentID.Valid {
		resp.CreatedByAgentID = uuidToString(pr.CreatedByAgentID)
	}
	if pr.LastSyncedAt.Valid {
		resp.LastSyncedAt = pr.LastSyncedAt.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	return resp
}

// ListPullRequestsByIssue returns PRs linked to a specific issue.
// Route: GET /api/issues/:id/pull-requests
func (h *Handler) ListPullRequestsByIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	prs, err := h.Queries.ListPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pull requests")
		return
	}

	resp := make([]PullRequestResponse, len(prs))
	for i, pr := range prs {
		resp[i] = pullRequestToResponse(pr)
	}

	writeJSON(w, http.StatusOK, resp)
}
