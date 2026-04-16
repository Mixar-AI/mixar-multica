package agent

import (
	"log/slog"
	"strings"
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
