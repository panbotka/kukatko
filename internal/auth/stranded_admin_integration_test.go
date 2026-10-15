//go:build integration

package auth_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

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
		Email: new(admin.Email), InstanceRole: new(auth.RoleUser), Disabled: new(true),
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

// TestDisable_onlyTheTransitionNotesStrandedProjects pins that the note rides
// the enabled → disabled transition only: disabling an account that is already
// blocked, or a profile patch that sets disabled on it again, takes no working
// admin away and must not record the stranded projects a second time.
func TestDisable_onlyTheTransitionNotesStrandedProjects(t *testing.T) {
	env := newTestEnv(t)
	maint := newMaintainerOutsideProjects(t, env)
	admin := env.createUser(t, "admin-only", auth.AccountAdmin)

	entry := audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserDisable, "users", admin.UID, nil)
	for range 2 {
		if _, err := env.svc.SetUserDisabledAudited(t.Context(), admin.UID, true, maint.Role, entry); err != nil {
			t.Fatalf("disabling the only admin: %v", err)
		}
	}
	update := audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserUpdate, "users", admin.UID, nil)
	if _, err := env.svc.UpdateUserAudited(t.Context(), admin.UID, auth.UpdateUserInput{
		DisplayName: new("Still blocked"), Disabled: new(true),
	}, maint.Role, update); err != nil {
		t.Fatalf("editing the blocked admin: %v", err)
	}

	recs, err := audit.NewStore(env.db.Pool()).List(t.Context(), audit.Filter{TargetUID: admin.UID, Limit: 10})
	if err != nil {
		t.Fatalf("listing the audit of %s: %v", admin.UID, err)
	}
	noted := 0
	for _, rec := range recs {
		if rec.Details["stranded_projects"] != nil {
			noted++
		}
	}
	if len(recs) != 3 || noted != 1 {
		t.Errorf("audit rows = %d with %d stranded notes, want 3 rows and exactly one note", len(recs), noted)
	}
}

// lockWaiters counts the other sessions of the test database that are waiting
// for a lock.
func lockWaiters(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).
		Scan(&n); err != nil {
		t.Fatalf("counting lock waiters: %v", err)
	}
	return n
}

// TestDisable_serialisedWithTheLastAdminGuard races a maintainer disabling
// admin B against admin A demoting themselves, both in the one project they
// govern. Each alone is fine: the demotion sees B still working, the disable
// sees A still admin. Together they must not leave the project with no working
// admin and no stranded_projects note — the disable takes the project lock the
// demotion's last-admin guard holds, so one of them sees the other's result.
//
// The interleaving is forced: the test holds B's account row, the last thing
// the disable needs, so the disable is parked mid-transaction; then the
// demotion starts. A disable that does not hold the project lets the demotion
// commit underneath it.
func TestDisable_serialisedWithTheLastAdminGuard(t *testing.T) {
	env := newTestEnv(t)
	maint := newMaintainerOutsideProjects(t, env)
	adminA := env.createUser(t, "admin-a", auth.AccountAdmin)
	adminB := env.createUser(t, "admin-b", auth.AccountAdmin)
	pool := env.db.Pool()

	hold, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = hold.Rollback(context.WithoutCancel(t.Context())) }()
	if _, err := hold.Exec(t.Context(), `SELECT uid FROM users WHERE uid = $1 FOR UPDATE`, adminB.UID); err != nil {
		t.Fatalf("holding the account row: %v", err)
	}

	disabled := make(chan error, 1)
	go func() {
		entry := audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserDisable, "users", adminB.UID, nil)
		_, err := env.svc.SetUserDisabledAudited(t.Context(), adminB.UID, true, maint.Role, entry)
		disabled <- err
	}()
	waitFor(t, "the disable to park on the held account row", func() bool { return lockWaiters(t, env) >= 1 })

	demoted := make(chan error, 1)
	go func() {
		_, err := project.NewStore(pool).SetRole(t.Context(), audit.Meta{ActorUID: adminA.UID},
			project.BootstrapUID, adminA.UID, project.RoleEditor)
		demoted <- err
	}()
	var demoteErr error
	demoteDone := false
	waitFor(t, "the demotion to finish or queue", func() bool {
		select {
		case demoteErr = <-demoted:
			demoteDone = true
			return true
		default:
			return lockWaiters(t, env) >= 2
		}
	})

	if err := hold.Rollback(t.Context()); err != nil {
		t.Fatalf("releasing the account row: %v", err)
	}
	if err := <-disabled; err != nil {
		t.Fatalf("disabling admin B: %v", err)
	}
	if !demoteDone {
		demoteErr = <-demoted
	}

	var working int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM project_members m JOIN users u ON u.uid = m.user_uid
		WHERE m.project_uid = $1 AND m.role = 'admin' AND m.approved_at IS NOT NULL AND NOT u.disabled`,
		project.BootstrapUID).Scan(&working); err != nil {
		t.Fatalf("counting working admins: %v", err)
	}
	note := disableAudit(t, env, adminB.UID).Details["stranded_projects"]
	if working == 0 && note == nil {
		t.Fatalf("the project was left with no working admin and no stranded_projects note (demotion err %v)",
			demoteErr)
	}
	// The disable held the project first, so the demotion found B disabled.
	if !errors.Is(demoteErr, project.ErrLastAdmin) || working != 1 || note != nil {
		t.Errorf("demotion err = %v, working admins = %d, note = %v; want ErrLastAdmin, 1, none",
			demoteErr, working, note)
	}
}

// TestDisable_doesNotDeadlockAnInsertByTheAccount races a disable against a
// write by (or about) the same account into one of its projects — an audit row,
// a photo — whose foreign keys PostgreSQL checks with a KEY SHARE lock on the
// account and then on the project. The disable locks the account's projects and
// then the account. Were the projects locked FOR UPDATE (which KEY SHARE waits
// for), the two would wait on each other and one of them would be aborted as a
// deadlock, a 500 for somebody; locked FOR NO KEY UPDATE, the foreign-key check
// passes and the disable merely waits for the write to commit.
//
// The interleaving is forced: the test takes the account's KEY SHARE first,
// waits for the disable to park on the account row, then asks for the
// project's.
func TestDisable_doesNotDeadlockAnInsertByTheAccount(t *testing.T) {
	env := newTestEnv(t)
	maint := newMaintainerOutsideProjects(t, env)
	editor := env.createUser(t, "editor-fk", auth.AccountEditor)
	pool := env.db.Pool()

	insert, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = insert.Rollback(context.WithoutCancel(t.Context())) }()
	if _, err := insert.Exec(t.Context(), `SET LOCAL lock_timeout = '5s'`); err != nil {
		t.Fatalf("lock timeout: %v", err)
	}
	if _, err := insert.Exec(t.Context(), `SELECT uid FROM users WHERE uid = $1 FOR KEY SHARE`, editor.UID); err != nil {
		t.Fatalf("the account's foreign-key lock: %v", err)
	}

	disabled := make(chan error, 1)
	go func() {
		entry := audit.Meta{ActorUID: maint.UID}.Entry(audit.ActionUserDisable, "users", editor.UID, nil)
		_, err := env.svc.SetUserDisabledAudited(t.Context(), editor.UID, true, maint.Role, entry)
		disabled <- err
	}()
	waitFor(t, "the disable to park on the account row", func() bool { return lockWaiters(t, env) >= 1 })

	if _, err := insert.Exec(t.Context(), `SELECT uid FROM projects WHERE uid = $1 FOR KEY SHARE`,
		project.BootstrapUID); err != nil {
		t.Fatalf("the project's foreign-key lock behind a disable: %v", err)
	}
	if err := insert.Commit(t.Context()); err != nil {
		t.Fatalf("committing the write: %v", err)
	}
	if err := <-disabled; err != nil {
		t.Fatalf("disabling the account: %v", err)
	}
}

// waitFor polls cond until it holds, failing the test after five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
