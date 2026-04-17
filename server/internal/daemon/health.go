package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// HealthResponse is returned by the daemon's local health endpoint.
type HealthResponse struct {
	Status          string            `json:"status"`
	PID             int               `json:"pid"`
	Uptime          string            `json:"uptime"`
	DaemonID        string            `json:"daemon_id"`
	DeviceName      string            `json:"device_name"`
	ServerURL       string            `json:"server_url"`
	CLIVersion      string            `json:"cli_version"`
	ActiveTaskCount int64             `json:"active_task_count"`
	Agents          []string          `json:"agents"`
	Workspaces      []healthWorkspace `json:"workspaces"`
}

type healthWorkspace struct {
	ID       string   `json:"id"`
	Runtimes []string `json:"runtimes"`
}

// listenHealth binds the health port. Returns the listener or an error if
// another daemon is already running (port taken).
func (d *Daemon) listenHealth() (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", d.cfg.HealthPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("another daemon is already running on %s: %w", addr, err)
	}
	return ln, nil
}

// repoCheckoutRequest is the body of a POST /repo/checkout request.
type repoCheckoutRequest struct {
	URL          string   `json:"url"`
	WorkspaceID  string   `json:"workspace_id"`
	WorkDir      string   `json:"workdir"`
	AgentName    string   `json:"agent_name"`
	TaskID       string   `json:"task_id"`
	BaseBranch   string   `json:"base_branch,omitempty"`
	ReuseWorktree bool    `json:"reuse_worktree,omitempty"`
	SparsePaths  []string `json:"sparse_paths,omitempty"`
}

// healthHandler returns the /health HTTP handler. Extracted from serveHealth
// so tests can exercise it without spinning up a listener.
func (d *Daemon) healthHandler(startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		var wsList []healthWorkspace
		for id, ws := range d.workspaces {
			wsList = append(wsList, healthWorkspace{
				ID:       id,
				Runtimes: ws.runtimeIDs,
			})
		}
		d.mu.Unlock()

		agents := make([]string, 0, len(d.cfg.Agents))
		for name := range d.cfg.Agents {
			agents = append(agents, name)
		}

		resp := HealthResponse{
			Status:          "running",
			PID:             os.Getpid(),
			Uptime:          time.Since(startedAt).Truncate(time.Second).String(),
			DaemonID:        d.cfg.DaemonID,
			DeviceName:      d.cfg.DeviceName,
			ServerURL:       d.cfg.ServerBaseURL,
			CLIVersion:      d.cfg.CLIVersion,
			ActiveTaskCount: d.activeTasks.Load(),
			Agents:          agents,
			Workspaces:      wsList,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// serveHealth runs the health HTTP server on the given listener.
// Blocks until ctx is cancelled.
func (d *Daemon) serveHealth(ctx context.Context, ln net.Listener, startedAt time.Time) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.healthHandler(startedAt))

	mux.HandleFunc("/repo/checkout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req repoCheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url is required", http.StatusBadRequest)
			return
		}
		if req.WorkspaceID == "" {
			http.Error(w, "workspace_id is required", http.StatusBadRequest)
			return
		}
		if req.WorkDir == "" {
			http.Error(w, "workdir is required", http.StatusBadRequest)
			return
		}

		if d.repoCache == nil {
			http.Error(w, "repo cache not initialized", http.StatusInternalServerError)
			return
		}

		if err := d.ensureRepoReady(r.Context(), req.WorkspaceID, req.URL); err != nil {
			statusCode := http.StatusInternalServerError
			if errors.Is(err, ErrRepoNotConfigured) {
				statusCode = http.StatusBadRequest
			}
			d.logger.Error("repo checkout readiness failed", "workspace_id", req.WorkspaceID, "url", req.URL, "error", err)
			http.Error(w, err.Error(), statusCode)
			return
		}

		result, err := d.repoCache.CreateWorktree(r.Context(), repocache.WorktreeParams{
			WorkspaceID:   req.WorkspaceID,
			RepoURL:       req.URL,
			WorkDir:       req.WorkDir,
			AgentName:     req.AgentName,
			TaskID:        req.TaskID,
			BaseBranch:    req.BaseBranch,
			ReuseWorktree: req.ReuseWorktree,
		})
		if err != nil {
			d.logger.Error("repo checkout failed", "url", req.URL, "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Apply sparse-checkout patterns if requested. This is done after
		// worktree creation so the tree is in a valid state before scoping.
		if len(req.SparsePaths) > 0 {
			if err := applySparseCheckout(result.Path, req.SparsePaths); err != nil {
				d.logger.Warn("sparse-checkout failed (non-fatal, full checkout used)", "path", result.Path, "error", err)
				// Non-fatal: the agent gets a full checkout rather than a scoped one.
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	d.logger.Info("health server listening", "addr", ln.Addr().String())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		d.logger.Warn("health server error", "error", err)
	}
}

// applySparseCheckout enables cone-mode sparse-checkout in a worktree and
// limits the working tree to the given path patterns.
//
// It runs:
//
//	git -C <path> sparse-checkout init --cone
//	git -C <path> sparse-checkout set <patterns...>
//
// This is a best-effort operation — if it fails the worktree falls back to a
// full checkout and the caller logs a warning.
func applySparseCheckout(worktreePath string, patterns []string) error {
	initCmd := exec.Command("git", "-C", worktreePath, "sparse-checkout", "init", "--cone")
	if out, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sparse-checkout init: %s: %w", string(out), err)
	}
	setArgs := append([]string{"-C", worktreePath, "sparse-checkout", "set"}, patterns...)
	setCmd := exec.Command("git", setArgs...)
	if out, err := setCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sparse-checkout set: %s: %w", string(out), err)
	}
	return nil
}
