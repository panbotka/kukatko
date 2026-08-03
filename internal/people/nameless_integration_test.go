//go:build integration

package people_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They cover the repair for the catch-all subject an
// importer minted in production: listing nameless subjects, detaching one, and
// replaying the snapshot that undoes it.

// seedAssignedFace creates a photo, a face marker on it assigned to subjectUID and
// a cached face row linked to that marker, and returns the photo and marker uids.
// It is the shape the repair has to unwind: a marker pointing at a subject and a
// faces row caching that subject.
func seedAssignedFace(
	t *testing.T, store *people.Store, photoStore *photos.Store, vecStore *vectors.Store,
	hash, subjectUID string,
) (photoUID, markerUID string) {
	t.Helper()
	photoUID = makePhoto(t, photoStore, hash)
	marker, err := store.CreateMarker(t.Context(), people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 0.1, Y: 0.1, W: 0.3, H: 0.3,
	})
	if err != nil {
		t.Fatalf("CreateMarker on %s: %v", hash, err)
	}
	saveLinkedFace(t, vecStore, photoUID, marker.UID)
	if _, err := store.AssignSubject(t.Context(), marker.UID, subjectUID); err != nil {
		t.Fatalf("AssignSubject %s: %v", marker.UID, err)
	}
	return photoUID, marker.UID
}

// markerSubject returns the subject a marker is assigned to, or nil when it is
// unassigned.
func markerSubject(t *testing.T, store *people.Store, markerUID string) *string {
	t.Helper()
	marker, err := store.GetMarkerByUID(t.Context(), markerUID)
	if err != nil {
		t.Fatalf("GetMarkerByUID %s: %v", markerUID, err)
	}
	return marker.SubjectUID
}

// TestListNamelessSubjects reports only the subjects whose name identifies nobody,
// with the markers and faces currently hanging off them, and leaves the catalogue
// untouched — it is the dry run of the repair.
func TestListNamelessSubjects(t *testing.T) {
	store, photoStore, vecStore, _ := newStores(t)
	ctx := t.Context()

	// The catch-all as production has it: an empty name, created by an importer
	// that keyed find-or-create on the fallback slug.
	catchAll, err := store.CreateSubject(ctx, people.Subject{Name: ""})
	if err != nil {
		t.Fatalf("CreateSubject nameless: %v", err)
	}
	if catchAll.Slug != "subject" {
		t.Fatalf("nameless subject slug = %q, want the fallback %q", catchAll.Slug, "subject")
	}
	punctuation, err := store.CreateSubject(ctx, people.Subject{Name: "!!!"})
	if err != nil {
		t.Fatalf("CreateSubject punctuation: %v", err)
	}
	alice, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject Alice: %v", err)
	}
	seedAssignedFace(t, store, photoStore, vecStore, "nameless_a", catchAll.UID)
	seedAssignedFace(t, store, photoStore, vecStore, "nameless_b", catchAll.UID)
	seedAssignedFace(t, store, photoStore, vecStore, "named_a", alice.UID)

	found, err := store.ListNamelessSubjects(ctx)
	if err != nil {
		t.Fatalf("ListNamelessSubjects: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("nameless subjects = %d (%+v), want 2", len(found), found)
	}
	byUID := map[string]people.NamelessSubject{}
	for _, ns := range found {
		byUID[ns.UID] = ns
	}
	if _, ok := byUID[alice.UID]; ok {
		t.Errorf("Alice reported as nameless")
	}
	if _, ok := byUID[punctuation.UID]; !ok {
		t.Errorf("a punctuation-only name is not reported as nameless")
	}
	got := byUID[catchAll.UID]
	if got.MarkerCount != 2 || got.FaceCount != 2 {
		t.Errorf("catch-all counts = %d markers / %d faces, want 2/2", got.MarkerCount, got.FaceCount)
	}
	// Read-only: the dry run must not have moved anything.
	if _, err := store.GetSubjectByUID(ctx, catchAll.UID); err != nil {
		t.Errorf("the dry run deleted the subject: %v", err)
	}
}

// TestDetachSubject_roundTripsThroughItsSnapshot is the whole repair: detaching
// leaves the catch-all's markers and faces unassigned and the subject gone, and
// replaying the returned snapshot puts every link back. Reversibility is the
// reason this is a CLI step rather than a migration, so it is asserted, not
// assumed.
func TestDetachSubject_roundTripsThroughItsSnapshot(t *testing.T) {
	store, photoStore, vecStore, db := newStores(t)
	ctx := t.Context()
	actor := makeUser(t, db, "usr_detach0000000000000000000", "detacher")

	catchAll, err := store.CreateSubject(ctx, people.Subject{Name: "", Notes: "importer artefact"})
	if err != nil {
		t.Fatalf("CreateSubject nameless: %v", err)
	}
	alice, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject Alice: %v", err)
	}
	photoA, markerA := seedAssignedFace(t, store, photoStore, vecStore, "detach_a", catchAll.UID)
	_, markerB := seedAssignedFace(t, store, photoStore, vecStore, "detach_b", catchAll.UID)
	photoAlice, markerAlice := seedAssignedFace(t, store, photoStore, vecStore, "detach_c", alice.UID)

	snap, err := store.DetachSubject(ctx, catchAll.UID,
		actorEntry(actor, audit.ActionSubjectDelete, "subject", catchAll.UID,
			map[string]any{"reason": "nameless catch-all"}))
	if err != nil {
		t.Fatalf("DetachSubject: %v", err)
	}
	if snap.Subject.UID != catchAll.UID || snap.Subject.Notes != "importer artefact" {
		t.Errorf("snapshot subject = %+v, want the deleted row verbatim", snap.Subject)
	}
	if len(snap.MarkerUIDs) != 2 || len(snap.Faces) != 2 {
		t.Fatalf("snapshot = %d markers / %d faces, want 2/2", len(snap.MarkerUIDs), len(snap.Faces))
	}
	if _, err := store.GetSubjectByUID(ctx, catchAll.UID); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("subject after detach = %v, want ErrSubjectNotFound", err)
	}
	for _, uid := range []string{markerA, markerB} {
		if got := markerSubject(t, store, uid); got != nil {
			t.Errorf("marker %s still assigned to %q", uid, *got)
		}
	}
	if uid, name := faceCache(t, vecStore, photoA); uid != nil || name != "" {
		t.Errorf("face cache after detach = %v/%q, want nil/empty", uid, name)
	}
	// The untouched person keeps everything.
	if got := markerSubject(t, store, markerAlice); got == nil || *got != alice.UID {
		t.Errorf("Alice's marker = %v, want %s", got, alice.UID)
	}
	if uid, name := faceCache(t, vecStore, photoAlice); uid == nil || *uid != alice.UID || name != "Alice" {
		t.Errorf("Alice's face cache = %v/%q, want %s/Alice", uid, name, alice.UID)
	}
	requireOneAudit(t, ctx, audit.NewStore(db.Pool()), audit.ActionSubjectDelete, actor, catchAll.UID)

	restored, err := store.RestoreSubject(ctx, snap,
		actorEntry(actor, audit.ActionSubjectCreate, "subject", catchAll.UID,
			map[string]any{"reason": "undo"}))
	if err != nil {
		t.Fatalf("RestoreSubject: %v", err)
	}
	if restored.UID != catchAll.UID || restored.Notes != "importer artefact" ||
		!restored.CreatedAt.Equal(catchAll.CreatedAt) {
		t.Errorf("restored subject = %+v, want the snapshot's row", restored)
	}
	for _, uid := range []string{markerA, markerB} {
		if got := markerSubject(t, store, uid); got == nil || *got != catchAll.UID {
			t.Errorf("marker %s after undo = %v, want %s", uid, got, catchAll.UID)
		}
	}
	if uid, _ := faceCache(t, vecStore, photoA); uid == nil || *uid != catchAll.UID {
		t.Errorf("face cache after undo = %v, want %s", uid, catchAll.UID)
	}
}

// TestDetachSubject_missingSubject reports the sentinel and writes no audit row,
// so a repair run against an already-repaired library is a no-op rather than a
// half-recorded change.
func TestDetachSubject_missingSubject(t *testing.T) {
	store, _, _, db := newStores(t)
	actor := makeUser(t, db, "usr_detach0000000000000000001", "detacher2")

	_, err := store.DetachSubject(t.Context(), "su_does_not_exist",
		actorEntry(actor, audit.ActionSubjectDelete, "subject", "su_does_not_exist", nil))
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("DetachSubject missing = %v, want ErrSubjectNotFound", err)
	}
	var rows int
	if err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM audit_log").Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("audit rows = %d, want 0 (the failed detach rolled back)", rows)
	}
}

// TestRestoreSubject_slugTakenSinceTheSnapshot checks the undo still works when
// another subject has taken the snapshot's base slug in the meantime: the row
// comes back under a disambiguated slug rather than failing on the unique index.
func TestRestoreSubject_slugTakenSinceTheSnapshot(t *testing.T) {
	store, photoStore, vecStore, db := newStores(t)
	ctx := t.Context()
	actor := makeUser(t, db, "usr_detach0000000000000000002", "detacher3")

	catchAll, err := store.CreateSubject(ctx, people.Subject{Name: ""})
	if err != nil {
		t.Fatalf("CreateSubject nameless: %v", err)
	}
	_, markerUID := seedAssignedFace(t, store, photoStore, vecStore, "reslug_a", catchAll.UID)

	snap, err := store.DetachSubject(ctx, catchAll.UID,
		actorEntry(actor, audit.ActionSubjectDelete, "subject", catchAll.UID, nil))
	if err != nil {
		t.Fatalf("DetachSubject: %v", err)
	}
	squatter, err := store.CreateSubject(ctx, people.Subject{Name: "", Notes: "took the slug"})
	if err != nil {
		t.Fatalf("CreateSubject squatter: %v", err)
	}
	if squatter.Slug != snap.Subject.Slug {
		t.Fatalf("squatter slug = %q, want the freed %q", squatter.Slug, snap.Subject.Slug)
	}

	restored, err := store.RestoreSubject(ctx, snap,
		actorEntry(actor, audit.ActionSubjectCreate, "subject", catchAll.UID, nil))
	if err != nil {
		t.Fatalf("RestoreSubject: %v", err)
	}
	if restored.UID != catchAll.UID {
		t.Errorf("restored uid = %s, want %s", restored.UID, catchAll.UID)
	}
	if restored.Slug == squatter.Slug {
		t.Errorf("restored slug = %q, want one disambiguated from the squatter's", restored.Slug)
	}
	if got := markerSubject(t, store, markerUID); got == nil || *got != catchAll.UID {
		t.Errorf("marker after undo = %v, want %s", got, catchAll.UID)
	}
}

// TestSnapshotSubject_readsWhatADetachWouldRemove verifies the read-only half of
// the repair: the snapshot names the subject and every marker and cached face
// pointing at it, and taking it leaves all of them exactly where they were. It is
// what the admin repair hands to the browser *before* it schedules the detach, so
// it has to be both complete and harmless.
func TestSnapshotSubject_readsWhatADetachWouldRemove(t *testing.T) {
	store, photoStore, vecStore, _ := newStores(t)
	ctx := t.Context()

	catchAll, err := store.CreateSubject(ctx, people.Subject{Name: "", Notes: "importer artefact"})
	if err != nil {
		t.Fatalf("CreateSubject nameless: %v", err)
	}
	_, markerA := seedAssignedFace(t, store, photoStore, vecStore, "snapshot_a", catchAll.UID)
	_, markerB := seedAssignedFace(t, store, photoStore, vecStore, "snapshot_b", catchAll.UID)

	snap, err := store.SnapshotSubject(ctx, catchAll.UID)
	if err != nil {
		t.Fatalf("SnapshotSubject: %v", err)
	}
	if snap.Subject.UID != catchAll.UID || snap.Subject.Slug != catchAll.Slug {
		t.Errorf("snapshot subject = %s/%q, want %s/%q",
			snap.Subject.UID, snap.Subject.Slug, catchAll.UID, catchAll.Slug)
	}
	if len(snap.MarkerUIDs) != 2 || len(snap.Faces) != 2 {
		t.Fatalf("snapshot = %d markers / %d faces, want 2/2", len(snap.MarkerUIDs), len(snap.Faces))
	}
	for _, uid := range []string{markerA, markerB} {
		if !slices.Contains(snap.MarkerUIDs, uid) {
			t.Errorf("snapshot markers %v miss %s", snap.MarkerUIDs, uid)
		}
		if got := markerSubject(t, store, uid); got == nil || *got != catchAll.UID {
			t.Errorf("marker %s after the snapshot = %v, want still %s", uid, got, catchAll.UID)
		}
	}
	if _, err := store.GetSubjectByUID(ctx, catchAll.UID); err != nil {
		t.Errorf("the snapshot removed the subject: %v", err)
	}
}

// TestSnapshotSubject_missingSubject verifies a snapshot of a subject that is not
// there reports ErrSubjectNotFound, so a caller that lost a race skips it instead
// of handing out an empty undo.
func TestSnapshotSubject_missingSubject(t *testing.T) {
	store, _, _, _ := newStores(t)

	if _, err := store.SnapshotSubject(t.Context(), "sub_does_not_exist"); !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("SnapshotSubject on a missing subject = %v, want ErrSubjectNotFound", err)
	}
}
