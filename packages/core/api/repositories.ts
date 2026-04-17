import { api } from "./index";
import type {
  Repository,
  CreateRepositoryInput,
  UpdateRepositoryInput,
} from "./client";

export type { Repository, CreateRepositoryInput, UpdateRepositoryInput };

export const repositoriesApi = {
  list: (wsId: string): Promise<Repository[]> => api.listRepositories(wsId),
  get: (wsId: string, id: string): Promise<Repository> => api.getRepository(wsId, id),
  create: (wsId: string, input: CreateRepositoryInput): Promise<Repository> =>
    api.createRepository(wsId, input),
  update: (wsId: string, id: string, input: UpdateRepositoryInput): Promise<Repository> =>
    api.updateRepository(wsId, id, input),
  delete: (wsId: string, id: string): Promise<void> => api.deleteRepository(wsId, id),
};
