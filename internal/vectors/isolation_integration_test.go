//go:build integration

package vectors_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These are the tenant-isolation tests of the similarity searches. The two
// seeded projects each get a face and an image embedding with an IDENTICAL
// vector, so the other project's row is always a perfect (distance 0) neighbour:
// any search that ignored the project would return it first.

// isolationFace is the one face every isolation photo carries.
func isolationFace(vec []float32) vectors.Face {
	return vectors.Face{FaceIndex: 0, Vector: vec, BBox: [4]float64{0.2, 0.2, 0.3, 0.3}, DetScore: 0.9, Model: "buffalo_l"}
}

// TestKNN_neverCrossesProjects runs every project-filtered kNN from inside each
// project with the shared vector and asserts the answer is exactly the
// project's own photo.
func TestKNN_neverCrossesProjects(t *testing.T) {
	store, _, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	face := faceVec(map[int]float32{3: 1, 7: 0.5})
	image := imageVec(map[int]float32{11: 1, 42: 0.25})
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		uid := f.Tenant(side).PhotoUID
		if err := store.SaveFaces(ctx, uid, []vectors.Face{isolationFace(face)}); err != nil {
			t.Fatalf("SaveFaces %s: %v", side, err)
		}
		saveEmbedding(t, store, uid, image)
	}

	searches := map[string]func(projectUID string) ([]string, error){
		"FindSimilar": func(projectUID string) ([]string, error) {
			matches, err := store.FindSimilar(ctx, projectUID, image, 10, 0)
			return uids(matches), err
		},
		"FindSimilarFaces": func(projectUID string) ([]string, error) {
			matches, err := store.FindSimilarFaces(ctx, projectUID, face, 10, 0)
			out := make([]string, 0, len(matches))
			for _, m := range matches {
				out = append(out, m.PhotoUID)
			}
			return out, err
		},
		"FindSimilarFaceCandidates": func(projectUID string) ([]string, error) {
			found, err := store.FindSimilarFaceCandidates(ctx, projectUID, face, 10, 0)
			return candidatePhotos(found), err
		},
		"FindSimilarUnassignedFaceCandidates": func(projectUID string) ([]string, error) {
			found, err := store.FindSimilarUnassignedFaceCandidates(ctx, projectUID, face, 10, 0, nil)
			return candidatePhotos(found), err
		},
	}
	for name, search := range searches {
		t.Run(name, func(t *testing.T) {
			f.AssertStoreScoped(t, search)
			for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
				got, err := search(f.Tenant(side).ProjectUID)
				if err != nil {
					t.Fatalf("%s inside %s: %v", name, side, err)
				}
				if want := f.Tenant(side).PhotoUID; len(got) != 1 || got[0] != want {
					t.Errorf("%s inside %s = %v, want only [%s]", name, side, got, want)
				}
			}
			if _, err := search(""); !errors.Is(err, vectors.ErrNoProject) {
				t.Errorf("%s without a project = %v, want ErrNoProject", name, err)
			}
		})
	}
}

// TestKNN_smallProjectKeepsItsNeighbours crowds the shared vector's
// neighbourhood with many identical faces in A and asserts a search inside B —
// one face, buried behind all of A's — still finds B's face: the project filter
// is applied inside the index scan (iterative where pgvector supports it), not
// to the first ef_search candidates afterwards.
func TestKNN_smallProjectKeepsItsNeighbours(t *testing.T) {
	store, photoStore, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	face := faceVec(map[int]float32{5: 1})

	const crowd = 150
	for i := range crowd {
		uid := makePhoto(t, photoStore, fmt.Sprintf("crowd_%03d", i))
		if err := store.SaveFaces(ctx, uid, []vectors.Face{isolationFace(face)}); err != nil {
			t.Fatalf("SaveFaces crowd %d: %v", i, err)
		}
	}
	if err := store.SaveFaces(ctx, f.B.PhotoUID, []vectors.Face{isolationFace(face)}); err != nil {
		t.Fatalf("SaveFaces B: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "ANALYZE faces"); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	found, err := store.FindSimilarFaceCandidates(ctx, f.B.ProjectUID, face, 5, 0)
	if err != nil {
		t.Fatalf("FindSimilarFaceCandidates: %v", err)
	}
	if got := candidatePhotos(found); len(got) != 1 || got[0] != f.B.PhotoUID {
		t.Errorf("B's neighbours = %v, want only B's photo %s", got, f.B.PhotoUID)
	}
	crowded, err := store.FindSimilarFaceCandidates(ctx, f.A.ProjectUID, face, 5, 0)
	if err != nil {
		t.Fatalf("FindSimilarFaceCandidates A: %v", err)
	}
	if len(crowded) != 5 {
		t.Errorf("A's search returned %d faces, want the full 5", len(crowded))
	}
	for _, c := range crowded {
		if c.PhotoUID == f.B.PhotoUID {
			t.Errorf("A's search returned B's face")
		}
	}
}

// candidatePhotos returns the photo uid of every candidate, in order.
func candidatePhotos(found []vectors.FaceCandidate) []string {
	out := make([]string, 0, len(found))
	for _, c := range found {
		out = append(out, c.PhotoUID)
	}
	return out
}

// TestFindDuplicatePairs_neverCrossesProjects gives each project two photos and
// all four the IDENTICAL image embedding, so every photo is a distance-0
// neighbour of every other: a scan that ignored the project would pair A's
// photos with B's. Inside each project the pairs name only that project's two
// photos.
func TestFindDuplicatePairs_neverCrossesProjects(t *testing.T) {
	store, photoStore, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	image := imageVec(map[int]float32{5: 1, 9: 0.5})
	own := map[projecttest.Side][]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tenant := f.Tenant(side)
		twin, err := photoStore.Create(ctx, photos.Photo{
			ProjectID: tenant.ProjectUID,
			FileHash:  "dup-twin-" + tenant.Token,
			FilePath:  "2024/01/dup-twin-" + tenant.Token + ".jpg",
			FileName:  "dup-twin.jpg",
		})
		if err != nil {
			t.Fatalf("creating %s's twin: %v", side, err)
		}
		own[side] = []string{tenant.PhotoUID, twin.UID}
		for _, uid := range own[side] {
			saveEmbedding(t, store, uid, image)
		}
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		pairs, err := store.FindDuplicatePairs(ctx, projectUID, 8, 0.05)
		out := make([]string, 0, 2*len(pairs))
		for _, p := range pairs {
			out = append(out, p.A, p.B)
		}
		return out, err
	})
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		pairs, err := store.FindDuplicatePairs(ctx, f.Tenant(side).ProjectUID, 8, 0.05)
		if err != nil {
			t.Fatalf("FindDuplicatePairs inside %s: %v", side, err)
		}
		if !hasPair(pairs, own[side][0], own[side][1]) {
			t.Errorf("inside %s: want the pair %v, got %+v", side, own[side], pairs)
		}
		for _, p := range pairs {
			if !slices.Contains(own[side], p.A) || !slices.Contains(own[side], p.B) {
				t.Errorf("inside %s: pair %+v names a photo of the other project", side, p)
			}
		}
	}
	if _, err := store.FindDuplicatePairs(ctx, "", 8, 0.05); !errors.Is(err, vectors.ErrNoProject) {
		t.Errorf("FindDuplicatePairs without a project = %v, want ErrNoProject", err)
	}
}
