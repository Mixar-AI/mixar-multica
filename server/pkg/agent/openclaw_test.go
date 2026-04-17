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
