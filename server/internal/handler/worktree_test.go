package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListWorktreesByWorkspace_Empty(t *testing.T) {
	wsID := setupTestWorkspace(t)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+wsID+"/worktrees", nil)
	withWorkspace(req, wsID)
	testHandler.ListWorktreesByWorkspace(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var got []WorktreeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}
