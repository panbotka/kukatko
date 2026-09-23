package system

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/mapy"
)

// platformConfig is a Config wired only with what Platform may touch: the
// dashboard counter, the storage roots, the backup and the maps reporters. The
// database pinger, the sidecar probe, the queue and the import history are left
// nil on purpose, so a Platform that did any per-request work of Collect's would
// panic instead of passing.
func platformConfig(t *testing.T, counter *fakeDashboard) Config {
	t.Helper()
	originals := t.TempDir()
	writeFile(t, filepath.Join(originals, "a.bin"), 300)
	cacheDir := t.TempDir()
	writeFile(t, filepath.Join(cacheDir, "thumb.jpg"), 40)
	finished := time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	return Config{
		Dashboard: counter,
		Backup: fakeBackup{status: backup.Status{
			Configured: true, LastFinishedAt: &finished, LastSucceededAt: &finished,
		}},
		Maps:             rejectedMaps(),
		StreamingEnabled: true,
		OriginalsPath:    originals,
		CachePath:        cacheDir,
		Clock:            func() time.Time { return time.Unix(0, 0) },
	}
}

// TestPlatform_readsTheCachedSections verifies the scrape-time accessor hands
// over every section, stamps what Collect stamps (the streaming flag, the derived
// bytes from the storage measurement) and shares the dashboard's memoisation
// with Collect's reader instead of counting again.
func TestPlatform_readsTheCachedSections(t *testing.T) {
	t.Parallel()

	counter := &fakeDashboard{dashboard: Dashboard{
		Library:   LibrarySummary{Photos: 20, LibraryBytes: 4096, TrashBytes: 512},
		Remaining: RemainingWork{FacesUnassigned: 9, PhotosWithoutOCR: 4},
		Video:     Video{Videos: 5, Streamable: 3, Missing: 2, NotScheduled: 2},
	}}
	svc := New(platformConfig(t, counter))

	var got Platform
	for range 3 {
		got = svc.Platform(t.Context())
	}
	if counter.calls != 1 {
		t.Errorf("dashboard counted %d times over 3 scrapes, want 1 (memoised)", counter.calls)
	}
	if got.Dashboard == nil {
		t.Fatal("Platform().Dashboard = nil with a healthy counter")
	}
	if !got.Dashboard.Video.StreamingEnabled {
		t.Error("Video.StreamingEnabled = false, want the configured flag stamped")
	}
	if got.Dashboard.Remaining.FacesUnassigned != 9 || got.Dashboard.Library.TrashBytes != 512 {
		t.Errorf("Dashboard = %+v, want the counter's aggregation", *got.Dashboard)
	}
	if got.Storage == nil {
		t.Fatal("Platform().Storage = nil with readable directories")
	}
	if got.Storage.OriginalsBytes != 300 || got.Storage.CacheBytes != 40 || got.Storage.TotalBytes <= 0 {
		t.Errorf("Storage = %+v, want 300 originals, 40 cache and a real filesystem", *got.Storage)
	}
	if got.Dashboard.Library.DerivedBytes != 40 {
		t.Errorf("DerivedBytes = %d, want the cache measurement 40", got.Dashboard.Library.DerivedBytes)
	}
	if !got.Backup.Configured || got.Backup.LastSucceededAt == nil {
		t.Errorf("Backup = %+v, want the reporter's status", got.Backup)
	}
	if !got.Maps.Configured || got.Maps.State != string(mapy.HealthKeyRejected) || got.Maps.CheckedAt == nil {
		t.Errorf("Maps = %+v, want the rejected key with its check time", got.Maps)
	}
}

// TestPlatform_dashboardFailureDropsOnlyItsSection verifies a failing aggregation
// is reported as a nil section — never as zeroes, which would read as an empty
// library — while the sections that did not fail are still handed over.
func TestPlatform_dashboardFailureDropsOnlyItsSection(t *testing.T) {
	t.Parallel()

	got := New(platformConfig(t, &fakeDashboard{err: errors.New("db down")})).Platform(t.Context())
	if got.Dashboard != nil {
		t.Errorf("Dashboard = %+v with a failing counter, want nil", *got.Dashboard)
	}
	if got.Storage == nil || !got.Backup.Configured || !got.Maps.Configured {
		t.Errorf("a dashboard failure took other sections with it: %+v", got)
	}
}

// TestPlatform_storageFailureDropsOnlyItsSection verifies a failed measurement is
// a nil section (a partial one would read as a full disk) and leaves the
// dashboard in place, without the derived bytes it could not measure.
func TestPlatform_storageFailureDropsOnlyItsSection(t *testing.T) {
	t.Parallel()

	counter := &fakeDashboard{dashboard: Dashboard{Library: LibrarySummary{Photos: 1}}}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	got := New(platformConfig(t, counter)).Platform(cancelled)
	if got.Storage != nil {
		t.Errorf("Storage = %+v after an interrupted walk, want nil", *got.Storage)
	}
	if got.Dashboard == nil {
		t.Fatal("a storage failure dropped the dashboard section")
	}
	if got.Dashboard.Library.DerivedBytes != 0 {
		t.Errorf("DerivedBytes = %d without a measurement, want 0", got.Dashboard.Library.DerivedBytes)
	}
}

// TestPlatform_unconfiguredProviders verifies an instance without a backup
// destination or a mapy.com key reports both as not configured rather than as
// failing.
func TestPlatform_unconfiguredProviders(t *testing.T) {
	t.Parallel()

	cfg := platformConfig(t, &fakeDashboard{})
	cfg.Backup = nil
	cfg.Maps = nil

	got := New(cfg).Platform(t.Context())
	if got.Backup.Configured || got.Maps.Configured {
		t.Errorf("Backup = %+v, Maps = %+v, want both not configured", got.Backup, got.Maps)
	}
}
