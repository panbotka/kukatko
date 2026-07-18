// Package auth implements Kukátko's authentication and authorization: local
// users with bcrypt passwords, opaque-token sessions with sliding expiry,
// login rate limiting, and role-based access control (viewer / editor / admin /
// maintainer).
//
// The package is layered: pure helpers (Role, password hashing, UID and token
// generation, the rate limiter) carry no I/O and are unit-tested; Store wraps
// the database; Service orchestrates the domain logic; Handlers and the RBAC
// middleware expose it over HTTP. External state lives in PostgreSQL via the
// shared pgx pool.
package auth

// Role is a user's access level on a strict ladder: viewer < editor < admin <
// maintainer. Each role inherits every permission of the roles below it. viewer
// is read-only; editor adds write access to media and metadata; admin adds
// governance (user management, audit log, emptying/purging trash); maintainer
// adds operations (imports, maintenance, system status, backup, restore, jobs,
// processing backfills) and is the most powerful role.
type Role string

const (
	// RoleMaintainer sits at the top of the ladder: an admin plus operations —
	// imports, maintenance, system status, backup, restore, jobs and processing
	// backfills. Only a maintainer may grant or alter the maintainer role.
	RoleMaintainer Role = "maintainer"
	// RoleAdmin is an editor plus governance: user management, the audit log, and
	// emptying or purging the trash.
	RoleAdmin Role = "admin"
	// RoleEditor is a viewer plus write access to media and metadata, sorting and
	// review, and album/label management.
	RoleEditor Role = "editor"
	// RoleViewer has read-only access to photos, albums and labels.
	RoleViewer Role = "viewer"
)

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleViewer, RoleEditor, RoleAdmin, RoleMaintainer:
		return true
	default:
		return false
	}
}

// CanWrite reports whether r is permitted to create or modify content. Editors,
// admins and maintainers can write; viewers cannot.
func (r Role) CanWrite() bool {
	return r == RoleEditor || r == RoleAdmin || r == RoleMaintainer
}

// IsAdmin reports whether r holds the governance privileges (user management,
// the audit log, emptying/purging trash). Admins and maintainers qualify — the
// ladder gives a maintainer every admin power — while editors and viewers do not.
func (r Role) IsAdmin() bool {
	return r == RoleAdmin || r == RoleMaintainer
}

// CanMaintain reports whether r holds the operations privileges at the top of
// the ladder: imports, maintenance, system status, backup, restore, jobs and
// processing backfills. Only a maintainer qualifies.
func (r Role) CanMaintain() bool {
	return r == RoleMaintainer
}

// CanImport reports whether r may trigger the read-only import and migration
// runs. Import is an operations capability, so only a maintainer qualifies;
// admins, editors and viewers cannot.
func (r Role) CanImport() bool {
	return r.CanMaintain()
}

// requirement is the access level a request must satisfy, used by the RBAC
// middleware to make an authorization decision from a user's Role.
type requirement int

const (
	// requireAuth only needs an authenticated user of any role.
	requireAuth requirement = iota
	// requireWrite needs a role that CanWrite (editor, admin or maintainer).
	requireWrite
	// requireAdmin needs a role that IsAdmin (admin or maintainer).
	requireAdmin
	// requireMaintain needs a role that CanMaintain (maintainer only).
	requireMaintain
	// requireImport needs a role that CanImport (maintainer only).
	requireImport
)

// authorize reports whether role satisfies req. It is the pure decision behind
// the RequireAuth / RequireWrite / RequireAdmin / RequireMaintainer /
// RequireImport middlewares; an invalid role never satisfies any requirement.
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
	case requireMaintain:
		return role.CanMaintain()
	case requireImport:
		return role.CanImport()
	default:
		return false
	}
}
