// Package auditapi exposes the HTTP API over the durable audit trail
// (internal/audit). It serves two read endpoints twice. On the instance, GET
// /audit is the system administrator's (maintainer) listing of the whole trail,
// newest-first, with optional filters (project=<slug>, acting user, entity type
// and UID, action, review-game decisions via=review, Ano/Ne bucket
// decision=yes|no, created-at date range) and limit/offset pagination, plus the
// total matching count so the UI can page — instance_only=true narrows it to
// the entries filed under no project, the instance's own (accounts, instance
// roles, blocking), which no project's listing ever shows; GET /audit/mine is the same listing
// for any signed-in account, narrowed to its own actions in every project.
// Under a project, GET /p/{project}/audit is that project's admin's listing and
// GET /p/{project}/audit/mine a member's own actions there — both narrowed to
// the entries filed under the project, so an admin of one project never reads
// another's (or the instance's) entries. Every record names its project
// (project_uid, and project = the slug). The audit log is write-only from the
// application's side — entries are appended within mutation transactions
// elsewhere — so this package never mutates it.
//
// # Every filter is checked
//
// A reader's filters are validated strictly, and an unknown query key is refused
// by name rather than ignored (the own-activity listing's user and project are
// the one exception, see below). The reason is that this endpoint's empty answer
// is a statement about a person: ?user=panbotka returning nothing reads as "they
// did nothing", not "you spelled the parameter wrong", and that is a half hour
// of looking for actions that were there all along. So a user value is resolved
// as an account UID or a username, an action is checked against audit's closed
// set of labels, and an invented parameter is a 400. The two exceptions are
// entity_uid and entity_type, and both are deliberate: the trail outlives the
// entities it describes (an entry about a purged photo is the point of having a
// trail), and the entity type is a free string by design.
//
// # Why /audit/mine is a route of its own
//
// The own-activity view could have been the same endpoint under a looser guard,
// narrowing the filter for a non-admin inside the handler. It is a separate
// route instead: handleListMine overwrites the actor filter unconditionally, so
// "a caller cannot read someone else's actions" is a property of the route's
// shape rather than of a branch a later edit could weaken. It costs one small
// handler; the filter parsing, the response building and the store are shared.
//
// # /audit/mine ignores user and project
//
// The own-activity listing drops the user and project parameters before
// anything reads them (ownOnly), for every caller. Validating them there was an
// oracle: any signed-in account could tell an existing username from a missing
// one (403 "only your own actions" vs 400 "no such account") and an existing
// project slug from a missing one (200 vs 400) — and multi-tenancy promises a
// project learns nothing about another, not even a name. So the listing is
// always the caller's own actions — in the instance mount across every project,
// in a project mount in that project — whatever those two values say, and an
// unknown value gets the same 200 as a valid one. The cost is the one the
// strict checking above guards against: ?user=somebody-else is silently served
// your own actions. The client never sends either parameter (the page offers no
// such filter), and a maintainer who wants them filters the whole trail on GET
// /audit, which keeps every check.
//
// # A project's user filter knows only the project's people
//
// A project admin's listing (GET /p/{project}/audit) keeps the user filter —
// "what did this member do here" is the point of the page — but resolves it
// among the project's people only: its current members and every account that
// has acted in it (ResolveProjectUser). Anything else, an account of another
// tenant as much as a name nobody has, filters on an actor no entry has and
// gets the same empty 200 a member who did nothing gets. The instance
// listing's strict 400 for an unknown name stays where only the system
// administrator reads: there the whole instance is the reader's business.
//
// The narrowed listing returns the full record, ip and user_agent included.
// Those are the caller's own request metadata — the address and browser they
// themselves acted from — and seeing them is how a user recognises (or disowns)
// an action, so withholding them would only make the page harder to trust.
//
// # Every page names its actors
//
// Each page carries actors: the display name of every account that acted in
// one of its entries (audit.Store.ActorNames). A project admin's directory
// lists only today's approved members, so the people who acted in the project
// and have since left — or who are still waiting for approval — used to show
// as raw uids. The map names exactly the actors of entries the reader was
// already allowed to read, so it is no oracle: a project admin learns the name
// of somebody who acted in the project and of nobody else, the same people
// the project's user filter resolves. On a project's listing only the people
// who still belong to it are named; an actor who has left (or never joined) is
// listed in former_actors and shown as a former member, so the project never
// reads a live name the account may have changed elsewhere since.
package auditapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// API serves the audit log over HTTP: the whole trail behind the maintainer
// guard, a project's entries behind its admin guard, and the caller's own
// actions behind the plain sign-in and membership guards.
type API struct {
	store             *audit.Store
	resolveUser       ResolveUser
	resolveProjUser   ResolveProjectUser
	resolveProject    ResolveProject
	requireMaintainer func(http.Handler) http.Handler
	requireAdmin      func(http.Handler) http.Handler
	requireAuth       func(http.Handler) http.Handler
	requireSignedIn   func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. All fields are required.
type Config struct {
	// Store reads audit entries.
	Store *audit.Store
	// ResolveUser turns the user filter's value — an account UID or a username —
	// into an account UID, so the trail can be filtered by the name a human
	// knows. Without it a user filter is answered 500 rather than guessed at.
	ResolveUser ResolveUser
	// ResolveProjectUser is ResolveUser for a project admin's listing: it
	// resolves the user filter only among that project's people, so the
	// listing is no oracle for the instance's other accounts. Without it a
	// user filter there is answered 500.
	ResolveProjectUser ResolveProjectUser
	// ResolveProject turns the instance listing's project filter — a slug —
	// into the project's uid. Without it a project filter is answered 500.
	ResolveProject ResolveProject
	// RequireMaintainer guards the instance listing of the whole trail.
	RequireMaintainer func(http.Handler) http.Handler
	// RequireAdmin guards a project's listing: its admins (and maintainers).
	RequireAdmin func(http.Handler) http.Handler
	// RequireAuth guards a member's own actions in a project.
	RequireAuth func(http.Handler) http.Handler
	// RequireSignedIn guards the instance own-activity listing, which any
	// signed-in account may read — member of a project or not — because it
	// never leaves its own actions.
	RequireSignedIn func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store:             cfg.Store,
		resolveUser:       cfg.ResolveUser,
		resolveProjUser:   cfg.ResolveProjectUser,
		resolveProject:    cfg.ResolveProject,
		requireMaintainer: cfg.RequireMaintainer,
		requireAdmin:      cfg.RequireAdmin,
		requireAuth:       cfg.RequireAuth,
		requireSignedIn:   cfg.RequireSignedIn,
	}
}

// RegisterRoutes mounts the instance audit endpoints onto r, which the caller
// has scoped under the API base path (for example /api/v1):
//
//	GET /audit        RequireMaintainer  the whole trail, filters (project= too) + pagination
//	GET /audit/mine   RequireSignedIn    the same, narrowed to the caller's own actions;
//	                                     user= and project= are ignored
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireMaintainer).Get("/audit", a.handleList)
	r.With(a.requireSignedIn).Get("/audit/mine", ownOnly(a.handleListMine))
}

// RegisterProjectRoutes mounts the project audit endpoints onto r, which the
// caller has scoped to one project (/api/v1/p/{project}):
//
//	GET /audit        RequireAdmin  the project's entries, filters + pagination
//	GET /audit/mine   RequireAuth   the same, narrowed to the caller's own actions
//
// Both are narrowed to the request's project on every page: the route already
// names the one project they read. The admin listing refuses a project
// parameter by name; the own-activity one ignores it, and user, like its
// instance twin.
func (a *API) RegisterProjectRoutes(r chi.Router) {
	r.With(a.requireAdmin).Get("/audit", a.inProject(a.handleList))
	r.With(a.requireAuth).Get("/audit/mine", ownOnly(a.inProject(a.handleListMine)))
}

// ownOnly strips the user and project parameters from the request before the
// own-activity listing reads it, so neither is ever resolved there: the answer
// is the same 200 whether they name an existing account or project, a foreign
// one or nothing at all (see "/audit/mine ignores user and project" in the
// package comment). Every other parameter passes through and is checked as
// usual.
func ownOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if !q.Has(userParam) && !q.Has(projectParam) {
			next(w, r)
			return
		}
		q.Del(userParam)
		q.Del(projectParam)
		u := *r.URL
		u.RawQuery = q.Encode()
		stripped := r.Clone(r.Context())
		stripped.URL = &u
		next(w, stripped)
	}
}

// noActor is the actor filter of a project listing whose user value names none
// of the project's people. It is no account uid (those are letters and digits
// only), so the listing runs the very same queries as for a member who did
// nothing and answers the very same empty page — an account elsewhere on the
// instance and one that does not exist are indistinguishable from it.
const noActor = "-"

// projectPeople returns the user resolver of the listing of the project
// projectUID: the account among the project's people (ResolveProjectUser), or
// noActor for anything else, instead of the instance listing's 400 for a value
// that names no account — that refusal, next to an empty 200 for an account
// that exists, told a tenant's admin which accounts exist on the instance.
func (a *API) projectPeople(projectUID string) ResolveUser {
	return func(ctx context.Context, value string) (string, error) {
		if a.resolveProjUser == nil {
			return "", fmt.Errorf("%w: no project user lookup is configured", errLookup)
		}
		uid, err := a.resolveProjUser(ctx, projectUID, value)
		if errors.Is(err, ErrUnknownUser) {
			return noActor, nil
		}
		return uid, err
	}
}

// listResponse is the JSON body returned by the list endpoint. NextOffset is the
// offset to request for the following page, or null on the last page. Actors
// names the accounts that acted in the page's entries (uid → display name); an
// account since deleted is absent, its entries already lost their actor. On a
// project's listing an actor who is no member of the project any more is absent
// from Actors too and listed in FormerActors instead (sorted, never null): the
// client says "former member" rather than show a live name the project no
// longer gets to read. The instance listings name every actor and leave it empty.
type listResponse struct {
	Entries      []audit.Record    `json:"entries"`
	Actors       map[string]string `json:"actors"`
	FormerActors []string          `json:"former_actors"`
	Total        int               `json:"total"`
	Limit        int               `json:"limit"`
	Offset       int               `json:"offset"`
	NextOffset   *int              `json:"next_offset"`
}

// handleList parses the query filters, reads the matching page of audit entries
// newest-first plus the total count, and writes them with the next-page offset
// for pagination. An invalid filter or pagination value is answered with 400 and
// a store failure with 500.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	filter, err := a.parseRequest(r)
	if err != nil {
		writeFilterError(w, err)
		return
	}
	a.respond(w, r, filter)
}

// projectScopeKey is the context key under which inProject records the
// project a listing is narrowed to.
type projectScopeKey struct{}

// inProject wraps a listing handler for a project route: it refuses a project
// parameter by name (the route names the project) — and instance_only, which
// would contradict it — and narrows the listing to
// the request's project, which the guard in front resolved. A request that
// reached it without a project is refused rather than listed unfiltered.
func (a *API) inProject(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, param := range []string{projectParam, instanceOnlyParam} {
			if r.URL.Query().Has(param) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown query parameter %q", param))
				return
			}
		}
		projectUID := auth.ProjectUIDFromContext(r.Context())
		if projectUID == "" {
			// Unreachable behind the project guards; refuse rather than list
			// every project's entries.
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), projectScopeKey{}, projectUID)))
	}
}

// parseRequest builds the request's filter: the query's filters, then the
// project — the route's (inProject) when it has one, which nothing in the query
// can widen, or else the query's project=<slug> or instance_only=true (the two
// exclude each other).
//
// In a project the user filter is resolved among that project's people only
// (projectPeople), so it answers a foreign or a missing account alike.
func (a *API) parseRequest(r *http.Request) (audit.Filter, error) {
	scoped, inProject := r.Context().Value(projectScopeKey{}).(string)
	resolve := a.resolveUser
	if inProject {
		resolve = a.projectPeople(scoped)
	}
	filter, err := parseFilter(r.Context(), r.URL.Query(), resolve)
	if err != nil {
		return audit.Filter{}, err
	}
	if inProject {
		filter.ProjectUID = scoped
		return filter, nil
	}
	if filter.InstanceOnly, err = parseInstanceOnly(r.URL.Query()); err != nil {
		return audit.Filter{}, err
	}
	filter.ProjectUID, err = resolveProjectFilter(r.Context(), r.URL.Query().Get(projectParam), a.resolveProject)
	if err != nil {
		return audit.Filter{}, err
	}
	return filter, nil
}

// handleListMine serves the caller's own actions: the same filters and paging as
// handleList, except the actor is taken from the authenticated session and
// overwrites whatever the filter holds, on every page. Entries with no actor
// (system actions) therefore never appear. It sits behind ownOnly, so the
// query's user and project never reach it; the overwrite below holds even for
// a route mounted without it.
func (a *API) handleListMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		// Unreachable behind RequireAuth; refuse rather than list unfiltered.
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, err := a.parseRequest(r)
	if err != nil {
		writeFilterError(w, err)
		return
	}
	filter.ActorUID = user.UID
	a.respond(w, r, filter)
}

// respond reads the page of entries matching filter plus the total count and
// the names of the page's actors, and writes them with the next-page offset. A
// store failure is answered with 500.
func (a *API) respond(w http.ResponseWriter, r *http.Request, filter audit.Filter) {
	entries, err := a.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing audit entries failed")
		return
	}
	total, err := a.store.Count(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting audit entries failed")
		return
	}
	if _, inProject := r.Context().Value(projectScopeKey{}).(string); inProject {
		entries = withoutInstanceRole(entries)
	}
	resp := buildResponse(filter, entries, total)
	resp.Actors, resp.FormerActors, err = a.actorNames(r, entries)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "naming audit actors failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// actorNames names the actors of entries. On a project's route (inProject) a
// former member of the project is reported apart, nameless; the instance
// listings — the system administrator's trail and one's own actions — name
// everybody, since nothing there is shown to a project.
func (a *API) actorNames(r *http.Request, entries []audit.Record) (map[string]string, []string, error) {
	if scoped, inProject := r.Context().Value(projectScopeKey{}).(string); inProject {
		names, former, err := a.store.ProjectActorNames(r.Context(), scoped, actorUIDs(entries))
		if err != nil {
			return nil, nil, fmt.Errorf("auditapi: naming project actors: %w", err)
		}
		return names, former, nil
	}
	names, err := a.store.ActorNames(r.Context(), actorUIDs(entries))
	if err != nil {
		return nil, nil, fmt.Errorf("auditapi: naming actors: %w", err)
	}
	return names, []string{}, nil
}

// instanceRoleDetail is the detail audit.Meta.Entry stamps on an entry whose
// actor acted on the strength of the instance role alone ("maintainer").
const instanceRoleDetail = "actor_role"

// withoutInstanceRole returns entries with the instance-role detail taken out
// of every entry's details, for a project's listing: a project must not learn
// that the system administrator role exists, so it must not read which of its
// entries were made by one. The entries' detail maps are copied, never
// modified, and an entry without the detail is returned as it is. The
// instance listings keep it, since only the system administrator reads them.
func withoutInstanceRole(entries []audit.Record) []audit.Record {
	for i, entry := range entries {
		if _, ok := entry.Details[instanceRoleDetail]; !ok {
			continue
		}
		details := maps.Clone(entry.Details)
		delete(details, instanceRoleDetail)
		entries[i].Details = details
	}
	return entries
}

// actorUIDs returns the distinct actors of entries, in first-seen order;
// entries without an actor (system actions, anonymous uploads) add none.
func actorUIDs(entries []audit.Record) []string {
	seen := make(map[string]struct{}, len(entries))
	uids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.ActorUID == nil {
			continue
		}
		if _, dup := seen[*entry.ActorUID]; dup {
			continue
		}
		seen[*entry.ActorUID] = struct{}{}
		uids = append(uids, *entry.ActorUID)
	}
	return uids
}

// buildResponse assembles the paginated list body, computing the effective limit
// and the next-page offset (nil on the last page).
func buildResponse(filter audit.Filter, entries []audit.Record, total int) listResponse {
	limit := filter.Limit
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	if entries == nil {
		// An empty page is an empty array, never null: a client iterates it.
		entries = []audit.Record{}
	}
	resp := listResponse{
		Entries: entries,
		Actors:  map[string]string{},
		Total:   total,
		Limit:   limit,
		Offset:  filter.Offset,
	}
	if next := filter.Offset + len(entries); next < total && len(entries) > 0 {
		resp.NextOffset = &next
	}
	return resp
}

// Pagination bounds mirror the audit store's own clamps so the reported limit
// matches what the store applied.
const (
	defaultLimit = 100
	maxLimit     = 500
)

// Review decision buckets accepted by the decision query parameter. "yes" maps
// to the confirmations (face.assign + label.attach), "no" to the rejections
// (face.reject + label.reject). They partition the four review actions the
// via=review filter admits, so the admin decision view can page Ano/Ne server-side.
const (
	decisionYes = "yes"
	decisionNo  = "no"
)

// recognisedParams is the closed set of query parameters the two listings
// accept. It exists so a misspelled or invented key is answered rather than
// ignored: silently dropping ?actor= or ?from= leaves the caller reading a
// listing that does not mean what they asked for. Every entry here is parsed
// below, and the frontend's audit client (web/src/services/audit.ts) sends
// nothing else.
var recognisedParams = map[string]struct{}{
	projectParam: {}, instanceOnlyParam: {}, userParam: {}, "entity_type": {}, "entity_uid": {}, "action": {},
	"via": {}, "decision": {}, "since": {}, "until": {},
	"limit": {}, "offset": {},
}

// ResolveUser maps the value of the user filter — an account UID or a username,
// because a human filtering the trail reaches for the name they know — onto that
// account's UID. It returns ErrUnknownUser when the value names no account, and
// any other error when the lookup itself failed.
//
// It is a function rather than a store so this package stays a read-only reader
// of the trail and does not grow a dependency on the whole user service.
type ResolveUser func(ctx context.Context, value string) (string, error)

// projectParam is the instance listing's project filter: a project's slug.
const projectParam = "project"

// userParam is the listings' actor filter: an account UID or a username.
const userParam = "user"

// instanceOnlyParam is the instance listing's filter for the entries filed
// under no project: "true" narrows to them, absent (or "false") does not.
const instanceOnlyParam = "instance_only"

// parseInstanceOnly reads the instance_only filter and refuses it next to a
// project filter, which it contradicts: an entry is filed under a project or
// under none, so the pair could only ever answer an empty page.
func parseInstanceOnly(q url.Values) (bool, error) {
	var on bool
	switch value := q.Get(instanceOnlyParam); value {
	case "", "false":
	case "true":
		on = true
	default:
		return false, fmt.Errorf("instance_only must be true or false, not %q", value)
	}
	if on && q.Get(projectParam) != "" {
		return false, errors.New("instance_only and project exclude each other")
	}
	return on, nil
}

// ResolveProjectUser maps the user filter of the project projectUID's listing
// onto an account UID like ResolveUser, but only among that project's people:
// its current members and the accounts that have acted in it. It returns
// ErrUnknownUser for every other value — an account elsewhere on the instance
// as much as one that does not exist — and any other error when the lookup
// itself failed.
type ResolveProjectUser func(ctx context.Context, projectUID, value string) (string, error)

// ResolveProject maps the project filter's value — a project's slug, the name
// in its URL — onto that project's uid. It returns ErrUnknownProject when the
// slug names no project, and any other error when the lookup itself failed.
type ResolveProject func(ctx context.Context, slug string) (string, error)

// ErrUnknownProject is returned by a ResolveProject for a slug that names no
// project.
var ErrUnknownProject = errors.New("auditapi: no such project")

// resolveProjectFilter turns the project filter's slug into a project uid. An
// empty value means "every project, and the instance" and needs no lookup; a
// slug naming no project is the caller's error, a failing lookup the server's
// (wrapped in errLookup).
func resolveProjectFilter(ctx context.Context, slug string, resolve ResolveProject) (string, error) {
	if slug == "" {
		return "", nil
	}
	if resolve == nil {
		return "", fmt.Errorf("%w: no project lookup is configured", errLookup)
	}
	uid, err := resolve(ctx, slug)
	switch {
	case errors.Is(err, ErrUnknownProject):
		return "", fmt.Errorf("project %q does not exist", slug)
	case err != nil:
		return "", fmt.Errorf("%w: %w", errLookup, err)
	}
	return uid, nil
}

// ErrUnknownUser is returned by a ResolveUser for a value that is neither an
// account UID nor a username.
var ErrUnknownUser = errors.New("auditapi: no such user")

// errLookup marks a failure of a lookup itself (the user or the project
// filter's) — the store could not be reached. It is the one filter error that
// is the server's fault rather than the caller's, so it is answered 500 while
// everything else is a 400.
var errLookup = errors.New("resolving a filter failed")

// parseFilter builds an audit.Filter from the request query parameters. Every
// value is checked, because an unchecked filter answers a typo with an empty
// list — indistinguishable from "this person did nothing".
//
// Recognised parameters: user, entity_type, entity_uid, action, via, decision,
// since, until, limit, offset; any other key is refused by name. The user value
// is resolved through resolve as a UID or a username, an unknown action is
// refused against audit's closed set, via accepts only "review" and decision
// only "yes" or "no", the timestamps must be RFC 3339 and the pagination
// non-negative integers.
//
// Two values stay deliberately unchecked. entity_uid is not resolved because the
// trail outlives what it describes — an entry about a purged photo is exactly
// what the trail is for — and entity_type is a free string by design, so an
// allow-list would go stale the moment a new kind of entity is audited.
func parseFilter(ctx context.Context, q url.Values, resolve ResolveUser) (audit.Filter, error) {
	if err := rejectUnknownParams(q); err != nil {
		return audit.Filter{}, err
	}
	filter := audit.Filter{
		TargetType: q.Get("entity_type"),
		TargetUID:  q.Get("entity_uid"),
	}
	if action := q.Get("action"); action != "" {
		if !audit.KnownAction(action) {
			return audit.Filter{}, fmt.Errorf("unknown action %q", action)
		}
		filter.Action = action
	}
	if err := parseReviewFilter(q, &filter); err != nil {
		return audit.Filter{}, err
	}
	if err := parseRange(q, &filter); err != nil {
		return audit.Filter{}, err
	}
	// The actor is resolved last: it is the only filter that reaches the
	// database, and there is no point paying for that behind a value the pure
	// checks above would have rejected anyway.
	actorUID, err := resolveActor(ctx, q.Get(userParam), resolve)
	if err != nil {
		return audit.Filter{}, err
	}
	filter.ActorUID = actorUID
	return filter, nil
}

// rejectUnknownParams returns an error naming the first unrecognised query key,
// in sorted order so the same request always gets the same message.
func rejectUnknownParams(q url.Values) error {
	unknown := make([]string, 0, len(q))
	for key := range q {
		if _, ok := recognisedParams[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	return fmt.Errorf("unknown query parameter %q", unknown[0])
}

// resolveActor turns the user filter's value into an actor UID, accepting either
// an account UID or a username. An empty value means "every actor" and needs no
// lookup; a value naming no account is a caller error, while a failing lookup is
// wrapped in errLookup so the handler can tell the two apart.
func resolveActor(ctx context.Context, value string, resolve ResolveUser) (string, error) {
	if value == "" {
		return "", nil
	}
	if resolve == nil {
		return "", fmt.Errorf("%w: no user lookup is configured", errLookup)
	}
	uid, err := resolve(ctx, value)
	switch {
	case errors.Is(err, ErrUnknownUser):
		return "", fmt.Errorf("user %q is neither an account uid nor a username", value)
	case err != nil:
		return "", fmt.Errorf("%w: %w", errLookup, err)
	}
	return uid, nil
}

// parseRange applies the created-at range and the pagination onto filter,
// returning an error for a timestamp that is not RFC 3339 or a limit/offset that
// is not a non-negative integer.
func parseRange(q url.Values, filter *audit.Filter) error {
	since, err := parseTime(q.Get("since"))
	if err != nil {
		return errors.New("since must be an RFC 3339 timestamp")
	}
	filter.Since = since
	until, err := parseTime(q.Get("until"))
	if err != nil {
		return errors.New("until must be an RFC 3339 timestamp")
	}
	filter.Until = until
	if filter.Limit, err = parseNonNegative(q.Get("limit")); err != nil {
		return errors.New("limit must be a non-negative integer")
	}
	if filter.Offset, err = parseNonNegative(q.Get("offset")); err != nil {
		return errors.New("offset must be a non-negative integer")
	}
	return nil
}

// parseReviewFilter applies the review-decision filters onto filter: via=review
// restricts to the review game's decisions and decision=yes|no to the Ano/Ne
// action bucket. It returns an error for an unsupported via or decision value.
func parseReviewFilter(q url.Values, filter *audit.Filter) error {
	switch via := q.Get("via"); via {
	case "":
	case "review":
		filter.ReviewOnly = true
	default:
		return errors.New("via filter only supports 'review'")
	}
	switch decision := q.Get("decision"); decision {
	case "":
	case decisionYes:
		filter.Actions = audit.ReviewYesActions()
	case decisionNo:
		filter.Actions = audit.ReviewNoActions()
	default:
		return errors.New("decision filter only supports 'yes' or 'no'")
	}
	return nil
}

// parseTime parses an optional RFC 3339 timestamp, returning nil for an empty
// value and an error for a malformed one.
func parseTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil //nolint:nilnil // absent filter is a legitimate nil value.
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("parsing timestamp %q: %w", value, err)
	}
	return &t, nil
}

// parseNonNegative parses an optional non-negative integer, returning 0 for an
// empty value and an error for a malformed or negative one.
func parseNonNegative(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, errors.New("invalid integer")
	}
	return n, nil
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("auditapi: encoding JSON response: %v", err)
	}
}

// writeFilterError answers a rejected filter: 500 when the user lookup itself
// failed — the server's fault, and nothing the caller can rewrite — and 400 with
// the parser's own message, which names the offending value, for everything else.
func writeFilterError(w http.ResponseWriter, err error) {
	if errors.Is(err, errLookup) {
		writeError(w, http.StatusInternalServerError, "resolving the filter failed")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
