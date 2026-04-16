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
