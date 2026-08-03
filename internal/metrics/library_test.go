package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// sampleSnapshot is a small but complete library snapshot: every labelled family
// has at least two label values and one import source is finished while another
// is still running, so a scrape of it exercises every branch of the collector.
func sampleSnapshot() LibrarySnapshot {
	started := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	finished := time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC)
	return LibrarySnapshot{
		PhotosByMediaType: map[string]int{"image": 20_500, "video": 380, "live": 9},
		PhotosArchived:    42,
		PhotosProcessed: map[string]int{
			StageEmbedding: 20_647, StageFaces: 9_800, StagePlaces: 4_010,
		},
		PhotosPending: map[string]int{
			StageEmbedding: 242, StageFaces: 11_089, StagePlaces: 130,
		},
		Embeddings:     20_647,
		Faces:          115_457,
		MarkersByState: map[string]int{MarkerAssigned: 18_003, MarkerUnassigned: 3_164},
		SubjectsByType: map[string]int{"person": 100, "pet": 4, "other": 1},
		AlbumsByType:   map[string]int{"album": 400, "folder": 30, "month": 7},
		Labels:         113,
		Imports: []ImportRun{
			{
				Source: "photoprism", Status: "done",
				Statuses:  []string{"running", "done", "partial", "failed"},
				StartedAt: started, FinishedAt: finished,
			},
			{
				Source: "folder", Status: "running",
				Statuses:  []string{"running", "done", "partial", "failed"},
				StartedAt: started,
			},
		},
	}
}

// TestRegisterLibrary_exportsCounts verifies every library family reaches the
// exposition with its label values and value intact — the counts an operator
// actually reads off the dashboard.
func TestRegisterLibrary_exportsCounts(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterLibrary(func(context.Context) (LibrarySnapshot, error) {
		return sampleSnapshot(), nil
	}, time.Minute)

	body := scrape(t, r)
	want := []string{
		`kukatko_library_photos{media_type="image"} 20500`,
		`kukatko_library_photos{media_type="video"} 380`,
		`kukatko_library_photos{media_type="live"} 9`,
		`kukatko_library_photos_archived 42`,
		`kukatko_library_photos_processed{stage="embedding"} 20647`,
		`kukatko_library_photos_pending{stage="places"} 130`,
		`kukatko_library_embeddings 20647`,
		`kukatko_library_faces 115457`,
		`kukatko_library_markers{state="unassigned"} 3164`,
		`kukatko_library_subjects{type="person"} 100`,
		`kukatko_library_albums{type="folder"} 30`,
		`kukatko_library_labels 113`,
		`kukatko_library_collect_errors_total 0`,
	}
	for _, series := range want {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", series, body)
		}
	}
}

// TestRegisterLibrary_exportsImportRuns verifies the import gauges: the status a
// run is in reads 1 while every other known status reads 0 (so a transition is
// visible rather than a series silently vanishing), and a run still in flight
// exports no finish timestamp.
func TestRegisterLibrary_exportsImportRuns(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterLibrary(func(context.Context) (LibrarySnapshot, error) {
		return sampleSnapshot(), nil
	}, time.Minute)

	body := scrape(t, r)
	want := []string{
		`kukatko_import_last_run_status{source="photoprism",status="done"} 1`,
		`kukatko_import_last_run_status{source="photoprism",status="failed"} 0`,
		`kukatko_import_last_run_status{source="folder",status="running"} 1`,
		`kukatko_import_last_run_start_timestamp_seconds{source="photoprism"} 1.7855784e+09`,
		`kukatko_import_last_run_finish_timestamp_seconds{source="photoprism"} 1.7855802e+09`,
	}
	for _, series := range want {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", series, body)
		}
	}
	if strings.Contains(body, `kukatko_import_last_run_finish_timestamp_seconds{source="folder"}`) {
		t.Error("a running import must not export a finish timestamp")
	}
}

// TestLibraryCollector_memoises verifies the aggregation is recomputed at most
// once per TTL. This is the whole point of the cache: /metrics is scraped
// forever, and these counts are aggregates over the largest tables there are.
func TestLibraryCollector_memoises(t *testing.T) {
	t.Parallel()

	calls := 0
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	collector := newLibraryCollector(func(context.Context) (LibrarySnapshot, error) {
		calls++
		return sampleSnapshot(), nil
	}, time.Minute, func() time.Time { return now })

	r := New()
	r.reg.MustRegister(collector)

	scrape(t, r)
	scrape(t, r)
	if calls != 1 {
		t.Errorf("two scrapes within the TTL triggered %d aggregations, want 1", calls)
	}

	now = now.Add(2 * time.Minute)
	scrape(t, r)
	if calls != 2 {
		t.Errorf("a scrape past the TTL triggered %d aggregations in total, want 2", calls)
	}
}

// TestLibraryCollector_errorHidesGauges verifies a failed aggregation exports no
// library gauges — a gap, not a stale or zeroed count that would read as a
// library that stopped growing — while still bumping the error counter so the
// failure is alertable.
func TestLibraryCollector_errorHidesGauges(t *testing.T) {
	t.Parallel()

	failing := true
	collector := newLibraryCollector(func(context.Context) (LibrarySnapshot, error) {
		if failing {
			return LibrarySnapshot{}, errors.New("database is down")
		}
		return sampleSnapshot(), nil
	}, time.Nanosecond, nil)

	r := New()
	r.reg.MustRegister(collector)

	body := scrape(t, r)
	if strings.Contains(body, "kukatko_library_photos{") {
		t.Errorf("a failed aggregation must export no library gauges, got:\n%s", body)
	}
	if !strings.Contains(body, "kukatko_library_collect_errors_total 1") {
		t.Errorf("expected one recorded collect error, got:\n%s", body)
	}

	failing = false
	body = scrape(t, r)
	if !strings.Contains(body, `kukatko_library_photos{media_type="image"} 20500`) {
		t.Errorf("the collector must recover once the aggregation succeeds, got:\n%s", body)
	}
	if !strings.Contains(body, "kukatko_library_collect_errors_total 1") {
		t.Errorf("a successful scrape must not bump the error counter, got:\n%s", body)
	}
}

// TestLibraryCollector_staleValueIsNotServed verifies a failure past the TTL
// hides the numbers rather than re-serving the last successful snapshot, which
// would pin the gauges at a size the library no longer has.
func TestLibraryCollector_staleValueIsNotServed(t *testing.T) {
	t.Parallel()

	failing := false
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	collector := newLibraryCollector(func(context.Context) (LibrarySnapshot, error) {
		if failing {
			return LibrarySnapshot{}, errors.New("database is down")
		}
		return sampleSnapshot(), nil
	}, time.Minute, func() time.Time { return now })

	r := New()
	r.reg.MustRegister(collector)
	if body := scrape(t, r); !strings.Contains(body, "kukatko_library_faces 115457") {
		t.Fatalf("expected the first scrape to succeed, got:\n%s", body)
	}

	failing = true
	now = now.Add(2 * time.Minute)
	if body := scrape(t, r); strings.Contains(body, "kukatko_library_faces") {
		t.Errorf("a stale snapshot must not be served past its TTL, got:\n%s", body)
	}
}

// TestRegisterLibrary_nilIsNoop verifies an instance wired without a catalogue
// source registers nothing rather than panicking on the nil function.
func TestRegisterLibrary_nilIsNoop(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterLibrary(nil, time.Minute)
	if body := scrape(t, r); strings.Contains(body, "kukatko_library_") {
		t.Errorf("a nil library func should register no series, got:\n%s", body)
	}
}

// TestNewLibraryCollector_defaults verifies a non-positive TTL and a nil clock
// fall back to the package defaults, so production callers may leave both unset.
func TestNewLibraryCollector_defaults(t *testing.T) {
	t.Parallel()

	collector := newLibraryCollector(func(context.Context) (LibrarySnapshot, error) {
		return LibrarySnapshot{}, nil
	}, 0, nil)
	if collector.ttl != defaultLibraryTTL {
		t.Errorf("ttl = %v, want the default %v", collector.ttl, defaultLibraryTTL)
	}
	if collector.now == nil {
		t.Error("a nil clock should fall back to time.Now")
	}
}

// TestRegisterJobQueue_foldsDimensions verifies the per-state and per-type gauges
// are sums over the single type/state breakdown, so one query answers all three
// families and they can never disagree with each other.
func TestRegisterJobQueue_foldsDimensions(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterJobQueue(func(context.Context) (map[QueueCell]int, error) {
		return map[QueueCell]int{
			{Type: "image_embed", State: "queued"}:  5,
			{Type: "image_embed", State: "running"}: 2,
			{Type: "thumbnail", State: "queued"}:    3,
		}, nil
	})

	body := scrape(t, r)
	want := []string{
		`kukatko_jobs_queue_depth_by_type_state{state="queued",type="image_embed"} 5`,
		`kukatko_jobs_queue_depth{state="queued"} 8`,
		`kukatko_jobs_queue_depth{state="running"} 2`,
		`kukatko_jobs_queue_depth_by_type{type="image_embed"} 7`,
		`kukatko_jobs_queue_depth_by_type{type="thumbnail"} 3`,
	}
	for _, series := range want {
		if !strings.Contains(body, series) {
			t.Errorf("/metrics output missing %q\n--- got ---\n%s", series, body)
		}
	}
}

// TestRegisterJobQueue_errorDropsGauges verifies a failing queue query drops the
// queue gauges for that scrape instead of failing the whole /metrics response,
// which would take every other series down with it.
func TestRegisterJobQueue_errorDropsGauges(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterJobQueue(func(context.Context) (map[QueueCell]int, error) {
		return nil, errors.New("database is down")
	})

	body := scrape(t, r)
	if strings.Contains(body, "kukatko_jobs_queue_depth") {
		t.Errorf("a failed queue query should export no depth gauges, got:\n%s", body)
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Errorf("the rest of the scrape must survive a queue query failure, got:\n%s", body)
	}
}

// TestRegisterJobQueue_nilIsNoop verifies an unwired queue registers nothing
// rather than panicking.
func TestRegisterJobQueue_nilIsNoop(t *testing.T) {
	t.Parallel()

	r := New()
	r.RegisterJobQueue(nil)
	if body := scrape(t, r); strings.Contains(body, "kukatko_jobs_queue_depth") {
		t.Errorf("a nil queue func should register no gauge, got:\n%s", body)
	}
}
