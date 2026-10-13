package reset

import (
	"slices"
	"testing"
)

// TestReferencingFirst verifies every table is ordered before the tables it
// references, ties fall alphabetically, edges leaving the set and
// self-references are ignored, and a cycle still yields every table once.
func TestReferencingFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tables []string
		edges  []foreignKey
		want   []string
	}{
		{
			name:   "no keys is alphabetical",
			tables: []string{"photos", "albums", "labels"},
			want:   []string{"albums", "labels", "photos"},
		},
		{
			name:   "the referencing table goes first",
			tables: []string{"photos", "embeddings", "subjects", "subject_families"},
			edges: []foreignKey{
				{from: "embeddings", to: "photos"},
				{from: "subjects", to: "photos"},
				{from: "subject_families", to: "subjects"},
				{from: "photos", to: "users"},      // leaves the set
				{from: "subjects", to: "subjects"}, // self-reference
			},
			want: []string{"embeddings", "subject_families", "subjects", "photos"},
		},
		{
			name:   "a cycle keeps every table",
			tables: []string{"a", "b", "c"},
			edges:  []foreignKey{{from: "a", to: "b"}, {from: "b", to: "a"}, {from: "c", to: "a"}},
			want:   []string{"c", "a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := referencingFirst(tt.tables, tt.edges); !slices.Equal(got, tt.want) {
				t.Errorf("referencingFirst() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPurgeStatements verifies the deletion order: the job queue before the
// catalogue tables its conditions read, the memberships and invitations last,
// and every statement scoped by the project uid.
func TestPurgeStatements(t *testing.T) {
	t.Parallel()

	statements := purgeStatements([]string{"albums", "photos"})
	tables := make([]string, len(statements))
	for i, statement := range statements {
		tables[i] = statement.table
		if statement.where == "" {
			t.Errorf("statement for %s has no condition", statement.table)
		}
	}
	want := []string{"jobs", "albums", "photos", "project_members", "project_invites"}
	if !slices.Equal(tables, want) {
		t.Errorf("tables = %v, want %v", tables, want)
	}
	for _, table := range []string{"jobs", "project_members", "project_invites"} {
		if !slices.Contains(catalogueTables, table) && !slices.Contains(preservedTables, table) {
			t.Errorf("%s is classified nowhere in the reset's lists", table)
		}
	}
}

// TestPurgePlan_Rows verifies the plan's row total.
func TestPurgePlan_Rows(t *testing.T) {
	t.Parallel()

	plan := PurgePlan{Tables: []TableCount{{Table: "photos", Rows: 3}, {Table: "jobs", Rows: 2}}}
	if got := plan.Rows(); got != 5 {
		t.Errorf("Rows() = %d, want 5", got)
	}
}
