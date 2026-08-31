package vectors

import "testing"

// TestIoU covers the overlap measure the carry-over pairs by: identical boxes,
// partial overlap, disjoint boxes and a degenerate one.
func TestIoU(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b [4]float64
		want float64
	}{
		{name: "identical boxes", a: [4]float64{0.1, 0.1, 0.2, 0.2}, b: [4]float64{0.1, 0.1, 0.2, 0.2}, want: 1},
		{name: "disjoint boxes", a: [4]float64{0, 0, 0.1, 0.1}, b: [4]float64{0.5, 0.5, 0.1, 0.1}, want: 0},
		{name: "zero area", a: [4]float64{0, 0, 0, 0}, b: [4]float64{0, 0, 0.1, 0.1}, want: 0},
		{
			name: "half overlap in one axis",
			a:    [4]float64{0, 0, 0.2, 0.2},
			b:    [4]float64{0.1, 0, 0.2, 0.2},
			want: 1.0 / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IoU(tt.a, tt.b); got < tt.want-1e-9 || got > tt.want+1e-9 {
				t.Errorf("IoU(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestCarryAssignments_movesTheNameOntoTheSameFace is the invariant a forced
// re-detection depends on: a face detected again in the same place inherits the
// marker and subject the row it replaces carried, while a face that is genuinely
// new stays unassigned.
func TestCarryAssignments_movesTheNameOntoTheSameFace(t *testing.T) {
	t.Parallel()

	faces := []Face{
		{FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20}}, // the same face, a hair to the left
		{FaceIndex: 1, BBox: [4]float64{0.70, 0.70, 0.10, 0.10}}, // nobody was ever here
	}
	assigned := []assignedFace{{
		FaceIndex: 0, BBox: [4]float64{0.11, 0.11, 0.20, 0.20},
		MarkerUID: new("mk1"), SubjectUID: new("sb1"), SubjectName: "Anna",
	}}

	if got := carryAssignments(faces, assigned); got != 1 {
		t.Fatalf("carryAssignments moved %d assignments, want 1", got)
	}
	if faces[0].SubjectUID == nil || *faces[0].SubjectUID != "sb1" ||
		faces[0].MarkerUID == nil || *faces[0].MarkerUID != "mk1" ||
		faces[0].SubjectName != "Anna" {
		t.Errorf("face 0 = %+v, want Anna's marker and subject carried over", faces[0])
	}
	if faces[1].SubjectUID != nil || faces[1].MarkerUID != nil {
		t.Errorf("face 1 = %+v, want an unassigned new face", faces[1])
	}
}

// TestCarryAssignments_refusesADifferentFace guards the cost of being wrong: a
// detection that lands somewhere else on the photo must not inherit somebody's
// name, however lonely the old assignment is.
func TestCarryAssignments_refusesADifferentFace(t *testing.T) {
	t.Parallel()

	faces := []Face{{FaceIndex: 0, BBox: [4]float64{0.60, 0.60, 0.20, 0.20}}}
	assigned := []assignedFace{{
		FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20}, SubjectUID: new("sb1"),
	}}

	if got := carryAssignments(faces, assigned); got != 0 {
		t.Fatalf("carryAssignments moved %d assignments, want 0", got)
	}
	if faces[0].SubjectUID != nil {
		t.Errorf("face 0 subject = %v, want nil", *faces[0].SubjectUID)
	}
}

// TestCarryAssignments_isExclusive proves one name cannot land on two faces: when
// two detections overlap the same old row, only the closer of them takes it.
func TestCarryAssignments_isExclusive(t *testing.T) {
	t.Parallel()

	faces := []Face{
		{FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20}},
		{FaceIndex: 1, BBox: [4]float64{0.12, 0.12, 0.20, 0.20}},
	}
	assigned := []assignedFace{{
		FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20}, SubjectUID: new("sb1"),
	}}

	if got := carryAssignments(faces, assigned); got != 1 {
		t.Fatalf("carryAssignments moved %d assignments, want 1", got)
	}
	if faces[0].SubjectUID == nil {
		t.Error("the exactly-matching face 0 lost the assignment")
	}
	if faces[1].SubjectUID != nil {
		t.Error("face 1 also took the assignment; the pairing is not exclusive")
	}
}

// TestCarryAssignments_leavesAnExplicitAssignmentAlone keeps the carry-over out of
// the way of a caller that named a face itself: an inherited link must never
// overwrite one that was supplied.
func TestCarryAssignments_leavesAnExplicitAssignmentAlone(t *testing.T) {
	t.Parallel()

	faces := []Face{{
		FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20},
		SubjectUID: new("explicit"), SubjectName: "Bára",
	}}
	assigned := []assignedFace{{
		FaceIndex: 0, BBox: [4]float64{0.10, 0.10, 0.20, 0.20},
		SubjectUID: new("sb1"), SubjectName: "Anna",
	}}

	if got := carryAssignments(faces, assigned); got != 0 {
		t.Fatalf("carryAssignments moved %d assignments, want 0", got)
	}
	if *faces[0].SubjectUID != "explicit" || faces[0].SubjectName != "Bára" {
		t.Errorf("face 0 = %+v, want the explicit assignment untouched", faces[0])
	}
}

// TestCarryAssignments_emptyInputs keeps the two trivial cases honest: nothing to
// carry, and nowhere to carry it to.
func TestCarryAssignments_emptyInputs(t *testing.T) {
	t.Parallel()

	if got := carryAssignments(nil, []assignedFace{{SubjectUID: new("sb1")}}); got != 0 {
		t.Errorf("carryAssignments with no new faces = %d, want 0", got)
	}
	if got := carryAssignments([]Face{{FaceIndex: 0}}, nil); got != 0 {
		t.Errorf("carryAssignments with nothing assigned = %d, want 0", got)
	}
}
