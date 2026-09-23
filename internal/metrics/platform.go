package metrics

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Encode-state label values of the library_videos_by_encode_state gauge. They
// are disjoint and add up to every browsable video: a video either has a
// streaming rendition or is in exactly one of the four states of not having one.
const (
	// EncodeStreamable is a video with at least one encoded rendition.
	EncodeStreamable = "streamable"
	// EncodeQueued is a video whose encode waits in the queue.
	EncodeQueued = "queued"
	// EncodeRunning is a video being encoded right now.
	EncodeRunning = "running"
	// EncodeFailed is a video whose encode failed or was dead-lettered.
	EncodeFailed = "failed"
	// EncodeNotScheduled is a video with no rendition and no encode outstanding —
	// the state that stalls silently, because no queue row records it.
	EncodeNotScheduled = "not_scheduled"
)

// Backlog kind label values of the library_backlog gauge. Each is a pile of
// curation work where zero is the good value.
const (
	// BacklogFacesUnassigned is detected faces that name nobody.
	BacklogFacesUnassigned = "faces_unassigned"
	// BacklogClusters is auto-clusters waiting for a name.
	BacklogClusters = "clusters"
	// BacklogPhotosWithoutTakenAt is browsable photos with no capture time.
	BacklogPhotosWithoutTakenAt = "photos_without_taken_at"
	// BacklogPhotosWithoutGPS is browsable photos with no coordinates.
	BacklogPhotosWithoutGPS = "photos_without_gps"
	// BacklogPhotosWithoutOCR is browsable stills never read by text recognition.
	BacklogPhotosWithoutOCR = "photos_without_ocr"
	// BacklogDuplicateMarkers is (photo, person) pairs marked more than once.
	BacklogDuplicateMarkers = "duplicate_markers"
)

// Byte-set label values of the library_bytes gauge.
const (
	// BytesLive is what the browsable library's originals weigh.
	BytesLive = "live"
	// BytesTrash is what the trash's originals weigh, i.e. what a purge reclaims.
	BytesTrash = "trash"
	// BytesDerived is what the derived media weigh in the local cache.
	BytesDerived = "derived"
)

// Source label values of platform_collect_errors_total: the two sections of the
// platform snapshot that are computed (and can therefore fail) rather than read
// from memory.
const (
	// SourceCatalogue is the memoised dashboard aggregation over the catalogue.
	SourceCatalogue = "catalogue"
	// SourceDisk is the memoised on-disk storage measurement.
	SourceDisk = "disk"
)

// BackupState is the backup subsystem's state as the gauges need it. Every
// timestamp is the zero time until the event has happened in this process: the
// state is held in memory and starts empty on every restart.
type BackupState struct {
	// Running is true while a run is in progress.
	Running bool
	// LastFinishedAt is when the most recent run finished, successful or not.
	LastFinishedAt time.Time
	// LastSucceededAt is when the most recent successful run finished.
	LastSucceededAt time.Time
	// LastSucceeded is whether the most recent finished run succeeded;
	// meaningful only when LastFinishedAt is set.
	LastSucceeded bool
	// OriginalsUploaded and OriginalsSkipped are the most recent finished run's
	// originals sync tally: copied this time versus already in the destination.
	OriginalsUploaded int
	OriginalsSkipped  int
}

// DiskUsage is the on-disk measurement: the two trees Kukátko writes and the
// filesystem the originals live on.
type DiskUsage struct {
	// OriginalsBytes is what the files under the originals root weigh. On an
	// instance whose originals live in an object store it is (close to) zero.
	OriginalsBytes int64
	// CacheBytes is what the files under the derived-media cache weigh.
	CacheBytes int64
	// FreeBytes is the space an unprivileged process may still use on the
	// filesystem holding the originals root.
	FreeBytes int64
	// TotalBytes is that filesystem's capacity.
	TotalBytes int64
}

// MapsState is the map provider's last observed health.
type MapsState struct {
	// Up is true when the last observed mapy.com call left map data working.
	Up bool
	// CheckedAt is when that call happened; the zero time while none has been
	// observed, in which case Up says nothing and is not exported.
	CheckedAt time.Time
}

// CatalogueState is the dashboard aggregation's share of the platform snapshot:
// the streaming-encode states, the curation backlog and what the library weighs.
// As with LibrarySnapshot every label dimension is a map keyed by label value.
type CatalogueState struct {
	// StreamingEnabled is whether this instance encodes streaming renditions at
	// all; with it off every video is not_scheduled by design.
	StreamingEnabled bool
	// VideosByEncodeState counts browsable videos per Encode* state.
	VideosByEncodeState map[string]int
	// OldestQueuedEncode is when the longest-waiting queued encode was enqueued;
	// the zero time when nothing is queued.
	OldestQueuedEncode time.Time
	// Backlog counts each Backlog* pile.
	Backlog map[string]int
	// Bytes is what each Bytes* set weighs.
	Bytes map[string]int64
}

// PlatformSnapshot is everything the platform collector exports, read in one
// call. A nil Backup or Maps means the subsystem is not configured; a nil Disk or
// Catalogue means its source failed for this scrape, which drops exactly that
// section's families and bumps platform_collect_errors_total{source}.
type PlatformSnapshot struct {
	// Backup is nil when no backup destination is configured.
	Backup *BackupState
	// Maps is nil when no mapy.com key is configured.
	Maps *MapsState
	// Disk is nil when the storage measurement failed.
	Disk *DiskUsage
	// Catalogue is nil when the dashboard aggregation failed.
	Catalogue *CatalogueState
}

// PlatformFunc returns the current platform snapshot. The serve command adapts
// internal/system's scrape-safe accessor (system.Service.Platform) to it; that
// accessor serves the dashboard aggregation and the storage measurement out of
// the caches the admin page already uses, so this function must never be backed
// by an unmemoised query.
type PlatformFunc func(ctx context.Context) PlatformSnapshot

// RegisterPlatform installs the collector exporting what the instance runs on:
// the backup outcome, disk usage, the streaming-encode states, the map
// provider's health, the curation backlog and the library's weight. A nil fn is
// a no-op.
//
// The collector keeps no cache of its own: the sources it reads are already
// memoised (the dashboard aggregation and the storage measurement for their own
// TTL, shared with the System page) or plain in-memory state, and a second layer
// would only make the backup and map-provider series lag behind the truth.
func (r *Registry) RegisterPlatform(fn PlatformFunc) {
	if fn == nil {
		return
	}
	r.reg.MustRegister(newPlatformCollector(fn))
}

// platformDescs holds the descriptors of every platform series, built once per
// collector so two registries in one process do not share state.
type platformDescs struct {
	backupConfigured    *prometheus.Desc
	backupRunning       *prometheus.Desc
	backupLastSuccess   *prometheus.Desc
	backupLastFinished  *prometheus.Desc
	backupLastSucceeded *prometheus.Desc
	backupOriginals     *prometheus.Desc

	diskTree       *prometheus.Desc
	diskFree       *prometheus.Desc
	diskTotal      *prometheus.Desc
	mapsConfigured *prometheus.Desc
	mapsUp         *prometheus.Desc
	mapsChecked    *prometheus.Desc

	streamingEnabled *prometheus.Desc
	videosByState    *prometheus.Desc
	oldestQueued     *prometheus.Desc
	backlog          *prometheus.Desc
	bytes            *prometheus.Desc

	collectErrors *prometheus.Desc
}

// newPlatformDescs wires the platform descriptors. Timestamps are exported as
// Unix seconds, from which PromQL derives an age (time() minus the gauge).
func newPlatformDescs() platformDescs {
	desc := func(subsystem, name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, name), help, labels, nil)
	}
	return platformDescs{
		backupConfigured: desc("backup", "configured", "1 when a backup destination is configured, else 0."),
		backupRunning:    desc("backup", "running", "1 while a backup run is in progress, else 0."),
		backupLastSuccess: desc("backup", "last_run_success",
			"1 when the most recent finished backup run succeeded, 0 when it failed; absent until a run "+
				"has finished since the process started."),
		backupLastFinished: desc("backup", "last_run_finish_timestamp_seconds",
			"Unix time the most recent backup run finished, successful or not."),
		backupLastSucceeded: desc("backup", "last_success_timestamp_seconds",
			"Unix time the most recent successful backup run finished."),
		backupOriginals: desc("backup", "last_run_originals",
			"Originals the most recent finished backup run copied (uploaded) or found already "+
				"in the destination (skipped).", "result"),
		diskTree: desc("disk", "tree_bytes",
			"Bytes of the files under a directory tree Kukátko writes.", "tree"),
		diskFree: desc("disk", "filesystem_free_bytes",
			"Bytes still available to Kukátko on the filesystem holding the originals root."),
		diskTotal: desc("disk", "filesystem_size_bytes",
			"Capacity in bytes of the filesystem holding the originals root."),
		mapsConfigured: desc("maps", "configured", "1 when a mapy.com API key is configured, else 0."),
		mapsUp: desc("maps", "up",
			"1 when the last observed mapy.com call left map data working, 0 when it failed "+
				"(key rejected, rate limited, unreachable); absent until a call has been observed."),
		mapsChecked: desc("maps", "last_check_timestamp_seconds",
			"Unix time of the last observed mapy.com call."),
		streamingEnabled: desc("library", "video_streaming_enabled",
			"1 when the instance encodes streaming renditions (video.hls.enabled), else 0."),
		videosByState: desc("library", "videos_by_encode_state",
			"Browsable videos per streaming-encode state; the states are disjoint and sum to every video.",
			"state"),
		oldestQueued: desc("library", "video_encode_oldest_queued_timestamp_seconds",
			"Unix time the longest-waiting queued streaming encode was enqueued; absent when none is queued."),
		backlog: desc("library", "backlog",
			"Curation backlog per kind; zero is the good value.", "kind"),
		bytes: desc("library", "bytes",
			"Bytes of the library per set: live and trash originals by the catalogue's own "+
				"file sizes, derived media as measured in the local cache.", "set"),
		collectErrors: desc("platform", "collect_errors_total",
			"Scrapes whose platform source failed, dropping that source's series.", "source"),
	}
}

// platformCollector exports a PlatformSnapshot read at scrape time. It is safe
// for concurrent use.
type platformCollector struct {
	descs platformDescs
	fn    PlatformFunc

	mu     sync.Mutex
	errors map[string]float64
}

// newPlatformCollector returns a collector over fn with both error counters
// present at zero, so an alert on them has a series to read from the start.
func newPlatformCollector(fn PlatformFunc) *platformCollector {
	return &platformCollector{
		descs:  newPlatformDescs(),
		fn:     fn,
		errors: map[string]float64{SourceCatalogue: 0, SourceDisk: 0},
	}
}

// Describe implements prometheus.Collector.
func (c *platformCollector) Describe(ch chan<- *prometheus.Desc) {
	d := c.descs
	for _, desc := range []*prometheus.Desc{
		d.backupConfigured, d.backupRunning, d.backupLastSuccess, d.backupLastFinished,
		d.backupLastSucceeded, d.backupOriginals, d.diskTree, d.diskFree, d.diskTotal,
		d.mapsConfigured, d.mapsUp, d.mapsChecked, d.streamingEnabled, d.videosByState,
		d.oldestQueued, d.backlog, d.bytes, d.collectErrors,
	} {
		ch <- desc
	}
}

// Collect implements prometheus.Collector. Each section is emitted on its own: a
// failed source drops only its families for this scrape (a gap, not a stale or
// zero number) and bumps its error counter, never the whole response.
func (c *platformCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	snapshot := c.fn(ctx)
	c.emitBackup(ch, snapshot.Backup)
	c.emitMaps(ch, snapshot.Maps)
	if snapshot.Disk != nil {
		c.emitDisk(ch, *snapshot.Disk)
	}
	if snapshot.Catalogue != nil {
		c.emitCatalogue(ch, *snapshot.Catalogue)
	}
	for source, n := range c.countErrors(snapshot) {
		ch <- prometheus.MustNewConstMetric(c.descs.collectErrors, prometheus.CounterValue, n, source)
	}
}

// countErrors bumps the counter of every computed source missing from snapshot
// and returns a copy of the counters to export.
func (c *platformCollector) countErrors(snapshot PlatformSnapshot) map[string]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot.Disk == nil {
		c.errors[SourceDisk]++
	}
	if snapshot.Catalogue == nil {
		c.errors[SourceCatalogue]++
	}
	return maps.Clone(c.errors)
}

// emitBackup writes the backup series. The configured gauge is always present;
// the outcome series appear only once a run has finished in this process,
// because a zero would claim a failure (or an epoch-old success) nobody observed.
func (c *platformCollector) emitBackup(ch chan<- prometheus.Metric, b *BackupState) {
	d := c.descs
	if b == nil {
		ch <- prometheus.MustNewConstMetric(d.backupConfigured, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(d.backupConfigured, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(d.backupRunning, prometheus.GaugeValue, boolValue(b.Running))
	emitTimestamp(ch, d.backupLastSucceeded, b.LastSucceededAt)
	if b.LastFinishedAt.IsZero() {
		return
	}
	ch <- prometheus.MustNewConstMetric(d.backupLastSuccess, prometheus.GaugeValue, boolValue(b.LastSucceeded))
	emitTimestamp(ch, d.backupLastFinished, b.LastFinishedAt)
	ch <- prometheus.MustNewConstMetric(d.backupOriginals, prometheus.GaugeValue,
		float64(b.OriginalsUploaded), "uploaded")
	ch <- prometheus.MustNewConstMetric(d.backupOriginals, prometheus.GaugeValue,
		float64(b.OriginalsSkipped), "skipped")
}

// emitMaps writes the map-provider series. Up is exported only once a call has
// been observed: before that the provider's health is unknown, not down.
func (c *platformCollector) emitMaps(ch chan<- prometheus.Metric, m *MapsState) {
	d := c.descs
	if m == nil {
		ch <- prometheus.MustNewConstMetric(d.mapsConfigured, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(d.mapsConfigured, prometheus.GaugeValue, 1)
	if m.CheckedAt.IsZero() {
		return
	}
	ch <- prometheus.MustNewConstMetric(d.mapsUp, prometheus.GaugeValue, boolValue(m.Up))
	emitTimestamp(ch, d.mapsChecked, m.CheckedAt)
}

// emitDisk writes the storage measurement.
func (c *platformCollector) emitDisk(ch chan<- prometheus.Metric, u DiskUsage) {
	d := c.descs
	ch <- prometheus.MustNewConstMetric(d.diskTree, prometheus.GaugeValue, float64(u.OriginalsBytes), "originals")
	ch <- prometheus.MustNewConstMetric(d.diskTree, prometheus.GaugeValue, float64(u.CacheBytes), "cache")
	ch <- prometheus.MustNewConstMetric(d.diskFree, prometheus.GaugeValue, float64(u.FreeBytes))
	ch <- prometheus.MustNewConstMetric(d.diskTotal, prometheus.GaugeValue, float64(u.TotalBytes))
}

// emitCatalogue writes the series read from the dashboard aggregation.
func (c *platformCollector) emitCatalogue(ch chan<- prometheus.Metric, s CatalogueState) {
	d := c.descs
	ch <- prometheus.MustNewConstMetric(d.streamingEnabled, prometheus.GaugeValue, boolValue(s.StreamingEnabled))
	emitLabelled(ch, d.videosByState, s.VideosByEncodeState)
	emitTimestamp(ch, d.oldestQueued, s.OldestQueuedEncode)
	emitLabelled(ch, d.backlog, s.Backlog)
	for set, n := range s.Bytes {
		ch <- prometheus.MustNewConstMetric(d.bytes, prometheus.GaugeValue, float64(n), set)
	}
}

// emitTimestamp writes at as Unix seconds, or nothing for the zero time.
func emitTimestamp(ch chan<- prometheus.Metric, desc *prometheus.Desc, at time.Time) {
	if at.IsZero() {
		return
	}
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, float64(at.Unix()))
}

// boolValue maps a flag onto a 0/1 gauge value.
func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
