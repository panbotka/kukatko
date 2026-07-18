// Package importapi exposes the HTTP triggers for the read-only imports: the
// PhotoPrism import (pp_import) and the photo-sorter migration (ps_migrate). It
// does not run either inline — both are long-running and belong on the
// background worker — but enqueues a single job and returns its id. Each job's
// payload carries a fixed sentinel so the queue's dedup key allows only one of
// that kind to be queued or running at a time: triggering again while one is in
// flight is reported as a conflict, not a second run. Only the endpoints whose
// source is configured are registered.
//
// The triggers are guarded by an injected import guard, which admits only the
// maintainer role (see auth.RequireImport); imports are an operations capability
// at the top of the role ladder. The package depends only on a Queue behaviour
// and that guard, so it stays decoupled from the job store, the importers'
// wiring, and auth's role model.
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

// RunLister reads the import-run history for the admin history view. It is the
// import-facing subset of importer.Store, satisfied by *importer.Store.
type RunLister interface {
	// List returns a page of import runs across every source, most recently
	// started first.
	List(ctx context.Context, limit, offset int) ([]importer.Run, error)
}

// API exposes the import triggers over HTTP. The import guard is supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not
// its wiring.
type API struct {
	queue             Queue
	runs              RunLister
	requireImport     func(http.Handler) http.Handler
	rateLimit         func(http.Handler) http.Handler
	enablePhotoPrism  bool
	enablePhotoSorter bool
}

// Config bundles the dependencies of NewAPI. Queue, Runs and RequireImport are
// required; the Enable* flags select which source triggers are registered.
type Config struct {
	// Queue is the job queue the triggers enqueue jobs onto.
	Queue Queue
	// Runs reads the import-run history for the history endpoint.
	Runs RunLister
	// RequireImport guards the endpoints for callers permitted to import (the
	// maintainer role only); imports are an operations capability.
	RequireImport func(http.Handler) http.Handler
	// RateLimit is an optional per-client-IP throttle applied to the POST trigger
	// routes ahead of the import check. A nil value disables throttling.
	RateLimit func(http.Handler) http.Handler
	// EnablePhotoPrism registers POST /import/photoprism when set.
	EnablePhotoPrism bool
	// EnablePhotoSorter registers POST /import/photosorter when set.
	EnablePhotoSorter bool
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
		requireImport:     cfg.RequireImport,
		rateLimit:         rateLimit,
		enablePhotoPrism:  cfg.EnablePhotoPrism,
		enablePhotoSorter: cfg.EnablePhotoSorter,
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

// RegisterRoutes mounts the import endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1). The history endpoint is always
// registered so the admin UI can render past runs even when no source is
// configured; the triggers are registered only for configured sources. Every
// route is behind the import guard (maintainer only):
//
//	GET  /import/runs         RequireImport  recent import-run history + enabled sources
//	POST /import/photoprism   RequireImport  enqueue a PhotoPrism import job
//	POST /import/photosorter  RequireImport  enqueue a photo-sorter migration job
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireImport).Get("/import/runs", a.handleListRuns)
	if a.enablePhotoPrism {
		r.With(a.rateLimit, a.requireImport).Post("/import/photoprism", a.handleImportPhotoPrism)
	}
	if a.enablePhotoSorter {
		r.With(a.rateLimit, a.requireImport).Post("/import/photosorter", a.handleImportPhotoSorter)
	}
}

// sources reports which import sources are configured, so the admin UI can show
// or disable each section.
type sources struct {
	PhotoPrism  bool `json:"photoprism"`
	PhotoSorter bool `json:"photosorter"`
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
		Runs:    runs,
		Limit:   limit,
		Offset:  offset,
		Sources: sources{PhotoPrism: a.enablePhotoPrism, PhotoSorter: a.enablePhotoSorter},
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
