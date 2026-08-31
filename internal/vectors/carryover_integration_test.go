//go:build integration

package vectors_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// detectedFace builds a face as a detector would hand it over: a box, a vector
// and nothing else. It carries no assignment, which is the point — the detector
// knows nothing about who is on the photo.
func detectedFace(index int, box [4]float64, vec []float32) vectors.Face {
	return vectors.Face{
		FaceIndex: index, Vector: vec, BBox: box, DetScore: 0.9, Model: "buffalo_l",
		PhotoWidth: 4000, PhotoHeight: 3000, Orientation: 1,
	}
}

// TestRecordFaceDetection_carriesAssignmentsOverARedetection is the invariant a
// forced re-detection rests on: running the detector again replaces the photo's
// faces without un-naming the people on it. The face that comes back in the same
// place keeps its marker and subject; a face that appears somewhere new arrives
// unassigned.
//
// It matters because faces.subject_uid is what a person's gallery and a `person:`
// search read. Without the carry-over a rebuilt photo would silently leave
// somebody's gallery until the next time a human opened it.
func TestRecordFaceDetection_carriesAssignmentsOverARedetection(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "carry_over")

	marker, subject := "mk_alice", "sub_alice"
	first := detectedFace(0, [4]float64{0.10, 0.10, 0.20, 0.20}, faceVec(map[int]float32{0: 1}))
	first.MarkerUID, first.SubjectUID, first.SubjectName = &marker, &subject, "Alice"
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: []vectors.Face{first}, Model: "buffalo_l",
	}); err != nil {
		t.Fatalf("RecordFaceDetection (first run): %v", err)
	}

	// The rebuild: the same face a hair to the left, plus one the first pass missed.
	redetected := []vectors.Face{
		detectedFace(0, [4]float64{0.11, 0.11, 0.20, 0.20}, faceVec(map[int]float32{2: 1})),
		detectedFace(1, [4]float64{0.60, 0.60, 0.15, 0.15}, faceVec(map[int]float32{3: 1})),
	}
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: redetected, Model: "buffalo_l",
	}); err != nil {
		t.Fatalf("RecordFaceDetection (rebuild): %v", err)
	}

	got, err := store.ListFaces(ctx, uid)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListFaces = %d faces, want 2 — the rebuild must replace, never append", len(got))
	}
	if got[0].SubjectUID == nil || *got[0].SubjectUID != subject ||
		got[0].MarkerUID == nil || *got[0].MarkerUID != marker || got[0].SubjectName != "Alice" {
		t.Errorf("re-detected face 0 = %+v, want Alice's marker and subject carried over", got[0])
	}
	if got[1].SubjectUID != nil || got[1].MarkerUID != nil {
		t.Errorf("newly found face 1 = %+v, want it unassigned", got[1])
	}
}

// TestRecordFaceDetection_dropsAnAssignmentWithNowhereToGo confirms the carry-over
// is not a way of keeping a name alive forever: when the detector no longer finds
// a face where the assigned one was, the assignment goes with the row. The marker
// itself is a separate table and survives, so nothing a person recorded is lost —
// only the face cache is, and it is a cache.
func TestRecordFaceDetection_dropsAnAssignmentWithNowhereToGo(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "carry_drop")

	subject := "sub_alice"
	first := detectedFace(0, [4]float64{0.10, 0.10, 0.20, 0.20}, faceVec(map[int]float32{0: 1}))
	first.SubjectUID, first.SubjectName = &subject, "Alice"
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: []vectors.Face{first}, Model: "buffalo_l",
	}); err != nil {
		t.Fatalf("RecordFaceDetection (first run): %v", err)
	}

	elsewhere := detectedFace(0, [4]float64{0.70, 0.70, 0.15, 0.15}, faceVec(map[int]float32{1: 1}))
	if err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: []vectors.Face{elsewhere}, Model: "buffalo_l",
	}); err != nil {
		t.Fatalf("RecordFaceDetection (rebuild): %v", err)
	}

	got, err := store.ListFaces(ctx, uid)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListFaces = %d faces, %v; want 1", len(got), err)
	}
	if got[0].SubjectUID != nil {
		t.Errorf("face subject = %q, want nil: it is a different face", *got[0].SubjectUID)
	}
}
