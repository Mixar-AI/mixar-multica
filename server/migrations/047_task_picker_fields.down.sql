ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS sparse_paths,
  DROP COLUMN IF EXISTS reuse_worktree,
  DROP COLUMN IF EXISTS base_branch,
  DROP COLUMN IF EXISTS repository_id;
