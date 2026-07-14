package system

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
)

// fakeDB is a DBPinger whose Ping returns the configured error.
type fakeDB struct{ err error }

// Ping returns the configured error.
func (f fakeDB) Ping(context.Context) error { return f.err }

// fakeHealth is an EmbeddingHealth returning the configured online flag.
type fakeHealth struct{ online bool }

// Healthy returns the configured online flag.
func (f fakeHealth) Healthy(context.Context) bool { return f.online }

// fakeJobs is a JobCounter backed by static maps and a pending count.
type fakeJobs struct {
	byState map[jobs.State]int
	byType  map[string]int
	pending int
	err     error
}

// CountsByState returns the configured per-state counts or error.
func (f fakeJobs) CountsByState(context.Context) (map[jobs.State]int, error) {
	return f.byState, f.err
}

// CountsByType returns the configured per-type counts or error.
func (f fakeJobs) CountsByType(context.Context) (map[string]int, error) {
	return f.byType, f.err
}

// CountPending returns the configured pending count or error.
func (f fakeJobs) CountPending(context.Context, ...string) (int, error) {
	return f.pending, f.err
}

// fakeImports is an ImportLister returning a fixed run per source.
type fakeImports struct {
	runs map[importer.Source]importer.Run
	err  error
}

// LatestRun returns the configured run for source, ok=false when absent.
func (f fakeImports) LatestRun(_ context.Context, source importer.Source) (importer.Run, bool, error) {
	if f.err != nil {
		return importer.Run{}, false, f.err
	}
	run, ok := f.runs[source]
	return run, ok, nil
}

// fakeBackup is a BackupReporter returning a fixed status.
type fakeBackup struct{ status backup.Status }

// Status returns the configured backup status.
func (f fakeBackup) Status() backup.Status { return f.status }

// healthyMaps returns a maps health tracker that has last seen a successful
// mapy.com call.
func healthyMaps() *mapy.Health {
	health := mapy.NewHealth()
	health.Record(nil)
	return health
}

// rejectedMaps returns a maps health tracker that has last seen mapy.com reject
// the API key (its 403), i.e. the state that leaves the map grey.
func rejectedMaps() *mapy.Health {
	health := mapy.NewHealth()
	health.Record(fmt.Errorf("tile: %w (status 403)", mapy.ErrUnauthorized))
	return health
}

// healthyConfig builds a Config wired with healthy fakes over the given
// originals directory, so individual tests can override single fields.
func healthyConfig(originals string) Config {
	return Config{
		Maps:       healthyMaps(),
		DB:         fakeDB{},
		Embeddings: fakeHealth{online: true},
		Jobs: fakeJobs{
			byState: map[jobs.State]int{jobs.StateQueued: 3, jobs.StateDead: 2},
			byType:  map[string]int{jobs.TypeImageEmbed: 4},
			pending: 5,
		},
		Imports: fakeImports{runs: map[importer.Source]importer.Run{
			importer.SourcePhotoPrism: {ID: 7, Source: importer.SourcePhotoPrism, Status: importer.StatusDone},
		}},
		Backup:        fakeBackup{status: backup.Status{Configured: true, Running: true}},
		EmbeddingURL:  "http://box:8000",
		OriginalsPath: originals,
		CachePath:     "",
	}
}

// TestCollect_Aggregates verifies a healthy snapshot folds every section
// correctly: job totals/dead-letter/pending, embeddings, backup, the latest
// import per source, storage sizes and reachable database.
func TestCollect_Aggregates(t *testing.T) {
	t.Parallel()

	originals := t.TempDir()
	writeFile(t, filepath.Join(originals, "x.bin"), 120)

	status, err := New(healthyConfig(originals)).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if !status.Database.Reachable {
		t.Error("database not reachable, want reachable")
	}
	if !status.Embeddings.Online || status.Embeddings.URL != "http://box:8000" {
		t.Errorf("embeddings = %+v, want online with url", status.Embeddings)
	}
	if status.Jobs.Total != 5 || status.Jobs.DeadLetter != 2 || status.Jobs.PendingEmbeddings != 5 {
		t.Errorf("jobs = %+v, want total 5 / dead 2 / pending 5", status.Jobs)
	}
	if status.Jobs.ByState["queued"] != 3 {
		t.Errorf("jobs.by_state[queued] = %d, want 3", status.Jobs.ByState["queued"])
	}
	if !status.Backup.Configured || !status.Backup.Running {
		t.Errorf("backup = %+v, want configured + running", status.Backup)
	}
	if status.Imports.PhotoPrism == nil || status.Imports.PhotoPrism.ID != 7 {
		t.Errorf("imports.photoprism = %+v, want run id 7", status.Imports.PhotoPrism)
	}
	if status.Imports.PhotoSorter != nil {
		t.Errorf("imports.photosorter = %+v, want nil", status.Imports.PhotoSorter)
	}
	if status.Storage.OriginalsBytes != 120 {
		t.Errorf("storage.originals = %d, want 120", status.Storage.OriginalsBytes)
	}
	if status.Storage.TotalBytes <= 0 {
		t.Errorf("storage.total = %d, want positive", status.Storage.TotalBytes)
	}
	if !status.Maps.Configured || status.Maps.Degraded || status.Maps.State != string(mapy.HealthOK) {
		t.Errorf("maps = %+v, want configured, healthy and not degraded", status.Maps)
	}
}

// TestCollect_MapsKeyRejected verifies a mapy.com key the provider is rejecting
// shows up as a degraded map backend, so the operator sees it on the dashboard
// instead of only as a grey map. The detail must never carry the key itself.
func TestCollect_MapsKeyRejected(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Maps = rejectedMaps()

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if !status.Maps.Configured {
		t.Error("maps.configured = false, want true (a key is configured, it is just rejected)")
	}
	if !status.Maps.Degraded {
		t.Error("maps.degraded = false, want true (the provider is rejecting the key)")
	}
	if status.Maps.State != string(mapy.HealthKeyRejected) {
		t.Errorf("maps.state = %q, want %q", status.Maps.State, mapy.HealthKeyRejected)
	}
	if status.Maps.Detail == "" {
		t.Error("maps.detail is empty, want a sanitised explanation")
	}
	if status.Maps.CheckedAt == nil {
		t.Error("maps.checked_at is nil, want the time of the observation")
	}
}

// TestCollect_MapsNotConfigured verifies no mapy.com key reports the map backend
// as absent rather than degraded — nothing is broken, maps are simply off.
func TestCollect_MapsNotConfigured(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Maps = nil

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if status.Maps.Configured || status.Maps.Degraded {
		t.Errorf("maps = %+v, want not configured and not degraded", status.Maps)
	}
	if status.Maps.CheckedAt != nil {
		t.Errorf("maps.checked_at = %v, want nil (nothing was ever observed)", status.Maps.CheckedAt)
	}
}

// TestCollect_DatabaseUnreachable verifies a ping failure is reported inline
// (sanitised) without failing the whole collection.
func TestCollect_DatabaseUnreachable(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.DB = fakeDB{err: errors.New("dial tcp 1.2.3.4:5432: connection refused")}

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Database.Reachable {
		t.Error("database reachable, want unreachable")
	}
	if status.Database.Error != "database is unreachable" {
		t.Errorf("database error = %q, want sanitised message", status.Database.Error)
	}
}

// TestCollect_OfflineBox verifies an offline sidecar with queued embedding work
// is reflected as offline with a positive pending backlog.
func TestCollect_OfflineBox(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Embeddings = fakeHealth{online: false}

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Embeddings.Online {
		t.Error("embeddings online, want offline")
	}
	if status.Jobs.PendingEmbeddings == 0 {
		t.Error("pending embeddings = 0, want a backlog while the box is offline")
	}
}

// TestCollect_BackupNotConfigured verifies a nil BackupReporter yields a
// not-configured status rather than panicking.
func TestCollect_BackupNotConfigured(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Backup = nil

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Backup.Configured {
		t.Errorf("backup configured = true, want false when not wired")
	}
}

// TestCollect_JobError verifies a queue read failure surfaces as a collection
// error (the dashboard renders 500 rather than a partial snapshot).
func TestCollect_JobError(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Jobs = fakeJobs{err: errors.New("db down")}

	if _, err := New(cfg).Collect(t.Context()); err == nil {
		t.Error("Collect with failing job counter = nil error, want error")
	}
}

// TestCollect_ImportError verifies an import-history read failure surfaces as a
// collection error.
func TestCollect_ImportError(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Imports = fakeImports{err: errors.New("db down")}

	if _, err := New(cfg).Collect(t.Context()); err == nil {
		t.Error("Collect with failing import lister = nil error, want error")
	}
}

// TestCollect_StorageBestEffort verifies an unreadable originals directory does
// not fail the collection; the byte counts simply fall back to zero.
func TestCollect_StorageBestEffort(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(filepath.Join(t.TempDir(), "missing"))
	cfg.Clock = func() time.Time { return time.Unix(0, 0) }

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Storage.OriginalsBytes != 0 {
		t.Errorf("storage.originals = %d, want 0 for a missing directory", status.Storage.OriginalsBytes)
	}
}
