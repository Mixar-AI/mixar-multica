-- name: GetWorkspaceIntegration :one
SELECT * FROM workspace_integration
WHERE workspace_id = $1 AND platform = $2;

-- name: ListWorkspaceIntegrations :many
SELECT * FROM workspace_integration
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: UpsertWorkspaceIntegration :one
INSERT INTO workspace_integration (workspace_id, platform, account_login, access_token, scopes)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, platform) DO UPDATE SET
  account_login = EXCLUDED.account_login,
  access_token  = EXCLUDED.access_token,
  scopes        = EXCLUDED.scopes,
  updated_at    = NOW()
RETURNING *;

-- name: DeleteWorkspaceIntegration :execrows
DELETE FROM workspace_integration
WHERE workspace_id = $1 AND platform = $2;
