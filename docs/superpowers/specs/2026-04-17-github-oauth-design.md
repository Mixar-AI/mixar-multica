# GitHub OAuth + Repo Picker UI — Design + Plan (Sub-project E)

**Status:** Proposed (user-directed execution)
**Date:** 2026-04-17
**Depends on:** Sub-projects A (repository table), D (uses GitHub tokens for polling — this PR moves from env-var PAT to per-workspace OAuth tokens)

## Problem

- Adding a repository requires the operator to paste a git URL by hand. Annoying and error-prone.
- The GitHub token for sub-project D's poller is a single global `GITHUB_TOKEN` env var. Per-workspace tokens don't exist, so the poller can't hit private repos owned by different orgs.

Sub-project E solves both: workspace-scoped **OAuth GitHub App installation** replaces the manual URL entry with a picker, and gives per-workspace access tokens for polling.

## Goals

1. Per-workspace GitHub OAuth flow: "Connect GitHub" button → redirect to GitHub → callback stores encrypted token on a new `workspace_integration` row.
2. **Repo picker UI** replaces the manual URL entry in the existing add-repository dialog: shows installed-app repos + option to "Paste URL manually" fallback.
3. **PR poller uses per-workspace tokens** instead of the global `GITHUB_TOKEN` env var. Falls back to env var if no workspace token.

## Non-goals (v1)

- GitHub App (manifest-based install) — use simpler OAuth App for v1; App follows once OAuth is validated
- GitLab OAuth (follow-up)
- Automatic rotation of expired tokens
- UI for revoking integration (can be done via GitHub settings directly)
- Fine-grained permission UI (v1 requests the scopes the poller needs: `repo` + `read:org`)

## Approach

### OAuth App flow

Operator registers a GitHub OAuth App at `https://github.com/settings/applications/new` with:
- **Homepage URL**: Multica instance URL
- **Authorization callback URL**: `<multica_url>/api/integrations/github/callback`

Server-side env vars:
- `GITHUB_OAUTH_CLIENT_ID`
- `GITHUB_OAUTH_CLIENT_SECRET`

If unset, the Connect button is hidden and the manual URL entry remains the only path.

### Schema

New migration `048_workspace_integrations.up.sql`:

```sql
CREATE TABLE workspace_integration (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  platform        TEXT NOT NULL, -- 'github'
  account_login   TEXT NOT NULL, -- e.g. "multica-ai" or user's login
  access_token    TEXT NOT NULL, -- encrypted at rest (see below)
  scopes          TEXT[],
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT workspace_integration_platform_check CHECK (platform IN ('github','gitlab')),
  UNIQUE (workspace_id, platform)
);
```

### Token encryption

`access_token` is stored AES-GCM encrypted using a key derived from an env var `MULTICA_INTEGRATION_KEY` (32-byte hex, same approach Multica uses elsewhere if any — or introduce new helper). Functions:
- `encryptToken(plain string) (string, error)` — returns base64(nonce || ciphertext)
- `decryptToken(cipher string) (string, error)`

If `MULTICA_INTEGRATION_KEY` is not set, the server refuses to accept OAuth callbacks (500 with clear error), forcing operator to configure the key.

### OAuth handlers

`server/internal/handler/integration_github.go`:

- `GET /api/integrations/github/authorize` — builds and returns GitHub authorize URL with workspace-scoped state parameter. Response: `{ authorize_url }`.
- `GET /api/integrations/github/callback?code=X&state=Y` — exchanges code for token, stores encrypted row, redirects user back to workspace settings.

### Repo picker UI

Replace the "Add repository" dialog's URL text field with:
1. If no integration: existing URL input.
2. If integration exists:
   - Toggle at top: "From GitHub" (default) / "Paste URL"
   - **From GitHub mode**: dropdown showing repos from `/user/repos?affiliation=owner,collaborator,organization_member`. On select: auto-fills name, URL, default_branch from the response.
   - **Paste URL mode**: the existing manual form.

New endpoint: `GET /api/integrations/github/repositories` — uses the workspace's stored token to hit GitHub's list-repositories API. Returns `[{ full_name, clone_url, default_branch, private, description }]`.

### Poller integration

`service.PRPoller` now takes a `TokenProvider` interface:

```go
type TokenProvider interface {
    GetGitHubToken(ctx context.Context, workspaceID uuid.UUID) (string, error)
}
```

Implementation tries `workspace_integration` first, falls back to `GITHUB_TOKEN` env var.

### Settings UI

"Integrations" tab in workspace settings (new tab):
- List connected integrations with account login, connection date, and disconnect button
- "Connect GitHub" button if no integration exists and env is configured
- Post-connect redirect back to this page with success toast

## Tasks

1. Migration 048 + sqlc queries for `workspace_integration`
2. Token encryption helper (`server/internal/crypto/token.go`)
3. OAuth authorize + callback handlers
4. List-repositories endpoint (workspace-scoped, uses stored token)
5. Poller uses per-workspace token via new TokenProvider
6. Frontend: Integrations settings tab + Connect GitHub button
7. Frontend: Repo picker (GitHub dropdown) in add-repository dialog
8. Verification + PR

Estimate: ~3 days.
