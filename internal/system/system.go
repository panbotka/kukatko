// Package system aggregates the operational health of the running kukatko
// instance into a single snapshot for the admin status dashboard: embeddings
// sidecar reachability, job-queue depth and dead-letter backlog, the backup
// subsystem state, the last import run per source, on-disk storage usage,
// database reachability, the map provider's last observed state (so a mapy.com
// key that is being rejected is visible without opening the map) and the
// reverse-geocode credit budget (so a running import's credit spend is visible
// while it happens), plus the build version. It depends on small interfaces so
// the aggregation is unit-testable with fakes, and the HTTP layer lives in
// internal/systemapi.
//
// Alongside that maintainer view it aggregates the library statistics — the
// instance-wide photo/embedding/face/people counts every signed-in user may see
// (see Library and Service.LibraryStats). Both aggregations memoise their
// expensive part for a short TTL so a polled page cannot turn into a query
// storm.
package system

import (
	"context"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/version"
)

// defaultStorageTTL is how long a storage-usage measurement is memoised before
// the next status request recomputes it, so polling does not re-walk a large
// originals tree every few seconds.
const defaultStorageTTL = 30 * time.Second

// DBPinger reports database reachability. It is satisfied by *database.DB.
type DBPinger interface {
	// Ping checks that the database accepts a round-trip, returning an error
	// when it is unreachable.
	Ping(ctx context.Context) error
}

// EmbeddingHealth probes the embeddings sidecar. It is satisfied by
// embedding.Client; any HTTP response counts as online, only a transport
// failure as offline.
type EmbeddingHealth interface {
	// Healthy reports whether the embeddings sidecar is currently reachable.
	Healthy(ctx context.Context) bool
}

// JobCounter exposes the queue aggregates the dashboard needs. It is satisfied
// by *jobs.Store.
type JobCounter interface {
	// CountsByState returns the number of jobs in each lifecycle state.
	CountsByState(ctx context.Context) (map[jobs.State]int, error)
	// CountsByType returns the number of jobs of each type.
	CountsByType(ctx context.Context) (map[string]int, error)
	// CountPending returns how many jobs of the given types are queued or
	// running.
	CountPending(ctx context.Context, types ...string) (int, error)
}

// ImportLister exposes the most recent run per source. It is satisfied by
// *importer.Store.
type ImportLister interface {
	// LatestRun returns the most recently started run for source, whatever its
	// status, with ok=false when the source has never run.
	LatestRun(ctx context.Context, source importer.Source) (importer.Run, bool, error)
}

// BackupReporter reports the backup subsystem state. It is satisfied by
// *backup.Service; a nil BackupReporter means no backup destination is wired.
type BackupReporter interface {
	// Status returns the current backup subsystem state.
	Status() backup.Status
}

// MapsReporter reports the last observed outcome of a mapy.com call. It is
// satisfied by *mapy.Health; a nil MapsReporter means no mapy.com key is
// configured, so the map backend is reported as not configured.
type MapsReporter interface {
	// Snapshot returns the map provider's last observed health.
	Snapshot() mapy.HealthStatus
}

// GeocodeReporter reports the reverse-geocode credit budget. It is satisfied by
// *placesjob.WindowBudget; a nil GeocodeReporter means no mapy.com key is
// configured, so no geocoding happens at all.
type GeocodeReporter interface {
	// Snapshot returns the budget's current window: limit, spend and refill.
	Snapshot() placesjob.BudgetSnapshot
}

// Database is the database-reachability section of the status snapshot.
type Database struct {
	// Reachable is true when the database answered a ping.
	Reachable bool `json:"reachable"`
	// Error is a short, sanitised message when the database is unreachable.
	Error string `json:"error,omitempty"`
}

// Embeddings is the sidecar-reachability section of the status snapshot.
type Embeddings struct {
	// Online is true when the embeddings sidecar answered a health probe. When
	// false, image_embed and face_detect jobs queue and resume once it returns.
	Online bool `json:"online"`
	// URL is the configured sidecar base URL, for display.
	URL string `json:"url"`
}

// Jobs is the queue-depth section of the status snapshot.
type Jobs struct {
	// ByState is the number of jobs per lifecycle state (queued/running/...).
	ByState map[string]int `json:"by_state"`
	// ByType is the number of jobs per type (image_embed/face_detect/...).
	ByType map[string]int `json:"by_type"`
	// Total is the grand total across all states.
	Total int `json:"total"`
	// DeadLetter is the number of jobs that exhausted their retries.
	DeadLetter int `json:"dead_letter"`
	// PendingEmbeddings is the number of queued or running embedding/face jobs,
	// i.e. work waiting on the sidecar.
	PendingEmbeddings int `json:"pending_embeddings"`
}

// Imports is the last-import section of the status snapshot. A nil run means
// nothing has been imported that way yet.
type Imports struct {
	// Folder is the most recent `kukatko import dir` run, or nil. It is the only
	// import that can still happen: the PhotoPrism/photo-sorter migration finished
	// in August 2026 and its importers are gone. Its runs are still in the history
	// (GET /import/runs); they are simply not live state a dashboard should watch.
	Folder *importer.Run `json:"folder"`
}

// Maps is the map-provider (mapy.com) section of the status snapshot. It reports
// what the proxy last saw upstream, so a rejected API key — which otherwise shows
// up only as a grey map — is visible from the dashboard.
type Maps struct {
	// Configured is true when a mapy.com API key is set. When false, the map view
	// has no tiles at all and the rest of this section is meaningless.
	Configured bool `json:"configured"`
	// State is the last observed upstream outcome (ok, key_rejected, ...).
	State string `json:"state"`
	// Degraded is true when the last outcome means map data is currently broken,
	// most notably when mapy.com is rejecting the API key.
	Degraded bool `json:"degraded"`
	// Detail is a short, sanitised description of the last failure (never carries
	// the API key); empty while healthy.
	Detail string `json:"detail,omitempty"`
	// CheckedAt is when the last outcome was observed; nil when none has been.
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

// Geocode is the reverse-geocode credit section of the status snapshot. Every
// geocode the `places` job performs costs a metered mapy.com credit, so this
// reports what the current budget window has spent and what is left of it —
// visible while an import runs, not reconstructed from the bill afterwards.
type Geocode struct {
	// Configured is true when a mapy.com API key is set, i.e. when the `places`
	// job runs at all. When false the rest of this section is meaningless.
	Configured bool `json:"configured"`
	// BudgetEnabled is true when a credit budget caps the spend. When false the
	// only bound is the per-second rate limiter.
	BudgetEnabled bool `json:"budget_enabled"`
	// Limit is how many geocodes one budget window allows.
	Limit int `json:"limit"`
	// Spent is how many have been spent in the current window.
	Spent int `json:"spent"`
	// Remaining is how many the current window still allows.
	Remaining int `json:"remaining"`
	// WindowSeconds is the length of one budget window in seconds.
	WindowSeconds float64 `json:"window_seconds"`
	// ResetsAt is when the current window ends and the budget refills; nil while
	// no budget is enforced or nothing has been spent yet.
	ResetsAt *time.Time `json:"resets_at,omitempty"`
}

// Status is the full system-status snapshot returned by GET /system/status.
type Status struct {
	Version    version.Info  `json:"version"`
	Database   Database      `json:"database"`
	Embeddings Embeddings    `json:"embeddings"`
	Jobs       Jobs          `json:"jobs"`
	Backup     backup.Status `json:"backup"`
	Imports    Imports       `json:"imports"`
	Storage    StorageUsage  `json:"storage"`
	Maps       Maps          `json:"maps"`
	Geocode    Geocode       `json:"geocode"`
}

// Config bundles the dependencies of New. Backup may be nil (no destination
// configured); every other field is required.
type Config struct {
	// DB pings the database for the reachability readout.
	DB DBPinger
	// Embeddings probes the sidecar.
	Embeddings EmbeddingHealth
	// EmbeddingURL is the configured sidecar URL, surfaced for display.
	EmbeddingURL string
	// Jobs supplies the queue aggregates.
	Jobs JobCounter
	// Backup reports the backup subsystem state; nil when not configured.
	Backup BackupReporter
	// Maps reports the map provider's last observed health; nil when no mapy.com
	// key is configured.
	Maps MapsReporter
	// Geocode reports the reverse-geocode credit budget; nil when no mapy.com key
	// is configured.
	Geocode GeocodeReporter
	// Imports supplies the latest run per source.
	Imports ImportLister
	// Library supplies the library-wide counts behind LibraryStats. A nil Library
	// makes LibraryStats fail rather than panic, so a caller that only needs
	// Collect may leave it unset.
	Library LibraryCounter
	// OriginalsPath is the on-disk root of the stored originals.
	OriginalsPath string
	// CachePath is the on-disk root of the derived cache (thumbnails).
	CachePath string
	// StorageTTL memoises the storage measurement; non-positive uses the default.
	StorageTTL time.Duration
	// LibraryTTL memoises the library counts; non-positive uses the default.
	LibraryTTL time.Duration
	// Clock supplies the current time for both caches; nil uses time.Now.
	Clock func() time.Time
}

// Service aggregates the operational status of the running instance. It holds no
// mutable state beyond the storage-usage and library-counts caches and is safe
// for concurrent use.
type Service struct {
	db           DBPinger
	embeddings   EmbeddingHealth
	embeddingURL string
	jobs         JobCounter
	backup       BackupReporter
	maps         MapsReporter
	geocode      GeocodeReporter
	imports      ImportLister
	storage      *storageCache
	library      *libraryCache
}

// New constructs a Service from cfg.
func New(cfg Config) *Service {
	return &Service{
		db:           cfg.DB,
		embeddings:   cfg.Embeddings,
		embeddingURL: cfg.EmbeddingURL,
		jobs:         cfg.Jobs,
		backup:       cfg.Backup,
		maps:         cfg.Maps,
		geocode:      cfg.Geocode,
		imports:      cfg.Imports,
		storage:      newStorageCache(cfg.OriginalsPath, cfg.CachePath, cfg.StorageTTL, cfg.Clock),
		library:      newLibraryCache(cfg.Library, cfg.LibraryTTL, cfg.Clock),
	}
}

// LibraryStats returns the instance-wide library counts with their derived
// coverage gaps, memoised for a short TTL. Unlike Collect it reports a failure
// as an error rather than inline: a caller must show the reader that the numbers
// are unavailable instead of rendering zeroes as if they were real counts.
func (s *Service) LibraryStats(ctx context.Context) (Library, error) {
	return s.library.counts(ctx)
}

// Collect gathers the full status snapshot. Database reachability and storage
// usage are best-effort (a down database or an unreadable directory is reported
// inline, not as an error); only a failure to read the queue aggregates or the
// import history — which require a working database — is returned as an error.
func (s *Service) Collect(ctx context.Context) (Status, error) {
	jobsStatus, err := s.collectJobs(ctx)
	if err != nil {
		return Status{}, err
	}
	imports, err := s.collectImports(ctx)
	if err != nil {
		return Status{}, err
	}
	// Storage usage is best-effort: a missing or unreadable directory must not
	// fail the whole readout, so the measurement error is intentionally dropped.
	storageUsage, _ := s.storage.usage(ctx)
	return Status{
		Version:    version.Get(),
		Database:   s.collectDatabase(ctx),
		Embeddings: Embeddings{Online: s.embeddings.Healthy(ctx), URL: s.embeddingURL},
		Jobs:       jobsStatus,
		Backup:     s.collectBackup(),
		Imports:    imports,
		Storage:    storageUsage,
		Maps:       s.collectMaps(),
		Geocode:    s.collectGeocode(),
	}, nil
}

// collectJobs reads the queue aggregates and folds them into the Jobs section,
// deriving the grand total, the dead-letter count and the embedding backlog.
func (s *Service) collectJobs(ctx context.Context) (Jobs, error) {
	byState, err := s.jobs.CountsByState(ctx)
	if err != nil {
		return Jobs{}, fmt.Errorf("counting jobs by state: %w", err)
	}
	byType, err := s.jobs.CountsByType(ctx)
	if err != nil {
		return Jobs{}, fmt.Errorf("counting jobs by type: %w", err)
	}
	pending, err := s.jobs.CountPending(ctx, jobs.TypeImageEmbed, jobs.TypeFaceDetect)
	if err != nil {
		return Jobs{}, fmt.Errorf("counting pending embedding jobs: %w", err)
	}
	state := make(map[string]int, len(byState))
	total := 0
	for key, count := range byState {
		state[string(key)] = count
		total += count
	}
	return Jobs{
		ByState:           state,
		ByType:            byType,
		Total:             total,
		DeadLetter:        state[string(jobs.StateDead)],
		PendingEmbeddings: pending,
	}, nil
}

// LatestRuns returns the most recent run of every recognised import source,
// keyed by source. A source that has never run is absent from the map rather
// than present with a zero run, so a caller can tell "never imported" from
// "imported and the tallies happen to be zero".
//
// It exists alongside Collect because /metrics wants every source, including the
// retired migration ones whose finished runs stay in the table, while the
// dashboard's Imports section reports only the folder import it renders.
func (s *Service) LatestRuns(ctx context.Context) (map[importer.Source]importer.Run, error) {
	sources := importer.AllSources()
	runs := make(map[importer.Source]importer.Run, len(sources))
	for _, source := range sources {
		run, err := s.latestRun(ctx, source)
		if err != nil {
			return nil, err
		}
		if run != nil {
			runs[source] = *run
		}
	}
	return runs, nil
}

// collectImports reads the latest folder-import run for the import section;
// LatestRuns covers every source, including the retired migration ones.
func (s *Service) collectImports(ctx context.Context) (Imports, error) {
	run, err := s.latestRun(ctx, importer.SourceFolder)
	if err != nil {
		return Imports{}, err
	}
	return Imports{Folder: run}, nil
}

// latestRun returns the most recent run for source, or nil when none exists.
func (s *Service) latestRun(ctx context.Context, source importer.Source) (*importer.Run, error) {
	run, ok, err := s.imports.LatestRun(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("latest %s run: %w", source, err)
	}
	if !ok {
		return nil, nil //nolint:nilnil // (nil, nil) means the source has never run.
	}
	return &run, nil
}

// collectDatabase pings the database and reports reachability without leaking
// connection details into the error message.
func (s *Service) collectDatabase(ctx context.Context) Database {
	if err := s.db.Ping(ctx); err != nil {
		return Database{Reachable: false, Error: "database is unreachable"}
	}
	return Database{Reachable: true}
}

// collectBackup returns the backup subsystem status, synthesising a
// not-configured status when no backup destination is wired.
func (s *Service) collectBackup() backup.Status {
	if s.backup == nil {
		return backup.Status{Configured: false}
	}
	return s.backup.Status()
}

// collectMaps returns the map-provider status, reporting not-configured when no
// mapy.com key is wired. The detail comes from the mapy client's sentinel errors,
// which never carry the API key, so it is safe to surface to an admin.
func (s *Service) collectMaps() Maps {
	if s.maps == nil {
		return Maps{Configured: false, State: string(mapy.HealthUnknown)}
	}
	snapshot := s.maps.Snapshot()
	status := Maps{
		Configured: true,
		State:      string(snapshot.State),
		Degraded:   snapshot.State.Degraded(),
		Detail:     snapshot.Detail,
	}
	if !snapshot.CheckedAt.IsZero() {
		checkedAt := snapshot.CheckedAt
		status.CheckedAt = &checkedAt
	}
	return status
}

// collectGeocode returns the reverse-geocode credit budget, reporting
// not-configured when no mapy.com key is wired (nothing geocodes, so nothing is
// spent).
func (s *Service) collectGeocode() Geocode {
	if s.geocode == nil {
		return Geocode{}
	}
	snapshot := s.geocode.Snapshot()
	status := Geocode{
		Configured:    true,
		BudgetEnabled: snapshot.Enabled,
		Limit:         snapshot.Limit,
		Spent:         snapshot.Spent,
		Remaining:     snapshot.Remaining,
		WindowSeconds: snapshot.Window.Seconds(),
	}
	if !snapshot.ResetsAt.IsZero() {
		resetsAt := snapshot.ResetsAt
		status.ResetsAt = &resetsAt
	}
	return status
}
