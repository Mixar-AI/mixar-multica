import { api } from "./index";
import type { PullRequest } from "../types";

export type { PullRequest };

export const pullRequestsApi = {
  listByIssue: (wsId: string, issueId: string): Promise<PullRequest[]> =>
    api.listIssuePullRequests(wsId, issueId),
};

