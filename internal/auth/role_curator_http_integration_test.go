//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// TestHTTP_curatorRoleAssignable proves the 0085 migration applied: a
// curator-role user can be created, so the users.role CHECK constraint admits
// 'curator'. A failure here means either the migration did not run against the
// test database or Role.Valid still rejects the role.
func TestHTTP_curatorRoleAssignable(t *testing.T) {
	env := newTokenEnv(t, 50)
	user := env.user(t, "curator", auth.AccountCurator)
	if user.Role != auth.RoleUser {
		t.Fatalf("created instance role = %q, want %q", user.Role, auth.RoleUser)
	}
	if got := bootstrapRole(t, env.db.Pool(), user.UID); got != project.RoleCurator {
		t.Fatalf("created bootstrap role = %q, want %q", got, project.RoleCurator)
	}
}

// TestHTTP_curatorGuardRoleMatrix pins every guard's decision across all five
// roles, end-to-end through the real service and database: the curate guard
// admits the curator and everyone above it, while the curator is refused by the
// write, admin and import guards exactly like a viewer.
func TestHTTP_curatorGuardRoleMatrix(t *testing.T) {
	env := newTokenEnv(t, 50)
	paths := []string{
		apiV + "/probe/auth", apiV + "/probe/curate", apiV + "/probe/write",
		apiV + "/probe/admin", apiV + "/probe/import",
	}
	const ok, no = http.StatusOK, http.StatusForbidden
	tests := []struct {
		role auth.AccountRole
		want [5]int // auth, curate, write, admin, import
	}{
		{auth.AccountViewer, [5]int{ok, no, no, no, no}},
		{auth.AccountCurator, [5]int{ok, ok, no, no, no}},
		{auth.AccountEditor, [5]int{ok, ok, ok, no, no}},
		{auth.AccountAdmin, [5]int{ok, ok, ok, ok, no}},
		{auth.AccountMaintainer, [5]int{ok, ok, ok, ok, ok}},
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
