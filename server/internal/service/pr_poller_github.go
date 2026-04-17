package service

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/github"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// githubClientAdapter adapts *github.Client to the GitHubClient interface.
type githubClientAdapter struct {
	c *github.Client
}

func (a *githubClientAdapter) FetchPR(ctx context.Context, owner, repo string, number int) (*GHPullRequest, error) {
	p, err := a.c.FetchPR(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	return &GHPullRequest{
		Number:    p.Number,
		State:     p.State,
		Draft:     p.Draft,
		Merged:    p.Merged,
		Title:     p.Title,
		HeadRef:   p.HeadRef,
		BaseRef:   p.BaseRef,
		MergedAt:  p.MergedAt,
		ClosedAt:  p.ClosedAt,
		UpdatedAt: p.UpdatedAt,
	}, nil
}

func (a *githubClientAdapter) FetchReviewComments(ctx context.Context, owner, repo string, number int, since time.Time) ([]GHReviewComment, error) {
	raw, err := a.c.FetchReviewComments(ctx, owner, repo, number, since)
	if err != nil {
		return nil, err
	}
	out := make([]GHReviewComment, len(raw))
	for i, r := range raw {
		out[i] = GHReviewComment{
			ID:        r.ID,
			Body:      r.Body,
			User:      r.User,
			CreatedAt: r.CreatedAt,
			HTMLURL:   r.HTMLURL,
		}
	}
	return out, nil
}

// NewPRPollerFromGitHubClient creates a PRPoller backed by a real *github.Client.
// Use this in main; use NewPRPoller with a GitHubClient mock in tests.
func NewPRPollerFromGitHubClient(q *db.Queries, c *github.Client, taskSvc *TaskService) *PRPoller {
	return NewPRPoller(q, &githubClientAdapter{c: c}, taskSvc)
}
