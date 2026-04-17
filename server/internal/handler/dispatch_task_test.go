package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createAgentWithRuntimeForDispatch creates a runtime + agent in the workspace and
// returns the agent ID as a string. Cleans up via t.Cleanup.
func createAgentWithRuntimeForDispatch(t *testing.T, wsID string) string {
	t.Helper()
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'dispatch_test_provider', 'online', '{}', '{}', now())
		RETURNING id
	`, wsID, fmt.Sprintf("dispatch-test-runtime-%s", wsID)).Scan(&runtimeID); err != nil {
		t.Fatalf("createAgentWithRuntimeForDispatch: create runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Dispatch Test Agent', '', 'cloud', '{}', $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("createAgentWithRuntimeForDispatch: create agent: %v", err)
	}

	return agentID
}

// createIssueWithAgentForDispatch creates a todo issue assigned to agentID.
func createIssueWithAgentForDispatch(t *testing.T, wsID, agentID string) string {
	t.Helper()
	ctx := context.Background()

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			assignee_type, assignee_id
		)
		VALUES ($1, 'Dispatch test issue', 'todo', 'medium', 'member', $2, 'agent', $3)
		RETURNING id
	`, wsID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("createIssueWithAgentForDispatch: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func TestDispatchTask_NoPickerFields(t *testing.T) {
	wsID := setupTestWorkspace(t)
	agentID := createAgentWithRuntimeForDispatch(t, wsID)
	issueID := createIssueWithAgentForDispatch(t, wsID, agentID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/dispatch", map[string]any{})
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", issueID)

	testHandler.DispatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var got AgentTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if got.Status != "queued" {
		t.Errorf("status: got %q, want queued", got.Status)
	}
}

func TestDispatchTask_WithBaseBranch(t *testing.T) {
	wsID := setupTestWorkspace(t)
	agentID := createAgentWithRuntimeForDispatch(t, wsID)
	issueID := createIssueWithAgentForDispatch(t, wsID, agentID)

	branch := "feat/my-feature"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/dispatch", map[string]any{
		"base_branch": branch,
	})
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", issueID)

	testHandler.DispatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
}

func TestDispatchTask_InvalidRepositoryID(t *testing.T) {
	wsID := setupTestWorkspace(t)
	agentID := createAgentWithRuntimeForDispatch(t, wsID)
	issueID := createIssueWithAgentForDispatch(t, wsID, agentID)

	// Use a valid UUID that doesn't exist in the workspace.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/dispatch", map[string]any{
		"repository_id": "00000000-0000-0000-0000-000000000001",
	})
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", issueID)

	testHandler.DispatchTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400 for invalid repo; body=%s", w.Code, w.Body.String())
	}
}

func TestDispatchTask_ValidRepositoryID(t *testing.T) {
	wsID := setupTestWorkspace(t)
	agentID := createAgentWithRuntimeForDispatch(t, wsID)
	issueID := createIssueWithAgentForDispatch(t, wsID, agentID)
	repo := createRepoForTest(t, wsID, "https://github.com/test/dispatch-repo.git", "dispatch-repo")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/dispatch", map[string]any{
		"repository_id":  repo.ID,
		"base_branch":    "main",
		"reuse_worktree": true,
		"sparse_paths":   []string{"packages/web/", "packages/core/"},
	})
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", issueID)

	testHandler.DispatchTask(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
}

func TestDispatchTask_IssueNotAssignedToAgent(t *testing.T) {
	wsID := setupTestWorkspace(t)

	// Create an issue without an agent assignee.
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id)
		VALUES ($1, 'Unassigned issue', 'todo', 'medium', 'member', $2)
		RETURNING id
	`, wsID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueID+"/dispatch", map[string]any{})
	withWorkspace(req, wsID)
	req = withURLParam(req, "id", issueID)

	testHandler.DispatchTask(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422 for unassigned issue; body=%s", w.Code, w.Body.String())
	}
}
