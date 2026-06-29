package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// collectTimeout bounds the queue-depth queries a scrape triggers so a slow or
// unavailable database cannot block /metrics indefinitely.
const collectTimeout = 5 * time.Second

// RegisterDBPool installs a collector that reads the pgx pool's live stats on
// every scrape and exports them as gauges/counters. A nil pool is a no-op so
// startup paths that delay pool creation can defer wiring.
func (r *Registry) RegisterDBPool(pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	r.reg.MustRegister(newDBPoolCollector(pool))
}

// dbPoolCollector exports pgxpool.Stat as Prometheus metrics. It implements
// prometheus.Collector directly so every scrape sees the exact pool state at
// scrape time without an extra goroutine.
type dbPoolCollector struct {
	pool *pgxpool.Pool

	total       *prometheus.Desc
	acquired    *prometheus.Desc
	idle        *prometheus.Desc
	maxConns    *prometheus.Desc
	acquireWait *prometheus.Desc
	emptyAcq    *prometheus.Desc
}

// newDBPoolCollector wires the descriptors for the pool stats that matter
// operationally: total/acquired/idle for capacity, max_conns as a static
// reference, and the acquire-wait/empty-acquire counters for saturation alerts.
func newDBPoolCollector(pool *pgxpool.Pool) *dbPoolCollector {
	const subsystem = "db_pool"
	desc := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, name), help, nil, nil)
	}
	return &dbPoolCollector{
		pool:        pool,
		total:       desc("total_conns", "Total connections currently established (acquired + idle + constructing)."),
		acquired:    desc("acquired_conns", "Connections currently checked out by the application."),
		idle:        desc("idle_conns", "Idle connections available in the pool."),
		maxConns:    desc("max_conns", "Maximum number of connections the pool allows."),
		acquireWait: desc("acquire_wait_seconds_total", "Cumulative time spent blocked waiting to acquire a connection."),
		emptyAcq:    desc("empty_acquire_count_total", "Cumulative acquires that had to wait for a new or freed connection."),
	}
}

// Describe implements prometheus.Collector, declaring every descriptor up front
// so the registry can detect duplicate registrations early.
func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.total
	ch <- c.acquired
	ch <- c.idle
	ch <- c.maxConns
	ch <- c.acquireWait
	ch <- c.emptyAcq
}

// Collect implements prometheus.Collector. pgxpool.Stat is a cheap atomic
// snapshot, so sampling it on every scrape is fine.
func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(stat.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.acquired, prometheus.GaugeValue, float64(stat.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(stat.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.acquireWait, prometheus.CounterValue, stat.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.emptyAcq, prometheus.CounterValue, float64(stat.EmptyAcquireCount()))
}

// QueueDepthFunc returns the current job count grouped by a dimension (queue
// state or job type). The serve command adapts jobs.Store.CountsByState and
// CountsByType to this signature.
type QueueDepthFunc func(ctx context.Context) (map[string]int, error)

// RegisterJobQueue installs a collector exporting job queue depth by state and
// by type, sampled on every scrape via the supplied functions. Either function
// may be nil to skip that dimension; both nil is a no-op.
func (r *Registry) RegisterJobQueue(byState, byType QueueDepthFunc) {
	if byState == nil && byType == nil {
		return
	}
	r.reg.MustRegister(&queueDepthCollector{byState: byState, byType: byType})
}

// queueDepthCollector exports job queue depth as gauges pulled at scrape time.
type queueDepthCollector struct {
	byState QueueDepthFunc
	byType  QueueDepthFunc
}

// stateDesc and typeDesc describe the two queue-depth gauge families.
var (
	stateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "jobs", "queue_depth"),
		"Number of jobs in the queue, partitioned by state.", []string{"state"}, nil)
	typeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "jobs", "queue_depth_by_type"),
		"Number of jobs in the queue, partitioned by type.", []string{"type"}, nil)
)

// Describe implements prometheus.Collector.
func (c *queueDepthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- stateDesc
	ch <- typeDesc
}

// Collect implements prometheus.Collector, sampling both queue-depth functions
// within collectTimeout. A query error drops that dimension for the scrape
// rather than failing the whole /metrics response.
func (c *queueDepthCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()
	emitDepth(ctx, ch, c.byState, stateDesc)
	emitDepth(ctx, ch, c.byType, typeDesc)
}

// emitDepth samples one queue-depth function and emits a gauge per label value.
// A nil function or a query error emits nothing for that dimension.
func emitDepth(ctx context.Context, ch chan<- prometheus.Metric, fn QueueDepthFunc, desc *prometheus.Desc) {
	if fn == nil {
		return
	}
	counts, err := fn(ctx)
	if err != nil {
		return
	}
	for label, n := range counts {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(n), label)
	}
}
