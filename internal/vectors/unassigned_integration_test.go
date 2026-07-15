//go:build integration

package vectors_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/vectors"
)

// gradientFace builds a face whose embedding drifts away from the query direction
// (index 0) as spread grows, so a larger spread means a larger cosine distance. It
// is left unassigned (SubjectUID nil) unless a subject is given.
func gradientFace(index int, spread float32, subject *string) vectors.Face {
	return vectors.Face{
		FaceIndex:   index,
		Vector:      faceVec(map[int]float32{0: 1, 1: spread}),
		BBox:        [4]float64{0.1, 0.2, 0.3, 0.4},
		DetScore:    0.9,
		Model:       "buffalo_l",
		SubjectUID:  subject,
		SubjectName: subjectName(subject),
	}
}

// subjectName returns a display name for an assigned face, empty for an unassigned
// one.
func subjectName(subject *string) string {
	if subject == nil {
		return ""
	}
	return "Named"
}

// TestFindSimilarUnassignedFaceCandidates_skipsAssigned checks that the search never
// returns a face already assigned to a subject, even when the assigned faces are the
// nearest neighbours of the query.
func TestFindSimilarUnassignedFaceCandidates_skipsAssigned(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	assigned := "sub_named"
	named := makePhoto(t, photoStore, "named")
	if err := store.SaveFaces(ctx, named, []vectors.Face{
		gradientFace(0, 0, &assigned), // exact match to the query, but assigned
		gradientFace(1, 0, &assigned),
	}); err != nil {
		t.Fatalf("SaveFaces assigned: %v", err)
	}
	unnamed := makePhoto(t, photoStore, "unnamed")
	if err := store.SaveFaces(ctx, unnamed, []vectors.Face{
		gradientFace(0, 0.1, nil),
		gradientFace(1, 0.2, nil),
	}); err != nil {
		t.Fatalf("SaveFaces unassigned: %v", err)
	}

	query := faceVec(map[int]float32{0: 1})
	got, err := store.FindSimilarUnassignedFaceCandidates(ctx, query, 20, 0, nil)
	if err != nil {
		t.Fatalf("FindSimilarUnassignedFaceCandidates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2 unassigned", len(got))
	}
	for _, candidate := range got {
		if candidate.SubjectUID != nil {
			t.Errorf("assigned face leaked into result: %+v", candidate)
		}
		if candidate.PhotoUID != unnamed {
			t.Errorf("candidate photo = %s, want %s (the only unassigned photo)", candidate.PhotoUID, unnamed)
		}
	}
}

// TestFindSimilarUnassignedFaceCandidates_exclusionKeepsLimit is the important case:
// when the rejection exclusion set removes the nearest neighbours, the caller must
// still get `limit` candidates back — filtering after the HNSW limit would silently
// shrink the result, which is the bug this search must avoid.
func TestFindSimilarUnassignedFaceCandidates_exclusionKeepsLimit(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	photo := makePhoto(t, photoStore, "pool")
	faces := make([]vectors.Face, 8)
	for i := range faces {
		faces[i] = gradientFace(i, 0.05*float32(i), nil) // face 0 nearest, face 7 farthest
	}
	if err := store.SaveFaces(ctx, photo, faces); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}

	// Exclude the three nearest faces, as if they had been rejected for the subject
	// being searched. A naive "fetch 5, then drop excluded" would return only 2.
	exclude := []vectors.FaceKey{
		{PhotoUID: photo, FaceIndex: 0},
		{PhotoUID: photo, FaceIndex: 1},
		{PhotoUID: photo, FaceIndex: 2},
	}
	query := faceVec(map[int]float32{0: 1})
	got, err := store.FindSimilarUnassignedFaceCandidates(ctx, query, 5, 0, exclude)
	if err != nil {
		t.Fatalf("FindSimilarUnassignedFaceCandidates: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d candidates, want 5 (exclusion must not shrink the result)", len(got))
	}
	excluded := map[int]bool{0: true, 1: true, 2: true}
	for _, candidate := range got {
		if excluded[candidate.FaceIndex] {
			t.Errorf("excluded face %d returned", candidate.FaceIndex)
		}
	}
	// The five nearest survivors are face indexes 3..7, nearest first.
	if got[0].FaceIndex != 3 || got[4].FaceIndex != 7 {
		t.Errorf("survivor order = first %d, last %d, want first 3, last 7", got[0].FaceIndex, got[4].FaceIndex)
	}
}

// TestFindSimilarUnassignedFaceCandidates_dimMismatch checks the vector length guard.
func TestFindSimilarUnassignedFaceCandidates_dimMismatch(t *testing.T) {
	store, _, _ := newStore(t)
	if _, err := store.FindSimilarUnassignedFaceCandidates(
		t.Context(), []float32{1, 2, 3}, 5, 0, nil); !errors.Is(err, vectors.ErrDimMismatch) {
		t.Fatalf("dim mismatch = %v, want ErrDimMismatch", err)
	}
}
