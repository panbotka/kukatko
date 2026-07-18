package auth

import (
	"errors"
	"testing"
)

// TestRole_Valid verifies role validity classification for the strict ladder
// viewer < editor < admin < maintainer, and that the retired 'ai' role is rejected.
func TestRole_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role Role
		want bool
	}{
		{RoleViewer, true},
		{RoleEditor, true},
		{RoleAdmin, true},
		{RoleMaintainer, true},
		{Role("ai"), false},
		{Role(""), false},
		{Role("root"), false},
		{Role("Admin"), false},
		{Role("Maintainer"), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("Role(%q).Valid() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

// TestRole_Predicates verifies the privilege helpers across the four roles on the
// ladder: write is editor and up, admin (governance) is admin and up, and both
// maintain (operations) and import require maintainer.
func TestRole_Predicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role            Role
		wantWrite       bool
		wantIsAdmin     bool
		wantCanMaintain bool
		wantCanImport   bool
	}{
		{RoleViewer, false, false, false, false},
		{RoleEditor, true, false, false, false},
		{RoleAdmin, true, true, false, false},
		{RoleMaintainer, true, true, true, true},
		{Role("bogus"), false, false, false, false},
	}
	for _, tt := range tests {
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

// TestAuthorize verifies the RBAC decision matrix across roles and requirements,
// including that requireAdmin is satisfied by a maintainer (ladder inheritance)
// and that requireMaintain and requireImport admit only the maintainer.
func TestAuthorize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role Role
		req  requirement
		want bool
	}{
		{"viewer satisfies auth", RoleViewer, requireAuth, true},
		{"viewer blocked from write", RoleViewer, requireWrite, false},
		{"viewer blocked from admin", RoleViewer, requireAdmin, false},
		{"viewer blocked from maintain", RoleViewer, requireMaintain, false},
		{"viewer blocked from import", RoleViewer, requireImport, false},
		{"editor satisfies write", RoleEditor, requireWrite, true},
		{"editor blocked from admin", RoleEditor, requireAdmin, false},
		{"editor blocked from maintain", RoleEditor, requireMaintain, false},
		{"editor blocked from import", RoleEditor, requireImport, false},
		{"admin satisfies write", RoleAdmin, requireWrite, true},
		{"admin satisfies admin", RoleAdmin, requireAdmin, true},
		{"admin blocked from maintain", RoleAdmin, requireMaintain, false},
		{"admin blocked from import", RoleAdmin, requireImport, false},
		{"maintainer satisfies auth", RoleMaintainer, requireAuth, true},
		{"maintainer satisfies write", RoleMaintainer, requireWrite, true},
		{"maintainer satisfies admin", RoleMaintainer, requireAdmin, true},
		{"maintainer satisfies maintain", RoleMaintainer, requireMaintain, true},
		{"maintainer satisfies import", RoleMaintainer, requireImport, true},
		{"ai role satisfies nothing", Role("ai"), requireAuth, false},
		{"invalid role satisfies nothing", Role("x"), requireAuth, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := authorize(tt.role, tt.req); got != tt.want {
				t.Errorf("authorize(%q, %v) = %v, want %v", tt.role, tt.req, got, tt.want)
			}
		})
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
