CREATE TABLE workspace_integration (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  platform        TEXT NOT NULL,
  account_login   TEXT NOT NULL,
  access_token    TEXT NOT NULL,
  scopes          TEXT[],
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT workspace_integration_platform_check CHECK (platform IN ('github','gitlab')),
  UNIQUE (workspace_id, platform)
);
