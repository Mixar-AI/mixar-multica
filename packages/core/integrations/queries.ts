import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const integrationsKeys = {
  all: (wsId: string) => ["integrations", wsId] as const,
  list: (wsId: string) => [...integrationsKeys.all(wsId), "list"] as const,
  githubRepos: (wsId: string) => [...integrationsKeys.all(wsId), "github-repos"] as const,
};

export function integrationListOptions(wsId: string) {
  return queryOptions({
    queryKey: integrationsKeys.list(wsId),
    queryFn: () => api.listIntegrations(),
    enabled: Boolean(wsId),
  });
}

export function githubReposOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: integrationsKeys.githubRepos(wsId),
    queryFn: () => api.listGitHubRepositories(),
    enabled: Boolean(wsId) && enabled,
  });
}
