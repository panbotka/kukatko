package photos

import (
	"strings"
	"testing"
)

// TestAccumulate verifies that cumulative counts are the running sum of the
// counts of the preceding (newer) buckets, so the first bucket starts at zero.
func TestAccumulate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		buckets []TimelineBucket
		want    []int // expected Cumulative per bucket, in order
	}{
		{
			name:    "empty",
			buckets: nil,
			want:    nil,
		},
		{
			name:    "single bucket starts at zero",
			buckets: []TimelineBucket{{Year: 2023, Month: 12, Count: 5}},
			want:    []int{0},
		},
		{
			name: "running sum of preceding counts",
			buckets: []TimelineBucket{
				{Year: 2023, Month: 12, Count: 2},
				{Year: 2023, Month: 6, Count: 1},
				{Year: 2022, Month: 1, Count: 3},
			},
			want: []int{0, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			accumulate(tt.buckets)
			for i, want := range tt.want {
				if tt.buckets[i].Cumulative != want {
					t.Errorf("bucket[%d].Cumulative = %d, want %d", i, tt.buckets[i].Cumulative, want)
				}
			}
		})
	}
}

// TestBuildTimelineQuery verifies the aggregation SQL always guards on a known
// capture time, groups and orders by month newest-first, and folds the shared
// List/Count filters (here the default archived-only-live clause) into the WHERE.
func TestBuildTimelineQuery(t *testing.T) {
	t.Parallel()

	query, args := buildTimelineQuery(ListParams{})
	if len(args) != 0 {
		t.Fatalf("default params args = %v, want none", args)
	}
	for _, want := range []string{
		"taken_at IS NOT NULL",
		"archived_at IS NULL",
		"GROUP BY year, month",
		"ORDER BY year DESC, month DESC",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q missing %q", query, want)
		}
	}

	// A scoped filter binds its value and is ANDed onto the guard.
	scoped, scopedArgs := buildTimelineQuery(ListParams{AlbumUID: "al1"})
	if len(scopedArgs) != 1 || scopedArgs[0] != "al1" {
		t.Fatalf("album scope args = %v, want [al1]", scopedArgs)
	}
	if !strings.Contains(scoped, "taken_at IS NOT NULL AND") {
		t.Errorf("scoped query %q should AND the filter onto the guard", scoped)
	}
}
