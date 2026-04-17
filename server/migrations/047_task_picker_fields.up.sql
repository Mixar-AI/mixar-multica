ALTER TABLE agent_task_queue
  ADD COLUMN repository_id   UUID REFERENCES repository(id) ON DELETE SET NULL,
  ADD COLUMN base_branch     TEXT,
  ADD COLUMN reuse_worktree  BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN sparse_paths    TEXT[];
