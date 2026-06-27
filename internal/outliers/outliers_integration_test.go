//go:build integration

package outliers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// harness bundles the three stores over a freshly truncated integration database.
type harness struct {
	faces  *vectors.Store
	people *people.Store
	photos *photos.Store
	svc    *outliers.Service
}

// newHarness returns a harness over a truncated integration database.
func newHarness(t *testing.T) harness {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	faceStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	return harness{
		faces:  faceStore,
		people: peopleStore,
		photos: photos.NewStore(db.Pool()),
		svc:    outliers.New(outliers.Config{Faces: faceStore, People: peopleStore}),
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

// TestOutliers_plantedOutlierRanksFirst checks a face orthogonal to several
// consistent ones ranks at the top, with descending distances behind it.
func TestOutliers_plantedOutlierRanksFirstDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj, err := h.people.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	consistent := []map[int]float32{{0: 1}, {0: 1, 1: 0.05}, {0: 1, 2: 0.05}}
	for i, set := range consistent {
		h.assignFace(t, fmt.Sprintf("consistent-%d", i), subj.UID, faceVec(set))
	}
	outlierPhoto := h.assignFace(t, "planted-outlier", subj.UID, faceVec(map[int]float32{1: 1}))

	res, err := h.svc.Outliers(ctx, subj.UID)
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

// TestOutliers_smallSet checks a two-face subject is returned but not meaningful.
func TestOutliers_smallSetDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj, err := h.people.CreateSubject(ctx, people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	h.assignFace(t, "small-1", subj.UID, faceVec(map[int]float32{0: 1}))
	h.assignFace(t, "small-2", subj.UID, faceVec(map[int]float32{1: 1}))

	res, err := h.svc.Outliers(ctx, subj.UID)
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 2 || res.Meaningful || len(res.Faces) != 2 {
		t.Errorf("small-set result mismatch: %+v", res)
	}
}

// TestOutliers_noFaces checks a subject with no assigned faces yields an empty
// ranking that is not meaningful.
func TestOutliers_noFacesDB(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	subj, err := h.people.CreateSubject(ctx, people.Subject{Name: "Carol"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	res, err := h.svc.Outliers(ctx, subj.UID)
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 0 || res.Meaningful || len(res.Faces) != 0 {
		t.Errorf("no-faces result mismatch: %+v", res)
	}
}

// TestOutliers_subjectNotFound checks the people sentinel is surfaced.
func TestOutliers_subjectNotFoundDB(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Outliers(t.Context(), "su_missing")
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("Outliers = %v, want ErrSubjectNotFound", err)
	}
}
