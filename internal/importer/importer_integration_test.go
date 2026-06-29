//go:build integration

package importer_test

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importer"
)

// TestStore_RunLifecycle walks a run from Start through UpdateCounts to Complete
// and verifies each transition is persisted: status, finished_at, watermark, and
// the final counts.
func TestStore_RunLifecycle(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := importer.NewStore(db.Pool())
	ctx := t.Context()

	run, err := store.Start(ctx, importer.SourcePhotoPrism)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.ID == 0 {
		t.Fatal("Start returned zero id")
	}
	if run.Status != importer.StatusRunning {
		t.Errorf("status after Start = %q, want running", run.Status)
	}
	if run.FinishedAt != nil {
		t.Errorf("finished_at after Start = %v, want nil", run.FinishedAt)
	}

	progress := importer.Counts{Imported: 4, Skipped: 1}
	if err := store.UpdateCounts(ctx, run.ID, progress); err != nil {
		t.Fatalf("UpdateCounts: %v", err)
	}
	mid, err := store.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get after UpdateCounts: %v", err)
	}
	if mid.Counts != progress {
		t.Errorf("counts after UpdateCounts = %+v, want %+v", mid.Counts, progress)
	}
	if mid.Status != importer.StatusRunning {
		t.Errorf("status after UpdateCounts = %q, want running", mid.Status)
	}

	watermark := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	final := importer.Counts{Imported: 10, Updated: 3, Skipped: 2, Failed: 0}
	if err := store.Complete(ctx, run.ID, &watermark, final); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	done, err := store.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get after Complete: %v", err)
	}
	if done.Status != importer.StatusDone {
		t.Errorf("status after Complete = %q, want done", done.Status)
	}
	if done.FinishedAt == nil {
		t.Error("finished_at after Complete = nil, want a timestamp")
	}
	if done.HighWatermark == nil || !done.HighWatermark.Equal(watermark) {
		t.Errorf("high_watermark after Complete = %v, want %v", done.HighWatermark, watermark)
	}
	if done.Counts != final {
		t.Errorf("counts after Complete = %+v, want %+v", done.Counts, final)
	}
}

// TestStore_Fail records a failed run with an error message and confirms it does
// not advance the watermark cursor.
func TestStore_Fail(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := importer.NewStore(db.Pool())
	ctx := t.Context()

	run, err := store.Start(ctx, importer.SourcePhotoSorter)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := store.Fail(ctx, run.ID, "connection refused", importer.Counts{Failed: 7}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	failed, err := store.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get after Fail: %v", err)
	}
	if failed.Status != importer.StatusFailed {
		t.Errorf("status = %q, want failed", failed.Status)
	}
	if failed.LastError != "connection refused" {
		t.Errorf("last_error = %q, want connection refused", failed.LastError)
	}
	if failed.HighWatermark != nil {
		t.Errorf("high_watermark = %v, want nil", failed.HighWatermark)
	}

	if _, ok, err := store.LatestWatermark(ctx, importer.SourcePhotoSorter); err != nil || ok {
		t.Errorf("LatestWatermark after only a failed run = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

// TestStore_LatestWatermark verifies the cursor query returns the most recent
// successful run's watermark per source and ignores failed and still-running
// runs, including a newer running run that must not shadow the done one.
func TestStore_LatestWatermark(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := importer.NewStore(db.Pool())
	ctx := t.Context()

	// No runs yet: not found.
	if _, ok, err := store.LatestWatermark(ctx, importer.SourcePhotoPrism); err != nil || ok {
		t.Fatalf("LatestWatermark with no runs = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// First successful run sets the cursor to older.
	first := mustStart(t, store, importer.SourcePhotoPrism)
	if err := store.Complete(ctx, first, &older, importer.Counts{Imported: 1}); err != nil {
		t.Fatalf("Complete first: %v", err)
	}
	// Second successful run, finished later, advances the cursor to newer.
	second := mustStart(t, store, importer.SourcePhotoPrism)
	if err := store.Complete(ctx, second, &newer, importer.Counts{Imported: 2}); err != nil {
		t.Fatalf("Complete second: %v", err)
	}
	// A failed run with a (would-be) even newer watermark must be ignored: Fail
	// stores no watermark, so this just confirms it cannot win.
	failed := mustStart(t, store, importer.SourcePhotoPrism)
	if err := store.Fail(ctx, failed, "boom", importer.Counts{Failed: 1}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	// A still-running run must not shadow the latest done run.
	mustStart(t, store, importer.SourcePhotoPrism)

	got, ok, err := store.LatestWatermark(ctx, importer.SourcePhotoPrism)
	if err != nil {
		t.Fatalf("LatestWatermark: %v", err)
	}
	if !ok {
		t.Fatal("LatestWatermark ok = false, want true")
	}
	if !got.Equal(newer) {
		t.Errorf("LatestWatermark = %v, want %v", got, newer)
	}

	// A different source has its own independent cursor (still empty here).
	if _, ok, err := store.LatestWatermark(ctx, importer.SourcePhotoSorter); err != nil || ok {
		t.Errorf("LatestWatermark for photosorter = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

// TestStore_LatestRun verifies the per-source latest-run query returns the most
// recently started run regardless of status (unlike LatestWatermark) and keeps
// each source's cursor independent.
func TestStore_LatestRun(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := importer.NewStore(db.Pool())
	ctx := t.Context()

	// No runs yet: not found.
	if _, ok, err := store.LatestRun(ctx, importer.SourcePhotoPrism); err != nil || ok {
		t.Fatalf("LatestRun with no runs = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	// A done run, then a newer failed run for the same source: the failed run is
	// the latest and must win (status is not filtered).
	done := mustStart(t, store, importer.SourcePhotoPrism)
	watermark := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if err := store.Complete(ctx, done, &watermark, importer.Counts{Imported: 3}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	failed := mustStart(t, store, importer.SourcePhotoPrism)
	if err := store.Fail(ctx, failed, "boom", importer.Counts{Failed: 1}); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	got, ok, err := store.LatestRun(ctx, importer.SourcePhotoPrism)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if !ok {
		t.Fatal("LatestRun ok = false, want true")
	}
	if got.ID != failed {
		t.Errorf("LatestRun id = %d, want %d (the newer failed run)", got.ID, failed)
	}
	if got.Status != importer.StatusFailed {
		t.Errorf("LatestRun status = %q, want failed", got.Status)
	}

	// A different source has its own cursor, still empty here.
	if _, ok, err := store.LatestRun(ctx, importer.SourcePhotoSorter); err != nil || ok {
		t.Errorf("LatestRun for photosorter = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	// An unrecognised source is rejected.
	if _, _, err := store.LatestRun(ctx, importer.Source("nope")); !errors.Is(err, importer.ErrInvalidSource) {
		t.Errorf("LatestRun(invalid) error = %v, want ErrInvalidSource", err)
	}
}

// TestStore_Errors covers the not-found and invalid-source error paths.
func TestStore_Errors(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := importer.NewStore(db.Pool())
	ctx := t.Context()

	if _, err := store.Start(ctx, importer.Source("nope")); !errors.Is(err, importer.ErrInvalidSource) {
		t.Errorf("Start(invalid) error = %v, want ErrInvalidSource", err)
	}
	if _, err := store.Get(ctx, 99999); !errors.Is(err, importer.ErrRunNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrRunNotFound", err)
	}
	if err := store.UpdateCounts(ctx, 99999, importer.Counts{}); !errors.Is(err, importer.ErrRunNotFound) {
		t.Errorf("UpdateCounts(missing) error = %v, want ErrRunNotFound", err)
	}
	if err := store.Complete(ctx, 99999, nil, importer.Counts{}); !errors.Is(err, importer.ErrRunNotFound) {
		t.Errorf("Complete(missing) error = %v, want ErrRunNotFound", err)
	}
	if _, _, err := store.LatestWatermark(ctx, importer.Source("nope")); !errors.Is(err, importer.ErrInvalidSource) {
		t.Errorf("LatestWatermark(invalid) error = %v, want ErrInvalidSource", err)
	}
}

// TestStore_List verifies the history listing returns runs across all sources
// most-recently-started first, honours limit and offset, and yields a non-nil
// empty slice when there is no history.
func TestStore_List(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := importer.NewStore(db.Pool())
	ctx := t.Context()

	// Empty history: a non-nil, empty page.
	empty, err := store.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List(empty): %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("List(empty) = %v, want non-nil empty slice", empty)
	}

	// Three runs across both sources, started in order.
	first := mustStart(t, store, importer.SourcePhotoPrism)
	second := mustStart(t, store, importer.SourcePhotoSorter)
	third := mustStart(t, store, importer.SourcePhotoPrism)

	all, err := store.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List returned %d runs, want 3", len(all))
	}
	// Most recently started first: third, second, first.
	wantOrder := []int64{third, second, first}
	for i, want := range wantOrder {
		if all[i].ID != want {
			t.Errorf("List[%d].ID = %d, want %d", i, all[i].ID, want)
		}
	}

	// Paging: limit 1, offset 1 returns just the second-newest run.
	page, err := store.List(ctx, 1, 1)
	if err != nil {
		t.Fatalf("List(paged): %v", err)
	}
	if len(page) != 1 || page[0].ID != second {
		t.Errorf("List(limit=1,offset=1) = %+v, want one run id %d", page, second)
	}
}

// mustStart starts a run and fails the test on error, returning the new run id.
func mustStart(t *testing.T, store *importer.Store, source importer.Source) int64 {
	t.Helper()
	run, err := store.Start(t.Context(), source)
	if err != nil {
		t.Fatalf("Start(%s): %v", source, err)
	}
	return run.ID
}
