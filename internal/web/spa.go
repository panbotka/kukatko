// Package web serves the embedded frontend single-page application with an SPA
// fallback: client-side routes resolve to index.html while real asset files are
// served directly, and fingerprinted assets are cached aggressively.
package web

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/panbotka/kukatko/internal/web/static"
)

// assetsPrefix is the directory (within dist) that Vite emits fingerprinted,
// content-hashed bundles into; files there are safe to cache indefinitely.
const assetsPrefix = "assets/"

// indexFile is the SPA entry document served for the application root and for
// any client-side route that does not map to a real embedded file.
const indexFile = "index.html"

// serviceWorkerFile is the progressive-web-app service worker the frontend build
// emits at the root of dist (see web/build/pwa.ts). It shares the fingerprinted
// assets' no-fallback rule: a worker script answered with the index document
// fails registration with a confusing MIME error instead of a plain 404.
const serviceWorkerFile = "sw.js"

// extraContentTypes pins the media type of extensions the Go mime table cannot
// be trusted to know. mime.TypeByExtension consults the operating system's mime
// database, so its answer differs between a developer's box and the server, and
// for .webmanifest it is commonly empty — which leaves net/http sniffing the
// manifest as text/plain, a type no browser accepts for one.
var extraContentTypes = map[string]string{
	".webmanifest": "application/manifest+json",
}

// Handler returns the SPA HTTP handler backed by the embedded frontend build.
// If the embedded filesystem cannot be initialised (which should not happen for
// a correctly built binary), it returns a handler that reports HTTP 500 so the
// failure is visible rather than silently swallowed.
func Handler() http.Handler {
	dist, err := static.FS()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "frontend assets unavailable", http.StatusInternalServerError)
		})
	}
	return SPAHandler(dist)
}

// SPAHandler returns an http.Handler that serves files from dist and falls back
// to index.html for non-asset paths so client-side routing works on deep links.
// A missing file under the assets/ prefix yields 404 rather than the index
// document, so a stale asset URL fails loudly instead of returning HTML.
func SPAHandler(dist fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = indexFile
		}

		if serveFile(w, dist, name) {
			return
		}
		if !servedVerbatim(name) && serveFile(w, dist, indexFile) {
			return
		}
		http.NotFound(w, r)
	})
}

// servedVerbatim reports whether name must resolve to a real file or to nothing
// at all, never to the index document: the fingerprinted bundles under assets/
// and the service worker. Everything else is a candidate for the SPA fallback.
func servedVerbatim(name string) bool {
	return strings.HasPrefix(name, assetsPrefix) || name == serviceWorkerFile
}

// contentTypeFor returns the media type to serve a file with, given its
// extension (including the leading dot), or the empty string when neither the
// pinned overrides nor the platform mime table knows it and sniffing should
// decide.
func contentTypeFor(ext string) string {
	if pinned, ok := extraContentTypes[strings.ToLower(ext)]; ok {
		return pinned
	}
	return mime.TypeByExtension(ext)
}

// serveFile writes the named file from fsys to w with a content type derived
// from its extension, returning false if the file is absent or is a directory.
// Fingerprinted files under assets/ get a long immutable cache; everything else
// (notably index.html) is served with no-cache so deploys take effect at once.
func serveFile(w http.ResponseWriter, fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	if ct := contentTypeFor(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if strings.HasPrefix(name, assetsPrefix) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
	return true
}
