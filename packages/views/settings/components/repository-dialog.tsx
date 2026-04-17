"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { useCreateRepository, useUpdateRepository } from "@multica/core/repositories";
import { integrationListOptions, githubReposOptions } from "@multica/core/integrations";
import type { Repository } from "@multica/core/api/client";
import type { GitHubRepo } from "@multica/core/api/client";
import { toast } from "sonner";

type SourceMode = "github" | "manual";

interface Props {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editing?: Repository | null;
}

export function RepositoryDialog({ wsId, open, onOpenChange, editing }: Props) {
  const isEdit = Boolean(editing);

  // Source mode toggle: "github" (picker) or "manual" (paste URL).
  const [sourceMode, setSourceMode] = React.useState<SourceMode>("github");

  const [url, setUrl] = React.useState(editing?.url ?? "");
  const [name, setName] = React.useState(editing?.name ?? "");
  const [defaultBranch, setDefaultBranch] = React.useState(editing?.default_branch ?? "main");
  const [description, setDescription] = React.useState(editing?.description ?? "");
  const [submitting, setSubmitting] = React.useState(false);

  // Selected GitHub repo full_name (used to drive auto-fill in picker mode).
  const [selectedRepoFullName, setSelectedRepoFullName] = React.useState<string>("");

  const createMut = useCreateRepository(wsId);
  const updateMut = useUpdateRepository(wsId, editing?.id ?? "");

  // Load integrations to check if GitHub is connected.
  const { data: integrations = [] } = useQuery(integrationListOptions(wsId));
  const hasGitHub = integrations.some((i) => i.platform === "github");

  // Load GitHub repos when the dialog is open, GitHub is connected, and we're in picker mode.
  const { data: githubRepos = [], isLoading: reposLoading } = useQuery(
    githubReposOptions(wsId, open && hasGitHub && !isEdit),
  );

  React.useEffect(() => {
    if (open) {
      setUrl(editing?.url ?? "");
      setName(editing?.name ?? "");
      setDefaultBranch(editing?.default_branch ?? "main");
      setDescription(editing?.description ?? "");
      setSelectedRepoFullName("");
      // Default to github picker when integration exists and not editing.
      setSourceMode(hasGitHub && !isEdit ? "github" : "manual");
    }
  }, [open, editing, hasGitHub, isEdit]);

  function handleGitHubRepoSelect(fullName: string | null) {
    if (!fullName) return;
    setSelectedRepoFullName(fullName);
    const repo = githubRepos.find((r: GitHubRepo) => r.full_name === fullName);
    if (!repo) return;
    setUrl(repo.clone_url);
    // Derive name from full_name (e.g. "org/repo" → "repo").
    const parts = repo.full_name.split("/");
    setName(parts[parts.length - 1] ?? repo.full_name);
    setDefaultBranch(repo.default_branch || "main");
    setDescription(repo.description ?? "");
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      if (isEdit && editing) {
        await updateMut.mutateAsync({ name, description, default_branch: defaultBranch });
        toast.success("Repository updated");
      } else {
        await createMut.mutateAsync({ url, name, default_branch: defaultBranch, description });
        toast.success("Repository added");
      }
      onOpenChange(false);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Failed to save repository");
    } finally {
      setSubmitting(false);
    }
  }

  // Whether to show the URL field (always in edit mode, always in manual mode,
  // and in github mode after a repo has been selected).
  const showURLField = isEdit || sourceMode === "manual" || selectedRepoFullName !== "";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit repository" : "Add repository"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Source toggle — only show when adding a new repo and GitHub is connected */}
          {!isEdit && hasGitHub && (
            <div className="flex items-center gap-1 rounded-md border p-1 w-fit">
              <Button
                type="button"
                size="sm"
                variant={sourceMode === "github" ? "default" : "ghost"}
                className="h-6 px-3 text-xs"
                onClick={() => {
                  setSourceMode("github");
                  setUrl("");
                  setName("");
                  setDefaultBranch("main");
                  setDescription("");
                  setSelectedRepoFullName("");
                }}
              >
                From GitHub
              </Button>
              <Button
                type="button"
                size="sm"
                variant={sourceMode === "manual" ? "default" : "ghost"}
                className="h-6 px-3 text-xs"
                onClick={() => {
                  setSourceMode("manual");
                  setSelectedRepoFullName("");
                }}
              >
                Paste URL
              </Button>
            </div>
          )}

          {/* GitHub picker mode */}
          {!isEdit && sourceMode === "github" && hasGitHub && (
            <div className="space-y-1.5">
              <Label>Repository</Label>
              {reposLoading ? (
                <div className="text-xs text-muted-foreground py-2">
                  Loading GitHub repositories...
                </div>
              ) : (
                <Select value={selectedRepoFullName} onValueChange={handleGitHubRepoSelect}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select a repository..." />
                  </SelectTrigger>
                  <SelectContent className="max-h-64 overflow-y-auto">
                    {githubRepos.map((repo: GitHubRepo) => (
                      <SelectItem key={repo.full_name} value={repo.full_name}>
                        <span className="truncate max-w-xs">{repo.full_name}</span>
                        {repo.private && (
                          <span className="ml-2 text-xs text-muted-foreground">private</span>
                        )}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
          )}

          {/* URL field — shown in manual mode, edit mode, or after picking */}
          {showURLField && (
            <div className="space-y-1.5">
              <Label htmlFor="repo-url">URL</Label>
              <Input
                id="repo-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://github.com/org/repo.git"
                required
                disabled={isEdit || (sourceMode === "github" && selectedRepoFullName !== "")}
              />
              {isEdit && (
                <p className="text-xs text-muted-foreground">URL is immutable after creation.</p>
              )}
            </div>
          )}

          {/* Name, branch, description — always shown once a source is chosen */}
          {(isEdit || sourceMode === "manual" || selectedRepoFullName !== "") && (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="repo-name">Name</Label>
                <Input
                  id="repo-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="frontend"
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="repo-branch">Default branch</Label>
                <Input
                  id="repo-branch"
                  value={defaultBranch}
                  onChange={(e) => setDefaultBranch(e.target.value)}
                  placeholder="main"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="repo-desc">Description</Label>
                <Textarea
                  id="repo-desc"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={3}
                />
              </div>
            </>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={submitting || (sourceMode === "github" && !isEdit && selectedRepoFullName === "" && !isEdit)}
            >
              {submitting ? "Saving..." : isEdit ? "Save changes" : "Add repository"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
