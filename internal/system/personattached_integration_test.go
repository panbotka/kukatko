//go:build integration

package system_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/system"
)

// seedPersonMarker inserts one hand-attached link: a 'person' marker with no box.
func seedPersonMarker(t *testing.T, db *database.DB, uid, photoUID, subjectUID string) {
	t.Helper()
	const stmt = `INSERT INTO markers (uid, photo_uid, subject_uid, type) VALUES ($1, $2, $3, 'person')`
	if _, err := db.Pool().Exec(t.Context(), stmt, uid, photoUID, subjectUID); err != nil {
		t.Fatalf("seed person marker %s: %v", uid, err)
	}
}

// TestCountLibrary_handAttachedPersonIsAssigned verifies a person attached by
// hand is never reported as an unassigned face.
//
// MarkersUnassigned is derived as Markers - MarkersAssigned, so the invariant
// that protects it is that a hand-attached link always names a subject. This test
// pins that down against the real aggregation rather than trusting the schema:
// the gauge is what an operator reads as "faces still waiting for a name", and a
// link that can never be named would sit in that number forever.
func TestCountLibrary_handAttachedPersonIsAssigned(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	seedPhoto(t, db, "p1", "image", false)
	seedSubject(t, db, "s1", "person")
	// One nameless detected region, and one hand-attached person.
	seedMarker(t, db, "m1", "p1", "")
	seedPersonMarker(t, db, "m2", "p1", "s1")

	svc := system.New(system.Config{Library: system.NewStore(db.Pool())})
	got, err := svc.LibraryStats(t.Context())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}

	if got.Markers != 2 || got.MarkersAssigned != 1 {
		t.Fatalf("markers = %d assigned = %d, want 2 and 1", got.Markers, got.MarkersAssigned)
	}
	if got.MarkersUnassigned != 1 {
		t.Errorf("markers_unassigned = %d, want 1 (only the nameless detection)", got.MarkersUnassigned)
	}
}

// TestCountDashboard_handAttachedPersonIsNoDuplicate verifies the
// remaining-work backlog of repeated markers ignores hand-attached links.
//
// Two links naming one person on one photo cannot exist (the partial unique index
// forbids them), and a link next to a detected face of the same person is not a
// mistake to review: there is only one box, so there is nothing for the repair to
// keep or reject. Counting either would send a curator to a page reporting
// nothing.
func TestCountDashboard_handAttachedPersonIsNoDuplicate(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	seedPhoto(t, db, "p1", "image", false)
	seedSubject(t, db, "s1", "person")
	seedMarker(t, db, "m1", "p1", "s1")
	seedPersonMarker(t, db, "m2", "p1", "s1")

	got, err := system.NewStore(db.Pool()).CountDashboard(t.Context())
	if err != nil {
		t.Fatalf("CountDashboard: %v", err)
	}
	if got.Remaining.DuplicateMarkers != 0 {
		t.Errorf("duplicate_markers = %d, want 0", got.Remaining.DuplicateMarkers)
	}
}
