//go:build integration

package vectors_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// TestFacesByKeys checks the batch fetch returns exactly the requested rows with
// their embeddings, tolerates keys with no matching row, and returns nil for an
// empty input.
func TestFacesByKeys(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	photoA := makePhoto(t, photoStore, "bykeys-a")
	photoB := makePhoto(t, photoStore, "bykeys-b")
	if err := store.SaveFaces(ctx, photoA, []vectors.Face{
		{FaceIndex: 0, Vector: faceVec(map[int]float32{0: 1}), BBox: [4]float64{0.1, 0.1, 0.2, 0.2}},
		{FaceIndex: 1, Vector: faceVec(map[int]float32{1: 1}), BBox: [4]float64{0.3, 0.3, 0.2, 0.2}},
	}); err != nil {
		t.Fatalf("SaveFaces(A): %v", err)
	}
	if err := store.SaveFaces(ctx, photoB, []vectors.Face{
		{FaceIndex: 0, Vector: faceVec(map[int]float32{2: 1}), BBox: [4]float64{0.4, 0.4, 0.2, 0.2}},
	}); err != nil {
		t.Fatalf("SaveFaces(B): %v", err)
	}

	// Request two real faces plus one that does not exist; expect only the two real.
	keys := []vectors.FaceKey{
		{PhotoUID: photoA, FaceIndex: 1},
		{PhotoUID: photoB, FaceIndex: 0},
		{PhotoUID: photoA, FaceIndex: 9},
	}
	got, err := store.FacesByKeys(ctx, keys)
	if err != nil {
		t.Fatalf("FacesByKeys: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("FacesByKeys returned %d rows, want 2", len(got))
	}
	byKey := map[vectors.FaceKey]vectors.Face{}
	for _, face := range got {
		byKey[vectors.FaceKey{PhotoUID: face.PhotoUID, FaceIndex: face.FaceIndex}] = face
	}
	faceA1, ok := byKey[vectors.FaceKey{PhotoUID: photoA, FaceIndex: 1}]
	if !ok {
		t.Fatalf("photoA#1 missing from result: %+v", got)
	}
	if len(faceA1.Vector) != vectors.FaceDim || faceA1.Vector[1] != 1 {
		t.Errorf("photoA#1 embedding not round-tripped: len=%d", len(faceA1.Vector))
	}

	empty, err := store.FacesByKeys(ctx, nil)
	if err != nil || empty != nil {
		t.Errorf("FacesByKeys(nil) = %v, %v; want nil, nil", empty, err)
	}
}
