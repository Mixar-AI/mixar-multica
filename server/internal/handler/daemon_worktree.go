package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DaemonCreateWorktreeRequest is the body for POST /daemon/worktrees.
type DaemonCreateWorktreeRequest struct {
	RepositoryURL string  `json:"repository_url"`
	TaskID        *string `json:"task_id,omitempty"`
	Path          string  `json:"path"`
	BranchName    string  `json:"branch_name"`
	BaseBranch    string  `json:"base_branch"`
	HeadSHA       string  `json:"head_sha,omitempty"`
}

// DaemonCreateWorktree records a new worktree the daemon just created.
// Route: POST /daemon/worktrees
func (h *Handler) DaemonCreateWorktree(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	var req DaemonCreateWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Normalize the incoming URL before lookup so that ".git" suffix variants match.
	normalizedURL, err := validateRepositoryURL(req.RepositoryURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository_url: "+err.Error())
		return
	}

	repo, err := h.Queries.GetRepositoryByURL(r.Context(), db.GetRepositoryByURLParams{
		WorkspaceID: parseUUID(workspaceID),
		Url:         normalizedURL,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "repository not registered: "+req.RepositoryURL)
			return
		}
		slog.Error("get repo by url", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to lookup repository")
		return
	}

	var taskID pgtype.UUID
	if req.TaskID != nil {
		if err := taskID.Scan(*req.TaskID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid task_id")
			return
		}
	}

	wt, err := h.Queries.CreateWorktree(r.Context(), db.CreateWorktreeParams{
		RepositoryID: repo.ID,
		TaskID:       taskID,
		Path:         req.Path,
		BranchName:   req.BranchName,
		BaseBranch:   req.BaseBranch,
		HeadSha:      req.HeadSHA,
	})
	if err != nil {
		if isUniqueViolation(err) {
			// Race: worktree already exists for (repo, path). Look up + return existing.
			existing, lookupErr := h.Queries.GetWorktreeByPath(r.Context(), db.GetWorktreeByPathParams{
				RepositoryID: repo.ID,
				Path:         req.Path,
			})
			if lookupErr != nil {
				slog.Error("worktree race lookup", "error", lookupErr)
				writeError(w, http.StatusInternalServerError, "worktree race")
				return
			}
			writeJSON(w, http.StatusOK, worktreeToResponse(existing))
			return
		}
		slog.Error("create worktree", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create worktree")
		return
	}

	writeJSON(w, http.StatusCreated, worktreeToResponse(wt))
}

// DaemonUpdateWorktreeRequest is the body for PATCH /daemon/worktrees/:id.
// All fields optional.
type DaemonUpdateWorktreeRequest struct {
	Status     *string `json:"status,omitempty"`
	HeadSHA    *string `json:"head_sha,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"` // RFC3339; daemon may also send "now"
	TaskID     *string `json:"task_id,omitempty"`
}

// DaemonUpdateWorktree updates worktree state. Status / head_sha / last_used_at / task_id all optional.
// Route: PATCH /daemon/worktrees/:id
//
// TODO(security): verify worktree's repository belongs to the requesting workspace.
// For v1 this is acceptable — daemon-token auth is workspace-scoped and worktree
// IDs are UUIDs. Sub-project E or a follow-up should add the scope check.
func (h *Handler) DaemonUpdateWorktree(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req DaemonUpdateWorktreeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	params := db.UpdateWorktreeParams{ID: id}
	if req.Status != nil {
		switch *req.Status {
		case "active", "inactive", "deleted":
			params.Status = pgtype.Text{String: *req.Status, Valid: true}
		default:
			writeError(w, http.StatusBadRequest, "status must be active|inactive|deleted")
			return
		}
	}
	if req.HeadSHA != nil {
		params.HeadSha = pgtype.Text{String: *req.HeadSHA, Valid: true}
	}
	if req.LastUsedAt != nil {
		t, err := time.Parse(time.RFC3339, *req.LastUsedAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "last_used_at must be RFC3339")
			return
		}
		params.LastUsedAt = pgtype.Timestamptz{Time: t, Valid: true}
	}
	if req.TaskID != nil {
		if *req.TaskID == "" {
			params.TaskID = pgtype.UUID{Valid: false} // explicit clear
		} else {
			var tid pgtype.UUID
			if err := tid.Scan(*req.TaskID); err != nil {
				writeError(w, http.StatusBadRequest, "invalid task_id")
				return
			}
			params.TaskID = tid
		}
	}

	wt, err := h.Queries.UpdateWorktree(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "worktree not found")
			return
		}
		slog.Error("update worktree", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update worktree")
		return
	}

	writeJSON(w, http.StatusOK, worktreeToResponse(wt))
}

// DaemonDeleteWorktree soft-deletes a worktree (sets status='deleted', deleted_at=NOW()).
// Idempotent — returns 204 even if already deleted.
// Route: DELETE /daemon/worktrees/:id
//
// TODO(security): verify worktree's repository belongs to the requesting workspace.
// For v1 this is acceptable — daemon-token auth is workspace-scoped and worktree
// IDs are UUIDs. Sub-project E or a follow-up should add the scope check.
func (h *Handler) DaemonDeleteWorktree(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.Queries.SoftDeleteWorktree(r.Context(), id); err != nil {
		slog.Error("soft-delete worktree", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete worktree")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
