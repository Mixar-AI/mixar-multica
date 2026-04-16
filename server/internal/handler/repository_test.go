package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setupTestWorkspace creates a fresh isolated workspace for a single test and
// cleans it up on t.Cleanup. Returns the workspace UUID string.
func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("repo-test-%s", t.Name())
	// Sanitize slug: test names may contain slashes.
	for i := 0; i < len(slug); i++ {
		if slug[i] == '/' || slug[i] == ' ' {
			slug = slug[:i] + "-" + slug[i+1:]
		}
	}
	// Truncate to avoid overly long slugs.
	if len(slug) > 63 {
		slug = slug[:63]
	}

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'RT')
		RETURNING id
	`, "Repo Test WS "+t.Name(), slug).Scan(&wsID); err != nil {
		t.Fatalf("setupTestWorkspace: create workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("setupTestWorkspace: add member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	return wsID
}

// withWorkspace sets the X-Workspace-ID header on the request so that
// resolveWorkspaceID returns the given workspace ID.
func withWorkspace(req *http.Request, wsID string) {
	req.Header.Set("X-Workspace-ID", wsID)
}

func TestListRepositories_Empty(t *testing.T) {
	wsID := setupTestWorkspace(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+wsID+"/repositories", nil)
	withWorkspace(req, wsID)
	testHandler.ListRepositories(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got []RepositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d", len(got))
	}
}

func TestCreateRepository_HappyPath(t *testing.T) {
	wsID := setupTestWorkspace(t)

	body := map[string]any{
		"url":            "https://github.com/multica-ai/multica.git",
		"name":           "multica",
		"default_branch": "main",
		"description":    "Multica repo",
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+wsID+"/repositories", body)
	withWorkspace(req, wsID)
	testHandler.CreateRepository(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var got RepositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// URL normalization strips .git; expect normalized form.
	if got.URL != "https://github.com/multica-ai/multica" {
		t.Errorf("URL: got %q, want normalized form without .git", got.URL)
	}
	if got.Name != "multica" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.DefaultBranch != "main" {
		t.Errorf("DefaultBranch: got %q", got.DefaultBranch)
	}
	if got.Platform != "github" {
		t.Errorf("Platform default: got %q", got.Platform)
	}
}

func TestCreateRepository_DuplicateURL(t *testing.T) {
	wsID := setupTestWorkspace(t)
	createRepoForTest(t, wsID, "https://github.com/foo/bar.git", "bar")

	body := map[string]any{
		"url":  "https://github.com/foo/bar.git",
		"name": "bar2",
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+wsID+"/repositories", body)
	withWorkspace(req, wsID)
	testHandler.CreateRepository(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body=%s", w.Code, w.Body.String())
	}
}

func TestCreateRepository_DuplicateURL_NormalizedEquivalent(t *testing.T) {
	wsID := setupTestWorkspace(t)
	// Create with .git suffix — stored as normalized (without .git)
	createRepoForTest(t, wsID, "https://github.com/foo/bar.git/", "bar")

	// Attempt to create again without .git — should normalize to same URL → 409
	body := map[string]any{
		"url":  "https://github.com/foo/bar",
		"name": "bar-nodotgit",
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+wsID+"/repositories", body)
	withWorkspace(req, wsID)
	testHandler.CreateRepository(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409 (normalized duplicate); body=%s", w.Code, w.Body.String())
	}
}

func TestCreateRepository_InvalidURL(t *testing.T) {
	wsID := setupTestWorkspace(t)

	body := map[string]any{
		"url":  "not-a-url",
		"name": "thing",
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+wsID+"/repositories", body)
	withWorkspace(req, wsID)
	testHandler.CreateRepository(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

// createRepoForTest is a test-only helper used by tests that need a pre-existing
// repository in the workspace.
func createRepoForTest(t *testing.T, wsID, url, name string) RepositoryResponse {
	t.Helper()
	body := map[string]any{"url": url, "name": name}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+wsID+"/repositories", body)
	withWorkspace(req, wsID)
	testHandler.CreateRepository(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createRepoForTest: status %d, body=%s", w.Code, w.Body.String())
	}
	var got RepositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("createRepoForTest unmarshal: %v", err)
	}
	return got
}

func TestGetRepository_HappyPath(t *testing.T) {
	wsID := setupTestWorkspace(t)
	created := createRepoForTest(t, wsID, "https://github.com/test/repo.git", "test")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+wsID+"/repositories/"+created.ID, nil)
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetRepository(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got RepositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, created.ID)
	}
}

func TestGetRepository_NotFound(t *testing.T) {
	wsID := setupTestWorkspace(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+wsID+"/repositories/00000000-0000-0000-0000-000000000000", nil)
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	testHandler.GetRepository(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}

func TestUpdateRepository_HappyPath(t *testing.T) {
	wsID := setupTestWorkspace(t)
	created := createRepoForTest(t, wsID, "https://github.com/test/repo.git", "test")

	newName := "test-renamed"
	body := map[string]any{"name": newName}
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+wsID+"/repositories/"+created.ID, body)
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateRepository(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got RepositoryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != newName {
		t.Errorf("Name: got %q, want %q", got.Name, newName)
	}
	// URL should be unchanged (normalized form, without .git)
	if got.URL != "https://github.com/test/repo" {
		t.Errorf("URL changed unexpectedly: got %q", got.URL)
	}
}

func TestUpdateRepository_NotFound(t *testing.T) {
	wsID := setupTestWorkspace(t)
	body := map[string]any{"name": "x"}
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+wsID+"/repositories/00000000-0000-0000-0000-000000000000", body)
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	testHandler.UpdateRepository(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", w.Code)
	}
}
