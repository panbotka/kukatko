// Package importapi exposes the HTTP triggers for the read-only imports: the
// PhotoPrism import (pp_import), the photo-sorter direct-database migration
// (ps_migrate) and the photo-sorter feeds enrichment (ps_feeds_import). It does
// not run any inline — all are long-running and belong on the
// background worker — but enqueues a single job and returns its id. Each job's
// payload carries a fixed sentinel so the queue's dedup key allows only one of
// that kind to be queued or running at a time: triggering again while one is in
// flight is reported as a conflict, not a second run. Only the endpoints whose
// source is configured are registered.
//
// The triggers are guarded by an injected maintainer guard, which admits only
// the maintainer role (see auth.RequireMaintainer); imports are an operations
// capability at the top of the role ladder. The package depends only on a Queue
// behaviour and that guard, so it stays decoupled from the job store, the
// importers' wiring, and auth's role model.
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
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/psfeedsimport"
	"github.com/panbotka/kukatko/internal/psimport"
)

// Paging defaults for the run-history listing. The store clamps too, but
// validating here yields a clear 400 on a malformed query parameter.
const (
	// defaultRunsLimit is the page size used when the client omits limit.
	defaultRunsLimit = 50
	// maxRunsLimit caps the page size accepted from the client.
	maxRunsLimit = 200
)

// Queue enqueues background jobs. It is the import-facing subset of jobs.Store,
// satisfied by *jobs.Store.
type Queue interface {
	// Enqueue inserts a job of the given type and payload, returning
	// jobs.ErrDuplicate when an active job already exists for its dedup key.
	Enqueue(ctx context.Context, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions) (jobs.Job, error)
}

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

// Verifier runs an import-completeness reconciliation against the configured
// sources and returns a JSON-encodable report. It is optional — nil when no
// import source is configured, in which case the verify endpoint answers 503. It
// is declared as any-returning so this package stays decoupled from the
// reconciler's report shape; *importverify.Service is adapted onto it at wiring.
type Verifier interface {
	// Verify reconciles the sources against the catalogue, returning the report.
	Verify(ctx context.Context) (any, error)
}

// API exposes the import triggers over HTTP. The maintainer guard is supplied by
// the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	queue             Queue
	runs              RunLister
	failures          FailureLister
	verifier          Verifier
	requireMaintainer func(http.Handler) http.Handler
	rateLimit         func(http.Handler) http.Handler
	enablePhotoPrism  bool
	enablePhotoSorter bool
	enableFeeds       bool
}

// Config bundles the dependencies of NewAPI. Queue, Runs and RequireMaintainer are
// required; the Enable* flags select which source triggers are registered.
type Config struct {
	// Queue is the job queue the triggers enqueue jobs onto.
	Queue Queue
	// Runs reads the import-run history for the history endpoint.
	Runs RunLister
	// Failures reads the persisted per-photo/per-file import failures.
	Failures FailureLister
	// Verifier runs the completeness reconciliation; nil disables the verify
	// endpoint (503), which is the case when no import source is configured.
	Verifier Verifier
	// RequireMaintainer guards the endpoints for callers permitted to import (the
	// maintainer role only); imports are an operations capability.
	RequireMaintainer func(http.Handler) http.Handler
	// RateLimit is an optional per-client-IP throttle applied to the POST trigger
	// routes ahead of the import check. A nil value disables throttling.
	RateLimit func(http.Handler) http.Handler
	// EnablePhotoPrism registers POST /import/photoprism when set.
	EnablePhotoPrism bool
	// EnablePhotoSorter registers POST /import/photosorter when set.
	EnablePhotoSorter bool
	// EnableFeeds registers POST /import/photosorter-feeds when set.
	EnableFeeds bool
}

// NewAPI returns an API from cfg. A nil RateLimit disables throttling.
func NewAPI(cfg Config) *API {
	rateLimit := cfg.RateLimit
	if rateLimit == nil {
		rateLimit = passthroughMiddleware
	}
	return &API{
		queue:             cfg.queueOrPanic(),
		runs:              cfg.runsOrPanic(),
		failures:          cfg.failuresOrPanic(),
		verifier:          cfg.Verifier,
		requireMaintainer: cfg.RequireMaintainer,
		rateLimit:         rateLimit,
		enablePhotoPrism:  cfg.EnablePhotoPrism,
		enablePhotoSorter: cfg.EnablePhotoSorter,
		enableFeeds:       cfg.EnableFeeds,
	}
}

// passthroughMiddleware is a no-op middleware used when no rate limiter is configured.
func passthroughMiddleware(next http.Handler) http.Handler { return next }

// queueOrPanic returns the configured queue, panicking on a nil one since a
// missing queue is a wiring bug that should surface at startup.
func (c Config) queueOrPanic() Queue {
	if c.Queue == nil {
		panic("importapi: NewAPI requires a Queue")
	}
	return c.Queue
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
// under the API base path (for example /api/v1). The history endpoint is always
// registered so the operations UI can render past runs even when no source is
// configured; the triggers are registered only for configured sources. Every
// route is behind the maintainer guard:
//
//	GET  /import/runs               RequireMaintainer  recent import-run history + enabled sources
//	GET  /import/failures           RequireMaintainer  recorded per-photo/per-file import failures
//	GET  /import/verify             RequireMaintainer  completeness reconciliation report (503 if unconfigured)
//	POST /import/photoprism         RequireMaintainer  enqueue a PhotoPrism import job
//	POST /import/photosorter        RequireMaintainer  enqueue a photo-sorter migration job
//	POST /import/photosorter-feeds  RequireMaintainer  enqueue a photo-sorter feeds import job
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireMaintainer).Get("/import/runs", a.handleListRuns)
	r.With(a.requireMaintainer).Get("/import/failures", a.handleListFailures)
	r.With(a.requireMaintainer).Get("/import/verify", a.handleVerify)
	if a.enablePhotoPrism {
		r.With(a.rateLimit, a.requireMaintainer).Post("/import/photoprism", a.handleImportPhotoPrism)
	}
	if a.enablePhotoSorter {
		r.With(a.rateLimit, a.requireMaintainer).Post("/import/photosorter", a.handleImportPhotoSorter)
	}
	if a.enableFeeds {
		r.With(a.rateLimit, a.requireMaintainer).Post("/import/photosorter-feeds", a.handleImportFeeds)
	}
}

// sources reports which import sources are configured, so the operations UI can show
// or disable each section.
type sources struct {
	PhotoPrism  bool `json:"photoprism"`
	PhotoSorter bool `json:"photosorter"`
	Feeds       bool `json:"photosorter_feeds"`
}

// runsResponse is the JSON body of the run-history endpoint: a page of runs plus
// the echoed paging and the set of configured sources.
type runsResponse struct {
	Runs    []importer.Run `json:"runs"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
	Sources sources        `json:"sources"`
}

// handleListRuns returns a page of import-run history across all sources, most
// recently started first, together with which sources are configured. A
// malformed limit or offset is answered with 400.
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
	writeJSON(w, http.StatusOK, runsResponse{
		Runs:   runs,
		Limit:  limit,
		Offset: offset,
		Sources: sources{
			PhotoPrism:  a.enablePhotoPrism,
			PhotoSorter: a.enablePhotoSorter,
			Feeds:       a.enableFeeds,
		},
	})
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

// handleVerify runs the import-completeness reconciliation and returns its report.
// It answers 503 when no verifier is configured (no import source) and 502 when
// reconciling against a source failed.
func (a *API) handleVerify(w http.ResponseWriter, r *http.Request) {
	if a.verifier == nil {
		writeError(w, http.StatusServiceUnavailable, "import verification is not available (no source configured)")
		return
	}
	report, err := a.verifier.Verify(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "import verification failed")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// importResponse is the JSON body returned when an import job is enqueued.
type importResponse struct {
	// JobID is the queued job's id.
	JobID int64 `json:"job_id"`
	// Status is the queued job's state ("queued").
	Status string `json:"status"`
}

// handleImportPhotoPrism enqueues a single pp_import job. An import already in
// flight (the dedup sentinel collides) is reported as 409 Conflict; the queued
// job is reported as 202 Accepted with its id.
func (a *API) handleImportPhotoPrism(w http.ResponseWriter, r *http.Request) {
	a.enqueue(w, r, jobs.TypePPImport, ppimport.JobPayload(), "a photoprism import is already in progress")
}

// handleImportPhotoSorter enqueues a single ps_migrate job, with the same
// conflict and accepted semantics as the PhotoPrism trigger.
func (a *API) handleImportPhotoSorter(w http.ResponseWriter, r *http.Request) {
	a.enqueue(w, r, jobs.TypePSMigrate, psimport.JobPayload(), "a photo-sorter migration is already in progress")
}

// handleImportFeeds enqueues a single ps_feeds_import job (enrich imported photos
// with photo-sorter's 1:1 embeddings and faces), with the same conflict and
// accepted semantics as the other triggers.
func (a *API) handleImportFeeds(w http.ResponseWriter, r *http.Request) {
	a.enqueue(w, r, jobs.TypePSFeedsImport, psfeedsimport.JobPayload(),
		"a photo-sorter feeds import is already in progress")
}

// enqueue inserts a singleton import job and writes the HTTP response: 409 on a
// dedup conflict (using conflictMsg), 500 on any other failure, or 202 with the
// queued job's id on success.
func (a *API) enqueue(
	w http.ResponseWriter, r *http.Request, jobType string, payload json.RawMessage, conflictMsg string,
) {
	job, err := a.queue.Enqueue(r.Context(), jobType, payload, jobs.EnqueueOptions{MaxAttempts: 3})
	if errors.Is(err, jobs.ErrDuplicate) {
		writeError(w, http.StatusConflict, conflictMsg)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueuing import failed")
		return
	}
	writeJSON(w, http.StatusAccepted, importResponse{JobID: job.ID, Status: string(job.State)})
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
