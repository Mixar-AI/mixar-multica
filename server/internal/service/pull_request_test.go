package service

import "testing"

func TestParsePRURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		wantRepo string
		wantNum  int
		wantPlat string
		wantErr  bool
	}{
		{"https://github.com/foo/bar/pull/42", "https://github.com/foo/bar", 42, "github", false},
		{"https://github.com/foo/bar/pull/42/", "https://github.com/foo/bar", 42, "github", false},
		{"https://gitlab.com/foo/bar/-/merge_requests/7", "https://gitlab.com/foo/bar", 7, "gitlab", false},
		{"", "", 0, "", true},
		{"not-a-url", "", 0, "", true},
		{"https://example.com/foo/bar/pull/1", "", 0, "", true},
		{"https://github.com/foo/bar/issues/1", "", 0, "", true},
	}
	for _, c := range cases {
		got, err := ParsePRURL(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParsePRURL(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if c.wantErr {
			continue
		}
		if got.RepoURL != c.wantRepo || got.Number != c.wantNum || got.Platform != c.wantPlat {
			t.Errorf("ParsePRURL(%q) = {%q, %d, %q}, want {%q, %d, %q}",
				c.in, got.RepoURL, got.Number, got.Platform, c.wantRepo, c.wantNum, c.wantPlat)
		}
	}
}
