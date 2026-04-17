import { api } from "./index";
import type { Worktree, ListWorktreesParams } from "./client";

export type { Worktree, ListWorktreesParams };

export const worktreesApi = {
  list: (wsId: string, params: ListWorktreesParams = {}): Promise<Worktree[]> =>
    api.listWorktrees(wsId, params),
  get: (wsId: string, id: string): Promise<Worktree> => api.getWorktree(wsId, id),
};
