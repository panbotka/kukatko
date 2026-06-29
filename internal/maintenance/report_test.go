package maintenance

import (
	"reflect"
	"testing"
)

// TestOrphanKeys verifies the set difference returns disk keys absent from the
// catalogue, sorted, and ignores catalogued keys not on disk.
func TestOrphanKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dbPaths []string
		disk    []string
		want    []string
	}{
		{
			name:    "no orphans",
			dbPaths: []string{"a", "b"},
			disk:    []string{"a", "b"},
			want:    []string{},
		},
		{
			name:    "some orphans sorted",
			dbPaths: []string{"a"},
			disk:    []string{"c", "a", "b"},
			want:    []string{"b", "c"},
		},
		{
			name:    "catalogued-but-missing-on-disk is not an orphan",
			dbPaths: []string{"a", "missing"},
			disk:    []string{"a"},
			want:    []string{},
		},
		{
			name:    "empty disk",
			dbPaths: []string{"a"},
			disk:    nil,
			want:    []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := orphanKeys(tt.dbPaths, tt.disk)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("orphanKeys(%v, %v) = %v, want %v", tt.dbPaths, tt.disk, got, tt.want)
			}
		})
	}
}

// TestFindingFrom verifies a Finding carries the full count but caps its samples
// at the limit, preserving input order, with a non-nil samples slice.
func TestFindingFrom(t *testing.T) {
	t.Parallel()

	got := findingFrom([]string{"a", "b", "c", "d"}, 2)
	if got.Count != 4 {
		t.Errorf("Count = %d, want 4", got.Count)
	}
	if !reflect.DeepEqual(got.Samples, []string{"a", "b"}) {
		t.Errorf("Samples = %v, want [a b]", got.Samples)
	}

	empty := findingFrom(nil, 5)
	if empty.Count != 0 || empty.Samples == nil {
		t.Errorf("findingFrom(nil) = %+v, want count 0 and non-nil samples", empty)
	}
}

// TestFindingCollector verifies the collector counts every identifier but retains
// only the first limit as samples.
func TestFindingCollector(t *testing.T) {
	t.Parallel()

	c := newFindingCollector(2)
	for _, id := range []string{"x", "y", "z"} {
		c.add(id)
	}
	got := c.finding()
	if got.Count != 3 {
		t.Errorf("Count = %d, want 3", got.Count)
	}
	if !reflect.DeepEqual(got.Samples, []string{"x", "y"}) {
		t.Errorf("Samples = %v, want [x y]", got.Samples)
	}
}

// TestReportClean verifies Clean is true only when every finding is empty.
func TestReportClean(t *testing.T) {
	t.Parallel()

	if !(Report{}).Clean() {
		t.Error("zero Report should be clean")
	}
	dirty := Report{MissingThumbnails: Finding{Count: 1}}
	if dirty.Clean() {
		t.Error("Report with a finding should not be clean")
	}
}

// TestRepairOptionsAny verifies Any is true when at least one repair is selected.
func TestRepairOptionsAny(t *testing.T) {
	t.Parallel()

	if (RepairOptions{}).Any() {
		t.Error("zero RepairOptions should select nothing")
	}
	if !(RepairOptions{Faces: true}).Any() {
		t.Error("RepairOptions with Faces should select something")
	}
}
