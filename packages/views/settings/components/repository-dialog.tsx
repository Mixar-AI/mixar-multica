"use client";

import * as React from "react";
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
import { useCreateRepository, useUpdateRepository } from "@multica/core/repositories";
import type { Repository } from "@multica/core/api/client";
import { toast } from "sonner";

interface Props {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editing?: Repository | null;
}

export function RepositoryDialog({ wsId, open, onOpenChange, editing }: Props) {
  const isEdit = Boolean(editing);
  const [url, setUrl] = React.useState(editing?.url ?? "");
  const [name, setName] = React.useState(editing?.name ?? "");
  const [defaultBranch, setDefaultBranch] = React.useState(editing?.default_branch ?? "main");
  const [description, setDescription] = React.useState(editing?.description ?? "");
  const [submitting, setSubmitting] = React.useState(false);

  const createMut = useCreateRepository(wsId);
  const updateMut = useUpdateRepository(wsId, editing?.id ?? "");

  React.useEffect(() => {
    if (open) {
      setUrl(editing?.url ?? "");
      setName(editing?.name ?? "");
      setDefaultBranch(editing?.default_branch ?? "main");
      setDescription(editing?.description ?? "");
    }
  }, [open, editing]);

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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit repository" : "Add repository"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="repo-url">URL</Label>
            <Input
              id="repo-url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://github.com/org/repo.git"
              required
              disabled={isEdit}
            />
            {isEdit && (
              <p className="text-xs text-muted-foreground">
                URL is immutable after creation.
              </p>
            )}
          </div>
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
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? "Saving…" : isEdit ? "Save changes" : "Add repository"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
