//go:build integration

package auth_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// TestStore_SetInstanceRoleAudited covers the instance-role write behind the
// system administrator's promote/demote: a promotion and a demotion land with
// their audit rows, the last enabled maintainer cannot be demoted, an invalid
// role and an unknown account are refused, and no refusal writes an audit row.
func TestStore_SetInstanceRoleAudited(t *testing.T) {
	env := newTestEnv(t)
	ctx := t.Context()
	maint := env.createUser(t, "maint", auth.AccountMaintainer)
	editor := env.createUser(t, "editor", auth.AccountEditor)
	entry := func() audit.Entry { return mgmtEntry(maint.UID, audit.ActionUserInstanceRole) }

	promoted, err := env.store.SetInstanceRoleAudited(ctx, editor.UID, auth.RoleMaintainer, entry())
	if err != nil || promoted.Role != auth.RoleMaintainer {
		t.Fatalf("promote = %q, %v; want maintainer", promoted.Role, err)
	}
	if _, err := env.store.SetInstanceRoleAudited(ctx, editor.UID, auth.RoleUser, entry()); err != nil {
		t.Fatalf("demote: %v", err)
	}

	if _, err := env.store.SetInstanceRoleAudited(ctx, maint.UID, auth.RoleUser, entry()); !errors.Is(
		err, auth.ErrLastMaintainer) {
		t.Errorf("demoting the last maintainer err = %v, want ErrLastMaintainer", err)
	}
	if _, err := env.store.SetInstanceRoleAudited(ctx, editor.UID, "admin", entry()); !errors.Is(
		err, auth.ErrInvalidRole) {
		t.Errorf("invalid role err = %v, want ErrInvalidRole", err)
	}
	if _, err := env.store.SetInstanceRoleAudited(ctx, "usmissing", auth.RoleUser, entry()); !errors.Is(
		err, auth.ErrUserNotFound) {
		t.Errorf("unknown account err = %v, want ErrUserNotFound", err)
	}

	var rows int
	if err := env.db.Pool().QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`,
		audit.ActionUserInstanceRole).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("user.instance_role audit rows = %d, %v; want 2 (refusals record nothing)", rows, err)
	}
}
