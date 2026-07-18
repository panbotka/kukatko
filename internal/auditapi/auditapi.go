// Package auditapi exposes the admin-only HTTP API over the durable audit trail
// (internal/audit). It serves a single read endpoint, GET /audit, that lists
// audit entries newest-first with optional filters (acting user, entity type and
// UID, action, review-game decisions via=review, Ano/Ne bucket decision=yes|no,
// created-at date range) and limit/offset pagination, plus the total matching
// count so the admin UI can page. The via=review and decision filters back the
// admin per-user review-decision view. The audit log is write-only
// from the application's side — entries are appended within mutation
// transactions elsewhere — so this package never mutates it.
package auditapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// API serves the audit log over HTTP behind the admin guard.
type API struct {
	store        *audit.Store
	requireAdmin func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Both fields are required.
type Config struct {
	// Store reads audit entries.
	Store *audit.Store
	// RequireAdmin guards the endpoint so only admins can read the trail.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireAdmin: cfg.RequireAdmin}
}

// RegisterRoutes mounts the audit endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET /audit   RequireAdmin   list audit entries with filters + pagination
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAdmin).Get("/audit", a.handleList)
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
	filter, err := parseFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
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

// parseFilter builds an audit.Filter from the request query parameters,
// validating the date range (RFC 3339) and the numeric pagination. Recognised
// parameters: user, entity_type, entity_uid, action, via, decision, since,
// until, limit, offset. The via parameter accepts only "review" (restricting to
// the review game's decisions); the decision parameter accepts "yes" or "no"
// (the Ano/Ne action buckets); any other non-empty value is rejected. It returns
// an error for a malformed value.
func parseFilter(q queryValues) (audit.Filter, error) {
	filter := audit.Filter{
		ActorUID:   q.Get("user"),
		TargetType: q.Get("entity_type"),
		TargetUID:  q.Get("entity_uid"),
		Action:     q.Get("action"),
	}
	if err := parseReviewFilter(q, &filter); err != nil {
		return audit.Filter{}, err
	}
	since, err := parseTime(q.Get("since"))
	if err != nil {
		return audit.Filter{}, errors.New("since must be an RFC 3339 timestamp")
	}
	filter.Since = since
	until, err := parseTime(q.Get("until"))
	if err != nil {
		return audit.Filter{}, errors.New("until must be an RFC 3339 timestamp")
	}
	filter.Until = until
	if filter.Limit, err = parseNonNegative(q.Get("limit")); err != nil {
		return audit.Filter{}, errors.New("limit must be a non-negative integer")
	}
	if filter.Offset, err = parseNonNegative(q.Get("offset")); err != nil {
		return audit.Filter{}, errors.New("offset must be a non-negative integer")
	}
	return filter, nil
}

// parseReviewFilter applies the review-decision filters onto filter: via=review
// restricts to the review game's decisions and decision=yes|no to the Ano/Ne
// action bucket. It returns an error for an unsupported via or decision value.
func parseReviewFilter(q queryValues, filter *audit.Filter) error {
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
		filter.Actions = []string{audit.ActionFaceAssign, audit.ActionLabelAttach}
	case decisionNo:
		filter.Actions = []string{audit.ActionFaceReject, audit.ActionLabelReject}
	default:
		return errors.New("decision filter only supports 'yes' or 'no'")
	}
	return nil
}

// queryValues is the subset of url.Values parseFilter needs, so it can be tested
// without constructing a request.
type queryValues interface {
	Get(key string) string
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

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
