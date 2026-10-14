//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// TestHTTP_maintainerRoleAssignable proves the 0036 migration applied: a
// maintainer-role user can be created, so the users.role CHECK constraint admits
// 'maintainer'. A failure here means either the migration did not run against the
// test database or Role.Valid still rejects the role.
func TestHTTP_maintainerRoleAssignable(t *testing.T) {
	env := newTokenEnv(t, 50)
	user := env.user(t, "ops", auth.AccountMaintainer)
	if user.Role != auth.RoleMaintainer {
		t.Fatalf("created instance role = %q, want %q", user.Role, auth.RoleMaintainer)
	}
	if got := bootstrapRole(t, env.db.Pool(), user.UID); got != project.RoleAdmin {
		t.Fatalf("created bootstrap role = %q, want %q", got, project.RoleAdmin)
	}
}

// TestHTTP_maintainerBoundary asserts the maintainer role's permission boundary
// end-to-end, through the real auth service and database, over both credential
// kinds: a Bearer API token (an automation account's intended mode) and a session
// cookie. A maintainer sits at the top of the ladder, so every guard — write,
// admin and import — admits it.
func TestHTTP_maintainerBoundary(t *testing.T) {
	env := newTokenEnv(t, 50)
	env.user(t, "ops-agent", auth.AccountMaintainer)
	opsUser := env.user(t, "ops-token-holder", auth.AccountMaintainer)
	_, opsToken := env.mintToken(t, opsUser, "agent cli", nil)

	// Bearer token: an automation account's intended credential.
	t.Run("maintainer bearer token", func(t *testing.T) {
		cases := []struct {
			name   string
			method string
			path   string
			want   int
		}{
			{"write allowed", http.MethodGet, apiV + "/probe/write", http.StatusOK},
			{"admin allowed", http.MethodGet, apiV + "/probe/admin", http.StatusOK},
			{"import allowed", http.MethodGet, apiV + "/probe/import", http.StatusOK},
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
			apiV + "/probe/write", apiV + "/probe/admin", apiV + "/probe/import",
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
		role auth.AccountRole
		user string
		want int
	}{
		{auth.AccountMaintainer, "imp-maint", http.StatusOK},
		{auth.AccountAdmin, "imp-admin", http.StatusForbidden},
		{auth.AccountEditor, "imp-editor", http.StatusForbidden},
		{auth.AccountViewer, "imp-viewer", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			u := env.user(t, tt.user, tt.role)
			_, token := env.mintToken(t, u, "cli", nil)
			if status, data := env.request(t, http.MethodGet, apiV+"/probe/import", token, ""); status != tt.want {
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
		role auth.AccountRole
		user string
		want int
	}{
		{auth.AccountMaintainer, "adm-maint", http.StatusOK},
		{auth.AccountAdmin, "adm-admin", http.StatusOK},
		{auth.AccountEditor, "adm-editor", http.StatusForbidden},
		{auth.AccountViewer, "adm-viewer", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			u := env.user(t, tt.user, tt.role)
			_, token := env.mintToken(t, u, "cli", nil)
			if status, data := env.request(t, http.MethodGet, apiV+"/probe/admin", token, ""); status != tt.want {
				t.Errorf("admin probe for %s = %d, want %d (body %s)", tt.role, status, tt.want, data)
			}
		})
	}
}

// TestHTTP_disableIsMaintainerOnly pins that blocking an account — which locks
// it out of every project of the instance — is the system administrator's
// call: a project admin is refused, a maintainer gets through.
func TestHTTP_disableIsMaintainerOnly(t *testing.T) {
	env := newTokenEnv(t, 50)
	target := env.user(t, "dis-target", auth.AccountViewer)
	tests := []struct {
		role auth.AccountRole
		user string
		want int
	}{
		{auth.AccountAdmin, "dis-admin", http.StatusForbidden},
		{auth.AccountMaintainer, "dis-maint", http.StatusOK},
	}
	for _, tt := range tests {
		u := env.user(t, tt.user, tt.role)
		_, token := env.mintToken(t, u, "cli", nil)
		path := "/api/v1/admin/users/" + target.UID + "/disable"
		if status, data := env.request(t, http.MethodPost, path, token, ""); status != tt.want {
			t.Errorf("disable by %s = %d, want %d (body %s)", tt.role, status, tt.want, data)
		}
	}
}

// TestHTTP_accountUpdateIsMaintainerOnly pins that the account edit — which
// carries the instance-wide disabled flag and the instance role — is the
// system administrator's: a project admin manages roles of their own members
// through /p/{project}/members instead, and is refused here before anything
// changes.
func TestHTTP_accountUpdateIsMaintainerOnly(t *testing.T) {
	env := newTokenEnv(t, 50)
	target := env.user(t, "upd-target", auth.AccountViewer)
	body := `{"display_name":"","email":"upd-target@example.test","instance_role":"user","disabled":false}`
	tests := []struct {
		role auth.AccountRole
		user string
		want int
	}{
		{auth.AccountAdmin, "upd-admin", http.StatusForbidden},
		{auth.AccountMaintainer, "upd-maint", http.StatusOK},
	}
	for _, tt := range tests {
		u := env.user(t, tt.user, tt.role)
		_, token := env.mintToken(t, u, "cli", nil)
		path := "/api/v1/admin/users/" + target.UID
		if status, data := env.request(t, http.MethodPatch, path, token, body); status != tt.want {
			t.Errorf("update by %s = %d, want %d (body %s)", tt.role, status, tt.want, data)
		}
	}
}
