package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/obs"
	"github.com/panbotka/kukatko/internal/project"
)

// ProjectURLParam is the chi URL parameter naming the request's project by slug
// (/p/{project}/…).
const ProjectURLParam = "project"

// ErrorPendingApproval is the error body of the 403 a member whose membership
// still waits for a project admin's approval gets for every project route.
const ErrorPendingApproval = "pending_approval"

// projectNotFoundMessage is the error body of every "no such project" 404 —
// identical for an unknown slug, a project the caller is not a member of and a
// deactivated one, so a tenant cannot tell the three apart.
const projectNotFoundMessage = "project not found"

// lastProjectTouchInterval is how often the remembered last project of one
// account is written at most: once a minute per account is plenty for "where
// did I work last" and keeps a busy grid of thumbnails from writing on every
// request.
const lastProjectTouchInterval = time.Minute

// errNoProject is the error of a project-scoped guard whose request names no
// project (see enterProject).
var errNoProject = errors.New("auth: project-scoped route reached without a project")

// errProjectDenied marks a project resolution that already wrote its response.
var errProjectDenied = errors.New("auth: project access denied")

// projectRequest is what WithProject records about the request's project before
// the caller is known: the store to resolve membership with and the project the
// slug names, or the lookup error (project.ErrNotFound for an unknown slug).
// The decision itself waits for the guard, so an anonymous caller is refused
// with 401 whether or not the slug exists.
type projectRequest struct {
	store   *project.Store
	project *project.Project
	err     error
}

// projectRequestKey is the context key of the projectRequest.
type projectRequestKey struct{}

// WithProject returns the middleware that names the request's project: the
// chi URL parameter ProjectURLParam (a slug), looked up through store. It must
// run before the Require* guards, which decide whether the authenticated caller
// may enter that project and with which role (see enterProject). The server
// applies it to the /api/v1/p/{project} group only; a request without the
// parameter names no project at all — there is no default project.
//
// It also records the path prefix the request came through (see APIBase), so
// the URLs a handler builds lead back through the same prefix.
func WithProject(store *project.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slug := chi.URLParam(r, ProjectURLParam)
			ctx := ContextWithAPIBase(r.Context(), apiBaseFor(slug))
			if slug != "" {
				p, err := store.GetBySlug(ctx, slug)
				ctx = context.WithValue(ctx, projectRequestKey{}, projectRequest{store: store, project: p, err: err})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// projectRequestFor returns the projectRequest WithProject recorded on ctx,
// reporting whether there is one: false for a request that named no project —
// an instance-wide route, or a router assembled without WithProject.
func projectRequestFor(ctx context.Context) (projectRequest, bool) {
	req, ok := ctx.Value(projectRequestKey{}).(projectRequest)
	return req, ok
}

// namesProject reports whether the request names a project (passed WithProject
// with a {project} parameter), whatever that project turns out to be.
func namesProject(r *http.Request) bool {
	_, ok := projectRequestFor(r.Context())
	return ok
}

// enterProject decides whether the authenticated principal p may enter the
// request's project and returns p with Project and ProjectRole filled in. The
// rules, in order:
//
//   - an unknown slug is 404;
//   - a maintainer enters every project — deactivated or not, member or not — as
//     its admin, and the access log records actor_role=maintainer;
//   - a deactivated project is 404 for everybody else;
//   - no membership is 404, the very same answer as an unknown slug, so a tenant
//     never learns another project exists;
//   - a membership nobody has approved yet is 403 {"error":"pending_approval"};
//   - otherwise the caller enters with its membership's role.
//
// A refusal is written to w and reported as errProjectDenied; any other error
// is a 500 already written too. On success the account's last project is
// remembered (throttled per account).
func (a *API) enterProject(w http.ResponseWriter, r *http.Request, p principal) (principal, error) {
	req, ok := projectRequestFor(r.Context())
	if !ok {
		// A project-scoped guard on a route that names no project is a wiring
		// mistake on the server, never a client's: the route belongs under
		// /api/v1/p/{project}, or behind an instance-level guard.
		slog.ErrorContext(r.Context(), "auth: project-scoped route reached without a project",
			"method", r.Method, "route", routePattern(r), "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal error")
		return p, errNoProject
	}
	if errors.Is(req.err, project.ErrNotFound) {
		writeError(w, http.StatusNotFound, projectNotFoundMessage)
		return p, errProjectDenied
	}
	if req.err != nil {
		slog.ErrorContext(r.Context(), "auth: resolving the request's project", "error", req.err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return p, req.err
	}
	role, subject, err := projectRoleOf(w, r, req, p.user)
	if err != nil {
		return p, err
	}
	p.project, p.projectRole, p.projectSubject = req.project, role, subject
	a.touchLastProject(r.Context(), req.store, p.user.UID, req.project.UID)
	return p, nil
}

// projectRoleOf applies enterProject's maintainer, deactivation and membership
// rules for user in req's project, writing any refusal to w and returning
// errProjectDenied. Alongside the role it returns the person the membership
// says the caller is in that project ("this is me"), nil when there is none —
// also for a maintainer who entered a project without being a member of it.
func projectRoleOf(
	w http.ResponseWriter, r *http.Request, req projectRequest, user User,
) (project.Role, *string, error) {
	member, err := req.store.Membership(r.Context(), req.project.UID, user.UID)
	if err != nil && !errors.Is(err, project.ErrNotMember) {
		slog.ErrorContext(r.Context(), "auth: reading the caller's membership", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return "", nil, fmt.Errorf("auth: reading the membership of %s: %w", user.UID, err)
	}
	if user.Role.CanMaintain() {
		obs.SetActorRole(r.Context(), string(RoleMaintainer))
		if member == nil {
			return project.RoleAdmin, nil, nil
		}
		revealIdentity(r.Context(), req.store, member)
		return project.RoleAdmin, member.SubjectUID, nil
	}
	switch {
	case !req.project.Active, member == nil:
		writeError(w, http.StatusNotFound, projectNotFoundMessage)
		return "", nil, errProjectDenied
	case member.ApprovedAt == nil:
		writeError(w, http.StatusForbidden, ErrorPendingApproval)
		return "", nil, errProjectDenied
	}
	revealIdentity(r.Context(), req.store, member)
	return member.Role, member.SubjectUID, nil
}

// revealIdentity ends the withholding of member's identity (see
// project.Member.IdentityWithheld) the first time its account enters the
// project: coming in is the account's own act, after which the project may see
// who it is. A membership that withholds nothing costs no write. A failure is
// logged and otherwise ignored — it is bookkeeping, never a reason to refuse,
// and the next request tries again.
func revealIdentity(ctx context.Context, store *project.Store, member *project.Member) {
	if !member.IdentityWithheld {
		return
	}
	if err := store.RevealIdentity(ctx, member.ProjectUID, member.UserUID); err != nil {
		slog.WarnContext(ctx, "auth: revealing a member's identity",
			"project_uid", member.ProjectUID, "user_uid", member.UserUID, "error", err)
	}
}

// touchLastProject remembers projectUID as userUID's last project unless that
// account was remembered less than lastProjectTouchInterval ago. A failure is
// logged and otherwise ignored: it is bookkeeping, never a reason to refuse.
func (a *API) touchLastProject(ctx context.Context, store *project.Store, userUID, projectUID string) {
	if !a.lastProject.due(userUID, projectUID, a.now()) {
		return
	}
	if err := store.TouchLastProject(ctx, userUID, projectUID); err != nil {
		slog.WarnContext(ctx, "auth: remembering the last project", "user_uid", userUID, "error", err)
	}
}

// lastProjectThrottle remembers, per account, which project was last written
// as its last project and when, so the write happens at most once per interval
// (or at once when the account moves to another project).
type lastProjectThrottle struct {
	mu   sync.Mutex
	seen map[string]lastProjectTouch
}

// lastProjectTouch is one account's last write.
type lastProjectTouch struct {
	projectUID string
	at         time.Time
}

// due reports whether userUID's last project should be written now and, when
// it should, records the write as done at now. It is due for an account never
// seen, for a different project than last time, or once lastProjectTouchInterval
// has passed. Entries older than the interval are dropped as they are found, so
// the map holds only the accounts active within the last minute or so.
func (t *lastProjectThrottle) due(userUID, projectUID string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = map[string]lastProjectTouch{}
	}
	last, ok := t.seen[userUID]
	if ok && last.projectUID == projectUID && now.Sub(last.at) < lastProjectTouchInterval {
		return false
	}
	for uid, touch := range t.seen {
		if now.Sub(touch.at) >= lastProjectTouchInterval {
			delete(t.seen, uid)
		}
	}
	t.seen[userUID] = lastProjectTouch{projectUID: projectUID, at: now}
	return true
}

// ProjectFromContext returns the project the request was authorised for — set
// by the Require* guards after WithProject named it — reporting whether there
// is one. Together with ProjectUIDFromContext it is the only way a handler or a
// store learns the current project.
func ProjectFromContext(ctx context.Context) (*project.Project, bool) {
	p, ok := principalFromContext(ctx)
	if !ok || p.project == nil {
		return nil, false
	}
	return p.project, true
}

// ProjectUIDFromContext returns the uid of the request's project, or "" when
// the request carries no project context.
func ProjectUIDFromContext(ctx context.Context) string {
	if p, ok := ProjectFromContext(ctx); ok {
		return p.UID
	}
	return ""
}

// LinkedSubjectFromContext returns the person the caller says it is in the
// request's project — its membership's subject_uid, what `person:me` and
// `family:me` resolve to — or nil when the membership names nobody, when the
// caller (a maintainer) has no membership there, or when the request carries no
// project context. There is no account-wide link (migration 0110 dropped it):
// who you are is a fact about one project's people.
func LinkedSubjectFromContext(ctx context.Context) *string {
	p, ok := principalFromContext(ctx)
	if !ok || p.project == nil {
		return nil
	}
	return p.projectSubject
}

// ProjectRoleFromContext returns the caller's role in the request's project —
// project.RoleAdmin for a maintainer — or the zero value when the request
// carries no project context. Every per-project permission check in a handler
// asks this, never the account's instance role.
func ProjectRoleFromContext(ctx context.Context) project.Role {
	p, _ := principalFromContext(ctx)
	return p.projectRole
}

// publicProjectKey is the context key of the project RequireActiveProject
// admitted an anonymous request into.
type publicProjectKey struct{}

// RequireActiveProject wraps next so it runs for anybody — signed in or not,
// member or not — whose request names an existing, active project, which it
// records for PublicProjectFromContext. An unknown slug and a deactivated
// project are the same 404 every other project route answers a tenant that may
// not see the project, so the route tells a stranger nothing a sign-in screen
// would not. A request that names no project is a 404 too.
//
// It is for the one anonymous project route, GET /p/{project}/settings/public,
// which the project's registration page asks before anybody has an account. It
// deliberately does not authenticate and puts no principal on the context:
// ProjectFromContext and ProjectUIDFromContext stay empty behind it, so a
// project-scoped store can never mistake an anonymous request for an admitted
// one.
func RequireActiveProject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, ok := projectRequestFor(r.Context())
		if !ok || errors.Is(req.err, project.ErrNotFound) || (req.err == nil && !req.project.Active) {
			writeError(w, http.StatusNotFound, projectNotFoundMessage)
			return
		}
		if req.err != nil {
			slog.ErrorContext(r.Context(), "auth: resolving the request's project", "error", req.err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		ctx := obs.WithProject(r.Context(), req.project.UID, req.project.Slug)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, publicProjectKey{}, req.project)))
	})
}

// PublicProjectFromContext returns the project RequireActiveProject admitted an
// anonymous request into, reporting whether there is one. It is the
// anonymous route's counterpart of ProjectFromContext, kept apart from it on
// purpose (see RequireActiveProject).
func PublicProjectFromContext(ctx context.Context) (*project.Project, bool) {
	p, ok := ctx.Value(publicProjectKey{}).(*project.Project)
	return p, ok && p != nil
}

// routePattern returns the chi route pattern r matched ("/p/{project}/photos"),
// or its path when chi recorded none, for a log line naming the route.
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return r.URL.Path
}
