// Package memberapi is a project admin's management of the members of their
// project over HTTP: list them, change a member's role, approve a waiting one,
// remove one, and add an existing account by its exact username or e-mail
// address.
//
// Every route is project-scoped — mounted under /api/v1/p/{project} only — and
// RequireAdmin: the project's admin, or a system administrator, who enters
// every project as its admin. The project is the request's (auth.WithProject),
// so an admin of one project addressing another gets the very 404 an unknown
// slug gets, and no route takes a project from the body.
//
// The add never tells the caller whether the account exists: its answer is
// 202 {"status":"accepted"}, byte for byte, whether the account was added, was
// already a member or does not exist at all, so a project admin cannot use it
// to learn which accounts the instance holds. What really happened is recorded
// in the audit trail (internal/project.AddByIdentifier). Nor does anything
// after it: a membership made that way has its identity withheld until the
// account enters the project, and until then the project cannot see it at all
// — it is not listed, and every route addressing a member by uid answers
// 404 not_member for it exactly as for an account that is no member — so
// neither its uid, nor its picture, nor whether it exists leaks through the
// list. The one way back is as blind as the add: POST /members/withdraw names
// the same identifier and always answers 202. Both are throttled per caller,
// so the blind guesses cannot be scripted at speed either.
//
// Every mutation is audited, with the project, in the same transaction as the
// change, by internal/project. Errors are a stable machine-readable code —
// {"error":"last_admin"} — plus, for a rule the client should show, a human
// "message".
package memberapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
)

// maxBodyBytes caps a request body. Every body here is one or two short fields,
// so a small limit is ample and keeps a hostile request cheap.
const maxBodyBytes = 16 << 10

// Members is the subset of project.Store the routes need. It is an interface
// so the handlers can be unit-tested with a fake.
type Members interface {
	// Members lists the project's members by display name.
	Members(ctx context.Context, projectUID string) ([]project.Member, error)
	// AddByIdentifier adds the account an exact username or e-mail names,
	// approved, and audits the real outcome.
	AddByIdentifier(
		ctx context.Context, meta audit.Meta, projectUID, identifier string, role project.Role,
	) (project.AddOutcome, error)
	// WithdrawByIdentifier removes the withheld membership of the account an
	// exact username or e-mail names, and audits the real outcome.
	WithdrawByIdentifier(
		ctx context.Context, meta audit.Meta, projectUID, identifier string,
	) (project.WithdrawOutcome, error)
	// Membership reads one member, project.ErrNotMember when there is none.
	Membership(ctx context.Context, projectUID, userUID string) (*project.Member, error)
	// SetRole changes a member's role, audited; project.ErrLastAdmin guards the
	// project's only admin.
	SetRole(ctx context.Context, meta audit.Meta, projectUID, userUID string, role project.Role) (*project.Member, error)
	// ApproveWith approves a waiting member, audited, running hook on the same
	// transaction when the approval changes something.
	ApproveWith(
		ctx context.Context, meta audit.Meta, projectUID, userUID string, hook project.ApproveHook,
	) (*project.Member, error)
	// RemoveMember removes a membership, audited; project.ErrLastAdmin guards
	// the project's only admin.
	RemoveMember(ctx context.Context, meta audit.Meta, projectUID, userUID string) error
}

// Accounts is the subset of auth.Store the approval mail needs: the address of
// the account being approved.
type Accounts interface {
	// GetUserByUID returns one account, or auth.ErrUserNotFound.
	GetUserByUID(ctx context.Context, uid string) (auth.User, error)
}

// MailScheduler schedules a mail on the caller's transaction, so it is sent if
// and only if the change that caused it commits (see internal/mailjob).
type MailScheduler interface {
	// Enqueue schedules m using exec, a pool or an open transaction.
	Enqueue(ctx context.Context, exec jobs.Execer, m mailjob.Mail) error
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Members backs every route.
	Members Members
	// Accounts reads the address the approval mail goes to. Optional together
	// with Mail: without either, an approval sends nothing.
	Accounts Accounts
	// Mail schedules the "you may come in" mail of an approval. Optional: nil
	// approves and mails nothing, like an instance whose mail is off.
	Mail MailScheduler
	// ProjectURL returns the public address of a project's page (/p/<slug>/ on
	// this instance) for the approval mail; nil or "" leaves the link blank.
	ProjectURL func(slug string) string
	// RequireAdmin guards every route: the project's admin (or a system
	// administrator).
	RequireAdmin func(http.Handler) http.Handler
	// Throttle limits the blind add and its withdrawal per caller (after
	// RequireAdmin, so it may key on the signed-in account). Optional: nil
	// leaves them unthrottled.
	Throttle func(http.Handler) http.Handler
}

// API serves a project's member management.
type API struct {
	members      Members
	accounts     Accounts
	mail         MailScheduler
	projectURL   func(slug string) string
	requireAdmin func(http.Handler) http.Handler
	throttle     func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		members: cfg.Members, accounts: cfg.Accounts, mail: cfg.Mail,
		projectURL: cfg.ProjectURL, requireAdmin: cfg.RequireAdmin, throttle: cfg.Throttle,
	}
}

// RegisterRoutes mounts the routes onto r, which the caller has scoped to one
// project (/api/v1/p/{project}):
//
//	GET    /members                      the members with role, approval and last sign-in
//	POST   /members                      add an account by exact username or e-mail (always 202)
//	POST   /members/withdraw             undo such an add by the same identifier (always 202)
//	PATCH  /members/{user_uid}           change a member's role (never one's own)
//	POST   /members/{user_uid}/approve   approve a waiting member, mailing them
//	DELETE /members/{user_uid}           remove a member (204)
//
// All of them RequireAdmin; the two blind ones are throttled per caller too.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/members", func(r chi.Router) {
		r.Use(a.requireAdmin)
		r.Get("/", a.handleList)
		r.With(a.throttled).Post("/", a.handleAdd)
		r.With(a.throttled).Post("/withdraw", a.handleWithdraw)
		r.Patch("/{user_uid}", a.handleSetRole)
		r.Post("/{user_uid}/approve", a.handleApprove)
		r.Delete("/{user_uid}", a.handleRemove)
	})
}

// throttled wraps next in the configured per-caller throttle, or returns it
// as it is when there is none.
func (a *API) throttled(next http.Handler) http.Handler {
	if a.throttle == nil {
		return next
	}
	return a.throttle(next)
}

// auditMeta is the audit envelope of a mutation, attributed to the signed-in
// admin (RequireAdmin guarantees one).
func auditMeta(r *http.Request) audit.Meta {
	return audit.FromRequest(r, actorUID(r))
}

// actorUID is the uid of the signed-in caller.
func actorUID(r *http.Request) string {
	user, _ := auth.UserFromContext(r.Context())
	return user.UID
}
