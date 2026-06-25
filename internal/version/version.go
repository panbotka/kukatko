// Package version exposes build-time version information for the kukatko binary.
//
// The Version and Commit variables are placeholders that are meant to be
// overridden at build time via -ldflags, for example:
//
//	go build -ldflags "\
//	  -X github.com/panbotka/kukatko/internal/version.Version=1.2.3 \
//	  -X github.com/panbotka/kukatko/internal/version.Commit=$(git rev-parse --short HEAD)"
package version

import "fmt"

// Build information overridable at link time with -ldflags "-X ...".
var (
	// Version is the semantic version of the build. It defaults to "dev" for
	// local, non-released builds.
	Version = "dev"
	// Commit is the short git commit hash the binary was built from. It
	// defaults to "none" when no commit information is injected.
	Commit = "none"
)

// Info bundles the build-time version metadata for a kukatko binary so it can
// be serialised (for example into the /healthz response) as a single value.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Get returns the current build information assembled from the package-level
// Version and Commit variables.
func Get() Info {
	return Info{Version: Version, Commit: Commit}
}

// String returns a human-readable "version (commit)" representation of the
// build information.
func (i Info) String() string {
	return fmt.Sprintf("%s (%s)", i.Version, i.Commit)
}
