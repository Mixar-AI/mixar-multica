"use client";

import { useState } from "react";
import { Rocket } from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { repositoryListOptions } from "@multica/core/repositories/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { api } from "@multica/core/api";
import type { DispatchTaskInput } from "@multica/core/api";

interface DispatchTaskDialogProps {
  issueId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Dialog for dispatching an agent task with optional per-task picker fields:
 * - Repository: dropdown from workspace's registered repos
 * - Base branch: text input (e.g. "feat/my-feature")
 * - Reuse worktree: checkbox — reuse the existing worktree as-is
 * - Folder scope: comma-separated sparse-checkout patterns
 */
export function DispatchTaskDialog({
  issueId,
  open,
  onOpenChange,
}: DispatchTaskDialogProps) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data: repositories = [] } = useQuery(repositoryListOptions(wsId));

  const [repositoryId, setRepositoryId] = useState<string>("");
  const [baseBranch, setBaseBranch] = useState("");
  const [reuseWorktree, setReuseWorktree] = useState(false);
  const [folderScope, setFolderScope] = useState("");

  const selectedRepo = repositories.find((r) => r.id === repositoryId);

  const dispatch = useMutation({
    mutationFn: (input: DispatchTaskInput) => api.dispatchTask(issueId, input),
    onSuccess: () => {
      toast.success("Task dispatched");
      onOpenChange(false);
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, issueId) });
    },
    onError: (err: Error) => {
      toast.error(err.message ?? "Failed to dispatch task");
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const input: DispatchTaskInput = {};
    if (repositoryId) input.repository_id = repositoryId;
    if (baseBranch.trim()) input.base_branch = baseBranch.trim();
    if (reuseWorktree) input.reuse_worktree = true;
    if (folderScope.trim()) {
      input.sparse_paths = folderScope
        .split(",")
        .map((p) => p.trim())
        .filter(Boolean);
    }
    dispatch.mutate(input);
  };

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      // Reset form on close.
      setRepositoryId("");
      setBaseBranch("");
      setReuseWorktree(false);
      setFolderScope("");
    }
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] !max-w-[440px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Rocket className="size-4" />
            Dispatch task
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-1">
          {/* Repository picker */}
          <div className="space-y-1.5">
            <Label htmlFor="dispatch-repo">Repository</Label>
            {repositories.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                No repositories registered in this workspace.
              </p>
            ) : (
              <Select value={repositoryId} onValueChange={(v) => setRepositoryId(v ?? "")}>
                <SelectTrigger id="dispatch-repo" className="w-full">
                  <SelectValue placeholder="Auto-pick" />
                </SelectTrigger>
                <SelectContent>
                  {repositories.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      {r.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          {/* Base branch input */}
          <div className="space-y-1.5">
            <Label htmlFor="dispatch-branch">Base branch</Label>
            <Input
              id="dispatch-branch"
              placeholder={
                selectedRepo?.default_branch
                  ? `Default: ${selectedRepo.default_branch}`
                  : "Default branch"
              }
              value={baseBranch}
              onChange={(e) => setBaseBranch(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Branch to fork from. Leave blank to use the repo&apos;s default branch.
            </p>
          </div>

          {/* Folder scope */}
          <div className="space-y-1.5">
            <Label htmlFor="dispatch-scope">Folder scope</Label>
            <Input
              id="dispatch-scope"
              placeholder="e.g. packages/web, docs"
              value={folderScope}
              onChange={(e) => setFolderScope(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              Comma-separated paths to limit the agent&apos;s working tree (sparse
              checkout). Leave blank for full checkout.
            </p>
          </div>

          {/* Reuse worktree */}
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <Checkbox
              checked={reuseWorktree}
              onCheckedChange={(v) => setReuseWorktree(v === true)}
            />
            <span>Reuse existing worktree</span>
          </label>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={dispatch.isPending}>
              {dispatch.isPending ? "Dispatching..." : "Dispatch"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
