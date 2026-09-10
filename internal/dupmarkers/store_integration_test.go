//go:build integration

package dupmarkers_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/dupmarkers"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

// seedRepeatFixture inserts one photo, one named subject, two face markers of
// that subject on it (the genuine finding) and one hand-attached link to the same
// person on the same photo.
func seedRepeatFixture(t *testing.T, db *database.DB) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Pool().Exec(t.Context(), sql, args...); err != nil {
			t.Fatalf("seeding (%s): %v", sql, err)
		}
	}
	exec(`INSERT INTO photos (uid, file_hash, file_path, file_name)
	      VALUES ('ph_1', 'h1', '2026/01/h1.jpg', 'h1.jpg'), ('ph_2', 'h2', '2026/01/h2.jpg', 'h2.jpg')`)
	exec(`INSERT INTO subjects (uid, slug, name) VALUES ('su_1', 'jana', 'Jana')`)
	exec(`INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h) VALUES
	      ('mk_1', 'ph_1', 'su_1', 'face', 0.1, 0.1, 0.2, 0.2),
	      ('mk_2', 'ph_1', 'su_1', 'face', 0.5, 0.5, 0.2, 0.2)`)
	// The same person, attached by hand, on a photo where they are marked once and
	// on a photo where they are not marked at all.
	exec(`INSERT INTO markers (uid, photo_uid, subject_uid, type) VALUES
	      ('mk_3', 'ph_1', 'su_1', 'person'),
	      ('mk_4', 'ph_2', 'su_1', 'person')`)
}

// TestListRepeatedMarkers_ignoresHandAttached verifies the repeated-marker
// listing reports only real face regions.
//
// The repair a finding leads to is "keep this box, reject that one", and a
// hand-attached person has no box to keep or reject. Counting it would turn every
// photo where somebody is both detected and attached into a finding no repair can
// act on — and would report a group of one on a photo carrying nothing else.
func TestListRepeatedMarkers_ignoresHandAttached(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	seedRepeatFixture(t, db)

	rows, err := dupmarkers.NewStore(db.Pool()).ListRepeatedMarkers(t.Context())
	if err != nil {
		t.Fatalf("ListRepeatedMarkers: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the two face markers of ph_1", rows)
	}
	for _, row := range rows {
		if row.MarkerUID != "mk_1" && row.MarkerUID != "mk_2" {
			t.Errorf("row %+v, want only the face markers", row)
		}
	}
}
