package facejob

import (
	"math"
	"testing"
)

// bboxClose reports whether two boxes match within a small epsilon per element.
func bboxClose(a, b [4]float64) bool {
	const eps = 1e-9
	for i := range a {
		if math.Abs(a[i]-b[i]) > eps {
			return false
		}
	}
	return true
}

// TestNormalizeBBox_orientations checks the pixel→normalized conversion across
// all eight EXIF orientations. Orientations 1–4 keep the file dimensions;
// orientations 5–8 are the 90°/270° rotations, where the display dimensions are
// the file dimensions swapped, so the same relative box maps from a swapped pixel
// box. Every case is chosen to land on the same normalized [0.1, 0.1, 0.2, 0.2].
func TestNormalizeBBox_orientations(t *testing.T) {
	t.Parallel()

	const (
		fileWidth  = 1000
		fileHeight = 500
	)
	want := [4]float64{0.1, 0.1, 0.2, 0.2}

	tests := []struct {
		name        string
		orientation int
		bbox        [4]float64 // pixel [x1, y1, x2, y2] in display space
	}{
		// Display dimensions equal file dimensions (1000x500).
		{"orientation 1 (normal)", 1, [4]float64{100, 50, 300, 150}},
		{"orientation 2 (mirror h)", 2, [4]float64{100, 50, 300, 150}},
		{"orientation 3 (180)", 3, [4]float64{100, 50, 300, 150}},
		{"orientation 4 (mirror v)", 4, [4]float64{100, 50, 300, 150}},
		// Display dimensions are swapped (500x1000).
		{"orientation 5 (transpose)", 5, [4]float64{50, 100, 150, 300}},
		{"orientation 6 (rotate 90 cw)", 6, [4]float64{50, 100, 150, 300}},
		{"orientation 7 (transverse)", 7, [4]float64{50, 100, 150, 300}},
		{"orientation 8 (rotate 270 cw)", 8, [4]float64{50, 100, 150, 300}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeBBox(tt.bbox, fileWidth, fileHeight, tt.orientation)
			if !bboxClose(got, want) {
				t.Errorf("normalizeBBox(%v, %d, %d, %d) = %v, want %v",
					tt.bbox, fileWidth, fileHeight, tt.orientation, got, want)
			}
		})
	}
}

// TestNormalizeBBox_exactValues verifies the raw arithmetic for a non-square
// portrait box under orientation 1 and the corresponding swap under orientation 6.
func TestNormalizeBBox_exactValues(t *testing.T) {
	t.Parallel()

	// Orientation 1: display 800x600. Box [200,150,600,450] → [0.25,0.25,0.5,0.5].
	got := normalizeBBox([4]float64{200, 150, 600, 450}, 800, 600, 1)
	if want := [4]float64{0.25, 0.25, 0.5, 0.5}; !bboxClose(got, want) {
		t.Errorf("orientation 1: got %v, want %v", got, want)
	}

	// Orientation 6: file 800x600 → display 600x800. Box [150,200,450,600] →
	// [0.25,0.25,0.5,0.5].
	got = normalizeBBox([4]float64{150, 200, 450, 600}, 800, 600, 6)
	if want := [4]float64{0.25, 0.25, 0.5, 0.5}; !bboxClose(got, want) {
		t.Errorf("orientation 6: got %v, want %v", got, want)
	}
}

// TestNormalizeBBox_degenerate returns the box unchanged when a dimension is
// non-positive, so a missing/zero stored dimension never yields NaN or Inf.
func TestNormalizeBBox_degenerate(t *testing.T) {
	t.Parallel()

	box := [4]float64{10, 20, 30, 40}
	tests := []struct {
		name          string
		width, height int
	}{
		{"zero width", 0, 500},
		{"zero height", 500, 0},
		{"negative width", -1, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeBBox(box, tt.width, tt.height, 1); got != box {
				t.Errorf("normalizeBBox with %s = %v, want unchanged %v", tt.name, got, box)
			}
		})
	}
}
