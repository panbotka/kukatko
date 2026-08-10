package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// newTestDist builds an in-memory dist tree mimicking a Vite build so the SPA
// handler can be exercised without an actual frontend compile.
func newTestDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html><div id=root></div>")},
		"assets/app-abc123.js":  {Data: []byte("console.log('app')")},
		"favicon.svg":           {Data: []byte("<svg/>")},
		"sw.js":                 {Data: []byte("self.addEventListener('fetch', () => {})")},
		"manifest.webmanifest":  {Data: []byte(`{"name":"Kukátko"}`)},
		"icons/kukatko-192.png": {Data: []byte("\x89PNG\r\n\x1a\n")},
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

// TestSPAHandler_fallsBackToIndexForSharePost verifies the share target's POST
// resolves to the index document like any other client route.
//
// That POST is normally intercepted by the service worker (it stages the shared
// files and redirects), so it only reaches the server when no worker is
// installed to catch it — a first run, or a browser that dropped the
// registration. Answering it with the SPA lands the user on the share page,
// which says the files did not come through and offers the picker; a 405 would
// show a bare error instead. Method-gating the fallback would break that, hence
// the test.
func TestSPAHandler_fallsBackToIndexForSharePost(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/share-target", strings.NewReader("files=..."),
	)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	SPAHandler(newTestDist()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, "id=root") {
		t.Errorf("body = %q, want the index document", got)
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

// TestSPAHandler_servesServiceWorkerAtRootScope verifies /sw.js is served as a
// script with a revalidating cache policy, which is what lets a deployment
// replace the worker (and with it the precached shell).
func TestSPAHandler_servesServiceWorkerAtRootScope(t *testing.T) {
	t.Parallel()

	rec := doGet(t, SPAHandler(newTestDist()), "/sw.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

// TestSPAHandler_missingServiceWorkerReturns404 verifies a build without a
// worker answers /sw.js with 404 rather than with the index document, which the
// browser would reject as a script with an unhelpful MIME error.
func TestSPAHandler_missingServiceWorkerReturns404(t *testing.T) {
	t.Parallel()

	dist := newTestDist()
	delete(dist, "sw.js")

	rec := doGet(t, SPAHandler(dist), "/sw.js")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestSPAHandler_servesWebManifestAsManifestType verifies the web app manifest
// is served with a media type a browser accepts for one. The Go mime table
// takes its answer from the host, and on most hosts it has none for
// .webmanifest, so the type has to be pinned by this package.
func TestSPAHandler_servesWebManifestAsManifestType(t *testing.T) {
	t.Parallel()

	rec := doGet(t, SPAHandler(newTestDist()), "/manifest.webmanifest")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("Content-Type = %q, want application/manifest+json", ct)
	}
}

// TestContentTypeFor covers the pinned overrides, the platform-known types and
// the unknown extension that must fall through to content sniffing.
func TestContentTypeFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ext  string
		want string
	}{
		{name: "pinned web manifest", ext: ".webmanifest", want: "application/manifest+json"},
		{name: "pinned lookup is case-insensitive", ext: ".WEBMANIFEST", want: "application/manifest+json"},
		{name: "platform type for png", ext: ".png", want: "image/png"},
		{name: "unknown extension yields nothing", ext: ".kukatko", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contentTypeFor(tt.ext); got != tt.want {
				t.Errorf("contentTypeFor(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

// TestServedVerbatim covers which paths refuse the SPA fallback: fingerprinted
// bundles and the service worker, but not the app's own client-side routes.
func TestServedVerbatim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "fingerprinted bundle", path: "assets/app-abc123.js", want: true},
		{name: "service worker", path: "sw.js", want: true},
		{name: "web manifest may fall back", path: "manifest.webmanifest", want: false},
		{name: "client-side route", path: "albums/42", want: false},
		{name: "index document", path: "index.html", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := servedVerbatim(tt.path); got != tt.want {
				t.Errorf("servedVerbatim(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
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
