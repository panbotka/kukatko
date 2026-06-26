//go:build integration

package vectors_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// TestUpdateFaceMarker checks the cache columns round-trip, including clearing a
// link back to NULL via empty-string arguments.
func TestUpdateFaceMarker(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "update_marker")

	if err := store.SaveFaces(ctx, uid, []vectors.Face{
		{FaceIndex: 0, Vector: faceVec(map[int]float32{0: 1}), BBox: [4]float64{0.1, 0.1, 0.3, 0.3}},
	}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}

	if err := store.UpdateFaceMarker(ctx, uid, 0, "mk1", "su1", "Alice"); err != nil {
		t.Fatalf("UpdateFaceMarker set: %v", err)
	}
	faces, err := store.ListFaces(ctx, uid)
	if err != nil || len(faces) != 1 {
		t.Fatalf("ListFaces = %d, %v", len(faces), err)
	}
	if faces[0].MarkerUID == nil || *faces[0].MarkerUID != "mk1" ||
		faces[0].SubjectUID == nil || *faces[0].SubjectUID != "su1" || faces[0].SubjectName != "Alice" {
		t.Fatalf("cache after set = %+v", faces[0])
	}

	// Empty arguments clear the nullable identifier columns back to NULL.
	if err := store.UpdateFaceMarker(ctx, uid, 0, "", "", ""); err != nil {
		t.Fatalf("UpdateFaceMarker clear: %v", err)
	}
	faces, _ = store.ListFaces(ctx, uid)
	if faces[0].MarkerUID != nil || faces[0].SubjectUID != nil || faces[0].SubjectName != "" {
		t.Errorf("cache after clear = %+v, want nil/nil/empty", faces[0])
	}

	// A non-existent (photo, face_index) updates nothing and is not an error.
	if err := store.UpdateFaceMarker(ctx, uid, 99, "mk", "su", "x"); err != nil {
		t.Errorf("UpdateFaceMarker missing = %v, want nil", err)
	}
}

// TestFindSimilarFaceCandidates checks ordering, the distance filter and that the
// cached assignment + bbox round-trip on each candidate.
func TestFindSimilarFaceCandidates(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "candidates")

	if err := store.SaveFaces(ctx, uid, []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{0: 1})),       // near, assigned to Alice
		sampleFace(1, faceVec(map[int]float32{0: 1, 1: 1})), // mid
		sampleFace(2, faceVec(map[int]float32{1: 1})),       // far
	}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}

	query := faceVec(map[int]float32{0: 1, 1: 0.1})
	got, err := store.FindSimilarFaceCandidates(ctx, query, 10, 0)
	if err != nil {
		t.Fatalf("FindSimilarFaceCandidates: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}
	if got[0].FaceIndex != 0 || got[1].FaceIndex != 1 || got[2].FaceIndex != 2 {
		t.Fatalf("candidate order = %+v, want 0,1,2", got)
	}
	first := got[0]
	if first.PhotoUID != uid || first.SubjectName != "Alice" ||
		first.SubjectUID == nil || *first.SubjectUID != "sub_alice" {
		t.Errorf("candidate cache not round-tripped: %+v", first)
	}
	if first.BBox != [4]float64{0.1, 0.2, 0.3, 0.4} {
		t.Errorf("candidate bbox = %v, want [0.1 0.2 0.3 0.4]", first.BBox)
	}

	filtered, err := store.FindSimilarFaceCandidates(ctx, query, 10, 0.5)
	if err != nil {
		t.Fatalf("FindSimilarFaceCandidates filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("filtered = %d candidates, want 2 (far excluded)", len(filtered))
	}
}
