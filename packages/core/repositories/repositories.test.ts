import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { ApiClient } from "../api/client";
import { repositoriesKeys, repositoryListOptions, repositoryDetailOptions } from "./queries";

// ── query key tests ───────────────────────────────────────────────────────────

describe("repositoriesKeys", () => {
  it("list key includes wsId", () => {
    expect(repositoriesKeys.list("ws-1")).toEqual(["repositories", "ws-1", "list"]);
  });

  it("detail key includes wsId + id", () => {
    expect(repositoriesKeys.detail("ws-1", "repo-1")).toEqual([
      "repositories",
      "ws-1",
      "detail",
      "repo-1",
    ]);
  });

  it("all is a prefix of list", () => {
    const all = repositoriesKeys.all("ws-1");
    const list = repositoriesKeys.list("ws-1");
    expect(list.slice(0, all.length)).toEqual([...all]);
  });
});

// ── queryOptions shape tests ──────────────────────────────────────────────────

describe("repositoryListOptions", () => {
  it("uses the correct query key", () => {
    const opts = repositoryListOptions("ws-1");
    expect(opts.queryKey).toEqual(repositoriesKeys.list("ws-1"));
  });

  it("is disabled when wsId is empty", () => {
    const opts = repositoryListOptions("");
    expect(opts.enabled).toBe(false);
  });
});

describe("repositoryDetailOptions", () => {
  it("uses the correct query key", () => {
    const opts = repositoryDetailOptions("ws-1", "repo-1");
    expect(opts.queryKey).toEqual(repositoriesKeys.detail("ws-1", "repo-1"));
  });

  it("is disabled when id is empty", () => {
    const opts = repositoryDetailOptions("ws-1", "");
    expect(opts.enabled).toBe(false);
  });
});

// ── ApiClient HTTP contract tests ─────────────────────────────────────────────

describe("ApiClient — repository endpoints", () => {
  const baseUrl = "https://api.example.test";
  let fetchMock: ReturnType<typeof vi.fn>;
  let client: ApiClient;

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "repo-1",
          workspace_id: "ws-1",
          url: "https://github.com/org/repo.git",
          name: "repo",
          default_branch: "main",
          description: "",
          platform: "github",
          created_at: "",
          updated_at: "",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    client = new ApiClient(baseUrl);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("listRepositories calls GET /api/workspaces/:wsId/repositories", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    await client.listRepositories("ws-1");
    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/api/workspaces/ws-1/repositories`,
      expect.objectContaining({ headers: expect.any(Object) }),
    );
  });

  it("getRepository calls GET /api/workspaces/:wsId/repositories/:id", async () => {
    await client.getRepository("ws-1", "repo-1");
    const url = fetchMock.mock.calls[0]?.[0] as string;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/repositories/repo-1`);
  });

  it("createRepository calls POST with body", async () => {
    await client.createRepository("ws-1", {
      url: "https://github.com/org/repo.git",
      name: "repo",
    });
    const url = fetchMock.mock.calls[0]?.[0] as string;
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/repositories`);
    expect(init?.method).toBe("POST");
    expect(JSON.parse(init?.body as string)).toMatchObject({
      url: "https://github.com/org/repo.git",
      name: "repo",
    });
  });

  it("updateRepository calls PATCH with body", async () => {
    await client.updateRepository("ws-1", "repo-1", { name: "renamed" });
    const url = fetchMock.mock.calls[0]?.[0] as string;
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/repositories/repo-1`);
    expect(init?.method).toBe("PATCH");
    expect(JSON.parse(init?.body as string)).toEqual({ name: "renamed" });
  });

  it("deleteRepository calls DELETE", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await client.deleteRepository("ws-1", "repo-1");
    const url = fetchMock.mock.calls[0]?.[0] as string;
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/repositories/repo-1`);
    expect(init?.method).toBe("DELETE");
  });
});

// ── ApiClient HTTP contract tests — worktrees ─────────────────────────────────

describe("ApiClient — worktree endpoints", () => {
  const baseUrl = "https://api.example.test";
  let fetchMock: ReturnType<typeof vi.fn>;
  let client: ApiClient;

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);
    client = new ApiClient(baseUrl);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("listWorktrees calls GET /api/workspaces/:wsId/worktrees by default", async () => {
    await client.listWorktrees("ws-1");
    const url = fetchMock.mock.calls[0]?.[0] as string;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/worktrees`);
  });

  it("listWorktrees uses per-repo path when repositoryId is provided", async () => {
    await client.listWorktrees("ws-1", { repositoryId: "repo-1" });
    const url = fetchMock.mock.calls[0]?.[0] as string;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/repositories/repo-1/worktrees`);
  });

  it("listWorktrees appends include_inactive query param", async () => {
    await client.listWorktrees("ws-1", { includeInactive: true });
    const url = fetchMock.mock.calls[0]?.[0] as string;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/worktrees?include_inactive=true`);
  });

  it("getWorktree calls GET /api/workspaces/:wsId/worktrees/:id", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({ id: "wt-1", repository_id: "repo-1", task_id: null, path: "/tmp/wt", branch_name: "feat", base_branch: "main", status: "active", head_sha: "", sparse_paths: null, created_at: "", last_used_at: "", deleted_at: null }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    await client.getWorktree("ws-1", "wt-1");
    const url = fetchMock.mock.calls[0]?.[0] as string;
    expect(url).toBe(`${baseUrl}/api/workspaces/ws-1/worktrees/wt-1`);
  });
});
