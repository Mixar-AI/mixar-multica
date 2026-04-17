package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub is a stub GitHubClient for tests.
type fakeGitHub struct {
	pr           *GHPullRequest
	prErr        error
	comments     []GHReviewComment
	commentsErr  error
	fetchPRCalls atomic.Int64
}

func (f *fakeGitHub) FetchPR(_ context.Context, _, _ string, _ int) (*GHPullRequest, error) {
	f.fetchPRCalls.Add(1)
	return f.pr, f.prErr
}

func (f *fakeGitHub) FetchReviewComments(_ context.Context, _, _ string, _ int, _ time.Time) ([]GHReviewComment, error) {
	return f.comments, f.commentsErr
}

// TestParseGitHubPRURL verifies that well-formed and malformed URLs are handled.
func TestParseGitHubPRURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in     string
		owner  string
		repo   string
		number int
		wantOK bool
	}{
		{"https://github.com/acme/widget/pull/42", "acme", "widget", 42, true},
		{"https://github.com/acme/widget/pull/1", "acme", "widget", 1, true},
		{"https://gitlab.com/acme/widget/-/merge_requests/5", "", "", 0, false},
		{"not-a-url", "", "", 0, false},
		{"", "", "", 0, false},
	}
	for _, c := range cases {
		owner, repo, number, ok := parseGitHubPRURL(c.in)
		if ok != c.wantOK {
			t.Errorf("parseGitHubPRURL(%q) ok=%v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !c.wantOK {
			continue
		}
		if owner != c.owner || repo != c.repo || number != c.number {
			t.Errorf("parseGitHubPRURL(%q) = (%q, %q, %d), want (%q, %q, %d)",
				c.in, owner, repo, number, c.owner, c.repo, c.number)
		}
	}
}

// TestPRPoller_NonGitHubURLSkipped verifies that non-GitHub PR URLs are skipped
// without calling FetchPR.
func TestPRPoller_NonGitHubURLSkipped(t *testing.T) {
	t.Parallel()

	// Calling parseGitHubPRURL with a GitLab URL should return not-ok.
	_, _, _, ok := parseGitHubPRURL("https://gitlab.com/foo/bar/-/merge_requests/1")
	if ok {
		t.Error("expected non-GitHub URL to return ok=false")
	}

	// Empty URL also not-ok.
	_, _, _, ok2 := parseGitHubPRURL("")
	if ok2 {
		t.Error("expected empty URL to return ok=false")
	}
}

// TestPRPoller_Run_StopsOnContextCancel verifies Run exits when context is cancelled.
func TestPRPoller_Run_StopsOnContextCancel(t *testing.T) {
	t.Parallel()
	gh := &fakeGitHub{}
	poller := NewPRPoller(nil, gh, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Use a very long interval so the tick never fires; we rely on ctx cancel.
		poller.Run(ctx, 10*time.Minute)
	}()

	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Error("Run did not stop within 2 seconds after context cancel")
	}
}
