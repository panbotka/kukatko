package maintenance

import (
	"reflect"
	"testing"
)

// TestKeySet verifies the catalogued paths become a lookup set the store listing
// can be streamed against, including for an empty catalogue.
func TestKeySet(t *testing.T) {
	t.Parallel()

	got := keySet([]string{"a", "b", "a"})
	want := map[string]struct{}{"a": {}, "b": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keySet = %v, want %v", got, want)
	}
	if len(keySet(nil)) != 0 {
		t.Errorf("keySet(nil) = %v, want an empty set", keySet(nil))
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
	// A store nobody could list has findings nobody could compute, so the verdict
	// must not be "everything agrees".
	unread := Report{Store: StoreInventory{Kind: StoreObject, Error: "listing bucket: timeout"}}
	if unread.Clean() {
		t.Error("Report whose store listing failed should not be clean")
	}
	if unread.Store.Listed() {
		t.Error("StoreInventory carrying an error should not report itself listed")
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
