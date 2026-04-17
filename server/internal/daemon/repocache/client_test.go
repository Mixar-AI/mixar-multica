package repocache

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorktreeClient_CreateSendsCorrectRequest(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/daemon/worktrees" {
			t.Errorf("path: got %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing Bearer auth: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Workspace-ID") != "ws-123" {
			t.Errorf("X-Workspace-ID: got %q", r.Header.Get("X-Workspace-ID"))
		}
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(WorktreeRow{ID: "wt-abc", Path: "/tmp/wt", Status: "active"})
	}))
	defer srv.Close()

	c := NewWorktreeClient(srv.URL, "tok-xyz", "ws-123", srv.Client())
	row, err := c.Create(context.Background(), CreateWorktreeRequest{
		RepositoryURL: "https://github.com/x/y.git",
		Path:          "/tmp/wt",
		BranchName:    "br",
		BaseBranch:    "main",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.ID != "wt-abc" {
		t.Errorf("ID: got %q", row.ID)
	}

	// Verify body roundtrip
	var got CreateWorktreeRequest
	if err := json.Unmarshal(capturedBody, &got); err != nil {
		t.Fatalf("unmarshal captured body: %v", err)
	}
	if got.RepositoryURL != "https://github.com/x/y.git" {
		t.Errorf("RepositoryURL roundtrip failed: got %q", got.RepositoryURL)
	}
}
