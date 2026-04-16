package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
