package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthHandlerReportsCLIVersionAndActiveTaskCount(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg: Config{
			CLIVersion:    "v9.9.9",
			DaemonID:      "daemon-test",
			DeviceName:    "dev",
			ServerBaseURL: "http://localhost:8080",
		},
		workspaces: map[string]*workspaceState{},
		logger:     slog.Default(),
	}
	d.activeTasks.Store(3)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	d.healthHandler(time.Now()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Decode into a raw map so the test locks in the exact wire-level JSON
	// keys — the desktop TS client depends on snake_case (cli_version,
	// active_task_count), so a silent struct-tag rename must fail here.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if got, want := raw["cli_version"], "v9.9.9"; got != want {
		t.Errorf("cli_version key: got %v, want %q", got, want)
	}
	// JSON numbers decode to float64 through map[string]any.
	if got, want := raw["active_task_count"], float64(3); got != want {
		t.Errorf("active_task_count key: got %v, want %v", got, want)
	}
	if got, want := raw["status"], "running"; got != want {
		t.Errorf("status key: got %v, want %q", got, want)
	}

	// Also round-trip into the typed struct as a separate check that the
	// field values match, independent of key naming.
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode typed response: %v", err)
	}
	if resp.CLIVersion != "v9.9.9" {
		t.Errorf("CLIVersion: got %q, want %q", resp.CLIVersion, "v9.9.9")
	}
	if resp.ActiveTaskCount != 3 {
		t.Errorf("ActiveTaskCount: got %d, want 3", resp.ActiveTaskCount)
	}
}

func TestHealthHandlerActiveTaskCountTracksCounter(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg:        Config{CLIVersion: "v1.0.0"},
		workspaces: map[string]*workspaceState{},
		logger:     slog.Default(),
	}
	handler := d.healthHandler(time.Now())

	// Simulate the pollLoop increment/decrement protocol.
	d.activeTasks.Add(1)
	d.activeTasks.Add(1)
	assertActiveTaskCount(t, handler, 2)

	d.activeTasks.Add(-1)
	assertActiveTaskCount(t, handler, 1)

	d.activeTasks.Add(-1)
	assertActiveTaskCount(t, handler, 0)
}

func assertActiveTaskCount(t *testing.T, h http.HandlerFunc, want int64) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ActiveTaskCount != want {
		t.Errorf("active_task_count: got %d, want %d", resp.ActiveTaskCount, want)
	}
}

// TestApplySparseCheckout verifies that applySparseCheckout correctly initialises
// cone-mode sparse-checkout and limits the working tree to the requested paths.
func TestApplySparseCheckout(t *testing.T) {
	// Set up a source repository with two top-level directories.
	srcDir := t.TempDir()
	for _, args := range [][]string{
		{"init", srcDir},
		{"-C", srcDir, "config", "user.email", "test@test.com"},
		{"-C", srcDir, "config", "user.name", "Test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Skipf("git setup: %s: %v", out, err)
		}
	}

	// Create files in two subdirectories.
	for _, rel := range []string{"pkg/a/a.go", "cmd/main.go"} {
		path := filepath.Join(srcDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("// content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitEnv := []string{
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		"HOME=" + os.Getenv("HOME"),
	}
	addCmd := exec.Command("git", "-C", srcDir, "add", ".")
	addCmd.Env = append(os.Environ(), gitEnv...)
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Skipf("git add: %s: %v", out, err)
	}
	commitCmd := exec.Command("git", "-C", srcDir, "commit", "-m", "initial")
	commitCmd.Env = append(os.Environ(), gitEnv...)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Skipf("git commit: %s: %v", out, err)
	}

	// Clone to a worktree-style checkout.
	cloneDir := t.TempDir()
	cloneCmd := exec.Command("git", "clone", srcDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Skipf("git clone: %s: %v", out, err)
	}

	// Apply sparse-checkout to include only "pkg/".
	if err := applySparseCheckout(cloneDir, []string{"pkg"}); err != nil {
		t.Fatalf("applySparseCheckout: %v", err)
	}

	// "pkg/a/a.go" must be present.
	if _, err := os.Stat(filepath.Join(cloneDir, "pkg", "a", "a.go")); err != nil {
		t.Errorf("expected pkg/a/a.go to be present: %v", err)
	}
	// "cmd/main.go" must be absent (scoped out).
	if _, err := os.Stat(filepath.Join(cloneDir, "cmd", "main.go")); err == nil {
		t.Error("expected cmd/main.go to be absent after sparse-checkout")
	}
}
