// Package auth implements Kukátko's authentication and authorization: local
// users with bcrypt passwords, opaque-token sessions with sliding expiry,
// login rate limiting, and role-based access control (admin / editor / viewer).
//
// The package is layered: pure helpers (Role, password hashing, UID and token
// generation, the rate limiter) carry no I/O and are unit-tested; Store wraps
// the database; Service orchestrates the domain logic; Handlers and the RBAC
// middleware expose it over HTTP. External state lives in PostgreSQL via the
// shared pgx pool.
package auth

// Role is a user's access level. Read/write/admin privilege grows viewer <
// editor < admin, with one role off that ladder: ai, an automated agent that
// authenticates by API token and holds an editor's write powers plus the
// ability to trigger imports, but no other administrative capability.
type Role string

const (
	// RoleAdmin can do everything, including user management.
	RoleAdmin Role = "admin"
	// RoleEditor has read and write access to media and metadata.
	RoleEditor Role = "editor"
	// RoleViewer has read-only access.
	RoleViewer Role = "viewer"
	// RoleAI is an automated agent (API token) with an editor's write access
	// plus permission to trigger imports; it is not an administrator.
	RoleAI Role = "ai"
)

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleEditor, RoleViewer, RoleAI:
		return true
	default:
		return false
	}
}

// CanWrite reports whether r is permitted to create or modify content. Editors,
// admins and the ai agent can write; viewers cannot.
func (r Role) CanWrite() bool {
	return r == RoleEditor || r == RoleAdmin || r == RoleAI
}

// IsAdmin reports whether r has administrative privileges (user management,
// backups, jobs, maintenance, and every other admin-gated surface). Only admin
// qualifies; the ai agent does not.
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

// CanImport reports whether r may trigger the read-only import and migration
// runs. Admins and the ai agent can; editors and viewers cannot. Import is the
// one otherwise admin-gated action the ai role is permitted to reach.
func (r Role) CanImport() bool {
	return r == RoleAdmin || r == RoleAI
}

// requirement is the access level a request must satisfy, used by the RBAC
// middleware to make an authorization decision from a user's Role.
type requirement int

const (
	// requireAuth only needs an authenticated user of any role.
	requireAuth requirement = iota
	// requireWrite needs a role that CanWrite (editor, admin or ai).
	requireWrite
	// requireAdmin needs the admin role.
	requireAdmin
	// requireImport needs a role that CanImport (admin or ai).
	requireImport
)

// authorize reports whether role satisfies req. It is the pure decision behind
// the RequireAuth / RequireWrite / RequireAdmin / RequireImport middlewares; an
// invalid role never satisfies any requirement.
func authorize(role Role, req requirement) bool {
	if !role.Valid() {
		return false
	}
	switch req {
	case requireAuth:
		return true
	case requireWrite:
		return role.CanWrite()
	case requireAdmin:
		return role.IsAdmin()
	case requireImport:
		return role.CanImport()
	default:
		return false
	}
}
