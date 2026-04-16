-- 046: First-class repository / worktree / pull_request data model.
--
-- Promotes the existing JSON column workspace.repos to a real table with
-- FKs, indexes, and proper schema. Adds worktree and pull_request tables
-- as siblings. Sub-project A of the repo+folder+PR+worktree workstream;
-- sub-projects B-E build user-facing features on top.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE repository (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  url             TEXT NOT NULL,
  name            TEXT NOT NULL,
  default_branch  TEXT NOT NULL DEFAULT 'main',
  description     TEXT NOT NULL DEFAULT '',
  platform        TEXT NOT NULL DEFAULT 'github',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT repository_url_workspace_unique UNIQUE (workspace_id, url),
  CONSTRAINT repository_platform_check CHECK (platform IN ('github', 'gitlab', 'other'))
);

CREATE INDEX repository_workspace ON repository (workspace_id);

CREATE TABLE worktree (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  repository_id UUID NOT NULL REFERENCES repository(id) ON DELETE CASCADE,
  task_id       UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
  path          TEXT NOT NULL,
  branch_name   TEXT NOT NULL,
  base_branch   TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'active',
  head_sha      TEXT NOT NULL DEFAULT '',
  sparse_paths  TEXT[],
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at    TIMESTAMPTZ,
  CONSTRAINT worktree_repo_path_unique UNIQUE (repository_id, path),
  CONSTRAINT worktree_status_check CHECK (status IN ('active', 'inactive', 'deleted'))
);

CREATE INDEX worktree_task ON worktree (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX worktree_active ON worktree (repository_id, status) WHERE status = 'active';

CREATE TABLE pull_request (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  repository_id       UUID NOT NULL REFERENCES repository(id) ON DELETE CASCADE,
  issue_id            UUID REFERENCES issue(id) ON DELETE SET NULL,
  task_id             UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
  pr_url              TEXT NOT NULL,
  pr_number           INTEGER NOT NULL,
  head_branch         TEXT NOT NULL,
  base_branch         TEXT NOT NULL,
  state               TEXT NOT NULL DEFAULT 'open',
  title               TEXT NOT NULL DEFAULT '',
  created_by_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_synced_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  merged_at           TIMESTAMPTZ,
  closed_at           TIMESTAMPTZ,
  CONSTRAINT pull_request_repo_number_unique UNIQUE (repository_id, pr_number),
  CONSTRAINT pull_request_state_check CHECK (state IN ('draft', 'open', 'merged', 'closed'))
);

CREATE INDEX pull_request_workspace ON pull_request (workspace_id);
CREATE INDEX pull_request_issue ON pull_request (issue_id) WHERE issue_id IS NOT NULL;
CREATE INDEX pull_request_state ON pull_request (state);

-- Drop the old JSON column. Pre-launch, no data migration.
ALTER TABLE workspace DROP COLUMN repos;
