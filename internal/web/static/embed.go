// Package static embeds the built frontend single-page application so the
// kukatko binary can serve the UI without any external files on disk.
//
// The build pipeline (see the Makefile build target) compiles the Vite project
// into the dist directory before "go build" runs, so the contents of dist are
// captured into the binary at compile time. A committed dist/.gitkeep keeps the
// embed directive valid even when no frontend has been built yet.
package static

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:dist/*
var distFS embed.FS

// FS returns the embedded frontend distribution rooted at the dist directory.
// It returns an error only if the embedded tree cannot be sub-rooted, which
// indicates a build-time packaging fault rather than a runtime condition.
func FS() (fs.FS, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("sub-rooting embedded dist: %w", err)
	}
	return sub, nil
}
