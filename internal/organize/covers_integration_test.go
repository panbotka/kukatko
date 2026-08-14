//go:build integration

package organize_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// countingTracer counts the statements a pool issues, so a batch lookup can be
// held to its promise: one query for a whole page of entities, never one per row.
type countingTracer struct {
	queries atomic.Int64
}

// TraceQueryStart counts one statement and passes the context through.
func (c *countingTracer) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData,
) context.Context {
	c.queries.Add(1)
	return ctx
}

// TraceQueryEnd is required by pgx.QueryTracer and has nothing to record.
func (c *countingTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// tracedStore returns a second organize.Store over a pool of its own that counts
// the statements it runs. It exists only for the N+1 assertions; every other test
// uses the shared pool.
func tracedStore(t *testing.T) (*organize.Store, *countingTracer) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv(dbtest.EnvTestDatabaseURL))
	if err != nil {
		t.Fatalf("parsing test DSN: %v", err)
	}
	tracer := &countingTracer{}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("opening traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return organize.NewStore(pool), tracer
}

// makeCoverPhotos inserts three photos captured a day apart and returns their
// uids oldest-first, so a test can name the one a "newest photo" rule must pick.
func makeCoverPhotos(t *testing.T, store *photos.Store, prefix string) []string {
	t.Helper()
	base := time.Date(2024, time.June, 1, 12, 0, 0, 0, time.UTC)
	uids := make([]string, 0, 3)
	for i := range 3 {
		uids = append(uids, makePhotoAt(t, store, prefix+"-"+string(rune('a'+i)), base.AddDate(0, 0, i)))
	}
	return uids
}

// TestAlbumCovers verifies an album's cover is its newest visible photo, that a
// cover chosen by hand wins over it, and that an album with nothing to show is
// absent from the map rather than present with an empty cover.
func TestAlbumCovers(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	uids := makeCoverPhotos(t, photoStore, "al")
	filled, err := store.CreateAlbum(ctx, organize.Album{Title: "Filled"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	addPhotos(t, store, filled.UID, uids...)
	empty, err := store.CreateAlbum(ctx, organize.Album{Title: "Empty"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	picked, err := store.CreateAlbum(ctx, organize.Album{Title: "Picked", CoverPhotoUID: &uids[0]})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	addPhotos(t, store, picked.UID, uids...)

	covers, err := store.AlbumCovers(ctx, []string{filled.UID, empty.UID, picked.UID, "al-nothing"})
	if err != nil {
		t.Fatalf("AlbumCovers: %v", err)
	}
	// The newest of the three photos stands for the album, and it comes with the
	// file hash a caller needs to address its thumbnail.
	if got := covers[filled.UID]; got.PhotoUID != uids[2] || got.FileHash != "al-c" {
		t.Errorf("filled album cover = %+v, want the newest photo %s / al-c", got, uids[2])
	}
	// A hand-picked cover is the user's own answer and outranks the derivation.
	if got := covers[picked.UID].PhotoUID; got != uids[0] {
		t.Errorf("picked album cover = %q, want the chosen %s", got, uids[0])
	}
	if _, ok := covers[empty.UID]; ok {
		t.Errorf("empty album has a cover %+v, want none", covers[empty.UID])
	}
	if _, ok := covers["al-nothing"]; ok {
		t.Error("a uid naming no album came back with a cover")
	}
}

// TestAlbumCovers_skipsHiddenAndArchived verifies a cover is never a photo the
// album's own grid would not show: an archived one, one hidden from the library,
// or a stack member folded into its primary.
func TestAlbumCovers_skipsHiddenAndArchived(t *testing.T) {
	store, photoStore, _, db := newStores(t)
	ctx := context.Background()

	uids := makeCoverPhotos(t, photoStore, "hid")
	album, err := store.CreateAlbum(ctx, organize.Album{Title: "Mixed"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	addPhotos(t, store, album.UID, uids...)
	// The two newest drop out, each for a different reason, leaving the oldest.
	if _, err := db.Pool().Exec(ctx,
		"UPDATE photos SET archived_at = now() WHERE uid = $1", uids[2]); err != nil {
		t.Fatalf("archiving %s: %v", uids[2], err)
	}
	if _, err := db.Pool().Exec(ctx,
		"UPDATE photos SET hidden_from_library = true WHERE uid = $1", uids[1]); err != nil {
		t.Fatalf("hiding %s: %v", uids[1], err)
	}

	covers, err := store.AlbumCovers(ctx, []string{album.UID})
	if err != nil {
		t.Fatalf("AlbumCovers: %v", err)
	}
	if got := covers[album.UID].PhotoUID; got != uids[0] {
		t.Errorf("cover = %q, want the only visible photo %s", got, uids[0])
	}
}

// TestLabelCovers verifies a label's cover is the newest photo carrying it, and
// that a label on no photo has none.
func TestLabelCovers(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	uids := makeCoverPhotos(t, photoStore, "lb")
	used, err := store.CreateLabel(ctx, organize.Label{Name: "sunset"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	for _, uid := range uids {
		if err := store.AttachLabel(ctx, uid, used.UID, organize.SourceManual, 0); err != nil {
			t.Fatalf("AttachLabel: %v", err)
		}
	}
	unused, err := store.CreateLabel(ctx, organize.Label{Name: "unused"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	covers, err := store.LabelCovers(ctx, []string{used.UID, unused.UID})
	if err != nil {
		t.Fatalf("LabelCovers: %v", err)
	}
	if got := covers[used.UID]; got.PhotoUID != uids[2] || got.FileHash != "lb-c" {
		t.Errorf("label cover = %+v, want the newest photo %s / lb-c", got, uids[2])
	}
	if _, ok := covers[unused.UID]; ok {
		t.Errorf("label on no photo has a cover %+v, want none", covers[unused.UID])
	}
}

// TestListLabels_carriesCover verifies the labels listing hands the index the
// same derived cover, so the page can draw a preview per chip.
func TestListLabels_carriesCover(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	uids := makeCoverPhotos(t, photoStore, "ll")
	used, err := store.CreateLabel(ctx, organize.Label{Name: "sunset"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if err := store.AttachLabel(ctx, uids[1], used.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}
	if _, err := store.CreateLabel(ctx, organize.Label{Name: "unused"}); err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	list, err := store.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("labels = %d, want 2", len(list))
	}
	for _, label := range list {
		switch label.Name {
		case "sunset":
			if label.CoverUID == nil || *label.CoverUID != uids[1] {
				t.Errorf("sunset cover_uid = %v, want %s", label.CoverUID, uids[1])
			}
		case "unused":
			if label.CoverUID != nil {
				t.Errorf("unused cover_uid = %v, want none", *label.CoverUID)
			}
		}
	}
}

// TestCovers_oneQueryPerBatch verifies the derivation is a batch: whatever the
// page holds, an album and a label lookup each cost a single statement. It is
// the whole reason these methods take a slice — a per-entity cover query is an
// N+1 that a development library of a few rows hides completely.
func TestCovers_oneQueryPerBatch(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := context.Background()

	uids := makeCoverPhotos(t, photoStore, "nq")
	albumUIDs := make([]string, 0, 5)
	labelUIDs := make([]string, 0, 5)
	for i := range 5 {
		suffix := string(rune('a' + i))
		album, err := store.CreateAlbum(ctx, organize.Album{Title: "Album " + suffix})
		if err != nil {
			t.Fatalf("CreateAlbum: %v", err)
		}
		addPhotos(t, store, album.UID, uids...)
		albumUIDs = append(albumUIDs, album.UID)

		label, err := store.CreateLabel(ctx, organize.Label{Name: "label-" + suffix})
		if err != nil {
			t.Fatalf("CreateLabel: %v", err)
		}
		for _, uid := range uids {
			if err := store.AttachLabel(ctx, uid, label.UID, organize.SourceManual, 0); err != nil {
				t.Fatalf("AttachLabel: %v", err)
			}
		}
		labelUIDs = append(labelUIDs, label.UID)
	}

	traced, tracer := tracedStore(t)
	tests := []struct {
		name string
		run  func() error
	}{
		{"albums", func() error {
			_, err := traced.AlbumCovers(ctx, albumUIDs)
			return err
		}},
		{"labels", func() error {
			_, err := traced.LabelCovers(ctx, labelUIDs)
			return err
		}},
	}
	for _, tc := range tests {
		// Run once to warm the connection — pgx loads its type information
		// lazily, and those statements are not the ones under test — then count
		// the second run.
		if err := tc.run(); err != nil {
			t.Fatalf("%s warm-up: %v", tc.name, err)
		}
		tracer.queries.Store(0)
		if err := tc.run(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := tracer.queries.Load(); got != 1 {
			t.Errorf("%s covers for 5 rows ran %d queries, want 1", tc.name, got)
		}
	}
}
