//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
)

// TestHTTP_curatorRoleAssignable proves the 0085 migration applied: a
// curator-role user can be created, so the users.role CHECK constraint admits
// 'curator'. A failure here means either the migration did not run against the
// test database or Role.Valid still rejects the role.
func TestHTTP_curatorRoleAssignable(t *testing.T) {
	env := newTokenEnv(t, 50)
	user := env.user(t, "curator", auth.RoleCurator)
	if user.Role != auth.RoleCurator {
		t.Fatalf("created role = %q, want %q", user.Role, auth.RoleCurator)
	}
}

// TestHTTP_curatorGuardRoleMatrix pins every guard's decision across all five
// roles, end-to-end through the real service and database: the curate guard
// admits the curator and everyone above it, while the curator is refused by the
// write, admin and import guards exactly like a viewer.
func TestHTTP_curatorGuardRoleMatrix(t *testing.T) {
	env := newTokenEnv(t, 50)
	paths := []string{
		"/api/v1/probe/auth", "/api/v1/probe/curate", "/api/v1/probe/write",
		"/api/v1/probe/admin", "/api/v1/probe/import",
	}
	const ok, no = http.StatusOK, http.StatusForbidden
	tests := []struct {
		role auth.Role
		want [5]int // auth, curate, write, admin, import
	}{
		{auth.RoleViewer, [5]int{ok, no, no, no, no}},
		{auth.RoleCurator, [5]int{ok, ok, no, no, no}},
		{auth.RoleEditor, [5]int{ok, ok, ok, no, no}},
		{auth.RoleAdmin, [5]int{ok, ok, ok, ok, no}},
		{auth.RoleMaintainer, [5]int{ok, ok, ok, ok, ok}},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			u := env.user(t, "guard-"+string(tt.role), tt.role)
			_, token := env.mintToken(t, u, "cli", nil)
			for i, path := range paths {
				if status, data := env.request(t, http.MethodGet, path, token, ""); status != tt.want[i] {
					t.Errorf("GET %s for %s = %d, want %d (body %s)", path, tt.role, status, tt.want[i], data)
				}
			}
		})
	}
}
