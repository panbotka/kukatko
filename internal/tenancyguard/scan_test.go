package tenancyguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// scanSource scans one in-memory file of package p and returns the findings'
// lines with their tables, e.g. "4:photos".
func scanSource(t *testing.T, body string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/root/p/x.go", "package p\n\n"+body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	findings := ScanFiles(fset, "/root", []*ast.File{file})
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.Path != "p/x.go" {
			t.Errorf("path = %q, want p/x.go", f.Path)
		}
		got = append(got, fmt.Sprintf("%d:%s", f.Line, strings.Join(f.Tables, ",")))
	}
	return got
}

func TestScanFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "plain literal without project is a finding",
			body: "func f() { q(\"SELECT uid FROM photos WHERE uid = $1\") }",
			want: []string{"3:photos"},
		},
		{
			name: "plain literal naming the project is scoped",
			body: "func f() { q(\"SELECT uid FROM photos WHERE project_id = $1\") }",
		},
		{
			name: "every keyword is a reference",
			body: "func f() {\nq(\"DELETE FROM albums WHERE uid = $1\")\nq(\"INSERT INTO labels (uid) VALUES ($1)\")\n" +
				"q(\"UPDATE subjects SET x = 1\")\nq(\"SELECT 1 FROM a JOIN faces f ON true\")\n}",
			want: []string{"4:albums", "5:labels", "6:subjects", "7:faces"},
		},
		{
			name: "concatenation is one statement",
			body: "func f() { q(\"SELECT uid FROM photos WHERE \" + \"project_id = $1\") }",
		},
		{
			name: "package constant used in a scoped concatenation is judged there",
			body: "const base = `SELECT uid FROM photos p`\nfunc f() { q(base + ` WHERE p.project_id = $1`) }",
		},
		{
			name: "package constant used directly is judged by itself",
			body: "const base = `SELECT uid FROM photos p`\nfunc f() { q(base) }",
			want: []string{"3:photos"},
		},
		{
			name: "constant columns resolve into the statement",
			body: "const cols = `uid, project_id`\nfunc f() { q(`SELECT ` + cols + ` FROM photos`) }",
		},
		{
			name: "Sprintf argument fills its verb",
			body: "func f() { q(fmt.Sprintf(\"SELECT uid FROM photos WHERE %s = $1\", \"project_id\")) }",
		},
		{
			name: "Sprintf with an unknown argument is a finding",
			body: "func f(w string) { q(fmt.Sprintf(\"SELECT uid FROM photos WHERE %[1]s\", w)) }",
			want: []string{"3:photos"},
		},
		{
			name: "strings.Join over a literal slice",
			body: "func f() { q(strings.Join([]string{\"SELECT uid FROM photos\", \"WHERE project_id = $1\"}, \" \")) }",
		},
		{
			name: "local accumulated with += is one statement",
			body: "func f() {\nquery := \"SELECT count(*) FROM photos\"\nquery += \" WHERE project_id = $1\"\nq(query)\n}",
		},
		{
			name: "x = x + … extends the statement",
			body: "func f() {\nquery := \"SELECT count(*) FROM photos\"\nquery = query + \" WHERE project_id = $1\"\nq(query)\n}",
		},
		{
			name: "plain reassignment starts a new statement",
			body: "func f() {\nquery := \"SELECT 1 FROM photos WHERE project_id = $1\"\nq(query)\n" +
				"query = \"DELETE FROM photos WHERE uid = $1\"\nq(query)\n}",
			want: []string{"6:photos"},
		},
		{
			name: "a where slice built by a project-named call scopes the statement",
			body: "func f(projectUID string) {\nwhere, args := buildWhere(projectUID)\n" +
				"q(\"SELECT uid FROM photos WHERE \" + strings.Join(where, \" AND \"), args)\n}",
		},
		{
			name: "a project-named piece scopes the statement",
			body: "func f() { q(\"SELECT uid FROM photos WHERE \" + projectClause(\"$1\")) }",
		},
		{
			name: "an unknown piece does not scope the statement",
			body: "func f(where string) { q(\"SELECT uid FROM photos WHERE \" + where) }",
			want: []string{"3:photos"},
		},
		{
			name: "statements split on semicolons",
			body: "func f() { q(\"UPDATE photos SET x = 1 WHERE project_id = $1; DELETE FROM photos WHERE uid = $2\") }",
			want: []string{"3:photos"},
		},
		{
			name: "a raw literal reports the line of its table",
			body: "const listSQL = `SELECT uid\n  , taken_at\nFROM photos\nWHERE uid = $1`\nfunc f() { q(listSQL) }",
			want: []string{"5:photos"},
		},
		{
			name: "one finding per statement, every unscoped table listed",
			body: "func f() { q(\"SELECT 1 FROM photos p JOIN faces f ON f.photo_uid = p.uid\") }",
			want: []string{"3:faces,photos"},
		},
		{
			name: "lower-case prose is not SQL",
			body: "func f() error { return fmt.Errorf(\"update photos: %w\", err) }",
		},
		{
			name: "child tables are not roots",
			body: "func f() { q(\"SELECT 1 FROM photo_files JOIN photo_labels ON true\") }",
		},
		{
			name: "project_invites is scoped by project_uid",
			body: "func f() {\nq(\"UPDATE project_invites SET uses = 1 WHERE project_id = $1\")\n" +
				"q(\"UPDATE project_invites SET uses = 1 WHERE project_uid = $1\")\n}",
			want: []string{"4:project_invites"},
		},
		{
			name: "a literal nested in an unknown call is scanned on its own",
			body: "func f(where string) { q(\"x\" + g(\"SELECT 1 FROM albums\") + where) }",
			want: []string{"3:albums"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scanSource(t, tt.body)
			if !slices.Equal(got, tt.want) {
				t.Errorf("findings = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFinding_keyAndString(t *testing.T) {
	t.Parallel()
	f := Finding{Path: "internal/x/y.go", Line: 12, Tables: []string{"faces", "photos"}, Statement: "SELECT 1"}
	if got := f.Key(); got != "internal/x/y.go:12" {
		t.Errorf("Key() = %q", got)
	}
	want := "internal/x/y.go:12: statement over faces, photos names no project: SELECT 1"
	if got := f.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestExcerpt(t *testing.T) {
	t.Parallel()
	if got := excerpt("  SELECT\n\t uid  FROM photos "); got != "SELECT uid FROM photos" {
		t.Errorf("excerpt = %q", got)
	}
	long := strings.Repeat("x", excerptLen+10)
	if got := excerpt(long); len(got) != excerptLen+len("…") {
		t.Errorf("excerpt length = %d", len(got))
	}
}
