package tenancyguard_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/tenancyguard"
)

// requestPathDirs are the directories that serve requests: every HTTP API
// package and the server's own wiring. None of them may single out the
// bootstrap project.
var requestPathDirs = []string{"internal/*api", "cmd/kukatko"}

// allowedBootstrapReferences are the `file:line what` keys of references in
// requestPathDirs that are not a request path — start-up bootstrap code that
// must name the first project. There are none today; an entry needs a comment
// saying why it is not reachable from a request.
var allowedBootstrapReferences []string

// TestRepository_noRequestPathNamesTheBootstrapProject is the guard: no file
// under internal/*api or cmd/kukatko references project.BootstrapUID,
// project.BootstrapSlug or the bootstrap uid itself. A request names its
// project in the URL; there is no default one to fall back to.
func TestRepository_noRequestPathNamesTheBootstrapProject(t *testing.T) {
	t.Parallel()
	refs, err := tenancyguard.BootstrapReferences(repoRoot(t), requestPathDirs...)
	if err != nil {
		t.Fatalf("BootstrapReferences: %v", err)
	}
	for _, ref := range refs {
		if slices.Contains(allowedBootstrapReferences, ref.String()) {
			continue
		}
		t.Errorf("%s: a request path names the bootstrap project; take the project from the request "+
			"(auth.ProjectFromContext) instead", ref)
	}
}

// TestBootstrapReferences_findsEveryForm verifies the scan finds the two
// constants under the package's own name and under an alias, and a literal
// spelling out the uid — and skips test files, subdirectories and other
// packages' identically named constants.
func TestBootstrapReferences_findsEveryForm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("internal/fooapi/a.go", `package fooapi

import "github.com/panbotka/kukatko/internal/project"

var a = project.BootstrapUID
var b = project.NewStore
`)
	write("internal/fooapi/b.go", `package fooapi

import proj "github.com/panbotka/kukatko/internal/project"

var c = proj.BootstrapSlug
var d = "pj0000000000000000veselice"
`)
	write("internal/fooapi/c.go", `package fooapi

import "example.com/other/project"

var e = project.BootstrapUID
`)
	write("internal/fooapi/a_test.go", `package fooapi

import "github.com/panbotka/kukatko/internal/project"

var f = project.BootstrapUID
`)
	write("internal/fooapi/sub/d.go", `package sub

import "github.com/panbotka/kukatko/internal/project"

var g = project.BootstrapUID
`)
	write("internal/foo/e.go", `package foo

import "github.com/panbotka/kukatko/internal/project"

var h = project.BootstrapUID
`)

	refs, err := tenancyguard.BootstrapReferences(root, "internal/*api")
	if err != nil {
		t.Fatalf("BootstrapReferences: %v", err)
	}
	got := make([]string, 0, len(refs))
	for _, ref := range refs {
		got = append(got, ref.String())
	}
	want := []string{
		"internal/fooapi/a.go:5 project.BootstrapUID",
		"internal/fooapi/b.go:5 proj.BootstrapSlug",
		`internal/fooapi/b.go:6 "pj0000000000000000veselice"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("BootstrapReferences = %q, want %q", got, want)
	}
}
