//go:build integration

package system_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// seedPhoto inserts one photo row with the given uid, media type and archived
// state. Only the NOT NULL columns without a default are supplied; everything
// else keeps its schema default, which is what a counting test cares about.
func seedPhoto(t *testing.T, db *database.DB, uid, mediaType string, archived bool) {
	t.Helper()
	const stmt = `
INSERT INTO photos (uid, file_hash, file_path, file_name, media_type, archived_at)
VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 THEN now() ELSE NULL END)`
	_, err := db.Pool().Exec(t.Context(), stmt,
		uid, "hash-"+uid, "2026/07/"+uid+".jpg", uid+".jpg", mediaType, archived)
	if err != nil {
		t.Fatalf("seed photo %s: %v", uid, err)
	}
}

// seedEmbedding inserts a zero image embedding for the photo. The vector's
// content is irrelevant to a count; only its dimension has to satisfy the column.
func seedEmbedding(t *testing.T, db *database.DB, photoUID string) {
	t.Helper()
	const stmt = `INSERT INTO embeddings (photo_uid, embedding) VALUES ($1, $2)`
	vec := vectors.ToHalfVec(make([]float32, vectors.ImageDim))
	if _, err := db.Pool().Exec(t.Context(), stmt, photoUID, vec); err != nil {
		t.Fatalf("seed embedding for %s: %v", photoUID, err)
	}
}

// seedFace inserts one detected face for the photo at the given slot.
func seedFace(t *testing.T, db *database.DB, photoUID string, index int) {
	t.Helper()
	const stmt = `INSERT INTO faces (photo_uid, face_index, embedding, bbox) VALUES ($1, $2, $3, $4)`
	vec := vectors.ToHalfVec(make([]float32, vectors.FaceDim))
	bbox := []float64{0.1, 0.1, 0.2, 0.2}
	if _, err := db.Pool().Exec(t.Context(), stmt, photoUID, index, vec, bbox); err != nil {
		t.Fatalf("seed face %s/%d: %v", photoUID, index, err)
	}
}

// seedSubject inserts one subject of the given kind (person, pet, other).
func seedSubject(t *testing.T, db *database.DB, uid, kind string) {
	t.Helper()
	const stmt = `INSERT INTO subjects (uid, slug, name, type) VALUES ($1, $2, $3, $4)`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, "slug-"+uid, uid, kind); err != nil {
		t.Fatalf("seed subject %s: %v", uid, err)
	}
}

// seedMarker inserts one marker on a photo, assigned to subjectUID when non-empty
// and left nameless otherwise.
func seedMarker(t *testing.T, db *database.DB, uid, photoUID, subjectUID string) {
	t.Helper()
	const stmt = `INSERT INTO markers (uid, photo_uid, subject_uid) VALUES ($1, $2, $3)`
	var subject *string
	if subjectUID != "" {
		subject = &subjectUID
	}
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, photoUID, subject); err != nil {
		t.Fatalf("seed marker %s: %v", uid, err)
	}
}

// seedAlbum inserts one album.
func seedAlbum(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	const stmt = `INSERT INTO albums (uid, slug, title) VALUES ($1, $2, $3)`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, "slug-"+uid, uid); err != nil {
		t.Fatalf("seed album %s: %v", uid, err)
	}
}

// seedLabel inserts one label.
func seedLabel(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	const stmt = `INSERT INTO labels (uid, slug, name) VALUES ($1, $2, $3)`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, "slug-"+uid, uid); err != nil {
		t.Fatalf("seed label %s: %v", uid, err)
	}
}

// seedLibrary fills the empty database with a fixture whose every count is known
// by construction:
//
//	p1 image,   live,     embedding, 2 faces
//	p2 image,   live,     embedding, 1 face
//	p3 video,   live,     embedding, no faces
//	p4 image,   archived, no embedding, no faces
//	p5 image,   live,     no embedding, 1 face
//	p6 video,   archived, embedding, no faces
//
// plus three subjects (one of each kind), four markers (two named), two albums
// and three labels.
func seedLibrary(t *testing.T, db *database.DB) {
	t.Helper()
	seedPhoto(t, db, "p1", "image", false)
	seedPhoto(t, db, "p2", "image", false)
	seedPhoto(t, db, "p3", "video", false)
	seedPhoto(t, db, "p4", "image", true)
	seedPhoto(t, db, "p5", "image", false)
	seedPhoto(t, db, "p6", "video", true)

	for _, uid := range []string{"p1", "p2", "p3", "p6"} {
		seedEmbedding(t, db, uid)
	}
	seedFace(t, db, "p1", 0)
	seedFace(t, db, "p1", 1)
	seedFace(t, db, "p2", 0)
	seedFace(t, db, "p5", 0)

	seedSubject(t, db, "s1", "person")
	seedSubject(t, db, "s2", "pet")
	seedSubject(t, db, "s3", "other")

	seedMarker(t, db, "m1", "p1", "s1")
	seedMarker(t, db, "m2", "p1", "s2")
	seedMarker(t, db, "m3", "p2", "")
	seedMarker(t, db, "m4", "p5", "")

	seedAlbum(t, db, "a1")
	seedAlbum(t, db, "a2")
	seedLabel(t, db, "l1")
	seedLabel(t, db, "l2")
	seedLabel(t, db, "l3")
}

// TestLibraryStats_CountsFixture asserts every count and every derived coverage
// gap against a seeded fixture whose numbers are known by construction.
func TestLibraryStats_CountsFixture(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedLibrary(t, db)

	svc := system.New(system.Config{Library: system.NewStore(db.Pool())})
	got, err := svc.LibraryStats(t.Context())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}

	want := system.Library{
		Photos:                 6,
		Videos:                 2,
		PhotosLive:             4,
		PhotosArchived:         2,
		PhotosWithEmbedding:    4,
		PhotosWithFaces:        3,
		PhotosWithoutEmbedding: 2,
		PhotosWithoutFaces:     3,
		Embeddings:             4,
		Faces:                  4,
		Subjects:               3,
		SubjectsPerson:         1,
		SubjectsPet:            1,
		SubjectsOther:          1,
		Markers:                4,
		MarkersAssigned:        2,
		MarkersUnassigned:      2,
		Albums:                 2,
		Labels:                 3,
	}
	if got != want {
		t.Errorf("LibraryStats() = %+v, want %+v", got, want)
	}
}

// TestLibraryStats_EmptyLibrary verifies a freshly truncated database reports
// zeroes rather than failing — an instance before its first import must still
// render the statistics page.
func TestLibraryStats_EmptyLibrary(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	svc := system.New(system.Config{Library: system.NewStore(db.Pool())})
	got, err := svc.LibraryStats(t.Context())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}
	if got != (system.Library{}) {
		t.Errorf("LibraryStats() = %+v, want all zeroes", got)
	}
}

// TestCountLibrary_RawCountsLeaveDerivedZero verifies the store reports only what
// the database counted: the derived values are the service's job, so a caller
// reading the store directly cannot mistake a stale derivation for a count.
func TestCountLibrary_RawCountsLeaveDerivedZero(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedLibrary(t, db)

	got, err := system.NewStore(db.Pool()).CountLibrary(t.Context())
	if err != nil {
		t.Fatalf("CountLibrary: %v", err)
	}
	if got.Photos != 6 || got.PhotosWithEmbedding != 4 {
		t.Errorf("counts = %+v, want photos 6 / with embedding 4", got)
	}
	if got.PhotosLive != 0 || got.PhotosWithoutEmbedding != 0 ||
		got.PhotosWithoutFaces != 0 || got.MarkersUnassigned != 0 {
		t.Errorf("counts = %+v, want the derived fields left at zero", got)
	}
}
