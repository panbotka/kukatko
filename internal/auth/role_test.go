package auth

import (
	"errors"
	"testing"
)

// TestRole_Valid verifies role validity classification for the strict ladder
// viewer < curator < editor < admin < maintainer, and that the retired 'ai' role
// is rejected.
func TestRole_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role Role
		want bool
	}{
		{RoleViewer, true},
		{RoleCurator, true},
		{RoleEditor, true},
		{RoleAdmin, true},
		{RoleMaintainer, true},
		{Role("ai"), false},
		{Role(""), false},
		{Role("root"), false},
		{Role("Admin"), false},
		{Role("Maintainer"), false},
		{Role("Curator"), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("Role(%q).Valid() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

// TestRole_Predicates verifies the privilege helpers across the five roles on
// the ladder: curate is curator and up, write is editor and up, admin
// (governance) is admin and up, and both maintain (operations) and import
// require maintainer. A curator fails every predicate but CanCurate.
func TestRole_Predicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role            Role
		wantCurate      bool
		wantWrite       bool
		wantIsAdmin     bool
		wantCanMaintain bool
		wantCanImport   bool
	}{
		{RoleViewer, false, false, false, false, false},
		{RoleCurator, true, false, false, false, false},
		{RoleEditor, true, true, false, false, false},
		{RoleAdmin, true, true, true, false, false},
		{RoleMaintainer, true, true, true, true, true},
		{Role("bogus"), false, false, false, false, false},
	}
	for _, tt := range tests {
		if got := tt.role.CanCurate(); got != tt.wantCurate {
			t.Errorf("Role(%q).CanCurate() = %v, want %v", tt.role, got, tt.wantCurate)
		}
		if got := tt.role.CanWrite(); got != tt.wantWrite {
			t.Errorf("Role(%q).CanWrite() = %v, want %v", tt.role, got, tt.wantWrite)
		}
		if got := tt.role.IsAdmin(); got != tt.wantIsAdmin {
			t.Errorf("Role(%q).IsAdmin() = %v, want %v", tt.role, got, tt.wantIsAdmin)
		}
		if got := tt.role.CanMaintain(); got != tt.wantCanMaintain {
			t.Errorf("Role(%q).CanMaintain() = %v, want %v", tt.role, got, tt.wantCanMaintain)
		}
		if got := tt.role.CanImport(); got != tt.wantCanImport {
			t.Errorf("Role(%q).CanImport() = %v, want %v", tt.role, got, tt.wantCanImport)
		}
	}
}

// TestAuthorize pins the whole RBAC decision matrix: every role on the ladder
// against every requirement, one row per role. It proves the ladder inheritance
// (a maintainer satisfies requireAdmin, every writer satisfies requireCurate)
// and that the inserted curator rung satisfies requireCurate and nothing above
// it. Invalid roles satisfy nothing, not even requireAuth.
func TestAuthorize(t *testing.T) {
	t.Parallel()

	// want lists the decision for each requirement in the column order below.
	reqs := []struct {
		name string
		req  requirement
	}{
		{"auth", requireAuth},
		{"curate", requireCurate},
		{"write", requireWrite},
		{"admin", requireAdmin},
		{"maintain", requireMaintain},
		{"import", requireImport},
	}
	tests := []struct {
		role Role
		want [6]bool // auth, curate, write, admin, maintain, import
	}{
		{RoleViewer, [6]bool{true, false, false, false, false, false}},
		{RoleCurator, [6]bool{true, true, false, false, false, false}},
		{RoleEditor, [6]bool{true, true, true, false, false, false}},
		{RoleAdmin, [6]bool{true, true, true, true, false, false}},
		{RoleMaintainer, [6]bool{true, true, true, true, true, true}},
		{Role("ai"), [6]bool{}},
		{Role("x"), [6]bool{}},
	}
	for _, tt := range tests {
		for i, rq := range reqs {
			t.Run(string(tt.role)+"/"+rq.name, func(t *testing.T) {
				t.Parallel()
				if got := authorize(tt.role, rq.req); got != tt.want[i] {
					t.Errorf("authorize(%q, %s) = %v, want %v", tt.role, rq.name, got, tt.want[i])
				}
			})
		}
	}
}

// TestAuthorizeUserManagement verifies the maintainer boundary on user-management
// actions: only a maintainer may grant the maintainer role or touch an account
// that already holds it, while every other viewer/editor/admin action is allowed.
func TestAuthorizeUserManagement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		actor   Role
		current Role
		newRole Role
		wantErr error
	}{
		{"admin creates viewer", RoleAdmin, "", RoleViewer, nil},
		{"admin creates admin", RoleAdmin, "", RoleAdmin, nil},
		{"admin creates curator", RoleAdmin, "", RoleCurator, nil},
		{"admin promotes curator to editor", RoleAdmin, RoleCurator, RoleEditor, nil},
		{"admin promotes curator to maintainer", RoleAdmin, RoleCurator, RoleMaintainer, ErrMaintainerRequired},
		{"admin creates maintainer", RoleAdmin, "", RoleMaintainer, ErrMaintainerRequired},
		{"admin promotes editor to maintainer", RoleAdmin, RoleEditor, RoleMaintainer, ErrMaintainerRequired},
		{"admin modifies maintainer", RoleAdmin, RoleMaintainer, RoleMaintainer, ErrMaintainerRequired},
		{"admin disables maintainer (no role change)", RoleAdmin, RoleMaintainer, "", ErrMaintainerRequired},
		{"editor-actor creates maintainer", RoleEditor, "", RoleMaintainer, ErrMaintainerRequired},
		{"maintainer creates maintainer", RoleMaintainer, "", RoleMaintainer, nil},
		{"maintainer promotes editor to maintainer", RoleMaintainer, RoleEditor, RoleMaintainer, nil},
		{"maintainer modifies maintainer", RoleMaintainer, RoleMaintainer, RoleMaintainer, nil},
		{"admin demotes an admin to viewer", RoleAdmin, RoleAdmin, RoleViewer, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := authorizeUserManagement(tt.actor, tt.current, tt.newRole); !errors.Is(got, tt.wantErr) {
				t.Errorf("authorizeUserManagement(%q, %q, %q) = %v, want %v",
					tt.actor, tt.current, tt.newRole, got, tt.wantErr)
			}
		})
	}
}
