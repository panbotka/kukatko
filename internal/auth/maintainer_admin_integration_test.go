//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// mgmtEntry builds a minimal audit entry for a user-management mutation by the
// actor identified by actorUID, so the audited service methods have something to
// record when the maintainer boundary lets the call through.
func mgmtEntry(actorUID, action string) audit.Entry {
	return audit.Meta{ActorUID: actorUID}.Entry(action, "users", "", nil)
}

// TestMaintainerBoundary_create asserts, through the real service and database,
// that only a maintainer may create a maintainer account: an admin actor is
// refused with ErrMaintainerRequired, while lower and equal roles are allowed;
// and the retired 'ai' role is rejected as invalid.
func TestMaintainerBoundary_create(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	admin := env.createUser(t, "admin", auth.RoleAdmin)
	maint := env.createUser(t, "maint", auth.RoleMaintainer)

	tests := []struct {
		name     string
		actor    auth.Role
		username string
		role     auth.Role
		wantErr  error
	}{
		{"admin creates viewer", admin.Role, "v", auth.RoleViewer, nil},
		{"admin creates admin", admin.Role, "a", auth.RoleAdmin, nil},
		{"admin creates maintainer", admin.Role, "m1", auth.RoleMaintainer, auth.ErrMaintainerRequired},
		{"admin creates ai", admin.Role, "ai1", auth.Role("ai"), auth.ErrInvalidRole},
		{"maintainer creates maintainer", maint.Role, "m2", auth.RoleMaintainer, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := env.svc.CreateUserAudited(ctx, auth.CreateUserInput{
				Username: tt.username, Password: testPassword, Role: tt.role,
			}, tt.actor, mgmtEntry(admin.UID, audit.ActionUserCreate))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateUserAudited(actor=%s, role=%s) err = %v, want %v",
					tt.actor, tt.role, err, tt.wantErr)
			}
		})
	}
}

// TestMaintainerBoundary_modify asserts an admin actor cannot promote an account
// to maintainer, nor disable or reset the password of an existing maintainer,
// while a maintainer actor can do all three.
func TestMaintainerBoundary_modify(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	admin := env.createUser(t, "admin", auth.RoleAdmin)
	maint := env.createUser(t, "maint", auth.RoleMaintainer)
	editor := env.createUser(t, "editor", auth.RoleEditor)
	target := env.createUser(t, "target-maint", auth.RoleMaintainer)

	// An admin may not promote an editor to maintainer.
	_, err := env.svc.UpdateUserAudited(ctx, editor.UID, auth.UpdateUserInput{Role: auth.RoleMaintainer},
		admin.Role, mgmtEntry(admin.UID, audit.ActionUserUpdate))
	if !errors.Is(err, auth.ErrMaintainerRequired) {
		t.Fatalf("admin promote to maintainer err = %v, want ErrMaintainerRequired", err)
	}

	// An admin may not disable an existing maintainer.
	if _, err := env.svc.SetUserDisabledAudited(ctx, target.UID, true, admin.Role,
		mgmtEntry(admin.UID, audit.ActionUserDisable)); !errors.Is(err, auth.ErrMaintainerRequired) {
		t.Fatalf("admin disable maintainer err = %v, want ErrMaintainerRequired", err)
	}

	// An admin may not reset an existing maintainer's password.
	if err := env.svc.ResetPasswordAudited(ctx, target.UID, "another-strong-pass", admin.Role,
		mgmtEntry(admin.UID, audit.ActionUserPassword)); !errors.Is(err, auth.ErrMaintainerRequired) {
		t.Fatalf("admin reset maintainer password err = %v, want ErrMaintainerRequired", err)
	}

	// A maintainer may promote an editor to maintainer.
	if _, err := env.svc.UpdateUserAudited(ctx, editor.UID, auth.UpdateUserInput{Role: auth.RoleMaintainer},
		maint.Role, mgmtEntry(maint.UID, audit.ActionUserUpdate)); err != nil {
		t.Fatalf("maintainer promote to maintainer: %v", err)
	}
}

// TestMigration_aiRowBecomesMaintainer proves the 0036 migration body: an account
// on the retired 'ai' role is rewritten to 'maintainer' before the new CHECK
// constraint forbids 'ai'. The already-migrated test database has no 'ai' rows to
// migrate, so the test reproduces the pre-0036 state and replays the migration's
// statements inside a transaction it rolls back, leaving the shared schema untouched.
func TestMigration_aiRowBecomesMaintainer(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	tx, err := env.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Reproduce the world 0036 starts from: the 0023 constraint (which admits
	// 'ai') and a legacy 'ai' account.
	mustExec(t, ctx, tx, `ALTER TABLE users DROP CONSTRAINT users_role_check`)
	mustExec(t, ctx, tx,
		`ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin','editor','viewer','ai'))`)
	mustExec(t, ctx, tx,
		`INSERT INTO users (uid, username, password_hash, role) VALUES ('usr_legacyai','legacy-ai','x','ai')`)

	// Replay the 0036 body: drop the old constraint (which forbids 'maintainer'),
	// migrate the data, then add the new constraint.
	mustExec(t, ctx, tx, `ALTER TABLE users DROP CONSTRAINT users_role_check`)
	mustExec(t, ctx, tx, `UPDATE users SET role = 'maintainer' WHERE role = 'ai'`)
	mustExec(t, ctx, tx,
		`ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('viewer','editor','admin','maintainer'))`)

	// The legacy 'ai' account is now a maintainer.
	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE uid = 'usr_legacyai'`).Scan(&role); err != nil {
		t.Fatalf("select migrated role: %v", err)
	}
	if role != string(auth.RoleMaintainer) {
		t.Fatalf("legacy ai account role = %q, want maintainer", role)
	}

	// And the new constraint forbids inserting 'ai'. The failure aborts to a
	// savepoint so the surrounding rollback still works cleanly.
	mustExec(t, ctx, tx, `SAVEPOINT probe_ai`)
	_, err = tx.Exec(ctx,
		`INSERT INTO users (uid, username, password_hash, role) VALUES ('usr_newai','new-ai','x','ai')`)
	if err == nil {
		t.Fatal("insert of role 'ai' succeeded; the CHECK constraint no longer forbids it")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("insert of role 'ai' error = %v, want CHECK violation (SQLSTATE 23514)", err)
	}
}

// mustExec runs sql on tx and fails the test on error. It keeps the migration
// replay readable by hiding the repeated error check.
func mustExec(t *testing.T, ctx context.Context, tx pgx.Tx, sql string) {
	t.Helper()
	if _, err := tx.Exec(ctx, sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
