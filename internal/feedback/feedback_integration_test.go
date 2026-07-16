//go:build integration

package feedback_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel. Reaching the store at all
// proves migration 0031 applied (dbtest.New runs every migration).

// fixtures bundles the stores an integration case needs plus the database handle,
// used to create the parent rows the rejection foreign keys require and to inspect
// the tables directly.
type fixtures struct {
	feedback *feedback.Store
	photos   *photos.Store
	people   *people.Store
	labels   *organize.Store
	users    *auth.Store
	db       *database.DB
}

// newFixtures returns the fixture stores over a freshly truncated integration
// database.
func newFixtures(t *testing.T) *fixtures {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return &fixtures{
		feedback: feedback.NewStore(db.Pool()),
		photos:   photos.NewStore(db.Pool()),
		people:   people.NewStore(db.Pool()),
		labels:   organize.NewStore(db.Pool()),
		users:    auth.NewStore(db.Pool()),
		db:       db,
	}
}

// makePhoto inserts a minimal photo and returns its uid.
func (f *fixtures) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := f.photos.Create(context.Background(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// makeSubject inserts a person subject and returns its uid.
func (f *fixtures) makeSubject(t *testing.T, name string) string {
	t.Helper()
	subj, err := f.people.CreateSubject(context.Background(),
		people.Subject{Name: name, Type: people.SubjectPerson})
	if err != nil {
		t.Fatalf("creating subject %s: %v", name, err)
	}
	return subj.UID
}

// makeLabel inserts a label and returns its uid.
func (f *fixtures) makeLabel(t *testing.T, name string) string {
	t.Helper()
	label, err := f.labels.CreateLabel(context.Background(), organize.Label{Name: name})
	if err != nil {
		t.Fatalf("creating label %s: %v", name, err)
	}
	return label.UID
}

// makeUser inserts a viewer account and returns its uid.
func (f *fixtures) makeUser(t *testing.T, uid, username string) string {
	t.Helper()
	err := f.users.CreateUser(context.Background(), auth.User{
		UID: uid, Username: username, PasswordHash: "x", Role: auth.RoleViewer,
	})
	if err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// sortRefs orders face references by (photo_uid, face_index), matching the store's
// deterministic ordering so a test expectation can be compared element-wise.
func sortRefs(refs []feedback.FaceRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].PhotoUID != refs[j].PhotoUID {
			return refs[i].PhotoUID < refs[j].PhotoUID
		}
		return refs[i].FaceIndex < refs[j].FaceIndex
	})
}

// count runs a single-column count query and returns the result.
func (f *fixtures) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("counting (%s): %v", query, err)
	}
	return n
}

// TestFaceRejectionLifecycle checks idempotent rejecting, the rejected check and
// taking a rejection back (itself idempotent).
func TestFaceRejectionLifecycle(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "face_life")
	subject := f.makeSubject(t, "Tomáš")
	key := feedback.FaceRejectionKey{PhotoUID: photo, FaceIndex: 0, SubjectUID: subject}
	entry := audit.Entry{Action: audit.ActionFaceReject, TargetType: "subjects", TargetUID: subject}

	if err := f.feedback.RejectFace(ctx, key, entry); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}
	// Rejecting again is a no-op, not an error, and leaves exactly one row.
	if err := f.feedback.RejectFace(ctx, key, entry); err != nil {
		t.Fatalf("RejectFace (repeat): %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM face_rejections"); n != 1 {
		t.Fatalf("row count after double reject = %d, want 1", n)
	}
	if ok, err := f.feedback.IsFaceRejected(ctx, key); err != nil || !ok {
		t.Fatalf("IsFaceRejected = %v, %v, want true, nil", ok, err)
	}

	if err := f.feedback.UnrejectFace(ctx, key, entry); err != nil {
		t.Fatalf("UnrejectFace: %v", err)
	}
	if ok, _ := f.feedback.IsFaceRejected(ctx, key); ok {
		t.Fatalf("IsFaceRejected after unreject = true, want false")
	}
	// Un-rejecting what is not rejected is a no-op, not an error.
	if err := f.feedback.UnrejectFace(ctx, key, entry); err != nil {
		t.Fatalf("UnrejectFace (repeat): %v", err)
	}
}

// TestLabelRejectionLifecycle mirrors the face case for photo↔label rejections.
func TestLabelRejectionLifecycle(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "label_life")
	label := f.makeLabel(t, "Beach")
	key := feedback.LabelRejectionKey{PhotoUID: photo, LabelUID: label}
	entry := audit.Entry{Action: audit.ActionLabelReject, TargetType: "labels", TargetUID: label}

	if err := f.feedback.RejectLabel(ctx, key, entry); err != nil {
		t.Fatalf("RejectLabel: %v", err)
	}
	if err := f.feedback.RejectLabel(ctx, key, entry); err != nil {
		t.Fatalf("RejectLabel (repeat): %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM label_rejections"); n != 1 {
		t.Fatalf("row count after double reject = %d, want 1", n)
	}
	if ok, err := f.feedback.IsLabelRejected(ctx, key); err != nil || !ok {
		t.Fatalf("IsLabelRejected = %v, %v, want true, nil", ok, err)
	}
	if err := f.feedback.UnrejectLabel(ctx, key, entry); err != nil {
		t.Fatalf("UnrejectLabel: %v", err)
	}
	if ok, _ := f.feedback.IsLabelRejected(ctx, key); ok {
		t.Fatalf("IsLabelRejected after unreject = true, want false")
	}
}

// TestBulkLookups checks the exclusion-filter bulk reads: every face rejected for a
// subject, every photo rejected for a label, scoped and deterministically ordered,
// with an empty non-nil result when nothing is rejected.
func TestBulkLookups(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	p1, p2 := f.makePhoto(t, "bulk1"), f.makePhoto(t, "bulk2")
	subject := f.makeSubject(t, "Anna")
	other := f.makeSubject(t, "Eva")
	entry := audit.Entry{Action: audit.ActionFaceReject}

	reject := func(photo string, idx int, subj string) {
		key := feedback.FaceRejectionKey{PhotoUID: photo, FaceIndex: idx, SubjectUID: subj}
		if err := f.feedback.RejectFace(ctx, key, entry); err != nil {
			t.Fatalf("RejectFace: %v", err)
		}
	}
	reject(p2, 1, subject)
	reject(p1, 0, subject)
	reject(p1, 0, other) // a different subject must not leak into the lookup

	refs, err := f.feedback.FaceRejectionsForSubject(ctx, subject)
	if err != nil {
		t.Fatalf("FaceRejectionsForSubject: %v", err)
	}
	// The store orders by (photo_uid, face_index); the two photos' UIDs are random,
	// so sort the expectation the same way to assert content and the deterministic
	// order without hard-coding which UID sorts first.
	want := []feedback.FaceRef{{PhotoUID: p1, FaceIndex: 0}, {PhotoUID: p2, FaceIndex: 1}}
	sortRefs(want)
	if len(refs) != len(want) || refs[0] != want[0] || refs[1] != want[1] {
		t.Fatalf("face refs = %+v, want %+v (ordered by photo, index)", refs, want)
	}

	empty, err := f.feedback.FaceRejectionsForSubject(ctx, f.makeSubject(t, "Nobody"))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty lookup = %+v, %v, want non-nil empty slice", empty, err)
	}

	label := f.makeLabel(t, "Sky")
	lkey := feedback.LabelRejectionKey{PhotoUID: p1, LabelUID: label}
	if err := f.feedback.RejectLabel(ctx, lkey, entry); err != nil {
		t.Fatalf("RejectLabel: %v", err)
	}
	photoUIDs, err := f.feedback.LabelRejectionsForLabel(ctx, label)
	if err != nil || len(photoUIDs) != 1 || photoUIDs[0] != p1 {
		t.Fatalf("label rejection photos = %+v, %v, want [%s]", photoUIDs, err, p1)
	}
}

// TestRejectedByAndAudit checks the actor is stored on the row and an audit entry is
// written in the same transaction as the rejection.
func TestRejectedByAndAudit(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "who")
	subject := f.makeSubject(t, "Karel")
	actor := f.makeUser(t, "usr_reviewer", "reviewer")
	key := feedback.FaceRejectionKey{PhotoUID: photo, FaceIndex: 2, SubjectUID: subject}
	entry := audit.Entry{
		Action: audit.ActionFaceReject, TargetType: "subjects", TargetUID: subject, ActorUID: actor,
	}

	if err := f.feedback.RejectFace(ctx, key, entry); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}

	var rejectedBy *string
	err := f.db.Pool().QueryRow(ctx,
		"SELECT rejected_by FROM face_rejections WHERE photo_uid = $1 AND face_index = $2 AND subject_uid = $3",
		photo, 2, subject).Scan(&rejectedBy)
	if err != nil {
		t.Fatalf("reading rejected_by: %v", err)
	}
	if rejectedBy == nil || *rejectedBy != actor {
		t.Fatalf("rejected_by = %v, want %s", rejectedBy, actor)
	}

	records, err := audit.NewStore(f.db.Pool()).List(ctx, audit.Filter{Action: audit.ActionFaceReject})
	if err != nil || len(records) != 1 || records[0].TargetUID == nil || *records[0].TargetUID != subject {
		t.Fatalf("audit records = %+v, %v, want one face.reject targeting %s", records, err, subject)
	}
}

// TestForeignKeyCascades checks that deleting a subject, label or photo removes the
// rejections that reference it, so no orphan rows survive.
func TestForeignKeyCascades(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "cascade")
	subject := f.makeSubject(t, "Petra")
	label := f.makeLabel(t, "Dog")
	entry := audit.Entry{Action: audit.ActionFaceReject}

	faceKey := feedback.FaceRejectionKey{PhotoUID: photo, FaceIndex: 0, SubjectUID: subject}
	labelKey := feedback.LabelRejectionKey{PhotoUID: photo, LabelUID: label}
	if err := f.feedback.RejectFace(ctx, faceKey, entry); err != nil {
		t.Fatalf("RejectFace: %v", err)
	}
	if err := f.feedback.RejectLabel(ctx, labelKey, entry); err != nil {
		t.Fatalf("RejectLabel: %v", err)
	}

	// Deleting the subject cascades to its face rejection; the photo and its label
	// rejection survive.
	if _, err := f.db.Pool().Exec(ctx, "DELETE FROM subjects WHERE uid = $1", subject); err != nil {
		t.Fatalf("deleting subject: %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM face_rejections"); n != 0 {
		t.Fatalf("face rejections after subject delete = %d, want 0", n)
	}
	if n := f.count(t, "SELECT count(*) FROM label_rejections"); n != 1 {
		t.Fatalf("label rejections after subject delete = %d, want 1", n)
	}

	// Deleting the photo cascades to the remaining label rejection.
	if _, err := f.db.Pool().Exec(ctx, "DELETE FROM photos WHERE uid = $1", photo); err != nil {
		t.Fatalf("deleting photo: %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM label_rejections"); n != 0 {
		t.Fatalf("label rejections after photo delete = %d, want 0", n)
	}
}

// TestRejectMissingTarget checks that rejecting against a non-existent photo,
// subject or label surfaces ErrTargetNotFound (a foreign-key violation) rather than
// a raw database error, and writes no row.
func TestRejectMissingTarget(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "missing")
	subject := f.makeSubject(t, "Real")
	entry := audit.Entry{Action: audit.ActionFaceReject}

	badSubject := feedback.FaceRejectionKey{PhotoUID: photo, FaceIndex: 0, SubjectUID: "no_such_subject"}
	if err := f.feedback.RejectFace(ctx, badSubject, entry); !errors.Is(err, feedback.ErrTargetNotFound) {
		t.Fatalf("RejectFace missing subject = %v, want ErrTargetNotFound", err)
	}
	badPhoto := feedback.FaceRejectionKey{PhotoUID: "no_such_photo", FaceIndex: 0, SubjectUID: subject}
	if err := f.feedback.RejectFace(ctx, badPhoto, entry); !errors.Is(err, feedback.ErrTargetNotFound) {
		t.Fatalf("RejectFace missing photo = %v, want ErrTargetNotFound", err)
	}
	badLabel := feedback.LabelRejectionKey{PhotoUID: photo, LabelUID: "no_such_label"}
	if err := f.feedback.RejectLabel(ctx, badLabel, entry); !errors.Is(err, feedback.ErrTargetNotFound) {
		t.Fatalf("RejectLabel missing label = %v, want ErrTargetNotFound", err)
	}
	if n := f.count(t, "SELECT count(*) FROM face_rejections"); n != 0 {
		t.Fatalf("rows after failed rejects = %d, want 0", n)
	}
}

// TestFaceConfirmationLifecycle checks idempotent confirming, the confirmed check,
// taking a confirmation back (itself idempotent) and the audit row written in the
// same transaction.
func TestFaceConfirmationLifecycle(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "confirm_life")
	subject := f.makeSubject(t, "Tomáš")
	user := f.makeUser(t, "us_confirm", "confirmer")
	key := feedback.FaceConfirmationKey{PhotoUID: photo, FaceIndex: 0, SubjectUID: subject}
	entry := audit.Entry{
		ActorUID: user, Action: audit.ActionFaceConfirm, TargetType: "subjects", TargetUID: subject,
	}

	if err := f.feedback.ConfirmFace(ctx, key, entry); err != nil {
		t.Fatalf("ConfirmFace: %v", err)
	}
	// Confirming again is a no-op, not an error, and leaves exactly one row.
	if err := f.feedback.ConfirmFace(ctx, key, entry); err != nil {
		t.Fatalf("ConfirmFace (repeat): %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM face_confirmations"); n != 1 {
		t.Fatalf("row count after double confirm = %d, want 1", n)
	}
	if ok, err := f.feedback.IsFaceConfirmed(ctx, key); err != nil || !ok {
		t.Fatalf("IsFaceConfirmed = %v, %v, want true, nil", ok, err)
	}
	if n := f.count(t,
		"SELECT count(*) FROM face_confirmations WHERE confirmed_by = $1", user); n != 1 {
		t.Fatalf("confirmed_by rows = %d, want 1", n)
	}
	if n := f.count(t,
		"SELECT count(*) FROM audit_log WHERE action = $1", audit.ActionFaceConfirm); n != 2 {
		t.Fatalf("audit rows = %d, want 2 (one per ConfirmFace call)", n)
	}

	if err := f.feedback.UnconfirmFace(ctx, key, entry); err != nil {
		t.Fatalf("UnconfirmFace: %v", err)
	}
	if ok, _ := f.feedback.IsFaceConfirmed(ctx, key); ok {
		t.Fatalf("IsFaceConfirmed after unconfirm = true, want false")
	}
	// Un-confirming what is not confirmed is a no-op, not an error.
	if err := f.feedback.UnconfirmFace(ctx, key, entry); err != nil {
		t.Fatalf("UnconfirmFace (repeat): %v", err)
	}
}

// TestFaceConfirmationBulkLookup checks the exclusion-filter bulk read: every face
// confirmed for a subject, scoped to that subject and deterministically ordered,
// with an empty non-nil result when nothing is confirmed.
func TestFaceConfirmationBulkLookup(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	p1, p2 := f.makePhoto(t, "cbulk1"), f.makePhoto(t, "cbulk2")
	subject := f.makeSubject(t, "Anna")
	other := f.makeSubject(t, "Eva")
	entry := audit.Entry{Action: audit.ActionFaceConfirm}

	confirm := func(photo string, idx int, subj string) {
		key := feedback.FaceConfirmationKey{PhotoUID: photo, FaceIndex: idx, SubjectUID: subj}
		if err := f.feedback.ConfirmFace(ctx, key, entry); err != nil {
			t.Fatalf("ConfirmFace: %v", err)
		}
	}
	confirm(p2, 1, subject)
	confirm(p1, 0, subject)
	confirm(p1, 0, other) // a different subject must not leak into the lookup

	refs, err := f.feedback.FaceConfirmationsForSubject(ctx, subject)
	if err != nil {
		t.Fatalf("FaceConfirmationsForSubject: %v", err)
	}
	want := []feedback.FaceRef{{PhotoUID: p1, FaceIndex: 0}, {PhotoUID: p2, FaceIndex: 1}}
	sortRefs(want)
	if len(refs) != len(want) || refs[0] != want[0] || refs[1] != want[1] {
		t.Fatalf("confirmed refs = %+v, want %+v (ordered by photo, index)", refs, want)
	}

	empty, err := f.feedback.FaceConfirmationsForSubject(ctx, f.makeSubject(t, "Nobody"))
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty lookup = %+v, %v, want non-nil empty slice", empty, err)
	}
}

// TestConfirmMissingTarget checks that confirming against a non-existent photo or
// subject surfaces ErrTargetNotFound rather than a raw database error.
func TestConfirmMissingTarget(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "cmissing")
	subject := f.makeSubject(t, "Real")
	entry := audit.Entry{Action: audit.ActionFaceConfirm}

	badSubject := feedback.FaceConfirmationKey{PhotoUID: photo, FaceIndex: 0, SubjectUID: "no_such_subject"}
	if err := f.feedback.ConfirmFace(ctx, badSubject, entry); !errors.Is(err, feedback.ErrTargetNotFound) {
		t.Fatalf("ConfirmFace missing subject = %v, want ErrTargetNotFound", err)
	}
	badPhoto := feedback.FaceConfirmationKey{PhotoUID: "no_such_photo", FaceIndex: 0, SubjectUID: subject}
	if err := f.feedback.ConfirmFace(ctx, badPhoto, entry); !errors.Is(err, feedback.ErrTargetNotFound) {
		t.Fatalf("ConfirmFace missing photo = %v, want ErrTargetNotFound", err)
	}
	if n := f.count(t, "SELECT count(*) FROM face_confirmations"); n != 0 {
		t.Fatalf("rows after failed confirms = %d, want 0", n)
	}
}

// TestDuplicateDismissalLifecycle checks the pair dismissal round-trip: it is
// idempotent, unordered, reversible, and readable back in bulk.
func TestDuplicateDismissalLifecycle(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	a := f.makePhoto(t, "dup_a")
	b := f.makePhoto(t, "dup_b")
	key := feedback.DuplicateDismissalKey{PhotoUID: a, OtherUID: b}
	entry := audit.Entry{Action: audit.ActionDuplicateDismiss, TargetType: "photos", TargetUID: a}

	if err := f.feedback.DismissDuplicate(ctx, key, entry); err != nil {
		t.Fatalf("DismissDuplicate: %v", err)
	}
	// Dismissing again — and in the reverse order — is a no-op, not an error, and
	// leaves exactly one row: the pair is unordered and normalised on the way in.
	if err := f.feedback.DismissDuplicate(ctx, key, entry); err != nil {
		t.Fatalf("DismissDuplicate (repeat): %v", err)
	}
	reversed := feedback.DuplicateDismissalKey{PhotoUID: b, OtherUID: a}
	if err := f.feedback.DismissDuplicate(ctx, reversed, entry); err != nil {
		t.Fatalf("DismissDuplicate (reversed): %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM duplicate_dismissals"); n != 1 {
		t.Fatalf("row count after three dismissals = %d, want 1", n)
	}
	// Either argument order reads the same decision back.
	for _, k := range []feedback.DuplicateDismissalKey{key, reversed} {
		if ok, err := f.feedback.IsDuplicateDismissed(ctx, k); err != nil || !ok {
			t.Fatalf("IsDuplicateDismissed(%v) = %v, %v, want true, nil", k, ok, err)
		}
	}

	pairs, err := f.feedback.DismissedDuplicatePairs(ctx)
	if err != nil {
		t.Fatalf("DismissedDuplicatePairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("bulk lookup returned %d pairs, want 1", len(pairs))
	}
	// The row is stored canonically, smaller uid first, whatever order it came in.
	lo, hi := a, b
	if hi < lo {
		lo, hi = hi, lo
	}
	if pairs[0].PhotoUID != lo || pairs[0].OtherUID != hi {
		t.Fatalf("bulk pair = (%s, %s), want the canonical (%s, %s)",
			pairs[0].PhotoUID, pairs[0].OtherUID, lo, hi)
	}

	if err := f.feedback.UndismissDuplicate(ctx, reversed, entry); err != nil {
		t.Fatalf("UndismissDuplicate: %v", err)
	}
	if ok, _ := f.feedback.IsDuplicateDismissed(ctx, key); ok {
		t.Fatalf("IsDuplicateDismissed after undismiss = true, want false")
	}
	// Un-dismissing what was never dismissed is a no-op, not an error.
	if err := f.feedback.UndismissDuplicate(ctx, key, entry); err != nil {
		t.Fatalf("UndismissDuplicate (repeat): %v", err)
	}
}

// TestDuplicateDismissalRejectsBadKeys checks the two impossible keys are refused
// with the sentinels the HTTP layer maps to 400/404, rather than writing a row.
func TestDuplicateDismissalRejectsBadKeys(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	photo := f.makePhoto(t, "dup_bad")
	entry := audit.Entry{Action: audit.ActionDuplicateDismiss, TargetType: "photos", TargetUID: photo}

	// A photo is not a duplicate of itself.
	same := feedback.DuplicateDismissalKey{PhotoUID: photo, OtherUID: photo}
	if err := f.feedback.DismissDuplicate(ctx, same, entry); !errors.Is(err, feedback.ErrSamePhoto) {
		t.Fatalf("DismissDuplicate(self) = %v, want ErrSamePhoto", err)
	}
	// An incomplete key never reaches the database.
	partial := feedback.DuplicateDismissalKey{PhotoUID: photo}
	if err := f.feedback.DismissDuplicate(ctx, partial, entry); !errors.Is(err, feedback.ErrEmptyKey) {
		t.Fatalf("DismissDuplicate(partial) = %v, want ErrEmptyKey", err)
	}
	// A pair naming a photo that does not exist trips the foreign key.
	ghost := feedback.DuplicateDismissalKey{PhotoUID: photo, OtherUID: "ph_nonexistent00000000000"}
	if err := f.feedback.DismissDuplicate(ctx, ghost, entry); !errors.Is(err, feedback.ErrTargetNotFound) {
		t.Fatalf("DismissDuplicate(ghost) = %v, want ErrTargetNotFound", err)
	}
	if n := f.count(t, "SELECT count(*) FROM duplicate_dismissals"); n != 0 {
		t.Fatalf("row count after rejected keys = %d, want 0", n)
	}
}

// TestDuplicateDismissalAudited checks the dismissal and its audit row are written
// together, so a settled pair is always traceable to who settled it.
func TestDuplicateDismissalAudited(t *testing.T) {
	f := newFixtures(t)
	ctx := t.Context()
	a := f.makePhoto(t, "dup_audit_a")
	b := f.makePhoto(t, "dup_audit_b")
	user := f.makeUser(t, "usr_dupdismiss000000000", "dupdismisser")
	key := feedback.DuplicateDismissalKey{PhotoUID: a, OtherUID: b}
	entry := audit.Entry{
		Action: audit.ActionDuplicateDismiss, TargetType: "photos", TargetUID: a, ActorUID: user,
	}

	if err := f.feedback.DismissDuplicate(ctx, key, entry); err != nil {
		t.Fatalf("DismissDuplicate: %v", err)
	}
	n := f.count(t, "SELECT count(*) FROM audit_log WHERE action = $1", audit.ActionDuplicateDismiss)
	if n != 1 {
		t.Fatalf("audit rows for a dismissal = %d, want 1", n)
	}
	// entry.ActorUID doubles as dismissed_by, so the row itself names the user.
	n = f.count(t, "SELECT count(*) FROM duplicate_dismissals WHERE dismissed_by = $1", user)
	if n != 1 {
		t.Fatalf("dismissals attributed to the actor = %d, want 1", n)
	}
}
