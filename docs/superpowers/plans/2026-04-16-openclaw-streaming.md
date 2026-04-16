# OpenClaw Live Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make OpenClaw agent execution stream live progress (text deltas, tool calls, thinking, errors) to the Multica UI in real time, matching Claude Code's behavior.

**Architecture:** Daemon writes a Multica-owned `openclaw.json` to the per-profile config dir (`~/.multica/profiles/<profile>/openclaw/openclaw.json`) defining a `multica-claude` cliBackend with `output: "jsonl"` + `jsonlDialect: "claude-stream-json"`. When spawning `openclaw`, daemon sets `OPENCLAW_CONFIG_PATH` to that file. The OpenClaw stream parser is rewritten to handle the documented `init` / `stream_event` / `result` envelope; the existing speculative parser is removed entirely.

**Tech Stack:** Go (server), JSON parsing (`encoding/json`), OpenClaw CLI as a subprocess, existing `agent.Backend` interface and unified `Message` channel.

**Spec:** [`2026-04-16-openclaw-streaming-design.md`](../specs/2026-04-16-openclaw-streaming-design.md)

**Prior art:** [phodal/routa](https://github.com/phodal/routa) — a multi-agent platform with a mature Claude Code stream-json adapter (`ClaudeCodeProcess`). Three patterns from routa are folded into this plan: ANSI escape stripping (Task 8), MCP tool name normalization (Task 11), and `signature_delta` acknowledgement (Task 12).

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `server/internal/daemon/config.go` | Modify | Detect openclaw, call `openclawcfg.EnsureConfig`, store path on `Config` |
| `server/internal/daemon/openclawcfg/openclawcfg.go` | New | Write/merge Multica-owned openclaw.json |
| `server/internal/daemon/openclawcfg/openclawcfg_test.go` | New | Unit tests for the config writer |
| `server/internal/daemon/daemon.go` | Modify | Inject `OPENCLAW_CONFIG_PATH` into per-task `agentEnv` for openclaw runs |
| `server/pkg/agent/openclaw.go` | Rewrite | New CLI invocation, stdout reader, delegates parsing to `openclaw_parser.go` |
| `server/pkg/agent/openclaw_parser.go` | New | Envelope types + `processOpenclawOutput` parser |
| `server/pkg/agent/openclaw_parser_test.go` | New | Parser unit tests (TDD-driven) |
| `server/pkg/agent/openclaw_test.go` | Rewrite | CLI-args tests + smoke contract (no parser tests; those moved to `openclaw_parser_test.go`) |
| `server/pkg/agent/openclaw_integration_test.go` | New | Build-tagged end-to-end (`//go:build integration`) |
| `CLI_AND_DAEMON.md` | Modify | Document the auto-generated config + env var |
| `.env.example` | Modify | Comment that `MULTICA_OPENCLAW_MODEL` defaults to `multica-claude/sonnet` |

---

## Task 1: Add `OpenclawConfigPath` field to daemon `Config` struct

**Files:**
- Modify: `server/internal/daemon/config.go:28-48` (Config struct)

- [ ] **Step 1: Add the field**

Edit `server/internal/daemon/config.go`. In the `Config` struct (around line 36, after `Agents`), add:

```go
	OpenclawConfigPath string                // absolute path to Multica-managed openclaw.json; empty if write failed or openclaw not detected
```

- [ ] **Step 2: Verify compilation**

```bash
cd server && go build ./internal/daemon/
```

Expected: compiles cleanly (no usage yet, just a new struct field).

- [ ] **Step 3: Commit**

```bash
git add server/internal/daemon/config.go
git commit -m "$(cat <<'EOF'
chore(daemon): add OpenclawConfigPath field to Config

Pre-work for OpenClaw live-streaming feature: gives LoadConfig a
place to record the Multica-managed openclaw.json path it writes
on detection.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: openclawcfg package — write minimal config when file is missing

**Files:**
- Create: `server/internal/daemon/openclawcfg/openclawcfg.go`
- Create: `server/internal/daemon/openclawcfg/openclawcfg_test.go`

- [ ] **Step 1: Create the package skeleton**

Create `server/internal/daemon/openclawcfg/openclawcfg.go`:

```go
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
```

- [ ] **Step 2: Write the failing test**

Create `server/internal/daemon/openclawcfg/openclawcfg_test.go`:

```go
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
```

- [ ] **Step 3: Run the test**

```bash
cd server && go test ./internal/daemon/openclawcfg/ -run TestEnsureConfigWritesNewFile -v
```

Expected: PASS — the implementation in Step 1 already satisfies it.

- [ ] **Step 4: Commit**

```bash
git add server/internal/daemon/openclawcfg/
git commit -m "$(cat <<'EOF'
feat(daemon/openclawcfg): write Multica-owned openclaw.json on first call

EnsureConfig creates <profileDir>/openclaw/openclaw.json with a
multica-claude cliBackend configured for claude-stream-json output.
This unlocks live streaming from OpenClaw without touching the user's
own ~/.openclaw/openclaw.json.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: openclawcfg — idempotent (no rewrite when up to date)

**Files:**
- Modify: `server/internal/daemon/openclawcfg/openclawcfg_test.go` (add test)

- [ ] **Step 1: Write the failing test**

Append to `server/internal/daemon/openclawcfg/openclawcfg_test.go`:

```go
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
```

Update the import block at the top of the file to include `"time"`:

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run the test**

```bash
cd server && go test ./internal/daemon/openclawcfg/ -run TestEnsureConfigIdempotent -v
```

Expected: PASS — `mergeMulticaBackend` returns `changed=false` when the existing entry already matches.

- [ ] **Step 3: Commit**

```bash
git add server/internal/daemon/openclawcfg/openclawcfg_test.go
git commit -m "$(cat <<'EOF'
test(daemon/openclawcfg): assert EnsureConfig is idempotent

Repeated calls with no config change must not rewrite the file
(verified via mtime). Prevents needless I/O on daemon startup.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: openclawcfg — preserve unrelated keys in user-edited file

**Files:**
- Modify: `server/internal/daemon/openclawcfg/openclawcfg_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/internal/daemon/openclawcfg/openclawcfg_test.go`:

```go
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
```

- [ ] **Step 2: Run the test**

```bash
cd server && go test ./internal/daemon/openclawcfg/ -run TestEnsureConfigPreservesUnrelatedKeys -v
```

Expected: PASS — `mergeMulticaBackend` only mutates the `multica-claude` key under `agents.defaults.cliBackends`, leaving siblings and parent-sibling keys intact.

- [ ] **Step 3: Commit**

```bash
git add server/internal/daemon/openclawcfg/openclawcfg_test.go
git commit -m "$(cat <<'EOF'
test(daemon/openclawcfg): preserve unrelated keys when merging

Verifies that a user-authored openclaw.json with sibling cliBackends,
sibling defaults keys, and top-level keys (telemetry) is preserved
when EnsureConfig adds the multica-claude entry.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: openclawcfg — error path for permission denied

**Files:**
- Modify: `server/internal/daemon/openclawcfg/openclawcfg_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/internal/daemon/openclawcfg/openclawcfg_test.go`:

```go
func TestEnsureConfigErrorOnReadOnlyDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod-based permission denial doesn't apply")
	}
	t.Parallel()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		// Restore so t.TempDir cleanup can remove it.
		_ = os.Chmod(dir, 0o700)
	})

	_, err := EnsureConfig(dir)
	if err == nil {
		t.Fatal("expected error on read-only dir, got nil")
	}
	if !strings.Contains(err.Error(), "openclaw") {
		t.Errorf("error should mention openclaw context, got: %v", err)
	}
}
```

Update the import block to include `"strings"`:

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run the test**

```bash
cd server && go test ./internal/daemon/openclawcfg/ -run TestEnsureConfigErrorOnReadOnlyDir -v
```

Expected: PASS — `os.MkdirAll` fails with permission denied; we wrap it with the `"create openclaw config dir"` prefix.

- [ ] **Step 3: Run the full openclawcfg test suite**

```bash
cd server && go test ./internal/daemon/openclawcfg/ -v
```

Expected: all 4 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/internal/daemon/openclawcfg/openclawcfg_test.go
git commit -m "$(cat <<'EOF'
test(daemon/openclawcfg): assert error wrapping on permission denied

Confirms EnsureConfig surfaces filesystem errors with enough context
for the daemon to log them and fall back gracefully (registering the
runtime without OPENCLAW_CONFIG_PATH).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Wire openclawcfg into LoadConfig

**Files:**
- Modify: `server/internal/daemon/config.go:102-108` (openclaw detection block)

- [ ] **Step 1: Add the import**

In `server/internal/daemon/config.go`, add to the import block:

```go
import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/openclawcfg"
)
```

(Verify the module path matches the existing imports in `daemon.go` — adjust if different.)

- [ ] **Step 2: Modify the openclaw detection branch**

Replace lines 102-108 in `server/internal/daemon/config.go`:

```go
	openclawPath := envOrDefault("MULTICA_OPENCLAW_PATH", "openclaw")
	if _, err := exec.LookPath(openclawPath); err == nil {
		agents["openclaw"] = AgentEntry{
			Path:  openclawPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_OPENCLAW_MODEL")),
		}
	}
```

with:

```go
	var openclawConfigPath string
	openclawPath := envOrDefault("MULTICA_OPENCLAW_PATH", "openclaw")
	if _, err := exec.LookPath(openclawPath); err == nil {
		agents["openclaw"] = AgentEntry{
			Path:  openclawPath,
			Model: strings.TrimSpace(os.Getenv("MULTICA_OPENCLAW_MODEL")),
		}
		profileDir, perr := cli.ProfileDir(profile)
		if perr != nil {
			slog.Default().Warn("openclaw: cannot resolve profile dir; live streaming disabled", "error", perr)
		} else if cfgPath, cerr := openclawcfg.EnsureConfig(profileDir); cerr != nil {
			slog.Default().Warn("openclaw: config write failed; live streaming disabled", "error", cerr)
		} else {
			openclawConfigPath = cfgPath
		}
	}
```

(Note: the existing `profile` variable is set further down at line 188; we reference it here. Move the `profile := overrides.Profile` line to before the agent detection block, around line 79.)

- [ ] **Step 3: Move `profile` earlier in LoadConfig**

In `server/internal/daemon/config.go`, find the existing line:

```go
	// Profile
	profile := overrides.Profile
```

(currently around line 187-188). Cut it and paste it just before the `// Probe available agent CLIs` comment (around line 79):

```go
	profile := overrides.Profile

	// Probe available agent CLIs
	agents := map[string]AgentEntry{}
```

- [ ] **Step 4: Set `OpenclawConfigPath` in the returned Config**

In `server/internal/daemon/config.go`, find the `return Config{...}` block at the bottom (around line 258). Add the field assignment:

```go
	return Config{
		ServerBaseURL:      serverBaseURL,
		DaemonID:           daemonID,
		DeviceName:         deviceName,
		RuntimeName:        runtimeName,
		Profile:            profile,
		Agents:             agents,
		OpenclawConfigPath: openclawConfigPath,
		WorkspacesRoot:     workspacesRoot,
		// ... rest unchanged
	}, nil
```

- [ ] **Step 5: Verify compilation**

```bash
cd server && go build ./...
```

Expected: clean build.

- [ ] **Step 6: Run the daemon package tests**

```bash
cd server && go test ./internal/daemon/...
```

Expected: existing tests still pass; openclawcfg tests still pass.

- [ ] **Step 7: Commit**

```bash
git add server/internal/daemon/config.go
git commit -m "$(cat <<'EOF'
feat(daemon): write openclaw.json on detection, expose path in Config

When openclaw is found on PATH, LoadConfig now calls
openclawcfg.EnsureConfig and stores the resulting file path on the
Config struct. Failures are logged but non-fatal — the runtime still
registers, just without live streaming.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Inject `OPENCLAW_CONFIG_PATH` into per-task agentEnv

**Files:**
- Modify: `server/internal/daemon/daemon.go:993-1014` (per-task env block)

- [ ] **Step 1: Add the injection**

In `server/internal/daemon/daemon.go`, find the env-injection block around line 1010-1014:

```go
	// Point Codex to the per-task CODEX_HOME so it discovers skills natively
	// without polluting the system ~/.codex/skills/.
	if env.CodexHome != "" {
		agentEnv["CODEX_HOME"] = env.CodexHome
	}
```

Add immediately after it:

```go
	// Point OpenClaw at the Multica-managed config so it streams in
	// claude-stream-json dialect. If EnsureConfig failed at startup,
	// OpenclawConfigPath is empty and we leave the env var unset —
	// OpenClaw falls back to the user's own config (no streaming).
	if provider == "openclaw" && d.cfg.OpenclawConfigPath != "" {
		agentEnv["OPENCLAW_CONFIG_PATH"] = d.cfg.OpenclawConfigPath
	}
```

- [ ] **Step 2: Verify compilation**

```bash
cd server && go build ./...
```

Expected: clean build.

- [ ] **Step 3: Run daemon tests**

```bash
cd server && go test ./internal/daemon/...
```

Expected: PASS (no test directly covers this branch yet; integration test in Task 17 will).

- [ ] **Step 4: Commit**

```bash
git add server/internal/daemon/daemon.go
git commit -m "$(cat <<'EOF'
feat(daemon): inject OPENCLAW_CONFIG_PATH into openclaw task env

When dispatching a task to openclaw, the daemon now points the CLI
at its Multica-managed config so the underlying claude-cli backend
emits the streaming JSONL envelope.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Create `openclaw_parser.go` with envelope types and dispatcher skeleton

**Files:**
- Create: `server/pkg/agent/openclaw_parser.go`

- [ ] **Step 1: Write the file**

Create `server/pkg/agent/openclaw_parser.go`:

```go
package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

// ansiEscapeRE matches ANSI escape sequences (CSI, OSC, and bare ESC forms).
// Some claude-cli builds emit colored progress text to stdout when not TTY-
// detected, which can sneak into stream lines and break JSON parsing.
// Pattern adapted from routa's clearAnsi helper.
var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-_]`)

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

// processOpenclawOutput reads claude-stream-json envelope events from r and
// emits unified Message events on ch. Returns the final result state.
//
// Envelope shape (top-level "type"):
//   - "init"          → captures session_id
//   - "stream_event"  → drills into event.type for content_block_*, message_*
//   - "result"        → terminal: final result text, is_error, total usage
//
// Unknown top-level types are logged at debug and skipped (forward-compat).
// Malformed JSON lines are skipped (next line still parses).
// ANSI escape codes are stripped before parsing.
func processOpenclawOutput(r io.Reader, ch chan<- Message, logger *slog.Logger) openclawResultState {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	state := newOpenclawResultState()

	for scanner.Scan() {
		line := strings.TrimSpace(stripANSI(scanner.Text()))
		if line == "" || line[0] != '{' {
			continue
		}
		var env openclawEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			logger.Debug("openclaw: malformed envelope line", "error", err)
			continue
		}
		switch env.Type {
		case "init":
			state.handleInit(env)
		case "stream_event":
			state.handleStreamEvent(env, ch, logger)
		case "result":
			state.handleResult(env)
		default:
			logger.Debug("openclaw: unknown envelope type", "type", env.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		state.fail(fmt.Sprintf("read openclaw stdout: %v", err))
	}
	return state
}

// normalizeMCPToolName strips the `mcp__<server>__` prefix from MCP-bundled
// tool names so the UI displays e.g. `create_task` instead of
// `mcp__routa-coordination__create_task`. Non-MCP names are returned as-is.
// Pattern adapted from routa's mapClaudeToolName.
func normalizeMCPToolName(name string) string {
	if !strings.HasPrefix(name, "mcp__") {
		return name
	}
	rest := strings.TrimPrefix(name, "mcp__")
	parts := strings.SplitN(rest, "__", 2)
	if len(parts) != 2 {
		return name
	}
	return parts[1]
}

// openclawEnvelope is the top-level wrapper for every line in the stream.
type openclawEnvelope struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"` // init
	Event     json.RawMessage `json:"event,omitempty"`      // stream_event
	Result    string          `json:"result,omitempty"`     // result
	IsError   bool            `json:"is_error,omitempty"`   // result
	Usage     *openclawUsage  `json:"usage,omitempty"`      // result
}

// openclawStreamEvent is the inner payload of a stream_event envelope.
type openclawStreamEvent struct {
	Type         string                `json:"type"`
	Index        int                   `json:"index,omitempty"`
	ContentBlock *openclawContentBlock `json:"content_block,omitempty"`
	Delta        *openclawDelta        `json:"delta,omitempty"`
	Usage        *openclawUsage        `json:"usage,omitempty"` // message_delta variant
}

// openclawContentBlock is the per-block descriptor on content_block_start.
type openclawContentBlock struct {
	Type string `json:"type"` // "text" | "tool_use" | "thinking"
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"` // tool name
}

// openclawDelta is the incremental update on content_block_delta / message_delta.
type openclawDelta struct {
	Type        string         `json:"type"` // "text_delta" | "input_json_delta" | "thinking_delta"
	Text        string         `json:"text,omitempty"`
	PartialJSON string         `json:"partial_json,omitempty"`
	Thinking    string         `json:"thinking,omitempty"`
	Usage       *openclawUsage `json:"usage,omitempty"` // message_delta variant
}

// openclawUsage matches the snake_case Anthropic-style usage fields used by
// the claude-stream-json dialect.
type openclawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// openclawResultState accumulates parser output across the stream.
type openclawResultState struct {
	status      string
	errMsg      string
	output      strings.Builder
	sessionID   string
	usage       TokenUsage
	openBlocks  map[int]*openclawOpenBlock // keyed by event.index
}

// openclawOpenBlock tracks an in-progress content_block (for tool_use buffering).
type openclawOpenBlock struct {
	kind     string // "tool_use" | "thinking" | "text"
	tool     string
	callID   string
	inputBuf strings.Builder
}

func newOpenclawResultState() openclawResultState {
	return openclawResultState{
		status:     "completed",
		openBlocks: map[int]*openclawOpenBlock{},
	}
}

func (s *openclawResultState) fail(msg string) {
	s.status = "failed"
	s.errMsg = msg
}

func (s *openclawResultState) addUsage(u *openclawUsage) {
	if u == nil {
		return
	}
	s.usage.InputTokens += u.InputTokens
	s.usage.OutputTokens += u.OutputTokens
	s.usage.CacheReadTokens += u.CacheReadInputTokens
	s.usage.CacheWriteTokens += u.CacheCreationInputTokens
}

// handleInit captures session_id for resume support.
func (s *openclawResultState) handleInit(env openclawEnvelope) {
	if env.SessionID != "" {
		s.sessionID = env.SessionID
	}
}

// handleResult is invoked on the terminal `result` envelope.
func (s *openclawResultState) handleResult(env openclawEnvelope) {
	if env.Result != "" {
		s.output.Reset()
		s.output.WriteString(env.Result)
	}
	if env.IsError {
		s.status = "failed"
		if env.Result != "" {
			s.errMsg = env.Result
		} else if s.errMsg == "" {
			s.errMsg = "openclaw reported error with no message"
		}
	}
	s.addUsage(env.Usage)
}

// handleStreamEvent is implemented across Tasks 10–14.
func (s *openclawResultState) handleStreamEvent(env openclawEnvelope, ch chan<- Message, logger *slog.Logger) {
	var ev openclawStreamEvent
	if err := json.Unmarshal(env.Event, &ev); err != nil {
		logger.Debug("openclaw: malformed stream_event", "error", err)
		return
	}
	switch ev.Type {
	// content_block_start, content_block_delta, content_block_stop, message_delta
	// implemented in subsequent tasks.
	default:
		logger.Debug("openclaw: unknown stream_event type", "type", ev.Type)
	}
}
```

- [ ] **Step 2: Add test for ANSI stripping helper**

Append to (or create) `server/pkg/agent/openclaw_parser_test.go`:

```go
package agent

import (
	"testing"
)

func TestStripANSIRemovesEscapeSequences(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"\x1b[31mred\x1b[0m text", "red text"},
		{"\x1b[1;33mbold yellow\x1b[0m", "bold yellow"},
		{"\x1b[K", ""},
		{"prefix\x1b[2J{\"type\":\"init\"}", "prefix{\"type\":\"init\"}"},
	}
	for _, c := range cases {
		got := stripANSI(c.in)
		if got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeMCPToolNameStripsPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"bash", "bash"},
		{"mcp__routa-coordination__create_task", "create_task"},
		{"mcp__server__nested__tool_name", "nested__tool_name"},
		{"mcp__bad", "mcp__bad"}, // no second __ — leave alone
		{"", ""},
	}
	for _, c := range cases {
		got := normalizeMCPToolName(c.in)
		if got != c.want {
			t.Errorf("normalizeMCPToolName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Verify compilation and tests**

```bash
cd server && go test ./pkg/agent/ -run "TestStripANSI|TestNormalizeMCPToolName" -v
```

Expected: both tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/agent/openclaw_parser.go server/pkg/agent/openclaw_parser_test.go
git commit -m "$(cat <<'EOF'
feat(agent/openclaw): scaffold claude-stream-json parser

Adds envelope/stream_event/delta/usage types and an empty dispatcher
for processOpenclawOutput. Init and result handlers are wired;
content_block_* and message_delta land in subsequent commits.

Includes routa-inspired robustness helpers (stripANSI for terminal
output leakage, normalizeMCPToolName for clean UI tool names) ready
for use in the streaming branches.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Parser — init envelope (TDD)

**Files:**
- Modify: `server/pkg/agent/openclaw_parser_test.go` (extends the file created in Task 8)

- [ ] **Step 1: Write the failing test**

Append to `server/pkg/agent/openclaw_parser_test.go`. Update the import block at the top to include `log/slog` and `strings`:

```go
import (
	"log/slog"
	"strings"
	"testing"
)
```

Then append the test:

```go
func TestOpenclawParserInitCapturesSessionID(t *testing.T) {
	t.Parallel()

	ch := make(chan Message, 16)
	input := `{"type":"init","session_id":"sess_abc123"}` + "\n"

	state := processOpenclawOutput(strings.NewReader(input), ch, slog.Default())

	if state.sessionID != "sess_abc123" {
		t.Errorf("sessionID = %q, want %q", state.sessionID, "sess_abc123")
	}
	close(ch)
	if msgs := drainMessages(ch); len(msgs) != 0 {
		t.Errorf("expected 0 messages from init alone, got %d", len(msgs))
	}
}

// drainMessages collects all remaining messages from a closed channel.
// Test-only helper used by openclaw_parser_test.go cases.
func drainMessages(ch <-chan Message) []Message {
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	return msgs
}
```

- [ ] **Step 2: Run the test**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserInitCapturesSessionID -v
```

Expected: PASS — `handleInit` is already implemented in Task 8.

- [ ] **Step 3: Commit**

```bash
git add server/pkg/agent/openclaw_parser_test.go
git commit -m "$(cat <<'EOF'
test(agent/openclaw): assert init envelope captures session_id

First test for the new parser. Verifies session_id propagates to
the result state without emitting any user-visible messages.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Parser — `text_delta` streaming (TDD)

**Files:**
- Modify: `server/pkg/agent/openclaw_parser_test.go` (add test)
- Modify: `server/pkg/agent/openclaw_parser.go` (extend `handleStreamEvent`)

- [ ] **Step 1: Write the failing test**

Append to `server/pkg/agent/openclaw_parser_test.go`:

```go
func TestOpenclawParserTextDeltaStreams(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"init","session_id":"s1"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	state := processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	if state.output.String() != "Hello world" {
		t.Errorf("accumulated output = %q, want %q", state.output.String(), "Hello world")
	}
	msgs := drainMessages(ch)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(msgs))
	}
	if msgs[0].Type != MessageText || msgs[0].Content != "Hello " {
		t.Errorf("msg[0] = %+v, want text \"Hello \"", msgs[0])
	}
	if msgs[1].Type != MessageText || msgs[1].Content != "world" {
		t.Errorf("msg[1] = %+v, want text \"world\"", msgs[1])
	}
}
```

- [ ] **Step 2: Run the test (expect failure)**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserTextDeltaStreams -v
```

Expected: FAIL — current `handleStreamEvent` only logs unknown event types.

- [ ] **Step 3: Implement `content_block_delta` for text_delta**

In `server/pkg/agent/openclaw_parser.go`, replace the `handleStreamEvent` switch with:

```go
	switch ev.Type {
	case "content_block_start":
		s.handleContentBlockStart(ev)
	case "content_block_delta":
		s.handleContentBlockDelta(ev, ch)
	case "content_block_stop":
		s.handleContentBlockStop(ev, ch)
	case "message_delta":
		s.addUsage(ev.Usage)
		if ev.Delta != nil {
			s.addUsage(ev.Delta.Usage)
		}
	case "message_stop":
		// terminal marker for an assistant message; nothing to emit.
	default:
		logger.Debug("openclaw: unknown stream_event type", "type", ev.Type)
	}
```

Then add the three handler methods at the bottom of the file:

```go
// handleContentBlockStart records open-block bookkeeping for tool_use and
// thinking blocks. text blocks are emitted directly by the delta handler
// and don't need state.
func (s *openclawResultState) handleContentBlockStart(ev openclawStreamEvent) {
	if ev.ContentBlock == nil {
		return
	}
	switch ev.ContentBlock.Type {
	case "tool_use":
		s.openBlocks[ev.Index] = &openclawOpenBlock{
			kind:   "tool_use",
			tool:   ev.ContentBlock.Name,
			callID: ev.ContentBlock.ID,
		}
	case "thinking":
		s.openBlocks[ev.Index] = &openclawOpenBlock{kind: "thinking"}
	}
}

// handleContentBlockDelta routes per-block deltas to the right sink.
func (s *openclawResultState) handleContentBlockDelta(ev openclawStreamEvent, ch chan<- Message) {
	if ev.Delta == nil {
		return
	}
	switch ev.Delta.Type {
	case "text_delta":
		if ev.Delta.Text != "" {
			s.output.WriteString(ev.Delta.Text)
			trySend(ch, Message{Type: MessageText, Content: ev.Delta.Text})
		}
	case "input_json_delta":
		blk, ok := s.openBlocks[ev.Index]
		if !ok || blk.kind != "tool_use" {
			return
		}
		blk.inputBuf.WriteString(ev.Delta.PartialJSON)
	case "thinking_delta":
		if ev.Delta.Thinking != "" {
			trySend(ch, Message{Type: MessageThinking, Content: ev.Delta.Thinking})
		}
	}
}

// handleContentBlockStop flushes a buffered tool_use input as MessageToolUse.
// Other block types have already emitted everything via deltas.
func (s *openclawResultState) handleContentBlockStop(ev openclawStreamEvent, ch chan<- Message) {
	blk, ok := s.openBlocks[ev.Index]
	if !ok {
		return
	}
	defer delete(s.openBlocks, ev.Index)
	if blk.kind != "tool_use" {
		return
	}
	input := map[string]any{}
	if buf := blk.inputBuf.String(); buf != "" {
		if err := json.Unmarshal([]byte(buf), &input); err != nil {
			// Surface the call anyway so the user sees something happened.
			input = map[string]any{"_raw": buf}
		}
	}
	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   normalizeMCPToolName(blk.tool),
		CallID: blk.callID,
		Input:  input,
	})
}
```

- [ ] **Step 4: Run the test**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserTextDeltaStreams -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/agent/openclaw_parser.go server/pkg/agent/openclaw_parser_test.go
git commit -m "$(cat <<'EOF'
feat(agent/openclaw): emit MessageText for text_delta stream events

Implements the content_block_start/delta/stop dispatch and the
text_delta path. Tool_use and thinking handlers are also added but
covered in subsequent tests.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Parser — `tool_use` block buffering + MCP name normalization (TDD)

**Files:**
- Modify: `server/pkg/agent/openclaw_parser_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/pkg/agent/openclaw_parser_test.go`:

```go
func TestOpenclawParserToolUseBuffersInputAcrossDeltas(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_abc","name":"bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"comm"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"and\":\"ls -la\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	msgs := drainMessages(ch)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 tool_use message, got %d (%+v)", len(msgs), msgs)
	}
	m := msgs[0]
	if m.Type != MessageToolUse {
		t.Errorf("type = %s, want %s", m.Type, MessageToolUse)
	}
	if m.Tool != "bash" {
		t.Errorf("tool = %q, want %q", m.Tool, "bash")
	}
	if m.CallID != "call_abc" {
		t.Errorf("callID = %q, want %q", m.CallID, "call_abc")
	}
	if m.Input["command"] != "ls -la" {
		t.Errorf("input.command = %v, want %q", m.Input["command"], "ls -la")
	}
}

func TestOpenclawParserToolUseWithMalformedInputUsesRaw(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"weird"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"not json at all"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	msgs := drainMessages(ch)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Input["_raw"] != "not json at all" {
		t.Errorf("expected _raw fallback, got %+v", msgs[0].Input)
	}
}

func TestOpenclawParserToolUseStripsMCPPrefix(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_x","name":"mcp__multica-coordination__create_issue"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"title\":\"Bug\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	msgs := drainMessages(ch)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 tool_use message, got %d", len(msgs))
	}
	if msgs[0].Tool != "create_issue" {
		t.Errorf("tool = %q, want %q (MCP prefix should be stripped)", msgs[0].Tool, "create_issue")
	}
}
```

- [ ] **Step 2: Run the tests**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserToolUse -v
```

Expected: PASS — implementation in Task 10 already covers buffering, malformed-input fallback, and MCP name stripping (via `normalizeMCPToolName` on emit).

- [ ] **Step 3: Commit**

```bash
git add server/pkg/agent/openclaw_parser_test.go
git commit -m "$(cat <<'EOF'
test(agent/openclaw): tool_use buffering + MCP name normalization

Three cases: (a) input_json_delta fragments concatenate and parse to
a single MessageToolUse with structured input; (b) malformed JSON
falls back to {_raw: "..."} so the call still surfaces in the UI;
(c) tool names with mcp__<server>__ prefix are stripped to keep the
UI readable (pattern borrowed from routa's mapClaudeToolName).

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Parser — `thinking_delta` and `signature_delta` (TDD)

**Files:**
- Modify: `server/pkg/agent/openclaw_parser_test.go`
- Modify: `server/pkg/agent/openclaw_parser.go` (acknowledge `signature_delta`)

- [ ] **Step 1: Write the failing tests**

Append to `server/pkg/agent/openclaw_parser_test.go`:

```go
func TestOpenclawParserThinkingDeltaEmits(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Considering the trade-offs..."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_abc"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	msgs := drainMessages(ch)
	// signature_delta is acknowledged but not surfaced as a Message.
	if len(msgs) != 1 {
		t.Fatalf("expected 1 thinking message (signature_delta is silent), got %d", len(msgs))
	}
	if msgs[0].Type != MessageThinking {
		t.Errorf("type = %s, want %s", msgs[0].Type, MessageThinking)
	}
	if msgs[0].Content != "Considering the trade-offs..." {
		t.Errorf("content = %q", msgs[0].Content)
	}
}
```

- [ ] **Step 2: Run the test (expect it to pass for thinking, may emit a debug log for signature_delta)**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserThinkingDeltaEmits -v
```

Expected: PASS — Task 10 wired the thinking_delta branch. `signature_delta` falls through the existing delta switch and is silently ignored (no message emitted), which matches the test expectation.

- [ ] **Step 3: Acknowledge `signature_delta` explicitly to avoid debug-log noise**

In `server/pkg/agent/openclaw_parser.go`, find `handleContentBlockDelta` and add a `signature_delta` case in the switch:

```go
func (s *openclawResultState) handleContentBlockDelta(ev openclawStreamEvent, ch chan<- Message) {
	if ev.Delta == nil {
		return
	}
	switch ev.Delta.Type {
	case "text_delta":
		if ev.Delta.Text != "" {
			s.output.WriteString(ev.Delta.Text)
			trySend(ch, Message{Type: MessageText, Content: ev.Delta.Text})
		}
	case "input_json_delta":
		blk, ok := s.openBlocks[ev.Index]
		if !ok || blk.kind != "tool_use" {
			return
		}
		blk.inputBuf.WriteString(ev.Delta.PartialJSON)
	case "thinking_delta":
		if ev.Delta.Thinking != "" {
			trySend(ch, Message{Type: MessageThinking, Content: ev.Delta.Thinking})
		}
	case "signature_delta":
		// Extended-thinking signature verification — acknowledged but not surfaced.
		// Routa pattern: stored alongside thinking text; we don't expose signatures
		// in the UI so we no-op silently.
	}
}
```

- [ ] **Step 4: Run the test again**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserThinkingDeltaEmits -v
```

Expected: PASS, no debug logs about unknown delta types.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/agent/openclaw_parser_test.go server/pkg/agent/openclaw_parser.go
git commit -m "$(cat <<'EOF'
feat(agent/openclaw): handle thinking_delta and signature_delta

Extended-thinking blocks surface as MessageThinking so the UI can
render them distinctly from assistant text. signature_delta events
(extended-thinking verification, per Claude SDK) are explicitly
acknowledged as no-ops to avoid debug-log noise on every thinking
session — pattern borrowed from routa's ClaudeCodeProcess.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Parser — `result` envelope (TDD)

**Files:**
- Modify: `server/pkg/agent/openclaw_parser_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/pkg/agent/openclaw_parser_test.go`:

```go
func TestOpenclawParserResultEnvelopeReplacesOutputAndCapturesUsage(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial..."}}}`,
		`{"type":"result","result":"final answer text","is_error":false,"usage":{"input_tokens":120,"output_tokens":45,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	state := processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	if state.status != "completed" {
		t.Errorf("status = %q, want completed", state.status)
	}
	if state.output.String() != "final answer text" {
		t.Errorf("output = %q, want %q", state.output.String(), "final answer text")
	}
	if state.usage.InputTokens != 120 {
		t.Errorf("input tokens = %d, want 120", state.usage.InputTokens)
	}
	if state.usage.OutputTokens != 45 {
		t.Errorf("output tokens = %d, want 45", state.usage.OutputTokens)
	}
	if state.usage.CacheReadTokens != 10 {
		t.Errorf("cache read = %d, want 10", state.usage.CacheReadTokens)
	}
	if state.usage.CacheWriteTokens != 5 {
		t.Errorf("cache write = %d, want 5", state.usage.CacheWriteTokens)
	}
}

func TestOpenclawParserResultIsErrorMarksFailed(t *testing.T) {
	t.Parallel()

	input := `{"type":"result","result":"model not found: gpt-99","is_error":true}` + "\n"

	ch := make(chan Message, 16)
	state := processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	if state.status != "failed" {
		t.Errorf("status = %q, want failed", state.status)
	}
	if state.errMsg != "model not found: gpt-99" {
		t.Errorf("errMsg = %q", state.errMsg)
	}
}
```

- [ ] **Step 2: Run the tests**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserResult -v
```

Expected: PASS — `handleResult` from Task 8 already covers both behaviors.

- [ ] **Step 3: Commit**

```bash
git add server/pkg/agent/openclaw_parser_test.go
git commit -m "$(cat <<'EOF'
test(agent/openclaw): assert result envelope finalizes output and usage

Two cases: (a) successful result replaces accumulated text and stamps
usage; (b) is_error:true flips status to failed with the result body
as the error message.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Parser — robustness (unknown event, malformed JSON) (TDD)

**Files:**
- Modify: `server/pkg/agent/openclaw_parser_test.go`

- [ ] **Step 1: Write the failing test**

Append to `server/pkg/agent/openclaw_parser_test.go`:

```go
func TestOpenclawParserSkipsUnknownEnvelopeAndMalformedLines(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"future_event","payload":{"anything":true}}`,
		`this is not json`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}}`,
		`{"type":"stream_event","event":{"type":"future_inner_event"}}`,
		`{"type":"result","result":"done"}`,
	}
	input := strings.Join(lines, "\n") + "\n"

	ch := make(chan Message, 16)
	state := processOpenclawOutput(strings.NewReader(input), ch, slog.Default())
	close(ch)

	if state.status != "completed" {
		t.Errorf("status = %q, want completed", state.status)
	}
	msgs := drainMessages(ch)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 text message (the only valid stream_event), got %d (%+v)", len(msgs), msgs)
	}
	if msgs[0].Type != MessageText || msgs[0].Content != "hello" {
		t.Errorf("msg[0] = %+v", msgs[0])
	}
}
```

- [ ] **Step 2: Run the test**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParserSkipsUnknown -v
```

Expected: PASS — the dispatcher logs and skips unknowns; the json.Unmarshal error path skips malformed lines.

- [ ] **Step 3: Run the full parser suite**

```bash
cd server && go test ./pkg/agent/ -run TestOpenclawParser -v
```

Expected: all parser tests PASS.

- [ ] **Step 4: Commit**

```bash
git add server/pkg/agent/openclaw_parser_test.go
git commit -m "$(cat <<'EOF'
test(agent/openclaw): assert parser skips unknown types and bad JSON

Forward-compatibility check: a future OpenClaw release adding new
envelope or stream_event types must not break Multica. Same for
non-JSON log lines that may sneak into stdout.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Rewrite `openclaw.go` — new CLI invocation, stdout reader, delegate to parser

**Files:**
- Replace: `server/pkg/agent/openclaw.go` (delete legacy types/parser, add new Execute body)

- [ ] **Step 1: Replace the entire file**

Overwrite `server/pkg/agent/openclaw.go` with:

```go
package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// openclawBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. Overriding these would break
// the daemon↔OpenClaw communication protocol.
var openclawBlockedArgs = map[string]blockedArgMode{
	"--local":      blockedStandalone, // forces non-interactive embedded agent
	"--json":       blockedStandalone, // enables claude-stream-json on stdout
	"--session-id": blockedWithValue,  // managed by daemon for session resumption
	"--message":    blockedWithValue,  // prompt is set by daemon
	"--model":      blockedWithValue,  // backend/model selection is set by daemon
}

// openclawBackend implements Backend by spawning `openclaw agent --local --json`
// with a Multica-managed config (set via OPENCLAW_CONFIG_PATH in daemon env)
// and parsing the resulting claude-stream-json envelope from stdout.
type openclawBackend struct {
	cfg Config
}

func (b *openclawBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "openclaw"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("openclaw executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("multica-%d", time.Now().UnixNano())
	}

	args := []string{
		"agent",
		"--local",
		"--json",
		"--session-id", sessionID,
		"--model", openclawModelString(opts.Model),
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, openclawBlockedArgs, b.cfg.Logger)...)
	args = append(args, "--message", prompt)

	cmd := exec.CommandContext(runCtx, execPath, args...)
	b.cfg.Logger.Debug("agent command", "exec", execPath, "args", args)
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("openclaw stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[openclaw:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start openclaw: %w", err)
	}

	b.cfg.Logger.Info("openclaw started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// Close stdout when context is cancelled so the parser unblocks.
	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		state := processOpenclawOutput(stdout, msgCh, b.cfg.Logger)
		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			state.status = "timeout"
			state.errMsg = fmt.Sprintf("openclaw timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			state.status = "aborted"
			state.errMsg = "execution cancelled"
		} else if exitErr != nil && state.status == "completed" {
			state.status = "failed"
			state.errMsg = fmt.Sprintf("openclaw exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("openclaw finished",
			"pid", cmd.Process.Pid,
			"status", state.status,
			"duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := state.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "multica-claude/sonnet"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     state.status,
			Output:     state.output.String(),
			Error:      state.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  state.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// openclawModelString resolves the daemon's MULTICA_OPENCLAW_MODEL value into
// the <backend-id>/<model-name> format OpenClaw expects.
//
//	""              → "multica-claude/sonnet"          (zero-config default)
//	"opus"          → "multica-claude/opus"            (model alias under default backend)
//	"claude-cli/4"  → "claude-cli/4"                   (operator passed full backend/model)
func openclawModelString(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "multica-claude/sonnet"
	}
	if strings.Contains(model, "/") {
		return model
	}
	return "multica-claude/" + model
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd server && go build ./pkg/agent/
```

Expected: FAIL — `openclaw_test.go` (still has the old tests) references deleted types like `openclawResult`, `openclawPayload`, etc.

This is intentional — Task 16 cleans up the test file.

- [ ] **Step 3: Stage but do not commit yet**

```bash
git add server/pkg/agent/openclaw.go
```

Don't commit until Task 16 lands so the repo stays buildable.

---

## Task 16: Replace `openclaw_test.go` with new CLI-args tests

**Files:**
- Replace: `server/pkg/agent/openclaw_test.go`

- [ ] **Step 1: Overwrite the test file**

Overwrite `server/pkg/agent/openclaw_test.go` with:

```go
package agent

import (
	"testing"
)

func TestNewReturnsOpenclawBackend(t *testing.T) {
	t.Parallel()
	b, err := New("openclaw", Config{ExecutablePath: "/nonexistent/openclaw"})
	if err != nil {
		t.Fatalf("New(openclaw) error: %v", err)
	}
	if _, ok := b.(*openclawBackend); !ok {
		t.Fatalf("expected *openclawBackend, got %T", b)
	}
}

func TestOpenclawModelStringDefaults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"", "multica-claude/sonnet"},
		{"  ", "multica-claude/sonnet"},
		{"opus", "multica-claude/opus"},
		{"claude-cli/4", "claude-cli/4"},
		{"my-backend/custom-alias", "my-backend/custom-alias"},
	}
	for _, c := range cases {
		got := openclawModelString(c.in)
		if got != c.want {
			t.Errorf("openclawModelString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Verify build + tests pass**

```bash
cd server && go test ./pkg/agent/ -v
```

Expected: all tests PASS (parser tests + CLI args tests + every other agent backend test, untouched).

- [ ] **Step 3: Commit the rewrite as one cohesive change**

Stage the test rewrite alongside the already-staged `openclaw.go`:

```bash
git add server/pkg/agent/openclaw_test.go
git commit -m "$(cat <<'EOF'
refactor(agent/openclaw): rewrite for claude-stream-json envelope

Replaces the speculative NDJSON parser (which never matched a real
OpenClaw release) with one that handles the documented init /
stream_event / result envelope. Reads stdout instead of stderr now
that --json enables stdout streaming. Model resolution defaults to
multica-claude/sonnet, paired with the daemon-managed openclaw.json.

Drops 700+ lines of code and tests for event types OpenClaw doesn't
emit. Live progress (text deltas, tool calls, thinking) now reaches
the UI in real time, matching Claude Code's behavior.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: Add build-tagged integration test

**Files:**
- Create: `server/pkg/agent/openclaw_integration_test.go`

- [ ] **Step 1: Write the test**

Create `server/pkg/agent/openclaw_integration_test.go`:

```go
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
```

- [ ] **Step 2: Verify it compiles under the integration tag**

```bash
cd server && go test -tags integration -run TestOpenclawIntegrationSmoke -v ./pkg/agent/ 2>&1 | head -20
```

Expected: either SKIP (if openclaw isn't installed) or PASS (if it is and OPENCLAW_CONFIG_PATH points at a valid config). Either is acceptable.

- [ ] **Step 3: Verify default test run skips it**

```bash
cd server && go test ./pkg/agent/ -v 2>&1 | grep -i openclawintegration | head -5
```

Expected: no matches (build tag excludes the file).

- [ ] **Step 4: Commit**

```bash
git add server/pkg/agent/openclaw_integration_test.go
git commit -m "$(cat <<'EOF'
test(agent/openclaw): add build-tagged integration smoke

Runs a real openclaw + claude end-to-end (only when both are on PATH
and the integration build tag is set). Asserts streaming MessageText
arrives and the session completes — the smallest end-to-end check
that the new parser actually matches OpenClaw's wire format.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: Documentation — `CLI_AND_DAEMON.md` and `.env.example`

**Files:**
- Modify: `CLI_AND_DAEMON.md`
- Modify: `.env.example`

- [ ] **Step 1: Add OpenClaw streaming section to CLI_AND_DAEMON.md**

In `CLI_AND_DAEMON.md`, find the "Supported Agents" table (around line 137-150). Below the agent-specific overrides table (around line 195), add a new subsection:

```markdown
### OpenClaw Live Streaming

The daemon writes a Multica-managed `openclaw.json` to
`~/.multica/profiles/<profile>/openclaw/openclaw.json` on first
detection of the `openclaw` binary, and sets `OPENCLAW_CONFIG_PATH`
when spawning agent tasks. This config defines a `multica-claude`
cliBackend with `output: "jsonl"` and `jsonlDialect: "claude-stream-json"`,
which is what enables OpenClaw to emit live progress events (text
deltas, tool calls, thinking) instead of returning only a final result.

Requirements:

- `claude` on PATH — the `multica-claude` backend wraps the Claude CLI.
- Your own `~/.openclaw/openclaw.json` is never read or modified.

By default, the daemon resolves models as `multica-claude/sonnet`.
Override via `MULTICA_OPENCLAW_MODEL`:

- A bare model name (`opus`) is prefixed with the default backend
  (`multica-claude/opus`).
- A fully-qualified `<backend>/<model>` string (`claude-cli/4`) is
  passed through as-is.
```

- [ ] **Step 2: Update `.env.example`**

In `.env.example`, find the OpenClaw line:

```
MULTICA_OPENCLAW_PATH=openclaw
```

Add comments above the related vars (or replace the existing OpenClaw block):

```
# OpenClaw — defaults to multica-claude/sonnet when MULTICA_OPENCLAW_MODEL is empty.
# Use a full <backend-id>/<model-name> (e.g. claude-cli/4) to bypass the daemon's
# managed cliBackend.
# MULTICA_OPENCLAW_MODEL=
```

(Match the surrounding comment style in the file.)

- [ ] **Step 3: Commit**

```bash
git add CLI_AND_DAEMON.md .env.example
git commit -m "$(cat <<'EOF'
docs: explain OpenClaw live-streaming setup and model resolution

CLI_AND_DAEMON.md gains a section describing the daemon-managed
openclaw.json, OPENCLAW_CONFIG_PATH env var, and what claude-cli
prerequisite is required for the multica-claude backend.

.env.example clarifies how MULTICA_OPENCLAW_MODEL is interpreted.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 19: End-to-end smoke test on dev environment

**Files:** None (manual verification)

- [ ] **Step 1: Build everything**

```bash
make build
```

Expected: `server/bin/{server,multica,migrate}` exist.

- [ ] **Step 2: Restart the dev stack**

```bash
make stop || true
make dev > /tmp/multica-dev.log 2>&1 &
```

- [ ] **Step 3: Wait for health**

```bash
until curl -sf http://localhost:8080/health > /dev/null; do sleep 2; done
echo "Backend up"
```

- [ ] **Step 4: Confirm openclawcfg wrote the file**

```bash
ls -la ~/.multica/profiles/*/openclaw/openclaw.json 2>/dev/null || \
  ls -la ~/.multica/openclaw/openclaw.json 2>/dev/null
```

Expected: file exists. (If openclaw is NOT installed locally — which is the case on this machine — this step is skipped; the daemon doesn't write the config without detecting openclaw.)

- [ ] **Step 5: If openclaw is installed, verify the file content**

```bash
cat ~/.multica/profiles/*/openclaw/openclaw.json 2>/dev/null || \
  cat ~/.multica/openclaw/openclaw.json 2>/dev/null
```

Expected: contains `"multica-claude"` cliBackend with `"jsonlDialect": "claude-stream-json"`.

- [ ] **Step 6: If openclaw is installed, run a task end-to-end via UI**

In the browser at `http://localhost:3000`:
1. Create an agent with provider `openclaw`.
2. Create an issue and assign to that agent.
3. Trigger the task.
4. Watch the Agent Live Card on the issue page — text and tool-call events should appear progressively (not just the final result).

If openclaw is not installed locally, document this manual verification as deferred until the user installs it. Skipping is acceptable.

- [ ] **Step 7: Final commit (changelog entry, if applicable)**

If the repo has a CHANGELOG, add an entry. If not, no commit needed — feature is shipped as of Task 18.

---

## Self-Review

**Spec coverage:**

| Spec section | Plan task(s) | OK? |
|---|---|---|
| Architecture diagram | Tasks 6, 7, 15 (env var injection + invocation) | ✓ |
| `openclawcfg.EnsureConfig` behavior | Tasks 2-5 | ✓ |
| Daemon detection wiring | Task 6 | ✓ |
| Per-task env injection | Task 7 | ✓ |
| Parser envelope dispatch (init/stream_event/result) | Tasks 8, 9, 13 | ✓ |
| `content_block_*` handling | Tasks 10, 11, 12 | ✓ |
| Tool input buffer + lenient `_raw` fallback | Task 11 | ✓ |
| Robustness (unknown type, malformed JSON) | Task 14 | ✓ |
| **Robustness — ANSI escape stripping (routa pattern)** | Task 8 (helper + test) | ✓ |
| **Robustness — MCP tool name normalization (routa pattern)** | Task 8 (helper + test), Task 11 (integration test) | ✓ |
| **Robustness — `signature_delta` acknowledgement (routa pattern)** | Task 12 | ✓ |
| New CLI invocation (--local --json --model multica-claude/...) | Task 15 | ✓ |
| Switch from stderr to stdout reading | Task 15 | ✓ |
| Delete legacy types/tests | Tasks 15, 16 | ✓ |
| Build-tagged integration smoke | Task 17 | ✓ |
| Docs (CLI_AND_DAEMON.md, .env.example) | Task 18 | ✓ |
| Manual end-to-end verification | Task 19 | ✓ |
| **Future direction: ACP adoption (deferred)** | (out of scope, captured in spec) | n/a |

No gaps.

**Placeholder scan:** No "TBD" / "TODO" / "fill in" found. All code blocks contain real content. Commit messages are written out, not "[fill in]".

**Type consistency:** `openclawEnvelope`, `openclawStreamEvent`, `openclawContentBlock`, `openclawDelta`, `openclawUsage`, `openclawResultState`, `openclawOpenBlock` defined once in Task 8 and used unchanged in Tasks 9-14. Function names (`processOpenclawOutput`, `handleInit`, `handleResult`, `handleStreamEvent`, `handleContentBlockStart`, `handleContentBlockDelta`, `handleContentBlockStop`, `addUsage`, `openclawModelString`) consistent across tasks.

**Scope:** 19 tasks, single feature, no decomposition needed.

---

**Plan complete and saved to `docs/superpowers/plans/2026-04-16-openclaw-streaming.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Best when tasks are well-isolated (which these are).

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch with checkpoints for review.

**Which approach?**
