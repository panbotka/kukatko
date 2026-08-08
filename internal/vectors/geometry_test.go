package vectors

import (
	"math"
	"testing"
)

// almostEqual compares two normalised coordinates with a tolerance that is far
// below anything visible (a millionth of the frame) but well above float noise.
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// centreOf returns the centre of a normalised box.
func centreOf(b [4]float64) (float64, float64) { return b[0] + b[2]/2, b[1] + b[3]/2 }

// boxAround builds a normalised box of the given size centred on (cx, cy).
func boxAround(cx, cy, w, h float64) [4]float64 {
	return [4]float64{cx - w/2, cy - h/2, w, h}
}

// productionFace is the row the bug report measured against the live catalogue:
// photo `phqale6fftf3a3v5tn17vtfd3d`, stored 3000 × 4000 with orientation 6 while
// its original is really 4000 × 3000, and a detection whose numbers are a correct
// box in the RAW frame — the one nobody ever displays.
func productionFace() TransposedFace {
	return TransposedFace{
		PhotoUID:    "phqale6fftf3a3v5tn17vtfd3d",
		FaceIndex:   0,
		BBox:        [4]float64{0.21085, 0.68267, 0.21971, 0.14213},
		Orientation: 6,
		RawWidth:    4000,
		RawHeight:   3000,
	}
}

// productionMarker is the PhotoPrism marker that detection belongs to: a face
// region a person has seen sit on the displayed image, whose centre the reported
// measurement puts 0.019 from the centre of the ROTATED box.
func productionMarker() [4]float64 {
	cx, cy := centreOf(RotateRawBBox(productionFace().BBox, 6))
	return boxAround(cx+0.019, cy, 0.12, 0.16)
}

// TestRotateRawBBox covers every quarter turn plus the orientations that do not
// swap the sides. The fixture is a small box in the top-left quadrant, so each
// turn lands it in a different, recognisable corner.
func TestRotateRawBBox(t *testing.T) {
	t.Parallel()
	box := [4]float64{0.1, 0.2, 0.3, 0.4}

	tests := []struct {
		name        string
		orientation int
		want        [4]float64
	}{
		{name: "transpose", orientation: 5, want: [4]float64{0.2, 0.1, 0.4, 0.3}},
		{name: "90 clockwise", orientation: 6, want: [4]float64{0.4, 0.1, 0.4, 0.3}},
		{name: "transverse", orientation: 7, want: [4]float64{0.4, 0.6, 0.4, 0.3}},
		{name: "270 clockwise", orientation: 8, want: [4]float64{0.2, 0.6, 0.4, 0.3}},
		{name: "upright leaves the box alone", orientation: 1, want: box},
		{name: "180 degrees leaves the box alone", orientation: 3, want: box},
		{name: "absent tag leaves the box alone", orientation: 0, want: box},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RotateRawBBox(box, tc.orientation)
			for i := range got {
				if !almostEqual(got[i], tc.want[i]) {
					t.Errorf("component %d = %v, want %v (full %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestRotateRawBBoxStaysInFrame is the property that makes the turn safe to apply
// to any row: a box inside the raw frame is still inside the display frame after
// it, whichever quarter turn was applied.
func TestRotateRawBBoxStaysInFrame(t *testing.T) {
	t.Parallel()
	for _, orientation := range []int{5, 6, 7, 8} {
		for _, box := range [][4]float64{
			{0, 0, 1, 1}, {0.9, 0.9, 0.1, 0.1}, {0.21085, 0.68267, 0.21971, 0.14213},
		} {
			if got := RotateRawBBox(box, orientation); !insideFrame(got) {
				t.Errorf("orientation %d: %v turned into %v, which is off the photo",
					orientation, box, got)
			}
		}
	}
}

// TestRenormalizeTransposedBBox checks the rescale candidate's arithmetic: x and w
// scale up by rawWidth/rawHeight and y and h down by the same factor, and only for
// a quarter turn.
func TestRenormalizeTransposedBBox(t *testing.T) {
	t.Parallel()
	// A square 240 px face at pixel (912, 1368) of the DISPLAYED 3648 × 5472
	// frame, divided by that pair swapped.
	stored := [4]float64{912.0 / 5472, 1368.0 / 3648, 240.0 / 5472, 240.0 / 3648}
	rescaled := [4]float64{912.0 / 3648, 1368.0 / 5472, 240.0 / 3648, 240.0 / 5472}

	tests := []struct {
		name        string
		orientation int
		want        [4]float64
	}{
		{name: "upright leaves the box alone", orientation: 1, want: stored},
		{name: "180 degrees leaves the box alone", orientation: 3, want: stored},
		{name: "90 CW rescales per axis", orientation: 6, want: rescaled},
		{name: "270 CW rescales per axis", orientation: 8, want: rescaled},
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

// TestDecideFaceBoxRotatesTheProductionRow is the measurement the repair was
// rewritten for: on the reported photo the quarter turn puts the detection on the
// marker and the rescale the old repair applied does not come close.
func TestDecideFaceBoxRotatesTheProductionRow(t *testing.T) {
	t.Parallel()
	face := productionFace()
	marker := productionMarker()

	if got := decideFaceBox(face, [][4]float64{marker}); got != TransformRotate {
		t.Fatalf("transform = %d, want TransformRotate (%d)", got, TransformRotate)
	}
	// The point of the whole exercise: the corrected box lands on the face.
	cx, cy := centreOf(face.transformed(TransformRotate))
	mx, my := centreOf(marker)
	if math.Hypot(cx-mx, cy-my) > 0.02 {
		t.Errorf("turned box centre (%v, %v) is not on the marker (%v, %v)", cx, cy, mx, my)
	}
	// And the transform it replaces is nowhere near it.
	rx, ry := centreOf(face.transformed(TransformRescale))
	if math.Hypot(rx-mx, ry-my) < 0.2 {
		t.Errorf("rescaled box centre (%v, %v) is unexpectedly close to the marker", rx, ry)
	}
}

// TestDecideFaceBoxNeedsEvidence verifies the repair never guesses: without a
// marker to reconcile against, and on a row it cannot reason about at all, no
// transform is chosen and the row is left exactly as it is.
func TestDecideFaceBoxNeedsEvidence(t *testing.T) {
	t.Parallel()
	face := productionFace()
	marker := productionMarker()

	tests := []struct {
		name    string
		face    TransposedFace
		markers [][4]float64
	}{
		{name: "no markers at all", face: face},
		{name: "no usable marker", face: face, markers: [][4]float64{}},
		{
			name:    "marker nowhere near any candidate",
			face:    face,
			markers: [][4]float64{boxAround(0.9, 0.05, 0.05, 0.05)},
		},
		{
			name:    "not a quarter turn",
			face:    TransposedFace{BBox: face.BBox, Orientation: 1, RawWidth: 4000, RawHeight: 3000},
			markers: [][4]float64{marker},
		},
		{
			name:    "unknown frame",
			face:    TransposedFace{BBox: face.BBox, Orientation: 6},
			markers: [][4]float64{marker},
		},
		{
			name:    "square frame, where every candidate is the same box",
			face:    TransposedFace{BBox: face.BBox, Orientation: 6, RawWidth: 3000, RawHeight: 3000},
			markers: [][4]float64{marker},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := decideFaceBox(tc.face, tc.markers); got != TransformSkip {
				t.Errorf("transform = %d, want TransformSkip", got)
			}
		})
	}
}

// TestDecideFaceBoxKeepsACorrectBox verifies a row that is already in display
// space is recognised as such: its box is left where it is (only the cached frame
// is wrong), rather than being turned onto a face it is already on.
func TestDecideFaceBoxKeepsACorrectBox(t *testing.T) {
	t.Parallel()
	face := productionFace()
	cx, cy := centreOf(face.BBox)
	marker := boxAround(cx, cy, 0.12, 0.16)

	if got := decideFaceBox(face, [][4]float64{marker}); got != TransformFrame {
		t.Errorf("transform = %d, want TransformFrame (%d)", got, TransformFrame)
	}
	if got := face.transformed(TransformFrame); got != face.BBox {
		t.Errorf("box = %v, want it untouched %v", got, face.BBox)
	}
}

// TestDecideFaceBoxPicksTheRescale verifies the other provenance still gets its
// own repair: a box the sidecar had auto-rotated into display space and that was
// only divided by the wrong pair reconciles under the rescale, and that is what is
// chosen for it.
func TestDecideFaceBoxPicksTheRescale(t *testing.T) {
	t.Parallel()
	face := productionFace()
	cx, cy := centreOf(face.transformed(TransformRescale))
	marker := boxAround(cx, cy, 0.12, 0.16)

	if got := decideFaceBox(face, [][4]float64{marker}); got != TransformRescale {
		t.Errorf("transform = %d, want TransformRescale (%d)", got, TransformRescale)
	}
}

// TestDecideFaceBoxRefusesATie verifies the runner-up margin: when two candidates
// land equally near the marker, the evidence names no winner and the row is left
// alone rather than moved on a coin flip.
func TestDecideFaceBoxRefusesATie(t *testing.T) {
	t.Parallel()
	// A face at the very centre of the frame: every candidate keeps it there, so
	// the distances are indistinguishable.
	face := TransposedFace{
		BBox:        [4]float64{0.45, 0.45, 0.1, 0.1},
		Orientation: 6,
		RawWidth:    4000,
		RawHeight:   3000,
	}
	marker := boxAround(0.5, 0.5, 0.12, 0.16)

	if got := decideFaceBox(face, [][4]float64{marker}); got != TransformSkip {
		t.Errorf("transform = %d, want TransformSkip on a tie", got)
	}
}

// TestDecideFaceBoxRefutesABoxOffThePhoto verifies the frame test: a candidate
// that would put the face outside the image is refuted before it is scored, even
// when it is the one nearest a marker.
func TestDecideFaceBoxRefutesABoxOffThePhoto(t *testing.T) {
	t.Parallel()
	// Near the right edge the rescale (×4/3 on x) pushes the box off the photo.
	face := TransposedFace{
		BBox:        [4]float64{0.8, 0.4, 0.15, 0.15},
		Orientation: 6,
		RawWidth:    4000,
		RawHeight:   3000,
	}
	rescaled := face.transformed(TransformRescale)
	if insideFrame(rescaled) {
		t.Fatalf("fixture does not leave the frame: %v", rescaled)
	}
	cx, cy := centreOf(rescaled)

	if got := decideFaceBox(face, [][4]float64{boxAround(cx, cy, 0.12, 0.16)}); got != TransformSkip {
		t.Errorf("transform = %d, want TransformSkip: the nearest candidate is off the photo", got)
	}
}

// TestPlanFaceBoxesSharesOneVerdict verifies a photo's undecided rows follow the
// verdict its decided rows agree on — a photo's faces come from one detection run,
// so they are in one space, and a photo with more detections than markers must not
// end up half repaired.
func TestPlanFaceBoxesSharesOneVerdict(t *testing.T) {
	t.Parallel()
	decided := productionFace()
	blind := decided
	blind.FaceIndex = 1
	blind.BBox = [4]float64{0.55, 0.10, 0.10, 0.10}

	plans := planFaceBoxes([]TransposedFace{decided, blind}, [][4]float64{productionMarker()})
	if len(plans) != 2 {
		t.Fatalf("planned %d rows, want 2", len(plans))
	}
	for i, plan := range plans {
		if plan.Transform != TransformRotate {
			t.Errorf("row %d: transform = %d, want TransformRotate", i, plan.Transform)
		}
		if want := plan.Face.transformed(TransformRotate); plan.BBox != want {
			t.Errorf("row %d: box = %v, want %v", i, plan.BBox, want)
		}
	}
}

// TestPlanFaceBoxesLeavesUndecidedRowsAlone verifies nothing is inherited when the
// photo itself is undecided (no marker at all) or when its decided rows disagree —
// the two cases where following a neighbour would be a guess.
func TestPlanFaceBoxesLeavesUndecidedRowsAlone(t *testing.T) {
	t.Parallel()
	turned := productionFace()
	// A second row of the same photo whose box is already in display space, far
	// enough from the first that neither row's candidates reach the other's marker.
	correct := turned
	correct.FaceIndex = 1
	correct.BBox = [4]float64{0.60, 0.70, 0.10, 0.10}
	blind := turned
	blind.FaceIndex = 2
	blind.BBox = [4]float64{0.55, 0.10, 0.10, 0.10}
	cx, cy := centreOf(correct.BBox)

	t.Run("no evidence on the photo", func(t *testing.T) {
		t.Parallel()
		for i, plan := range planFaceBoxes([]TransposedFace{turned, blind}, nil) {
			if plan.Transform != TransformSkip {
				t.Errorf("row %d: transform = %d, want TransformSkip", i, plan.Transform)
			}
		}
	})

	t.Run("decided rows disagree", func(t *testing.T) {
		t.Parallel()
		markers := [][4]float64{productionMarker(), boxAround(cx, cy, 0.12, 0.16)}
		plans := planFaceBoxes([]TransposedFace{turned, correct, blind}, markers)
		want := []FrameTransform{TransformRotate, TransformFrame, TransformSkip}
		for i, plan := range plans {
			if plan.Transform != want[i] {
				t.Errorf("row %d: transform = %d, want %d", i, plan.Transform, want[i])
			}
		}
	})
}
