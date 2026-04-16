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

-- Write queries (Insert/Update) deferred to sub-project C/D.
