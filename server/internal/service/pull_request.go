package service

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ParsedPRURL is the result of parsing a PR/MR URL.
type ParsedPRURL struct {
	RepoURL  string // canonical https URL to the repo (no .git, no trailing slash)
	Number   int
	Platform string // "github" | "gitlab"
}

var (
	githubPRPath = regexp.MustCompile(`^/([^/]+)/([^/]+)/pull/(\d+)/?$`)
	gitlabMRPath = regexp.MustCompile(`^/([^/]+)/([^/]+)/-/merge_requests/(\d+)/?$`)
)

// ParsePRURL parses a GitHub pull-request URL or GitLab merge-request URL.
// Returns an error for anything else.
func ParsePRURL(raw string) (ParsedPRURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedPRURL{}, fmt.Errorf("pr url is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ParsedPRURL{}, fmt.Errorf("parse pr url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return ParsedPRURL{}, fmt.Errorf("pr url must be http(s)")
	}
	host := strings.ToLower(u.Host)

	switch {
	case host == "github.com":
		m := githubPRPath.FindStringSubmatch(u.Path)
		if m == nil {
			return ParsedPRURL{}, fmt.Errorf("not a github pull-request url: %s", raw)
		}
		n, _ := strconv.Atoi(m[3])
		return ParsedPRURL{
			RepoURL:  fmt.Sprintf("https://github.com/%s/%s", m[1], m[2]),
			Number:   n,
			Platform: "github",
		}, nil
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com") || strings.HasPrefix(host, "gitlab."):
		m := gitlabMRPath.FindStringSubmatch(u.Path)
		if m == nil {
			return ParsedPRURL{}, fmt.Errorf("not a gitlab merge-request url: %s", raw)
		}
		n, _ := strconv.Atoi(m[3])
		return ParsedPRURL{
			RepoURL:  fmt.Sprintf("https://%s/%s/%s", host, m[1], m[2]),
			Number:   n,
			Platform: "gitlab",
		}, nil
	default:
		return ParsedPRURL{}, fmt.Errorf("unsupported pr url host: %s", host)
	}
}
