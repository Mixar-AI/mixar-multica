package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

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
