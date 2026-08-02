//go:build integration

package vectors_test

import (
	"errors"
	"fmt"
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

// subjectFace builds a face with the given index, embedding and cached subject
// uid, used to populate ListFacesBySubject cases.
func subjectFace(index int, vec []float32, subject string) vectors.Face {
	return vectors.Face{
		FaceIndex:   index,
		Vector:      vec,
		BBox:        [4]float64{0.1, 0.2, 0.3, 0.4},
		DetScore:    0.95,
		Model:       "buffalo_l",
		SubjectUID:  &subject,
		PhotoWidth:  4000,
		PhotoHeight: 3000,
		Orientation: 1,
	}
}

// TestListFacesBySubject returns only the faces cached for the given subject, in
// ascending (photo_uid, face_index) order, and an empty slice for an unknown one.
func TestListFacesBySubject(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	const alice, bob = "su_alice", "su_bob"
	p1 := makePhoto(t, photoStore, "subj_p1")
	p2 := makePhoto(t, photoStore, "subj_p2")

	if err := store.SaveFaces(ctx, p1, []vectors.Face{
		subjectFace(0, faceVec(map[int]float32{0: 1}), alice),
		subjectFace(1, faceVec(map[int]float32{1: 1}), bob),
	}); err != nil {
		t.Fatalf("SaveFaces p1: %v", err)
	}
	if err := store.SaveFaces(ctx, p2, []vectors.Face{
		subjectFace(0, faceVec(map[int]float32{2: 1}), alice),
	}); err != nil {
		t.Fatalf("SaveFaces p2: %v", err)
	}

	got, err := store.ListFacesBySubject(ctx, alice)
	if err != nil {
		t.Fatalf("ListFacesBySubject: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListFacesBySubject(alice) = %d faces, want 2", len(got))
	}
	for _, f := range got {
		if f.SubjectUID == nil || *f.SubjectUID != alice {
			t.Errorf("face not assigned to alice: %+v", f)
		}
	}
	seen := map[string]bool{got[0].PhotoUID: true, got[1].PhotoUID: true}
	if !seen[p1] || !seen[p2] {
		t.Errorf("ListFacesBySubject(alice) photos = %v, want %s and %s", seen, p1, p2)
	}
	if got[0].PhotoUID > got[1].PhotoUID {
		t.Errorf("faces not ordered by photo_uid: %s, %s", got[0].PhotoUID, got[1].PhotoUID)
	}

	if none, _ := store.ListFacesBySubject(ctx, "su_nobody"); len(none) != 0 {
		t.Errorf("ListFacesBySubject(nobody) = %d, want 0", len(none))
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

// TestSampleFacesBySubject checks the bounded read: a subject within the limit
// comes back whole, a subject over it comes back as a spread sample carrying the
// true totals, and a non-positive limit still means "all".
func TestSampleFacesBySubject(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	const subject = "su_sample"

	// Twelve faces over six photos, two faces per photo.
	for i := range 6 {
		uid := makePhoto(t, photoStore, fmt.Sprintf("sample_p%d", i))
		if err := store.SaveFaces(ctx, uid, []vectors.Face{
			subjectFace(0, faceVec(map[int]float32{2 * i: 1}), subject),
			subjectFace(1, faceVec(map[int]float32{2*i + 1: 1}), subject),
		}); err != nil {
			t.Fatalf("SaveFaces p%d: %v", i, err)
		}
	}

	all, err := store.SampleFacesBySubject(ctx, subject, 100)
	if err != nil {
		t.Fatalf("SampleFacesBySubject(limit 100): %v", err)
	}
	if len(all.Faces) != 12 || all.Total != 12 || all.Photos != 6 {
		t.Fatalf("under the limit = %d faces, total %d, photos %d; want 12/12/6",
			len(all.Faces), all.Total, all.Photos)
	}

	sample, err := store.SampleFacesBySubject(ctx, subject, 4)
	if err != nil {
		t.Fatalf("SampleFacesBySubject(limit 4): %v", err)
	}
	if len(sample.Faces) != 4 {
		t.Fatalf("sample = %d faces, want exactly the requested 4", len(sample.Faces))
	}
	// The totals describe the subject, not the sample — the search reports them.
	if sample.Total != 12 || sample.Photos != 6 {
		t.Errorf("sample totals = %d faces over %d photos, want 12/6", sample.Total, sample.Photos)
	}
	if len(sample.Faces[0].Vector) != vectors.FaceDim {
		t.Errorf("sampled face embedding len = %d, want %d", len(sample.Faces[0].Vector), vectors.FaceDim)
	}
	// A sample must be spread across the ordered set, not its first rows: with a
	// quarter of the rows kept, the last one has to come from the tail.
	ordered, err := store.ListFacesBySubject(ctx, subject)
	if err != nil {
		t.Fatalf("ListFacesBySubject: %v", err)
	}
	last := sample.Faces[len(sample.Faces)-1]
	tail := ordered[len(ordered)-1]
	if last.PhotoUID != tail.PhotoUID || last.FaceIndex != tail.FaceIndex {
		t.Errorf("sample ends at %s#%d, want the ordered set's last row %s#%d — an even stride "+
			"spreads across the subject rather than reading its head",
			last.PhotoUID, last.FaceIndex, tail.PhotoUID, tail.FaceIndex)
	}

	unbounded, err := store.SampleFacesBySubject(ctx, subject, 0)
	if err != nil {
		t.Fatalf("SampleFacesBySubject(limit 0): %v", err)
	}
	if len(unbounded.Faces) != 12 {
		t.Errorf("limit 0 = %d faces, want all 12", len(unbounded.Faces))
	}

	none, err := store.SampleFacesBySubject(ctx, "su_nobody", 4)
	if err != nil || len(none.Faces) != 0 || none.Total != 0 || none.Photos != 0 {
		t.Errorf("SampleFacesBySubject(nobody) = %+v, %v; want an empty result", none, err)
	}
}
