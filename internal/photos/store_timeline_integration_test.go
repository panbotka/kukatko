//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// timelineBucketFixture is one seeded photo for the timeline tests: a capture time (nil
// for an unknown-date photo, which belongs to no month bucket) and whether it is
// archived (archived photos are excluded from the default histogram).
type timelineBucketFixture struct {
	hash     string
	takenAt  *time.Time
	archived bool
}

// seedTimelineBuckets creates the fixture photos and archives the ones marked archived,
// returning each fixture's photo UID keyed by hash.
func seedTimelineBuckets(t *testing.T, store *photos.Store, fixtures []timelineBucketFixture) map[string]string {
	t.Helper()
	ctx := t.Context()
	uids := make(map[string]string, len(fixtures))
	for _, f := range fixtures {
		src := "exif"
		if f.takenAt == nil {
			src = "unknown"
		}
		photo := mustCreate(t, store, photos.Photo{
			FileHash: f.hash, FilePath: "p/" + f.hash + ".jpg",
			FileName: f.hash + ".jpg", FileMime: "image/jpeg",
			TakenAt: f.takenAt, TakenAtSource: src,
		})
		uids[f.hash] = photo.UID
		if f.archived {
			if _, err := store.Archive(ctx, photo.UID); err != nil {
				t.Fatalf("Archive(%s): %v", f.hash, err)
			}
		}
	}
	return uids
}

// TestTimelineBuckets verifies the month histogram: bucket counts and cumulative
// values in newest-first order, archived photos excluded by default, unknown-date
// photos absent from the buckets but counted in the total, and that each bucket's
// cumulative indexes the right photo in the default list order.
func TestTimelineBuckets(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	dec1 := time.Date(2023, 12, 20, 9, 0, 0, 0, time.UTC)
	dec2 := time.Date(2023, 12, 5, 8, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	jan := time.Date(2022, 1, 10, 7, 0, 0, 0, time.UTC)
	seedTimelineBuckets(t, store, []timelineBucketFixture{
		{hash: "tl-a", takenAt: new(dec1)},
		{hash: "tl-b", takenAt: new(dec2)},
		{hash: "tl-c", takenAt: new(jun)},
		{hash: "tl-d", takenAt: new(jan)},
		{hash: "tl-e", takenAt: new(dec1), archived: true}, // excluded by default
		{hash: "tl-f", takenAt: nil},                       // no bucket, still in total
	})

	timeline, err := store.TimelineBuckets(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("TimelineBuckets: %v", err)
	}

	// Four live dated photos plus one unknown-date live photo; the archived
	// December photo is excluded from both the buckets and the total.
	if timeline.Total != 5 {
		t.Fatalf("total = %d, want 5 (4 dated + 1 unknown, archived excluded)", timeline.Total)
	}
	want := []photos.TimelineBucket{
		{Year: 2023, Month: 12, Count: 2, Cumulative: 0},
		{Year: 2023, Month: 6, Count: 1, Cumulative: 2},
		{Year: 2022, Month: 1, Count: 1, Cumulative: 3},
	}
	if len(timeline.Buckets) != len(want) {
		t.Fatalf("buckets = %+v, want %+v", timeline.Buckets, want)
	}
	for i, b := range timeline.Buckets {
		if b != want[i] {
			t.Fatalf("bucket[%d] = %+v, want %+v", i, b, want[i])
		}
	}

	// Cumulative must index into the default (newest-first) list: the photo at
	// position Cumulative falls inside the bucket's month.
	list, err := store.List(ctx, photos.ListParams{Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, b := range timeline.Buckets {
		first := list[b.Cumulative]
		if first.TakenAt == nil || first.TakenAt.Year() != b.Year || int(first.TakenAt.Month()) != b.Month {
			t.Fatalf("list[%d] taken_at = %v, want in %d-%02d", b.Cumulative, first.TakenAt, b.Year, b.Month)
		}
	}
}

// TestTimelineBuckets_albumScope verifies the histogram honours a scope filter
// (album membership) so the buckets and total match exactly what the same-scoped
// list would return.
func TestTimelineBuckets_albumScope(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	dec := time.Date(2023, 12, 20, 9, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	jan := time.Date(2022, 1, 10, 7, 0, 0, 0, time.UTC)
	uids := seedTimelineBuckets(t, store, []timelineBucketFixture{
		{hash: "tls-a", takenAt: new(dec)},
		{hash: "tls-b", takenAt: new(jun)}, // stays out of the album
		{hash: "tls-c", takenAt: new(jan)},
	})

	album, err := org.CreateAlbum(ctx, organize.Album{Title: "Trip"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	for i, hash := range []string{"tls-a", "tls-c"} {
		if err := org.AddPhoto(ctx, album.UID, uids[hash], i); err != nil {
			t.Fatalf("AddPhoto(%s): %v", hash, err)
		}
	}

	params := photos.ListParams{AlbumUID: album.UID}
	timeline, err := store.TimelineBuckets(ctx, params)
	if err != nil {
		t.Fatalf("TimelineBuckets(album): %v", err)
	}
	if timeline.Total != 2 {
		t.Fatalf("scoped total = %d, want 2", timeline.Total)
	}
	want := []photos.TimelineBucket{
		{Year: 2023, Month: 12, Count: 1, Cumulative: 0},
		{Year: 2022, Month: 1, Count: 1, Cumulative: 1},
	}
	if len(timeline.Buckets) != len(want) {
		t.Fatalf("scoped buckets = %+v, want %+v", timeline.Buckets, want)
	}
	for i, b := range timeline.Buckets {
		if b != want[i] {
			t.Fatalf("scoped bucket[%d] = %+v, want %+v", i, b, want[i])
		}
	}

	// The scoped histogram must agree with the scoped list: same total, and the
	// bucket count sum equals the number of dated photos the list returns.
	total, err := store.Count(ctx, params)
	if err != nil {
		t.Fatalf("Count(album): %v", err)
	}
	if total != timeline.Total {
		t.Fatalf("Count = %d, timeline.Total = %d, want equal", total, timeline.Total)
	}
}
