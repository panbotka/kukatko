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

// Role is a user's access level. Roles are totally ordered by privilege:
// viewer < editor < admin.
type Role string

const (
	// RoleAdmin can do everything, including user management.
	RoleAdmin Role = "admin"
	// RoleEditor has read and write access to media and metadata.
	RoleEditor Role = "editor"
	// RoleViewer has read-only access.
	RoleViewer Role = "viewer"
)

// Valid reports whether r is one of the three known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleEditor, RoleViewer:
		return true
	default:
		return false
	}
}

// CanWrite reports whether r is permitted to create or modify content. Editors
// and admins can write; viewers cannot.
func (r Role) CanWrite() bool {
	return r == RoleEditor || r == RoleAdmin
}

// IsAdmin reports whether r has administrative privileges (user management).
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

// requirement is the access level a request must satisfy, used by the RBAC
// middleware to make an authorization decision from a user's Role.
type requirement int

const (
	// requireAuth only needs an authenticated user of any role.
	requireAuth requirement = iota
	// requireWrite needs a role that CanWrite (editor or admin).
	requireWrite
	// requireAdmin needs the admin role.
	requireAdmin
)

// authorize reports whether role satisfies req. It is the pure decision behind
// the RequireAuth / RequireWrite / RequireAdmin middlewares; an invalid role
// never satisfies any requirement.
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
	default:
		return false
	}
}
