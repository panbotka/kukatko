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

// seedFaceSubject names an already-seeded face. That is what the faces-assigned
// coverage count measures — a detected face somebody has put a name to — as
// opposed to a marker, which may also be a hand-drawn label box.
func seedFaceSubject(t *testing.T, db *database.DB, photoUID string, index int, subjectUID string) {
	t.Helper()
	const stmt = `UPDATE faces SET subject_uid = $3 WHERE photo_uid = $1 AND face_index = $2`
	if _, err := db.Pool().Exec(t.Context(), stmt, photoUID, index, subjectUID); err != nil {
		t.Fatalf("assign face %s/%d: %v", photoUID, index, err)
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

// seedAlbum inserts one album of the given type ('album', 'folder', 'month', ...).
func seedAlbum(t *testing.T, db *database.DB, uid, albumType string) {
	t.Helper()
	const stmt = `INSERT INTO albums (uid, slug, title, type) VALUES ($1, $2, $3, $4)`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, "slug-"+uid, uid, albumType); err != nil {
		t.Fatalf("seed album %s: %v", uid, err)
	}
}

// seedCoordinates stamps GPS coordinates onto an already-seeded photo, which is
// what makes it eligible for reverse geocoding.
func seedCoordinates(t *testing.T, db *database.DB, uid string, lat, lng float64) {
	t.Helper()
	const stmt = `UPDATE photos SET lat = $2, lng = $3 WHERE uid = $1`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, lat, lng); err != nil {
		t.Fatalf("seed coordinates for %s: %v", uid, err)
	}
}

// seedPlace inserts the cached place of a photo. Passing geocoded=false records
// the photo as processed without coordinates — how the `places` job marks a photo
// with no GPS so it never retries it — which must not count as geocoded.
func seedPlace(t *testing.T, db *database.DB, photoUID string, geocoded bool) {
	t.Helper()
	const stmt = `
INSERT INTO photo_places (photo_uid, country, lat, lng)
VALUES ($1, $2, CASE WHEN $3 THEN 50.08 ELSE NULL END, CASE WHEN $3 THEN 14.44 ELSE NULL END)`
	if _, err := db.Pool().Exec(t.Context(), stmt, photoUID, "Česko", geocoded); err != nil {
		t.Fatalf("seed place for %s: %v", photoUID, err)
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
//	p1  image, live,     embedding, 2 faces
//	p2  image, live,     embedding, 1 face
//	p3  video, live,     embedding, no faces
//	p4  image, archived, no embedding, no faces
//	p5  image, live,     no embedding, 1 face
//	p6  video, archived, embedding, no faces
//	p7  live,  live,     the third media type
//	p8  image, live,     GPS, geocoded
//	p9  image, live,     GPS, not geocoded yet — the backlog
//	p10 image, archived, GPS, not geocoded — archived, so not backlog
//	p11 image, live,     no GPS, recorded as processed without coordinates
//
// plus three subjects (one of each kind), one of p1's two faces named, four
// markers (two named), three albums of three different types and three labels.
func seedLibrary(t *testing.T, db *database.DB) {
	t.Helper()
	seedPhoto(t, db, "p1", "image", false)
	seedPhoto(t, db, "p2", "image", false)
	seedPhoto(t, db, "p3", "video", false)
	seedPhoto(t, db, "p4", "image", true)
	seedPhoto(t, db, "p5", "image", false)
	seedPhoto(t, db, "p6", "video", true)
	seedPhoto(t, db, "p7", "live", false)

	for _, uid := range []string{"p1", "p2", "p3", "p6"} {
		seedEmbedding(t, db, uid)
	}
	seedFace(t, db, "p1", 0)
	seedFace(t, db, "p1", 1)
	seedFace(t, db, "p2", 0)
	seedFace(t, db, "p5", 0)

	seedPlaces(t, db)

	seedSubject(t, db, "s1", "person")
	seedSubject(t, db, "s2", "pet")
	seedSubject(t, db, "s3", "other")

	seedFaceSubject(t, db, "p1", 0, "s1")

	seedMarker(t, db, "m1", "p1", "s1")
	seedMarker(t, db, "m2", "p1", "s2")
	seedMarker(t, db, "m3", "p2", "")
	seedMarker(t, db, "m4", "p5", "")

	seedAlbum(t, db, "a1", "album")
	seedAlbum(t, db, "a2", "folder")
	seedAlbum(t, db, "a3", "month")
	seedLabel(t, db, "l1")
	seedLabel(t, db, "l2")
	seedLabel(t, db, "l3")
}

// seedPlaces adds the geocoding half of the fixture: one geocoded photo, one
// still in the backlog, one archived (which the backlog must ignore) and one
// recorded as processed without coordinates (which must not count as geocoded).
func seedPlaces(t *testing.T, db *database.DB) {
	t.Helper()
	seedPhoto(t, db, "p8", "image", false)
	seedPhoto(t, db, "p9", "image", false)
	seedPhoto(t, db, "p10", "image", true)
	seedPhoto(t, db, "p11", "image", false)

	seedCoordinates(t, db, "p8", 50.08, 14.44)
	seedCoordinates(t, db, "p9", 49.19, 16.61)
	seedCoordinates(t, db, "p10", 48.97, 14.47)

	seedPlace(t, db, "p8", true)
	seedPlace(t, db, "p11", false)
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
		Photos:                 11,
		Videos:                 2,
		LivePhotos:             1,
		Images:                 8,
		PhotosLive:             8,
		PhotosArchived:         3,
		PhotosWithEmbedding:    4,
		PhotosWithFaces:        3,
		PhotosWithoutEmbedding: 7,
		PhotosWithoutFaces:     8,
		PhotosWithGPS:          3,
		PhotosGeocoded:         1,
		PhotosPendingGeocode:   1,
		Embeddings:             4,
		Faces:                  4,
		FacesAssigned:          1,
		FacesUnassigned:        3,
		Subjects:               3,
		SubjectsPerson:         1,
		SubjectsPet:            1,
		SubjectsOther:          1,
		Markers:                4,
		MarkersAssigned:        2,
		MarkersUnassigned:      2,
		Albums:                 3,
		AlbumsManual:           1,
		AlbumsFolder:           1,
		AlbumsMonth:            1,
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
	if got.Photos != 11 || got.PhotosWithEmbedding != 4 {
		t.Errorf("counts = %+v, want photos 11 / with embedding 4", got)
	}
	if got.PhotosLive != 0 || got.Images != 0 || got.PhotosWithoutEmbedding != 0 ||
		got.PhotosWithoutFaces != 0 || got.FacesUnassigned != 0 || got.MarkersUnassigned != 0 {
		t.Errorf("counts = %+v, want the derived fields left at zero", got)
	}
}
