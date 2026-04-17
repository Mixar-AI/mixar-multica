# PR Publisher Skill + Auto-Track — Design + Plan (Sub-project C)

**Status:** Proposed (user-directed execution; approval waived per "don't ask" directive)
**Date:** 2026-04-17
**Depends on:** Sub-project A (schema + `pull_request` table + handlers)

## Problem

After sub-project A, `pull_request` is a first-class table — but stays empty because nothing populates it. Agents today push branches and run `gh pr create` themselves (unreliably: some skip it, some malform the URL), returning a `pr_url` string in their task result which the server drops into `result` JSON with no further processing.

Sub-project C does two things:

1. **PR Publisher skill** — a built-in skill prompt auto-appended to agent context that standardizes "on completion, commit / push the branch / open a PR / return the URL". Modeled after [routa's PR Publisher specialist](https://github.com/phodal/routa). Makes agent behavior consistent instead of agent-configured.

2. **Server-side PR row insertion** — when a task completes with a `pr_url` in the payload, the server parses it (owner/repo/number), looks up the linked issue + repository + agent, and inserts a `pull_request` row.

3. **Minimal UI surfacing** — the issue detail page renders a "Linked PR: <title> · <state>" panel if a `pull_request` row references the issue. No polling, no live state (that's sub-project D).

## Non-goals

- PR state polling / review loop (sub-project D)
- GitHub API calls to fetch title/body/state server-side (v1 uses the data the agent provides + a stub title)
- Multiple PRs per task (v1 captures the first `pr_url` only; subsequent ones from the same task are ignored)
- Custom PR templates or branding
- OAuth / GitHub App integration (sub-project E)

## Approach

### Skill file

New file: `server/internal/daemon/execenv/pr-publisher-skill.md` — a markdown prompt embedded via `go:embed`. The content instructs the agent to:

1. Before ending the task, `git status` and `git add .` all task-relevant changes.
2. Commit with a descriptive message.
3. Push the branch (`git push -u origin HEAD`).
4. Open a PR: `gh pr create --title "..." --body "..."` using the Multica issue title as the PR title and linking back to the issue via `mention://issue/<id>`.
5. Report the PR URL via the completion payload (the daemon's existing `TaskCompleteRequest.pr_url` field).

### Context injection

`server/internal/daemon/execenv/runtime_config.go` — modify `buildMetaSkillContent` to append the PR Publisher skill content after the existing "## Workflow" section when:
- The task has a linked repository (i.e., `ctx.Repos` is non-empty)
- The task is assignment-triggered (not chat-triggered)

### PR URL parser

New Go function in `server/internal/service/pull_request.go`:

```go
// ParsePRURL parses a GitHub/GitLab PR URL into repo URL + PR number.
// Returns (repoURL, prNumber, platform, error).
// Supports:
//   https://github.com/<owner>/<repo>/pull/<n>  → platform="github"
//   https://gitlab.com/<owner>/<repo>/-/merge_requests/<n> → platform="gitlab"
func ParsePRURL(url string) (repoURL string, number int, platform string, err error)
```

Unit tests cover both platforms, trailing slashes, extra path segments, and malformed URLs.

### Task completion handler

`server/internal/handler/daemon.go` — in the `CompleteTask` handler (the one that receives `TaskCompleteRequest`):

1. If `req.PRURL == ""`, nothing to do.
2. Call `ParsePRURL(req.PRURL)` — on error, log warning + skip (task completion still succeeds).
3. Look up the `repository` row in the workspace by URL (if not found, log warning + skip).
4. Build PR title from the issue title (if task has `issue_id`) or "Agent task" fallback.
5. Insert `pull_request` row with: workspace_id, repository_id, issue_id (nullable), task_id, pr_url, pr_number, head_branch (from worktree row if available), base_branch (from worktree row or "main"), state='open', title, created_by_agent_id.
6. On unique-constraint violation (same `repository_id + pr_number` already exists), treat as idempotent — the PR was already captured (e.g., agent retried). Return the existing row.

### sqlc query

New query in `server/pkg/db/queries/pull_request.sql`:

```sql
-- name: InsertPullRequest :one
INSERT INTO pull_request (
    workspace_id, repository_id, issue_id, task_id,
    pr_url, pr_number, head_branch, base_branch,
    state, title, created_by_agent_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (repository_id, pr_number) DO UPDATE SET
    last_synced_at = NOW()
RETURNING *;
```

Idempotent via `ON CONFLICT DO UPDATE` — same PR number in the same repo returns the existing row.

### Frontend

- New API client module `packages/core/api/pull-requests.ts` (list + get).
- New hooks `packages/core/pull-requests/queries.ts` (`useIssuePullRequestsQuery(wsId, issueId)`).
- New UI component `packages/views/issues/components/linked-pr-panel.tsx` — simple list rendered on the issue detail page.

## Testing

- Parser unit tests (GitHub + GitLab + malformed URLs)
- Handler integration test: POST completion with pr_url → assert row exists in `pull_request`
- Idempotency test: duplicate pr_url on same repo → returns existing row, doesn't double-insert
- Frontend hook test with mocked API
- UI panel test: renders nothing when no PRs; renders list when PRs exist

## Files changed

| Path | Status |
|---|---|
| `server/internal/daemon/execenv/pr-publisher-skill.md` | New (embedded) |
| `server/internal/daemon/execenv/runtime_config.go` | Modify (append skill) |
| `server/internal/service/pull_request.go` + `_test.go` | New (parser) |
| `server/pkg/db/queries/pull_request.sql` | Modify (add Insert) |
| `server/pkg/db/generated/*` | Regenerated |
| `server/internal/handler/daemon.go` + `_test.go` | Modify (insertion path) |
| `packages/core/api/client.ts` | Modify (add `listIssuePullRequests`) |
| `packages/core/api/pull-requests.ts` | New |
| `packages/core/pull-requests/{index,queries}.ts` | New |
| `packages/views/issues/components/linked-pr-panel.tsx` | New |
| `packages/views/issues/components/issue-detail.tsx` | Modify (render panel) |

## Tasks

1. `ParsePRURL` function + tests
2. `InsertPullRequest` sqlc query + regen
3. Embed + inject `pr-publisher-skill.md` into agent context
4. Handler: parse pr_url on task complete, insert row
5. Frontend API + hooks
6. `LinkedPRPanel` component + integration into issue detail
7. Manual verification

Estimate: ~1-2 days of implementation.
