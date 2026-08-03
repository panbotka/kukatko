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

// QueueCell identifies one cell of the job-queue breakdown: a job type paired
// with a lifecycle state.
type QueueCell struct {
	// Type is the job type ("image_embed", "thumbnail", ...).
	Type string
	// State is the lifecycle state ("queued", "running", ...).
	State string
}

// QueueDepthFunc returns the current job count per (type, state) pair. The serve
// command adapts jobs.Store.CountsByTypeState to this signature.
type QueueDepthFunc func(ctx context.Context) (map[QueueCell]int, error)

// RegisterJobQueue installs a collector exporting job queue depth by state, by
// type, and by both at once, all folded from the single breakdown fn returns —
// one query per scrape rather than one per dimension. A nil fn is a no-op.
func (r *Registry) RegisterJobQueue(fn QueueDepthFunc) {
	if fn == nil {
		return
	}
	r.reg.MustRegister(&queueDepthCollector{depth: fn})
}

// queueDepthCollector exports job queue depth as gauges pulled at scrape time.
type queueDepthCollector struct {
	depth QueueDepthFunc
}

// stateDesc, typeDesc and cellDesc describe the three queue-depth gauge
// families. The two one-dimensional families are sums over the third; they are
// kept because they are what an alert on "the queue is backing up" or "a type is
// stuck" reads, without a by() aggregation.
var (
	stateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "jobs", "queue_depth"),
		"Number of jobs in the queue, partitioned by state.", []string{"state"}, nil)
	typeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "jobs", "queue_depth_by_type"),
		"Number of jobs in the queue, partitioned by type.", []string{"type"}, nil)
	cellDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "jobs", "queue_depth_by_type_state"),
		"Number of jobs in the queue, partitioned by type and state.", []string{"type", "state"}, nil)
)

// Describe implements prometheus.Collector.
func (c *queueDepthCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- stateDesc
	ch <- typeDesc
	ch <- cellDesc
}

// Collect implements prometheus.Collector, sampling the queue breakdown within
// collectTimeout. A query error drops the queue gauges for the scrape rather
// than failing the whole /metrics response. depth is never nil: RegisterJobQueue
// is the only constructor and it refuses one.
func (c *queueDepthCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()
	cells, err := c.depth(ctx)
	if err != nil {
		return
	}
	byState := make(map[string]int, len(cells))
	byType := make(map[string]int, len(cells))
	for cell, n := range cells {
		byState[cell.State] += n
		byType[cell.Type] += n
		ch <- prometheus.MustNewConstMetric(cellDesc, prometheus.GaugeValue, float64(n), cell.Type, cell.State)
	}
	emitLabelled(ch, stateDesc, byState)
	emitLabelled(ch, typeDesc, byType)
}

// GeocodeBudgetFunc reports the reverse-geocode credit budget's current window:
// how many credits remain in it and how many one window allows. The serve
// command adapts placesjob's budget snapshot to this signature.
type GeocodeBudgetFunc func() (remaining, limit int)

// RegisterGeocodeBudget installs a collector exporting how much of the mapy.com
// geocode credit budget is left, sampled on every scrape so the gauge follows
// the window rolling over even while no job runs. A nil fn is a no-op (no budget
// is enforced, or no mapy.com key is configured).
func (r *Registry) RegisterGeocodeBudget(fn GeocodeBudgetFunc) {
	if fn == nil {
		return
	}
	r.reg.MustRegister(&geocodeBudgetCollector{fn: fn})
}

// geocodeBudgetCollector exports the geocode credit budget as gauges pulled at
// scrape time.
type geocodeBudgetCollector struct {
	fn GeocodeBudgetFunc
}

// budgetRemainingDesc and budgetLimitDesc describe the credit-budget gauges.
var (
	budgetRemainingDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "geocode", "credits_remaining"),
		"Reverse-geocode credits left in the current budget window.", nil, nil)
	budgetLimitDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "geocode", "credits_limit"),
		"Reverse-geocode credits one budget window allows.", nil, nil)
)

// Describe implements prometheus.Collector.
func (c *geocodeBudgetCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- budgetRemainingDesc
	ch <- budgetLimitDesc
}

// Collect implements prometheus.Collector, sampling the budget at scrape time.
func (c *geocodeBudgetCollector) Collect(ch chan<- prometheus.Metric) {
	remaining, limit := c.fn()
	ch <- prometheus.MustNewConstMetric(budgetRemainingDesc, prometheus.GaugeValue, float64(remaining))
	ch <- prometheus.MustNewConstMetric(budgetLimitDesc, prometheus.GaugeValue, float64(limit))
}
