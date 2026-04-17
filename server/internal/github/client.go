// Package github provides a minimal client for the GitHub REST API,
// used by the PR polling loop (sub-project D) to sync PR state.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is a minimal GitHub REST API client.
type Client struct {
	token string
	http  *http.Client
	base  string // defaults to "https://api.github.com"
}

// NewClient constructs a Client using the given personal access token.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 15 * time.Second},
		base:  "https://api.github.com",
	}
}

// PR captures the fields we care about from the GitHub REST API.
type PR struct {
	Number    int        `json:"number"`
	State     string     `json:"state"`     // "open" | "closed"
	Draft     bool       `json:"draft"`
	Merged    bool       `json:"merged"`
	Title     string     `json:"title"`
	HeadRef   string     `json:"head_ref"`
	BaseRef   string     `json:"base_ref"`
	MergedAt  *time.Time `json:"merged_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ReviewComment is a subset of GitHub's review comment fields.
type ReviewComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
}

// githubPR is the raw GitHub API shape for a pull request.
type githubPR struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
	Merged bool   `json:"merged"`
	Title  string `json:"title"`
	Head   struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	MergedAt  *time.Time `json:"merged_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// FetchPR returns the current state of a pull request.
func (c *Client) FetchPR(ctx context.Context, owner, repo string, number int) (*PR, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.base, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	c.authHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("github: PR %s/%s#%d not found", owner, repo, number)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github: PR fetch status %d", resp.StatusCode)
	}

	var raw githubPR
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("github: decode PR: %w", err)
	}

	return &PR{
		Number:    raw.Number,
		State:     raw.State,
		Draft:     raw.Draft,
		Merged:    raw.Merged,
		Title:     raw.Title,
		HeadRef:   raw.Head.Ref,
		BaseRef:   raw.Base.Ref,
		MergedAt:  raw.MergedAt,
		ClosedAt:  raw.ClosedAt,
		UpdatedAt: raw.UpdatedAt,
	}, nil
}

// FetchReviewComments returns review comments created at or after `since`.
func (c *Client) FetchReviewComments(ctx context.Context, owner, repo string, number int, since time.Time) ([]ReviewComment, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments?since=%s&per_page=100",
		c.base, owner, repo, number, since.Format(time.RFC3339))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	c.authHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github: comments fetch status %d", resp.StatusCode)
	}

	var raw []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt time.Time `json:"created_at"`
		HTMLURL   string    `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("github: decode comments: %w", err)
	}

	out := make([]ReviewComment, 0, len(raw))
	for _, r := range raw {
		out = append(out, ReviewComment{
			ID:        r.ID,
			Body:      r.Body,
			User:      r.User.Login,
			CreatedAt: r.CreatedAt,
			HTMLURL:   r.HTMLURL,
		})
	}
	return out, nil
}

func (c *Client) authHeaders(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}
