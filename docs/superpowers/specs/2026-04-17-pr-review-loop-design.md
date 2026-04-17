# PR Review Feedback Loop — Design + Plan (Sub-project D)

**Status:** Proposed (user-directed execution)
**Date:** 2026-04-17
**Depends on:** Sub-projects A (schema) + C (`pull_request` row population)

## Problem

After sub-project C, `pull_request` rows get created when agents push PRs. But:
- The `state` is frozen at `'open'` — never reflects GitHub merging / closing / reviewing.
- Review comments on the PR go nowhere. If a reviewer says "please add a test", the agent never sees it.

Sub-project D closes the loop: a background poller fetches PR state from GitHub, updates `pull_request` rows, and — when review activity demands agent iteration — auto-creates a follow-up task with the review comment as a trigger.

## Goals

1. Background poller that syncs `pull_request` state from GitHub every N minutes.
2. Detect new review comments since `last_synced_at`.
3. Auto-dispatch follow-up tasks when review requests changes — each follow-up task is triggered by a specific review comment (reuses the existing `TriggerCommentID` flow from runtime_config.go).
4. Simple "review summary" UI on the issue detail PR panel showing latest review state + comment count.

## Non-goals (v1)

- GitLab merge-request polling (GitHub-only; GitLab support follows once there's demand)
- Webhook-based realtime updates (polling is simpler; webhooks require public endpoint + signature verification — sub-project E territory)
- Inline review comment threading in the UI (just the count + latest state)
- Respecting GitHub's per-user rate limits with a sophisticated scheduler (v1 uses naive fixed-interval polling)
- Auto-merge on approval
- Configurable poll interval per workspace (v1 is one global config)

## Approach

### GitHub client

New package `server/internal/github/client.go`:

```go
type Client struct {
    token string
    http  *http.Client
}

// FetchPR fetches the current PR state + review metadata.
func (c *Client) FetchPR(ctx context.Context, owner, repo string, number int) (*PR, error)

// FetchReviewComments fetches comments since a given time.
func (c *Client) FetchReviewComments(ctx context.Context, owner, repo string, number int, since time.Time) ([]ReviewComment, error)
```

Uses `GITHUB_TOKEN` env var. Rate-limit-aware (reads `X-RateLimit-Remaining`, backs off if near limit).

### Poll loop

New `server/internal/service/pr_poller.go`:

```go
type PRPoller struct {
    queries *db.Queries
    github  *github.Client
    logger  *slog.Logger
    // triggers follow-up task creation
    dispatcher TaskDispatcher
}

// Run starts the poll loop; cancels on ctx.Done(). Meant to run as one
// goroutine per server process, started at boot.
func (p *PRPoller) Run(ctx context.Context, interval time.Duration)
```

Each tick:

1. `SELECT * FROM pull_request WHERE state IN ('draft', 'open') ORDER BY last_synced_at ASC LIMIT 50`
2. For each row, parse `pr_url` → owner/repo/number, call `github.FetchPR`.
3. Update `pull_request` row: `state`, `title`, `merged_at`, `closed_at`, `last_synced_at = NOW()`.
4. If `state` just transitioned to a state that needs agent action (e.g., GitHub `review_state = 'changes_requested'`), fetch review comments since `last_synced_at`, pick the most recent one, create a follow-up agent task with that comment as the trigger.

### Follow-up task creation

Reuse the existing dispatch path. The follow-up task references:
- Same issue + agent as the original
- Same repository (registered)
- Base branch = the PR's base branch
- Reuse worktree = `true` (the agent should continue in its existing worktree)
- Trigger comment = the review comment body, tagged so the agent knows it's review feedback

Need a new type of "trigger" or a flag on the task:
```go
type TaskTriggerKind string
const (
    TriggerKindInitial         TaskTriggerKind = "initial"
    TriggerKindComment         TaskTriggerKind = "comment"
    TriggerKindReviewFeedback  TaskTriggerKind = "review_feedback"
)
```

The meta-skill content in `runtime_config.go` renders slightly different workflow text based on trigger kind.

### sqlc queries

```sql
-- name: ListOpenPullRequests :many
SELECT * FROM pull_request
WHERE state IN ('draft', 'open')
ORDER BY last_synced_at ASC
LIMIT $1;

-- name: UpdatePullRequestState :one
UPDATE pull_request SET
    state          = $2,
    title          = COALESCE(sqlc.narg('title'), title),
    merged_at      = COALESCE(sqlc.narg('merged_at'), merged_at),
    closed_at      = COALESCE(sqlc.narg('closed_at'), closed_at),
    last_synced_at = NOW()
WHERE id = $1
RETURNING *;
```

### Config

New env var:
- `MULTICA_PR_POLL_INTERVAL` — default `2m`
- `GITHUB_TOKEN` — required for polling; if unset, poller logs warning once and exits

### UI update

Modify `LinkedPRPanel` to show:
- PR state badge (green=merged, gray=closed, blue=open, yellow=draft)
- Last synced timestamp (relative: "2m ago")
- Review comment count (if any reviews)

## Non-goals clarifications

- Webhook-based sync: out. Polling is simpler and gets us 90% of the value.
- GitLab: out. Add in a follow-up once GitHub side is proven.
- Auto-merge: out. Human approval remains required.

## Tasks

1. `github.Client` + FetchPR + FetchReviewComments + unit tests (mocked HTTP)
2. `ListOpenPullRequests` + `UpdatePullRequestState` sqlc queries
3. `PRPoller` service with tick loop + unit test
4. Wire poller into server boot: start goroutine in `main.go`; cancel on shutdown
5. Review-feedback-triggered follow-up task dispatch (new trigger kind + meta-skill adjustment)
6. UI: badges + last-synced + comment count in `LinkedPRPanel`
7. Verification + PR

Estimate: ~2 days.
