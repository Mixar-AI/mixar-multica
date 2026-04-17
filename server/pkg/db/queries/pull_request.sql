-- name: ListPullRequestsByWorkspace :many
SELECT * FROM pull_request
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: ListPullRequestsByIssue :many
SELECT * FROM pull_request
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: GetPullRequest :one
SELECT * FROM pull_request
WHERE id = $1;

-- name: InsertPullRequest :one
INSERT INTO pull_request (
    workspace_id, repository_id, issue_id, task_id,
    pr_url, pr_number, head_branch, base_branch,
    state, title, created_by_agent_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (repository_id, pr_number) DO UPDATE SET
    last_synced_at = NOW()
RETURNING *;

-- name: ListOpenPullRequests :many
SELECT * FROM pull_request
WHERE state IN ('draft', 'open')
ORDER BY last_synced_at ASC
LIMIT $1;

-- name: UpdatePullRequestState :one
UPDATE pull_request SET
    state          = COALESCE(sqlc.narg('state'), state),
    title          = COALESCE(sqlc.narg('title'), title),
    merged_at      = COALESCE(sqlc.narg('merged_at'), merged_at),
    closed_at      = COALESCE(sqlc.narg('closed_at'), closed_at),
    last_synced_at = NOW()
WHERE id = $1
RETURNING *;
