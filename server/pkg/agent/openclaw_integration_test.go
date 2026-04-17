//go:build integration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"testing"
	"time"
)

// TestOpenclawIntegrationSmoke runs a real openclaw binary end-to-end and
// asserts that at least one MessageText arrives via the stream channel and
// the session completes successfully.
//
// Run with: go test -tags integration ./pkg/agent/ -run TestOpenclawIntegration -v
//
// Skips automatically unless both `openclaw` and `claude` are on PATH.
func TestOpenclawIntegrationSmoke(t *testing.T) {
	if _, err := exec.LookPath("openclaw"); err != nil {
		t.Skip("openclaw not on PATH")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH (required by multica-claude backend)")
	}

	backend, err := New("openclaw", Config{
		Logger: slog.Default(),
		// Caller must set OPENCLAW_CONFIG_PATH externally to point at a
		// config with the multica-claude backend. We do not write it here
		// to keep the test focused on the Execute path.
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sess, err := backend.Execute(ctx, "Reply with the single word: ready", ExecOptions{Timeout: 90 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sawText bool
	for msg := range sess.Messages {
		if msg.Type == MessageText && msg.Content != "" {
			sawText = true
		}
	}
	if !sawText {
		t.Error("expected at least one MessageText during streaming")
	}

	res := <-sess.Result
	if res.Status != "completed" {
		t.Errorf("status = %q, want completed (error: %s)", res.Status, res.Error)
	}
	if res.Output == "" {
		t.Error("Result.Output is empty")
	}
}
