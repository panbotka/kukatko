//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They assert that each admin user-administration
// mutation appends the expected audit_log row in the mutation's transaction (the
// durable-audit guarantee from ARCHITECTURE.md §5.1), and that a mutation
// targeting a missing user rolls back and writes none. They reuse newTestEnv /
// testPassword from auth_service_integration_test.go.

// requireOneUserAudit asserts exactly one audit_log row exists for action with the
// given actor and target, and returns it for further detail assertions.
func requireOneUserAudit(
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

// requireNoUserAudit asserts no audit_log row exists for action, proving a
// rolled-back mutation left no trail.
func requireNoUserAudit(t *testing.T, ctx context.Context, store *audit.Store, action string) {
	t.Helper()
	recs, err := store.List(ctx, audit.Filter{Action: action, Limit: 50})
	if err != nil {
		t.Fatalf("listing audit %s: %v", action, err)
	}
	if len(recs) != 0 {
		t.Fatalf("audit %s rows = %d, want 0 (%+v)", action, len(recs), recs)
	}
}

// TestUserAudit_mutationsRecordRows checks every audited user-admin mutation
// records one audit row for the acting admin, targeting the affected user, with
// useful details.
func TestUserAudit_mutationsRecordRows(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	auditStore := audit.NewStore(env.db.Pool())
	admin := env.createUser(t, "admin", auth.RoleAdmin)
	meta := audit.Meta{ActorUID: admin.UID, IP: "203.0.113.9", UserAgent: "admin-agent"}

	created, err := env.svc.CreateUserAudited(ctx, auth.CreateUserInput{
		Username: "newuser", Password: testPassword, Role: auth.RoleViewer,
	}, meta.Entry(audit.ActionUserCreate, "users", "", nil))
	if err != nil {
		t.Fatalf("CreateUserAudited: %v", err)
	}
	rec := requireOneUserAudit(t, ctx, auditStore, audit.ActionUserCreate, admin.UID, created.UID)
	if rec.Details["username"] != "newuser" || rec.Details["role"] != "viewer" {
		t.Errorf("create details = %v, want username=newuser role=viewer", rec.Details)
	}
	if rec.IP == nil || *rec.IP != "203.0.113.9" {
		t.Errorf("create ip = %v, want 203.0.113.9", rec.IP)
	}

	if _, err := env.svc.UpdateUserAudited(ctx, created.UID, auth.UpdateUserInput{
		DisplayName: "New Name", Role: auth.RoleEditor,
	}, meta.Entry(audit.ActionUserUpdate, "users", created.UID,
		map[string]any{"role": "editor", "disabled": false})); err != nil {
		t.Fatalf("UpdateUserAudited: %v", err)
	}
	upd := requireOneUserAudit(t, ctx, auditStore, audit.ActionUserUpdate, admin.UID, created.UID)
	if upd.Details["role"] != "editor" {
		t.Errorf("update details role = %v, want editor", upd.Details["role"])
	}

	if _, err := env.svc.SetUserDisabledAudited(ctx, created.UID, true,
		meta.Entry(audit.ActionUserDisable, "users", created.UID,
			map[string]any{"disabled": true})); err != nil {
		t.Fatalf("SetUserDisabledAudited: %v", err)
	}
	requireOneUserAudit(t, ctx, auditStore, audit.ActionUserDisable, admin.UID, created.UID)

	if err := env.svc.ResetPasswordAudited(ctx, created.UID, "another-strong-pass",
		meta.Entry(audit.ActionUserPassword, "users", created.UID, nil)); err != nil {
		t.Fatalf("ResetPasswordAudited: %v", err)
	}
	requireOneUserAudit(t, ctx, auditStore, audit.ActionUserPassword, admin.UID, created.UID)
}

// TestUserAudit_rollbackWritesNoAudit checks a user mutation targeting a missing
// user fails and writes no audit row, for both a RETURNING update and an Exec
// password reset.
func TestUserAudit_rollbackWritesNoAudit(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	auditStore := audit.NewStore(env.db.Pool())
	admin := env.createUser(t, "admin", auth.RoleAdmin)
	meta := audit.Meta{ActorUID: admin.UID}

	_, err := env.svc.UpdateUserAudited(ctx, "usr_missing", auth.UpdateUserInput{Role: auth.RoleViewer},
		meta.Entry(audit.ActionUserUpdate, "users", "usr_missing", nil))
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("UpdateUserAudited(missing) err = %v, want ErrUserNotFound", err)
	}
	requireNoUserAudit(t, ctx, auditStore, audit.ActionUserUpdate)

	err = env.svc.ResetPasswordAudited(ctx, "usr_missing", "another-strong-pass",
		meta.Entry(audit.ActionUserPassword, "users", "usr_missing", nil))
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("ResetPasswordAudited(missing) err = %v, want ErrUserNotFound", err)
	}
	requireNoUserAudit(t, ctx, auditStore, audit.ActionUserPassword)
}
