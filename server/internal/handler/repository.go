package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RepositoryResponse is the JSON shape returned by repository endpoints.
type RepositoryResponse struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspace_id"`
	URL           string `json:"url"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
	Platform      string `json:"platform"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// CreateRepositoryRequest is the body for POST /workspaces/:wsId/repositories.
type CreateRepositoryRequest struct {
	URL           string `json:"url"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch,omitempty"` // defaults to "main"
	Description   string `json:"description,omitempty"`
	Platform      string `json:"platform,omitempty"` // defaults to "github"
}

// UpdateRepositoryRequest is the body for PATCH /workspaces/:wsId/repositories/:id.
// All fields are optional. URL and platform are immutable (not present).
type UpdateRepositoryRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	DefaultBranch *string `json:"default_branch,omitempty"`
}

// validateRepositoryURL accepts https://host/path or git@host:path.
// Returns the trimmed canonical form or an error.
// For https URLs, normalizes by stripping trailing slash and .git suffix
// to prevent near-duplicate rows from slightly different input forms.
func validateRepositoryURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("url is required")
	}
	if strings.HasPrefix(raw, "git@") {
		// SSH format: git@host:owner/repo.git — minimal sanity check
		if !strings.Contains(raw, ":") {
			return "", errors.New("invalid ssh url; expected git@host:owner/repo")
		}
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("url must be https or git@ form")
	}
	if u.Host == "" || u.Path == "" || u.Path == "/" {
		return "", errors.New("url must include host and path")
	}
	// Normalize: strip trailing slash and .git to prevent near-duplicate rows.
	// Per code-review feedback: https://github.com/org/repo and
	// https://github.com/org/repo.git should be the same repository.
	raw = strings.TrimSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	return raw, nil
}

func validateRepositoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > 100 {
		return "", errors.New("name must be ≤ 100 characters")
	}
	return name, nil
}

func validatePlatform(platform string) (string, error) {
	if platform == "" {
		return "github", nil
	}
	switch platform {
	case "github", "gitlab", "other":
		return platform, nil
	default:
		return "", errors.New("platform must be one of: github, gitlab, other")
	}
}

// parseUUIDParam extracts and parses a UUID from a chi URL parameter.
func parseUUIDParam(r *http.Request, key string) (pgtype.UUID, bool) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return pgtype.UUID{}, false
	}
	var u pgtype.UUID
	if err := u.Scan(raw); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

// repositoryToResponse converts a db.Repository to a RepositoryResponse.
func repositoryToResponse(r db.Repository) RepositoryResponse {
	return RepositoryResponse{
		ID:            uuidToString(r.ID),
		WorkspaceID:   uuidToString(r.WorkspaceID),
		URL:           r.Url,
		Name:          r.Name,
		DefaultBranch: r.DefaultBranch,
		Description:   r.Description,
		Platform:      r.Platform,
		CreatedAt:     timestampToString(r.CreatedAt),
		UpdatedAt:     timestampToString(r.UpdatedAt),
	}
}

// ListRepositories returns repositories registered in the workspace.
// Route: GET /workspaces/:wsId/repositories
func (h *Handler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	repos, err := h.Queries.ListRepositoriesByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("list repositories", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list repositories")
		return
	}

	resp := make([]RepositoryResponse, 0, len(repos))
	for _, repo := range repos {
		resp = append(resp, repositoryToResponse(repo))
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateRepository registers a new repository in the workspace.
// Route: POST /workspaces/:wsId/repositories
func (h *Handler) CreateRepository(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	var req CreateRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	urlStr, err := validateRepositoryURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := validateRepositoryName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	platform, err := validatePlatform(req.Platform)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	defaultBranch := strings.TrimSpace(req.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	repo, err := h.Queries.CreateRepository(r.Context(), db.CreateRepositoryParams{
		WorkspaceID:   parseUUID(workspaceID),
		Url:           urlStr,
		Name:          name,
		DefaultBranch: defaultBranch,
		Description:   strings.TrimSpace(req.Description),
		Platform:      platform,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "repository with this url already exists")
			return
		}
		slog.Error("create repository", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create repository")
		return
	}

	writeJSON(w, http.StatusCreated, repositoryToResponse(repo))
}

// GetRepository returns a single repository by ID.
// Route: GET /workspaces/:wsId/repositories/:id
func (h *Handler) GetRepository(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	repo, err := h.Queries.GetRepository(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		slog.Error("get repository", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get repository")
		return
	}

	writeJSON(w, http.StatusOK, repositoryToResponse(repo))
}

// UpdateRepository updates name / description / default_branch.
// URL and platform are immutable per the spec.
// Route: PATCH /workspaces/:wsId/repositories/:id
func (h *Handler) UpdateRepository(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req UpdateRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	params := db.UpdateRepositoryParams{ID: id}
	if req.Name != nil {
		name, err := validateRepositoryName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: strings.TrimSpace(*req.Description), Valid: true}
	}
	if req.DefaultBranch != nil {
		branch := strings.TrimSpace(*req.DefaultBranch)
		if branch == "" {
			writeError(w, http.StatusBadRequest, "default_branch cannot be empty")
			return
		}
		params.DefaultBranch = pgtype.Text{String: branch, Valid: true}
	}

	repo, err := h.Queries.UpdateRepository(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		slog.Error("update repository", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update repository")
		return
	}

	writeJSON(w, http.StatusOK, repositoryToResponse(repo))
}
