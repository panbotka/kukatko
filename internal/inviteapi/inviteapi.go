// Package inviteapi exposes invitation links over HTTP: a project admin's
// management of their project's invites, and the two public routes behind a
// link — the page's description of what the invite grants, and accepting it
// signed in.
//
// The management routes are project-scoped — mounted under /api/v1/p/{project}
// only — and RequireAdmin, so an admin of one project addressing another gets
// the very 404 an unknown slug gets, and another project's invite UID is a 404
// like a missing one. The public routes are instance-level (/api/v1/invite/…):
// an invite resolves its own project from its row. They are throttled per
// client IP ahead of the code lookup, and every unusable code — unknown,
// expired, revoked, used up, of a deactivated project — is answered with the
// same 404 body, so a guesser learns nothing.
//
// It also implements auth.InviteGate, which lets an invitation stand in for
// the shared registration secret (and for the approval) at POST
// /auth/register.
//
// Errors are a stable machine-readable code — {"error":"not_found"} — plus,
// for a rule the client should show, a human "message".
package inviteapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/invite"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// InvitePath is the frontend route an invite's URL opens: <base>/invite/<code>.
const InvitePath = "/invite/"

// Store is the subset of invite.Store the routes need. It is an interface so
// the handlers can be unit-tested with a fake.
type Store interface {
	// List returns every invite of the project, newest first.
	List(ctx context.Context, projectUID string) ([]invite.Invite, error)
	// Create stores an invite in the project, audited.
	Create(ctx context.Context, projectUID string, in invite.NewInvite, meta audit.Meta) (invite.Invite, error)
	// Revoke revokes one of the project's invites for good, audited; another
	// project's is invite.ErrNotFound.
	Revoke(ctx context.Context, projectUID, uid string, meta audit.Meta) (invite.Invite, error)
	// Resolve returns the live invite a code names, or invite.ErrInvalid.
	Resolve(ctx context.Context, code string) (invite.Invite, error)
	// Accept lets a signed-in account accept an invite, audited, or answers
	// invite.ErrInvalid.
	Accept(ctx context.Context, code, userUID string, meta audit.Meta) (invite.Acceptance, error)
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs every route (required).
	Store Store
	// BaseURL is the instance's public address; an invite's URL is
	// BaseURL + InvitePath + code. Empty yields a path relative to the host.
	BaseURL string
	// RequireAdmin guards the management routes: the project's admin (or a
	// system administrator) (required).
	RequireAdmin func(http.Handler) http.Handler
	// OptionalAuth puts a signed-in caller on the context of the public page
	// without refusing anybody else (required).
	OptionalAuth func(http.Handler) http.Handler
	// RequireSignedIn guards the accept: any signed-in account, whatever
	// project it belongs to (required).
	RequireSignedIn func(http.Handler) http.Handler
	// IPLimit throttles the public routes per client IP; nil throttles nothing.
	IPLimit *ratelimit.Limiter
	// Logger records the failures a handler swallows; nil uses slog.Default().
	Logger *slog.Logger
}

// API serves invitation links.
type API struct {
	store           Store
	baseURL         string
	requireAdmin    func(http.Handler) http.Handler
	optionalAuth    func(http.Handler) http.Handler
	requireSignedIn func(http.Handler) http.Handler
	ipLimit         *ratelimit.Limiter
	log             *slog.Logger
}

// NewAPI returns an API from cfg. A nil limiter becomes a disabled one, so the
// routes never branch on whether throttling is configured.
func NewAPI(cfg Config) *API {
	a := &API{
		store: cfg.Store, baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		requireAdmin: cfg.RequireAdmin, optionalAuth: cfg.OptionalAuth, requireSignedIn: cfg.RequireSignedIn,
		ipLimit: cfg.IPLimit, log: cfg.Logger,
	}
	if a.ipLimit == nil {
		a.ipLimit = ratelimit.New(0, 0)
	}
	if a.log == nil {
		a.log = slog.Default()
	}
	return a
}

// RegisterRoutes mounts the project-scoped management routes onto r, which the
// caller has scoped to one project (/api/v1/p/{project}):
//
//	GET    /invites        the project's invites with their codes and URLs
//	POST   /invites        create {role, days?, max_uses?} → 201 with the URL
//	DELETE /invites/{uid}  revoke for good (204)
//
// All of them RequireAdmin.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/invites", func(r chi.Router) {
		r.Use(a.requireAdmin)
		r.Get("/", a.handleList)
		r.Post("/", a.handleCreate)
		r.Delete("/{uid}", a.handleRevoke)
	})
}

// RegisterPublicRoutes mounts the instance-level public routes onto r (the API
// base path, /api/v1):
//
//	GET  /invite/{code}         public           project name, role, signed in?
//	POST /invite/{code}/accept  RequireSignedIn  join (or be raised in) the project → {slug}
//
// Both are throttled per client IP ahead of everything else.
func (a *API) RegisterPublicRoutes(r chi.Router) {
	r.Route("/invite/{code}", func(r chi.Router) {
		r.Use(a.ipLimit.Middleware)
		r.With(a.optionalAuth).Get("/", a.handlePublic)
		r.With(a.requireSignedIn).Post("/accept", a.handleAccept)
	})
}

// inviteURL is the address an invite with code opens.
func (a *API) inviteURL(code string) string {
	return a.baseURL + InvitePath + code
}

// auditMeta is the audit envelope of a mutation, attributed to the signed-in
// caller.
func auditMeta(r *http.Request) audit.Meta {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
}
