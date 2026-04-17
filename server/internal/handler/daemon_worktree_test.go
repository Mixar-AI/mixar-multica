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

func TestDaemonUpdateWorktree_HeadSHA(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	wsID := setupTestWorkspace(t)
	createRepoForTest(t, wsID, "https://github.com/test/repo.git", "test")

	// Create a worktree first
	body := map[string]any{
		"repository_url": "https://github.com/test/repo.git",
		"path":           "/tmp/wt/upd-abc",
		"branch_name":    "br",
		"base_branch":    "main",
	}
	w := httptest.NewRecorder()
	testHandler.DaemonCreateWorktree(w, newDaemonRequest(t, "POST", "/api/daemon/worktrees", body, wsID))
	if w.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d; body=%s", w.Code, w.Body.String())
	}
	var created WorktreeResponse
	json.Unmarshal(w.Body.Bytes(), &created)

	// Now update head_sha
	patch := map[string]any{"head_sha": "abc1234567"}
	w2 := httptest.NewRecorder()
	req := newDaemonRequest(t, "PATCH", "/api/daemon/worktrees/"+created.ID, patch, wsID)
	req = withURLParam(req, "id", created.ID)
	testHandler.DaemonUpdateWorktree(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w2.Code, w2.Body.String())
	}
	var updated WorktreeResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updated.HeadSHA != "abc1234567" {
		t.Errorf("HeadSHA: got %q", updated.HeadSHA)
	}
}

func TestDaemonDeleteWorktree_SoftDelete(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	wsID := setupTestWorkspace(t)
	createRepoForTest(t, wsID, "https://github.com/test/repo.git", "test")

	body := map[string]any{
		"repository_url": "https://github.com/test/repo.git",
		"path":           "/tmp/wt/del",
		"branch_name":    "br",
		"base_branch":    "main",
	}
	w := httptest.NewRecorder()
	testHandler.DaemonCreateWorktree(w, newDaemonRequest(t, "POST", "/api/daemon/worktrees", body, wsID))
	if w.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d; body=%s", w.Code, w.Body.String())
	}
	var created WorktreeResponse
	json.Unmarshal(w.Body.Bytes(), &created)

	w2 := httptest.NewRecorder()
	req := newDaemonRequest(t, "DELETE", "/api/daemon/worktrees/"+created.ID, nil, wsID)
	req = withURLParam(req, "id", created.ID)
	testHandler.DaemonDeleteWorktree(w2, req)

	if w2.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204; body=%s", w2.Code, w2.Body.String())
	}

	// Verify the row is now deleted-status
	w3 := httptest.NewRecorder()
	req3 := newRequest("GET", "/api/workspaces/"+wsID+"/worktrees/"+created.ID, nil)
	withWorkspace(req3, wsID)
	req3 = withURLParam(req3, "id", created.ID)
	testHandler.GetWorktree(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("get after delete: got %d; body=%s", w3.Code, w3.Body.String())
	}
	var afterDelete WorktreeResponse
	json.Unmarshal(w3.Body.Bytes(), &afterDelete)
	if afterDelete.Status != "deleted" {
		t.Errorf("status after soft-delete: got %q, want deleted", afterDelete.Status)
	}
	if afterDelete.DeletedAt == nil {
		t.Errorf("DeletedAt should be set after soft-delete")
	}
}
