"use client";

import { useQuery } from "@tanstack/react-query";
import { issuePullRequestsOptions } from "@multica/core/pull-requests";
import { Card } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";

function formatRelativeTime(isoString: string): string {
  const diffMs = Date.now() - new Date(isoString).getTime();
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return `${diffSec}s ago`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  return `${diffDay}d ago`;
}

function PRStateBadge({ state }: { state: string }) {
  switch (state) {
    case "merged":
      return (
        <Badge className="bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300 border-transparent">
          merged
        </Badge>
      );
    case "closed":
      return <Badge variant="secondary">closed</Badge>;
    case "draft":
      return <Badge variant="outline">draft</Badge>;
    case "open":
    default:
      return (
        <Badge className="bg-success/10 text-success border-transparent">
          open
        </Badge>
      );
  }
}

export function LinkedPRPanel({ wsId, issueId }: { wsId: string; issueId: string }) {
  const { data = [] } = useQuery(issuePullRequestsOptions(wsId, issueId));

  if (data.length === 0) return null;

  return (
    <Card className="p-4">
      <h3 className="text-sm font-medium mb-2">Linked pull requests</h3>
      <ul className="space-y-2">
        {data.map((pr) => (
          <li key={pr.id} className="flex items-start justify-between gap-2 text-sm">
            <a
              href={pr.pr_url}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:underline truncate min-w-0"
            >
              #{pr.pr_number} {pr.title}
            </a>
            <div className="flex items-center gap-2 shrink-0">
              <PRStateBadge state={pr.state} />
              {pr.last_synced_at && (
                <span
                  className="text-xs text-muted-foreground"
                  title={`Last synced: ${pr.last_synced_at}`}
                >
                  {formatRelativeTime(pr.last_synced_at)}
                </span>
              )}
            </div>
          </li>
        ))}
      </ul>
    </Card>
  );
}
