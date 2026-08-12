package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
)

// deadURL starts a server and immediately stops it, yielding an address nothing
// answers — a stand-in for the powered-off box.
func deadURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

// liveURL starts a server that answers anything with 200 and stops it when the
// test ends — a stand-in for the always-on text instance.
func liveURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestTextEmbeddingURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		url     string
		textURL string
		want    string
	}{
		{"no text url falls back to the box", "http://box:8000", "", "http://box:8000"},
		{"text url wins", "http://box:8000", "http://text:8000", "http://text:8000"},
		{"nothing configured", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			cfg.Embedding.URL = tt.url
			cfg.Embedding.TextURL = tt.textURL
			if got := textEmbeddingURL(cfg); got != tt.want {
				t.Errorf("textEmbeddingURL = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildReachabilityChecker_followsTextURL is the whole point of the split
// seen from the UI: with the box off but a text instance up, semantic search
// still answers, so the capability must stay advertised — otherwise the search
// page greys the mode out and the always-on instance is never used.
func TestBuildReachabilityChecker_followsTextURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Embedding.URL = deadURL(t)
	cfg.Embedding.TextURL = liveURL(t)

	checker, err := buildReachabilityChecker(cfg)
	if err != nil {
		t.Fatalf("buildReachabilityChecker: %v", err)
	}
	checker.Tick(context.Background())
	if !checker.Reachable() {
		t.Error("Reachable = false, want true (the text instance answered)")
	}
}

// TestBuildReachabilityChecker_withoutTextURL keeps the unsplit deployment
// honest: with only the box configured, an offline box means no semantic search.
func TestBuildReachabilityChecker_withoutTextURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Embedding.URL = deadURL(t)

	checker, err := buildReachabilityChecker(cfg)
	if err != nil {
		t.Fatalf("buildReachabilityChecker: %v", err)
	}
	checker.Tick(context.Background())
	if checker.Reachable() {
		t.Error("Reachable = true, want false (the box is offline)")
	}
}

// TestBuildReachabilityChecker_unconfigured pins the inert case: no URL at all
// builds no client and never advertises semantic search.
func TestBuildReachabilityChecker_unconfigured(t *testing.T) {
	t.Parallel()
	checker, err := buildReachabilityChecker(&config.Config{})
	if err != nil {
		t.Fatalf("buildReachabilityChecker: %v", err)
	}
	checker.Tick(context.Background())
	if checker.Reachable() {
		t.Error("Reachable = true, want false (nothing configured)")
	}
}
