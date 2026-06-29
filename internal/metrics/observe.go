package metrics

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Outcome label values shared by the job and embedding observation methods.
const (
	// OutcomeSuccess marks an operation that completed without error.
	OutcomeSuccess = "success"
	// OutcomeError marks an operation that ended in an error.
	OutcomeError = "error"
	// OutcomeDeferred marks a job requeued without a burned attempt (the box
	// was offline, so the handler asked to retry later).
	OutcomeDeferred = "deferred"
)

// routeLabel returns the bounded HTTP route label for req: the chi route
// pattern (for example "/api/v1/photos/{uid}") when routing matched one,
// otherwise the constant "unmatched". It is the routeOf argument the serve
// command passes to Middleware so the route label can never be a raw,
// unbounded URL path. Pass it post-handler: chi populates the pattern as
// routing descends, so it is only complete once next.ServeHTTP has returned.
func routeLabel(req *http.Request) string {
	if rc := chi.RouteContext(req.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

// RouteLabel is the exported route-labelling helper the serve command wires
// into Middleware; see routeLabel for the contract.
func RouteLabel(req *http.Request) string { return routeLabel(req) }

// JobStarted records that a job of jobType was dispatched to its handler. It
// satisfies the worker's metrics-observer contract.
func (r *Registry) JobStarted(jobType string) {
	r.jobsStarted.WithLabelValues(jobType).Inc()
}

// JobFinished records that a job of jobType finished with the given outcome
// (one of OutcomeSuccess, OutcomeError, OutcomeDeferred) after running for d.
// It satisfies the worker's metrics-observer contract.
func (r *Registry) JobFinished(jobType, outcome string, d time.Duration) {
	r.jobsFinished.WithLabelValues(jobType, outcome).Inc()
	r.jobDuration.WithLabelValues(jobType, outcome).Observe(d.Seconds())
}

// ObserveEmbeddingCall records the latency and outcome of one embeddings
// sidecar call. operation names the call ("image", "text", "face"); err is the
// call's error (nil means success). It satisfies embedding.Observer.
func (r *Registry) ObserveEmbeddingCall(operation string, d time.Duration, err error) {
	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeError
	}
	r.embeddingDuration.WithLabelValues(operation, outcome).Observe(d.Seconds())
}

// SetEmbeddingUp records the embeddings sidecar's reachability: true when the
// last call reached the box, false when it was offline. It satisfies
// embedding.Observer.
func (r *Registry) SetEmbeddingUp(up bool) {
	if up {
		r.embeddingUp.Set(1)
		return
	}
	r.embeddingUp.Set(0)
}

// SetImportProgress publishes the latest checkpointed photo tally of an import
// run for source ("photoprism" or "photosorter"). It satisfies the import
// services' progress-observer contract.
func (r *Registry) SetImportProgress(source string, imported, updated, skipped, failed int) {
	r.importProgress.WithLabelValues(source, "imported").Set(float64(imported))
	r.importProgress.WithLabelValues(source, "updated").Set(float64(updated))
	r.importProgress.WithLabelValues(source, "skipped").Set(float64(skipped))
	r.importProgress.WithLabelValues(source, "failed").Set(float64(failed))
}

// ObserveThumbnail records the wall-clock time to generate one thumbnail size.
// It satisfies the thumbnailer's observer contract.
func (r *Registry) ObserveThumbnail(d time.Duration) {
	r.thumbnailDuration.Observe(d.Seconds())
}
