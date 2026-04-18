"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import { FolderGit2, GitBranch, Link2, Lock, Search } from "lucide-react";
import {
  useCreateRepository,
  useUpdateRepository,
} from "@multica/core/repositories";
import { integrationListOptions, githubReposOptions } from "@multica/core/integrations";
import type { GitHubRepo, Repository } from "@multica/core/api/client";
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

  const [sourceMode, setSourceMode] = React.useState<SourceMode>("github");
  const [url, setUrl] = React.useState(editing?.url ?? "");
  const [name, setName] = React.useState(editing?.name ?? "");
  const [defaultBranch, setDefaultBranch] = React.useState(editing?.default_branch ?? "main");
  const [description, setDescription] = React.useState(editing?.description ?? "");
  const [submitting, setSubmitting] = React.useState(false);
  const [selectedRepoFullName, setSelectedRepoFullName] = React.useState<string>("");
  const [repoFilter, setRepoFilter] = React.useState("");

  const createMut = useCreateRepository(wsId);
  const updateMut = useUpdateRepository(wsId, editing?.id ?? "");

  const { data: integrations = [] } = useQuery(integrationListOptions(wsId));
  const hasGitHub = integrations.some((i) => i.platform === "github");

  const { data: githubRepos = [], isLoading: reposLoading } = useQuery(
    githubReposOptions(wsId, open && hasGitHub && !isEdit),
  );

  const filteredRepos = React.useMemo(() => {
    const q = repoFilter.trim().toLowerCase();
    if (!q) return githubRepos;
    return githubRepos.filter((r: GitHubRepo) =>
      r.full_name.toLowerCase().includes(q) ||
      (r.description?.toLowerCase().includes(q) ?? false),
    );
  }, [githubRepos, repoFilter]);

  React.useEffect(() => {
    if (open) {
      setUrl(editing?.url ?? "");
      setName(editing?.name ?? "");
      setDefaultBranch(editing?.default_branch ?? "main");
      setDescription(editing?.description ?? "");
      setSelectedRepoFullName("");
      setRepoFilter("");
      setSourceMode(hasGitHub && !isEdit ? "github" : "manual");
    }
  }, [open, editing, hasGitHub, isEdit]);

  function selectGitHubRepo(repo: GitHubRepo) {
    setSelectedRepoFullName(repo.full_name);
    setUrl(repo.clone_url);
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

  // Which fields to show beyond the source picker.
  const showForm = isEdit || sourceMode === "manual" || selectedRepoFullName !== "";
  const submitDisabled =
    submitting ||
    (!isEdit && sourceMode === "github" && selectedRepoFullName === "") ||
    (showForm && (!url.trim() || !name.trim()));

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit repository" : "Add repository"}</DialogTitle>
          {!isEdit && (
            <DialogDescription>
              {hasGitHub
                ? "Pick from your connected GitHub account, paste any git URL, or enter an absolute local path (e.g. /Users/you/work/project)."
                : "Paste a git URL or enter an absolute local path (e.g. /Users/you/work/project). Connect GitHub in Integrations for a picker."}
            </DialogDescription>
          )}
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {!isEdit && hasGitHub && (
            <div className="inline-flex w-fit rounded-md border p-0.5">
              <Button
                type="button"
                size="sm"
                variant={sourceMode === "github" ? "secondary" : "ghost"}
                className="h-7 text-xs"
                onClick={() => {
                  setSourceMode("github");
                  setUrl("");
                  setName("");
                  setDefaultBranch("main");
                  setDescription("");
                  setSelectedRepoFullName("");
                }}
              >
                <FolderGit2 className="size-3.5" />
                From GitHub
              </Button>
              <Button
                type="button"
                size="sm"
                variant={sourceMode === "manual" ? "secondary" : "ghost"}
                className="h-7 text-xs"
                onClick={() => {
                  setSourceMode("manual");
                  setSelectedRepoFullName("");
                }}
              >
                <Link2 className="size-3.5" />
                Paste URL
              </Button>
            </div>
          )}

          {!isEdit && sourceMode === "github" && hasGitHub && (
            <GitHubRepoPicker
              repos={filteredRepos}
              allReposCount={githubRepos.length}
              loading={reposLoading}
              filter={repoFilter}
              onFilterChange={setRepoFilter}
              selected={selectedRepoFullName}
              onSelect={selectGitHubRepo}
            />
          )}

          {showForm && (
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="repo-url">
                  URL
                  {isEdit && (
                    <span className="ml-2 text-xs text-muted-foreground">(immutable)</span>
                  )}
                </Label>
                <Input
                  id="repo-url"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="https://github.com/org/repo.git  or  /Users/you/work/project"
                  required
                  disabled={isEdit || (sourceMode === "github" && selectedRepoFullName !== "")}
                  className="font-mono text-xs"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
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
                  <Label htmlFor="repo-branch">
                    Default branch{" "}
                    <GitBranch className="inline size-3 text-muted-foreground" />
                  </Label>
                  <Input
                    id="repo-branch"
                    value={defaultBranch}
                    onChange={(e) => setDefaultBranch(e.target.value)}
                    placeholder="main"
                  />
                </div>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="repo-desc">
                  Description{" "}
                  <span className="text-xs text-muted-foreground">(optional)</span>
                </Label>
                <Textarea
                  id="repo-desc"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={2}
                  placeholder="One-line summary for agents and teammates"
                />
              </div>
            </div>
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
            <Button type="submit" disabled={submitDisabled}>
              {submitting ? "Saving…" : isEdit ? "Save changes" : "Add repository"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface GitHubRepoPickerProps {
  repos: GitHubRepo[];
  allReposCount: number;
  loading: boolean;
  filter: string;
  onFilterChange: (value: string) => void;
  selected: string;
  onSelect: (repo: GitHubRepo) => void;
}

function GitHubRepoPicker({
  repos,
  allReposCount,
  loading,
  filter,
  onFilterChange,
  selected,
  onSelect,
}: GitHubRepoPickerProps) {
  if (loading) {
    return (
      <div className="space-y-1.5">
        <Label>Repository</Label>
        <div className="space-y-2 rounded-md border p-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      </div>
    );
  }

  if (allReposCount === 0) {
    return (
      <div className="rounded-md border border-dashed p-4 text-center text-xs text-muted-foreground">
        No repositories accessible from your GitHub account.
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      <Label>Repository</Label>
      <div className="rounded-md border">
        <div className="relative border-b">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter}
            onChange={(e) => onFilterChange(e.target.value)}
            placeholder={`Search ${allReposCount} repositories…`}
            className="h-8 border-0 pl-8 text-xs focus-visible:ring-0 focus-visible:ring-offset-0"
          />
        </div>
        <div className="max-h-56 overflow-y-auto">
          {repos.length === 0 ? (
            <div className="p-3 text-center text-xs text-muted-foreground">
              No matches for &ldquo;{filter}&rdquo;
            </div>
          ) : (
            <ul className="divide-y">
              {repos.map((repo) => (
                <li key={repo.full_name}>
                  <button
                    type="button"
                    onClick={() => onSelect(repo)}
                    className={`flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-muted focus:bg-muted focus:outline-none ${
                      selected === repo.full_name ? "bg-muted" : ""
                    }`}
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate font-mono text-xs">{repo.full_name}</span>
                        {repo.private && (
                          <Badge variant="outline" className="gap-1 text-[10px] font-normal">
                            <Lock className="size-2.5" />
                            private
                          </Badge>
                        )}
                      </div>
                      {repo.description && (
                        <p className="truncate text-xs text-muted-foreground">
                          {repo.description}
                        </p>
                      )}
                    </div>
                    <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                      {repo.default_branch}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
