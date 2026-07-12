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

// These tests run only under `make test-integration`. They exercise the audited
// subject and face-assignment mutations against the real database and assert that
// each appends the expected audit_log row in the mutation's transaction, and that
// a rolled-back mutation writes none — the durable-audit guarantee from
// ARCHITECTURE.md §5.1.

// makeAuditUser inserts a user so a non-null actor_uid satisfies the audit_log FK.
func makeAuditUser(t *testing.T, db *database.DB, uid string) string {
	t.Helper()
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     uid,
		PasswordHash: "x",
		Role:         auth.RoleEditor,
	}); err != nil {
		t.Fatalf("creating user %s: %v", uid, err)
	}
	return uid
}

// actorEntry builds an audit entry attributed to actorUID the way a handler would
// (minus the request IP/UA, which the store does not depend on).
func actorEntry(actorUID, action, targetUID string, details map[string]any) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: action, TargetType: "subjects", TargetUID: targetUID, Details: details}
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

// requireOneAudit asserts exactly one audit_log row exists for action with the
// given actor and target, and returns it for further detail assertions.
func requireOneAudit(
	t *testing.T, ctx context.Context, store *audit.Store, action, actorUID, targetUID string,
) audit.Record {
	t.Helper()
	recs := auditRecords(t, ctx, store, action)
	if len(recs) != 1 {
		t.Fatalf("audit %s rows = %d, want 1 (%+v)", action, len(recs), recs)
	}
	rec := recs[0]
	if rec.ActorUID == nil || *rec.ActorUID != actorUID {
		t.Errorf("audit %s actor = %v, want %q", action, rec.ActorUID, actorUID)
	}
	if rec.TargetUID == nil || *rec.TargetUID != targetUID {
		t.Errorf("audit %s target = %v, want %q", action, rec.TargetUID, targetUID)
	}
	return rec
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
// the acting user, targeting the subject, with useful details.
func TestAuditSubjectMutations(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeAuditUser(t, db, "usr_subj")

	created, err := store.CreateSubjectAudited(ctx, people.Subject{Name: "Alice", Type: people.SubjectPerson},
		actorEntry(actor, audit.ActionSubjectCreate, "", map[string]any{"name": "Alice", "type": "person"}))
	if err != nil {
		t.Fatalf("CreateSubjectAudited: %v", err)
	}
	rec := requireOneAudit(t, ctx, auditStore, audit.ActionSubjectCreate, actor, created.UID)
	if rec.Details["name"] != "Alice" {
		t.Errorf("create details name = %v, want Alice", rec.Details["name"])
	}

	if _, err := store.UpdateSubjectAudited(ctx, created.UID,
		people.SubjectUpdate{Name: "Alice II", Type: people.SubjectPet},
		actorEntry(actor, audit.ActionSubjectUpdate, created.UID, map[string]any{"name": "Alice II"})); err != nil {
		t.Fatalf("UpdateSubjectAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionSubjectUpdate, actor, created.UID)

	if err := store.DeleteSubjectAudited(ctx, created.UID,
		actorEntry(actor, audit.ActionSubjectDelete, created.UID,
			map[string]any{"name": "Alice II", "type": "pet"})); err != nil {
		t.Fatalf("DeleteSubjectAudited: %v", err)
	}
	rec = requireOneAudit(t, ctx, auditStore, audit.ActionSubjectDelete, actor, created.UID)
	if rec.Details["type"] != "pet" {
		t.Errorf("delete details type = %v, want pet", rec.Details["type"])
	}
}

// TestAuditFaceAssignMutations checks the face-assignment mutations each record one
// audit row targeting the marker, capturing the subject.
func TestAuditFaceAssignMutations(t *testing.T) {
	store, photoStore, vectorStore, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeAuditUser(t, db, "usr_face")
	photoUID := makePhoto(t, photoStore, "auditfacephoto")

	subj, err := store.CreateSubject(ctx, people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	// CreateMarkerAudited: a fresh marker assigned to the subject.
	created, err := store.CreateMarkerAudited(ctx, people.Marker{
		PhotoUID: photoUID, SubjectUID: &subj.UID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}, actorEntry(actor, audit.ActionFaceAssign, "", map[string]any{"subject_uid": subj.UID}))
	if err != nil {
		t.Fatalf("CreateMarkerAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionFaceAssign, actor, created.UID)

	// A plain marker to exercise assign then unassign.
	marker, err := store.CreateMarker(ctx, people.Marker{
		PhotoUID: photoUID, Type: people.MarkerFace, X: 0.5, Y: 0.5, W: 0.1, H: 0.1,
	})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	saveLinkedFace(t, vectorStore, photoUID, marker.UID)

	if _, err := store.AssignSubjectAudited(ctx, marker.UID, subj.UID,
		actorEntry(actor, audit.ActionFaceAssign, marker.UID, map[string]any{"subject_uid": subj.UID})); err != nil {
		t.Fatalf("AssignSubjectAudited: %v", err)
	}
	// Two face.assign rows now: the create above and this assign.
	if recs := auditRecords(t, ctx, auditStore, audit.ActionFaceAssign); len(recs) != 2 {
		t.Fatalf("face.assign rows = %d, want 2", len(recs))
	}
	if gotUID, _ := faceCache(t, vectorStore, photoUID); gotUID == nil || *gotUID != subj.UID {
		t.Errorf("face cache subject_uid = %v, want %q", gotUID, subj.UID)
	}

	if _, err := store.UnassignSubjectAudited(ctx, marker.UID,
		actorEntry(actor, audit.ActionFaceUnassign, marker.UID, nil)); err != nil {
		t.Fatalf("UnassignSubjectAudited: %v", err)
	}
	requireOneAudit(t, ctx, auditStore, audit.ActionFaceUnassign, actor, marker.UID)
	if gotUID, _ := faceCache(t, vectorStore, photoUID); gotUID != nil {
		t.Errorf("face cache subject_uid = %v, want nil after unassign", gotUID)
	}
}

// TestAuditSubjectRollback checks a failed mutation writes no audit row.
func TestAuditSubjectRollback(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeAuditUser(t, db, "usr_rb")

	// Deleting a missing subject changes nothing and writes no audit row.
	err := store.DeleteSubjectAudited(ctx, "su_missing",
		actorEntry(actor, audit.ActionSubjectDelete, "su_missing", nil))
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("DeleteSubjectAudited err = %v, want ErrSubjectNotFound", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionSubjectDelete)

	// Assigning a missing marker rolls back and writes no audit row.
	_, err = store.AssignSubjectAudited(ctx, "mk_missing", "su_missing",
		actorEntry(actor, audit.ActionFaceAssign, "mk_missing", nil))
	if !errors.Is(err, people.ErrSubjectNotFound) && !errors.Is(err, people.ErrMarkerNotFound) {
		t.Fatalf("AssignSubjectAudited err = %v, want not-found sentinel", err)
	}
	requireNoAudit(t, ctx, auditStore, audit.ActionFaceAssign)
}
