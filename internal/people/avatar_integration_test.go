//go:build integration

package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/people"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// markFace puts a valid face marker of the given size for subjectUID on
// photoUID, so a test can control which face the avatar rule picks.
func markFace(
	t *testing.T, store *people.Store, photoUID, subjectUID string, x, y, w, h float64,
) {
	t.Helper()
	if _, err := store.CreateMarker(context.Background(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &subjectUID,
		X: x, Y: y, W: w, H: h, Score: 90,
	}); err != nil {
		t.Fatalf("CreateMarker on %s: %v", photoUID, err)
	}
}

// TestSubjectAvatar_picksTheBiggestFace verifies the avatar source is the
// subject's largest valid face, and that it carries that face's box — the same
// choice the people index's list payload makes, since the two must agree.
func TestSubjectAvatar_picksTheBiggestFace(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	small := makePhoto(t, photoStore, "av-small")
	big := makePhoto(t, photoStore, "av-big")
	subject, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	markFace(t, store, small, subject.UID, 0.1, 0.1, 0.05, 0.05)
	markFace(t, store, big, subject.UID, 0.4, 0.3, 0.2, 0.25)

	source, err := store.SubjectAvatar(ctx, subject.UID)
	if err != nil {
		t.Fatalf("SubjectAvatar: %v", err)
	}
	if source.PhotoUID != big {
		t.Errorf("avatar photo = %q, want the photo with the bigger face %q", source.PhotoUID, big)
	}
	if source.Face == nil {
		t.Fatal("avatar carries no face box, want the biggest face's")
	}
	if source.Face.W != 0.2 || source.Face.H != 0.25 {
		t.Errorf("face box = %+v, want the 0.2×0.25 one", *source.Face)
	}
}

// TestSubjectAvatar_handPickedCoverWins verifies a chosen cover photo beats the
// detected face and is reported with no crop, i.e. shown whole.
func TestSubjectAvatar_handPickedCoverWins(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	cover := makePhoto(t, photoStore, "av-cover")
	other := makePhoto(t, photoStore, "av-other")
	subject, err := store.CreateSubject(ctx, people.Subject{Name: "Alice", CoverPhotoUID: &cover})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	markFace(t, store, other, subject.UID, 0.4, 0.3, 0.2, 0.2)

	source, err := store.SubjectAvatar(ctx, subject.UID)
	if err != nil {
		t.Fatalf("SubjectAvatar: %v", err)
	}
	if source.PhotoUID != cover {
		t.Errorf("avatar photo = %q, want the hand-picked cover %q", source.PhotoUID, cover)
	}
	if source.Face != nil {
		t.Errorf("a hand-picked cover came back cropped to %+v, want it whole", *source.Face)
	}
}

// TestSubjectAvatar_skipsRejectedAndHidden verifies the avatar never comes from a
// marker the user rejected nor from a photo the subject's own gallery hides, and
// that a subject left with nothing answers ErrNoAvatar rather than a broken source.
func TestSubjectAvatar_skipsRejectedAndHidden(t *testing.T) {
	store, photoStore, _, db := newStores(t)
	ctx := context.Background()

	rejected := makePhoto(t, photoStore, "av-bad")
	archived := makePhoto(t, photoStore, "av-hid")
	subject, err := store.CreateSubject(ctx, people.Subject{Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	marker, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: rejected, SubjectUID: &subject.UID,
		X: 0.1, Y: 0.1, W: 0.3, H: 0.3, Score: 90,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE markers SET invalid = TRUE WHERE uid = $1`, marker.UID); err != nil {
		t.Fatalf("rejecting the marker: %v", err)
	}
	markFace(t, store, archived, subject.UID, 0.2, 0.2, 0.4, 0.4)
	if _, err := db.Pool().Exec(ctx,
		`UPDATE photos SET archived_at = NOW() WHERE uid = $1`, archived); err != nil {
		t.Fatalf("archiving the photo: %v", err)
	}

	if _, err := store.SubjectAvatar(ctx, subject.UID); !errors.Is(err, people.ErrNoAvatar) {
		t.Errorf("SubjectAvatar error = %v, want ErrNoAvatar", err)
	}
}

// TestSubjectAvatar_unknownSubject verifies a uid naming nothing is reported as a
// missing subject, which the HTTP layer turns into a 404 rather than a 500.
func TestSubjectAvatar_unknownSubject(t *testing.T) {
	store, _, _, _ := newStores(t)

	_, err := store.SubjectAvatar(context.Background(), "su_nothing")
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Errorf("SubjectAvatar error = %v, want ErrSubjectNotFound", err)
	}
}
