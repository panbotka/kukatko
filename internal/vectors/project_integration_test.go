//go:build integration

package vectors_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/vectors"
)

// otherProjectUID is a second project the tests move a photo into, so a
// denormalised project_id that merely took the column's bootstrap DEFAULT
// cannot pass for one copied from the photo.
const otherProjectUID = "pjvectorsotherproject00001"

// photoInOtherProject moves the photo uid into a freshly created
// second project.
func photoInOtherProject(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	ctx := t.Context()
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO projects (uid, slug, name) VALUES ($1, 'vectors-other', 'Other')
		 ON CONFLICT (uid) DO NOTHING`, otherProjectUID); err != nil {
		t.Fatalf("creating the second project: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		"UPDATE photos SET project_id = $1 WHERE uid = $2", otherProjectUID, uid); err != nil {
		t.Fatalf("moving photo %s into the second project: %v", uid, err)
	}
}

// projectsOf returns the distinct project_id values of the rows of table that
// belong to photoUID.
func projectsOf(t *testing.T, db *database.DB, table, photoUID string) []string {
	t.Helper()
	rows, err := db.Pool().Query(t.Context(),
		"SELECT DISTINCT project_id FROM "+table+" WHERE photo_uid = $1 ORDER BY 1", photoUID)
	if err != nil {
		t.Fatalf("reading %s.project_id: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scanning %s.project_id: %v", table, err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating %s.project_id: %v", table, err)
	}
	return out
}

// TestEmbeddingCarriesItsPhotosProject: an embedding written for a photo of a
// non-bootstrap project carries that project, not the column default.
func TestEmbeddingCarriesItsPhotosProject(t *testing.T) {
	store, photoStore, db := newStore(t)
	uid := makePhoto(t, photoStore, "proj_embed")
	photoInOtherProject(t, db, uid)

	saveEmbedding(t, store, uid, imageVec(map[int]float32{0: 1}))
	if got := projectsOf(t, db, "embeddings", uid); !equalStrings(got, []string{otherProjectUID}) {
		t.Fatalf("embedding project = %v, want [%s]", got, otherProjectUID)
	}
}

// TestFacesCarryTheirPhotosProject: faces written by both writers (SaveFaces and
// RecordFaceDetection) for a photo of a non-bootstrap project carry that
// project; a bootstrap photo's faces carry the bootstrap project.
func TestFacesCarryTheirPhotosProject(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()
	saved := makePhoto(t, photoStore, "proj_faces_saved")
	detected := makePhoto(t, photoStore, "proj_faces_detected")
	bootstrap := makePhoto(t, photoStore, "proj_faces_bootstrap")
	photoInOtherProject(t, db, saved)
	photoInOtherProject(t, db, detected)

	face := func(index int) vectors.Face {
		return vectors.Face{
			FaceIndex: index, Vector: faceVec(map[int]float32{index: 1}),
			BBox: [4]float64{0.1, 0.2, 0.3, 0.4}, DetScore: 0.9, Model: "buffalo_l",
		}
	}
	if err := store.SaveFaces(ctx, saved, []vectors.Face{face(0), face(1)}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	if err := store.RecordFaceDetection(ctx, detected,
		vectors.Detection{Faces: []vectors.Face{face(0)}, Model: "buffalo_l", ProjectUID: otherProjectUID}); err != nil {
		t.Fatalf("RecordFaceDetection: %v", err)
	}
	if err := store.SaveFaces(ctx, bootstrap, []vectors.Face{face(0)}); err != nil {
		t.Fatalf("SaveFaces bootstrap: %v", err)
	}

	for _, uid := range []string{saved, detected} {
		if got := projectsOf(t, db, "faces", uid); !equalStrings(got, []string{otherProjectUID}) {
			t.Errorf("faces of %s carry projects %v, want [%s]", uid, got, otherProjectUID)
		}
	}
	if got := projectsOf(t, db, "faces", bootstrap); !equalStrings(got, []string{project.BootstrapUID}) {
		t.Errorf("faces of the bootstrap photo carry projects %v, want [%s]", got, project.BootstrapUID)
	}
}

// TestRecordFaceDetection_refusesAPhotoOutsideTheProject: the detection writes
// faces only for a photo of the project it names — another project's photo is
// ErrPhotoNotInProject and leaves nothing behind, no project at all is
// ErrNoProject — so a worker handed a stale or foreign uid cannot plant faces
// in a project the photo is not in.
func TestRecordFaceDetection_refusesAPhotoOutsideTheProject(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "proj_faces_foreign")
	photoInOtherProject(t, db, uid)
	face := vectors.Face{
		FaceIndex: 0, Vector: faceVec(map[int]float32{0: 1}),
		BBox: [4]float64{0.1, 0.2, 0.3, 0.4}, DetScore: 0.9, Model: "buffalo_l",
	}

	err := store.RecordFaceDetection(ctx, uid, vectors.Detection{
		Faces: []vectors.Face{face}, Model: "buffalo_l", ProjectUID: project.BootstrapUID,
	})
	if !errors.Is(err, vectors.ErrPhotoNotInProject) {
		t.Fatalf("RecordFaceDetection(foreign photo) = %v, want ErrPhotoNotInProject", err)
	}
	if got := projectsOf(t, db, "faces", uid); len(got) != 0 {
		t.Errorf("a refused detection left faces in %v", got)
	}
	err = store.RecordFaceDetection(ctx, uid, vectors.Detection{Faces: []vectors.Face{face}, Model: "buffalo_l"})
	if !errors.Is(err, vectors.ErrNoProject) {
		t.Errorf("RecordFaceDetection(no project) = %v, want ErrNoProject", err)
	}
}

// TestSaveFaces_unknownPhoto: SaveFaces stamps the photo's own project, so a
// uid that names no photo is ErrPhotoNotInProject rather than a row without one.
func TestSaveFaces_unknownPhoto(t *testing.T) {
	store, _, _ := newStore(t)
	err := store.SaveFaces(t.Context(), "ph_missing", []vectors.Face{{
		FaceIndex: 0, Vector: faceVec(map[int]float32{0: 1}), Model: "buffalo_l",
	}})
	if !errors.Is(err, vectors.ErrPhotoNotInProject) {
		t.Errorf("SaveFaces(missing photo) = %v, want ErrPhotoNotInProject", err)
	}
}
