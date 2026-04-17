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
