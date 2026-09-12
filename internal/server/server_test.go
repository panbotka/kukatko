package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestWellKnownIsNotSwallowedBySPA pins that the metadata prefix answers 404
// instead of the index document. An MCP client with no WWW-Authenticate to go on
// probes /.well-known/oauth-protected-resource/<path> and then
// /.well-known/oauth-protected-resource, and expects a 404 to mean "this server
// has no OAuth". Answering 200 and HTML makes it fail parsing a web page as
// JSON instead. Kukátko publishes nothing under the prefix: ACME is terminated by
// the reverse proxy and the PWA manifest lives at /manifest.webmanifest.
func TestWellKnownIsNotSwallowedBySPA(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/.well-known",
		"/.well-known/",
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/api/v1/mcp",
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	}
	srv := New("").Handler()

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s status = %d, want 404", path, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
				t.Errorf("GET %s Content-Type = %q; the SPA fallback swallowed the probe", path, ct)
			}
			if body := rec.Body.String(); body != notFoundBody {
				t.Errorf("GET %s body = %q, want %q", path, body, notFoundBody)
			}
		})
	}
}

// notFoundBody is exactly what handleNotFound writes, and nothing else in the
// router does. Comparing against it tells "this path was carved out of the SPA
// fallback" from "the SPA answered", without depending on whether the embedded
// frontend was built — an unbuilt dist makes the fallback 404 too, so the status
// code alone cannot tell the two apart.
const notFoundBody = "{\"error\":\"not found\"}\n"

// TestSPAFallbackStillOwnsUnknownPaths guards the other side of the same change:
// only /.well-known was carved out, and an ordinary client-side route still
// reaches the SPA handler. What that handler then answers depends on whether the
// frontend was built into the binary, so the assertion is that handleNotFound did
// not take the request — not a status code.
func TestSPAFallbackStillOwnsUnknownPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/albums/holiday", "/well-known/oauth-protected-resource"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)

			New("").Handler().ServeHTTP(rec, req)

			if rec.Body.String() == notFoundBody {
				t.Errorf("GET %s was answered by handleNotFound; only /.well-known is carved out", path)
			}
		})
	}
}
