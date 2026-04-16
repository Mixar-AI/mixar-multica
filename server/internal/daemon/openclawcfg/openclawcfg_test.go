package openclawcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
