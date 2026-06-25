package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/version"
)

// TestNew_defaultAddr verifies the listen address defaulting behaviour.
func TestNew_defaultAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "empty falls back to default", addr: "", want: DefaultAddr},
		{name: "explicit address is kept", addr: "127.0.0.1:9999", want: "127.0.0.1:9999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := New(tt.addr).Addr(); got != tt.want {
				t.Errorf("New(%q).Addr() = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

// TestHandleHealthz_ok checks that GET /healthz returns 200 with the expected
// JSON body and Content-Type.
func TestHandleHealthz_ok(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)

	New("").Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status field = %q, want %q", body.Status, "ok")
	}
	if body.Version != version.Get() {
		t.Errorf("version field = %+v, want %+v", body.Version, version.Get())
	}
}

// TestHandleHealthz_methodNotAllowed verifies the route only answers GET.
func TestHandleHealthz_methodNotAllowed(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/healthz", nil)

	New("").Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestServerRun_gracefulShutdown verifies Run serves requests and then returns
// nil when its context is canceled.
func TestServerRun_gracefulShutdown(t *testing.T) {
	t.Parallel()

	// Port 0 lets the OS pick a free port, avoiding collisions in tests.
	srv := New("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within timeout after context cancellation")
	}
}
