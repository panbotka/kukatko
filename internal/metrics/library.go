package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// defaultLibraryTTL is how long a library snapshot is memoised when the caller
// asks for no explicit TTL. Prometheus scrapes on a fixed interval forever, so
// unlike a dashboard the cost of these aggregates is paid continuously; a minute
// of staleness on numbers that move in the hundreds per import is a trade worth
// making.
const defaultLibraryTTL = time.Minute

// Stage label values of the photos_processed / photos_pending gauges. Each names
// an asynchronous enrichment a photo passes through after ingest.
const (
	// StageEmbedding is the image-embedding stage (the `image_embed` job).
	StageEmbedding = "embedding"
	// StageFaces is the face-detection stage (the `face_detect` job).
	StageFaces = "faces"
	// StagePlaces is the reverse-geocode stage (the `places` job).
	StagePlaces = "places"
)

// Marker state label values of the markers gauge.
const (
	// MarkerAssigned marks a marker that names a subject.
	MarkerAssigned = "assigned"
	// MarkerUnassigned marks a marker that is still nameless.
	MarkerUnassigned = "unassigned"
)

// ImportRun is the last recorded run of one import source, reduced to what the
// gauges need: which source, what state it ended in, and when it ran.
type ImportRun struct {
	// Source is the import source ("photoprism", "folder", ...).
	Source string
	// Status is the run's recorded status ("running", "done", ...).
	Status string
	// Statuses lists every status the source's runs can be in, so the collector
	// can publish a zero for the ones this run is not in and make a transition
	// visible instead of leaving a stale series behind.
	Statuses []string
	// StartedAt is when the run started.
	StartedAt time.Time
	// FinishedAt is when it finished; the zero time while it is still running.
	FinishedAt time.Time
}

// LibrarySnapshot is the library-content aggregation the collector turns into
// gauges. It is shaped for the metric families rather than for a domain reader:
// every dimension that becomes a label is a map keyed by that label's value, so
// the collector never has to know the label values in advance and a new album
// type or media type shows up without touching this package.
//
// The counts are instance-wide and unpartitioned by user — /metrics is
// unauthenticated (see server.WithMetricsHandler), so nothing per-user, and
// nothing that names a photo, album, label or person, may be exported here.
type LibrarySnapshot struct {
	// PhotosByMediaType counts catalogue rows per media_type, archived included.
	PhotosByMediaType map[string]int
	// PhotosArchived is how many photos are soft-deleted, i.e. in the trash.
	PhotosArchived int
	// PhotosProcessed counts photos that have completed each enrichment stage.
	PhotosProcessed map[string]int
	// PhotosPending counts the remaining backlog of each enrichment stage.
	PhotosPending map[string]int
	// Embeddings is the total number of image-embedding rows.
	Embeddings int
	// Faces is the total number of detected-face rows.
	Faces int
	// MarkersByState counts markers that do and do not name a subject.
	MarkersByState map[string]int
	// SubjectsByType counts subjects per type (person/pet/other).
	SubjectsByType map[string]int
	// AlbumsByType counts albums per type (album/folder/moment/state/month).
	AlbumsByType map[string]int
	// Labels is the total number of labels.
	Labels int
	// Imports is the last recorded run of each import source; sources that have
	// never run are simply absent.
	Imports []ImportRun
}

// LibraryStatsFunc returns the current library snapshot. It is the seam between
// this package and the catalogue: the serve command adapts internal/system's
// aggregation (which the admin dashboard already uses) to this signature, so the
// counts have exactly one implementation and one SQL statement behind them.
type LibraryStatsFunc func(ctx context.Context) (LibrarySnapshot, error)

// RegisterLibrary installs the library-content collector, which exports the
// catalogue's size and processing coverage plus the last import run per source.
//
// The snapshot is memoised for ttl (non-positive uses defaultLibraryTTL) because
// these are aggregates over the largest tables in the database and a scrape
// arrives every few seconds forever; without the cache /metrics would itself
// become the load it is there to report on. A nil fn is a no-op, so an instance
// wired without a catalogue simply exports no library series.
func (r *Registry) RegisterLibrary(fn LibraryStatsFunc, ttl time.Duration) {
	if fn == nil {
		return
	}
	r.reg.MustRegister(newLibraryCollector(fn, ttl, nil))
}

// libraryDescs holds the descriptors of every library and import series. They
// are built once per collector rather than as package-level vars so two
// registries in the same process (as tests build) do not share state.
type libraryDescs struct {
	photos          *prometheus.Desc
	photosArchived  *prometheus.Desc
	photosProcessed *prometheus.Desc
	photosPending   *prometheus.Desc
	embeddings      *prometheus.Desc
	faces           *prometheus.Desc
	markers         *prometheus.Desc
	subjects        *prometheus.Desc
	albums          *prometheus.Desc
	labels          *prometheus.Desc
	collectErrors   *prometheus.Desc
	importStatus    *prometheus.Desc
	importStarted   *prometheus.Desc
	importFinished  *prometheus.Desc
}

// newLibraryDescs wires the descriptors for the library-content gauges. The
// names carry no _total suffix: these are gauges of a current size, and _total is
// reserved for counters so rate() over them stays meaningful.
func newLibraryDescs() libraryDescs {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "library", name), help, labels, nil)
	}
	return libraryDescs{
		photos: desc("photos", "Photos in the catalogue, archived included, partitioned by media type.",
			"media_type"),
		photosArchived: desc("photos_archived", "Photos that are soft-deleted, i.e. sitting in the trash."),
		photosProcessed: desc("photos_processed",
			"Photos that have completed an asynchronous enrichment stage.", "stage"),
		photosPending: desc("photos_pending",
			"Photos still waiting for an asynchronous enrichment stage. For the faces stage this "+
				"also counts photos that genuinely contain no face, which the counts cannot tell apart.",
			"stage"),
		embeddings:    desc("embeddings", "Image-embedding rows stored in the database."),
		faces:         desc("faces", "Detected-face rows stored in the database."),
		markers:       desc("markers", "Markers, partitioned by whether they name a subject.", "state"),
		subjects:      desc("subjects", "Named subjects, partitioned by type.", "type"),
		albums:        desc("albums", "Albums, partitioned by type.", "type"),
		labels:        desc("labels", "Labels defined in the catalogue."),
		collectErrors: desc("collect_errors_total", "Scrapes whose library aggregation failed."),
		importStatus: prometheus.NewDesc(prometheus.BuildFQName(namespace, "import", "last_run_status"),
			"Status of the most recent import run per source (1 for the status it is in, 0 for the rest).",
			[]string{"source", "status"}, nil),
		importStarted: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "import", "last_run_start_timestamp_seconds"),
			"Unix start time of the most recent import run per source.", []string{"source"}, nil),
		importFinished: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "import", "last_run_finish_timestamp_seconds"),
			"Unix finish time of the most recent import run per source; absent while it is still running.",
			[]string{"source"}, nil),
	}
}

// libraryCollector exports the library snapshot as gauges, recomputing it at
// most once per TTL. It implements prometheus.Collector and is safe for
// concurrent use.
type libraryCollector struct {
	descs libraryDescs

	fn  LibraryStatsFunc
	ttl time.Duration
	now func() time.Time

	mu         sync.Mutex
	cached     LibrarySnapshot
	computedAt time.Time
	valid      bool
	errors     float64
}

// newLibraryCollector returns a collector over fn. A non-positive ttl defaults to
// defaultLibraryTTL and a nil now defaults to time.Now, so production callers may
// leave both unset while tests drive a fake clock.
func newLibraryCollector(fn LibraryStatsFunc, ttl time.Duration, now func() time.Time) *libraryCollector {
	if ttl <= 0 {
		ttl = defaultLibraryTTL
	}
	if now == nil {
		now = time.Now
	}
	return &libraryCollector{descs: newLibraryDescs(), fn: fn, ttl: ttl, now: now}
}

// Describe implements prometheus.Collector.
func (c *libraryCollector) Describe(ch chan<- *prometheus.Desc) {
	d := c.descs
	for _, desc := range []*prometheus.Desc{
		d.photos, d.photosArchived, d.photosProcessed, d.photosPending, d.embeddings, d.faces,
		d.markers, d.subjects, d.albums, d.labels, d.collectErrors,
		d.importStatus, d.importStarted, d.importFinished,
	} {
		ch <- desc
	}
}

// Collect implements prometheus.Collector. A failed aggregation exports no
// library gauges for that scrape — a gap Prometheus renders as missing data,
// which is honest — but always bumps collect_errors_total, so the failure itself
// is visible and alertable rather than silent.
func (c *libraryCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	snapshot, errs, ok := c.snapshot(ctx)
	ch <- prometheus.MustNewConstMetric(c.descs.collectErrors, prometheus.CounterValue, errs)
	if !ok {
		return
	}
	c.emitLibrary(ch, snapshot)
	c.emitImports(ch, snapshot.Imports)
}

// snapshot returns the memoised snapshot, recomputing it when the cached value is
// older than the TTL. It reports ok=false when nothing valid is available, and
// returns the running failure count alongside so Collect can export it whatever
// the outcome. A stale value is never served: a fresh failure hides the numbers
// rather than pinning them at their last-known size, which would read as a
// library that stopped growing.
func (c *libraryCollector) snapshot(ctx context.Context) (LibrarySnapshot, float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.valid && c.now().Sub(c.computedAt) < c.ttl {
		return c.cached, c.errors, true
	}
	fresh, err := c.fn(ctx)
	if err != nil {
		c.errors++
		c.valid = false
		return LibrarySnapshot{}, c.errors, false
	}
	c.cached = fresh
	c.computedAt = c.now()
	c.valid = true
	return c.cached, c.errors, true
}

// emitLibrary writes the catalogue-content gauges of one snapshot.
func (c *libraryCollector) emitLibrary(ch chan<- prometheus.Metric, s LibrarySnapshot) {
	d := c.descs
	emitLabelled(ch, d.photos, s.PhotosByMediaType)
	emitLabelled(ch, d.photosProcessed, s.PhotosProcessed)
	emitLabelled(ch, d.photosPending, s.PhotosPending)
	emitLabelled(ch, d.markers, s.MarkersByState)
	emitLabelled(ch, d.subjects, s.SubjectsByType)
	emitLabelled(ch, d.albums, s.AlbumsByType)
	emitScalar(ch, d.photosArchived, s.PhotosArchived)
	emitScalar(ch, d.embeddings, s.Embeddings)
	emitScalar(ch, d.faces, s.Faces)
	emitScalar(ch, d.labels, s.Labels)
}

// emitImports writes the per-source import-run gauges. The status is published as
// one series per known status so a run moving from running to failed shows the
// old series drop to 0 instead of silently disappearing; the timestamps are
// exported as Unix seconds, which is how Prometheus expresses an age (time() minus
// the gauge) without the exporter having to compute one.
func (c *libraryCollector) emitImports(ch chan<- prometheus.Metric, runs []ImportRun) {
	for _, run := range runs {
		for _, status := range run.Statuses {
			value := 0.0
			if status == run.Status {
				value = 1
			}
			ch <- prometheus.MustNewConstMetric(c.descs.importStatus, prometheus.GaugeValue, value,
				run.Source, status)
		}
		if !run.StartedAt.IsZero() {
			ch <- prometheus.MustNewConstMetric(c.descs.importStarted, prometheus.GaugeValue,
				float64(run.StartedAt.Unix()), run.Source)
		}
		if !run.FinishedAt.IsZero() {
			ch <- prometheus.MustNewConstMetric(c.descs.importFinished, prometheus.GaugeValue,
				float64(run.FinishedAt.Unix()), run.Source)
		}
	}
}

// emitLabelled writes one gauge per entry of counts, using the map key as the
// series' single label value.
func emitLabelled(ch chan<- prometheus.Metric, desc *prometheus.Desc, counts map[string]int) {
	for label, n := range counts {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(n), label)
	}
}

// emitScalar writes one unlabelled gauge.
func emitScalar(ch chan<- prometheus.Metric, desc *prometheus.Desc, value int) {
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(value))
}
