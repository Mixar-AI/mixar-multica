"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { repositoryListOptions, useDeleteRepository } from "@multica/core/repositories";
import type { Repository } from "@multica/core/api/client";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { RepositoryDialog } from "./repository-dialog";
import { toast } from "sonner";

export function RepositoriesTab() {
  const wsId = useWorkspaceId();
  const { data: repos = [], isLoading } = useQuery(repositoryListOptions(wsId));
  const deleteMut = useDeleteRepository(wsId);
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<Repository | null>(null);

  function openAdd() {
    setEditing(null);
    setDialogOpen(true);
  }

  function openEdit(repo: Repository) {
    setEditing(repo);
    setDialogOpen(true);
  }

  async function handleDelete(repo: Repository) {
    if (
      !window.confirm(
        `Delete repository "${repo.name}"? This cascades to all linked worktrees and pull requests.`,
      )
    ) {
      return;
    }
    try {
      await deleteMut.mutateAsync(repo.id);
      toast.success(`Deleted "${repo.name}"`);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to delete");
    }
  }

  if (isLoading) {
    return <div className="text-sm text-muted-foreground">Loading repositories…</div>;
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold">Repositories</h2>
        <Button size="sm" onClick={openAdd}>
          Add repository
        </Button>
      </div>

      {repos.length === 0 ? (
        <div className="rounded-md border border-dashed p-8 text-center">
          <p className="text-sm text-muted-foreground mb-4">No repositories registered yet.</p>
          <Button size="sm" onClick={openAdd}>
            Add your first repository
          </Button>
        </div>
      ) : (
        <div className="rounded-md border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">URL</th>
                <th className="px-4 py-2 font-medium">Default branch</th>
                <th className="px-4 py-2 font-medium">Updated</th>
                <th className="px-4 py-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {repos.map((repo) => (
                <tr key={repo.id} className="border-b last:border-0">
                  <td className="px-4 py-2 font-medium truncate max-w-[160px]">{repo.name}</td>
                  <td className="px-4 py-2 text-muted-foreground truncate max-w-[240px]">
                    {repo.url}
                  </td>
                  <td className="px-4 py-2 text-muted-foreground">{repo.default_branch}</td>
                  <td className="px-4 py-2 text-muted-foreground">
                    {new Date(repo.updated_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex items-center gap-1 justify-end">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => openEdit(repo)}
                      >
                        Edit
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-muted-foreground hover:text-destructive"
                        onClick={() => handleDelete(repo)}
                        disabled={deleteMut.isPending}
                      >
                        Delete
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <RepositoryDialog
        wsId={wsId}
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        editing={editing}
      />
    </div>
  );
}
