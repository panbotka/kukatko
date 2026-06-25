package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// newTestDist builds an in-memory dist tree mimicking a Vite build so the SPA
// handler can be exercised without an actual frontend compile.
func newTestDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/app-abc123.js": {Data: []byte("console.log('app')")},
		"favicon.svg":          {Data: []byte("<svg/>")},
	}
}

// doGet runs a GET request for target against h and returns the recorder.
func doGet(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	h.ServeHTTP(rec, req)
	return rec
}

// TestSPAHandler_servesIndexForRoot verifies the application root returns the
// index document with a no-cache policy.
func TestSPAHandler_servesIndexForRoot(t *testing.T) {
	t.Parallel()

	rec := doGet(t, SPAHandler(newTestDist()), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if got := rec.Body.String(); got == "" {
		t.Error("expected index.html body, got empty response")
	}
}

// TestSPAHandler_servesAssetWithImmutableCache verifies fingerprinted assets are
// returned with an immutable, long-lived cache header.
func TestSPAHandler_servesAssetWithImmutableCache(t *testing.T) {
	t.Parallel()

	rec := doGet(t, SPAHandler(newTestDist()), "/assets/app-abc123.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable long cache", cc)
	}
}

// TestSPAHandler_fallsBackToIndexForClientRoute verifies an unknown non-asset
// path (a client-side route) resolves to index.html so deep links work.
func TestSPAHandler_fallsBackToIndexForClientRoute(t *testing.T) {
	t.Parallel()

	rec := doGet(t, SPAHandler(newTestDist()), "/library/albums/42")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got == "" {
		t.Error("expected index.html fallback body, got empty response")
	}
}

// TestSPAHandler_missingAssetReturns404 verifies a missing file under assets/
// fails loudly with 404 rather than serving the SPA index document.
func TestSPAHandler_missingAssetReturns404(t *testing.T) {
	t.Parallel()

	rec := doGet(t, SPAHandler(newTestDist()), "/assets/does-not-exist.js")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestHandler_usesEmbeddedFS verifies the embedded-FS handler constructor wires
// up without panicking and answers requests.
func TestHandler_usesEmbeddedFS(t *testing.T) {
	t.Parallel()

	rec := doGet(t, Handler(), "/assets/missing.js")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for missing embedded asset", rec.Code, http.StatusNotFound)
	}
}
