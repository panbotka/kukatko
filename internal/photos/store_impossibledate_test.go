package photos

import (
	"testing"
	"time"
)

// TestCaptureYearWindow verifies the inclusive year range is turned into the
// half-open instant interval the impossible-date predicate compares against: the
// first moment of the first plausible year, up to but not including the first
// moment of the year after the last plausible one. Getting the upper edge wrong
// would report every photo taken in the final year as impossibly dated.
func TestCaptureYearWindow(t *testing.T) {
	t.Parallel()

	from, until := captureYearWindow(1826, 2027)
	if want := time.Date(1826, time.January, 1, 0, 0, 0, 0, time.UTC); !from.Equal(want) {
		t.Errorf("from = %v, want %v", from, want)
	}
	if want := time.Date(2028, time.January, 1, 0, 0, 0, 0, time.UTC); !until.Equal(want) {
		t.Errorf("until = %v, want %v", until, want)
	}

	// impossible mirrors the SQL predicate: taken_at < from OR taken_at >= until.
	impossible := func(at time.Time) bool { return at.Before(from) || !at.Before(until) }
	tests := []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "the last instant before photography", at: from.Add(-time.Nanosecond), want: true},
		{name: "the first instant of the first year", at: from, want: false},
		{name: "the last instant of the last year", at: until.Add(-time.Nanosecond), want: false},
		{name: "the first instant of the year after", at: until, want: true},
		{name: "a facebook asset id read as a date", at: time.Date(9009, 3, 10, 0, 0, 0, 0, time.UTC), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := impossible(tt.at); got != tt.want {
				t.Errorf("impossible(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}
