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
	// OutcomeTerminal marks a job that failed permanently: the handler declared
	// that no retry can succeed, so the job was parked as failed at once. Kept
	// apart from OutcomeError, which may well succeed on the next attempt.
	OutcomeTerminal = "terminal"
)

// jobOutcomes is every outcome a finished job can be reported under, in the
// order the job families are pre-initialised with.
var jobOutcomes = []string{OutcomeSuccess, OutcomeError, OutcomeDeferred, OutcomeTerminal}

// callOutcomes is every outcome an embeddings call or a streaming encode can be
// reported under.
var callOutcomes = []string{OutcomeSuccess, OutcomeError}

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
// (one of OutcomeSuccess, OutcomeError, OutcomeDeferred, OutcomeTerminal) after
// running for d.
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

// StaleLocksRecovered records that n running jobs were requeued because their
// lock went stale. It satisfies the worker's metrics-observer contract; a
// non-positive n records nothing.
func (r *Registry) StaleLocksRecovered(n int64) {
	if n > 0 {
		r.staleLocks.Add(float64(n))
	}
}

// SetEmbeddingUp records the reachability of one embeddings sidecar target:
// true when it answered its latest health probe or call, false when it was
// offline. target is a small fixed vocabulary naming the role of the host
// ("box", "text"), never a URL. It satisfies embedding.Observer.
func (r *Registry) SetEmbeddingUp(target string, up bool) {
	value := 0.0
	if up {
		value = 1
	}
	r.embeddingUp.WithLabelValues(target).Set(value)
}

// GeocodeCreditSpent records that the places job spent one mapy.com
// reverse-geocode credit. Every credit is metered money, so this counter is the
// live view of what an import run is spending; it satisfies
// placesjob.CreditMeter.
func (r *Registry) GeocodeCreditSpent() {
	r.geocodeCredits.Inc()
}

// ObserveThumbnail records the wall-clock time to generate one thumbnail size.
// It satisfies the thumbnailer's observer contract.
func (r *Registry) ObserveThumbnail(d time.Duration) {
	r.thumbnailDuration.Observe(d.Seconds())
}

// ObserveRenditionEncode records that one streaming rendition of one video
// finished: how long it took, how many bytes of segments it published, which
// rendition it was and whether it succeeded. It satisfies hlsjob.Observer.
//
// The rendition name and the outcome are the only labels: /metrics is
// unauthenticated, so nothing naming the video may become a label value, and
// both label sets are small and fixed by the encoder's plan.
func (r *Registry) ObserveRenditionEncode(rendition, outcome string, d time.Duration, written int64) {
	r.encodeDuration.WithLabelValues(rendition, outcome).Observe(d.Seconds())
	r.encodeOutputBytes.WithLabelValues(rendition, outcome).Add(float64(written))
}

// ObserveEncodedSource records that a clip of length d has been through the
// encoder. It satisfies hlsjob.Observer.
//
// It is a counter of footage rather than a ready-made ratio on purpose: a ratio
// can be neither aggregated across instances nor rated over a window, whereas
// this divided into the encode-duration sum answers "what does a minute of video
// cost to encode?" for whatever window the query asks about.
func (r *Registry) ObserveEncodedSource(d time.Duration) {
	r.encodeSourceSecond.Add(d.Seconds())
}
