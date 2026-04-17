"use client";

import { useQuery } from "@tanstack/react-query";
import type { PullRequest } from "@multica/core/types";
import { api } from "@multica/core/api";
import { Card } from "@multica/ui/components/ui/card";

export function LinkedPRPanel({ wsId, issueId }: { wsId: string; issueId: string }) {
  const { data = [] } = useQuery<PullRequest[]>({
    queryKey: ["pull-requests", wsId, "issue", issueId],
    queryFn: () => api.listIssuePullRequests(wsId, issueId),
    enabled: Boolean(wsId && issueId),
  });
  if (data.length === 0) return null;
  return (
    <Card className="p-4">
      <h3 className="text-sm font-medium mb-2">Linked pull requests</h3>
      <ul className="space-y-1">
        {data.map((pr) => (
          <li key={pr.id} className="flex items-center justify-between text-sm">
            <a href={pr.pr_url} target="_blank" rel="noopener noreferrer" className="hover:underline truncate mr-2">
              #{pr.pr_number} {pr.title}
            </a>
            <span className="text-xs text-muted-foreground shrink-0">{pr.state}</span>
          </li>
        ))}
      </ul>
    </Card>
  );
}
