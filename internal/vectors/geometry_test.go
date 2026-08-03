package vectors

import (
	"math"
	"testing"
)

// almostEqual compares two normalised coordinates with a tolerance that is far
// below anything visible (a millionth of the frame) but well above float noise.
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestRenormalizeTransposedBBox covers the four orientations the library actually
// holds. 1 and 3 leave the box alone (their display frame is the stored frame);
// 6 and 8 rescale it per axis, and the fixture is the reported production case —
// a 5472 × 3648 original displayed 3648 × 5472.
func TestRenormalizeTransposedBBox(t *testing.T) {
	t.Parallel()
	// A square 240 px face at pixel (912, 1368) of the DISPLAYED 3648 × 5472
	// frame. Divided by the transposed frame (5472 × 3648) it was stored as
	// 912/5472, 1368/3648, 240/5472, 240/3648.
	stored := [4]float64{912.0 / 5472, 1368.0 / 3648, 240.0 / 5472, 240.0 / 3648}
	want := [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472}

	tests := []struct {
		name        string
		orientation int
		want        [4]float64
	}{
		{name: "upright leaves the box alone", orientation: 1, want: stored},
		{name: "180 degrees leaves the box alone", orientation: 3, want: stored},
		{name: "90 CW rescales per axis", orientation: 6, want: want},
		{name: "270 CW rescales per axis", orientation: 8, want: want},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RenormalizeTransposedBBox(stored, 5472, 3648, tc.orientation)
			for i := range got {
				if !almostEqual(got[i], tc.want[i]) {
					t.Errorf("component %d = %v, want %v (full %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestRenormalizeTransposedBBoxKeepsFacesSquare is the property the bug report
// measured: a face that is square in pixels must come out with w/h equal to the
// DISPLAY frame's height/width ratio. The three markers on the reported photo all
// measured 1.500 against a 3648 × 5472 display frame — 5472/3648 — which is what
// a correctly normalised box looks like there.
func TestRenormalizeTransposedBBoxKeepsFacesSquare(t *testing.T) {
	t.Parallel()
	stored := [4]float64{912.0 / 5472, 1368.0 / 3648, 240.0 / 5472, 240.0 / 3648}
	got := RenormalizeTransposedBBox(stored, 5472, 3648, 8)
	ratio := got[2] / got[3]
	if !almostEqual(ratio, 5472.0/3648.0) {
		t.Errorf("w/h = %v, want %v", ratio, 5472.0/3648.0)
	}
	// The bug: before the fix the same box measured the reciprocal.
	if almostEqual(stored[2]/stored[3], 5472.0/3648.0) {
		t.Fatal("fixture is not the transposed normalisation it claims to be")
	}
}

// TestRenormalizeTransposedBBoxDegenerate keeps a missing or zero frame from
// producing NaN/Inf coordinates.
func TestRenormalizeTransposedBBoxDegenerate(t *testing.T) {
	t.Parallel()
	box := [4]float64{0.1, 0.2, 0.3, 0.4}
	for _, tc := range []struct{ w, h int }{{0, 3648}, {5472, 0}, {-1, -1}} {
		if got := RenormalizeTransposedBBox(box, tc.w, tc.h, 8); got != box {
			t.Errorf("frame %dx%d: got %v, want the box unchanged", tc.w, tc.h, got)
		}
	}
}
