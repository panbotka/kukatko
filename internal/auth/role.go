// Package auth implements Kukátko's authentication and authorization: local
// users with bcrypt passwords, opaque-token sessions with sliding expiry,
// login rate limiting, and role-based access control on two ladders.
//
// The two ladders:
//
//   - the instance role on the account (users.role, Role): "user" or
//     "maintainer". A maintainer — the system administrator — operates the
//     instance (imports, maintenance, system status, backup, restore, jobs,
//     processing backfills) and is implicitly an admin of every project;
//   - the project role (project_members.role, project.Role): viewer < curator <
//     editor < admin, per project. Everything a request does to a library is
//     authorised by the caller's role in the request's project.
//
// The request's project is resolved by the middleware WithProject installs and
// the Require* guards apply (see project.go): ProjectFromContext and
// ProjectUIDFromContext are the only way a handler or store learns the current
// project — nothing reads a project from a request body, a query parameter or a
// default of its own.
//
// The package is layered: pure helpers (Role, password hashing, UID and token
// generation, the rate limiter) carry no I/O and are unit-tested; Store wraps
// the database; Service orchestrates the domain logic; Handlers and the RBAC
// middleware expose it over HTTP. External state lives in PostgreSQL via the
// shared pgx pool.
package auth

import "github.com/panbotka/kukatko/internal/project"

// Role is an account's instance role: RoleUser or RoleMaintainer. It says
// nothing about what the account may do inside a project — that is the
// project.Role of its membership, carried on the request as the principal's
// project role. The only capabilities it grants are the instance operations
// (CanMaintain, CanImport) and, through the project resolution, the implicit
// admin role in every project.
type Role string

const (
	// RoleMaintainer is the system administrator: instance operations —
	// imports, maintenance, system status, backup, restore, jobs and processing
	// backfills — and implicitly the admin of every project. Only a maintainer
	// may grant or alter the maintainer role.
	RoleMaintainer Role = "maintainer"
	// RoleUser is every other account; what it may do lives entirely in its
	// project memberships.
	RoleUser Role = "user"
)

// Valid reports whether r is one of the two instance roles.
func (r Role) Valid() bool {
	return r == RoleUser || r == RoleMaintainer
}

// CanMaintain reports whether r holds the instance operations privileges:
// imports, maintenance, system status, backup, restore, jobs and processing
// backfills. Only a maintainer qualifies.
func (r Role) CanMaintain() bool {
	return r == RoleMaintainer
}

// CanImport reports whether r may trigger the read-only import and migration
// runs. Import is an operations capability, so only a maintainer qualifies.
func (r Role) CanImport() bool {
	return r.CanMaintain()
}

// AccountRole is the pre-project five-rung vocabulary — viewer, curator,
// editor, admin, maintainer — that the maintainer-only /admin/users endpoints
// accept and that the "role" field of a serialised User reports. The frontend
// no longer reads it (it reads instance_role and the per-project roles of
// GET /auth/me). It is never an authorization decision: Split turns it into the
// instance role and the role in the bootstrap project, which is what is stored
// and what is checked.
type AccountRole string

const (
	// AccountMaintainer is an instance maintainer (and admin of the project).
	AccountMaintainer AccountRole = "maintainer"
	// AccountAdmin is a user who is an admin of the project.
	AccountAdmin AccountRole = "admin"
	// AccountEditor is a user who is an editor of the project.
	AccountEditor AccountRole = "editor"
	// AccountCurator is a user who is a curator of the project.
	AccountCurator AccountRole = "curator"
	// AccountViewer is a user who is a viewer of the project.
	AccountViewer AccountRole = "viewer"
)

// Valid reports whether r is one of the five account roles.
func (r AccountRole) Valid() bool {
	return r == AccountMaintainer || project.Role(r).Valid()
}

// Split returns the instance role and the project role r stands for: a
// maintainer is RoleMaintainer and an admin of the project, every other value is
// RoleUser with the project role of the same name. An invalid r yields RoleUser
// and an invalid project role, which the callers have already refused.
func (r AccountRole) Split() (Role, project.Role) {
	if r == AccountMaintainer {
		return RoleMaintainer, project.RoleAdmin
	}
	return RoleUser, project.Role(r)
}

// instanceRoleOf returns the instance half of r's Split.
func instanceRoleOf(r AccountRole) Role {
	instance, _ := r.Split()
	return instance
}

// accountRoleOf folds an instance role and a project role back into the
// five-rung vocabulary: a maintainer is always "maintainer" — whatever its
// membership says, it is an admin everywhere — and anybody else is its project
// role, or "" when it has none.
func accountRoleOf(instance Role, inProject project.Role) AccountRole {
	if instance.CanMaintain() {
		return AccountMaintainer
	}
	return AccountRole(inProject)
}

// requirement is the access level a request must satisfy, used by the RBAC
// middleware to make an authorization decision from the principal.
type requirement int

const (
	// requireAuth needs an authenticated member of the request's project (any
	// project role).
	requireAuth requirement = iota
	// requireCurate needs a project role that CanCurate (curator, editor, admin).
	requireCurate
	// requireWrite needs a project role that CanWrite (editor, admin).
	requireWrite
	// requireAdmin needs a project role that IsAdmin (admin).
	requireAdmin
	// requireMaintain needs an instance role that CanMaintain (maintainer).
	requireMaintain
	// requireImport needs an instance role that CanImport (maintainer).
	requireImport
)

// instanceLevel reports whether req is decided by the instance role rather than
// the project role.
func (req requirement) instanceLevel() bool {
	return req == requireMaintain || req == requireImport
}

// authorize reports whether a caller with the instance role instance and the
// project role inProject satisfies req. It is the pure decision behind the
// RequireAuth / RequireCurator / RequireWrite / RequireAdmin / RequireMaintainer /
// RequireImport middlewares: the first four evaluate the project role (an
// invalid or empty one satisfies none of them), the last two the instance role.
func authorize(instance Role, inProject project.Role, req requirement) bool {
	switch req {
	case requireMaintain:
		return instance.CanMaintain()
	case requireImport:
		return instance.CanImport()
	case requireAuth:
		return inProject.Valid()
	case requireCurate:
		return inProject.CanCurate()
	case requireWrite:
		return inProject.CanWrite()
	case requireAdmin:
		return inProject.IsAdmin()
	default:
		return false
	}
}
