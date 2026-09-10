//go:build integration

package vectors_test

import (
	"testing"
)

// TestCountMarkersWithoutFace_ignoresHandAttached verifies the outlier report's
// "assignments we could not score" count ignores a person attached by hand.
//
// Such a link has no region and no embedding by construction, so it matches the
// anti-join on faces perfectly — and would be reported as one more face the
// library failed to embed, sending an operator looking for a detection problem
// that is not there.
func TestCountMarkersWithoutFace_ignoresHandAttached(t *testing.T) {
	store, photoStore, db := newStore(t)
	ctx := t.Context()

	photoUID := makePhoto(t, photoStore, "hash-unscored")
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO subjects (uid, slug, name) VALUES ('su_1', 'ota', 'Ota')`); err != nil {
		t.Fatalf("seeding subject: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
		 VALUES ('mk_face', $1, 'su_1', 'face', 0.1, 0.1, 0.2, 0.2)`, photoUID); err != nil {
		t.Fatalf("seeding face marker: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO markers (uid, photo_uid, subject_uid, type)
		 VALUES ('mk_person', $1, 'su_1', 'person')`, photoUID); err != nil {
		t.Fatalf("seeding person marker: %v", err)
	}

	got, err := store.CountMarkersWithoutFace(ctx, "su_1")
	if err != nil {
		t.Fatalf("CountMarkersWithoutFace: %v", err)
	}
	if got != 1 {
		t.Errorf("CountMarkersWithoutFace = %d, want 1 (the face marker only)", got)
	}
}
