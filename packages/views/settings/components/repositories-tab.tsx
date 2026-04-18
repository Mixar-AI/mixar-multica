"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { repositoryListOptions, useDeleteRepository } from "@multica/core/repositories";
import type { Repository } from "@multica/core/api/client";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { Copy, GitBranch, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { RepositoryDialog } from "./repository-dialog";
import { toast } from "sonner";
import { timeAgo } from "../../common/time";

const PLATFORM_LABELS: Record<string, string> = {
  github: "GitHub",
  gitlab: "GitLab",
  other: "Other",
};

export function RepositoriesTab() {
  const wsId = useWorkspaceId();
  const { data: repos = [], isLoading } = useQuery(repositoryListOptions(wsId));
  const deleteMut = useDeleteRepository(wsId);
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<Repository | null>(null);
  const [pendingDelete, setPendingDelete] = React.useState<Repository | null>(null);

  function openAdd() {
    setEditing(null);
    setDialogOpen(true);
  }

  function openEdit(repo: Repository) {
    setEditing(repo);
    setDialogOpen(true);
  }

  async function confirmDelete() {
    if (!pendingDelete) return;
    const name = pendingDelete.name;
    try {
      await deleteMut.mutateAsync(pendingDelete.id);
      toast.success(`Deleted "${name}"`);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to delete");
    } finally {
      setPendingDelete(null);
    }
  }

  async function copyUrl(url: string) {
    try {
      await navigator.clipboard.writeText(url);
      toast.success("URL copied");
    } catch {
      toast.error("Clipboard copy failed");
    }
  }

  return (
    <TooltipProvider delay={250}>
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-sm font-semibold">Repositories</h2>
            <p className="text-xs text-muted-foreground">
              Registered repos are available to agents via{" "}
              <code className="rounded bg-muted px-1 py-0.5">multica repo checkout</code>.
            </p>
          </div>
          <Button size="sm" onClick={openAdd}>
            <Plus className="size-3.5" />
            Add repository
          </Button>
        </div>

        {isLoading ? (
          <RepositoryTableSkeleton />
        ) : repos.length === 0 ? (
          <Empty className="border py-12">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <GitBranch className="size-6" />
              </EmptyMedia>
              <EmptyTitle>No repositories registered</EmptyTitle>
              <EmptyDescription>
                Add a repo to let agents check it out and open pull requests.
              </EmptyDescription>
            </EmptyHeader>
            <Button size="sm" onClick={openAdd}>
              <Plus className="size-3.5" />
              Add your first repository
            </Button>
          </Empty>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Branch</TableHead>
                  <TableHead>Updated</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {repos.map((repo) => (
                  <TableRow key={repo.id}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        <span className="truncate max-w-[200px]">{repo.name}</span>
                        <PlatformBadge platform={repo.platform} />
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <span className="inline-block max-w-[280px] truncate align-middle font-mono text-xs">
                              {repo.url}
                            </span>
                          }
                        />
                        <TooltipContent side="top" className="font-mono text-xs">
                          {repo.url}
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      <span className="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-xs">
                        <GitBranch className="size-3" />
                        {repo.default_branch}
                      </span>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      <Tooltip>
                        <TooltipTrigger render={<span>{timeAgo(repo.updated_at)} ago</span>} />
                        <TooltipContent side="top">
                          {new Date(repo.updated_at).toLocaleString()}
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell className="text-right">
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label="Row actions"
                            >
                              <MoreHorizontal className="size-4" />
                            </Button>
                          }
                        />
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onSelect={() => openEdit(repo)}>
                            <Pencil className="size-3.5" />
                            Edit
                          </DropdownMenuItem>
                          <DropdownMenuItem onSelect={() => copyUrl(repo.url)}>
                            <Copy className="size-3.5" />
                            Copy URL
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            onSelect={() => setPendingDelete(repo)}
                            className="text-destructive focus:text-destructive"
                          >
                            <Trash2 className="size-3.5" />
                            Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        <RepositoryDialog
          wsId={wsId}
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          editing={editing}
        />

        <AlertDialog
          open={pendingDelete !== null}
          onOpenChange={(open) => !open && setPendingDelete(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete repository?</AlertDialogTitle>
              <AlertDialogDescription>
                This deletes <span className="font-medium text-foreground">{pendingDelete?.name}</span>{" "}
                and cascades to all linked worktrees and pull requests. This can't be undone.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={deleteMut.isPending}>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={(e) => {
                  e.preventDefault();
                  confirmDelete();
                }}
                disabled={deleteMut.isPending}
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              >
                {deleteMut.isPending ? "Deleting…" : "Delete"}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </TooltipProvider>
  );
}

function PlatformBadge({ platform }: { platform: string }) {
  const label = PLATFORM_LABELS[platform] ?? platform;
  return (
    <Badge variant="secondary" className="text-[10px] font-normal">
      {label}
    </Badge>
  );
}

function RepositoryTableSkeleton() {
  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>URL</TableHead>
            <TableHead>Branch</TableHead>
            <TableHead>Updated</TableHead>
            <TableHead className="w-10" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {[0, 1, 2].map((i) => (
            <TableRow key={i}>
              <TableCell>
                <Skeleton className="h-4 w-24" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-4 w-56" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-4 w-16" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-4 w-12" />
              </TableCell>
              <TableCell>
                <Skeleton className="h-7 w-7 rounded-md" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
