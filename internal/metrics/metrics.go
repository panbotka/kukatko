// Package metrics exposes Prometheus instrumentation for the kukatko HTTP
// server, the background job worker, and supporting infrastructure (the pgx
// connection pool, the embeddings sidecar, imports, and thumbnail generation).
//
// All series live in a single isolated *prometheus.Registry rather than the
// process-global prometheus.DefaultRegisterer, so tests can construct
// independent metric surfaces without cross-test leakage. The serve command
// builds one Registry, mounts its Handler at /metrics, and hands its
// observation methods to the subsystems that emit events.
//
// The design mirrors photo-sorter's lightweight approach: a single namespace,
// bounded label sets (the HTTP route label is the chi route pattern, never the
// raw URL), and collectors that pull live stats (DB pool, queue depth) at
// scrape time instead of running extra goroutines.
package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// namespace is the metric name prefix shared by every series this binary
// exposes (e.g. kukatko_http_requests_total).
const namespace = "kukatko"

// metricsPath is the endpoint the registry's Handler is mounted at; the HTTP
// middleware skips it so a scrape never instruments itself.
const metricsPath = "/metrics"

// Registry bundles every kukatko metric so the HTTP middleware and background
// subsystems can take a single dependency instead of reaching into globals.
// Construct one with New; it is safe for concurrent use once built.
type Registry struct {
	reg *prometheus.Registry

	// HTTP middleware.
	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	httpInflight        prometheus.Gauge

	// Background job lifecycle.
	jobsStarted  *prometheus.CounterVec
	jobsFinished *prometheus.CounterVec
	jobDuration  *prometheus.HistogramVec

	// Embeddings sidecar.
	embeddingDuration *prometheus.HistogramVec
	embeddingUp       prometheus.Gauge

	// Import progress (latest checkpointed run tally per source/outcome).
	importProgress *prometheus.GaugeVec

	// Thumbnail generation.
	thumbnailDuration prometheus.Histogram
}

// New constructs a Registry with every series registered, including the
// standard Go runtime and process collectors so /metrics returns the usual
// "go_" and "process_" families without callers wiring them up.
func New() *Registry {
	r := &Registry{reg: prometheus.NewRegistry()}
	r.registerHTTP()
	r.registerJobs()
	r.registerExternal()
	r.reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return r
}

// registerHTTP creates and registers the HTTP request counter, latency
// histogram, and in-flight gauge populated by Middleware.
func (r *Registry) registerHTTP() {
	r.httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests served, partitioned by method, route pattern, and response status.",
	}, []string{"method", "route", "status"})
	r.httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration in seconds, partitioned by method and route pattern.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route"})
	r.httpInflight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "http",
		Name:      "inflight_requests",
		Help:      "Number of HTTP requests currently being served.",
	})
	r.reg.MustRegister(r.httpRequests, r.httpRequestDuration, r.httpInflight)
}

// registerJobs creates and registers the background-job lifecycle counters and
// the execution-duration histogram populated by JobStarted and JobFinished.
func (r *Registry) registerJobs() {
	r.jobsStarted = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "jobs",
		Name:      "started_total",
		Help:      "Total background jobs dispatched to a handler, partitioned by job type.",
	}, []string{"type"})
	r.jobsFinished = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "jobs",
		Name:      "finished_total",
		Help:      "Total background jobs that finished, partitioned by job type and outcome.",
	}, []string{"type", "outcome"})
	r.jobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "jobs",
		Name:      "execution_duration_seconds",
		Help:      "Background job execution time in seconds, partitioned by job type and outcome.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"type", "outcome"})
	r.reg.MustRegister(r.jobsStarted, r.jobsFinished, r.jobDuration)
}

// registerExternal creates and registers the embeddings sidecar, import
// progress, and thumbnail-generation series.
func (r *Registry) registerExternal() {
	r.embeddingDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "embedding",
		Name:      "request_duration_seconds",
		Help:      "Embeddings sidecar call duration in seconds, partitioned by operation and outcome.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation", "outcome"})
	r.embeddingUp = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "embedding",
		Name:      "service_up",
		Help:      "Whether the embeddings sidecar responded to its last call (1 = reachable, 0 = offline).",
	})
	r.importProgress = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "import",
		Name:      "run_photos",
		Help:      "Latest checkpointed import-run photo tally, partitioned by source and outcome.",
	}, []string{"source", "outcome"})
	r.thumbnailDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: "thumbnail",
		Name:      "generation_duration_seconds",
		Help:      "Wall-clock time to generate one thumbnail size in seconds.",
		Buckets:   prometheus.DefBuckets,
	})
	r.reg.MustRegister(r.embeddingDuration, r.embeddingUp, r.importProgress, r.thumbnailDuration)
}

// Handler returns an http.Handler that serves the registered metrics in the
// Prometheus text exposition format. Mount it at /metrics.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{Registry: r.reg})
}

// Gatherer exposes the underlying registry so tests can introspect a metric
// without rendering the full /metrics text output.
func (r *Registry) Gatherer() prometheus.Gatherer { return r.reg }

// Middleware wraps an http.Handler with the kukatko_http_* metrics: it tracks
// inflight_requests, increments requests_total, and samples
// request_duration_seconds. The /metrics endpoint is skipped so a scrape does
// not instrument itself.
//
// routeOf maps a request to its route label; pass routeLabel (or a chi-aware
// equivalent) so the label is the bounded route pattern rather than the raw
// URL path, which would explode cardinality. A nil routeOf falls back to the
// literal request path.
func (r *Registry) Middleware(routeOf func(*http.Request) string) func(http.Handler) http.Handler {
	if routeOf == nil {
		routeOf = func(req *http.Request) string { return req.URL.Path }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == metricsPath {
				next.ServeHTTP(w, req)
				return
			}
			r.httpInflight.Inc()
			defer r.httpInflight.Dec()

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, req)

			route := routeOf(req)
			r.httpRequests.WithLabelValues(req.Method, route, strconv.Itoa(rec.status)).Inc()
			r.httpRequestDuration.WithLabelValues(req.Method, route).Observe(time.Since(start).Seconds())
		})
	}
}

// statusRecorder is a minimal ResponseWriter wrapper that captures the response
// status code so the middleware can attribute it without requiring callers to
// use chi's WrapResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader captures the status code on the first call and forwards every
// call to the wrapped writer.
func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

// Write records an implicit 200 status when the handler writes a body without
// calling WriteHeader first (net/http's default behaviour).
func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	n, err := s.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("status recorder write: %w", err)
	}
	return n, nil
}
