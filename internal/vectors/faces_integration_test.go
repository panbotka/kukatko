//go:build integration

package vectors_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// sampleFace builds a Face with the given index and embedding plus representative
// bounding box and cached metadata.
func sampleFace(index int, vec []float32) vectors.Face {
	subject := "sub_alice"
	return vectors.Face{
		FaceIndex:   index,
		Vector:      vec,
		BBox:        [4]float64{0.1, 0.2, 0.3, 0.4},
		DetScore:    0.97,
		Model:       "buffalo_l",
		SubjectUID:  &subject,
		SubjectName: "Alice",
		PhotoWidth:  4000,
		PhotoHeight: 3000,
		Orientation: 1,
	}
}

// TestFacesLifecycle exercises save, list (ordered), round-trip and delete.
func TestFacesLifecycle(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "faces1")

	faces := []vectors.Face{
		sampleFace(1, faceVec(map[int]float32{1: 1})),
		sampleFace(0, faceVec(map[int]float32{0: 1})),
	}
	if err := store.SaveFaces(ctx, uid, faces); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}

	got, err := store.ListFaces(ctx, uid)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListFaces returned %d faces, want 2", len(got))
	}
	if got[0].FaceIndex != 0 || got[1].FaceIndex != 1 {
		t.Errorf("faces not ordered by face_index: %d, %d", got[0].FaceIndex, got[1].FaceIndex)
	}
	first := got[0]
	if first.ID == 0 || first.Dim != vectors.FaceDim || first.SubjectName != "Alice" ||
		first.SubjectUID == nil || *first.SubjectUID != "sub_alice" || first.MarkerUID != nil {
		t.Errorf("face fields not round-tripped: %+v", first)
	}
	if first.BBox != [4]float64{0.1, 0.2, 0.3, 0.4} || first.DetScore != 0.97 {
		t.Errorf("face bbox/score not round-tripped: %+v", first)
	}

	deleted, err := store.DeleteFaces(ctx, uid)
	if err != nil || deleted != 2 {
		t.Fatalf("DeleteFaces = %d, %v; want 2, nil", deleted, err)
	}
	if remaining, _ := store.ListFaces(ctx, uid); len(remaining) != 0 {
		t.Errorf("faces survived delete: %d", len(remaining))
	}
}

// TestSaveFaces_replaceIdempotent checks that re-saving replaces a photo's faces.
func TestSaveFaces_replaceIdempotent(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "faces_replace")

	if err := store.SaveFaces(ctx, uid, []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{0: 1})),
		sampleFace(1, faceVec(map[int]float32{1: 1})),
	}); err != nil {
		t.Fatalf("SaveFaces first: %v", err)
	}
	if err := store.SaveFaces(ctx, uid, []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{2: 1})),
	}); err != nil {
		t.Fatalf("SaveFaces second: %v", err)
	}
	got, err := store.ListFaces(ctx, uid)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListFaces after replace = %d faces, %v; want 1", len(got), err)
	}
}

// TestSaveFaces_duplicateIndex checks the UNIQUE(photo_uid, face_index) constraint.
func TestSaveFaces_duplicateIndex(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "faces_dup")

	err := store.SaveFaces(ctx, uid, []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{0: 1})),
		sampleFace(0, faceVec(map[int]float32{1: 1})),
	})
	if !errors.Is(err, vectors.ErrFaceIndexTaken) {
		t.Fatalf("SaveFaces duplicate index = %v, want ErrFaceIndexTaken", err)
	}
	if got, _ := store.ListFaces(ctx, uid); len(got) != 0 {
		t.Errorf("failed SaveFaces left rows behind: %d", len(got))
	}
}

// TestSaveFaces_dimMismatch checks face-vector length validation.
func TestSaveFaces_dimMismatch(t *testing.T) {
	store, photoStore, _ := newStore(t)
	uid := makePhoto(t, photoStore, "faces_dim")
	err := store.SaveFaces(t.Context(), uid, []vectors.Face{sampleFace(0, []float32{1, 2})})
	if !errors.Is(err, vectors.ErrDimMismatch) {
		t.Fatalf("SaveFaces short vector = %v, want ErrDimMismatch", err)
	}
}

// TestFindSimilarFaces checks cosine ordering and the maxDistance filter for faces.
func TestFindSimilarFaces(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "faces_sim")

	if err := store.SaveFaces(ctx, uid, []vectors.Face{
		sampleFace(0, faceVec(map[int]float32{0: 1})),       // near
		sampleFace(1, faceVec(map[int]float32{0: 1, 1: 1})), // mid
		sampleFace(2, faceVec(map[int]float32{1: 1})),       // far
	}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}

	query := faceVec(map[int]float32{0: 1, 1: 0.1})

	matches, err := store.FindSimilarFaces(ctx, query, 10, 0)
	if err != nil {
		t.Fatalf("FindSimilarFaces: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("FindSimilarFaces returned %d, want 3", len(matches))
	}
	wantOrder := []int{0, 1, 2}
	for i, m := range matches {
		if m.FaceIndex != wantOrder[i] {
			t.Fatalf("FindSimilarFaces order = %+v, want face_index %v", matches, wantOrder)
		}
	}

	filtered, err := store.FindSimilarFaces(ctx, query, 10, 0.5)
	if err != nil {
		t.Fatalf("FindSimilarFaces filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("FindSimilarFaces filtered = %d, want 2 (far excluded)", len(filtered))
	}
}

// TestFacesCascadeDelete checks that deleting a photo removes its faces.
func TestFacesCascadeDelete(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "faces_cascade")
	if err := store.SaveFaces(ctx, uid, []vectors.Face{sampleFace(0, faceVec(map[int]float32{0: 1}))}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	if err := photoStore.Delete(ctx, uid); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	if got, _ := store.ListFaces(ctx, uid); len(got) != 0 {
		t.Errorf("faces survived photo delete: %d", len(got))
	}
}
