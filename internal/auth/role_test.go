package auth

import "testing"

// TestRole_Valid verifies role validity classification.
func TestRole_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role Role
		want bool
	}{
		{RoleAdmin, true},
		{RoleEditor, true},
		{RoleViewer, true},
		{Role(""), false},
		{Role("root"), false},
		{Role("Admin"), false},
	}
	for _, tt := range tests {
		if got := tt.role.Valid(); got != tt.want {
			t.Errorf("Role(%q).Valid() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

// TestRole_CanWriteAndIsAdmin verifies the privilege helpers per role.
func TestRole_CanWriteAndIsAdmin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role        Role
		wantWrite   bool
		wantIsAdmin bool
	}{
		{RoleAdmin, true, true},
		{RoleEditor, true, false},
		{RoleViewer, false, false},
		{Role("bogus"), false, false},
	}
	for _, tt := range tests {
		if got := tt.role.CanWrite(); got != tt.wantWrite {
			t.Errorf("Role(%q).CanWrite() = %v, want %v", tt.role, got, tt.wantWrite)
		}
		if got := tt.role.IsAdmin(); got != tt.wantIsAdmin {
			t.Errorf("Role(%q).IsAdmin() = %v, want %v", tt.role, got, tt.wantIsAdmin)
		}
	}
}

// TestAuthorize verifies the RBAC decision matrix across roles and requirements.
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
		{"editor satisfies write", RoleEditor, requireWrite, true},
		{"editor blocked from admin", RoleEditor, requireAdmin, false},
		{"admin satisfies write", RoleAdmin, requireWrite, true},
		{"admin satisfies admin", RoleAdmin, requireAdmin, true},
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
