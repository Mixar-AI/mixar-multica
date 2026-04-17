import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { PullRequest } from "../types";

export type { PullRequest };

export const pullRequestKeys = {
  byIssue: (wsId: string, issueId: string) =>
    ["pull-requests", wsId, "issue", issueId] as const,
};

export function issuePullRequestsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: pullRequestKeys.byIssue(wsId, issueId),
    queryFn: () => api.listIssuePullRequests(wsId, issueId),
    enabled: Boolean(wsId && issueId),
  });
}

export function useIssuePullRequestsQuery(wsId: string, issueId: string) {
  // Return query options object for use with useQuery
  return issuePullRequestsOptions(wsId, issueId);
}
