import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { repositoriesKeys } from "./queries";
import type { Repository, CreateRepositoryInput, UpdateRepositoryInput } from "../api/client";

export function useCreateRepository(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateRepositoryInput) => api.createRepository(wsId, input),
    onSuccess: (created) => {
      qc.setQueryData<Repository[]>(repositoriesKeys.list(wsId), (prev) =>
        prev ? [...prev, created] : [created],
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: repositoriesKeys.list(wsId) });
    },
  });
}

export function useUpdateRepository(wsId: string, id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateRepositoryInput) => api.updateRepository(wsId, id, input),
    onMutate: (input) => {
      qc.cancelQueries({ queryKey: repositoriesKeys.list(wsId) });
      const prevList = qc.getQueryData<Repository[]>(repositoriesKeys.list(wsId));
      const prevDetail = qc.getQueryData<Repository>(repositoriesKeys.detail(wsId, id));
      qc.setQueryData<Repository[]>(repositoriesKeys.list(wsId), (prev) =>
        prev?.map((r) => (r.id === id ? { ...r, ...input } : r)),
      );
      qc.setQueryData<Repository>(repositoriesKeys.detail(wsId, id), (prev) =>
        prev ? { ...prev, ...input } : prev,
      );
      return { prevList, prevDetail };
    },
    onError: (_err, _input, ctx) => {
      if (ctx?.prevList) qc.setQueryData(repositoriesKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(repositoriesKeys.detail(wsId, id), ctx.prevDetail);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: repositoriesKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: repositoriesKeys.list(wsId) });
    },
  });
}

export function useDeleteRepository(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteRepository(wsId, id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: repositoriesKeys.list(wsId) });
      const prevList = qc.getQueryData<Repository[]>(repositoriesKeys.list(wsId));
      qc.setQueryData<Repository[]>(repositoriesKeys.list(wsId), (prev) =>
        prev?.filter((r) => r.id !== id),
      );
      qc.removeQueries({ queryKey: repositoriesKeys.detail(wsId, id) });
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(repositoriesKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: repositoriesKeys.list(wsId) });
    },
  });
}
