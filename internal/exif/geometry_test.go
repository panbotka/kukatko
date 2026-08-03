package exif

import "testing"

// TestQuarterTurn covers the whole orientation domain, including the absent tag
// and out-of-range values, since the predicate gates every dimension swap.
func TestQuarterTurn(t *testing.T) {
	t.Parallel()
	for orientation, want := range map[int]bool{
		0: false, 1: false, 2: false, 3: false, 4: false,
		5: true, 6: true, 7: true, 8: true,
		9: false, -1: false,
	} {
		if got := QuarterTurn(orientation); got != want {
			t.Errorf("QuarterTurn(%d) = %v, want %v", orientation, got, want)
		}
	}
}

// TestRawDimensions checks every orientation the library actually holds — 1 (no
// rotation), 3 (180°), 6 (90° CW) and 8 (270° CW) — plus the absent tag: only the
// quarter turns exchange the sides.
func TestRawDimensions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		orientation int
		wantW       int
		wantH       int
	}{
		{name: "absent tag keeps the pair", orientation: 0, wantW: 3648, wantH: 5472},
		{name: "upright keeps the pair", orientation: 1, wantW: 3648, wantH: 5472},
		{name: "180 degrees keeps the pair", orientation: 3, wantW: 3648, wantH: 5472},
		{name: "90 CW swaps back", orientation: 6, wantW: 5472, wantH: 3648},
		{name: "270 CW swaps back", orientation: 8, wantW: 5472, wantH: 3648},
		{name: "out of range keeps the pair", orientation: 9, wantW: 3648, wantH: 5472},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// 3648 × 5472 with orientation 8 is the production photo from the bug
			// report: PhotoPrism reported the displayed (portrait) frame of a
			// 5472 × 3648 landscape original.
			gotW, gotH := RawDimensions(3648, 5472, tc.orientation)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Errorf("RawDimensions(3648, 5472, %d) = %d×%d, want %d×%d",
					tc.orientation, gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

// TestRawDimensionsIsItsOwnInverse pins the property the repair relies on:
// applying the conversion twice returns the original pair, so a re-run of a
// dimension repair can never drift.
func TestRawDimensionsIsItsOwnInverse(t *testing.T) {
	t.Parallel()
	for _, orientation := range []int{1, 3, 6, 8} {
		w, h := RawDimensions(4000, 3000, orientation)
		if gotW, gotH := RawDimensions(w, h, orientation); gotW != 4000 || gotH != 3000 {
			t.Errorf("orientation %d: round trip = %d×%d, want 4000×3000", orientation, gotW, gotH)
		}
	}
}
