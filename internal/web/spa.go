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
		if !strings.HasPrefix(name, assetsPrefix) && serveFile(w, dist, indexFile) {
			return
		}
		http.NotFound(w, r)
	})
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

	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
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
