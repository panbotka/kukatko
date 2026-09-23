package metrics

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Fixed instants of the sample platform snapshot.
var (
	backupFailedAt    = time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC)
	backupSucceededAt = time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	mapsCheckedAt     = time.Date(2026, 9, 23, 9, 30, 0, 0, time.UTC)
	encodeQueuedAt    = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
)

// samplePlatform is a complete platform snapshot in which last night's backup
// failed after an earlier success, the map key is being rejected and a queued
// encode has waited for days — the situations the series exist to alert on.
func samplePlatform() PlatformSnapshot {
	return PlatformSnapshot{
		Backup: &BackupState{
			LastFinishedAt: backupFailedAt, LastSucceededAt: backupSucceededAt, LastSucceeded: false,
			OriginalsUploaded: 12, OriginalsSkipped: 20_400,
		},
		Maps: &MapsState{Up: false, CheckedAt: mapsCheckedAt},
		Disk: &DiskUsage{OriginalsBytes: 1_000, CacheBytes: 200, FreeBytes: 5_000, TotalBytes: 9_000},
		Catalogue: &CatalogueState{
			StreamingEnabled: true,
			VideosByEncodeState: map[string]int{
				EncodeStreamable: 300, EncodeQueued: 4, EncodeRunning: 1, EncodeFailed: 2, EncodeNotScheduled: 50,
			},
			OldestQueuedEncode: encodeQueuedAt,
			Backlog: map[string]int{
				BacklogFacesUnassigned: 3_164, BacklogClusters: 41, BacklogPhotosWithoutTakenAt: 7,
				BacklogPhotosWithoutGPS: 9_000, BacklogPhotosWithoutOCR: 120, BacklogDuplicateMarkers: 2,
			},
			Bytes: map[string]int64{BytesLive: 70_000, BytesTrash: 600, BytesDerived: 200},
		},
	}
}

// unix renders at the way the exposition format prints a timestamp gauge.
func unix(at time.Time) string {
	return strconv.FormatFloat(float64(at.Unix()), 'g', -1, 64)
}

// registryWithPlatform returns a registry whose platform collector serves fn.
func registryWithPlatform(fn func() PlatformSnapshot) *Registry {
	r := New()
	r.RegisterPlatform(func(context.Context) PlatformSnapshot { return fn() })
	return r
}

// assertSeries fails the test for every wanted series missing from body.
func assertSeries(t *testing.T, body string, want []string) {
	t.Helper()
	for _, series := range want {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", series, body)
		}
	}
}

// assertNoSeries fails the test for every series prefix present in body.
func assertNoSeries(t *testing.T, body string, unwanted []string) {
	t.Helper()
	for _, prefix := range unwanted {
		if strings.Contains(body, "\n"+prefix) {
			t.Errorf("/metrics output has %q, want it absent\n--- got ---\n%s", prefix, body)
		}
	}
}

// TestRegisterPlatform_exportsEverySection verifies every platform family reaches
// the exposition with its labels and values intact — in particular that a failed
// backup after an earlier success reads as "last run failed" while still telling
// how long ago the last good one was.
func TestRegisterPlatform_exportsEverySection(t *testing.T) {
	t.Parallel()

	body := scrape(t, registryWithPlatform(samplePlatform))
	assertSeries(t, body, []string{
		`kukatko_backup_configured 1`,
		`kukatko_backup_running 0`,
		`kukatko_backup_last_run_success 0`,
		`kukatko_backup_last_run_finish_timestamp_seconds ` + unix(backupFailedAt),
		`kukatko_backup_last_success_timestamp_seconds ` + unix(backupSucceededAt),
		`kukatko_backup_last_run_originals{result="uploaded"} 12`,
		`kukatko_backup_last_run_originals{result="skipped"} 20400`,
		`kukatko_maps_configured 1`,
		`kukatko_maps_up 0`,
		`kukatko_maps_last_check_timestamp_seconds ` + unix(mapsCheckedAt),
		`kukatko_disk_tree_bytes{tree="originals"} 1000`,
		`kukatko_disk_tree_bytes{tree="cache"} 200`,
		`kukatko_disk_filesystem_free_bytes 5000`,
		`kukatko_disk_filesystem_size_bytes 9000`,
		`kukatko_library_video_streaming_enabled 1`,
		`kukatko_library_videos_by_encode_state{state="streamable"} 300`,
		`kukatko_library_videos_by_encode_state{state="queued"} 4`,
		`kukatko_library_videos_by_encode_state{state="running"} 1`,
		`kukatko_library_videos_by_encode_state{state="failed"} 2`,
		`kukatko_library_videos_by_encode_state{state="not_scheduled"} 50`,
		`kukatko_library_video_encode_oldest_queued_timestamp_seconds ` + unix(encodeQueuedAt),
		`kukatko_library_backlog{kind="faces_unassigned"} 3164`,
		`kukatko_library_backlog{kind="clusters"} 41`,
		`kukatko_library_backlog{kind="photos_without_taken_at"} 7`,
		`kukatko_library_backlog{kind="photos_without_gps"} 9000`,
		`kukatko_library_backlog{kind="photos_without_ocr"} 120`,
		`kukatko_library_backlog{kind="duplicate_markers"} 2`,
		`kukatko_library_bytes{set="live"} 70000`,
		`kukatko_library_bytes{set="trash"} 600`,
		`kukatko_library_bytes{set="derived"} 200`,
		`kukatko_platform_collect_errors_total{source="catalogue"} 0`,
		`kukatko_platform_collect_errors_total{source="disk"} 0`,
	})
}

// TestRegisterPlatform_failedSourceDropsOnlyItsFamilies verifies a failed disk
// measurement and a failed catalogue aggregation each remove only their own
// series and bump only their own error counter, while the in-memory backup and
// map series still reach the scrape.
func TestRegisterPlatform_failedSourceDropsOnlyItsFamilies(t *testing.T) {
	t.Parallel()

	snapshot := samplePlatform()
	snapshot.Disk = nil
	body := scrape(t, registryWithPlatform(func() PlatformSnapshot { return snapshot }))
	assertNoSeries(t, body, []string{"kukatko_disk_"})
	assertSeries(t, body, []string{
		`kukatko_platform_collect_errors_total{source="disk"} 1`,
		`kukatko_platform_collect_errors_total{source="catalogue"} 0`,
		`kukatko_library_backlog{kind="clusters"} 41`,
		`kukatko_backup_last_run_success 0`,
		`kukatko_maps_up 0`,
	})

	snapshot = samplePlatform()
	snapshot.Catalogue = nil
	body = scrape(t, registryWithPlatform(func() PlatformSnapshot { return snapshot }))
	assertNoSeries(t, body, []string{
		"kukatko_library_backlog", "kukatko_library_bytes", "kukatko_library_videos_by_encode_state",
		"kukatko_library_video_encode_oldest", "kukatko_library_video_streaming_enabled",
	})
	assertSeries(t, body, []string{
		`kukatko_platform_collect_errors_total{source="catalogue"} 1`,
		`kukatko_platform_collect_errors_total{source="disk"} 0`,
		`kukatko_disk_filesystem_free_bytes 5000`,
		`kukatko_backup_configured 1`,
	})
}

// TestRegisterPlatform_errorCounterAccumulates verifies the error counter is a
// real counter across scrapes, so rate() and increase() over it mean something.
func TestRegisterPlatform_errorCounterAccumulates(t *testing.T) {
	t.Parallel()

	r := registryWithPlatform(func() PlatformSnapshot { return PlatformSnapshot{} })
	scrape(t, r)
	body := scrape(t, r)
	assertSeries(t, body, []string{
		`kukatko_platform_collect_errors_total{source="catalogue"} 2`,
		`kukatko_platform_collect_errors_total{source="disk"} 2`,
	})
}

// TestRegisterPlatform_unobservedStatesAreAbsent verifies what has not happened
// yet is not exported as a zero: no backup outcome before a run has finished, no
// map health before a call was observed, no oldest-queued time with nothing
// queued — and an unconfigured subsystem says so instead of looking broken.
func TestRegisterPlatform_unobservedStatesAreAbsent(t *testing.T) {
	t.Parallel()

	snapshot := samplePlatform()
	snapshot.Backup = &BackupState{Running: true}
	snapshot.Maps = &MapsState{}
	snapshot.Catalogue.OldestQueuedEncode = time.Time{}
	body := scrape(t, registryWithPlatform(func() PlatformSnapshot { return snapshot }))
	assertSeries(t, body, []string{`kukatko_backup_running 1`, `kukatko_maps_configured 1`})
	assertNoSeries(t, body, []string{
		"kukatko_backup_last_run_success", "kukatko_backup_last_run_finish",
		"kukatko_backup_last_success_timestamp", "kukatko_backup_last_run_originals",
		"kukatko_maps_up", "kukatko_maps_last_check", "kukatko_library_video_encode_oldest",
	})

	snapshot.Backup = nil
	snapshot.Maps = nil
	body = scrape(t, registryWithPlatform(func() PlatformSnapshot { return snapshot }))
	assertSeries(t, body, []string{`kukatko_backup_configured 0`, `kukatko_maps_configured 0`})
	assertNoSeries(t, body, []string{"kukatko_backup_running", "kukatko_maps_up"})
}

// TestRegisterPlatform_nilIsNoop verifies an instance wired without a platform
// source exports none of its series rather than panicking.
func TestRegisterPlatform_nilIsNoop(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterPlatform(nil)
	assertNoSeries(t, scrape(t, r), []string{"kukatko_backup_", "kukatko_platform_"})
}

// TestRegisterPlatform_callsSourceOncePerScrape verifies a scrape reads the
// source exactly once, so every family of one scrape comes from the same
// snapshot and the collector adds no load beyond what its source memoises.
func TestRegisterPlatform_callsSourceOncePerScrape(t *testing.T) {
	t.Parallel()

	calls := 0
	r := registryWithPlatform(func() PlatformSnapshot {
		calls++
		return samplePlatform()
	})
	scrape(t, r)
	if calls != 1 {
		t.Errorf("one scrape read the source %d times, want 1", calls)
	}
}
