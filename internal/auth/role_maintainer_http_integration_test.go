//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
)

// TestHTTP_maintainerRoleAssignable proves the 0036 migration applied: a
// maintainer-role user can be created, so the users.role CHECK constraint admits
// 'maintainer'. A failure here means either the migration did not run against the
// test database or Role.Valid still rejects the role.
func TestHTTP_maintainerRoleAssignable(t *testing.T) {
	env := newTokenEnv(t, 50)
	user := env.user(t, "ops", auth.RoleMaintainer)
	if user.Role != auth.RoleMaintainer {
		t.Fatalf("created role = %q, want %q", user.Role, auth.RoleMaintainer)
	}
}

// TestHTTP_maintainerBoundary asserts the maintainer role's permission boundary
// end-to-end, through the real auth service and database, over both credential
// kinds: a Bearer API token (an automation account's intended mode) and a session
// cookie. A maintainer sits at the top of the ladder, so every guard — write,
// admin and import — admits it.
func TestHTTP_maintainerBoundary(t *testing.T) {
	env := newTokenEnv(t, 50)
	env.user(t, "ops-agent", auth.RoleMaintainer)
	opsUser := env.user(t, "ops-token-holder", auth.RoleMaintainer)
	_, opsToken := env.mintToken(t, opsUser.UID, "agent cli", nil)

	// Bearer token: an automation account's intended credential.
	t.Run("maintainer bearer token", func(t *testing.T) {
		cases := []struct {
			name   string
			method string
			path   string
			want   int
		}{
			{"write allowed", http.MethodGet, "/api/v1/probe/write", http.StatusOK},
			{"admin allowed", http.MethodGet, "/api/v1/probe/admin", http.StatusOK},
			{"import allowed", http.MethodGet, "/api/v1/probe/import", http.StatusOK},
			{"list users allowed", http.MethodGet, "/api/v1/admin/users", http.StatusOK},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if status, data := env.request(t, tc.method, tc.path, opsToken, ""); status != tc.want {
					t.Errorf("%s %s status = %d, want %d (body %s)", tc.method, tc.path, status, tc.want, data)
				}
			})
		}
	})

	// Session cookie: a maintainer logged in through the browser flow must hit the
	// same boundary as the token.
	t.Run("maintainer session cookie", func(t *testing.T) {
		cookie := env.login(t, "ops-agent")
		for _, path := range []string{
			"/api/v1/probe/write", "/api/v1/probe/admin", "/api/v1/probe/import",
		} {
			t.Run(path, func(t *testing.T) {
				if status, data := env.cookieRequest(t, http.MethodGet, path, cookie, ""); status != http.StatusOK {
					t.Errorf("GET %s status = %d, want 200 (body %s)", path, status, data)
				}
			})
		}
	})
}

// TestHTTP_importGuardRoleMatrix pins the import guard's decision across all four
// roles, proving import is now an operations capability reserved to maintainers —
// even a plain admin is refused, and editors and viewers stay refused.
func TestHTTP_importGuardRoleMatrix(t *testing.T) {
	env := newTokenEnv(t, 50)
	tests := []struct {
		role auth.Role
		user string
		want int
	}{
		{auth.RoleMaintainer, "imp-maint", http.StatusOK},
		{auth.RoleAdmin, "imp-admin", http.StatusForbidden},
		{auth.RoleEditor, "imp-editor", http.StatusForbidden},
		{auth.RoleViewer, "imp-viewer", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			u := env.user(t, tt.user, tt.role)
			_, token := env.mintToken(t, u.UID, "cli", nil)
			if status, data := env.request(t, http.MethodGet, "/api/v1/probe/import", token, ""); status != tt.want {
				t.Errorf("import probe for %s = %d, want %d (body %s)", tt.role, status, tt.want, data)
			}
		})
	}
}

// TestHTTP_adminGuardRoleMatrix pins the admin guard's decision across all four
// roles, proving the maintainer inherits admin (ladder inheritance) while editors
// and viewers stay refused.
func TestHTTP_adminGuardRoleMatrix(t *testing.T) {
	env := newTokenEnv(t, 50)
	tests := []struct {
		role auth.Role
		user string
		want int
	}{
		{auth.RoleMaintainer, "adm-maint", http.StatusOK},
		{auth.RoleAdmin, "adm-admin", http.StatusOK},
		{auth.RoleEditor, "adm-editor", http.StatusForbidden},
		{auth.RoleViewer, "adm-viewer", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			u := env.user(t, tt.user, tt.role)
			_, token := env.mintToken(t, u.UID, "cli", nil)
			if status, data := env.request(t, http.MethodGet, "/api/v1/probe/admin", token, ""); status != tt.want {
				t.Errorf("admin probe for %s = %d, want %d (body %s)", tt.role, status, tt.want, data)
			}
		})
	}
}
