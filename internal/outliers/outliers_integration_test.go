//go:build integration

package outliers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// harness bundles the stores over a freshly truncated integration database.
type harness struct {
	faces    *vectors.Store
	people   *people.Store
	photos   *photos.Store
	feedback *feedback.Store
	svc      *outliers.Service
}

// newHarness returns a harness over a truncated integration database.
func newHarness(t *testing.T) harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	faceStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	feedbackStore := feedback.NewStore(db.Pool())
	return harness{
		faces:    faceStore,
		people:   peopleStore,
		photos:   photos.NewStore(db.Pool()),
		feedback: feedbackStore,
		svc: outliers.New(outliers.Config{
			Faces: faceStore, People: peopleStore, Feedback: feedbackStore,
		}),
	}
}

// makePhoto inserts a minimal photo with the given file hash and returns its uid.
func makePhoto(t *testing.T, store *photos.Store, hash string) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash: hash,
		FilePath: "2024/01/" + hash + ".jpg",
		FileName: hash + ".jpg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// faceVec builds a FaceDim vector with the supplied index→value overrides.
func faceVec(set map[int]float32) []float32 {
	v := make([]float32, vectors.FaceDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// assignFace saves a single face on a fresh photo, cached as belonging to subject.
func (h harness) assignFace(t *testing.T, hash, subjectUID string, vec []float32) string {
	t.Helper()
	photoUID := makePhoto(t, h.photos, hash)
	face := vectors.Face{
		FaceIndex:   0,
		Vector:      vec,
		BBox:        [4]float64{0.1, 0.2, 0.3, 0.4},
		DetScore:    0.95,
		Model:       "buffalo_l",
		SubjectUID:  &subjectUID,
		PhotoWidth:  4000,
		PhotoHeight: 3000,
		Orientation: 1,
	}
	if err := h.faces.SaveFaces(t.Context(), photoUID, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces(%s): %v", photoUID, err)
	}
	return photoUID
}

// makeSubject inserts a person subject and returns its uid.
func (h harness) makeSubject(t *testing.T, name string) string {
	t.Helper()
	subj, err := h.people.CreateSubject(t.Context(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("CreateSubject(%s): %v", name, err)
	}
	return subj.UID
}

// plantCluster assigns ten consistent faces plus two planted outliers to subject
// and returns the two outlier photo uids.
func (h harness) plantCluster(t *testing.T, subjectUID string) (string, string) {
	t.Helper()
	for i := range 10 {
		h.assignFace(t, fmt.Sprintf("member-%02d", i), subjectUID,
			faceVec(map[int]float32{0: 1, 1: 0.01 * float32(i)}))
	}
	a := h.assignFace(t, "planted-a", subjectUID, faceVec(map[int]float32{1: 1}))
	b := h.assignFace(t, "planted-b", subjectUID, faceVec(map[int]float32{1: 1, 0: 0.05}))
	return a, b
}

// TestOutliers_plantedOutlierRanksFirstDB checks a face orthogonal to several
// consistent ones ranks at the top, with descending distances behind it.
func TestOutliers_plantedOutlierRanksFirstDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Alice")
	consistent := []map[int]float32{{0: 1}, {0: 1, 1: 0.05}, {0: 1, 2: 0.05}}
	for i, set := range consistent {
		h.assignFace(t, fmt.Sprintf("consistent-%d", i), subj, faceVec(set))
	}
	outlierPhoto := h.assignFace(t, "planted-outlier", subj, faceVec(map[int]float32{1: 1}))

	res, err := h.svc.Outliers(ctx, subj, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 4 || !res.Meaningful {
		t.Fatalf("Count=%d Meaningful=%v, want 4 true", res.Count, res.Meaningful)
	}
	if res.Faces[0].PhotoUID != outlierPhoto {
		t.Errorf("outlier %s not ranked first: %+v", outlierPhoto, res.Faces)
	}
	for i := 1; i < len(res.Faces); i++ {
		if res.Faces[i].Distance > res.Faces[i-1].Distance {
			t.Errorf("distances not descending at %d: %+v", i, res.Faces)
		}
	}
}

// TestOutliers_trimmedCentroidDB plants two obvious outliers among ten consistent
// faces and checks the trimmed centroid scores them strictly higher than the
// plain centroid of all faces would — the misassignments no longer mask
// themselves.
func TestOutliers_trimmedCentroidDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Alice")
	plantedA, plantedB := h.plantCluster(t, subj)

	res, err := h.svc.Outliers(ctx, subj, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}

	loaded, err := h.faces.ListFacesBySubject(ctx, subj)
	if err != nil {
		t.Fatalf("ListFacesBySubject: %v", err)
	}
	vecs := make([][]float32, len(loaded))
	byPhoto := make(map[string][]float32, len(loaded))
	for i := range loaded {
		vecs[i] = loaded[i].Vector
		byPhoto[loaded[i].PhotoUID] = loaded[i].Vector
	}
	plain := vectors.Centroid(vecs)

	distances := make(map[string]float64, len(res.Faces))
	for _, f := range res.Faces {
		distances[f.PhotoUID] = f.Distance
	}
	for _, planted := range []string{plantedA, plantedB} {
		untrimmed := vectors.CosineDistance(plain, byPhoto[planted])
		if distances[planted] <= untrimmed {
			t.Errorf("%s trimmed distance %g <= untrimmed %g, want strictly higher",
				planted, distances[planted], untrimmed)
		}
	}
	if res.Faces[0].PhotoUID != plantedA && res.Faces[0].PhotoUID != plantedB {
		t.Errorf("planted outlier not ranked first: %+v", res.Faces[0])
	}
}

// TestOutliers_thresholdAndLimitDB checks the threshold keeps only distant faces
// and the limit caps the list, while Count keeps describing the full scored set.
func TestOutliers_thresholdAndLimitDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Alice")
	h.plantCluster(t, subj)

	res, err := h.svc.Outliers(ctx, subj, outliers.Options{Threshold: 0.5})
	if err != nil {
		t.Fatalf("Outliers(threshold): %v", err)
	}
	if len(res.Faces) != 2 || res.Count != 12 {
		t.Errorf("threshold: faces=%d Count=%d, want 2 of 12", len(res.Faces), res.Count)
	}

	res, err = h.svc.Outliers(ctx, subj, outliers.Options{Limit: 3})
	if err != nil {
		t.Fatalf("Outliers(limit): %v", err)
	}
	if len(res.Faces) != 3 || res.Count != 12 {
		t.Errorf("limit: faces=%d Count=%d, want 3 of 12", len(res.Faces), res.Count)
	}
}

// TestOutliers_confirmedFaceExcludedDB confirms one planted outlier as correct and
// checks the next call no longer offers it while the other outlier stays.
func TestOutliers_confirmedFaceExcludedDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Alice")
	plantedA, plantedB := h.plantCluster(t, subj)

	key := feedback.FaceConfirmationKey{PhotoUID: plantedA, FaceIndex: 0, SubjectUID: subj}
	entry := audit.Entry{Action: audit.ActionFaceConfirm, TargetType: "subjects", TargetUID: subj}
	if err := h.feedback.ConfirmFace(ctx, key, entry); err != nil {
		t.Fatalf("ConfirmFace: %v", err)
	}

	res, err := h.svc.Outliers(ctx, subj, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	seenB := false
	for _, f := range res.Faces {
		if f.PhotoUID == plantedA {
			t.Errorf("confirmed face %s still offered: %+v", plantedA, f)
		}
		if f.PhotoUID == plantedB {
			seenB = true
		}
	}
	if !seenB {
		t.Errorf("unconfirmed outlier %s missing from results", plantedB)
	}
	if len(res.Faces) != 11 || res.Count != 12 {
		t.Errorf("faces=%d Count=%d, want 11 returned of 12 scored", len(res.Faces), res.Count)
	}
}

// TestOutliers_noEmbeddingCountDB tags the subject on a photo that has no face
// rows (as when the sidecar was offline) and checks the assignment is reported as
// unscorable rather than silently omitted.
func TestOutliers_noEmbeddingCountDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Alice")
	h.assignFace(t, "embedded", subj, faceVec(map[int]float32{0: 1}))

	bare := makePhoto(t, h.photos, "no-embedding")
	if _, err := h.people.CreateMarker(ctx, people.Marker{
		PhotoUID: bare, SubjectUID: &subj, X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}); err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}

	res, err := h.svc.Outliers(ctx, subj, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.NoEmbedding != 1 {
		t.Errorf("NoEmbedding = %d, want 1", res.NoEmbedding)
	}
	if res.Count != 1 {
		t.Errorf("Count = %d, want 1 (only the embedded face is scored)", res.Count)
	}
}

// TestOutliers_smallSetDB checks a two-face subject is returned but not meaningful.
func TestOutliers_smallSetDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Bob")
	h.assignFace(t, "small-1", subj, faceVec(map[int]float32{0: 1}))
	h.assignFace(t, "small-2", subj, faceVec(map[int]float32{1: 1}))

	res, err := h.svc.Outliers(ctx, subj, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 2 || res.Meaningful || len(res.Faces) != 2 {
		t.Errorf("small-set result mismatch: %+v", res)
	}
}

// TestOutliers_noFacesDB checks a subject with no assigned faces yields an empty
// ranking that is not meaningful.
func TestOutliers_noFacesDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj := h.makeSubject(t, "Carol")
	res, err := h.svc.Outliers(ctx, subj, outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 0 || res.Meaningful || len(res.Faces) != 0 {
		t.Errorf("no-faces result mismatch: %+v", res)
	}
}

// TestOutliers_subjectNotFoundDB checks the people sentinel is surfaced.
func TestOutliers_subjectNotFoundDB(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Outliers(t.Context(), "su_missing", outliers.Options{})
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("Outliers = %v, want ErrSubjectNotFound", err)
	}
}
