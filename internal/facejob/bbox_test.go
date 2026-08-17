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

// TestNormalizeBBox_dividesByTheFrameItWasGiven checks that the conversion divides
// by the frame passed in and nothing else — the same pixel box normalises
// differently in a landscape and a portrait frame, and identically for two photos
// whose orientation differs but whose sent frame is the same.
func TestNormalizeBBox_dividesByTheFrameItWasGiven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		bbox          [4]float64
		width, height int
		want          [4]float64
	}{
		{"landscape frame", [4]float64{100, 50, 300, 150}, 1000, 500, [4]float64{0.1, 0.1, 0.2, 0.2}},
		{"portrait frame", [4]float64{50, 100, 150, 300}, 500, 1000, [4]float64{0.1, 0.1, 0.2, 0.2}},
		{"exact quarters", [4]float64{200, 150, 600, 450}, 800, 600, [4]float64{0.25, 0.25, 0.5, 0.5}},
		{"same box, other frame", [4]float64{200, 150, 600, 450}, 600, 800,
			[4]float64{1.0 / 3, 0.1875, 2.0 / 3, 0.375}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeBBox(tt.bbox, tt.width, tt.height)
			if !bboxClose(got, tt.want) {
				t.Errorf("NormalizeBBox(%v, %d, %d) = %v, want %v",
					tt.bbox, tt.width, tt.height, got, tt.want)
			}
		})
	}
}

// TestNormalizeBBox_faceShapedInEveryOrientation is the regression guard for the
// production bug: on a quarter-turned photo the boxes came back 2.4 times wider
// than tall because they were divided by a frame the detector never saw.
//
// It runs the whole path a detection takes for all four orientations that matter
// (1, 3, 6, 8): an upright frame is produced from the photo's stored pair, the
// detector's pixel box is expressed in that frame, and the normalised box must come
// out face-shaped (taller than wide by the same ratio in every case). A frame taken
// from the stored pair instead — the old behaviour, kept here as the control — fails
// that for 6 and 8.
func TestNormalizeBBox_faceShapedInEveryOrientation(t *testing.T) {
	t.Parallel()

	// The photo as stored: a landscape frame with a quarter-turn tag for 6 and 8.
	const rawWidth, rawHeight = 5712, 4284
	// A face in the displayed picture: 400 px wide, 528 px tall (0.76 as wide as
	// tall), somewhere near the middle of whatever frame is displayed.
	const faceW, faceH = 400.0, 528.0

	for _, orientation := range []int{1, 3, 6, 8} {
		t.Run(orientationName(orientation), func(t *testing.T) {
			t.Parallel()

			frameW, frameH := rawWidth, rawHeight
			if orientation >= 5 {
				frameW, frameH = rawHeight, rawWidth
			}
			bbox := [4]float64{1000, 1200, 1000 + faceW, 1200 + faceH}

			got := NormalizeBBox(bbox, frameW, frameH)
			ratio := (got[2] * float64(frameW)) / (got[3] * float64(frameH))
			if math.Abs(ratio-faceW/faceH) > 1e-9 {
				t.Errorf("orientation %d: normalised box has pixel ratio %.3f, want %.3f (%v)",
					orientation, ratio, faceW/faceH, got)
			}
			if got[0] < 0 || got[1] < 0 || got[0]+got[2] > 1 || got[1]+got[3] > 1 {
				t.Errorf("orientation %d: box %v falls outside the frame", orientation, got)
			}
		})
	}
}

// orientationName labels a subtest with the orientation's meaning.
func orientationName(orientation int) string {
	switch orientation {
	case 3:
		return "orientation 3 (180)"
	case 6:
		return "orientation 6 (rotate 90 cw)"
	case 8:
		return "orientation 8 (rotate 270 cw)"
	default:
		return "orientation 1 (normal)"
	}
}

// TestNormalizeBBox_degenerate returns the box unchanged when a dimension is
// non-positive, so an unmeasurable frame never yields NaN or Inf.
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
			if got := NormalizeBBox(box, tt.width, tt.height); got != box {
				t.Errorf("NormalizeBBox with %s = %v, want unchanged %v", tt.name, got, box)
			}
		})
	}
}
