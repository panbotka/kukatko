package tenancyguard

import (
	"maps"
	"slices"
	"testing"
)

func TestPredicateColumns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stmt string
		want []string
	}{
		{name: "WHERE predicate", stmt: "SELECT uid FROM photos WHERE uid = $1 AND project_id = $2", want: []string{"project_id"}},
		{name: "lower-case keywords", stmt: "select uid from photos where project_id = $1", want: []string{"project_id"}},
		{name: "qualified column", stmt: "SELECT 1 FROM photos p WHERE p.project_id = $1", want: []string{"project_id"}},
		{name: "bracketed condition stays a condition", stmt: "SELECT 1 FROM photos WHERE (uid = $1 AND project_id = $2)", want: []string{"project_id"}},
		{
			name: "JOIN ON condition",
			stmt: "SELECT 1 FROM photo_files f JOIN photos p ON p.uid = f.photo_uid AND p.project_id = $1",
			want: []string{"project_id"},
		},
		{name: "INSERT column list", stmt: "INSERT INTO photos (uid, project_id) VALUES ($1, $2)", want: []string{"project_id"}},
		{name: "UPDATE SET list", stmt: "UPDATE photos SET project_id = $2 WHERE uid = $1", want: []string{"project_id"}},
		{
			name: "ON CONFLICT target",
			stmt: "INSERT INTO labels (uid) VALUES ($1) ON CONFLICT (project_id, uid) DO NOTHING",
			want: []string{"project_id"},
		},
		{name: "project_uid in a predicate", stmt: "UPDATE project_invites SET uses = 1 WHERE project_uid = $1", want: []string{"project_uid"}},
		{
			name: "CTE with the predicate inside",
			stmt: "WITH t AS (SELECT uid FROM photos WHERE project_id = $1) SELECT uid FROM t",
			want: []string{"project_id"},
		},
		{
			name: "subquery with the predicate inside",
			stmt: "SELECT x.uid FROM (SELECT uid FROM photos WHERE project_id = $1) x ORDER BY x.uid",
			want: []string{"project_id"},
		},
		{
			name: "IS DISTINCT FROM keeps the predicate open",
			stmt: "SELECT uid FROM albums WHERE created_by IS DISTINCT FROM $1 AND project_id = $2",
			want: []string{"project_id"},
		},
		{
			name: "a closed function call returns to the predicate",
			stmt: "SELECT 1 FROM photos WHERE extract(year FROM taken_at) = $1 AND project_id = $2",
			want: []string{"project_id"},
		},
		{name: "quoted keywords are skipped", stmt: "SELECT 1 FROM photos WHERE kind = 'FROM' AND project_id = $1", want: []string{"project_id"}},
		{name: "project mark in a predicate", stmt: "SELECT 1 FROM photos WHERE " + projectMark, want: []string{projectMark}},
		{name: "project mark in a FROM list", stmt: "SELECT 1 FROM photos p " + projectMark, want: []string{projectMark}},
		{name: "select list only", stmt: "SELECT uid, project_id FROM photos WHERE uid = $1"},
		{name: "project mark in a select list", stmt: "SELECT " + projectMark + " FROM photos WHERE uid = $1"},
		{name: "RETURNING only", stmt: "UPDATE photos SET title = $2 WHERE uid = $1 RETURNING project_id"},
		{name: "ORDER BY only", stmt: "SELECT uid FROM photos WHERE uid = ANY($1) ORDER BY project_id"},
		{name: "GROUP BY only", stmt: "SELECT project_id, count(*) FROM photos GROUP BY project_id"},
		{name: "VALUES only", stmt: "INSERT INTO photos (uid) VALUES ($1) RETURNING uid, project_id"},
		{name: "DISTINCT ON is a select list", stmt: "SELECT DISTINCT ON (project_id) uid FROM photos ORDER BY project_id"},
		{name: "a FROM list names no condition", stmt: "SELECT 1 FROM photos project_id"},
		{name: "a column only spelled like it", stmt: "SELECT 1 FROM photos WHERE photo_project_id = $1"},
		{
			name: "CTE selecting the column only",
			stmt: "WITH t AS (SELECT uid, project_id FROM photos WHERE uid = $1) SELECT * FROM t",
		},
		{
			name: "subquery in the select list selecting the column only",
			stmt: "SELECT (SELECT project_id FROM photos WHERE uid = $1)",
		},
		{name: "unbalanced parentheses do not panic", stmt: "SELECT 1)) FROM photos WHERE (project_id = $1", want: []string{"project_id"}},
		{name: "unterminated quote ends the scan", stmt: "SELECT 1 FROM photos WHERE kind = 'x AND project_id = $1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := slices.Sorted(maps.Keys(predicateColumns(tt.stmt)))
			if !slices.Equal(got, tt.want) && (len(got) > 0 || len(tt.want) > 0) {
				t.Errorf("predicateColumns(%q) = %q, want %q", tt.stmt, got, tt.want)
			}
		})
	}
}
