//go:build integration

package facematch_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestPhotoPeople_neverCrossesProjects gives each seeded project's photo one
// detected face next to its seeded marker and asks for the roll-call of every
// photo from inside every project, with no scoped load of the photo in front of
// it: a project's own photo lists its detection, the other project's photo lists
// nobody — not even the bare detection, which an unscoped face read would leak
// (a face count and boxes) — exactly like a photo with no faces at all. Without
// a project the read refuses.
func TestPhotoPeople_neverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	faces := vectors.NewStore(pool)
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		vec := make([]float32, vectors.FaceDim)
		vec[int(side)] = 1
		if err := faces.SaveFaces(t.Context(), f.Tenant(side).PhotoUID, []vectors.Face{{
			FaceIndex: 0, Vector: vec, BBox: [4]float64{0.7, 0.7, 0.2, 0.2}, DetScore: 0.9,
		}}); err != nil {
			t.Fatalf("SaveFaces %s: %v", side, err)
		}
	}
	svc := facematch.New(facematch.Config{
		Photos: photos.NewStore(pool), Faces: faces, People: people.NewStore(pool),
	})

	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		own, other := f.Tenant(side), f.Tenant(side.Other())
		mine, err := svc.PhotoPeople(t.Context(), own.ProjectUID, own.PhotoUID)
		if err != nil {
			t.Fatalf("PhotoPeople of %s's own photo: %v", side, err)
		}
		detected := 0
		for _, person := range mine {
			if person.DetScore > 0 {
				detected++
			}
		}
		if detected != 1 {
			t.Errorf("roll-call of %s's own photo = %+v, want its one detection", side, mine)
		}
		foreign, err := svc.PhotoPeople(t.Context(), own.ProjectUID, other.PhotoUID)
		if err != nil {
			t.Fatalf("PhotoPeople of %s's photo from %s: %v", side.Other(), side, err)
		}
		if len(foreign) != 0 {
			t.Errorf("roll-call of %s's photo from %s = %+v, want nobody", side.Other(), side, foreign)
		}
	}
	if _, err := svc.PhotoPeople(t.Context(), "", f.A.PhotoUID); err == nil {
		t.Error("PhotoPeople without a project succeeded, want an error")
	}
}
