import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const repositoriesKeys = {
  all: (wsId: string) => ["repositories", wsId] as const,
  list: (wsId: string) => [...repositoriesKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...repositoriesKeys.all(wsId), "detail", id] as const,
};

export function repositoryListOptions(wsId: string) {
  return queryOptions({
    queryKey: repositoriesKeys.list(wsId),
    queryFn: () => api.listRepositories(wsId),
    enabled: Boolean(wsId),
  });
}

export function repositoryDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: repositoriesKeys.detail(wsId, id),
    queryFn: () => api.getRepository(wsId, id),
    enabled: Boolean(wsId && id),
  });
}
