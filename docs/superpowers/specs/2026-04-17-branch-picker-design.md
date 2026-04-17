# Per-Task Repository + Branch + Worktree Picker — Design + Plan (Sub-project B)

**Status:** Proposed (user-directed execution)
**Date:** 2026-04-17
**Depends on:** Sub-projects A (schema) + C (PR Publisher skill — pipeline should be working end-to-end before B adds per-task controls)

## Problem

Today when assigning a task to an agent, there's no user control over:
- Which registered repository the agent works in
- What base branch the work forks off
- Whether an existing worktree is reused or a fresh one is created
- What folder scope (sub-project A's `sparse_paths` column — dormant)

The daemon auto-picks: base branch = remote default, worktree = fresh, scope = whole repo.

## Goals

1. Add four new optional columns to `agent_task_queue`: `repository_id`, `base_branch`, `reuse_worktree`, `sparse_paths`.
2. UI: agent dispatch dialog (or expanded form) includes dropdowns/inputs for these fields.
3. Daemon: respect these fields when creating worktrees. Base branch override, sparse-checkout application, worktree reuse decision.
4. Backward-compatible: if fields are NULL/empty, behavior is today's auto-pick.

## Non-goals

- Changing the task-dispatch UX radically (keep simple — add the picker, don't rethink the whole flow)
- Implementing sparse-checkout enforcement at git layer in v1 (just store the patterns; daemon applies `git sparse-checkout set <patterns>` after creating the worktree)
- Multi-repo tasks (one repo per task; if the user needs multi-repo, create multiple tasks)
- Branch creation UI (user picks from a text input; no live GitHub API branch list — sub-project E adds that)

## Approach

### Schema migration

New migration `047_task_picker_fields.up.sql`:

```sql
ALTER TABLE agent_task_queue
  ADD COLUMN repository_id   UUID REFERENCES repository(id) ON DELETE SET NULL,
  ADD COLUMN base_branch     TEXT,
  ADD COLUMN reuse_worktree  BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN sparse_paths    TEXT[];
```

All nullable / default-false so existing tasks are unaffected.

### sqlc queries

Update the existing `CreateAgentTask` (or similar) to accept the new fields. Add `nil` defaults on the application side for backward compatibility.

### Handler + API

Extend the task-create request body with optional `repository_id`, `base_branch`, `reuse_worktree`, `sparse_paths` fields. Handler passes them through to the DB insert.

### Daemon

Where the daemon decides worktree parameters (in `repocache.CreateWorktree`'s callers):
- If `task.RepositoryID` is set, use that repo's URL (instead of auto-detecting or erroring on "no repo linked")
- If `task.BaseBranch` is set, pass to `git worktree add -b <branch_name> <path> <base_branch>`
- If `task.ReuseWorktree == true` and a prior worktree exists for (repository_id, base_branch), reuse it
- If `task.SparsePaths` is non-empty, after creating the worktree, run `git -C <path> sparse-checkout init --cone && git -C <path> sparse-checkout set <paths...>`

Update the `worktree` row's `sparse_paths` column when setting.

### Frontend

New UI in the agent-assignment dialog (likely `packages/views/issues/components/assign-agent-dialog.tsx` or similar):
- **Repository**: dropdown populated from `useRepositoriesQuery(wsId)`. Required if any repos are registered; no-op if workspace has zero repos.
- **Base branch**: text input, placeholder shows the selected repo's `default_branch`. Optional.
- **Reuse worktree**: checkbox. Default unchecked.
- **Folder scope**: comma-separated text input for sparse-checkout patterns. Optional.

## Non-goals clarifications

- Live branch list from GitHub: defer to sub-project E (requires OAuth).
- Folder-picker tree UI: just a text field in v1. Operators can type patterns (e.g., `packages/web/,docs/`).

## Tasks

1. Migration 047 + sqlc query updates
2. Handler: extend task-create body + pass to DB
3. Daemon: respect task's repository_id, base_branch, reuse_worktree, sparse_paths
4. API client + hook updates (add new fields to `CreateTaskInput`)
5. UI: add picker fields to assign-agent dialog
6. Tests + manual verification

Estimate: ~1-2 days.
