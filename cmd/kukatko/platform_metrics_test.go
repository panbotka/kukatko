package main

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/system"
)

// TestPlatformSnapshot_mapsEverySection verifies the adapter between the system
// accessor and the metric shape: every dashboard count lands under the label
// value an operator queries by, and the in-memory states keep their timestamps.
func TestPlatformSnapshot_mapsEverySection(t *testing.T) {
	t.Parallel()

	queued := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	checked := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	got := platformSnapshot(system.Platform{
		Dashboard: &system.Dashboard{
			Library: system.LibrarySummary{LibraryBytes: 700, TrashBytes: 60, DerivedBytes: 20},
			Remaining: system.RemainingWork{
				FacesUnassigned: 1, Clusters: 2, PhotosWithoutTakenAt: 3,
				PhotosWithoutGPS: 4, PhotosWithoutOCR: 5, DuplicateMarkers: 6,
			},
			Video: system.Video{
				StreamingEnabled: true, Streamable: 10, EncodeQueued: 11, EncodeRunning: 12,
				EncodeFailed: 13, NotScheduled: 14, OldestQueuedAt: &queued,
			},
		},
		Storage: &system.StorageUsage{OriginalsBytes: 100, CacheBytes: 20, FreeBytes: 30, TotalBytes: 40},
		Backup:  backup.Status{Configured: true},
		Maps:    system.Maps{Configured: true, Degraded: true, CheckedAt: &checked},
	})

	if got.Catalogue == nil || got.Disk == nil || got.Backup == nil || got.Maps == nil {
		t.Fatalf("a section went missing: %+v", got)
	}
	c := got.Catalogue
	checks := []struct {
		name      string
		got, want int64
	}{
		{"streamable", int64(c.VideosByEncodeState[metrics.EncodeStreamable]), 10},
		{"queued", int64(c.VideosByEncodeState[metrics.EncodeQueued]), 11},
		{"running", int64(c.VideosByEncodeState[metrics.EncodeRunning]), 12},
		{"failed", int64(c.VideosByEncodeState[metrics.EncodeFailed]), 13},
		{"not scheduled", int64(c.VideosByEncodeState[metrics.EncodeNotScheduled]), 14},
		{"faces unassigned", int64(c.Backlog[metrics.BacklogFacesUnassigned]), 1},
		{"clusters", int64(c.Backlog[metrics.BacklogClusters]), 2},
		{"without taken_at", int64(c.Backlog[metrics.BacklogPhotosWithoutTakenAt]), 3},
		{"without gps", int64(c.Backlog[metrics.BacklogPhotosWithoutGPS]), 4},
		{"without ocr", int64(c.Backlog[metrics.BacklogPhotosWithoutOCR]), 5},
		{"duplicate markers", int64(c.Backlog[metrics.BacklogDuplicateMarkers]), 6},
		{"bytes live", c.Bytes[metrics.BytesLive], 700},
		{"bytes trash", c.Bytes[metrics.BytesTrash], 60},
		{"bytes derived", c.Bytes[metrics.BytesDerived], 20},
		{"disk originals", got.Disk.OriginalsBytes, 100},
		{"disk free", got.Disk.FreeBytes, 30},
		{"disk total", got.Disk.TotalBytes, 40},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %d, want %d", check.name, check.got, check.want)
		}
	}
	if !c.StreamingEnabled || !c.OldestQueuedEncode.Equal(queued) {
		t.Errorf("catalogue = %+v, want streaming on and the oldest queued time", *c)
	}
	if got.Maps.Up || !got.Maps.CheckedAt.Equal(checked) {
		t.Errorf("maps = %+v, want down (degraded) with its check time", *got.Maps)
	}
}

// TestPlatformSnapshot_missingSections verifies a failed or unconfigured source
// stays nil, and that the derived bytes are not exported without a storage
// measurement (the dashboard's zero would read as an empty cache).
func TestPlatformSnapshot_missingSections(t *testing.T) {
	t.Parallel()

	got := platformSnapshot(system.Platform{Dashboard: &system.Dashboard{}})
	if got.Disk != nil || got.Backup != nil || got.Maps != nil {
		t.Errorf("got %+v, want disk, backup and maps nil", got)
	}
	if _, ok := got.Catalogue.Bytes[metrics.BytesDerived]; ok {
		t.Error("derived bytes exported without a storage measurement")
	}
	if got := platformSnapshot(system.Platform{}); got.Catalogue != nil {
		t.Errorf("Catalogue = %+v without a dashboard, want nil", *got.Catalogue)
	}
}

// TestBackupState_lastRunOutcome verifies how the latest run's success is read
// off the status: the run succeeded exactly when its finish time is also the
// latest success time, whether or not a new run is in progress.
func TestBackupState_lastRunOutcome(t *testing.T) {
	t.Parallel()

	earlier := time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	later := earlier.Add(24 * time.Hour)
	tests := []struct {
		name   string
		status backup.Status
		want   bool
	}{
		{"succeeded", backup.Status{Configured: true, LastFinishedAt: &later, LastSucceededAt: &later}, true},
		{"failed after a success", backup.Status{
			Configured: true, LastFinishedAt: &later, LastSucceededAt: &earlier,
		}, false},
		{"failed, never succeeded", backup.Status{Configured: true, LastFinishedAt: &later}, false},
		{"running after a success", backup.Status{
			Configured: true, Running: true, LastFinishedAt: &earlier, LastSucceededAt: &earlier,
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := backupState(tt.status).LastSucceeded; got != tt.want {
				t.Errorf("LastSucceeded = %v, want %v", got, tt.want)
			}
		})
	}
	if backupState(backup.Status{}) != nil {
		t.Error("an unconfigured backup must map to nil")
	}
}
