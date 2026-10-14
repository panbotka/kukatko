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
			name: "a read of a read-scoped table without project is a finding",
			body: "func f() {\nq(\"SELECT count(*) FROM audit_log WHERE actor_uid = $1\")\n" +
				"q(\"SELECT 1 FROM users u JOIN audit_log a ON a.actor_uid = u.uid\")\n}",
			want: []string{"4:audit_log", "5:audit_log"},
		},
		{
			name: "a read of a read-scoped table naming the project is scoped",
			body: "func f() { q(\"SELECT count(*) FROM audit_log a WHERE a.project_id = $1\") }",
		},
		{
			name: "writes to a read-scoped table need no project",
			body: "func f() {\nq(\"INSERT INTO audit_log (action) VALUES ($1)\")\n" +
				"q(\"DELETE FROM audit_log WHERE created_at < $1\")\nq(\"UPDATE audit_log SET x = 1\")\n}",
		},
		{
			name: "a write that reads a read-scoped table is judged on the read",
			body: "func f() { q(\"INSERT INTO t (n) SELECT count(*) FROM audit_log\") }",
			want: []string{"3:audit_log"},
		},
		{
			name: "a project column only in the select list is a finding",
			body: "func f() { q(\"SELECT uid, project_id FROM photos WHERE uid = $1\") }",
			want: []string{"3:photos"},
		},
		{
			name: "a CTE scoped inside scopes the statement",
			body: "func f() { q(\"WITH t AS (SELECT uid FROM photos WHERE project_id = $1) SELECT uid FROM t\") }",
		},
		{
			name: "a subquery scoped inside scopes the statement",
			body: "func f() { q(\"SELECT x.uid FROM (SELECT uid FROM photos WHERE project_id = $1) x\") }",
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
			name: "package constant used directly is judged at its use",
			body: "const base = `SELECT uid FROM photos p`\nfunc f() { q(base) }",
			want: []string{"4:photos"},
		},
		{
			name: "a shared constant is judged at every use",
			body: "const base = `SELECT uid FROM photos p`\nfunc f() {\nq(base)\nr(base)\n}",
			want: []string{"5:photos", "6:photos"},
		},
		{
			name: "a shared constant is reported only where it is left unscoped",
			body: "const base = `SELECT uid FROM photos p`\nfunc f() {\nq(base + ` WHERE p.project_id = $1`)\n" +
				"q(base + ` WHERE p.uid = $1`)\n}",
			want: []string{"6:photos"},
		},
		{
			name: "a constant folded into a constant is anchored at the outer use",
			body: "const inner = `SELECT uid FROM photos p`\nconst outer = inner + ` WHERE p.uid = $1`\n" +
				"func f() { q(outer) }",
			want: []string{"5:photos"},
		},
		{
			name: "a comment names no project",
			body: "func f() { q(\"SELECT uid FROM photos WHERE uid = $1 -- AND project_id = $2\") }",
			want: []string{"3:photos"},
		},
		{
			name: "constant columns resolve into the statement",
			body: "const cols = `uid, project_id`\nfunc f() { q(`INSERT INTO photos (` + cols + `) VALUES ($1, $2)`) }",
		},
		{
			name: "constant columns only selected scope nothing",
			body: "const cols = `uid, project_id`\nfunc f() { q(`SELECT ` + cols + ` FROM photos`) }",
			want: []string{"4:photos"},
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
			body: "func f() {\nq(`SELECT uid\n  , taken_at\nFROM photos\nWHERE uid = $1`)\n}",
			want: []string{"6:photos"},
		},
		{
			name: "a raw constant reports the line of its use",
			body: "const listSQL = `SELECT uid\n  , taken_at\nFROM photos\nWHERE uid = $1`\nfunc f() { q(listSQL) }",
			want: []string{"7:photos"},
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
			name: "a whole-table read of a keyed child table is a finding",
			body: "func f() {\nq(\"SELECT photo_uid FROM photo_phashes ORDER BY phash LIMIT 1\")\n" +
				"q(\"SELECT 1 FROM photo_files JOIN photo_labels ON true\")\n}",
			want: []string{"4:photo_phashes", "5:photo_files,photo_labels"},
		},
		{
			name: "a keyed child table read by its parent key or through the project is scoped",
			body: "func f() {\nq(\"SELECT phash FROM photo_phashes WHERE photo_uid = $1\")\n" +
				"q(\"SELECT 1 FROM markers WHERE subject_uid = ANY($1)\")\n" +
				"q(\"SELECT ph.photo_uid FROM photo_phashes ph JOIN photos p ON p.uid = ph.photo_uid " +
				"WHERE p.project_id = $1\")\n}",
		},
		{
			name: "a join to the parent on its key leaves the parent judged on its own",
			body: "func f() { q(\"SELECT ph.photo_uid FROM photo_phashes ph JOIN photos p ON p.uid = ph.photo_uid\") }",
			want: []string{"3:photos"},
		},
		{
			name: "writes to a keyed child table are not judged",
			body: "func f() {\nq(\"INSERT INTO photo_phashes (photo_uid, phash) VALUES ($1, $2)\")\n" +
				"q(\"DELETE FROM photo_files\")\nq(\"UPDATE photo_edits SET x = 1\")\n}",
		},
		{
			name: "project_invites is scoped by project_uid",
			body: "func f() {\nq(\"UPDATE project_invites SET uses = 1 WHERE project_id = $1\")\n" +
				"q(\"UPDATE project_invites SET uses = 1 WHERE project_uid = $1\")\n}",
			want: []string{"4:project_invites"},
		},
		{
			name: "a project-named piece in a FROM list may be the join condition",
			body: "func f() { q(\"SELECT 1 FROM photos p\" + projectJoin(\"p\")) }",
		},
		{
			name: "a project-named piece in a select list scopes nothing",
			body: "func f() { q(\"SELECT \" + projectColumn + \" FROM photos WHERE uid = $1\") }",
			want: []string{"3:photos"},
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

// TestScanFiles_origin checks a finding anchored at a constant's use still
// names where the table sits: the constant's own line.
func TestScanFiles_origin(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	src := "package p\n\nconst listSQL = `SELECT uid\nFROM photos`\n\nfunc f() { q(listSQL) }\n"
	file, err := parser.ParseFile(fset, "/root/p/x.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	findings := ScanFiles(fset, "/root", []*ast.File{file})
	if len(findings) != 1 || findings[0].Key() != "p/x.go:6" || findings[0].Origin != "p/x.go:4" {
		t.Fatalf("findings = %+v, want one at p/x.go:6 from p/x.go:4", findings)
	}
	if !strings.Contains(findings[0].String(), "(table named at p/x.go:4)") {
		t.Errorf("String() = %q, want the origin named", findings[0].String())
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
