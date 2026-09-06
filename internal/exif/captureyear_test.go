package exif

import (
	"testing"
	"time"
)

// TestPlausibleCaptureYear covers the bounds of the year a capture date may
// claim: the birth of photography below, one year ahead of now above.
func TestPlausibleCaptureYear(t *testing.T) {
	t.Parallel()

	thisYear := time.Now().UTC().Year()

	tests := []struct {
		name string
		year int
		want bool
	}{
		{name: "before photography", year: MinCaptureYear - 1, want: false},
		{name: "first year of photography", year: MinCaptureYear, want: true},
		{name: "this year", year: thisYear, want: true},
		{name: "next year", year: thisYear + CaptureYearLookahead, want: true},
		{name: "two years ahead", year: thisYear + CaptureYearLookahead + 1, want: false},
		{name: "facebook asset id", year: 9009, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := PlausibleCaptureYear(tt.year); got != tt.want {
				t.Errorf("PlausibleCaptureYear(%d) = %v, want %v", tt.year, got, tt.want)
			}
		})
	}
}

// TestCaptureYearBounds checks the bounds the SQL-side callers read are exactly
// the ones PlausibleCaptureYear accepts, so the guard and the integrity scan
// cannot drift apart.
func TestCaptureYearBounds(t *testing.T) {
	t.Parallel()

	minYear, maxYear := CaptureYearBounds()
	if minYear != MinCaptureYear {
		t.Errorf("min = %d, want %d", minYear, MinCaptureYear)
	}
	if want := time.Now().UTC().Year() + CaptureYearLookahead; maxYear != want {
		t.Errorf("max = %d, want %d", maxYear, want)
	}
	for _, year := range []int{minYear - 1, minYear, maxYear, maxYear + 1} {
		if got, want := PlausibleCaptureYear(year), year >= minYear && year <= maxYear; got != want {
			t.Errorf("PlausibleCaptureYear(%d) = %v, want %v", year, got, want)
		}
	}
}
