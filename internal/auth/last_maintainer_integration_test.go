//go:build integration

package auth_test

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// The last-maintainer guard: an instance that has an enabled maintainer must keep
// one. Losing the role entirely is irreversible through the API — granting
// `maintainer` is itself maintainer-only and there is no delete-user endpoint to
// let Bootstrap run again — so every operations surface would need database
// surgery to come back. These tests drive the real service and database.
//
// Every account costs a bcrypt hash, so the cases below share accounts wherever
// the setup allows it rather than seeding a fresh pair per assertion. The
// integration build mints those hashes at bcrypt.MinCost (password_cost_integration.go),
// which is what keeps this package's runtime in seconds rather than minutes.

// assertEnabledMaintainers fails the test unless the store counts want enabled
// maintainer accounts, proving a refusal actually rolled the change back rather
// than merely reporting an error.
func assertEnabledMaintainers(t *testing.T, env *testEnv, want int) {
	t.Helper()
	got, err := env.store.CountEnabledMaintainers(t.Context())
	if err != nil {
		t.Fatalf("CountEnabledMaintainers: %v", err)
	}
	if got != want {
		t.Fatalf("enabled maintainers = %d, want %d", got, want)
	}
}

// assertStillEnabledMaintainer fails the test unless uid is an enabled maintainer,
// which is how a rolled-back refusal looks from the account's own row.
func assertStillEnabledMaintainer(t *testing.T, env *testEnv, uid string) {
	t.Helper()
	user, err := env.store.GetUserByUID(t.Context(), uid)
	if err != nil {
		t.Fatalf("GetUserByUID: %v", err)
	}
	if user.Role != auth.RoleMaintainer || user.Disabled {
		t.Fatalf("account = role %q disabled %v, want maintainer/false", user.Role, user.Disabled)
	}
}

// TestLastMaintainer_soloCannotStepDown is the self-lockout in every shape it
// comes in: the sole maintainer demoting themselves, disabling themselves over
// the dedicated path, or disabling themselves as part of a profile update —
// audited or not. All four are refused and leave the account untouched.
func TestLastMaintainer_soloCannotStepDown(t *testing.T) {
	env := newTestEnv(t)
	solo := env.createUser(t, "solo-maint", auth.RoleMaintainer)

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "audited demote",
			call: func() error {
				_, err := env.svc.UpdateUserAudited(t.Context(), solo.UID,
					auth.UpdateUserInput{Email: solo.Email, Role: auth.RoleAdmin}, solo.Role,
					mgmtEntry(solo.UID, audit.ActionUserUpdate))
				return err
			},
		},
		{
			name: "audited disable",
			call: func() error {
				_, err := env.svc.SetUserDisabledAudited(t.Context(), solo.UID, true, solo.Role,
					mgmtEntry(solo.UID, audit.ActionUserDisable))
				return err
			},
		},
		{
			name: "plain disable",
			call: func() error {
				_, err := env.svc.SetUserDisabled(t.Context(), solo.UID, true)
				return err
			},
		},
		{
			// Keeping the role but flipping the flag loses the capability just the
			// same, so the update path is guarded on `disabled` too, not only on `role`.
			name: "plain update that only disables",
			call: func() error {
				_, err := env.svc.UpdateUser(t.Context(), solo.UID, auth.UpdateUserInput{
					Email: solo.Email, Role: auth.RoleMaintainer, Disabled: true,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, auth.ErrLastMaintainer) {
				t.Fatalf("err = %v, want ErrLastMaintainer", err)
			}
			assertEnabledMaintainers(t, env, 1)
			assertStillEnabledMaintainer(t, env, solo.UID)
		})
	}

	// The audited refusals rolled their transactions back whole: no user.update or
	// user.disable row is left behind claiming a change that never happened.
	var entries int
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM audit_log WHERE target_uid = $1`, solo.UID).Scan(&entries); err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	if entries != 0 {
		t.Errorf("refused changes wrote %d audit entries, want 0", entries)
	}
}

// TestLastMaintainer_guardsOnlyTheLastOne asserts the guard blocks nothing but
// the final step: with two enabled maintainers either may step down, the survivor
// then cannot, and ordinary accounts are never affected either way.
func TestLastMaintainer_guardsOnlyTheLastOne(t *testing.T) {
	env := newTestEnv(t)
	first := env.createUser(t, "maint-one", auth.RoleMaintainer)
	second := env.createUser(t, "maint-two", auth.RoleMaintainer)

	// Two enabled maintainers: demoting one is a normal, allowed change.
	demoted, err := env.svc.UpdateUserAudited(t.Context(), first.UID,
		auth.UpdateUserInput{Email: first.Email, Role: auth.RoleAdmin}, second.Role,
		mgmtEntry(second.UID, audit.ActionUserUpdate))
	if err != nil {
		t.Fatalf("demote with a second maintainer present: %v", err)
	}
	if demoted.Role != auth.RoleAdmin {
		t.Fatalf("demoted role = %q, want admin", demoted.Role)
	}
	assertEnabledMaintainers(t, env, 1)

	// An account that never held the role is untouched by the guard.
	if _, err := env.svc.SetUserDisabled(t.Context(), first.UID, true); err != nil {
		t.Fatalf("disable an ordinary account while a maintainer exists: %v", err)
	}

	// The survivor is now the last one, so the very same demote is refused.
	if _, err := env.svc.UpdateUserAudited(t.Context(), second.UID,
		auth.UpdateUserInput{Email: second.Email, Role: auth.RoleAdmin}, second.Role,
		mgmtEntry(second.UID, audit.ActionUserUpdate)); !errors.Is(err, auth.ErrLastMaintainer) {
		t.Fatalf("demote of the survivor err = %v, want ErrLastMaintainer", err)
	}
	assertEnabledMaintainers(t, env, 1)
	assertStillEnabledMaintainer(t, env, second.UID)
}

// TestLastMaintainer_disabledMaintainerDoesNotCount asserts the invariant is
// about *enabled* maintainers: a disabled one cannot log in, so it cannot stand
// in for the account being taken away — and re-enabling it lifts the refusal.
func TestLastMaintainer_disabledMaintainerDoesNotCount(t *testing.T) {
	env := newTestEnv(t)
	spare := env.createUser(t, "maint-spare", auth.RoleMaintainer)
	active := env.createUser(t, "maint-active", auth.RoleMaintainer)

	// Park the spare: allowed, because the active one remains.
	if _, err := env.svc.SetUserDisabledAudited(t.Context(), spare.UID, true, active.Role,
		mgmtEntry(active.UID, audit.ActionUserDisable)); err != nil {
		t.Fatalf("disable the spare maintainer: %v", err)
	}
	assertEnabledMaintainers(t, env, 1)

	// The disabled spare does not keep the instance alive, so the active one stays.
	if _, err := env.svc.SetUserDisabledAudited(t.Context(), active.UID, true, active.Role,
		mgmtEntry(active.UID, audit.ActionUserDisable)); !errors.Is(err, auth.ErrLastMaintainer) {
		t.Fatalf("disable of the last enabled maintainer err = %v, want ErrLastMaintainer", err)
	}
	assertEnabledMaintainers(t, env, 1)

	// Re-enabling the spare raises the count, and the active one may then step down.
	if _, err := env.svc.UpdateUserAudited(t.Context(), spare.UID, auth.UpdateUserInput{
		Email: spare.Email, Role: auth.RoleMaintainer, Disabled: false,
	}, active.Role, mgmtEntry(active.UID, audit.ActionUserUpdate)); err != nil {
		t.Fatalf("re-enable the spare maintainer: %v", err)
	}
	if _, err := env.svc.SetUserDisabledAudited(t.Context(), active.UID, true, active.Role,
		mgmtEntry(active.UID, audit.ActionUserDisable)); err != nil {
		t.Fatalf("disable the active maintainer once the spare is back: %v", err)
	}
	assertEnabledMaintainers(t, env, 1)
}

// TestLastMaintainer_maintainerlessInstanceStaysEditable asserts the guard
// forbids *dropping* to zero enabled maintainers, never being there: an instance
// that has none (a database seeded without one, a bootstrap that never ran) must
// stay fully manageable, or the guard would be a lockout of its own.
func TestLastMaintainer_maintainerlessInstanceStaysEditable(t *testing.T) {
	env := newTestEnv(t)
	admin := env.createUser(t, "admin", auth.RoleAdmin)
	assertEnabledMaintainers(t, env, 0)

	if _, err := env.svc.SetUserDisabled(t.Context(), admin.UID, true); err != nil {
		t.Fatalf("disable the only account of a maintainer-less instance: %v", err)
	}
	assertEnabledMaintainers(t, env, 0)
}

// TestHTTP_lastMaintainerGuard drives the guard through the real admin endpoints:
// the sole maintainer's own PATCH demote and POST disable both come back 409 with
// a message the UI can show — and once a second maintainer exists, the identical
// demote succeeds. That last step is the whole point of answering 409 rather than
// 403: the caller was never the problem, the instance's state was.
func TestHTTP_lastMaintainerGuard(t *testing.T) {
	env := newHTTPEnv(t, 10)
	maint := env.mustCreate(t, "solo-maint", auth.RoleMaintainer)
	successor := env.mustCreate(t, "successor", auth.RoleAdmin)
	client := newClient(t)

	if status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("solo-maint", testPassword)); status != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", status, body)
	}

	const demote = `{"display_name":"","email":"solo-maint@example.test","role":"admin","disabled":false}`
	status, body := env.do(t, client, http.MethodPatch, "/api/v1/admin/users/"+maint.UID, demote)
	if status != http.StatusConflict {
		t.Fatalf("PATCH self-demote status = %d, want 409 (body %s)", status, body)
	}
	assertErrorMentionsMaintainer(t, body)

	status, body = env.do(t, client, http.MethodPost, "/api/v1/admin/users/"+maint.UID+"/disable", "")
	if status != http.StatusConflict {
		t.Fatalf("POST self-disable status = %d, want 409 (body %s)", status, body)
	}
	assertErrorMentionsMaintainer(t, body)

	// Still a maintainer, so an admin surface is still reachable.
	if status, body := env.do(t, client, http.MethodGet, "/api/v1/admin/users", ""); status != http.StatusOK {
		t.Fatalf("GET /admin/users after refused changes = %d, want 200 (body %s)", status, body)
	}

	// Hand the instance over, and the refused request goes through unchanged.
	const promote = `{"display_name":"","email":"successor@example.test","role":"maintainer","disabled":false}`
	if status, body := env.do(t, client, http.MethodPatch,
		"/api/v1/admin/users/"+successor.UID, promote); status != http.StatusOK {
		t.Fatalf("PATCH promote successor status = %d, want 200 (body %s)", status, body)
	}
	if status, body := env.do(t, client, http.MethodPatch,
		"/api/v1/admin/users/"+maint.UID, demote); status != http.StatusOK {
		t.Fatalf("PATCH self-demote after handover status = %d, want 200 (body %s)", status, body)
	}
}

// assertErrorMentionsMaintainer checks the 409 body carries the dedicated
// last-maintainer message rather than a generic failure, since the UI keys its
// explanation off it.
func assertErrorMentionsMaintainer(t *testing.T, body []byte) {
	t.Helper()
	if !bytes.Contains(bytes.ToLower(body), []byte("last maintainer")) {
		t.Errorf("error body %s does not name the last-maintainer rule", body)
	}
}
