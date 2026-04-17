package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/crypto"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// githubOAuthConfig holds the GitHub OAuth App credentials read from env.
type githubOAuthConfig struct {
	ClientID     string
	ClientSecret string
}

func getGitHubOAuthConfig() githubOAuthConfig {
	return githubOAuthConfig{
		ClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
	}
}

// IntegrationResponse is the JSON shape returned to clients (no access_token).
type IntegrationResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Platform     string `json:"platform"`
	AccountLogin string `json:"account_login"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func integrationToResponse(i db.WorkspaceIntegration) IntegrationResponse {
	return IntegrationResponse{
		ID:           uuidToString(i.ID),
		WorkspaceID:  uuidToString(i.WorkspaceID),
		Platform:     i.Platform,
		AccountLogin: i.AccountLogin,
		CreatedAt:    timestampToString(i.CreatedAt),
		UpdatedAt:    timestampToString(i.UpdatedAt),
	}
}

// GitHubRepoItem is one repository entry returned by the list-repositories endpoint.
type GitHubRepoItem struct {
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
}

// ListIntegrations returns all integrations for the workspace.
// Route: GET /api/integrations
func (h *Handler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	integrations, err := h.Queries.ListWorkspaceIntegrations(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Error("list integrations", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list integrations")
		return
	}

	resp := make([]IntegrationResponse, 0, len(integrations))
	for _, i := range integrations {
		resp = append(resp, integrationToResponse(i))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GitHubAuthorizeURL returns the GitHub OAuth authorization URL.
// Route: GET /api/integrations/github/authorize
func (h *Handler) GitHubAuthorizeURL(w http.ResponseWriter, r *http.Request) {
	cfg := getGitHubOAuthConfig()
	if cfg.ClientID == "" {
		writeError(w, http.StatusServiceUnavailable, "GitHub OAuth not configured")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	state, err := buildOAuthState(workspaceID)
	if err != nil {
		slog.Error("build oauth state", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to build oauth state")
		return
	}

	params := url.Values{
		"client_id": {cfg.ClientID},
		"scope":     {"repo read:org"},
		"state":     {state},
	}
	authorizeURL := "https://github.com/login/oauth/authorize?" + params.Encode()

	writeJSON(w, http.StatusOK, map[string]string{"authorize_url": authorizeURL})
}

// GitHubCallback handles the OAuth callback from GitHub.
// Route: GET /api/integrations/github/callback?code=X&state=Y
func (h *Handler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	cfg := getGitHubOAuthConfig()
	if cfg.ClientID == "" {
		writeError(w, http.StatusServiceUnavailable, "GitHub OAuth not configured")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}

	workspaceID, err := parseOAuthState(state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid state")
		return
	}

	// Exchange code for access token.
	token, scopes, login, err := exchangeGitHubCode(r.Context(), cfg, code)
	if err != nil {
		slog.Error("github oauth exchange", "error", err)
		writeError(w, http.StatusBadGateway, "failed to exchange GitHub code")
		return
	}

	// Encrypt the token before storage.
	encrypted, err := crypto.EncryptToken(token)
	if err != nil {
		slog.Error("encrypt github token", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token: MULTICA_INTEGRATION_KEY may not be configured")
		return
	}

	_, err = h.Queries.UpsertWorkspaceIntegration(r.Context(), db.UpsertWorkspaceIntegrationParams{
		WorkspaceID:  parseUUID(workspaceID),
		Platform:     "github",
		AccountLogin: login,
		AccessToken:  encrypted,
		Scopes:       scopes,
	})
	if err != nil {
		slog.Error("upsert workspace integration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store integration")
		return
	}

	// Redirect back to settings with success indicator.
	http.Redirect(w, r, "/settings/integrations?connected=github", http.StatusFound)
}

// ListGitHubRepositories returns repositories accessible to the workspace's GitHub integration.
// Route: GET /api/integrations/github/repositories
func (h *Handler) ListGitHubRepositories(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "missing workspace id")
		return
	}

	integration, err := h.Queries.GetWorkspaceIntegration(r.Context(), db.GetWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Platform:    "github",
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "no GitHub integration for this workspace")
			return
		}
		slog.Error("get workspace integration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get integration")
		return
	}

	token, err := crypto.DecryptToken(integration.AccessToken)
	if err != nil {
		slog.Error("decrypt github token", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to decrypt token")
		return
	}

	repos, err := fetchGitHubRepos(r.Context(), token)
	if err != nil {
		slog.Error("fetch github repos", "error", err)
		writeError(w, http.StatusBadGateway, "failed to fetch GitHub repositories")
		return
	}

	writeJSON(w, http.StatusOK, repos)
}

// buildOAuthState creates base64url(workspaceID + ":" + random nonce).
func buildOAuthState(workspaceID string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	raw := workspaceID + ":" + fmt.Sprintf("%x", nonce)
	return base64.URLEncoding.EncodeToString([]byte(raw)), nil
}

// parseOAuthState extracts workspaceID from the state param.
func parseOAuthState(state string) (string, error) {
	raw, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", fmt.Errorf("invalid state format")
	}
	return parts[0], nil
}

// exchangeGitHubCode exchanges a code for an access token and fetches the login.
// Returns (token, scopes, login, error).
func exchangeGitHubCode(ctx context.Context, cfg githubOAuthConfig, code string) (string, []string, string, error) {
	params := url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(params.Encode()))
	if err != nil {
		return "", nil, "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, "", fmt.Errorf("read token response: %w", err)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", nil, "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return "", nil, "", fmt.Errorf("github oauth error: %s", tokenResp.Error)
	}
	if tokenResp.AccessToken == "" {
		return "", nil, "", fmt.Errorf("no access_token in response")
	}

	scopes := parseScopes(tokenResp.Scope)
	login, err := fetchGitHubLogin(ctx, tokenResp.AccessToken)
	if err != nil {
		return "", nil, "", fmt.Errorf("fetch github user: %w", err)
	}

	return tokenResp.AccessToken, scopes, login, nil
}

func parseScopes(raw string) []string {
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func fetchGitHubLogin(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github user fetch status %d", resp.StatusCode)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	return user.Login, nil
}

// fetchGitHubRepos calls GitHub's /user/repos API and returns simplified results.
func fetchGitHubRepos(ctx context.Context, token string) ([]GitHubRepoItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/user/repos?per_page=100&affiliation=owner,collaborator,organization_member",
		nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github repos fetch status %d", resp.StatusCode)
	}

	var raw []struct {
		FullName      string `json:"full_name"`
		CloneURL      string `json:"clone_url"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
		Description   string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]GitHubRepoItem, 0, len(raw))
	for _, r := range raw {
		out = append(out, GitHubRepoItem{
			FullName:      r.FullName,
			CloneURL:      r.CloneURL,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
			Description:   r.Description,
		})
	}
	return out, nil
}
