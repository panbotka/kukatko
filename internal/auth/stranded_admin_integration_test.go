//go:build integration

package auth_test

import (
	"reflect"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// newMaintainerOutsideProjects creates a maintainer and drops the admin
// membership of the bootstrap project its creation grants, so it is nobody's
// working admin and the admins under test are the only ones.
func newMaintainerOutsideProjects(t *testing.T, env *testEnv) auth.User {
	t.Helper()
	maint := env.createUser(t, "maint", auth.AccountMaintainer)
	if _, err := env.db.Pool().Exec(t.Context(),
		`DELETE FROM project_members WHERE user_uid = $1`, maint.UID); err != nil {
		t.Fatalf("dropping the maintainer's membership: %v", err)
	}
	return maint
}

// disableAudit returns the one user.disable audit row targeting uid.
func disableAudit(t *testing.T, env *testEnv, uid string) audit.Record {
	t.Helper()
	recs, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionUserDisable, TargetUID: uid, Limit: 10})
	if err != nil {
		t.Fatalf("listing user.disable audit of %s: %v", uid, err)
	}
	if len(recs) != 1 {
		t.Fatalf("user.disable audit rows of %s = %d, want 1", uid, len(recs))
	}
	return recs[0]
}

// TestDisable_lastWorkingAdminIsAllowedAndNoted pins the choice for a disable
// that leaves a project without a working admin: it is allowed (a maintainer
// locking an account out must be able to), but never silent — the audit entry
// names the stranded projects. Disabling an admin while another working one
// remains notes nothing, and the caller's details map is never written.
func TestDisable_lastWorkingAdminIsAllowedAndNoted(t *testing.T) {
	env := newTestEnv(t)
	maint := newMaintainerOutsideProjects(t, env)
	first := env.createUser(t, "admin-one", auth.AccountAdmin)
	second := env.createUser(t, "admin-two", auth.AccountAdmin)

	details := map[string]any{"disabled": true}
	entry := audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserDisable, "users", first.UID, details)
	if _, err := env.svc.SetUserDisabledAudited(t.Context(), first.UID, true, maint.Role, entry); err != nil {
		t.Fatalf("disabling one of two admins: %v", err)
	}
	if rec := disableAudit(t, env, first.UID); rec.Details["stranded_projects"] != nil {
		t.Errorf("disabling one of two admins noted %v", rec.Details["stranded_projects"])
	}

	entry = audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserDisable, "users", second.UID, details)
	user, err := env.svc.SetUserDisabledAudited(t.Context(), second.UID, true, maint.Role, entry)
	if err != nil {
		t.Fatalf("disabling the last working admin: %v", err)
	}
	if !user.Disabled {
		t.Errorf("last working admin disabled = false, want true")
	}
	got := disableAudit(t, env, second.UID).Details["stranded_projects"]
	if want := []any{project.BootstrapUID}; !reflect.DeepEqual(got, want) {
		t.Errorf("stranded_projects = %#v, want %#v", got, want)
	}
	if len(details) != 1 {
		t.Errorf("the caller's details map was written: %v", details)
	}
}

// TestDisable_profileUpdateNotesStrandedProjects covers the other way to
// disable an account, a profile update with disabled set.
func TestDisable_profileUpdateNotesStrandedProjects(t *testing.T) {
	env := newTestEnv(t)
	maint := newMaintainerOutsideProjects(t, env)
	admin := env.createUser(t, "admin-only", auth.AccountAdmin)

	entry := audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserUpdate, "users", admin.UID,
		map[string]any{"disabled": true})
	if _, err := env.svc.UpdateUserAudited(t.Context(), admin.UID, auth.UpdateUserInput{
		Email: admin.Email, InstanceRole: auth.RoleUser, Disabled: true,
	}, maint.Role, entry); err != nil {
		t.Fatalf("disabling the only admin by a profile update: %v", err)
	}
	recs, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionUserUpdate, TargetUID: admin.UID, Limit: 10})
	if err != nil || len(recs) != 1 {
		t.Fatalf("user.update audit = %v, %v; want one row", recs, err)
	}
	if got, want := recs[0].Details["stranded_projects"], []any{project.BootstrapUID}; !reflect.DeepEqual(got, want) {
		t.Errorf("stranded_projects = %#v, want %#v", got, want)
	}
}
