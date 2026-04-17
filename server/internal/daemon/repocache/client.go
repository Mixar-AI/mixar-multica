package repocache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// WorktreeClient calls the server's /daemon/worktrees endpoints.
// One instance per daemon, shared across all repocache callers.
type WorktreeClient struct {
	baseURL string       // e.g., http://localhost:8080
	token   string       // daemon auth token
	wsID    string       // workspace ID (header X-Workspace-ID)
	http    *http.Client
}

// NewWorktreeClient constructs a client for the given server.
// token is the daemon's auth token; wsID is the workspace this daemon serves.
func NewWorktreeClient(baseURL, token, wsID string, httpClient *http.Client) *WorktreeClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &WorktreeClient{baseURL: baseURL, token: token, wsID: wsID, http: httpClient}
}

// CreateWorktreeRequest matches handler.DaemonCreateWorktreeRequest.
type CreateWorktreeRequest struct {
	RepositoryURL string  `json:"repository_url"`
	TaskID        *string `json:"task_id,omitempty"`
	Path          string  `json:"path"`
	BranchName    string  `json:"branch_name"`
	BaseBranch    string  `json:"base_branch"`
	HeadSHA       string  `json:"head_sha,omitempty"`
}

// WorktreeRow is the subset of the server's WorktreeResponse the daemon needs.
type WorktreeRow struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

// Create posts a new worktree to the server. Returns the row including the
// server-assigned UUID. If the server returns 200 (already-exists race),
// Create still returns the row.
func (c *WorktreeClient) Create(ctx context.Context, req CreateWorktreeRequest) (*WorktreeRow, error) {
	return c.do(ctx, http.MethodPost, "/api/daemon/worktrees", req)
}

// UpdateWorktreeRequest matches handler.DaemonUpdateWorktreeRequest.
type UpdateWorktreeRequest struct {
	Status     *string `json:"status,omitempty"`
	HeadSHA    *string `json:"head_sha,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	TaskID     *string `json:"task_id,omitempty"`
}

// Update patches a worktree's mutable fields.
func (c *WorktreeClient) Update(ctx context.Context, id string, req UpdateWorktreeRequest) (*WorktreeRow, error) {
	return c.do(ctx, http.MethodPatch, "/api/daemon/worktrees/"+id, req)
}

// Delete soft-deletes a worktree. Returns nil on success (204) or the existing
// row was already deleted.
func (c *WorktreeClient) Delete(ctx context.Context, id string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/daemon/worktrees/"+id, nil)
	if err != nil {
		return err
	}
	c.injectAuth(httpReq)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete worktree: status %d: %s", resp.StatusCode, body)
	}
	return nil
}

func (c *WorktreeClient) do(ctx context.Context, method, path string, body any) (*WorktreeRow, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.injectAuth(httpReq)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, bodyBytes)
	}
	var row WorktreeRow
	if err := json.NewDecoder(resp.Body).Decode(&row); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &row, nil
}

func (c *WorktreeClient) injectAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Workspace-ID", c.wsID)
}
