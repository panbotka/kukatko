package photos_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// forJobUse matches a call of, or a method value taken off, one of the unscoped
// job loaders (GetForJob, FilesForJob and any later *ForJob).
var forJobUse = regexp.MustCompile(`ForJob\(|\.\w*ForJob\b`)

// TestForJobLoadersStayOutOfTheAPI fails when any non-test source file of an
// internal/*api package reaches for an unscoped *ForJob loader. Those loaders
// read a photo in whichever project holds it — right for a background job,
// which has no request, and a cross-tenant leak in a handler, which must load
// through the project-scoped lookups instead. The test is the enforcement the
// loaders' names promise.
func TestForJobLoadersStayOutOfTheAPI(t *testing.T) {
	t.Parallel()

	dirs, err := filepath.Glob(filepath.Join("..", "*api"))
	if err != nil {
		t.Fatalf("globbing the api packages: %v", err)
	}
	// A walk that finds nothing would pass vacuously; there are dozens.
	if len(dirs) < 20 {
		t.Fatalf("found only %d internal/*api packages: the walk is looking in the wrong place", len(dirs))
	}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("globbing %s: %v", dir, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			checkNoForJob(t, file)
		}
	}
}

// checkNoForJob reports every line of file that uses a *ForJob loader.
func checkNoForJob(t *testing.T, file string) {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		if forJobUse.MatchString(line) {
			t.Errorf("%s:%d uses an unscoped job loader in an API package "+
				"(load through the project-scoped lookup instead): %s", file, i+1, strings.TrimSpace(line))
		}
	}
}
