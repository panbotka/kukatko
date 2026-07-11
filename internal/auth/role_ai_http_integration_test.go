//go:build integration

package auth_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
)

// TestHTTP_aiRoleAssignable proves the 0023 migration applied: an ai-role user
// can be created, so the users.role CHECK constraint admits 'ai'. A failure here
// means either the migration did not run against the test database or Role.Valid
// still rejects the role.
func TestHTTP_aiRoleAssignable(t *testing.T) {
	env := newTokenEnv(t, 50)
	user := env.user(t, "agent", auth.RoleAI)
	if user.Role != auth.RoleAI {
		t.Fatalf("created role = %q, want %q", user.Role, auth.RoleAI)
	}
}

// TestHTTP_aiRoleBoundary asserts the exact permission boundary of the ai role
// end-to-end, through the real auth service and database, over both credential
// kinds it can present: a Bearer API token (its intended mode) and a session
// cookie.
//
// The ai role holds an editor's write powers plus permission to trigger imports,
// and nothing else that is admin-gated. Every admin-only module (users, backups,
// jobs, maintenance, process backfills, audit, system) is mounted behind the same
// auth.RequireAdmin guard that /probe/admin exercises here, and the real
// /admin/users endpoints stand in for a concrete blocked area (user
// administration); a 403 on those is a 403 on all of them. /probe/import
// exercises auth.RequireImport, the one otherwise admin-gated action ai may reach.
func TestHTTP_aiRoleBoundary(t *testing.T) {
	env := newTokenEnv(t, 50)
	env.user(t, "ai-agent", auth.RoleAI)
	aiUser := env.user(t, "ai-token-holder", auth.RoleAI)
	_, aiToken := env.mintToken(t, aiUser.UID, "agent cli", nil)

	// Bearer token: the ai agent's intended credential.
	t.Run("ai bearer token", func(t *testing.T) {
		cases := []struct {
			name   string
			method string
			path   string
			body   string
			want   int
		}{
			{"write allowed", http.MethodGet, "/api/v1/probe/write", "", http.StatusOK},
			{"import allowed", http.MethodGet, "/api/v1/probe/import", "", http.StatusOK},
			{"admin guard blocked", http.MethodGet, "/api/v1/probe/admin", "", http.StatusForbidden},
			{"list users blocked", http.MethodGet, "/api/v1/admin/users", "", http.StatusForbidden},
			{"create user blocked", http.MethodPost, "/api/v1/admin/users",
				`{"username":"x","password":"password123","role":"viewer"}`, http.StatusForbidden},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if status, data := env.request(t, tc.method, tc.path, aiToken, tc.body); status != tc.want {
					t.Errorf("%s %s status = %d, want %d (body %s)", tc.method, tc.path, status, tc.want, data)
				}
			})
		}
	})

	// Session cookie: an ai principal logged in through the browser flow must hit
	// the same boundary as the token.
	t.Run("ai session cookie", func(t *testing.T) {
		cookie := env.login(t, "ai-agent")
		cases := []struct {
			name string
			path string
			want int
		}{
			{"write allowed", "/api/v1/probe/write", http.StatusOK},
			{"import allowed", "/api/v1/probe/import", http.StatusOK},
			{"admin guard blocked", "/api/v1/probe/admin", http.StatusForbidden},
			{"list users blocked", "/api/v1/admin/users", http.StatusForbidden},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if status, data := env.cookieRequest(t, http.MethodGet, tc.path, cookie, ""); status != tc.want {
					t.Errorf("GET %s status = %d, want %d (body %s)", tc.path, status, tc.want, data)
				}
			})
		}
	})
}

// TestHTTP_importGuardRoleMatrix pins the import guard's decision across all
// roles, proving import was opened to ai without being opened to editors or
// viewers and while staying available to admins.
func TestHTTP_importGuardRoleMatrix(t *testing.T) {
	env := newTokenEnv(t, 50)
	tests := []struct {
		role auth.Role
		user string
		want int
	}{
		{auth.RoleAdmin, "imp-admin", http.StatusOK},
		{auth.RoleAI, "imp-ai", http.StatusOK},
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
