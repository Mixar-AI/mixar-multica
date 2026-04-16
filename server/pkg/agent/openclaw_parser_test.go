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
