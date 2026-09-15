// Package auditapi exposes the HTTP API over the durable audit trail
// (internal/audit). It serves two read endpoints. GET /audit is the admin-only
// listing of the whole trail, newest-first, with optional filters (acting user,
// entity type and UID, action, review-game decisions via=review, Ano/Ne bucket
// decision=yes|no, created-at date range) and limit/offset pagination, plus the
// total matching count so the admin UI can page. GET /audit/mine is the same
// listing for any signed-in user, narrowed to their own actions. The audit log
// is write-only from the application's side — entries are appended within
// mutation transactions elsewhere — so this package never mutates it.
//
// # Every filter is checked
//
// A reader's filters are validated strictly, and an unknown query key is refused
// by name rather than ignored. The reason is that this endpoint's empty answer
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
// A caller who asks for another user's actions there is answered 403 rather than
// silently served their own: quietly rewriting the request would leave the user
// believing they are looking at something they are not.
//
// The narrowed listing returns the full record, ip and user_agent included.
// Those are the caller's own request metadata — the address and browser they
// themselves acted from — and seeing them is how a user recognises (or disowns)
// an action, so withholding them would only make the page harder to trust.
package auditapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// API serves the audit log over HTTP: the whole trail behind the admin guard,
// and the caller's own actions behind the plain auth guard.
type API struct {
	store        *audit.Store
	resolveUser  ResolveUser
	requireAdmin func(http.Handler) http.Handler
	requireAuth  func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. All fields are required.
type Config struct {
	// Store reads audit entries.
	Store *audit.Store
	// ResolveUser turns the user filter's value — an account UID or a username —
	// into an account UID, so the trail can be filtered by the name a human
	// knows. Without it a user filter is answered 500 rather than guessed at.
	ResolveUser ResolveUser
	// RequireAdmin guards the full listing so only admins can read the trail.
	RequireAdmin func(http.Handler) http.Handler
	// RequireAuth guards the own-activity listing, which any signed-in user may
	// read because it never leaves their own actions.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store:        cfg.Store,
		resolveUser:  cfg.ResolveUser,
		requireAdmin: cfg.RequireAdmin,
		requireAuth:  cfg.RequireAuth,
	}
}

// RegisterRoutes mounts the audit endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET /audit        RequireAdmin   list audit entries with filters + pagination
//	GET /audit/mine   RequireAuth    the same, narrowed to the caller's own actions
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAdmin).Get("/audit", a.handleList)
	r.With(a.requireAuth).Get("/audit/mine", a.handleListMine)
}

// listResponse is the JSON body returned by the list endpoint. NextOffset is the
// offset to request for the following page, or null on the last page.
type listResponse struct {
	Entries    []audit.Record `json:"entries"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"next_offset"`
}

// handleList parses the query filters, reads the matching page of audit entries
// newest-first plus the total count, and writes them with the next-page offset
// for pagination. An invalid filter or pagination value is answered with 400 and
// a store failure with 500.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r.Context(), r.URL.Query(), a.resolveUser)
	if err != nil {
		writeFilterError(w, err)
		return
	}
	a.respond(w, r, filter)
}

// handleListMine serves the caller's own actions: the same filters and paging as
// handleList, except the actor is taken from the authenticated session and
// overwrites whatever the query asked for, on every page. Entries with no actor
// (system actions) therefore never appear. A user parameter naming somebody else
// is answered with 403 — the request is refused rather than quietly rewritten,
// so nobody reads a listing believing it is somebody else's; naming oneself is
// accepted and changes nothing. Since the user parameter also accepts a
// username, asking for somebody else by name is the same refusal — the value is
// resolved first, and it is the resolved account that is compared.
func (a *API) handleListMine(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		// Unreachable behind RequireAuth; refuse rather than list unfiltered.
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	filter, err := parseFilter(r.Context(), r.URL.Query(), a.resolveUser)
	if err != nil {
		writeFilterError(w, err)
		return
	}
	if filter.ActorUID != "" && filter.ActorUID != user.UID {
		writeError(w, http.StatusForbidden, "this listing only serves your own actions")
		return
	}
	filter.ActorUID = user.UID
	a.respond(w, r, filter)
}

// respond reads the page of entries matching filter plus the total count and
// writes them with the next-page offset. A store failure is answered with 500.
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
	writeJSON(w, http.StatusOK, buildResponse(filter, entries, total))
}

// buildResponse assembles the paginated list body, computing the effective limit
// and the next-page offset (nil on the last page).
func buildResponse(filter audit.Filter, entries []audit.Record, total int) listResponse {
	limit := filter.Limit
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	resp := listResponse{
		Entries: entries,
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
	"user": {}, "entity_type": {}, "entity_uid": {}, "action": {},
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

// ErrUnknownUser is returned by a ResolveUser for a value that is neither an
// account UID nor a username.
var ErrUnknownUser = errors.New("auditapi: no such user")

// errUserLookup marks a failure of the user lookup itself — the store could not
// be reached. It is the one filter error that is the server's fault rather than
// the caller's, so it is answered 500 while everything else is a 400.
var errUserLookup = errors.New("resolving the user filter failed")

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
	actorUID, err := resolveActor(ctx, q.Get("user"), resolve)
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
// wrapped in errUserLookup so the handler can tell the two apart.
func resolveActor(ctx context.Context, value string, resolve ResolveUser) (string, error) {
	if value == "" {
		return "", nil
	}
	if resolve == nil {
		return "", fmt.Errorf("%w: no user lookup is configured", errUserLookup)
	}
	uid, err := resolve(ctx, value)
	switch {
	case errors.Is(err, ErrUnknownUser):
		return "", fmt.Errorf("user %q is neither an account uid nor a username", value)
	case err != nil:
		return "", fmt.Errorf("%w: %w", errUserLookup, err)
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
	if errors.Is(err, errUserLookup) {
		writeError(w, http.StatusInternalServerError, "resolving the user filter failed")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
