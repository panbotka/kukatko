//go:build integration

package system_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// seedArrival backdates when a photo entered the catalogue, which is what the
// upload windows are counted against.
func seedArrival(t *testing.T, db *database.DB, uid string, ago time.Duration) {
	t.Helper()
	const stmt = `UPDATE photos SET created_at = now() - $2::interval WHERE uid = $1`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, ago.String()); err != nil {
		t.Fatalf("backdate photo %s: %v", uid, err)
	}
}

// seedFileSize stamps the original's size onto a photo, the column the two
// storage sums add up.
func seedFileSize(t *testing.T, db *database.DB, uid string, size int64) {
	t.Helper()
	const stmt = `UPDATE photos SET file_size = $2 WHERE uid = $1`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, size); err != nil {
		t.Fatalf("size photo %s: %v", uid, err)
	}
}

// seedPrivate marks a photo private.
func seedPrivate(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	const stmt = `UPDATE photos SET private = true WHERE uid = $1`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid); err != nil {
		t.Fatalf("mark photo %s private: %v", uid, err)
	}
}

// seedTakenAt stamps a capture time onto a photo, taking it out of the
// "no capture time" backlog.
func seedTakenAt(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	const stmt = `UPDATE photos SET taken_at = now() WHERE uid = $1`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid); err != nil {
		t.Fatalf("stamp taken_at on %s: %v", uid, err)
	}
}

// seedOCRDone records that text recognition has run on a photo, whatever it
// read — which is exactly what takes it out of the OCR backlog.
func seedOCRDone(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	const stmt = `UPDATE photos SET ocr_at = now() WHERE uid = $1`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid); err != nil {
		t.Fatalf("stamp ocr_at on %s: %v", uid, err)
	}
}

// seedCluster inserts one auto-cluster waiting for a name.
func seedCluster(t *testing.T, db *database.DB, uid string) {
	t.Helper()
	const stmt = `INSERT INTO face_clusters (uid, centroid, size) VALUES ($1, $2, 2)`
	vec := vectors.ToHalfVec(make([]float32, vectors.FaceDim))
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, vec); err != nil {
		t.Fatalf("seed cluster %s: %v", uid, err)
	}
}

// seedMarkerDismissal records "this person really is marked twice here, leave
// it" for a (photo, subject) pair — the opinion the backlog count must respect.
func seedMarkerDismissal(t *testing.T, db *database.DB, photoUID, subjectUID string) {
	t.Helper()
	const stmt = `INSERT INTO duplicate_marker_dismissals (photo_uid, subject_uid) VALUES ($1, $2)`
	if _, err := db.Pool().Exec(t.Context(), stmt, photoUID, subjectUID); err != nil {
		t.Fatalf("seed marker dismissal %s/%s: %v", photoUID, subjectUID, err)
	}
}

// seedDashboard extends the library fixture (see seedLibrary) with what the
// dashboard adds on top of the statistics counts:
//
//   - arrivals: p1 two days ago, p2 ten days ago, p3 a hundred days ago and p4
//     four hundred days ago; the other twelve arrived just now. That makes the
//     four upload windows 12 / 13 / 14 / 15 of the sixteen photos.
//   - sizes: 1000 bytes on the live p1 and 500 on the archived p4, so the library
//     and trash sums are told apart rather than merely summed.
//   - one private photo, one photo with a capture time, one photo already OCR'd.
//   - two clusters waiting for a name.
//   - two people marked twice on a photo, one of which a curator has already
//     settled — so the backlog reports one finding, not two.
func seedDashboard(t *testing.T, db *database.DB) {
	t.Helper()
	seedLibrary(t, db)

	seedArrival(t, db, "p1", 2*24*time.Hour)
	seedArrival(t, db, "p2", 10*24*time.Hour)
	seedArrival(t, db, "p3", 100*24*time.Hour)
	seedArrival(t, db, "p4", 400*24*time.Hour)

	seedFileSize(t, db, "p1", 1000)
	seedFileSize(t, db, "p4", 500)

	seedPrivate(t, db, "p2")
	seedTakenAt(t, db, "p5")
	seedOCRDone(t, db, "p5")

	seedCluster(t, db, "c1")
	seedCluster(t, db, "c2")

	// p1 already carries m1 (s1); a second marker for the same person on the same
	// photo is the finding. p2's pair is dismissed and must not be counted.
	seedMarker(t, db, "m5", "p1", "s1")
	seedMarker(t, db, "m6", "p2", "s1")
	seedMarker(t, db, "m7", "p2", "s1")
	seedMarkerDismissal(t, db, "p2", "s1")
}

// TestCountDashboard_CountsFixture asserts every dashboard aggregate against a
// fixture whose numbers are known by construction.
func TestCountDashboard_CountsFixture(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedDashboard(t, db)

	got, err := system.NewStore(db.Pool()).CountDashboard(t.Context())
	if err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}

	wantLibrary := system.LibrarySummary{
		Photos:  13,
		Videos:  1,
		Trashed: 3,
		Hidden:  2,
		Private: 1,
		Uploads: system.Uploads{Day: 12, Week: 13, Month: 14, Year: 15},
		Albums:  3,
		Labels:  3,
		People:  1,
		// p15 is hidden and a stack sibling; every count above treats the sets it
		// belongs to as they are, so nothing here double-counts it.
		Faces:        4,
		Embeddings:   4,
		LibraryBytes: 1000,
		TrashBytes:   500,
	}
	if got.Library != wantLibrary {
		t.Errorf("library = %+v, want %+v", got.Library, wantLibrary)
	}

	wantRemaining := system.RemainingWork{
		FacesUnassigned:      3,
		Clusters:             2,
		PhotosWithoutTakenAt: 12,
		PhotosWithoutGPS:     11,
		PhotosWithoutPlace:   11,
		PhotosWithoutOCR:     11,
		DuplicateMarkers:     1,
	}
	if got.Remaining != wantRemaining {
		t.Errorf("remaining = %+v, want %+v", got.Remaining, wantRemaining)
	}
}

// TestCountDashboard_AgreesWithLibraryStats is the point of having two
// aggregations: they may report different things, but never a different number
// for the same thing. The browsable catalogue, the trash and the hidden photos
// are counted by both, and both are read from the same store.
func TestCountDashboard_AgreesWithLibraryStats(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedDashboard(t, db)

	store := system.NewStore(db.Pool())
	dash, err := store.CountDashboard(t.Context())
	if err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}
	stats, err := system.New(system.Config{Library: store}).LibraryStats(t.Context())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}

	if dash.Library.Photos != stats.PhotosLive {
		t.Errorf("dashboard photos = %d, want the statistics' live count %d",
			dash.Library.Photos, stats.PhotosLive)
	}
	if dash.Library.Trashed != stats.PhotosArchived {
		t.Errorf("dashboard trashed = %d, want %d", dash.Library.Trashed, stats.PhotosArchived)
	}
	if dash.Library.Hidden != stats.PhotosHidden {
		t.Errorf("dashboard hidden = %d, want %d", dash.Library.Hidden, stats.PhotosHidden)
	}
	if dash.Remaining.FacesUnassigned != stats.FacesUnassigned {
		t.Errorf("dashboard nameless faces = %d, want %d",
			dash.Remaining.FacesUnassigned, stats.FacesUnassigned)
	}
}

// TestCountDashboard_EmptyLibrary verifies a freshly truncated database reports
// zeroes rather than failing — an instance before its first import must still
// render the dashboard.
func TestCountDashboard_EmptyLibrary(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	got, err := system.NewStore(db.Pool()).CountDashboard(t.Context())
	if err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}
	if got != (system.Dashboard{}) {
		t.Errorf("CountDashboard() = %+v, want all zeroes", got)
	}
}
