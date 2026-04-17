// Package openclawcfg writes and maintains the Multica-managed openclaw.json
// config used to enable claude-stream-json streaming from OpenClaw.
//
// The file is written to <profileDir>/openclaw/openclaw.json and is owned
// entirely by Multica. The user's own ~/.openclaw/openclaw.json is never
// read or modified — we redirect OpenClaw to ours via OPENCLAW_CONFIG_PATH.
package openclawcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// EnsureConfig writes <profileDir>/openclaw/openclaw.json if missing, or
// merges our managed multica-claude cliBackend entry into an existing file.
// It is idempotent: if the file already contains our entry with the expected
// settings, no write happens. Other keys / cliBackends in the file are
// preserved.
//
// Returns the absolute path to the config file (to set as OPENCLAW_CONFIG_PATH).
func EnsureConfig(profileDir string) (string, error) {
	configPath := filepath.Join(profileDir, "openclaw", "openclaw.json")

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return "", fmt.Errorf("create openclaw config dir: %w", err)
	}

	current, err := readConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("read existing config %s: %w", configPath, err)
	}

	updated, changed := mergeMulticaBackend(current)
	if !changed {
		return configPath, nil
	}

	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal openclaw config: %w", err)
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write openclaw config %s: %w", configPath, err)
	}
	return configPath, nil
}

// readConfig loads the existing config or returns an empty map if missing.
func readConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var current map[string]any
	if err := json.Unmarshal(data, &current); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	if current == nil {
		current = map[string]any{}
	}
	return current, nil
}

// multicaBackend is the cliBackend entry we manage. OpenClaw normalizes
// claude-cli backends to inject --permission-mode bypassPermissions and
// bundleMcp:true defaults; we don't override those.
func multicaBackend() map[string]any {
	return map[string]any{
		"command":      "claude",
		"output":       "jsonl",
		"jsonlDialect": "claude-stream-json",
	}
}

// mergeMulticaBackend ensures current.agents.defaults.cliBackends["multica-claude"]
// matches multicaBackend(). Returns the (possibly mutated) config and a flag
// indicating whether a write is needed.
func mergeMulticaBackend(current map[string]any) (map[string]any, bool) {
	want := multicaBackend()

	agents, _ := current["agents"].(map[string]any)
	if agents == nil {
		agents = map[string]any{}
		current["agents"] = agents
	}

	defaults, _ := agents["defaults"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
		agents["defaults"] = defaults
	}

	cliBackends, _ := defaults["cliBackends"].(map[string]any)
	if cliBackends == nil {
		cliBackends = map[string]any{}
		defaults["cliBackends"] = cliBackends
	}

	existing, _ := cliBackends["multica-claude"].(map[string]any)
	if mapsEqual(existing, want) {
		return current, false
	}
	cliBackends["multica-claude"] = want
	return current, true
}

// mapsEqual compares two map[string]any for deep equality on string/bool/number leaves.
// Sufficient for our small backend config; not a general deep-equal.
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", va) != fmt.Sprintf("%v", vb) {
			return false
		}
	}
	return true
}
