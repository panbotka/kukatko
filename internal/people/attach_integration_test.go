//go:build integration

package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover attaching a person to a media item by
// hand: the link, its idempotency, and — just as important — the surfaces the
// link must stay out of, because everything about a person on a photo runs
// through the marker table and a new kind of row there is visible to a dozen
// queries at once.

// attachFixture is the world every attach test starts from: a photo, a named
// subject and an actor to attribute the audit rows to.
type attachFixture struct {
	store      *people.Store
	photoStore *photos.Store
	db         *database.DB
	photoUID   string
	subjectUID string
	actorUID   string
}

// newAttachFixture builds the fixture over a freshly truncated database.
func newAttachFixture(t *testing.T) attachFixture {
	t.Helper()
	store, photoStore, _, db := newStores(t)
	subject, err := store.CreateSubject(t.Context(), people.Subject{Name: "Marie Dvořáková"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	return attachFixture{
		store:      store,
		photoStore: photoStore,
		db:         db,
		photoUID:   makePhoto(t, photoStore, "hash-attach"),
		subjectUID: subject.UID,
		actorUID:   makeUser(t, db, "usr_attach", "curator"),
	}
}

// attach links f.subjectUID to f.photoUID through the audited store path.
func (f attachFixture) attach(t *testing.T) []people.PhotoSubject {
	t.Helper()
	return f.attachSubject(t, f.subjectUID)
}

// attachSubject links subjectUID to f.photoUID through the audited store path.
func (f attachFixture) attachSubject(t *testing.T, subjectUID string) []people.PhotoSubject {
	t.Helper()
	entry := actorEntry(f.actorUID, audit.ActionPersonAttach, "markers", "",
		map[string]any{"photo_uid": f.photoUID, "subject_uid": subjectUID})
	attached, err := f.store.AttachSubjectToPhoto(t.Context(), f.photoUID, subjectUID, entry)
	if err != nil {
		t.Fatalf("AttachSubjectToPhoto: %v", err)
	}
	return attached
}

// countPersonMarkers returns how many hand-attached links the database holds.
func countPersonMarkers(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM markers WHERE type = 'person'").Scan(&n); err != nil {
		t.Fatalf("counting person markers: %v", err)
	}
	return n
}

// TestAttachSubjectToPhoto_roundTrip attaches a person, reads the link back and
// removes it again.
func TestAttachSubjectToPhoto_roundTrip(t *testing.T) {
	f := newAttachFixture(t)
	ctx := t.Context()

	attached := f.attach(t)
	if len(attached) != 1 {
		t.Fatalf("attach returned %d people, want 1 (%+v)", len(attached), attached)
	}
	got := attached[0]
	if got.SubjectUID != f.subjectUID || got.Name != "Marie Dvořáková" ||
		got.Type != people.SubjectPerson || got.Slug != "marie-dvorakova" {
		t.Fatalf("attached = %+v, want the named subject with its type and slug", got)
	}
	if got.MarkerUID == "" || got.AttachedAt.IsZero() {
		t.Fatalf("attached = %+v, want a marker uid and a timestamp", got)
	}

	listed, err := f.store.ListPhotoSubjects(ctx, f.photoUID)
	if err != nil {
		t.Fatalf("ListPhotoSubjects: %v", err)
	}
	if len(listed) != 1 || listed[0].SubjectUID != f.subjectUID {
		t.Fatalf("ListPhotoSubjects = %+v, want the attached subject", listed)
	}

	entry := actorEntry(f.actorUID, audit.ActionPersonDetach, "subjects", f.subjectUID,
		map[string]any{"photo_uid": f.photoUID})
	remaining, err := f.store.DetachSubjectFromPhoto(ctx, f.photoUID, f.subjectUID, entry)
	if err != nil {
		t.Fatalf("DetachSubjectFromPhoto: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("after detach = %+v, want nobody attached", remaining)
	}
	if n := countPersonMarkers(t, f.db); n != 0 {
		t.Fatalf("person markers after detach = %d, want 0", n)
	}
}

// TestAttachSubjectToPhoto_idempotent verifies attaching twice leaves one link
// and answers the same body, and that detaching twice is equally uneventful.
func TestAttachSubjectToPhoto_idempotent(t *testing.T) {
	f := newAttachFixture(t)

	first := f.attach(t)
	second := f.attach(t)
	if len(second) != 1 {
		t.Fatalf("second attach returned %d people, want 1", len(second))
	}
	if second[0].MarkerUID != first[0].MarkerUID {
		t.Fatalf("second attach made a new marker %s (first %s), want one link",
			second[0].MarkerUID, first[0].MarkerUID)
	}
	if n := countPersonMarkers(t, f.db); n != 1 {
		t.Fatalf("person markers = %d, want exactly 1", n)
	}

	entry := actorEntry(f.actorUID, audit.ActionPersonDetach, "subjects", f.subjectUID, nil)
	if _, err := f.store.DetachSubjectFromPhoto(t.Context(), f.photoUID, f.subjectUID, entry); err != nil {
		t.Fatalf("first detach: %v", err)
	}
	if _, err := f.store.DetachSubjectFromPhoto(t.Context(), f.photoUID, f.subjectUID, entry); err != nil {
		t.Fatalf("second detach = %v, want success (the state already holds)", err)
	}
}

// TestAttachSubjectToPhoto_missing verifies an unknown photo and an unknown
// subject are each reported as such, by both mutations, and that neither writes
// an audit row.
func TestAttachSubjectToPhoto_missing(t *testing.T) {
	f := newAttachFixture(t)
	ctx := t.Context()
	auditStore := audit.NewStore(f.db.Pool())

	cases := []struct {
		name       string
		photoUID   string
		subjectUID string
		want       error
	}{
		{name: "unknown photo", photoUID: "ph_nope", subjectUID: f.subjectUID, want: people.ErrPhotoNotFound},
		{name: "unknown subject", photoUID: f.photoUID, subjectUID: "su_nope", want: people.ErrSubjectNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := actorEntry(f.actorUID, audit.ActionPersonAttach, "markers", "", nil)
			if _, err := f.store.AttachSubjectToPhoto(ctx, tc.photoUID, tc.subjectUID, entry); !errors.Is(err, tc.want) {
				t.Errorf("attach = %v, want %v", err, tc.want)
			}
			entry = actorEntry(f.actorUID, audit.ActionPersonDetach, "subjects", tc.subjectUID, nil)
			if _, err := f.store.DetachSubjectFromPhoto(ctx, tc.photoUID, tc.subjectUID, entry); !errors.Is(err, tc.want) {
				t.Errorf("detach = %v, want %v", err, tc.want)
			}
		})
	}
	if recs := auditRecords(t, ctx, auditStore, audit.ActionPersonAttach); len(recs) != 0 {
		t.Errorf("failed attach wrote %d audit rows, want 0", len(recs))
	}
	if recs := auditRecords(t, ctx, auditStore, audit.ActionPersonDetach); len(recs) != 0 {
		t.Errorf("failed detach wrote %d audit rows, want 0", len(recs))
	}
}

// TestAttachSubjectToPhoto_audited verifies both mutations land an audit row in
// their own transaction — the durable-audit guarantee every other curation write
// gives.
func TestAttachSubjectToPhoto_audited(t *testing.T) {
	f := newAttachFixture(t)
	ctx := t.Context()
	auditStore := audit.NewStore(f.db.Pool())

	attached := f.attach(t)
	rec := requireOneAudit(t, ctx, auditStore, audit.ActionPersonAttach, f.actorUID, attached[0].MarkerUID)
	if rec.Details["subject_uid"] != f.subjectUID {
		t.Errorf("attach audit details = %v, want the subject", rec.Details)
	}

	entry := actorEntry(f.actorUID, audit.ActionPersonDetach, "subjects", f.subjectUID,
		map[string]any{"photo_uid": f.photoUID})
	if _, err := f.store.DetachSubjectFromPhoto(ctx, f.photoUID, f.subjectUID, entry); err != nil {
		t.Fatalf("DetachSubjectFromPhoto: %v", err)
	}
	rec = requireOneAudit(t, ctx, auditStore, audit.ActionPersonDetach, f.actorUID, f.subjectUID)
	if rec.Details["photo_uid"] != f.photoUID {
		t.Errorf("detach audit details = %v, want the photo", rec.Details)
	}
}

// TestAttachSubjectToPhoto_countsAndGallery verifies the attached photo behaves
// like any other photo of that person: it is in the subject's gallery, it counts
// toward the photo count, and it is NOT counted as one of their faces.
func TestAttachSubjectToPhoto_countsAndGallery(t *testing.T) {
	f := newAttachFixture(t)
	ctx := t.Context()
	f.attach(t)

	uids, err := f.store.ListPhotoUIDsBySubject(ctx, f.subjectUID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsBySubject: %v", err)
	}
	if len(uids) != 1 || uids[0] != f.photoUID {
		t.Fatalf("subject gallery = %v, want the attached photo", uids)
	}

	subjects, err := f.store.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(subjects) != 1 {
		t.Fatalf("ListSubjects = %+v, want one subject", subjects)
	}
	if subjects[0].PhotoCount != 1 {
		t.Errorf("photo_count = %d, want 1: a hand-attached photo is a photo of that person",
			subjects[0].PhotoCount)
	}
	if subjects[0].MarkerCount != 0 {
		t.Errorf("marker_count = %d, want 0: a hand-attached person has no face to work on",
			subjects[0].MarkerCount)
	}
	if subjects[0].CoverFace != nil {
		t.Errorf("cover_face = %+v, want none: there is no box to crop a tile from", subjects[0].CoverFace)
	}

	stats, err := f.store.SubjectStats(ctx, f.subjectUID)
	if err != nil {
		t.Fatalf("SubjectStats: %v", err)
	}
	if stats.PhotoCount != 1 {
		t.Errorf("stats photo_count = %d, want 1", stats.PhotoCount)
	}
}

// TestAttachSubjectToPhoto_noAvatar verifies a hand-attached person never wins
// the avatar election: there is no crop to cut one from.
func TestAttachSubjectToPhoto_noAvatar(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	if _, err := f.store.SubjectAvatar(t.Context(), f.subjectUID); !errors.Is(err, people.ErrNoAvatar) {
		t.Fatalf("SubjectAvatar = %v, want ErrNoAvatar", err)
	}
}

// TestAttachSubjectToPhoto_cover verifies the opposite decision for the cover:
// SubjectCovers elects a whole photo, not a crop, so the attached photo is a
// perfectly good picture of that person and is allowed to stand for them.
func TestAttachSubjectToPhoto_cover(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	covers, err := f.store.SubjectCovers(t.Context(), []string{f.subjectUID})
	if err != nil {
		t.Fatalf("SubjectCovers: %v", err)
	}
	if covers[f.subjectUID].PhotoUID != f.photoUID {
		t.Fatalf("cover = %+v, want the attached photo", covers[f.subjectUID])
	}
}

// TestAttachSubjectToPhoto_sidecar verifies the link reaches the metadata
// sidecar, so it survives losing the database.
func TestAttachSubjectToPhoto_sidecar(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	markers, err := f.store.ListMarkersWithSubjects(t.Context(), f.photoUID)
	if err != nil {
		t.Fatalf("ListMarkersWithSubjects: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("sidecar markers = %+v, want the link", markers)
	}
	if markers[0].Type != people.MarkerPerson || markers[0].SubjectName != "Marie Dvořáková" {
		t.Fatalf("sidecar marker = %+v, want a person link naming the subject", markers[0])
	}
}

// TestAttachSubjectToPhoto_deletedSubject verifies deleting the person removes
// the link rather than leaving a nameless marker behind: a region survives losing
// its name, a bare "this person is here" does not.
func TestAttachSubjectToPhoto_deletedSubject(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	if err := f.store.DeleteSubject(t.Context(), f.subjectUID); err != nil {
		t.Fatalf("DeleteSubject: %v", err)
	}
	if n := countPersonMarkers(t, f.db); n != 0 {
		t.Fatalf("person markers after deleting the subject = %d, want 0", n)
	}
}

// TestAttachSubjectToPhoto_deletedSubjectAudited verifies the audited delete path
// clears the links too — the two delete paths must not disagree.
func TestAttachSubjectToPhoto_deletedSubjectAudited(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	entry := actorEntry(f.actorUID, audit.ActionSubjectDelete, "subjects", f.subjectUID, nil)
	if err := f.store.DeleteSubjectAudited(t.Context(), f.subjectUID, entry); err != nil {
		t.Fatalf("DeleteSubjectAudited: %v", err)
	}
	if n := countPersonMarkers(t, f.db); n != 0 {
		t.Fatalf("person markers after deleting the subject = %d, want 0", n)
	}
}

// TestAttachSubjectToPhoto_namelessRepair verifies the nameless-subject repair
// takes the links with the subject too, rather than detaching them into rows
// nobody can ever name. The undo cannot bring them back, which is why the
// snapshot's marker list is not the whole story — and why the repair only ever
// targets subjects that identify nobody.
func TestAttachSubjectToPhoto_namelessRepair(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	entry := actorEntry(f.actorUID, audit.ActionSubjectDelete, "subjects", f.subjectUID, nil)
	snap, err := f.store.DetachSubject(t.Context(), f.subjectUID, entry)
	if err != nil {
		t.Fatalf("DetachSubject: %v", err)
	}
	if len(snap.MarkerUIDs) != 1 {
		t.Fatalf("snapshot markers = %v, want the one link it removed", snap.MarkerUIDs)
	}
	if n := countPersonMarkers(t, f.db); n != 0 {
		t.Fatalf("person markers after the repair = %d, want 0", n)
	}
}

// TestAttachSubjectToPhoto_deletedPhoto verifies purging the media item takes its
// links with it, through the marker table's own ON DELETE CASCADE.
func TestAttachSubjectToPhoto_deletedPhoto(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	if _, err := f.db.Pool().Exec(t.Context(), "DELETE FROM photos WHERE uid = $1", f.photoUID); err != nil {
		t.Fatalf("deleting photo: %v", err)
	}
	if n := countPersonMarkers(t, f.db); n != 0 {
		t.Fatalf("person markers after deleting the photo = %d, want 0", n)
	}
}

// TestAttachSubjectToPhoto_merge verifies a merge carries the links over the way
// it carries markers over, and that two people attached to one photo collapse
// into one link rather than tripping the unique index.
func TestAttachSubjectToPhoto_merge(t *testing.T) {
	f := newAttachFixture(t)
	ctx := t.Context()

	other, err := f.store.CreateSubject(ctx, people.Subject{Name: "Marie D."})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	second := makePhoto(t, f.photoStore, "hash-attach-2")

	f.attach(t)                   // subject on photo 1
	f.attachSubject(t, other.UID) // the duplicate person on photo 1 too
	onlyOther := attachFixture{   // the duplicate person on photo 2 alone
		store: f.store, photoStore: f.photoStore, db: f.db,
		photoUID: second, subjectUID: other.UID, actorUID: f.actorUID,
	}
	onlyOther.attach(t)

	entry := actorEntry(f.actorUID, audit.ActionSubjectMerge, "subjects", f.subjectUID, nil)
	if _, err := f.store.MergeSubjectsAudited(ctx, other.UID, f.subjectUID, entry); err != nil {
		t.Fatalf("MergeSubjectsAudited: %v", err)
	}

	uids, err := f.store.ListPhotoUIDsBySubject(ctx, f.subjectUID)
	if err != nil {
		t.Fatalf("ListPhotoUIDsBySubject: %v", err)
	}
	if len(uids) != 2 {
		t.Fatalf("gallery after merge = %v, want both photos", uids)
	}
	if n := countPersonMarkers(t, f.db); n != 2 {
		t.Fatalf("person markers after merge = %d, want 2 (one per photo)", n)
	}
}

// TestAttachSubjectToPhoto_noFaceRow verifies attaching writes nothing into the
// faces table, which is what keeps the link out of clustering, the candidate
// search, the recognition sweep, the review game and every other
// nearest-neighbour path: all of them read faces, not markers.
func TestAttachSubjectToPhoto_noFaceRow(t *testing.T) {
	f := newAttachFixture(t)
	f.attach(t)

	var n int
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM faces").Scan(&n); err != nil {
		t.Fatalf("counting faces: %v", err)
	}
	if n != 0 {
		t.Fatalf("faces rows = %d, want 0: a hand-attached person has no embedding", n)
	}
}

// TestAttachSubjectToPhoto_noBox verifies the database refuses a 'person' marker
// that carries geometry, so nothing can ever hand a crop a box that means
// nothing.
func TestAttachSubjectToPhoto_noBox(t *testing.T) {
	f := newAttachFixture(t)

	_, err := f.store.CreateMarker(t.Context(), people.Marker{
		PhotoUID: f.photoUID, SubjectUID: &f.subjectUID,
		Type: people.MarkerPerson, X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	})
	if !errors.Is(err, people.ErrInvalidBounds) {
		t.Fatalf("CreateMarker with a box = %v, want ErrInvalidBounds", err)
	}
}
