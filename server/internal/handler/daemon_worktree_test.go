package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newDaemonRequest creates an HTTP request with daemon-token auth context.
// Mirrors the pattern from daemon_test.go (newDaemonTokenRequest) but uses the
// daemon context helper so the workspace ID is set correctly.
func newDaemonRequest(t *testing.T, method, url string, body any, wsID string) *http.Request {
	t.Helper()
	req := newRequest(method, url, body)
	withWorkspace(req, wsID)
	return req
}

func TestDaemonCreateWorktree_HappyPath(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	wsID := setupTestWorkspace(t)
	repo := createRepoForTest(t, wsID, "https://github.com/test/repo.git", "test")

	body := map[string]any{
		"repository_url": "https://github.com/test/repo.git",
		"path":           "/tmp/wt/abc",
		"branch_name":    "agent/claude/abc",
		"base_branch":    "main",
	}
	w := httptest.NewRecorder()
	req := newDaemonRequest(t, "POST", "/api/daemon/worktrees", body, wsID)
	testHandler.DaemonCreateWorktree(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var got WorktreeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RepositoryID != repo.ID {
		t.Errorf("RepositoryID: got %q, want %q", got.RepositoryID, repo.ID)
	}
	if got.Path != "/tmp/wt/abc" {
		t.Errorf("Path: got %q", got.Path)
	}
	if got.Status != "active" {
		t.Errorf("Status: got %q, want active", got.Status)
	}
}

func TestDaemonCreateWorktree_UnregisteredURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	wsID := setupTestWorkspace(t)

	body := map[string]any{
		"repository_url": "https://github.com/never/registered.git",
		"path":           "/tmp/wt/xyz",
		"branch_name":    "branch",
		"base_branch":    "main",
	}
	w := httptest.NewRecorder()
	req := newDaemonRequest(t, "POST", "/api/daemon/worktrees", body, wsID)
	testHandler.DaemonCreateWorktree(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}
