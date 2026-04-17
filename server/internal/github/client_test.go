package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchPR_HappyPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"number": 42,
		"state":  "open",
		"draft":  false,
		"merged": false,
		"title":  "Fix the thing",
		"head":   map[string]string{"ref": "fix/thing"},
		"base":   map[string]string{"ref": "main"},
		"updated_at": now.Format(time.RFC3339),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/pulls/42" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.base = srv.URL

	pr, err := c.FetchPR(context.Background(), "acme", "widget", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("number: got %d, want 42", pr.Number)
	}
	if pr.State != "open" {
		t.Errorf("state: got %q, want open", pr.State)
	}
	if pr.HeadRef != "fix/thing" {
		t.Errorf("head_ref: got %q, want fix/thing", pr.HeadRef)
	}
	if pr.BaseRef != "main" {
		t.Errorf("base_ref: got %q, want main", pr.BaseRef)
	}
}

func TestFetchPR_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.base = srv.URL

	_, err := c.FetchPR(context.Background(), "acme", "widget", 99)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestFetchReviewComments_HappyPath(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	comments := []map[string]any{
		{
			"id":         int64(1001),
			"body":       "Please add a test.",
			"user":       map[string]string{"login": "reviewer1"},
			"created_at": now.Format(time.RFC3339),
			"html_url":   "https://github.com/acme/widget/pull/42#discussion_r1001",
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(comments)
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.base = srv.URL

	since := now.Add(-time.Hour)
	got, err := c.FetchReviewComments(context.Background(), "acme", "widget", 42, since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(got))
	}
	if got[0].Body != "Please add a test." {
		t.Errorf("body: got %q", got[0].Body)
	}
	if got[0].User != "reviewer1" {
		t.Errorf("user: got %q", got[0].User)
	}
}

func TestFetchReviewComments_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.base = srv.URL

	_, err := c.FetchReviewComments(context.Background(), "acme", "widget", 42, time.Now())
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}
