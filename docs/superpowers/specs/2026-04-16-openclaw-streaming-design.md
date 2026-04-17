# OpenClaw Live Streaming — Design

**Status:** Proposed
**Date:** 2026-04-16
**Author:** Claude (with rahul@mixar.app)

## Problem

When an OpenClaw agent runs a task in Multica, the UI shows nothing until the final result lands. Claude Code agents stream live progress (text deltas, tool calls, thinking) the moment they happen. OpenClaw doesn't.

Root cause: `server/pkg/agent/openclaw.go` parses a hand-rolled NDJSON event vocabulary (`{type:"text"}`, `{type:"tool_use"}`, `{type:"step_finish"}`, ...) that no version of OpenClaw actually emits. The stream loop matches nothing, falls through to the legacy single-blob fallback, and only the final aggregated text is produced. Live events are silently lost.

OpenClaw's real wire format (verified via deepwiki against the upstream repo) uses the `claude-stream-json` envelope with three top-level event types:

```json
{"type":"init","session_id":"..."}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}
{"type":"result","result":"...","usage":{...}}
```

This format only emits when OpenClaw's CLI backend is configured with `output:"jsonl"` + `jsonlDialect:"claude-stream-json"`. That config normally lives in `~/.openclaw/openclaw.json`.

## Goals

1. **Live streaming parity with Claude Code.** OpenClaw text deltas, tool calls, errors, and session ID surface in the Multica UI in real time, using the same unified `Message` channel that Claude already uses.
2. **Zero user setup.** The daemon should make this work end-to-end without the operator hand-editing config files.
3. **No mutation of user-owned config.** The daemon must not write into `~/.openclaw/openclaw.json` (which the user may also edit by hand).

## Non-goals (v1)

- Support for OpenClaw cliBackends other than `claude-cli` (codex-cli, opencode-cli, ...) — defer to a follow-up once the Claude path is proven.
- Path/folder scoping per task.
- Replacing the daemon's existing detection / runtime registration flow — only extending it.
- Migration logic for users with prior broken OpenClaw runs (there's nothing to migrate; the prior parser produced no live events).
- Adopting Agent Client Protocol (ACP) as a universal agent interface (see "Prior art and future direction" below).

## Prior art and future direction

**Prior art reviewed:** [phodal/routa](https://github.com/phodal/routa) — a workspace-first multi-agent platform that integrates Claude Code via a stream-json adapter (`ClaudeCodeProcess`, both TS and Rust implementations). The implementation handles claude-stream-json envelope parsing, tool-use buffering, extended thinking with signatures, and MCP tool name normalization. Three robustness patterns from routa are folded into this design (see "Edge cases & error handling" below): ANSI escape stripping, MCP tool name normalization, and `signature_delta` acknowledgement.

**Routa does NOT integrate OpenClaw** — its agent set is Claude Code, OpenCode, Gemini, Codex, Copilot, all via Agent Client Protocol (ACP) JSON-RPC except Claude. So we can't borrow an adapter directly, but the claude-stream-json patterns transfer because OpenClaw emits the same envelope (per the deepwiki spec).

**Future direction:** routa's universal-ACP approach is the right long-term architecture for Multica too — one ACP client + per-CLI translators replaces N hand-rolled parsers (`claude.go`, `codex.go`, `openclaw.go`, ...). Adopting ACP would also unlock cross-platform agent messaging (A2A protocol). However, it's a multi-week refactor that's out of scope for the immediate "make OpenClaw stream" task. Captured as a follow-up brainstorm: "Adopt ACP as Multica's agent transport".

## Approach (chosen: A from brainstorm)

**Daemon-owned config + `OPENCLAW_CONFIG_PATH` env var.**

The daemon writes a Multica-owned `openclaw.json` to a per-profile dir (`~/.multica/profiles/<profile>/openclaw/openclaw.json`) and sets `OPENCLAW_CONFIG_PATH` to that path when spawning `openclaw`. The user's `~/.openclaw/openclaw.json` is never read or written.

This was selected over (B) "mutate the user's openclaw.json" and (C) "document-only, no auto-config" for three reasons:
1. Isolation matches the existing per-profile pattern (`~/.multica/profiles/<name>/`).
2. Self-healing: if the file gets deleted or corrupted, the daemon just rewrites it.
3. No risk of conflict with files the user edits by hand.

## Architecture

```
                           ~/.multica/profiles/<profile>/openclaw/openclaw.json
                            (daemon-owned, idempotent, written on detection)
                                       │
                                       ▼
   prompt ──► openclaw agent --local --json --message <p> --model multica-claude/<model>
                  │  env: OPENCLAW_CONFIG_PATH=~/.multica/profiles/<profile>/openclaw/openclaw.json
                  ▼
            ┌─────────────────────────────────────────────┐
            │  openclaw process                            │
            │   ▶ loads Multica-owned config               │
            │   ▶ spawns claude-cli backend (jsonl mode)   │
            │   ▶ re-emits {init} {stream_event}* {result} │
            │     on stdout                                │
            └─────────────────────────────────────────────┘
                                       │
                                       ▼
                  openclawBackend.processOutput(stdout, ch)
                       parses envelope → unified Message events
                                       │
                                       ▼
                       msgCh ─► daemon ─► server WS ─► UI (no change)
```

## Components

### 1. New: `server/internal/daemon/openclawcfg/openclawcfg.go`

```go
package openclawcfg

// EnsureConfig writes <profileDir>/openclaw/openclaw.json if missing or if our
// "multica-claude" cliBackend entry is absent or stale. Idempotent. Preserves
// any unrelated keys / cliBackends the file may already contain.
//
// Returns the absolute path to the config file (to be set as OPENCLAW_CONFIG_PATH).
func EnsureConfig(profileDir string) (string, error)
```

Behavior:
- Compute `path = <profileDir>/openclaw/openclaw.json`.
- If file doesn't exist → write a minimal config with only the `multica-claude` cliBackend entry. Return path.
- If file exists → unmarshal as `map[string]any` (lenient), navigate to `agents.defaults.cliBackends`, compare existing `multica-claude` entry against expected. If different, replace just that entry; preserve everything else. Write back. Return path.
- On any I/O or JSON error, return error wrapped with file path context.

The minimal config:

```jsonc
{
  "agents": {
    "defaults": {
      "cliBackends": {
        "multica-claude": {
          "command": "claude",
          "output": "jsonl",
          "jsonlDialect": "claude-stream-json"
          // OpenClaw's normalizeClaudeBackendConfig auto-injects
          // --permission-mode bypassPermissions, --setting-sources user,
          // and bundleMcp:true defaults. We don't override.
        }
      }
    }
  }
}
```

### 2. Modified: `server/internal/daemon/config.go`

Around line 102 (where openclaw is detected via `MULTICA_OPENCLAW_PATH`), after registering the agent entry:

```go
configPath, err := openclawcfg.EnsureConfig(profileDir)
if err != nil {
    logger.Warn("openclaw config write failed; live streaming will not work", "error", err)
    // Still register the agent — user can stream-fallback to result-only output
} else {
    agents["openclaw"].Env["OPENCLAW_CONFIG_PATH"] = configPath
}
```

`AgentEntry` already has an `Env map[string]string` field (or close — check during implementation; if not, add it). Daemon passes this to the backend's `Config.Env` which `openclaw.go` already merges via `buildEnv`.

`profileDir` is derived from the daemon's existing profile path (already used for daemon state under `~/.multica/profiles/<profile>/`).

### 3. Rewritten: `server/pkg/agent/openclaw.go`

**CLI invocation changes:**

```go
args := []string{
    "agent",
    "--local",
    "--json",
    "--session-id", sessionID,
    "--model", b.modelString(opts.Model), // "multica-claude/<model>" or "multica-claude/sonnet" if empty
    "--message", prompt,
}
```

`modelString`:
- If `opts.Model == ""` → `"multica-claude/sonnet"`
- If `opts.Model` contains `/` → use as-is (operator passed a full backend/model string)
- Else → `"multica-claude/" + opts.Model`

**Stdout vs stderr:** read from **stdout** (the deepwiki answer is explicit that `--json` enables stdout JSONL streaming for the modern dialect). Pipe stderr to a `logWriter` for debug logs only — same pattern as Claude.

**Parser rewrite (replaces `processOutput` and helpers):**

Top-level dispatch on envelope `type`:

| Envelope type | Action |
|---|---|
| `init` | capture `session_id` into result; no message emitted |
| `stream_event` | drill into `event` then `delta` (see below) |
| `result` | terminal — capture `result` text (replaces accumulated output if present), `is_error`, total `usage`, then exit loop |
| (unknown) | log at debug, continue |

Within `stream_event.event` — block identity is the integer `index` field (present on every block-related event in the `claude-stream-json` dialect). The `id` from `content_block.id` is only consumed at emission time as the `CallID` for tool calls.

| `event.type` | Action |
|---|---|
| `content_block_start` | If `content_block.type == "tool_use"`: record `index → {tool: content_block.name, callID: content_block.id, inputBuf: ""}` in a per-stream map. If `"thinking"`: mark `index` as a thinking block. Otherwise (e.g., `"text"`): no bookkeeping needed. |
| `content_block_delta` | Branch on `delta.type`: `"text_delta"` → emit `MessageText{Content: delta.text}` and append to running output buffer. `"input_json_delta"` → append `delta.partial_json` to the `inputBuf` for the open `index`. `"thinking_delta"` → emit `MessageThinking{Content: delta.thinking}`. |
| `content_block_stop` | If `index` had a buffered tool_use entry: parse `inputBuf` as JSON into `map[string]any` (lenient — empty buffer parses as empty map; malformed parses as `{"_raw": <buf>}` so the call still surfaces); emit `MessageToolUse{Tool, CallID, Input}`; remove the entry. |
| `message_delta` | If the envelope carries `usage` (some claude-stream-json variants put it on `message_delta` itself, others nest it in `delta.usage`) — accumulate token counts. |
| `message_stop` | No-op marker. |
| (unknown) | Log at debug, skip. |

Tool results (`MessageToolResult`) — these are observed by Multica's daemon when the agent's tool calls complete, but in the OpenClaw envelope they don't appear as separate stream events (they're only visible inside the `claude-stream-json` user/assistant blocks via `tool_result` content blocks). For v1, we surface tool calls but not tool results in real-time; the final `result` envelope contains the full transcript. **Acceptable trade-off** because the UI's primary value is "what is the agent doing right now", which `tool_use` provides.

**Code to delete:**
- `tryParseOpenclawEvent`, `tryParseOpenclawResult`, `buildOpenclawEventResult`, `parseOpenclawUsage`, `openclawInt64FirstOf`, `openclawInt64`
- `openclawEvent`, `openclawError`, `openclawErrorData`, `openclawResult`, `openclawPayload`, `openclawMeta` types
- `openclawBlockedArgs` becomes simpler (the new invocation has fewer flags to defend)

**New Go types** (snake-case JSON throughout — matches the `claude-stream-json` dialect deepwiki documented; exact field set will be confirmed against integration-test fixtures during implementation):

```go
type openclawEnvelope struct {
    Type      string          `json:"type"`
    SessionID string          `json:"session_id,omitempty"` // init
    Event     json.RawMessage `json:"event,omitempty"`      // stream_event
    Result    string          `json:"result,omitempty"`     // result
    IsError   bool            `json:"is_error,omitempty"`   // result
    Usage     *openclawUsage  `json:"usage,omitempty"`      // result
}

type openclawStreamEvent struct {
    Type         string                `json:"type"`
    Index        int                   `json:"index,omitempty"`
    ContentBlock *openclawContentBlock `json:"content_block,omitempty"`
    Delta        *openclawDelta        `json:"delta,omitempty"`
    Usage        *openclawUsage        `json:"usage,omitempty"` // message_delta variant 1
}

type openclawContentBlock struct {
    Type string `json:"type"` // "text" | "tool_use" | "thinking"
    ID   string `json:"id,omitempty"`
    Name string `json:"name,omitempty"` // tool name
}

type openclawDelta struct {
    Type        string         `json:"type"` // "text_delta" | "input_json_delta" | "thinking_delta"
    Text        string         `json:"text,omitempty"`
    PartialJSON string         `json:"partial_json,omitempty"`
    Thinking    string         `json:"thinking,omitempty"`
    Usage       *openclawUsage `json:"usage,omitempty"` // message_delta variant 2
}

type openclawUsage struct {
    InputTokens              int64 `json:"input_tokens"`
    OutputTokens             int64 `json:"output_tokens"`
    CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
    CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}
```

Usage may appear on either `openclawStreamEvent.Usage` or `openclawDelta.Usage` depending on OpenClaw version. The parser checks both and prefers whichever is present.

### 4. Modified: `CLI_AND_DAEMON.md`

Add ~15 lines under "Supported Agents → OpenClaw" describing:
- Daemon writes `~/.multica/profiles/<profile>/openclaw/openclaw.json` automatically
- Sets `OPENCLAW_CONFIG_PATH` when spawning `openclaw`
- Default model resolves to `multica-claude/sonnet`; override via `MULTICA_OPENCLAW_MODEL` (use `claude-cli/opus`-style format for full control)
- Requires `claude` on PATH (the underlying CLI for the `multica-claude` backend)

Add to `.env.example` under existing OpenClaw section: comment that `MULTICA_OPENCLAW_MODEL` defaults to `multica-claude/sonnet`.

## Data flow at runtime

1. Issue assigned to OpenClaw agent → daemon claims task.
2. Daemon spawns `openclaw agent --local --json --session-id <sid> --model multica-claude/sonnet --message <prompt>` with `OPENCLAW_CONFIG_PATH` set in env.
3. OpenClaw loads our config, picks `multica-claude` backend, spawns `claude` CLI underneath with `--permission-mode bypassPermissions` (auto-injected by OpenClaw's normalizer) and `--output-format stream-json`.
4. Claude streams stream-json events to OpenClaw. OpenClaw re-emits as `{init}`, `{stream_event}*`, `{result}` envelope on stdout.
5. `openclawBackend.processOutput` reads each line, decodes the envelope, emits unified `Message{Type, ...}` events to `msgCh` in real time.
6. Existing daemon → server WebSocket → React Query invalidation pipeline (already works for Claude) shows live tool calls + text deltas in the Multica UI.

## Edge cases & error handling

| Case | Handling |
|---|---|
| `openclaw` not on PATH | Existing `exec.LookPath` check fails fast; backend returns error before spawn |
| `claude` not on PATH | OpenClaw fails to spawn its inner backend; failure surfaces in `result` envelope with `is_error: true` and human-readable message |
| `~/.multica/profiles/<profile>/openclaw/openclaw.json` already exists with user edits | `EnsureConfig` does shallow merge: only `agents.defaults.cliBackends.multica-claude` is replaced; other keys preserved |
| `EnsureConfig` fails (permission denied, disk full) | Daemon logs warning, registers OpenClaw runtime anyway. Streaming won't work but final result still arrives via the (unchanged) `result` envelope path |
| Process exits before `result` envelope (timeout, OOM, kill) | `runCtx.Err()` distinguishes timeout (`status: "timeout"`) vs cancel (`status: "aborted"`); otherwise `cmd.Wait()` non-nil error → `status: "failed"`. Already-emitted text deltas remain in `Result.Output` |
| Malformed JSON line in stream | Logged at debug, skipped; next line still parses |
| Unknown envelope `type` | Logged at debug, ignored — forward-compatible with future OpenClaw versions |
| Tool input arriving in multiple `input_json_delta`s | Buffered per content-block index; flushed on `content_block_stop` — same pattern as Claude SDK handles partial tool inputs |
| OpenClaw version too old to support config path or stream-json dialect | v1 punt: log version on first detection (no version gate). If users hit this, follow-up adds a `requireVersion` check |
| **ANSI escape codes leaking into stream lines** *(new — routa pattern)* | Strip ANSI codes (`\x1b[...m` and similar) from each line before JSON parse. Some claude-cli builds emit colored progress hints to stdout when not TTY-detected. Failure mode without strip: every "colored" line fails JSON parse and gets dropped silently |
| **MCP tool names with `mcp__<server>__<name>` prefix** *(new — routa pattern)* | Normalize the `Tool` field on `MessageToolUse` by stripping the `mcp__<server>__` prefix if present, so the UI shows e.g. `create_task` instead of `mcp__routa-coordination__create_task`. The original prefixed name is still reachable via tool input for any consumer that needs it |
| **`signature_delta` events on extended-thinking blocks** *(new — routa pattern)* | Recognize `delta.type == "signature_delta"` as part of a thinking block; no-op (we don't surface signatures in the UI). Prevents "unknown event" log spam on Claude extended-thinking sessions |

## Testing strategy

| Layer | File | Coverage |
|---|---|---|
| Config writer | `server/internal/daemon/openclawcfg/openclawcfg_test.go` (new) | Writes when missing; no-ops when matching; merges into existing user-edited file (preserves unknown keys); error path on permission denied |
| Parser — happy paths | `server/pkg/agent/openclaw_test.go` (rewritten) | `init` captures session_id; sequence of `stream_event` text_deltas emits MessageText in order; `result` envelope provides final output + usage |
| Parser — tool calls | same | `content_block_start{tool_use}` followed by `input_json_delta`s and `content_block_stop` produces one `MessageToolUse` with parsed input map |
| Parser — thinking | same | `thinking_delta` emits `MessageThinking` |
| Parser — errors | same | `result` with `is_error: true` → status=failed, error message in Result.Error |
| Parser — robustness | same | Unknown envelope type ignored; malformed JSON line skipped without breaking subsequent parsing; empty stream → status=failed |
| Integration smoke | `server/pkg/agent/openclaw_integration_test.go` (new, build-tagged `//go:build integration`) | Skips unless `openclaw` and `claude` on PATH. Runs `openclaw agent --local --json --message "say hello"` end-to-end; asserts ≥1 MessageText arrives via channel and `Result.Status == "completed"`. Build-tagged so default `go test ./...` skips it |

No frontend tests needed — `Message` shape is unchanged, so existing `agent-live-card.tsx` rendering tests cover this implicitly.

## Migration

None. No DB schema change. No env var rename.

Existing OpenClaw runs were broken (live events never reached the UI; only the final result blob worked via the legacy fallback). After this change, those same runs stream properly. The legacy `openclawResult` blob format is removed entirely — it was never the modern OpenClaw output format and (per CLAUDE.md) we don't keep dead compatibility paths.

## Files changed (summary)

| File | Change |
|---|---|
| `server/internal/daemon/openclawcfg/openclawcfg.go` | **New**, ~80 lines |
| `server/internal/daemon/openclawcfg/openclawcfg_test.go` | **New**, ~120 lines |
| `server/internal/daemon/config.go` | ~10 lines added near openclaw detection |
| `server/pkg/agent/openclaw.go` | Rewritten (~300 lines, replaces existing ~490) |
| `server/pkg/agent/openclaw_test.go` | Rewritten (existing 900 lines → ~400 lines targeted at the new envelope) |
| `server/pkg/agent/openclaw_integration_test.go` | **New**, build-tagged, ~60 lines |
| `CLI_AND_DAEMON.md` | ~15 lines added under OpenClaw section |
| `.env.example` | One comment line added near `MULTICA_OPENCLAW_MODEL` |

## Open questions deferred to implementation

1. The exact name of the field on `AgentEntry` used to pass per-agent env vars to the backend `Config.Env` — verified during plan-writing. If absent, plan must include adding it.
2. Whether OpenClaw normalizes `multica-claude/sonnet` to a real Claude model alias on its own, or we need to pass the full alias (e.g., `multica-claude/claude-3-5-sonnet-20241022`). Resolved by integration test against the user's actual installed OpenClaw version.

## Success criteria

1. Assigning a task to an OpenClaw agent in Multica produces visible text deltas and tool-call events in the issue's Agent Live Card within a few seconds of execution start (parity with Claude).
2. `make test` passes after the rewrite.
3. The integration test (when run with both binaries installed) succeeds end-to-end.
4. Operator with no OpenClaw config in `~/.openclaw/` sees streaming work on first run, no manual config required.
5. Operator with their own `~/.openclaw/openclaw.json` is unaffected — their file is never read or written by the daemon.
