// Package importapi exposes the read-only HTTP views over the import
// bookkeeping: the history of import runs and the per-photo/per-file failures
// they recorded. Both are fed by `kukatko import dir` (internal/dirimport) and,
// historically, by the PhotoPrism and photo-sorter migration that finished in
// August 2026 — its rows stay in import_runs as the catalogue's provenance
// record even though the importers themselves are gone. There is nothing to
// trigger here: the only remaining import runs from the CLI.
//
// The endpoints are guarded by an injected maintainer guard, which admits only
// the maintainer role (see auth.RequireMaintainer); imports are an operations
// capability at the top of the role ladder. The package depends only on that
// guard and two read behaviours, so it stays decoupled from the importer's
// wiring and auth's role model.
package importapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/importer"
)

// Paging defaults for the run-history listing. The store clamps too, but
// validating here yields a clear 400 on a malformed query parameter.
const (
	// defaultRunsLimit is the page size used when the client omits limit.
	defaultRunsLimit = 50
	// maxRunsLimit caps the page size accepted from the client.
	maxRunsLimit = 200
)

// RunLister reads the import-run history for the run-history view. It is the
// import-facing subset of importer.Store, satisfied by *importer.Store.
type RunLister interface {
	// List returns a page of import runs across every source, most recently
	// started first.
	List(ctx context.Context, limit, offset int) ([]importer.Run, error)
}

// FailureLister reads the persisted per-photo/per-file import failures for the
// failures view. It is the import-facing subset of importer.Store, satisfied by
// *importer.Store.
type FailureLister interface {
	// ListFailures returns a page of recorded import failures matching the filter,
	// most recently recorded first.
	ListFailures(ctx context.Context, filter importer.FailureFilter) ([]importer.Failure, error)
}

// API exposes the import bookkeeping over HTTP. The maintainer guard is supplied
// by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	runs              RunLister
	failures          FailureLister
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. All three are required.
type Config struct {
	// Runs reads the import-run history for the history endpoint.
	Runs RunLister
	// Failures reads the persisted per-photo/per-file import failures.
	Failures FailureLister
	// RequireMaintainer guards the endpoints for callers permitted to see import
	// bookkeeping (the maintainer role only); imports are an operations capability.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		runs:              cfg.runsOrPanic(),
		failures:          cfg.failuresOrPanic(),
		requireMaintainer: cfg.RequireMaintainer,
	}
}

// runsOrPanic returns the configured run lister, panicking on a nil one since a
// missing store is a wiring bug that should surface at startup.
func (c Config) runsOrPanic() RunLister {
	if c.Runs == nil {
		panic("importapi: NewAPI requires a Runs store")
	}
	return c.Runs
}

// failuresOrPanic returns the configured failure lister, panicking on a nil one
// since a missing store is a wiring bug that should surface at startup.
func (c Config) failuresOrPanic() FailureLister {
	if c.Failures == nil {
		panic("importapi: NewAPI requires a Failures store")
	}
	return c.Failures
}

// RegisterRoutes mounts the import endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1). Both are behind the maintainer
// guard:
//
//	GET  /import/runs               RequireMaintainer  recent import-run history
//	GET  /import/failures           RequireMaintainer  recorded per-photo/per-file import failures
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireMaintainer).Get("/import/runs", a.handleListRuns)
	r.With(a.requireMaintainer).Get("/import/failures", a.handleListFailures)
}

// runsResponse is the JSON body of the run-history endpoint: a page of runs plus
// the echoed paging.
type runsResponse struct {
	Runs   []importer.Run `json:"runs"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// handleListRuns returns a page of import-run history across all sources, most
// recently started first. A malformed limit or offset is answered with 400.
func (a *API) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePaging(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	runs, err := a.runs.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing import runs failed")
		return
	}
	if runs == nil {
		runs = []importer.Run{}
	}
	writeJSON(w, http.StatusOK, runsResponse{Runs: runs, Limit: limit, Offset: offset})
}

// errInvalidLimit and errInvalidOffset are returned by parsePaging for malformed
// paging query parameters.
var (
	errInvalidLimit  = errors.New("invalid limit")
	errInvalidOffset = errors.New("invalid offset")
)

// parsePaging reads the limit and offset query parameters, applying the default
// limit when absent and capping it at maxRunsLimit. It returns errInvalidLimit
// or errInvalidOffset when a value is present but not a valid non-negative (for
// offset) or positive (for limit) integer.
func parsePaging(q url.Values) (limit, offset int, err error) {
	limit = defaultRunsLimit
	if raw := q.Get("limit"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 1 {
			return 0, 0, errInvalidLimit
		}
		if n > maxRunsLimit {
			n = maxRunsLimit
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return 0, 0, errInvalidOffset
		}
		offset = n
	}
	return limit, offset, nil
}

// failuresResponse is the JSON body of the failures endpoint: a page of recorded
// import failures plus the echoed paging.
type failuresResponse struct {
	Failures []importer.Failure `json:"failures"`
	Limit    int                `json:"limit"`
	Offset   int                `json:"offset"`
}

// handleListFailures returns a page of persisted per-photo/per-file import
// failures, most recently recorded first, filtered by the optional query
// parameters ?source=, ?run_id=, ?unresolved=true and paginated by ?limit=/?offset=.
// A malformed parameter is answered with 400.
func (a *API) handleListFailures(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePaging(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter, err := parseFailureFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter.Limit, filter.Offset = limit, offset
	failures, err := a.failures.ListFailures(r.Context(), filter)
	if errors.Is(err, importer.ErrInvalidSource) {
		writeError(w, http.StatusBadRequest, "invalid source")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing import failures failed")
		return
	}
	if failures == nil {
		failures = []importer.Failure{}
	}
	writeJSON(w, http.StatusOK, failuresResponse{Failures: failures, Limit: limit, Offset: offset})
}

// errInvalidRunID is returned by parseFailureFilter for a malformed run_id.
var errInvalidRunID = errors.New("invalid run_id")

// parseFailureFilter reads the failures-listing filter query parameters (source,
// run_id, unresolved), leaving Limit/Offset to the caller. It returns
// errInvalidRunID for a malformed run_id; an unrecognised source is left for the
// store to reject so the error text stays in one place.
func parseFailureFilter(q url.Values) (importer.FailureFilter, error) {
	filter := importer.FailureFilter{}
	if raw := q.Get("source"); raw != "" {
		filter.Source = importer.Source(raw)
	}
	if raw := q.Get("run_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			return importer.FailureFilter{}, errInvalidRunID
		}
		filter.RunID = id
	}
	filter.UnresolvedOnly = q.Get("unresolved") == "true"
	return filter, nil
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
		log.Printf("importapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
