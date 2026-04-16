package openclawcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureConfigWritesNewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path, err := EnsureConfig(dir)
	if err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}

	wantPath := filepath.Join(dir, "openclaw", "openclaw.json")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse written file: %v", err)
	}

	backend := nestedMap(t, got, "agents", "defaults", "cliBackends", "multica-claude")
	if backend["command"] != "claude" {
		t.Errorf("command = %v, want \"claude\"", backend["command"])
	}
	if backend["output"] != "jsonl" {
		t.Errorf("output = %v, want \"jsonl\"", backend["output"])
	}
	if backend["jsonlDialect"] != "claude-stream-json" {
		t.Errorf("jsonlDialect = %v, want \"claude-stream-json\"", backend["jsonlDialect"])
	}
}

func TestEnsureConfigIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path1, err := EnsureConfig(dir)
	if err != nil {
		t.Fatalf("first EnsureConfig: %v", err)
	}

	stat1, err := os.Stat(path1)
	if err != nil {
		t.Fatalf("stat after first call: %v", err)
	}
	mtime1 := stat1.ModTime()

	// Sleep just enough to detect a mtime change if a write happens.
	time.Sleep(20 * time.Millisecond)

	path2, err := EnsureConfig(dir)
	if err != nil {
		t.Fatalf("second EnsureConfig: %v", err)
	}
	if path2 != path1 {
		t.Errorf("second call returned different path: %q vs %q", path2, path1)
	}

	stat2, err := os.Stat(path2)
	if err != nil {
		t.Fatalf("stat after second call: %v", err)
	}
	if !stat2.ModTime().Equal(mtime1) {
		t.Errorf("file was rewritten: mtime changed from %v to %v", mtime1, stat2.ModTime())
	}
}

func TestEnsureConfigPreservesUnrelatedKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "openclaw", "openclaw.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Pre-populate with a config the user might have authored.
	preExisting := map[string]any{
		"telemetry": map[string]any{"enabled": false},
		"agents": map[string]any{
			"defaults": map[string]any{
				"cliBackends": map[string]any{
					"my-personal-claude": map[string]any{
						"command": "claude",
						"output":  "text",
					},
				},
				"systemPrompt": "Be concise.",
			},
		},
	}
	data, _ := json.MarshalIndent(preExisting, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if _, err := EnsureConfig(dir); err != nil {
		t.Fatalf("EnsureConfig: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(written, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Multica's entry must be present.
	multica := nestedMap(t, got, "agents", "defaults", "cliBackends", "multica-claude")
	if multica["jsonlDialect"] != "claude-stream-json" {
		t.Errorf("multica-claude not configured correctly: %v", multica)
	}

	// User's entries must be untouched.
	personal := nestedMap(t, got, "agents", "defaults", "cliBackends", "my-personal-claude")
	if personal["output"] != "text" {
		t.Errorf("user backend output mutated: got %v", personal["output"])
	}
	defaults := nestedMap(t, got, "agents", "defaults")
	if defaults["systemPrompt"] != "Be concise." {
		t.Errorf("user systemPrompt lost: got %v", defaults["systemPrompt"])
	}
	telemetry := nestedMap(t, got, "telemetry")
	if telemetry["enabled"] != false {
		t.Errorf("user telemetry section lost: got %v", telemetry)
	}
}

// nestedMap walks a map[string]any tree by keys, failing the test if any step
// is missing or not a map. Test-only helper.
func nestedMap(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	cur := m
	for i, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			t.Fatalf("expected map at path %v (step %d=%q), got %T", keys, i, k, cur[k])
		}
		cur = next
	}
	return cur
}
