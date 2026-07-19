package audit

import (
	"encoding/json"
	"testing"
	"time"
)

// TestChangeSet_AddSkipsUnchanged verifies Add records only fields whose value
// actually changed and skips equal ones, including equal pointer values.
func TestChangeSet_AddSkipsUnchanged(t *testing.T) {
	t.Parallel()

	cs := NewChangeSet()
	cs.Add("title", "old", "new")       // changed
	cs.Add("notes", "same", "same")     // unchanged, skipped
	cs.Add("scan", false, false)        // unchanged, skipped
	cs.Add("lat", new(50.0), new(50.0)) // equal by value, skipped
	cs.Add("lng", new(14.0), new(15.0)) // changed

	if cs.Len() != 2 {
		t.Fatalf("Len() = %d, want 2 (title, lng)", cs.Len())
	}
	diff := cs.Map()
	if _, ok := diff["title"]; !ok {
		t.Errorf("changes missing title: %v", diff)
	}
	if _, ok := diff["lng"]; !ok {
		t.Errorf("changes missing lng: %v", diff)
	}
	if _, ok := diff["notes"]; ok {
		t.Errorf("changes should omit unchanged notes: %v", diff)
	}
}

// TestChangeSet_ChangeValues verifies a recorded change keeps both the old and
// the new value under the conventional keys.
func TestChangeSet_ChangeValues(t *testing.T) {
	t.Parallel()

	cs := NewChangeSet()
	cs.Add("title", "stary popisek", "novy popisek")
	change, ok := cs.Map()["title"].(Change)
	if !ok {
		t.Fatalf("title change type = %T, want Change", cs.Map()["title"])
	}
	if change.Old != "stary popisek" || change.New != "novy popisek" {
		t.Errorf("title change = %+v, want stary→novy", change)
	}
}

// TestChangeSet_MapNilWhenEmpty verifies a set with no changes yields a nil map,
// so a no-op edit records no changes key at all.
func TestChangeSet_MapNilWhenEmpty(t *testing.T) {
	t.Parallel()

	cs := NewChangeSet()
	cs.Add("title", "same", "same")
	if cs.Len() != 0 {
		t.Errorf("Len() = %d, want 0", cs.Len())
	}
	if got := cs.Map(); got != nil {
		t.Errorf("Map() = %v, want nil for an empty set", got)
	}
}

// TestChangeSet_StampInto verifies StampInto attaches the diff under ChangesKey
// only when something changed, leaving a no-op edit's details untouched.
func TestChangeSet_StampInto(t *testing.T) {
	t.Parallel()

	changed := NewChangeSet()
	changed.Add("priority", 1, 2)
	details := map[string]any{"name": "Beach"}
	changed.StampInto(details)
	if _, ok := details[ChangesKey]; !ok {
		t.Errorf("StampInto did not add %q: %v", ChangesKey, details)
	}

	noop := NewChangeSet()
	noop.Add("priority", 2, 2)
	untouched := map[string]any{"name": "Beach"}
	noop.StampInto(untouched)
	if _, ok := untouched[ChangesKey]; ok {
		t.Errorf("StampInto added %q for a no-op edit: %v", ChangesKey, untouched)
	}
}

// TestChangeSet_JSONShape verifies the stamped diff serialises to the documented
// {field:{old,new}} shape, with a nil pointer becoming JSON null.
func TestChangeSet_JSONShape(t *testing.T) {
	t.Parallel()

	cs := NewChangeSet()
	cs.Add("taken_at", new(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)), (*time.Time)(nil))
	details := map[string]any{}
	cs.StampInto(details)

	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	var decoded map[string]map[string]map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	takenAt := decoded[ChangesKey]["taken_at"]
	if takenAt["old"] != "2020-01-02T03:04:05Z" {
		t.Errorf("old = %v, want RFC3339 timestamp", takenAt["old"])
	}
	if takenAt["new"] != nil {
		t.Errorf("new = %v, want null for a cleared pointer", takenAt["new"])
	}
}
