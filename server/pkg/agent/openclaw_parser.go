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
}

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
	case "signature_delta":
		// Extended-thinking signature verification — acknowledged but not surfaced.
		// Routa pattern: stored alongside thinking text; we don't expose signatures
		// in the UI so we no-op silently.
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
