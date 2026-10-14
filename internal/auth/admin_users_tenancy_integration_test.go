//go:build integration

package auth_test

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// bootstrapMembership returns uid's membership of the bootstrap project, or nil
// when the account has none there. The account record no longer carries a
// project role, so a test reads it where it lives.
func bootstrapMembership(t *testing.T, pool *pgxpool.Pool, uid string) *project.Member {
	t.Helper()
	m, err := project.NewStore(pool).Membership(t.Context(), project.BootstrapUID, uid)
	if errors.Is(err, project.ErrNotMember) {
		return nil
	}
	if err != nil {
		t.Fatalf("Membership(%s): %v", uid, err)
	}
	return m
}

// bootstrapRole returns uid's role in the bootstrap project, or "" when the
// account is not a member there.
func bootstrapRole(t *testing.T, pool *pgxpool.Pool, uid string) project.Role {
	t.Helper()
	if m := bootstrapMembership(t, pool, uid); m != nil {
		return m.Role
	}
	return ""
}

// membershipCount returns how many memberships, in any project, uid holds.
func membershipCount(t *testing.T, pool *pgxpool.Pool, uid string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM project_members WHERE user_uid = $1`, uid).Scan(&n); err != nil {
		t.Fatalf("count memberships of %s: %v", uid, err)
	}
	return n
}

// assertNoProjectKeys fails the test when the account record user carries a
// project-shaped key: a role or a linked person exists only per membership.
func assertNoProjectKeys(t *testing.T, label string, user map[string]any) {
	t.Helper()
	for _, key := range []string{"role", "subject_uid"} {
		if _, has := user[key]; has {
			t.Errorf("%s carries %q: %v", label, key, user)
		}
	}
}

// TestAdminUsers_createMakesABareAccount pins POST /admin/users in the tenancy
// era: the account record and nothing project-shaped — the answer has the
// instance role and the note but no "role" and no "subject_uid", and no
// membership of any project is created.
func TestAdminUsers_createMakesABareAccount(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "root", auth.AccountMaintainer)
	admin := env.loginClient(t, "root")

	status, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", adminUserBody(t, map[string]any{
		"username": "bare", "password": testPassword, "email": "bare@example.test",
		"instance_role": "user", "note": "made by hand",
	}))
	if status != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d, want 201 (body %s)", status, data)
	}
	created := decodeUser(t, data)
	if created["instance_role"] != string(auth.RoleUser) {
		t.Errorf("instance_role = %v, want user", created["instance_role"])
	}
	assertNote(t, created, "made by hand")
	assertNoProjectKeys(t, "created account", created)

	uid, _ := created["uid"].(string)
	if n := membershipCount(t, env.db.Pool(), uid); n != 0 {
		t.Errorf("the created account holds %d memberships, want none", n)
	}
	// It is still a working sign-in: an account in no project waits for nobody.
	env.loginClient(t, "bare")
}

// TestAdminUsers_createInstanceRole pins the one role POST /admin/users takes:
// omitted or empty is a plain user, "maintainer" a system administrator, and a
// project role ("admin") or the retired five-rung "role" field is a 400 that
// creates nothing.
func TestAdminUsers_createInstanceRole(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "root", auth.AccountMaintainer)
	admin := env.loginClient(t, "root")

	tests := []struct {
		name       string
		username   string
		extra      map[string]any
		wantStatus int
		wantRole   auth.Role
	}{
		{"omitted", "omitted", nil, http.StatusCreated, auth.RoleUser},
		{"empty", "empty", map[string]any{"instance_role": ""}, http.StatusCreated, auth.RoleUser},
		{"maintainer", "maint", map[string]any{"instance_role": "maintainer"}, http.StatusCreated, auth.RoleMaintainer},
		{"project role", "projadmin", map[string]any{"instance_role": "admin"}, http.StatusBadRequest, ""},
		{"retired role field", "fiverung", map[string]any{"role": "viewer"}, http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := map[string]any{
				"username": tt.username, "password": testPassword, "email": tt.username + "@example.test",
			}
			maps.Copy(fields, tt.extra)
			status, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", adminUserBody(t, fields))
			if status != tt.wantStatus {
				t.Fatalf("POST /admin/users = %d, want %d (body %s)", status, tt.wantStatus, data)
			}
			stored, err := env.store.GetUserByUsername(t.Context(), tt.username)
			if tt.wantStatus != http.StatusCreated {
				if !errors.Is(err, auth.ErrUserNotFound) {
					t.Errorf("a refused create stored %+v (%v), want no account", stored, err)
				}
				return
			}
			if err != nil || stored.Role != tt.wantRole {
				t.Errorf("stored instance role = %q (%v), want %q", stored.Role, err, tt.wantRole)
			}
			if created := decodeUser(t, data); created["instance_role"] != string(tt.wantRole) {
				t.Errorf("answered instance_role = %v, want %q", created["instance_role"], tt.wantRole)
			}
		})
	}
	// The refusal of a project role says what the field takes.
	_, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", adminUserBody(t, map[string]any{
		"username": "again", "password": testPassword, "email": "again@example.test", "instance_role": "curator",
	}))
	if !strings.Contains(string(data), "invalid instance role") {
		t.Errorf("400 body %s does not name the instance role", data)
	}
}

// TestAdminUsers_updateLeavesTheMembership pins PATCH /admin/users/{uid}: it
// rewrites the account record only, so a curator of the bootstrap project who
// is linked to a person there keeps that role and that link through an update
// (even a promotion to maintainer), and the audit entry speaks the instance
// role, never a project role.
func TestAdminUsers_updateLeavesTheMembership(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "root", auth.AccountMaintainer)
	admin := env.loginClient(t, "root")
	if _, err := env.db.Pool().Exec(t.Context(),
		`INSERT INTO subjects (uid, slug, name, type, project_id) VALUES ('sub_cur', 'cur', 'Cur', 'person', $1)`,
		project.BootstrapUID); err != nil {
		t.Fatalf("seeding a subject: %v", err)
	}
	linked := "sub_cur"
	target, err := env.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "cura", Email: "cura@example.test", Password: testPassword,
		Role: auth.AccountCurator, SubjectUID: &linked,
	})
	if err != nil {
		t.Fatalf("seeding the curator: %v", err)
	}
	path := "/api/v1/admin/users/" + target.UID

	for _, role := range []auth.Role{auth.RoleUser, auth.RoleMaintainer} {
		status, data := env.do(t, admin, http.MethodPatch, path, adminUserBody(t, map[string]any{
			"display_name": "Cura " + string(role), "email": "cura@example.test",
			"instance_role": string(role), "disabled": false,
		}))
		if status != http.StatusOK {
			t.Fatalf("PATCH as %s = %d, want 200 (body %s)", role, status, data)
		}
		updated := decodeUser(t, data)
		if updated["instance_role"] != string(role) {
			t.Errorf("instance_role = %v, want %s", updated["instance_role"], role)
		}
		assertNoProjectKeys(t, "updated account", updated)
		m := bootstrapMembership(t, env.db.Pool(), target.UID)
		if m == nil || m.Role != project.RoleCurator || m.SubjectUID == nil || *m.SubjectUID != linked {
			t.Errorf("membership after PATCH as %s = %+v, want a curator linked to %s", role, m, linked)
		}
	}

	recs, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionUserUpdate, TargetUID: target.UID, Limit: 10})
	if err != nil || len(recs) != 2 {
		t.Fatalf("user.update audit = %+v (%v), want two entries", recs, err)
	}
	for _, rec := range recs {
		if rec.Details["instance_role"] == nil {
			t.Errorf("audit details %v carry no instance_role", rec.Details)
		}
		for _, key := range []string{"role", "subject_uid"} {
			if _, has := rec.Details[key]; has {
				t.Errorf("audit details %v carry %q", rec.Details, key)
			}
		}
	}
}

// TestAdminUsers_meOfABareAccount pins GET /auth/me for an account the system
// administrator created: the user object is the account record without a role
// or a linked person, the top-level instance role is there, and the list of
// projects is empty — there is no default project to fall back on.
func TestAdminUsers_meOfABareAccount(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "root", auth.AccountMaintainer)
	admin := env.loginClient(t, "root")
	if status, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", adminUserBody(t, map[string]any{
		"username": "loner", "password": testPassword, "email": "loner@example.test",
	})); status != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d (body %s)", status, data)
	}

	client := env.loginClient(t, "loner")
	status, data := env.do(t, client, http.MethodGet, "/api/v1/auth/me", "")
	if status != http.StatusOK {
		t.Fatalf("GET /auth/me = %d (body %s)", status, data)
	}
	var me struct {
		User         map[string]any   `json:"user"`
		InstanceRole string           `json:"instance_role"`
		Projects     []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(data, &me); err != nil {
		t.Fatalf("decoding /auth/me: %v (body %s)", err, data)
	}
	if me.InstanceRole != string(auth.RoleUser) || me.User["instance_role"] != string(auth.RoleUser) {
		t.Errorf("instance_role = %q / user.instance_role = %v, want user", me.InstanceRole, me.User["instance_role"])
	}
	assertNoProjectKeys(t, "/auth/me user", me.User)
	if len(me.Projects) != 0 {
		t.Errorf("projects = %v, want none", me.Projects)
	}
}

// TestAdminUsers_projectAdminIsRefusedEverywhere pins that every /admin/users
// route is the system administrator's: an admin of the bootstrap project (a
// plain user of the instance) gets 403 on each, and none of them changes the
// target account.
func TestAdminUsers_projectAdminIsRefusedEverywhere(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "padmin", auth.AccountAdmin)
	target := env.mustCreate(t, "victim", auth.AccountViewer)
	client := env.loginClient(t, "padmin")
	base := "/api/v1/admin/users"
	user := base + "/" + target.UID

	routes := []struct{ method, path, body string }{
		{http.MethodGet, base, ""},
		{http.MethodPost, base, `{"username":"sneaky","password":"` + testPassword + `","email":"sneaky@example.test"}`},
		{http.MethodPatch, user, `{"display_name":"pwned","email":"victim@example.test","instance_role":"maintainer"}`},
		{http.MethodPut, user + "/username", `{"username":"renamed"}`},
		{http.MethodPost, user + "/disable", ""},
		{http.MethodPost, user + "/password", `{"new_password":"another-strong-pass"}`},
		{http.MethodPost, user + "/password-reset", ""},
	}
	for _, rt := range routes {
		assertStatus(t, env, client, rt.method, rt.path, rt.body, http.StatusForbidden)
	}

	after, err := env.store.GetUserByUID(t.Context(), target.UID)
	if err != nil {
		t.Fatalf("reading the target: %v", err)
	}
	if after.Username != "victim" || after.DisplayName != "" || after.Disabled || after.Role != auth.RoleUser {
		t.Errorf("target after refused calls = %+v, want it untouched", after)
	}
	if _, _, err := env.svc.Login(t.Context(), "victim", testPassword); err != nil {
		t.Errorf("the target's password changed: %v", err)
	}
	if _, err := env.store.GetUserByUsername(t.Context(), "sneaky"); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("a refused create left an account behind (%v)", err)
	}
}
