# Repository / Worktree / PullRequest Data Model — Design

**Status:** Proposed
**Date:** 2026-04-16
**Author:** Claude (with rahul@mixar.app)
**Sub-project:** A of 5 (the foundation)
**Follow-ups:** B (per-task picker), C (PR Publisher), D (review loop), E (GitHub OAuth)

## Problem

Multica today represents repositories as a JSON array of URL strings on `workspaces.repos`, worktrees as transient daemon-local state with no server visibility, and PRs as bare URL strings buried inside `agent_task_queue.result` JSON. This makes it impossible to:

- Query "what worktrees are currently active in this workspace?"
- Surface PR state in the UI (we only have a URL string)
- Build a base-branch picker, folder-scope picker, or PR review loop on top of stable identifiers
- Audit which agent created which PR for which issue

Sub-project A promotes **repositories**, **worktrees**, and **pull requests** to first-class entities: tables with FKs, queryable state, and proper schema. Sub-projects B–E build user-facing features on this foundation.

## Goals

1. **First-class data model** — three new tables: `repositories`, `git_worktrees`, `pull_requests`. All with FKs to `workspaces`, with proper indexes, with queryable state.
2. **Visible without re-architecture** — operators can manage repositories via a rewritten settings UI; workspace API exposes worktree state.
3. **Daemon-side worktree tracking** — every worktree the daemon creates has a persisted row; GC marks rows deleted (audit trail).
4. **Foundation for B–E** — schema includes columns sub-projects B–E will need (e.g., `sparse_paths`, `pr_state`), even if A doesn't yet write them.

## Non-goals (v1 of A)

- Migrating any existing data (per pre-launch assumption — see "Migration" below)
- Populating `pull_requests` (sub-project C)
- Folder/sparse-checkout enforcement (sub-project B)
- PR review polling, `last_synced_at` updates (sub-project D)
- GitHub OAuth, automated repo discovery (sub-project E)
- WorktreeCreate / WorktreeRemove HTTP hooks (future sub-project)
- ACP adoption (future sub-project)
- Bulk repo import / CSV upload
- Worktree-list UI in workspace settings (defer until asked)

## Prior art

- **[phodal/routa](https://github.com/phodal/routa)** — has `codebases` (their name for repositories) and `worktrees` as first-class entities, persisted via `CodebaseStore` and `WorktreeStore`. Routa does NOT have a `pull_requests` table — they store PR metadata on tasks. We diverge here because sub-project D's polling design needs a stable PR identity.
- **[anthropics/claude-code](https://github.com/anthropics/claude-code)** — uses git worktrees for session isolation via `--worktree` flag and `isolation: "worktree"` agent definitions. Has `WorktreeCreate` / `WorktreeRemove` HTTP hooks. Has `worktree.sparsePaths` setting for folder scoping in monorepos. We add `sparse_paths` column to `git_worktrees` (dormant in A, populated in B). Hooks are deferred.

## Future direction

- **Hooks** — model after Claude Code's `WorktreeCreate` / `WorktreeRemove` HTTP hooks. Operators register endpoint URLs; daemon fires HTTP POST on lifecycle events. Enables custom VCS setup (LFS, dependency caching, license scans). Own sub-project.
- **ACP adoption** — captured during OpenClaw streaming work; routa's universal-ACP architecture is the right long-term direction. Independent multi-week refactor.

## Approach (chosen: schema + minimal wiring)

Per Q2 (b): schema + minimal daemon/API/UI wiring. Tables get populated immediately for `repositories` (UI writes) and `git_worktrees` (daemon writes); `pull_requests` stays empty until sub-project C wires the population path.

## Architecture

```
workspace (existing)
   │
   │ 1:N
   ▼
repositories ─────────────┐
   │                      │
   │ 1:N                  │ 1:N
   ▼                      ▼
git_worktrees       pull_requests
   │                      │
   │ N:0..1               │ N:0..1
   ▼                      ▼
agent_task_queue.id ◄─────┘ (existing)
```

Three new tables, all workspace-scoped via FK. Worktrees and PRs both reference repositories and (optionally) tasks.

## Schemas

### `repositories` — replaces `workspaces.repos` JSON column

```sql
CREATE TABLE repositories (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  url             TEXT NOT NULL,                  -- normalized https URL or git@host:owner/repo.git
  name            TEXT NOT NULL,                  -- human-friendly label, e.g., "frontend"
  default_branch  TEXT NOT NULL DEFAULT 'main',   -- resolved at registration time, can be edited
  description     TEXT NOT NULL DEFAULT '',
  platform        TEXT NOT NULL DEFAULT 'github', -- 'github' | 'gitlab' | 'other' (forward-compat for E)
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (workspace_id, url)
);
CREATE INDEX repositories_workspace ON repositories (workspace_id);
```

### `git_worktrees` — server-side worktree tracking, written by daemon

```sql
CREATE TABLE git_worktrees (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  task_id       UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,  -- NULL when orphaned
  path          TEXT NOT NULL,                                              -- absolute path in workspaces_root
  branch_name   TEXT NOT NULL,                                              -- e.g., agent/claude/a1b2c3d4
  base_branch   TEXT NOT NULL,                                              -- branch we forked off
  status        TEXT NOT NULL DEFAULT 'active',                             -- 'active' | 'inactive' | 'deleted'
  head_sha      TEXT NOT NULL DEFAULT '',                                   -- last known HEAD (populated on update)
  sparse_paths  TEXT[],                                                     -- NULL or empty: full worktree; populated by sub-project B for git sparse-checkout
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at    TIMESTAMPTZ,                                                -- set when GC'd
  UNIQUE (repository_id, path)
);
CREATE INDEX git_worktrees_task ON git_worktrees (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX git_worktrees_active ON git_worktrees (repository_id, status) WHERE status = 'active';
```

### `pull_requests` — first-class PR entity (dormant in A; populated by sub-project C)

```sql
CREATE TABLE pull_requests (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id        UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  repository_id       UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  issue_id            UUID REFERENCES issues(id) ON DELETE SET NULL,        -- NULL: PR not tied to a Multica issue
  task_id             UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
  pr_url              TEXT NOT NULL,                                          -- e.g. https://github.com/org/repo/pull/42
  pr_number           INTEGER NOT NULL,
  head_branch         TEXT NOT NULL,
  base_branch         TEXT NOT NULL,
  state               TEXT NOT NULL DEFAULT 'open',                           -- 'draft' | 'open' | 'merged' | 'closed'
  title               TEXT NOT NULL DEFAULT '',
  created_by_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_synced_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  merged_at           TIMESTAMPTZ,
  closed_at           TIMESTAMPTZ,
  UNIQUE (repository_id, pr_number)
);
CREATE INDEX pull_requests_workspace ON pull_requests (workspace_id);
CREATE INDEX pull_requests_issue ON pull_requests (issue_id) WHERE issue_id IS NOT NULL;
CREATE INDEX pull_requests_state ON pull_requests (state);  -- for sub-project D's poller
```

### Drop existing column

```sql
ALTER TABLE workspaces DROP COLUMN repos;  -- the JSON column being replaced
```

Per pre-launch assumption: no migration code, no shims. Fresh schema.

### Design choice notes

- **UUIDs everywhere** — matches existing Multica tables (verified in existing migrations).
- **Soft delete via `status` for worktrees** — daemon GC sets `status='deleted'` + `deleted_at=NOW()`, preserving audit trail; the partial index `WHERE status='active'` keeps active queries cheap.
- **`platform` field on repositories from day 1** — sub-project E (OAuth) needs it; cheap to add now, expensive to backfill later.
- **`sparse_paths TEXT[]` on git_worktrees from day 1** — sub-project B will write to it for folder scoping (Claude Code-style sparse-checkout). Including the column now avoids a follow-up migration.
- **`pr_state` includes `'draft'`** — needed for GitHub-style draft PRs the agent might open before pushing all commits.
- **`UNIQUE (workspace_id, url)`** — same URL across workspaces is allowed (different teams' settings); within a workspace, dedupe by URL.

## API surface

### New endpoints — repositories CRUD (consumer)

```
GET    /workspaces/:wsId/repositories           → []Repository
POST   /workspaces/:wsId/repositories           → Repository  (body: {url, name, default_branch?, description?})
GET    /workspaces/:wsId/repositories/:id       → Repository
PATCH  /workspaces/:wsId/repositories/:id       → Repository  (body: {name?, description?, default_branch?})
DELETE /workspaces/:wsId/repositories/:id       → 204
```

Validation rules:
- `url` is required; format check (https URL or `git@host:owner/repo.git`); rejected if duplicate within workspace.
- `name` is required; trim + length 1-100.
- `default_branch` defaults to `"main"` if omitted; can be edited later.
- `url` is **immutable** after creation (renaming a remote breaks worktree paths) — only PATCH allows name/description/default_branch.

### New endpoints — worktrees (read-only consumer)

```
GET /workspaces/:wsId/worktrees                          → []Worktree  (default: active only; ?include_inactive=true)
GET /workspaces/:wsId/worktrees/:id                      → Worktree
GET /workspaces/:wsId/repositories/:repoId/worktrees     → []Worktree
```

No write endpoints — daemon owns worktree mutation via the daemon-only endpoints below.

### New endpoints — daemon-only (worktree write path)

```
POST   /daemon/worktrees                  → body: {task_id, repository_url, path, branch_name, base_branch, head_sha?}
                                            → server resolves repository_url → repository_id (404 if not registered)
                                            → inserts row, returns {id, ...}
PATCH  /daemon/worktrees/:id              → body: {status?, head_sha?, last_used_at?, task_id?}
DELETE /daemon/worktrees/:id              → soft-delete (status='deleted', deleted_at=NOW())
```

Auth: existing daemon-token middleware (same as other `/daemon/*` endpoints).

### Pull requests endpoints — none in A

Sub-project C adds the write path; sub-project D adds polling + read endpoints.

## UI changes

### Rewrite `packages/views/settings/components/repositories-tab.tsx`

Replace the current JSON-edit form with:

- **List view** — table of repositories: Name, URL, Default branch, Updated, [Edit] [Delete]
- **"Add repository" button** → dialog with form (URL required, name required, default_branch + description optional)
- **Edit dialog** — name / description / default_branch only; URL field shown disabled
- **Delete confirmation** — warn that all worktrees + PRs tied to this repo will be deleted (cascade)
- **Empty state** — "No repositories yet" with prominent "Add your first repository" CTA

### New TanStack Query hooks — `packages/core/repositories/`

- `useRepositoriesQuery(wsId)` — list
- `useRepositoryQuery(wsId, id)` — single
- `useCreateRepositoryMutation(wsId)` — POST (optimistic)
- `useUpdateRepositoryMutation(wsId)` — PATCH (optimistic)
- `useDeleteRepositoryMutation(wsId)` — DELETE (optimistic)
- All mutations invalidate `repositoriesKeys.list(wsId)` on settle

### New hook — `packages/core/worktrees/`

- `useWorktreesQuery(wsId, {repositoryId?, includeInactive?})` — read-only
- No mutations in A; daemon writes via daemon endpoints, not the consumer API

### Agent context source change

`TaskContextForEnv.Repos` (consumed by `execenv/runtime_config.go:90-104` to render the "## Repositories" section in `AGENTS.md` / `CLAUDE.md`) currently reads from `workspaces.repos` JSON. Change source to a SELECT against the new `repositories` table — same shape returned, no consumer changes needed.

## Daemon wiring

### `repocache` integration

`server/internal/daemon/repocache/cache.go` is where worktree creation lives today. Wire it to the new endpoints:

- **On `CreateWorktree()` success** → call `POST /daemon/worktrees`. Stash the returned `id` in repocache's in-memory index (keyed by path).
- **On reuse of an existing worktree** for a new task → call `PATCH /daemon/worktrees/:id` with `{last_used_at: now, task_id: <new-task-id>}`.
- **On daemon GC reaping** (existing GC at `gc.go`) → call `DELETE /daemon/worktrees/:id` for each path being reaped.
- **Error path:** if the server returns 404 (repo URL not registered), surface the error to the agent: `"repository <URL> not registered in workspace; register via 'multica repo create' or workspace settings before checkout"`.

### CLI behavior change

`multica repo checkout <url>` currently accepts any URL. After A:

- CLI calls `GET /workspaces/:wsId/repositories` first.
- If the URL isn't in the list, fail fast with the registration hint.
- This is the enforcement point for "first-class data model" — repos must be registered before use.

### New CLI subcommands

Extend `multica repo` (in `server/cmd/multica/cmd_repo.go`) with:

```
multica repo list                                              # GET /workspaces/:wsId/repositories
multica repo create --url X --name Y [--default-branch B] [--description D]
multica repo update <id> [--name X] [--description Y] [--default-branch B]
multica repo delete <id>
multica repo worktrees [--repo <id>] [--include-inactive]      # GET /workspaces/:wsId/worktrees
```

Existing `multica repo checkout <url>` stays but now validates registration first.

## Migration

**None.** Per pre-launch assumption (Q1 (a)).

The migration file is just `up` (create new tables, drop `workspaces.repos` JSON column) and `down` (the inverse). No data backfill. No dual-write period. Per CLAUDE.md: "If a flow or API is being replaced and the product is not yet live, prefer removing the old path."

If a deployed instance has populated `workspaces.repos` data we want to preserve, the migration can be hand-edited to add an INSERT-from-JSON step. Baseline assumption is: clean.

## Testing strategy

| Layer | Where | What |
|---|---|---|
| SQL migration | `server/migrations/NNN_repositories_worktrees_pull_requests.{up,down}.sql` | Manual review; CI runs migrations as part of test setup |
| sqlc-generated queries | `server/pkg/db/queries/{repository,worktree,pull_request}.sql` (new) | Regenerate via `make sqlc`; any compile error during regen is the test |
| HTTP handlers | `server/internal/handler/repository_test.go`, `worktree_test.go`, `daemon_worktree_test.go` (new) | CRUD happy paths + auth/validation/error paths; uses existing `httptest` + test DB pattern |
| Daemon → server worktree sync | `server/internal/daemon/repocache/cache_test.go` (extend existing) | Mock the daemon HTTP client; assert POST/PATCH/DELETE called at right lifecycle points |
| Frontend hooks | `packages/core/repositories/repositories.test.ts` (new) | Vitest with mocked `api.*`; assert query keys, optimistic updates, invalidation on settle |
| Frontend UI | `packages/views/settings/components/repositories-tab.test.tsx` (new) | jsdom + testing-library; render list, add/edit/delete flow, error states |
| End-to-end | `e2e/tests/repositories.spec.ts` (new) | Playwright: create workspace → add repo → verify in list → edit → delete → confirm removal |

## Edge cases & error handling

| Case | Handling |
|---|---|
| User adds repo with duplicate URL within workspace | 409 Conflict with `{"error": "repository with this url already exists"}` |
| User adds repo with invalid URL format | 400 Bad Request with format hint |
| User deletes repo with active worktrees | FK cascade deletes worktrees + PRs. **No protection against deleting a repo while a task is mid-execution** — known limitation; trust operators in v1 (the daemon will fail later when its in-memory worktree path no longer exists). Sub-project D may add a "is any task running here?" check. |
| Daemon POSTs worktree for unregistered URL | 404 Not Found; daemon surfaces error to agent |
| Daemon PATCHes a non-existent worktree | 404; daemon logs warning, doesn't retry |
| Daemon GC tries to DELETE an already-deleted worktree | 200 (idempotent — server returns success even if already deleted) |
| Concurrent daemon writes (race on `UNIQUE (repository_id, path)`) | DB rejects second insert; daemon catches, looks up existing row, proceeds |
| `gen_random_uuid()` requires `pgcrypto` extension | Migration includes `CREATE EXTENSION IF NOT EXISTS pgcrypto;` if not already present |

## Risks + mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Daemon's HTTP calls add latency to task setup | Medium | One round-trip per worktree create; ~10-50ms; not noticeable next to multi-second `git clone` |
| `multica repo checkout` requirement breaks agent workflows | Low (pre-launch) | Clear error message tells agent how to register; sub-project C's PR Publisher skill will pre-register if needed |
| `git_worktrees` table grows monotonically | Low (slow growth: ~1 row per task) | Add separate purge job in future sub-project; not blocking for v1 |
| Frontend rewrite breaks settings tab in subtle ways | Medium | E2E tests cover create-edit-delete flow; manual smoke before merge |

## Files changed (estimated)

| Path | Change | Lines |
|---|---|---|
| `server/migrations/NNN_repositories_worktrees_pull_requests.up.sql` | New | ~80 |
| `server/migrations/NNN_repositories_worktrees_pull_requests.down.sql` | New | ~20 |
| `server/pkg/db/queries/{repository,worktree,pull_request}.sql` | New | ~150 |
| `server/pkg/db/generated/*` | Regenerated by `make sqlc` | ~600 (auto-gen, committed) |
| `server/internal/handler/repository.go` + `_test.go` | New | ~250 + ~250 |
| `server/internal/handler/worktree.go` + `_test.go` | New | ~150 + ~150 |
| `server/internal/handler/daemon_worktree.go` + `_test.go` | New | ~150 + ~150 |
| `server/internal/daemon/repocache/cache.go` | Extend | ~80 added |
| `server/internal/daemon/repocache/client.go` | New (HTTP client for new endpoints) | ~120 |
| `server/internal/daemon/execenv/runtime_config.go` | Modify TaskContextForEnv source | ~20 |
| `server/cmd/multica/cmd_repo.go` | Extend with `create`/`update`/`delete`/`worktrees` subcommands | ~150 added |
| `packages/core/repositories/{queries,mutations,hooks}.ts` | New | ~200 |
| `packages/core/worktrees/{queries,hooks}.ts` | New | ~80 |
| `packages/views/settings/components/repositories-tab.tsx` | Rewrite | ~250 (was ~150) |
| `packages/views/settings/components/repository-dialog.tsx` | New | ~150 |
| `e2e/tests/repositories.spec.ts` | New | ~80 |
| Tests for the above | Various | ~600 |

**Rough total:** ~3500 LOC of hand-written code + ~600 LOC auto-generated. Estimate: **5-7 days of implementation**.

## Success criteria

1. `make check` passes after the sub-project lands.
2. New workspace settings UI lets the user add/edit/delete repositories; data persists in `repositories` table.
3. Agent task that does `multica repo checkout <url>` against an unregistered URL fails with a clear error.
4. After a task completes (existing flow unchanged), querying `GET /workspaces/:wsId/worktrees` shows the worktrees the daemon created, with correct branch/path/status.
5. After daemon GC reaps a worktree, its `git_worktrees` row has `status='deleted'`.
6. `pull_requests` table exists with all columns, schema reviewed — but stays empty (sub-project C populates it).

## Open questions deferred to implementation

1. **Migration number** — next available is `046` (current highest is `045_audit_dashboard_route_slugs`). Plan will use `046_repositories_worktrees_pull_requests`.
2. **`platform` validation** — should the API validate `platform` is one of `('github', 'gitlab', 'other')` or accept any string? Default to enum-like CHECK constraint for now; relax if needed.
3. **`agents` table FK on `pull_requests.created_by_agent_id`** — verify the `agents` table name and PK type during plan-writing.
4. **Existing `workspace.repos` consumers** — grep for `.Repos` and `workspaces.repos` references; may include UI components I haven't catalogued.
