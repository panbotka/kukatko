package audit

import "reflect"

// ChangesKey is the key under which an edit's field-level before/after diff is
// stored in an Entry's Details. Writers stamp the diff here and the admin audit
// page reads it back, so the name lives as one constant shared by both sides
// rather than a string literal repeated across packages.
const ChangesKey = "changes"

// Change is one field's transition in an edit's audit details: the value before
// the edit and the value after it. Both sides are serialised to JSON as they are
// given, so a nil pointer (a cleared date, coordinate or cover) is recorded as
// JSON null rather than dropped.
type Change struct {
	// Old is the field's value before the edit.
	Old any `json:"old"`
	// New is the field's value after the edit.
	New any `json:"new"`
}

// ChangeSet accumulates the fields an edit changed so an audit entry can record
// the previous value alongside the new one (old → new). Build one with
// NewChangeSet, Add each candidate field with its before/after values — a field
// whose value did not change is skipped — and attach the result to an Entry's
// Details under ChangesKey with StampInto (or read it back with Map). Keeping the
// diff-building in one helper is the documented convention every edit path uses,
// so the audit payload stays consistent (see docs/ARCHITECTURE.md §5.1).
type ChangeSet struct {
	changes map[string]Change
}

// NewChangeSet returns an empty ChangeSet ready to accumulate changed fields.
func NewChangeSet() *ChangeSet {
	return &ChangeSet{changes: make(map[string]Change)}
}

// Add records field's transition from oldValue to newValue when the two differ,
// and is a no-op when they are equal so an unchanged field never enters the set.
// Values are compared with reflect.DeepEqual, so pointer fields (*time.Time,
// *float64, *string) diff by the value they point at and a nil pointer differs
// from a set one.
func (c *ChangeSet) Add(field string, oldValue, newValue any) {
	if reflect.DeepEqual(oldValue, newValue) {
		return
	}
	c.changes[field] = Change{Old: oldValue, New: newValue}
}

// Len reports how many fields have been recorded as changed, so a caller can
// tell an edit that touched something from a no-op.
func (c *ChangeSet) Len() int {
	return len(c.changes)
}

// Map returns the accumulated {field: {old, new}} diff for embedding under
// ChangesKey in an Entry's Details, or nil when no field changed so a no-op edit
// records no changes key at all. Each value is a Change, which serialises to
// {"old":…,"new":…}.
func (c *ChangeSet) Map() map[string]any {
	if len(c.changes) == 0 {
		return nil
	}
	out := make(map[string]any, len(c.changes))
	for field, change := range c.changes {
		out[field] = change
	}
	return out
}

// StampInto records the accumulated diff into details under ChangesKey when any
// field changed, and leaves details untouched for a no-op edit. It lets a caller
// build its summary details map and attach the diff in one line.
func (c *ChangeSet) StampInto(details map[string]any) {
	if diff := c.Map(); diff != nil {
		details[ChangesKey] = diff
	}
}
