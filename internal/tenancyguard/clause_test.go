package tenancyguard

import (
	"slices"
	"strings"
	"testing"
)

// unscopedTables returns the tables unscopedRefs reports for stmt, in order.
func unscopedTables(stmt string) []string {
	refs := unscopedRefs(stmt)
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.table)
	}
	return out
}

// TestUnscopedRefs_scoped lists statements every item of which is tied to one
// project: the forms the predicate rule accepts.
func TestUnscopedRefs_scoped(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, stmt string }{
		{"WHERE predicate", "SELECT uid FROM photos WHERE uid = $1 AND project_id = $2"},
		{"lower-case keywords", "select uid from photos p where p.project_id = $1"},
		{"qualified column", "SELECT 1 FROM photos p WHERE p.project_id = $1"},
		{"value on the left", "SELECT 1 FROM photos p WHERE $1 = p.project_id"},
		{"AS alias", "SELECT 1 FROM photos AS p WHERE p.project_id = $1"},
		{"table name as qualifier", "SELECT 1 FROM photos WHERE photos.project_id = $1"},
		{"bracketed condition stays a condition", "SELECT 1 FROM photos WHERE (uid = $1 AND project_id = $2)"},
		{"cast parameter", "SELECT 1 FROM photos WHERE project_id = $1::text"},
		{"scalar subquery value", "SELECT 1 FROM photos WHERE project_id = (SELECT uid FROM projects WHERE slug = $1)"},
		{"BETWEEN keeps its AND", "SELECT 1 FROM photos WHERE taken_at BETWEEN $1 AND $2 AND project_id = $3"},
		{
			"JOIN tied to a scoped alias",
			"SELECT 1 FROM photos p JOIN albums a ON a.uid = p.album_uid AND a.project_id = p.project_id " +
				"WHERE p.project_id = $1",
		},
		{
			"equality spreads both ways",
			"SELECT 1 FROM photos p JOIN albums a ON p.project_id = a.project_id WHERE a.project_id = $1",
		},
		{"comma join, both scoped", "SELECT 1 FROM photos p, albums a WHERE p.project_id = $1 AND a.project_id = $1"},
		{"INSERT column list", "INSERT INTO photos (uid, project_id) VALUES ($1, $2)"},
		{"ON CONFLICT target", "INSERT INTO labels (uid) VALUES ($1) ON CONFLICT (project_id, uid) DO NOTHING"},
		{
			"INSERT … SELECT scoped in its SELECT",
			"INSERT INTO subject_families (uid, project_id) SELECT $1, s.project_id FROM subjects s " +
				"WHERE s.uid = $2 AND s.project_id = $3",
		},
		{"UPDATE scoped in WHERE", "UPDATE photos SET title = $2 WHERE uid = $1 AND project_id = $3"},
		{"DELETE scoped in WHERE", "DELETE FROM photos WHERE uid = $1 AND project_id = $2"},
		{"project_uid in a predicate", "UPDATE project_invites SET uses = 1 WHERE project_uid = $1"},
		{
			"CTE scoped inside",
			"WITH t AS (SELECT uid FROM photos WHERE project_id = $1) SELECT uid FROM t",
		},
		{
			"subquery scoped inside",
			"SELECT x.uid FROM (SELECT uid FROM photos WHERE project_id = $1) x ORDER BY x.uid",
		},
		{
			"correlated subquery tied to the outer alias",
			"SELECT 1 FROM subjects s WHERE s.project_id = $1 AND EXISTS " +
				"(SELECT 1 FROM faces f WHERE f.subject_uid = s.uid AND f.project_id = s.project_id)",
		},
		{
			"UNION, each arm scoped",
			"SELECT uid FROM photos WHERE project_id = $1 UNION SELECT uid FROM albums WHERE project_id = $1",
		},
		{
			"IS DISTINCT FROM keeps the predicate open",
			"SELECT uid FROM albums WHERE created_by IS DISTINCT FROM $1 AND project_id = $2",
		},
		{
			"a closed function call returns to the predicate",
			"SELECT 1 FROM photos WHERE extract(year FROM taken_at) = $1 AND project_id = $2",
		},
		{"quoted keywords are skipped", "SELECT 1 FROM photos WHERE kind = 'FROM x' AND project_id = $1"},
		{"FOR UPDATE is no target", "SELECT 1 FROM photos WHERE uid = $1 AND project_id = $2 FOR UPDATE"},
		{"project mark as a conjunct", "SELECT 1 FROM photos WHERE uid = $1 AND " + projectMark},
		{"project mark in a FROM list", "SELECT 1 FROM photos p " + projectMark},
		{"projects table by its uid", "SELECT 1 FROM photos p JOIN projects pr ON pr.uid = p.project_id WHERE pr.uid = $1"},
		{"keyed child by its parent key", "SELECT phash FROM photo_phashes WHERE photo_uid = $1"},
		{"keyed child by a qualified ANY", "SELECT 1 FROM markers m WHERE m.subject_uid = ANY($1)"},
		{
			"keyed child joined to a scoped parent",
			"SELECT 1 FROM photo_phashes ph JOIN photos p ON p.uid = ph.photo_uid WHERE p.project_id = $1",
		},
		{"keyed child JOIN … USING its key", "SELECT 1 FROM photos p JOIN photo_labels USING (photo_uid) WHERE p.project_id = $1"},
		{"writes to a keyed child table", "DELETE FROM photo_files WHERE size = 0"},
		{"writes to a read-scoped table", "INSERT INTO audit_log (action) VALUES ($1)"},
		{"a function in FROM is no table", "SELECT 1 FROM unnest($1::text[]) AS u(uid)"},
		{"lower-case prose is not SQL", "update photos: no rows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := unscopedTables(tt.stmt); len(got) > 0 {
				t.Errorf("unscopedRefs(%q) = %q, want none", tt.stmt, got)
			}
		})
	}
}

// TestUnscopedRefs_unscoped lists the statements the guard must catch — every
// bypass the per-statement rule let through among them — with the tables it
// must name.
func TestUnscopedRefs_unscoped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stmt string
		want []string
	}{
		{name: "no predicate", stmt: "SELECT uid FROM photos WHERE uid = $1", want: []string{"photos"}},
		{name: "OR bypass", stmt: "SELECT 1 FROM photos WHERE uid = $1 OR project_id = $2", want: []string{"photos"}},
		{
			name: "optional project under OR",
			stmt: "SELECT 1 FROM albums WHERE ($1::text IS NULL OR project_id = $1)",
			want: []string{"albums"},
		},
		{name: "IS NOT NULL bypass", stmt: "SELECT 1 FROM photos WHERE project_id IS NOT NULL", want: []string{"photos"}},
		{name: "NOT bypass", stmt: "SELECT 1 FROM photos WHERE NOT (project_id = $1)", want: []string{"photos"}},
		{name: "inequality", stmt: "SELECT 1 FROM photos WHERE project_id <> $1", want: []string{"photos"}},
		{name: "a set of projects", stmt: "SELECT 1 FROM photos WHERE project_id = ANY($1)", want: []string{"photos"}},
		{name: "compared with NULL", stmt: "SELECT 1 FROM photos WHERE project_id = NULL", want: []string{"photos"}},
		{
			name: "compared with a plain column",
			stmt: "SELECT 1 FROM photos p WHERE p.project_id = p.owner_uid",
			want: []string{"photos"},
		},
		{
			name: "wrong alias",
			stmt: "SELECT 1 FROM photos p JOIN albums a ON a.uid = p.album_uid WHERE a.project_id = $1",
			want: []string{"photos"},
		},
		{
			name: "join by a foreign key is no project",
			stmt: "SELECT 1 FROM photos p JOIN subjects s ON s.uid = p.subject_uid WHERE p.project_id = $1",
			want: []string{"subjects"},
		},
		{
			name: "comment bypass",
			stmt: "SELECT 1 FROM photos WHERE uid = $1 -- AND project_id = $2\n",
			want: []string{"photos"},
		},
		{
			name: "block comment bypass",
			stmt: "SELECT 1 FROM photos WHERE uid = $1 /* AND project_id = $2 */",
			want: []string{"photos"},
		},
		{
			name: "INSERT … SELECT reads unscoped",
			stmt: "INSERT INTO subject_families (uid, project_id) SELECT $1, s.project_id FROM subjects s WHERE s.uid = $2",
			want: []string{"subjects"},
		},
		{
			name: "VALUES subquery reads unscoped",
			stmt: "INSERT INTO faces (photo_uid, project_id) VALUES ($1, (SELECT project_id FROM photos WHERE uid = $1))",
			want: []string{"photos"},
		},
		{name: "INSERT without the column", stmt: "INSERT INTO labels (uid) VALUES ($1)", want: []string{"labels"}},
		{name: "UPDATE SET writes, never scopes", stmt: "UPDATE photos SET project_id = $2 WHERE uid = $1", want: []string{"photos"}},
		{
			name: "UPDATE … FROM a second table",
			stmt: "UPDATE photos p SET title = a.title FROM albums a WHERE a.uid = p.album_uid AND p.project_id = $1",
			want: []string{"albums"},
		},
		{name: "select list only", stmt: "SELECT uid, project_id FROM photos WHERE uid = $1", want: []string{"photos"}},
		{name: "RETURNING only", stmt: "DELETE FROM photos WHERE uid = $1 RETURNING project_id", want: []string{"photos"}},
		{name: "ORDER BY only", stmt: "SELECT uid FROM photos ORDER BY project_id", want: []string{"photos"}},
		{name: "GROUP BY only", stmt: "SELECT project_id, count(*) FROM photos GROUP BY project_id", want: []string{"photos"}},
		{
			name: "FILTER (WHERE …) in a select list",
			stmt: "SELECT count(*) FILTER (WHERE project_id = $1) FROM photos",
			want: []string{"photos"},
		},
		{name: "DISTINCT ON is a select list", stmt: "SELECT DISTINCT ON (project_id) uid FROM photos", want: []string{"photos"}},
		{
			name: "the predicate of one table scopes no other",
			stmt: "SELECT 1 FROM photos p JOIN faces f ON f.photo_uid = p.uid WHERE p.project_id = $1",
			want: []string{"faces"},
		},
		{
			name: "comma join",
			stmt: "SELECT 1 FROM albums a, photos p WHERE a.project_id = $1",
			want: []string{"photos"},
		},
		{
			name: "CTE selecting the column only",
			stmt: "WITH t AS (SELECT uid, project_id FROM photos WHERE uid = $1) SELECT * FROM t",
			want: []string{"photos"},
		},
		{
			name: "an inner predicate scopes no outer table",
			stmt: "SELECT 1 FROM albums a WHERE a.uid IN (SELECT album_uid FROM photos WHERE project_id = $1)",
			want: []string{"albums"},
		},
		{
			name: "a correlated subquery is judged on its own",
			stmt: "SELECT 1 FROM subjects s WHERE s.project_id = $1 AND EXISTS " +
				"(SELECT 1 FROM faces f WHERE f.subject_uid = s.uid)",
			want: []string{"faces"},
		},
		{
			name: "UNION arm without a predicate",
			stmt: "SELECT uid FROM photos WHERE project_id = $1 UNION SELECT uid FROM albums WHERE uid = $2",
			want: []string{"albums"},
		},
		{
			name: "membership join is no project",
			stmt: "SELECT 1 FROM photos p JOIN project_members m ON m.project_uid = p.project_id WHERE m.user_uid = $1",
			want: []string{"photos"},
		},
		{name: "project mark in a select list", stmt: "SELECT " + projectMark + " FROM photos WHERE uid = $1", want: []string{"photos"}},
		{name: "project mark under OR", stmt: "SELECT 1 FROM photos WHERE uid = $1 OR " + projectMark, want: []string{"photos"}},
		{name: "a column only spelled like it", stmt: "SELECT 1 FROM photos WHERE photo_project_id = $1", want: []string{"photos"}},
		{name: "project_invites wants project_uid", stmt: "UPDATE project_invites SET uses = 1 WHERE project_id = $1", want: []string{"project_invites"}},
		{name: "read of a read-scoped table", stmt: "SELECT count(*) FROM audit_log WHERE actor_uid = $1", want: []string{"audit_log"}},
		{
			name: "a write that reads a read-scoped table",
			stmt: "INSERT INTO t (n) SELECT count(*) FROM audit_log",
			want: []string{"audit_log"},
		},
		{name: "whole-table read of a keyed child", stmt: "SELECT photo_uid FROM photo_phashes ORDER BY phash LIMIT 1", want: []string{"photo_phashes"}},
		{name: "a NULL test keys nothing", stmt: "SELECT 1 FROM markers m WHERE m.subject_uid IS NULL", want: []string{"markers"}},
		{
			name: "a parent join leaves the parent judged",
			stmt: "SELECT ph.photo_uid FROM photo_phashes ph JOIN photos p ON p.uid = ph.photo_uid",
			want: []string{"photos"},
		},
		{name: "unbalanced parentheses do not panic", stmt: "SELECT 1)) FROM photos WHERE (uid = $1", want: []string{"photos"}},
		{name: "unterminated quote ends the scan", stmt: "SELECT 1 FROM photos WHERE kind = 'x AND project_id = $1", want: []string{"photos"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := unscopedTables(tt.stmt); !slices.Equal(got, tt.want) && (len(got) > 0 || len(tt.want) > 0) {
				t.Errorf("unscopedRefs(%q) = %q, want %q", tt.stmt, got, tt.want)
			}
		})
	}
}

// TestUnscopedRefs_locatesTheTable checks a reported item points at its table
// name in the statement, which is how a finding finds its source line.
func TestUnscopedRefs_locatesTheTable(t *testing.T) {
	t.Parallel()
	stmt := "SELECT 1 FROM photos p JOIN faces f ON f.photo_uid = p.uid WHERE p.project_id = $1"
	refs := unscopedRefs(stmt)
	if len(refs) != 1 || stmt[refs[0].start:refs[0].end] != "faces" {
		t.Fatalf("unscopedRefs = %+v, want the faces reference", refs)
	}
}

func TestBlankComments(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"a -- b\nc", "a     \nc"},
		{"a /* b\nc */ d", "a     \n     d"},
		{"a '-- not' b", "a '-- not' b"},
		{"a '/* not */' b", "a '/* not */' b"},
		{"a /* open", "a        "},
		{"a 'open", "a 'open"},
	}
	for _, tt := range tests {
		got := blankComments(tt.in)
		if got != tt.want {
			t.Errorf("blankComments(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if len(got) != len(tt.in) {
			t.Errorf("blankComments(%q) changed the length: %d", tt.in, len(got))
		}
	}
}

func TestTokenize(t *testing.T) {
	t.Parallel()
	toks, _ := tokenize(`SELECT "Photo".x, p.a<=$12 FROM (t) WHERE 'q''s' `+projectMark, 0, false)
	var got []string
	for _, tk := range toks {
		switch tk.kind {
		case tokGroup:
			got = append(got, "(")
		case tokString:
			got = append(got, "'")
		case tokMark:
			got = append(got, "MARK")
		default:
			got = append(got, tk.text)
		}
	}
	want := "SELECT Photo . x , p . a <= $12 FROM ( WHERE ' ' MARK"
	if s := strings.Join(got, " "); s != want {
		t.Errorf("tokens = %q, want %q", s, want)
	}
}
