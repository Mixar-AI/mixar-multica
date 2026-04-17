import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Repository } from "@multica/core/api/client";

// ─── hoisted mocks ────────────────────────────────────────────────────────────

const mockWsId = vi.hoisted(() => vi.fn(() => "ws-1"));
const mockUseQuery = vi.hoisted(() => vi.fn());
const mockDeleteMutateAsync = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

// ─── module mocks ─────────────────────────────────────────────────────────────

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: mockWsId,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: mockUseQuery,
}));

vi.mock("@multica/core/repositories", () => ({
  repositoryListOptions: (wsId: string) => ({ queryKey: ["repositories", wsId, "list"] }),
  useDeleteRepository: () => ({
    mutateAsync: mockDeleteMutateAsync,
    isPending: false,
  }),
  useCreateRepository: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useUpdateRepository: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

// Stub the dialog so we can test the tab in isolation
vi.mock("./repository-dialog", () => ({
  RepositoryDialog: ({
    open,
    onOpenChange,
    editing,
  }: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    editing?: Repository | null;
    wsId: string;
  }) =>
    open ? (
      <div data-testid="repository-dialog">
        <span data-testid="dialog-mode">{editing ? "edit" : "add"}</span>
        <button type="button" onClick={() => onOpenChange(false)}>
          Close dialog
        </button>
      </div>
    ) : null,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
    disabled,
    className,
    size: _size,
    variant: _variant,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    className?: string;
    size?: string;
    variant?: string;
  }) => (
    <button type="button" onClick={onClick} disabled={disabled} className={className}>
      {children}
    </button>
  ),
}));

// ─── component under test ─────────────────────────────────────────────────────

import { RepositoriesTab } from "./repositories-tab";

// ─── helpers ──────────────────────────────────────────────────────────────────

const makeRepo = (overrides: Partial<Repository> = {}): Repository => ({
  id: "repo-1",
  workspace_id: "ws-1",
  url: "https://github.com/org/repo.git",
  name: "repo",
  default_branch: "main",
  description: "A test repo",
  platform: "github",
  created_at: "2024-01-01T00:00:00Z",
  updated_at: "2024-01-02T00:00:00Z",
  ...overrides,
});

// ─── tests ────────────────────────────────────────────────────────────────────

describe("RepositoriesTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockWsId.mockReturnValue("ws-1");
    // Default: no repos
    mockUseQuery.mockReturnValue({ data: [], isLoading: false });
  });

  it("shows a loading state while fetching", () => {
    mockUseQuery.mockReturnValue({ data: undefined, isLoading: true });
    render(<RepositoriesTab />);
    expect(screen.getByText(/loading repositories/i)).toBeInTheDocument();
  });

  it("shows the empty state when there are no repositories", () => {
    render(<RepositoriesTab />);
    expect(screen.getByText(/no repositories registered yet/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add your first repository/i })).toBeInTheDocument();
  });

  it("opens the add dialog when 'Add repository' is clicked from the header", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /add repository/i }));

    expect(screen.getByTestId("repository-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("dialog-mode")).toHaveTextContent("add");
  });

  it("opens the add dialog when 'Add your first repository' is clicked in empty state", async () => {
    const user = userEvent.setup();
    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /add your first repository/i }));

    expect(screen.getByTestId("repository-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("dialog-mode")).toHaveTextContent("add");
  });

  it("renders the repository list when repos are present", () => {
    const repos = [
      makeRepo({ id: "r1", name: "frontend", url: "https://github.com/org/frontend.git" }),
      makeRepo({ id: "r2", name: "backend", url: "https://github.com/org/backend.git" }),
    ];
    mockUseQuery.mockReturnValue({ data: repos, isLoading: false });

    render(<RepositoriesTab />);

    expect(screen.getByText("frontend")).toBeInTheDocument();
    expect(screen.getByText("backend")).toBeInTheDocument();
    expect(screen.getByText("https://github.com/org/frontend.git")).toBeInTheDocument();
  });

  it("opens the edit dialog with the correct repo when Edit is clicked", async () => {
    const user = userEvent.setup();
    const repo = makeRepo({ name: "my-repo" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /edit/i }));

    expect(screen.getByTestId("repository-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("dialog-mode")).toHaveTextContent("edit");
  });

  it("calls delete mutation and shows success toast when Delete is confirmed", async () => {
    const user = userEvent.setup();
    vi.spyOn(window, "confirm").mockReturnValue(true);
    mockDeleteMutateAsync.mockResolvedValue(undefined);

    const repo = makeRepo({ id: "repo-99", name: "to-delete" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /delete/i }));

    await waitFor(() => {
      expect(mockDeleteMutateAsync).toHaveBeenCalledWith("repo-99");
    });
    expect(mockToastSuccess).toHaveBeenCalledWith('Deleted "to-delete"');
  });

  it("shows error toast when delete fails", async () => {
    const user = userEvent.setup();
    vi.spyOn(window, "confirm").mockReturnValue(true);
    mockDeleteMutateAsync.mockRejectedValue(new Error("Server error"));

    const repo = makeRepo({ id: "repo-99", name: "to-delete" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /delete/i }));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("Server error");
    });
  });

  it("does not call delete mutation when confirm is cancelled", async () => {
    const user = userEvent.setup();
    vi.spyOn(window, "confirm").mockReturnValue(false);

    const repo = makeRepo({ name: "keep-me" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /delete/i }));

    expect(mockDeleteMutateAsync).not.toHaveBeenCalled();
  });
});
