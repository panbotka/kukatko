// Package projectapi is the system administrator's instance-level management of
// projects and accounts over HTTP: list, create and edit projects (name,
// description, size limits, active flag), add or remove anybody's membership in
// any project, list every account of the instance with its memberships, and
// promote or demote a system administrator.
//
// Every route is RequireMaintainer and mounted once under /api/v1 — never under
// /p/{project}: it is about the instance, not one library. Every mutation is
// audited, with the project it touched, in the same transaction as the change
// by the store that makes it (internal/project, internal/auth). Errors are a
// stable machine-readable code — {"error":"slug_taken"} — plus, for a rule the
// client should show, a human "message".
package projectapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
)

// maxBodyBytes caps a request body. Every body here is a handful of short
// fields, so a small limit is ample and keeps a hostile request cheap.
const maxBodyBytes = 16 << 10

// Projects is the subset of project.Store the routes need. It is an interface
// so the handlers can be unit-tested with a fake.
type Projects interface {
	// Overviews lists every project with its member count and usage.
	Overviews(ctx context.Context) ([]project.Overview, error)
	// OverviewOf returns one project with its member count and usage, or
	// project.ErrNotFound.
	OverviewOf(ctx context.Context, uid string) (*project.Overview, error)
	// Create inserts a project (its settings row and the creator's admin
	// membership included), audited.
	Create(ctx context.Context, meta audit.Meta, p project.CreateParams) (*project.Project, error)
	// Update applies a patch to one project, audited.
	Update(ctx context.Context, meta audit.Meta, uid string, patch project.UpdatePatch) (*project.Project, error)
	// AddMember adds a membership, audited; project.ErrAlreadyMember when one exists.
	AddMember(
		ctx context.Context, meta audit.Meta, projectUID, userUID string, role project.Role, approved bool,
	) (*project.Member, error)
	// RemoveMember removes a membership, audited; project.ErrLastAdmin guards the
	// project's only admin.
	RemoveMember(ctx context.Context, meta audit.Meta, projectUID, userUID string) error
	// AllMemberships returns every account's memberships keyed by user uid.
	AllMemberships(ctx context.Context) (map[string][]project.MembershipSummary, error)
	// MembershipsOf returns one account's memberships.
	MembershipsOf(ctx context.Context, userUID string) ([]project.MembershipSummary, error)
}

// Accounts is the subset of auth.Store the account routes need.
type Accounts interface {
	// ListUsers returns the accounts matching filter (a zero filter: all).
	ListUsers(ctx context.Context, filter auth.UserFilter) ([]auth.User, error)
	// GetUserByUID returns one account, or auth.ErrUserNotFound.
	GetUserByUID(ctx context.Context, uid string) (auth.User, error)
	// SetInstanceRoleAudited sets an account's instance role, audited, guarded
	// against stranding the instance without a maintainer.
	SetInstanceRoleAudited(ctx context.Context, uid string, role auth.Role, entry audit.Entry) (auth.User, error)
}

// Setup is the "set up a project for somebody" flow (auth.ProjectSetup): a
// project together with its first admin, and the set-password link of a member
// who has not chosen a password yet.
type Setup interface {
	// Create creates the project p with the person in as its only admin, in one
	// transaction with the account, the link and the mail.
	Create(
		ctx context.Context, meta audit.Meta, p project.CreateParams, in auth.FirstAdminInput,
	) (*project.Project, auth.FirstAdmin, error)
	// ResendLink issues and mails a fresh set-password link to a member whose
	// account has no password yet.
	ResendLink(ctx context.Context, meta audit.Meta, projectUID, userUID string) (auth.FirstAdmin, error)
	// AwaitingPassword lists the members of a project whose accounts have no
	// password yet.
	AwaitingPassword(ctx context.Context, projectUID string) ([]auth.AwaitingMember, error)
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Projects backs the /projects routes and the memberships of /users.
	Projects Projects
	// Accounts backs the /users routes.
	Accounts Accounts
	// Setup backs a create with a first admin and the set-password routes.
	Setup Setup
	// RequireMaintainer guards every route: the system administrator only.
	RequireMaintainer func(http.Handler) http.Handler
}

// API serves the system administrator's project and account management.
type API struct {
	projects          Projects
	accounts          Accounts
	setup             Setup
	requireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		projects: cfg.Projects, accounts: cfg.Accounts, setup: cfg.Setup, requireMaintainer: cfg.RequireMaintainer,
	}
}

// RegisterRoutes mounts the routes onto r, which the caller has scoped under
// the API base path (/api/v1), instance-level:
//
//	GET    /projects                            list with members, photos and bytes
//	POST   /projects                            create (201); the caller — or the named first admin — is its admin
//	GET    /projects/{uid}                      one project
//	PATCH  /projects/{uid}                      edit name, description, active, limits
//	GET    /projects/{uid}/awaiting-password    members whose accounts have no password yet
//	POST   /projects/{uid}/members              add an approved membership (201)
//	DELETE /projects/{uid}/members/{user_uid}   remove a membership (204)
//	POST   /projects/{uid}/members/{user_uid}/password-link   re-send the set-password link
//	GET    /users                               every account with its memberships
//	PATCH  /users/{uid}                         set the instance role (promote/demote)
//
// All of them RequireMaintainer.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/projects", func(r chi.Router) {
		r.Use(a.requireMaintainer)
		r.Get("/", a.handleList)
		r.Post("/", a.handleCreate)
		r.Get("/{uid}", a.handleGet)
		r.Patch("/{uid}", a.handleUpdate)
		r.Get("/{uid}/awaiting-password", a.handleAwaitingPassword)
		r.Post("/{uid}/members", a.handleAddMember)
		r.Delete("/{uid}/members/{user_uid}", a.handleRemoveMember)
		r.Post("/{uid}/members/{user_uid}/password-link", a.handleResendLink)
	})
	// Flat rather than a /users sub-router: GET /users/{uid}/avatar
	// (internal/userpicapi) shares the prefix with a different guard.
	r.With(a.requireMaintainer).Get("/users", a.handleListUsers)
	r.With(a.requireMaintainer).Patch("/users/{uid}", a.handleSetInstanceRole)
}

// auditMeta is the audit envelope of a mutation, attributed to the signed-in
// maintainer (RequireMaintainer guarantees one).
func auditMeta(r *http.Request) audit.Meta {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
}
