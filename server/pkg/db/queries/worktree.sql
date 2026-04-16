-- name: ListWorktreesByWorkspace :many
SELECT w.* FROM worktree w
JOIN repository r ON r.id = w.repository_id
WHERE r.workspace_id = $1
  AND (sqlc.narg('include_inactive')::boolean OR w.status = 'active')
ORDER BY w.created_at DESC;

-- name: ListWorktreesByRepository :many
SELECT * FROM worktree
WHERE repository_id = $1
  AND (sqlc.narg('include_inactive')::boolean OR status = 'active')
ORDER BY created_at DESC;

-- name: GetWorktree :one
SELECT * FROM worktree
WHERE id = $1;

-- name: GetWorktreeByPath :one
SELECT * FROM worktree
WHERE repository_id = $1 AND path = $2;

-- name: CreateWorktree :one
INSERT INTO worktree (repository_id, task_id, path, branch_name, base_branch, head_sha)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateWorktree :one
UPDATE worktree SET
    status       = COALESCE(sqlc.narg('status'), status),
    head_sha     = COALESCE(sqlc.narg('head_sha'), head_sha),
    last_used_at = COALESCE(sqlc.narg('last_used_at'), last_used_at),
    task_id      = COALESCE(sqlc.narg('task_id'), task_id)
WHERE id = $1
RETURNING *;

-- name: SoftDeleteWorktree :exec
UPDATE worktree SET
    status     = 'deleted',
    deleted_at = NOW()
WHERE id = $1;
