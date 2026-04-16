package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorktreeResponse is the JSON shape for worktree endpoints.
type WorktreeResponse struct {
	ID           string   `json:"id"`
	RepositoryID string   `json:"repository_id"`
	TaskID       *string  `json:"task_id"`
	Path         string   `json:"path"`
	BranchName   string   `json:"branch_name"`
	BaseBranch   string   `json:"base_branch"`
	Status       string   `json:"status"`
	HeadSHA      string   `json:"head_sha"`
	SparsePaths  []string `json:"sparse_paths"`
	CreatedAt    string   `json:"created_at"`
	LastUsedAt   string   `json:"last_used_at"`
	DeletedAt    *string  `json:"deleted_at"`
}

func worktreeToResponse(wt db.Worktree) WorktreeResponse {
	return WorktreeResponse{
		ID:           uuidToString(wt.ID),
		RepositoryID: uuidToString(wt.RepositoryID),
		TaskID:       uuidToPtr(wt.TaskID),
		Path:         wt.Path,
		BranchName:   wt.BranchName,
		BaseBranch:   wt.BaseBranch,
		Status:       wt.Status,
		HeadSHA:      wt.HeadSha,
		SparsePaths:  wt.SparsePaths,
		CreatedAt:    timestampToString(wt.CreatedAt),
		LastUsedAt:   timestampToString(wt.LastUsedAt),
		DeletedAt:    timestampToPtr(wt.DeletedAt),
	}
}

// ListWorktreesByWorkspace returns worktrees for any repository in the workspace.
// Query param: ?include_inactive=true to include inactive/deleted (default false).
// Route: GET /workspaces/:wsId/worktrees
func (h *Handler) ListWorktreesByWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	worktrees, err := h.Queries.ListWorktreesByWorkspace(r.Context(), db.ListWorktreesByWorkspaceParams{
		WorkspaceID:     parseUUID(workspaceID),
		IncludeInactive: pgtype.Bool{Bool: includeInactive, Valid: true},
	})
	if err != nil {
		slog.Error("list worktrees", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list worktrees")
		return
	}

	resp := make([]WorktreeResponse, 0, len(worktrees))
	for _, wt := range worktrees {
		resp = append(resp, worktreeToResponse(wt))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetWorktree returns one worktree by ID.
// Route: GET /workspaces/:wsId/worktrees/:id
func (h *Handler) GetWorktree(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	wt, err := h.Queries.GetWorktree(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "worktree not found")
			return
		}
		slog.Error("get worktree", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get worktree")
		return
	}

	writeJSON(w, http.StatusOK, worktreeToResponse(wt))
}

// ListWorktreesByRepository returns worktrees for one repository.
// Route: GET /workspaces/:wsId/repositories/:repoId/worktrees
func (h *Handler) ListWorktreesByRepository(w http.ResponseWriter, r *http.Request) {
	repoID, ok := parseUUIDParam(r, "repoId")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid repo id")
		return
	}

	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	worktrees, err := h.Queries.ListWorktreesByRepository(r.Context(), db.ListWorktreesByRepositoryParams{
		RepositoryID:    repoID,
		IncludeInactive: pgtype.Bool{Bool: includeInactive, Valid: true},
	})
	if err != nil {
		slog.Error("list worktrees by repo", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list worktrees")
		return
	}

	resp := make([]WorktreeResponse, 0, len(worktrees))
	for _, wt := range worktrees {
		resp = append(resp, worktreeToResponse(wt))
	}
	writeJSON(w, http.StatusOK, resp)
}
