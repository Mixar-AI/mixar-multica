-- name: ListRepositoriesByWorkspace :many
SELECT * FROM repository
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetRepository :one
SELECT * FROM repository
WHERE id = $1;

-- name: GetRepositoryByURL :one
SELECT * FROM repository
WHERE workspace_id = $1 AND url = $2;

-- name: CreateRepository :one
INSERT INTO repository (workspace_id, url, name, default_branch, description, platform)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateRepository :one
UPDATE repository SET
    name           = COALESCE(sqlc.narg('name'), name),
    description    = COALESCE(sqlc.narg('description'), description),
    default_branch = COALESCE(sqlc.narg('default_branch'), default_branch),
    updated_at     = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteRepository :exec
DELETE FROM repository WHERE id = $1;
