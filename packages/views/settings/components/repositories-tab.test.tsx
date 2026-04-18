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

// Stub the nested dialog so we can test the tab in isolation
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

// Flatten UI primitives so userEvent can drive them without base-ui's portaling.
// Button supports both children and the `render` prop pattern used by base-ui
// triggers (DropdownMenuTrigger render={<Button>…</Button>}).

type ButtonStubProps = {
  children?: ReactNode;
  render?: React.ReactElement;
  onClick?: () => void;
  disabled?: boolean;
  "aria-label"?: string;
  className?: string;
};

function buttonStub({ children, render: renderProp, ...rest }: ButtonStubProps) {
  if (renderProp) return renderProp;
  return (
    <button type="button" {...rest}>
      {children}
    </button>
  );
}

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: buttonStub,
}));

vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: ({ className }: { className?: string }) => (
    <div data-testid="skeleton" className={className} />
  ),
}));

vi.mock("@multica/ui/components/ui/badge", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("@multica/ui/components/ui/table", () => {
  const passthrough = (tag: keyof HTMLElementTagNameMap) =>
    ({ children, ...rest }: { children?: ReactNode }) => {
      const El = tag as string;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return <El {...(rest as any)}>{children}</El>;
    };
  return {
    Table: passthrough("table"),
    TableHeader: passthrough("thead"),
    TableBody: passthrough("tbody"),
    TableRow: passthrough("tr"),
    TableHead: passthrough("th"),
    TableCell: passthrough("td"),
  };
});

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactElement }) => render,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({
    children,
    onSelect,
  }: {
    children: ReactNode;
    onSelect?: () => void;
  }) => (
    <button type="button" onClick={onSelect}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactElement }) => render,
  TooltipContent: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("@multica/ui/components/ui/empty", () => ({
  Empty: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  EmptyHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  EmptyMedia: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  EmptyTitle: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  EmptyDescription: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ open, children }: { open: boolean; children: ReactNode }) =>
    open ? <div role="alertdialog">{children}</div> : null,
  AlertDialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: ReactNode }) => <h2>{children}</h2>,
  AlertDialogDescription: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  AlertDialogCancel: ({ children }: { children: ReactNode }) => (
    <button type="button">{children}</button>
  ),
  AlertDialogAction: ({
    children,
    onClick,
    disabled,
  }: {
    children: ReactNode;
    onClick?: (e: React.MouseEvent) => void;
    disabled?: boolean;
  }) => (
    <button
      type="button"
      onClick={(e) => onClick?.(e)}
      disabled={disabled}
      data-testid="alert-dialog-confirm"
    >
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
    mockUseQuery.mockReturnValue({ data: [], isLoading: false });
  });

  it("shows skeleton placeholders while loading", () => {
    mockUseQuery.mockReturnValue({ data: undefined, isLoading: true });
    render(<RepositoriesTab />);
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  it("shows the empty state when there are no repositories", () => {
    render(<RepositoriesTab />);
    expect(screen.getByText(/no repositories registered/i)).toBeInTheDocument();
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
    // URL appears twice (visible span + tooltip content); just check it's present.
    expect(screen.getAllByText("https://github.com/org/frontend.git").length).toBeGreaterThan(0);
  });

  it("opens the edit dialog with the correct repo when Edit is chosen from the row menu", async () => {
    const user = userEvent.setup();
    const repo = makeRepo({ name: "my-repo" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /edit/i }));

    expect(screen.getByTestId("repository-dialog")).toBeInTheDocument();
    expect(screen.getByTestId("dialog-mode")).toHaveTextContent("edit");
  });

  it("opens confirm dialog, confirms delete, calls mutation and shows success toast", async () => {
    const user = userEvent.setup();
    mockDeleteMutateAsync.mockResolvedValue(undefined);

    const repo = makeRepo({ id: "repo-99", name: "to-delete" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    // Click Delete in the row menu — this opens the alert dialog.
    await user.click(screen.getByRole("button", { name: /delete/i }));
    // The alert dialog now renders with a confirm button.
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();

    await user.click(screen.getByTestId("alert-dialog-confirm"));

    await waitFor(() => {
      expect(mockDeleteMutateAsync).toHaveBeenCalledWith("repo-99");
    });
    expect(mockToastSuccess).toHaveBeenCalledWith('Deleted "to-delete"');
  });

  it("shows error toast when delete fails", async () => {
    const user = userEvent.setup();
    mockDeleteMutateAsync.mockRejectedValue(new Error("Server error"));

    const repo = makeRepo({ id: "repo-99", name: "to-delete" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    await user.click(screen.getByRole("button", { name: /delete/i }));
    await user.click(screen.getByTestId("alert-dialog-confirm"));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith("Server error");
    });
  });

  it("does not call delete mutation until confirm is clicked in the alert dialog", async () => {
    const user = userEvent.setup();

    const repo = makeRepo({ name: "keep-me" });
    mockUseQuery.mockReturnValue({ data: [repo], isLoading: false });

    render(<RepositoriesTab />);

    // Open delete menu — this opens the dialog but does NOT mutate yet.
    await user.click(screen.getByRole("button", { name: /delete/i }));
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(mockDeleteMutateAsync).not.toHaveBeenCalled();
  });
});
