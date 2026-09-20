package phototask

import (
	"strings"
	"testing"
)

// TestBuildSummarySQL_coversEveryState guards the generated statement: one
// FILTER per known state, in the order Summary scans them, plus the two
// caller-relative counts over the fragments the listing uses.
func TestBuildSummarySQL_coversEveryState(t *testing.T) {
	t.Parallel()
	sql := buildSummarySQL()

	last := -1
	for _, state := range States {
		needle := "count(*) FILTER (WHERE t.state = '" + string(state) + "')"
		at := strings.Index(sql, needle)
		if at < 0 {
			t.Errorf("summary SQL lacks a count for state %q", state)
			continue
		}
		if at < last {
			t.Errorf("state %q is counted out of order", state)
		}
		last = at
	}
	for _, fragment := range []string{waitingSQL, answeredSQL, threadJoin, activityJoin} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("summary SQL does not reuse the fragment %q", fragment[:40])
		}
	}
	if strings.Contains(sql, "pa ON TRUE") {
		t.Error("summary SQL aggregates participants, which nothing reads")
	}
}
