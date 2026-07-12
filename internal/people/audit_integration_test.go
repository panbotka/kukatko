//go:build integration

package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They exercise the audited subject and face-assignment
// store methods against the real database and assert that each one appends the
// expected audit_log row in the mutation's transaction, and that a rolled-back
// mutation writes none — the durable-audit guarantee from ARCHITECTURE.md §5.1.

// actorEntry builds an audit entry attributed to actorUID the way a handler would
// (minus the request IP/UA, which the store does not depend on).
func actorEntry(actorUID, action, targetType, targetUID string, details map[string]any) audit.Entry {
	return audit.Entry{
		ActorUID:   actorUID,
		Action:     action,
		TargetType: targetType,
		TargetUID:  targetUID,
		Details:    details,
	}
}

// makeUser inserts a viewer account with the given uid/username so audit rows have a
// valid actor to reference (audit_log.actor_uid is an FK to users), and returns the uid.
func makeUser(t *testing.T, db *database.DB, uid, username string) string {
	t.Helper()
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// auditRecords returns the audit_log rows for action, newest first.
func auditRecords(t *testing.T, ctx context.Context, store *audit.Store, action string) []audit.Record {
	t.Helper()
	recs, err := store.List(ctx, audit.Filter{Action: action, Limit: 50})
	if err != nil {
		t.Fatalf("listing audit %s: %v", action, err)
	}
	return recs
}

// requireOneAudit asserts exactly one audit_log row exists for action with the given
// actor and target, and returns it for further detail assertions.
func requireOneAudit(
	t *testing.T, ctx context.Context, store *audit.Store, action, actorUID, targetUID string,
) audit.Record {
	t.Helper()
	recs := auditRecords(t, ctx, store, action)
	if len(recs) != 1 {
		t.Fatalf("audit %s rows = %d, want 1 (%+v)", action, len(recs), recs)
	}
	assertActorTarget(t, action, recs[0], actorUID, targetUID)
	return recs[0]
}

// latestAudit asserts at least one audit_log row exists for action and returns the
// newest, checking its actor and target. It suits actions written more than once in a
// single test (a create-marker assignment and a later existing-marker assignment both
// record face.assign).
func latestAudit(
	t *testing.T, ctx context.Context, store *audit.Store, action, actorUID, targetUID string,
) audit.Record {
	t.Helper()
	recs := auditRecords(t, ctx, store, action)
	if len(recs) == 0 {
		t.Fatalf("audit %s rows = 0, want at least 1", action)
	}
	assertActorTarget(t, action, recs[0], actorUID, targetUID)
	return recs[0]
}

// assertActorTarget fails the test unless rec is attributed to actorUID and targets
// targetUID.
func assertActorTarget(t *testing.T, action string, rec audit.Record, actorUID, targetUID string) {
	t.Helper()
	if rec.ActorUID == nil || *rec.ActorUID != actorUID {
		t.Errorf("audit %s actor = %v, want %q", action, rec.ActorUID, actorUID)
	}
	if rec.TargetUID == nil || *rec.TargetUID != targetUID {
		t.Errorf("audit %s target = %v, want %q", action, rec.TargetUID, targetUID)
	}
}

// requireNoAudit asserts no audit_log row exists for action, proving a rolled-back
// mutation left no trail.
func requireNoAudit(t *testing.T, ctx context.Context, store *audit.Store, action string) {
	t.Helper()
	if recs := auditRecords(t, ctx, store, action); len(recs) != 0 {
		t.Fatalf("audit %s rows = %d, want 0 (%+v)", action, len(recs), recs)
	}
}

// TestAuditSubjectMutations checks every subject mutation records one audit row for
// the acting user, targeting the subject, with name/type in the details.
func TestAuditSubjectMutations(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, db, "usr_audp", "audp")

	created, err := store.CreateSubjectAudited(ctx, people.Subject{Name: "Anna Nováková"},
		actorEntry(actor, audit.ActionSubjectCreate, "subjects", "",
			map[string]any{"name": "Anna Nováková", "type": "person"}))
	if err != nil {
		t.Fatalf("CreateSubjectAudited: %v", err)
	}
	rec := requireOneAudit(t, ctx, auditStore, audit.ActionSubjectCreate, actor, created.UID)
	if rec.Details["name"] != "Anna Nováková" || rec.Details["type"] != "person" {
		t.Errorf("create details = %v, want name/type", rec.Details)
	}

	if _, err := store.UpdateSubjectAudited(ctx, created.UID,
		people.SubjectUpdate{Name: "Anna B", Type: people.SubjectPerson},
		actorEntry(actor, audit.ActionSubjectUpdate, "subjects", created.UID,
			map[string]any{"name": "Anna B", "type": "person"})); err != nil {
		t.Fatalf("UpdateSubjectAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionSubjectUpdate, actor, created.UID)

	if err := store.DeleteSubjectAudited(ctx, created.UID,
		actorEntry(actor, audit.ActionSubjectDelete, "subjects", created.UID,
			map[string]any{"name": "Anna B", "type": "person"})); err != nil {
		t.Fatalf("DeleteSubjectAudited: %v", err)
	}
	del := requireOneAudit(t, ctx, auditStore, audit.ActionSubjectDelete, actor, created.UID)
	if del.Details["name"] != "Anna B" {
		t.Errorf("delete details name = %v, want Anna B", del.Details["name"])
	}
}

// TestAuditSubjectRollback checks a subject mutation targeting a missing subject
// writes no audit row.
func TestAuditSubjectRollback(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, db, "usr_audp", "audp")

	_, err := store.UpdateSubjectAudited(ctx, "su_missing",
		people.SubjectUpdate{Name: "Ghost", Type: people.SubjectPerson},
		actorEntry(actor, audit.ActionSubjectUpdate, "subjects", "su_missing", nil))
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("UpdateSubjectAudited err = %v, want ErrSubjectNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionSubjectUpdate)

	err = store.DeleteSubjectAudited(ctx, "su_missing",
		actorEntry(actor, audit.ActionSubjectDelete, "subjects", "su_missing", nil))
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("DeleteSubjectAudited err = %v, want ErrSubjectNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionSubjectDelete)
}

// TestAuditFaceMutations checks each face-assignment mutation records one audit row
// for the acting user, targeting the affected marker: creating a marker while
// assigning and assigning an existing marker both record face.assign; clearing a
// marker's subject records face.unassign.
func TestAuditFaceMutations(t *testing.T) {
	store, photoStore, vectorStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, db, "usr_audf", "audf")
	photoUID := makePhoto(t, photoStore, "auditfacephoto")
	subj, err := store.CreateSubject(ctx, people.Subject{Name: "Face Subject"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	// create_marker: a new marker is inserted already naming the subject.
	m1, err := store.CreateMarkerAudited(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &subj.UID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}, actorEntry(actor, audit.ActionFaceAssign, "markers", "",
		map[string]any{"action": "create_marker", "subject_uid": subj.UID}))
	if err != nil {
		t.Fatalf("CreateMarkerAudited: %v", err)
	}
	rec := latestAudit(t, ctx, auditStore, audit.ActionFaceAssign, actor, m1.UID)
	if rec.Details["subject_uid"] != subj.UID {
		t.Errorf("create_marker details subject_uid = %v, want %q", rec.Details["subject_uid"], subj.UID)
	}
	if n := len(auditRecords(t, ctx, auditStore, audit.ActionFaceAssign)); n != 1 {
		t.Fatalf("face.assign rows after create_marker = %d, want 1", n)
	}

	// assign_person: an existing, unassigned marker is pointed at the subject, and
	// the linked face's cache follows in the same transaction.
	m2, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 0.4, Y: 0.4, W: 0.2, H: 0.2,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	saveLinkedFace(t, vectorStore, photoUID, m2.UID)
	if _, err := store.AssignSubjectAudited(ctx, m2.UID, subj.UID,
		actorEntry(actor, audit.ActionFaceAssign, "markers", m2.UID,
			map[string]any{"action": "assign_person", "subject_uid": subj.UID})); err != nil {
		t.Fatalf("AssignSubjectAudited: %v", err)
	}
	if n := len(auditRecords(t, ctx, auditStore, audit.ActionFaceAssign)); n != 2 {
		t.Fatalf("face.assign rows after assign_person = %d, want 2", n)
	}
	latestAudit(t, ctx, auditStore, audit.ActionFaceAssign, actor, m2.UID)
	if uid, _ := faceCache(t, vectorStore, photoUID); uid == nil || *uid != subj.UID {
		t.Errorf("faces cache subject_uid = %v, want %q", uid, subj.UID)
	}

	// unassign: the marker's subject is cleared.
	if _, err := store.UnassignSubjectAudited(ctx, m2.UID,
		actorEntry(actor, audit.ActionFaceUnassign, "markers", m2.UID,
			map[string]any{"marker_uid": m2.UID})); err != nil {
		t.Fatalf("UnassignSubjectAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionFaceUnassign, actor, m2.UID)
	if uid, _ := faceCache(t, vectorStore, photoUID); uid != nil {
		t.Errorf("faces cache subject_uid = %v after unassign, want nil", uid)
	}
}

// TestAuditFaceRollback checks a face-assignment mutation targeting a missing marker
// fails and writes no audit row.
func TestAuditFaceRollback(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, db, "usr_audf", "audf")
	subj, err := store.CreateSubject(ctx, people.Subject{Name: "Rollback Subject"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	_, err = store.AssignSubjectAudited(ctx, "mk_missing", subj.UID,
		actorEntry(actor, audit.ActionFaceAssign, "markers", "mk_missing", nil))
	if !errors.Is(err, people.ErrMarkerNotFound) {
		t.Fatalf("AssignSubjectAudited err = %v, want ErrMarkerNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionFaceAssign)

	_, err = store.UnassignSubjectAudited(ctx, "mk_missing",
		actorEntry(actor, audit.ActionFaceUnassign, "markers", "mk_missing", nil))
	if !errors.Is(err, people.ErrMarkerNotFound) {
		t.Fatalf("UnassignSubjectAudited err = %v, want ErrMarkerNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionFaceUnassign)
}
