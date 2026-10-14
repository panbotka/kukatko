package photos_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// forJobUse matches a call of, or a method value taken off, one of the unscoped
// loaders: the job loaders (GetForJob, vectors' GetEmbeddingForJob and any later
// *ForJob) and GetAcrossProjects, the cross-project photo read of the profile
// picture chain.
var forJobUse = regexp.MustCompile(`ForJob\(|\.\w*ForJob\b|AcrossProjects\b`)

// TestForJobLoadersStayOutOfTheAPI fails when any non-test source file of an
// internal/*api package reaches for an unscoped *ForJob loader or for
// GetAcrossProjects. Those loaders read a row in whichever project holds it —
// right for a background job, which has no request, and for the profile picture
// chain, which authorizes the photo's project itself; a cross-tenant leak in a
// handler, which must load through the project-scoped lookups instead. The test
// is the enforcement the loaders' names promise.
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

// TestForJobUse_matchesEveryUnscopedLoader pins the pattern itself, so a later
// edit cannot quietly stop matching one of the loaders it promises to keep out
// of the API packages.
func TestForJobUse_matchesEveryUnscopedLoader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want bool
	}{
		{line: "photo, err := a.photos.GetForJob(ctx, uid)", want: true},
		{line: "load := a.photos.GetForJob", want: true},
		{line: "emb, err := a.vectors.GetEmbeddingForJob(ctx, uid)", want: true},
		{line: "photo, err := a.photos.GetAcrossProjects(r.Context(), uid)", want: true},
		{line: "load := a.photos.GetAcrossProjects", want: true},
		{line: "photo, err := a.photos.GetByUIDInProject(ctx, projectUID, uid)", want: false},
		{line: "emb, err := a.similar.GetEmbedding(ctx, projectUID, uid)", want: false},
	}
	for _, tt := range tests {
		if got := forJobUse.MatchString(tt.line); got != tt.want {
			t.Errorf("forJobUse.MatchString(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}
