//go:build integration

package stacks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/stacks"
)

// These tests run only under `make test-integration`. They exercise the stack
// operations against the real database and assert each one appends the audit_log
// row that records it — grouping, changing which variant is shown and ungrouping
// all change what the library shows, so none of them may commit silently — and
// that a refused operation appends none.

// makeActor seeds the user an audit entry is attributed to. The actor column is a
// foreign key, so an unseeded actor fails the insert with SQLSTATE 23503 rather
// than storing a dangling uid.
func makeActor(t *testing.T, db *database.DB, uid string) string {
	t.Helper()
	store := auth.NewStore(db.Pool())
	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     uid,
		Email:        uid + "@example.test",
		PasswordHash: "x",
		Role:         auth.RoleEditor,
	}); err != nil {
		t.Fatalf("creating actor %s: %v", uid, err)
	}
	return uid
}

// actorEntry builds the audit entry a handler would hand down: who acted, under
// which action, on which kind of entity. The store fills in the rest.
func actorEntry(actorUID, action string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: action, TargetType: "photos"}
}

// oneAuditRow asserts exactly one audit_log row exists for action, checks its
// actor and target and returns it for detail assertions.
func oneAuditRow(
	t *testing.T, ctx context.Context, store *audit.Store, action, actorUID, targetUID string,
) audit.Record {
	t.Helper()
	recs, err := store.List(ctx, audit.Filter{Action: action, Limit: 50})
	if err != nil {
		t.Fatalf("listing audit %s: %v", action, err)
	}
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

// detailString reads a string out of an audit row's details, failing when it is
// missing or of another type.
func detailString(t *testing.T, rec audit.Record, key string) string {
	t.Helper()
	value, ok := rec.Details[key].(string)
	if !ok {
		t.Fatalf("details[%q] = %v, want a string (%+v)", key, rec.Details[key], rec.Details)
	}
	return value
}

// detailUIDs reads a list of uids out of an audit row's details. JSONB comes back
// as []any, so the elements are converted one by one.
func detailUIDs(t *testing.T, rec audit.Record, key string) []string {
	t.Helper()
	raw, ok := rec.Details[key].([]any)
	if !ok {
		t.Fatalf("details[%q] = %v, want a list (%+v)", key, rec.Details[key], rec.Details)
	}
	out := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("details[%q][%d] = %v, want a string", key, i, v)
		}
		out[i] = s
	}
	return out
}

func TestIntegration_StackSelectionIsAudited(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeActor(t, db, "stau_group")
	a := makePhoto(t, store, "aud_a", "A.jpg", "image/jpeg", 4000, 3000)
	b := makePhoto(t, store, "aud_b", "B.jpg", "image/jpeg", 6000, 4000) // highest resolution

	svc := stacks.New(store, stacks.Config{Enabled: true})
	stackUID, err := svc.StackSelection(ctx, []string{a.UID, b.UID}, actorEntry(actor, audit.ActionPhotosStack))
	if err != nil {
		t.Fatalf("StackSelection: %v", err)
	}

	// The primary is the target, so the trail links to the tile that survived.
	rec := oneAuditRow(t, ctx, auditStore, audit.ActionPhotosStack, actor, b.UID)
	if got := detailString(t, rec, "stack_uid"); got != stackUID {
		t.Errorf("details stack_uid = %q, want %q", got, stackUID)
	}
	if got := detailString(t, rec, "primary_uid"); got != b.UID {
		t.Errorf("details primary_uid = %q, want %q", got, b.UID)
	}
	if got := detailUIDs(t, rec, "photo_uids"); len(got) != 2 {
		t.Errorf("details photo_uids = %v, want both members", got)
	}
}

func TestIntegration_SetPrimaryIsAudited(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeActor(t, db, "stau_primary")
	a := makePhoto(t, store, "aud_pa", "A.jpg", "image/jpeg", 4000, 3000)
	b := makePhoto(t, store, "aud_pb", "B.jpg", "image/jpeg", 6000, 4000)

	svc := stacks.New(store, stacks.Config{Enabled: true})
	stackUID, err := svc.StackSelection(ctx, []string{a.UID, b.UID}, actorEntry(actor, audit.ActionPhotosStack))
	if err != nil {
		t.Fatalf("StackSelection: %v", err)
	}
	if _, err := svc.SetPrimary(ctx, a.UID, actorEntry(actor, audit.ActionStackSetPrimary)); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}

	rec := oneAuditRow(t, ctx, auditStore, audit.ActionStackSetPrimary, actor, a.UID)
	if got := detailString(t, rec, "stack_uid"); got != stackUID {
		t.Errorf("details stack_uid = %q, want %q", got, stackUID)
	}
	// Both halves of the swap are recorded, so the change can be read back.
	if got := detailString(t, rec, "previous_primary_uid"); got != b.UID {
		t.Errorf("details previous_primary_uid = %q, want %q", got, b.UID)
	}
	if got := detailString(t, rec, "photo_uid"); got != a.UID {
		t.Errorf("details photo_uid = %q, want %q", got, a.UID)
	}
}

func TestIntegration_UngroupIsAudited(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeActor(t, db, "stau_ungroup")
	a := makePhoto(t, store, "aud_ua", "A.jpg", "image/jpeg", 4000, 3000)
	b := makePhoto(t, store, "aud_ub", "B.jpg", "image/jpeg", 6000, 4000)
	c := makePhoto(t, store, "aud_uc", "C.jpg", "image/jpeg", 3000, 2000)

	svc := stacks.New(store, stacks.Config{Enabled: true})
	stackUID, err := svc.StackSelection(ctx, []string{a.UID, b.UID, c.UID}, actorEntry(actor, audit.ActionPhotosStack))
	if err != nil {
		t.Fatalf("StackSelection: %v", err)
	}
	if _, err := svc.Unstack(ctx, c.UID, actorEntry(actor, audit.ActionStackUngroup)); err != nil {
		t.Fatalf("Unstack: %v", err)
	}

	rec := oneAuditRow(t, ctx, auditStore, audit.ActionStackUngroup, actor, c.UID)
	if got := detailString(t, rec, "stack_uid"); got != stackUID {
		t.Errorf("details stack_uid = %q, want %q", got, stackUID)
	}
	if got := detailString(t, rec, "photo_uid"); got != c.UID {
		t.Errorf("details photo_uid = %q, want %q", got, c.UID)
	}

	// Dissolving the remnant lists the members it had — once the rows are
	// standalone again, nothing else records that they ever belonged together.
	if _, err := svc.UnstackWhole(ctx, a.UID, actorEntry(actor, audit.ActionStackUngroupAll)); err != nil {
		t.Fatalf("UnstackWhole: %v", err)
	}
	whole := oneAuditRow(t, ctx, auditStore, audit.ActionStackUngroupAll, actor, a.UID)
	members := detailUIDs(t, whole, "photo_uids")
	if len(members) != 2 {
		t.Errorf("details photo_uids = %v, want the two remaining members", members)
	}
}

func TestIntegration_DetectStacksIsAuditedOnce(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeActor(t, db, "stau_detect")
	makePhoto(t, store, "aud_draw", "IMG_9.CR2", "image/x-canon-cr2", 6000, 4000)
	makePhoto(t, store, "aud_djpg", "IMG_9.jpg", "image/jpeg", 6000, 4000)

	created, err := detector(store).DetectStacks(ctx, actorEntry(actor, audit.ActionStacksDetect))
	if err != nil || created != 1 {
		t.Fatalf("DetectStacks created %d (err %v), want 1", created, err)
	}

	// One entry for the pass, naming no single photo but every stack it formed.
	recs, err := auditStore.List(ctx, audit.Filter{Action: audit.ActionStacksDetect, Limit: 50})
	if err != nil {
		t.Fatalf("listing audit: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("stacks.detect rows = %d, want 1 for the whole pass", len(recs))
	}
	if recs[0].TargetUID != nil {
		t.Errorf("stacks.detect target = %v, want none (the pass spans many stacks)", recs[0].TargetUID)
	}
	if got, ok := recs[0].Details["created"].(float64); !ok || int(got) != 1 {
		t.Errorf("details created = %v, want 1", recs[0].Details["created"])
	}
	if got := detailUIDs(t, recs[0], "stack_uids"); len(got) != 1 {
		t.Errorf("details stack_uids = %v, want the one stack formed", got)
	}
}

func TestIntegration_RefusedStackOperationLeavesNoTrail(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()
	auditStore := audit.NewStore(db.Pool())
	actor := makeActor(t, db, "stau_refused")
	lone := makePhoto(t, store, "aud_lone", "L.jpg", "image/jpeg", 4000, 3000)

	svc := stacks.New(store, stacks.Config{Enabled: true})
	// An unstacked photo has no primary to set: the mutation rolls back, and the
	// audit row must roll back with it.
	if _, err := svc.SetPrimary(ctx, lone.UID, actorEntry(actor, audit.ActionStackSetPrimary)); !errors.Is(
		err, photos.ErrPhotoNotStacked,
	) {
		t.Fatalf("SetPrimary on an unstacked photo = %v, want ErrPhotoNotStacked", err)
	}
	recs, err := auditStore.List(ctx, audit.Filter{Limit: 50})
	if err != nil {
		t.Fatalf("listing audit: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("audit rows after a refused operation = %d, want 0 (%+v)", len(recs), recs)
	}
}
