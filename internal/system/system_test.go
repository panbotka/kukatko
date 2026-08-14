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
	"github.com/panbotka/kukatko/internal/placesjob"
)

// fakeDB is a DBPinger whose Ping returns the configured error.
type fakeDB struct{ err error }

// Ping returns the configured error.
func (f fakeDB) Ping(context.Context) error { return f.err }

// fakeHealth is an EmbeddingHealth returning the configured online flag.
type fakeHealth struct{ online bool }

// Healthy returns the configured online flag.
func (f fakeHealth) Healthy(context.Context) bool { return f.online }

// fakeJobs is a JobCounter backed by a static (type, state) breakdown and a
// pending count.
type fakeJobs struct {
	byTypeState map[jobs.TypeState]int
	pending     int
	err         error
}

// CountsByTypeState returns the configured breakdown or error.
func (f fakeJobs) CountsByTypeState(context.Context) (map[jobs.TypeState]int, error) {
	return f.byTypeState, f.err
}

// CountPending returns the configured pending count or error.
func (f fakeJobs) CountPending(context.Context, ...string) (int, error) {
	return f.pending, f.err
}

// fakeDashboard is a DashboardCounter returning a fixed aggregation or error.
type fakeDashboard struct {
	dashboard Dashboard
	err       error
	calls     int
}

// CountDashboard returns the configured aggregation or error, counting calls so
// a test can assert the memoisation.
func (f *fakeDashboard) CountDashboard(context.Context) (Dashboard, error) {
	f.calls++
	return f.dashboard, f.err
}

// fakeDuplicates is a DuplicateCounter returning a fixed group count or error.
type fakeDuplicates struct {
	groups int
	err    error
}

// CountGroups returns the configured group count or error.
func (f fakeDuplicates) CountGroups(context.Context) (int, error) { return f.groups, f.err }

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

// spentBudget returns a geocode credit budget of limit credits per day with
// spent of them already used, i.e. what the dashboard reads mid-import.
func spentBudget(limit, spent int) *placesjob.WindowBudget {
	budget := placesjob.NewWindowBudget(placesjob.BudgetConfig{Limit: limit, Window: 24 * time.Hour})
	for range spent {
		budget.Reserve()
	}
	return budget
}

// healthyConfig builds a Config wired with healthy fakes over the given
// originals directory, so individual tests can override single fields.
func healthyConfig(originals string) Config {
	return Config{
		Maps:       healthyMaps(),
		Geocode:    spentBudget(1000, 120),
		DB:         fakeDB{},
		Embeddings: fakeHealth{online: true},
		Jobs: fakeJobs{
			byTypeState: map[jobs.TypeState]int{
				{Type: jobs.TypeImageEmbed, State: jobs.StateQueued}: 3,
				{Type: jobs.TypeImageEmbed, State: jobs.StateDead}:   1,
				{Type: jobs.TypeThumbnail, State: jobs.StateDead}:    1,
			},
			pending: 5,
		},
		Dashboard: &fakeDashboard{dashboard: Dashboard{
			Library:   LibrarySummary{Photos: 20, Videos: 2, Trashed: 3, LibraryBytes: 4096},
			Remaining: RemainingWork{FacesUnassigned: 9, Clusters: 2},
		}},
		Imports: fakeImports{runs: map[importer.Source]importer.Run{
			importer.SourceFolder: {ID: 7, Source: importer.SourceFolder, Status: importer.StatusDone},
			// A finished migration run is still in the table; the dashboard section
			// must not resurrect it as if it were live import state.
			importer.SourcePhotoPrism: {ID: 4, Source: importer.SourcePhotoPrism, Status: importer.StatusDone},
		}},
		Backup:        fakeBackup{status: backup.Status{Configured: true, Running: true}},
		EmbeddingURL:  "http://box:8000",
		OriginalsPath: originals,
		CachePath:     "",
	}
}

// TestCollect_Aggregates verifies a healthy snapshot folds every section
// correctly: job totals/dead-letter/pending, embeddings, backup, the latest
// folder import, storage sizes and reachable database.
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
	// The three views are sums over one breakdown, so they must agree: the two
	// dead jobs are one per type, and the lifetime per-type tally adds its states.
	if status.Jobs.ByTypeState[jobs.TypeImageEmbed]["queued"] != 3 ||
		status.Jobs.ByTypeState[jobs.TypeThumbnail]["dead"] != 1 {
		t.Errorf("jobs.by_type_state = %+v, want image_embed queued 3 / thumbnail dead 1",
			status.Jobs.ByTypeState)
	}
	if status.Jobs.ByType[jobs.TypeImageEmbed] != 4 {
		t.Errorf("jobs.by_type[image_embed] = %d, want 4 (3 queued + 1 dead)",
			status.Jobs.ByType[jobs.TypeImageEmbed])
	}
	if !status.Backup.Configured || !status.Backup.Running {
		t.Errorf("backup = %+v, want configured + running", status.Backup)
	}
	if status.Imports.Folder == nil || status.Imports.Folder.ID != 7 {
		t.Errorf("imports.folder = %+v, want run id 7", status.Imports.Folder)
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

// TestCollect_GeocodeBudget verifies the credit budget is reported with its
// spend, remainder and refill instant, so an import's mapy.com spend is visible
// on the dashboard while it happens.
func TestCollect_GeocodeBudget(t *testing.T) {
	t.Parallel()

	status, err := New(healthyConfig(t.TempDir())).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	geocode := status.Geocode
	if !geocode.Configured || !geocode.BudgetEnabled {
		t.Errorf("geocode = %+v, want configured with a budget", geocode)
	}
	if geocode.Limit != 1000 || geocode.Spent != 120 || geocode.Remaining != 880 {
		t.Errorf("geocode = %+v, want 1000 limit / 120 spent / 880 remaining", geocode)
	}
	if geocode.WindowSeconds != (24 * time.Hour).Seconds() {
		t.Errorf("geocode.window_seconds = %v, want %v", geocode.WindowSeconds, (24 * time.Hour).Seconds())
	}
	if geocode.ResetsAt == nil || !geocode.ResetsAt.After(time.Now()) {
		t.Errorf("geocode.resets_at = %v, want a future refill instant", geocode.ResetsAt)
	}
}

// TestCollect_GeocodeNotConfigured verifies no mapy.com key reports the geocode
// section as absent — nothing geocodes, so nothing is spent.
func TestCollect_GeocodeNotConfigured(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Geocode = nil

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Geocode.Configured || status.Geocode.BudgetEnabled || status.Geocode.ResetsAt != nil {
		t.Errorf("geocode = %+v, want the not-configured zero value", status.Geocode)
	}
}

// TestCollect_GeocodeBudgetDisabled verifies a configured key with no budget cap
// reports the spend as unbounded rather than as a zero budget.
func TestCollect_GeocodeBudgetDisabled(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Geocode = placesjob.NewWindowBudget(placesjob.BudgetConfig{Limit: 0})

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !status.Geocode.Configured {
		t.Error("geocode.configured = false, want true (the key is wired, only the cap is off)")
	}
	if status.Geocode.BudgetEnabled {
		t.Errorf("geocode = %+v, want budget_enabled false", status.Geocode)
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

// TestCollect_DashboardSections verifies the two content sections are folded
// into the snapshot and that the one number the catalogue cannot count itself —
// what the derived media weighs — comes from the filesystem measurement rather
// than from the aggregation.
func TestCollect_DashboardSections(t *testing.T) {
	t.Parallel()

	cache := t.TempDir()
	writeFile(t, filepath.Join(cache, "thumb.jpg"), 300)
	cfg := healthyConfig(t.TempDir())
	cfg.CachePath = cache

	status, err := New(cfg).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Library.Photos != 20 || status.Library.Trashed != 3 {
		t.Errorf("library = %+v, want 20 photos / 3 trashed", status.Library)
	}
	if status.Library.DerivedBytes != 300 {
		t.Errorf("library.derived_bytes = %d, want the measured cache size 300",
			status.Library.DerivedBytes)
	}
	if status.Remaining.FacesUnassigned != 9 || status.Remaining.Clusters != 2 {
		t.Errorf("remaining = %+v, want 9 nameless faces / 2 clusters", status.Remaining)
	}
}

// TestCollect_DashboardMemoised verifies the dashboard aggregation is computed
// once per TTL however often the page is polled: a status endpoint refreshing
// every few seconds must not re-run the counts on every request.
func TestCollect_DashboardMemoised(t *testing.T) {
	t.Parallel()

	counter := &fakeDashboard{dashboard: Dashboard{Library: LibrarySummary{Photos: 1}}}
	cfg := healthyConfig(t.TempDir())
	cfg.Dashboard = counter
	cfg.Clock = func() time.Time { return time.Unix(0, 0) }

	svc := New(cfg)
	for range 3 {
		if _, err := svc.Collect(t.Context()); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	if counter.calls != 1 {
		t.Errorf("dashboard counted %d times over 3 polls, want 1", counter.calls)
	}
}

// TestCollect_DashboardError verifies a failing dashboard aggregation fails the
// whole collection: a section of zeroes would read as an empty library rather
// than as an unavailable count.
func TestCollect_DashboardError(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Dashboard = &fakeDashboard{err: errors.New("db down")}

	if _, err := New(cfg).Collect(t.Context()); err == nil {
		t.Error("Collect with a failing dashboard counter = nil error, want error")
	}
}

// TestCollect_DuplicatesNotConfigured verifies an instance with duplicate
// detection switched off reports the tile as not configured rather than as a
// finished scan that found nothing.
func TestCollect_DuplicatesNotConfigured(t *testing.T) {
	t.Parallel()

	status, err := New(healthyConfig(t.TempDir())).Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	scan := status.Remaining.Duplicates
	if scan.Configured || scan.Available || scan.ComputedAt != nil {
		t.Errorf("duplicates = %+v, want the not-configured zero value", scan)
	}
}

// TestCollect_DuplicatesScannedInBackground verifies the scan never runs on the
// request path: the first snapshot reports "no answer yet" and only a later one,
// after the background scan has finished, carries the count and its timestamp.
func TestCollect_DuplicatesScannedInBackground(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Duplicates = fakeDuplicates{groups: 12}
	svc := New(cfg)
	// Run the scheduled scan inline so the test does not race the goroutine; the
	// point under test is that Collect itself does not compute it.
	svc.duplicates.spawn = func(f func()) { f() }

	first, err := svc.Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !first.Remaining.Duplicates.Configured || first.Remaining.Duplicates.Available {
		t.Errorf("first duplicates = %+v, want configured but not yet available",
			first.Remaining.Duplicates)
	}

	second, err := svc.Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	scan := second.Remaining.Duplicates
	if !scan.Available || scan.Groups != 12 || scan.ComputedAt == nil {
		t.Errorf("second duplicates = %+v, want 12 groups with a timestamp", scan)
	}
}

// TestCollect_DuplicateScanFailureKeepsSnapshot verifies a failing scan does not
// fail the status readout: every other number is still worth showing, and the
// tile simply reports no answer.
func TestCollect_DuplicateScanFailureKeepsSnapshot(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Duplicates = fakeDuplicates{err: errors.New("scan failed")}
	svc := New(cfg)
	svc.duplicates.spawn = func(f func()) { f() }

	if _, err := svc.Collect(t.Context()); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	status, err := svc.Collect(t.Context())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if status.Remaining.Duplicates.Available {
		t.Errorf("duplicates = %+v, want unavailable after a failed scan",
			status.Remaining.Duplicates)
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

// TestLatestRuns_EveryConfiguredSource verifies LatestRuns reports every source
// that has run and omits the ones that never have, so a caller can tell "never
// imported" from "imported with zero tallies".
func TestLatestRuns_EveryConfiguredSource(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Imports = fakeImports{runs: map[importer.Source]importer.Run{
		importer.SourcePhotoPrism: {ID: 7, Source: importer.SourcePhotoPrism, Status: importer.StatusDone},
		importer.SourceFolder:     {ID: 9, Source: importer.SourceFolder, Status: importer.StatusRunning},
	}}

	runs, err := New(cfg).LatestRuns(t.Context())
	if err != nil {
		t.Fatalf("LatestRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("LatestRuns() returned %d runs, want 2: %+v", len(runs), runs)
	}
	if runs[importer.SourcePhotoPrism].Status != importer.StatusDone {
		t.Errorf("photoprism status = %q, want done", runs[importer.SourcePhotoPrism].Status)
	}
	if runs[importer.SourceFolder].ID != 9 {
		t.Errorf("folder run id = %d, want 9", runs[importer.SourceFolder].ID)
	}
	if _, ok := runs[importer.SourcePhotoSorter]; ok {
		t.Error("a source that never ran must be absent, not present with a zero run")
	}
}

// TestLatestRuns_Error verifies a failing import lister surfaces as an error
// rather than an empty map a caller would read as "nothing ever imported".
func TestLatestRuns_Error(t *testing.T) {
	t.Parallel()

	cfg := healthyConfig(t.TempDir())
	cfg.Imports = fakeImports{err: errors.New("db down")}

	if _, err := New(cfg).LatestRuns(t.Context()); err == nil {
		t.Error("LatestRuns with a failing lister = nil error, want error")
	}
}
