"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { integrationListOptions } from "@multica/core/integrations";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";

// IntegrationsTab shows connected OAuth integrations and provides a
// "Connect GitHub" button that initiates the OAuth flow.
export function IntegrationsTab() {
  const wsId = useWorkspaceId();
  const { data: integrations = [], isLoading, refetch } = useQuery(integrationListOptions(wsId));

  // After GitHub OAuth callback redirects back with ?connected=github,
  // show a success toast and refetch the integrations list.
  React.useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    if (params.get("connected") === "github") {
      toast.success("GitHub connected successfully!");
      // Remove the query param without navigating away.
      const url = new URL(window.location.href);
      url.searchParams.delete("connected");
      window.history.replaceState({}, "", url.toString());
      refetch();
    }
  }, [refetch]);

  async function handleConnectGitHub() {
    try {
      const { authorize_url } = await api.getGitHubAuthorizeURL();
      window.location.href = authorize_url;
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to get GitHub authorization URL");
    }
  }

  const githubIntegration = integrations.find((i) => i.platform === "github");

  if (isLoading) {
    return <div className="text-sm text-muted-foreground">Loading integrations...</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-sm font-semibold mb-1">Integrations</h2>
        <p className="text-xs text-muted-foreground">
          Connect external services to enable additional workflows.
        </p>
      </div>

      {/* GitHub integration card */}
      <div className="rounded-md border p-4 flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="h-8 w-8 rounded-md bg-muted flex items-center justify-center text-xs font-bold shrink-0">
            GH
          </div>
          <div>
            <p className="text-sm font-medium">GitHub</p>
            {githubIntegration ? (
              <p className="text-xs text-muted-foreground">
                Connected as{" "}
                <span className="font-medium">{githubIntegration.account_login}</span>
                {" · "}
                {new Date(githubIntegration.created_at).toLocaleDateString()}
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                Connect to pick repositories from your account.
              </p>
            )}
          </div>
        </div>

        {githubIntegration ? (
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground rounded-full border px-2 py-0.5">
              Connected
            </span>
            <Button
              size="sm"
              variant="ghost"
              onClick={handleConnectGitHub}
              className="text-xs"
            >
              Reconnect
            </Button>
          </div>
        ) : (
          <Button size="sm" onClick={handleConnectGitHub}>
            Connect GitHub
          </Button>
        )}
      </div>
    </div>
  );
}
