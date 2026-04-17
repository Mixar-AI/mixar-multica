import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListWorktreesParams } from "../api/client";

export const worktreesKeys = {
  all: (wsId: string) => ["worktrees", wsId] as const,
  list: (wsId: string, params: ListWorktreesParams = {}) =>
    [...worktreesKeys.all(wsId), "list", params] as const,
  detail: (wsId: string, id: string) =>
    [...worktreesKeys.all(wsId), "detail", id] as const,
};

export function worktreeListOptions(wsId: string, params: ListWorktreesParams = {}) {
  return queryOptions({
    queryKey: worktreesKeys.list(wsId, params),
    queryFn: () => api.listWorktrees(wsId, params),
    enabled: Boolean(wsId),
  });
}

export function worktreeDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: worktreesKeys.detail(wsId, id),
    queryFn: () => api.getWorktree(wsId, id),
    enabled: Boolean(wsId && id),
  });
}
