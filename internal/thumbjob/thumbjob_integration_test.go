//go:build integration

package thumbjob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/thumbjob"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// harness bundles the live collaborators a thumbjob.Service needs.
type harness struct {
	db          *database.DB
	photos      *photos.Store
	jobs        *jobs.Store
	storage     storage.Storage
	thumbnailer *thumb.Thumbnailer
}

// newHarness builds the live stores and on-disk storage over a freshly truncated
// integration database and isolated temp directories.
func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return &harness{
		db:          db,
		photos:      photos.NewStore(db.Pool()),
		jobs:        jobs.NewStore(db.Pool()),
		storage:     store,
		thumbnailer: thumb.New(store, filepath.Join(root, "cache")),
	}
}

// newService wires a thumbjob.Service with the full set of collaborators, so it
// can both regenerate one photo and drive a backfill.
func (h *harness) newService() *thumbjob.Service {
	return thumbjob.New(thumbjob.Config{
		Photos:      h.photos,
		Thumbnailer: h.thumbnailer,
		Decoder:     thumbjob.NewStorageDecoder(h.storage),
		Lister:      h.photos,
		Enqueuer:    jobs.NewEnqueuer(h.jobs),
	})
}

// storeJPEG stores a solid-colour JPEG through the originals store and inserts a
// photos row referencing it, returning the created photo. The colour both makes
// each photo's bytes (and thus content hash) distinct and gives the placeholder
// something recognisable to describe.
func (h *harness) storeJPEG(t *testing.T, name string, c color.RGBA) photos.Photo {
	t.Helper()
	const w, h2 = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h2))
	for y := range h2 {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	sf, err := h.storage.Store(context.Background(), &buf, time.Time{}, name+".jpg")
	if err != nil {
		t.Fatalf("store original: %v", err)
	}
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash:        sf.Hash,
		FilePath:        sf.RelPath,
		FileName:        name + ".jpg",
		FileSize:        sf.Size,
		FileMime:        "image/jpeg",
		FileWidth:       w,
		FileHeight:      h2,
		MediaType:       photos.MediaImage,
		FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo: %v", err)
	}
	return created
}

// queuedThumbnailUIDs returns the photo uids of every queued thumbnail job.
func (h *harness) queuedThumbnailUIDs(t *testing.T) []string {
	t.Helper()
	queued := jobs.StateQueued
	list, err := h.jobs.List(t.Context(), jobs.ListOptions{State: &queued, Limit: 100})
	if err != nil {
		t.Fatalf("jobs.List: %v", err)
	}
	var uids []string
	for _, job := range list {
		if job.Type != jobs.TypeThumbnail {
			t.Errorf("the placeholder backfill scheduled a %q job", job.Type)
			continue
		}
		var payload struct {
			PhotoUID string `json:"photo_uid"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			t.Fatalf("decode payload %s: %v", job.Payload, err)
		}
		uids = append(uids, payload.PhotoUID)
	}
	sort.Strings(uids)
	return uids
}

// TestRegenerate_storesAPlaceholder is the end-to-end claim: the job renders the
// photo's previews and leaves a placeholder on the row, encoded from the preview
// it just made.
func TestRegenerate_storesAPlaceholder(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "beach", color.RGBA{R: 210, G: 60, B: 40, A: 255})
	svc := h.newService()

	if err := svc.Regenerate(ctx, photo.UID); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	got, err := h.photos.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Blurhash == "" {
		t.Fatal("the photo has no placeholder after a thumbnail job")
	}

	// A second run must leave it exactly as it was: that idempotence is what lets
	// the backfill be re-run over a partly drained library for free.
	if err := svc.Regenerate(ctx, photo.UID); err != nil {
		t.Fatalf("Regenerate again: %v", err)
	}
	again, err := h.photos.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID again: %v", err)
	}
	if again.Blurhash != got.Blurhash {
		t.Errorf("placeholder changed on a repeat run: %q then %q", got.Blurhash, again.Blurhash)
	}
}

// TestRegenerate_placeholderDescribesThePhoto verifies the stored value is
// derived from the picture rather than being a constant.
func TestRegenerate_placeholderDescribesThePhoto(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	svc := h.newService()

	red := h.storeJPEG(t, "red", color.RGBA{R: 220, G: 20, B: 20, A: 255})
	blue := h.storeJPEG(t, "blue", color.RGBA{R: 20, G: 20, B: 220, A: 255})
	for _, uid := range []string{red.UID, blue.UID} {
		if err := svc.Regenerate(ctx, uid); err != nil {
			t.Fatalf("Regenerate(%s): %v", uid, err)
		}
	}

	gotRed, err := h.photos.GetByUID(ctx, red.UID)
	if err != nil {
		t.Fatalf("GetByUID(red): %v", err)
	}
	gotBlue, err := h.photos.GetByUID(ctx, blue.UID)
	if err != nil {
		t.Fatalf("GetByUID(blue): %v", err)
	}
	if gotRed.Blurhash == "" || gotBlue.Blurhash == "" {
		t.Fatalf("placeholders = %q, %q; want both computed", gotRed.Blurhash, gotBlue.Blurhash)
	}
	if gotRed.Blurhash == gotBlue.Blurhash {
		t.Errorf("a red and a blue photo share the placeholder %q", gotRed.Blurhash)
	}
}

// TestBackfillBlurhash_drainsTheLibrary is the backfill's whole claim: it
// schedules exactly the photos that have no placeholder, the jobs it schedules
// produce one, and once they have run there is nothing left to schedule.
func TestBackfillBlurhash_drainsTheLibrary(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	svc := h.newService()

	pending := []photos.Photo{
		h.storeJPEG(t, "one", color.RGBA{R: 200, A: 255}),
		h.storeJPEG(t, "two", color.RGBA{G: 200, A: 255}),
	}
	done := h.storeJPEG(t, "three", color.RGBA{B: 200, A: 255})
	if err := h.photos.SaveBlurhash(ctx, done.UID, "LEHV6nWB2yk8pyo0adR*.7kCMdnj"); err != nil {
		t.Fatalf("SaveBlurhash: %v", err)
	}
	archived := h.storeJPEG(t, "four", color.RGBA{R: 100, G: 100, A: 255})
	if _, err := h.photos.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	count, err := svc.CountBackfillBlurhash(ctx, false)
	if err != nil {
		t.Fatalf("CountBackfillBlurhash: %v", err)
	}
	if count != len(pending) {
		t.Errorf("dry run counted %d, want %d", count, len(pending))
	}

	enqueued, err := svc.BackfillBlurhash(ctx, false)
	if err != nil {
		t.Fatalf("BackfillBlurhash: %v", err)
	}
	if enqueued != len(pending) {
		t.Errorf("enqueued = %d, want %d", enqueued, len(pending))
	}

	want := []string{pending[0].UID, pending[1].UID}
	sort.Strings(want)
	if got := h.queuedThumbnailUIDs(t); !slices.Equal(got, want) {
		t.Errorf("queued thumbnail jobs for %v, want %v", got, want)
	}

	// A repeat run leans on the queue's per-photo dedup rather than piling up a
	// second job for the same photo — what makes the backfill safe to re-run while
	// the app is serving.
	if _, err := svc.BackfillBlurhash(ctx, false); err != nil {
		t.Fatalf("BackfillBlurhash again: %v", err)
	}
	if got := h.queuedThumbnailUIDs(t); !slices.Equal(got, want) {
		t.Errorf("after a repeat run the queue holds %v, want %v", got, want)
	}

	// Run the work the backfill scheduled, then ask again: a drained library
	// schedules nothing.
	for _, uid := range want {
		if err := svc.Regenerate(ctx, uid); err != nil {
			t.Fatalf("Regenerate(%s): %v", uid, err)
		}
	}
	left, err := svc.CountBackfillBlurhash(ctx, false)
	if err != nil {
		t.Fatalf("CountBackfillBlurhash after draining: %v", err)
	}
	if left != 0 {
		t.Errorf("%d photo(s) still pending after the backfill drained", left)
	}
	if got := h.photos; got != nil {
		for _, uid := range want {
			photo, err := got.GetByUID(ctx, uid)
			if err != nil {
				t.Fatalf("GetByUID(%s): %v", uid, err)
			}
			if photo.Blurhash == "" {
				t.Errorf("photo %s has no placeholder after its job ran", uid)
			}
		}
	}
}

// TestForceRegenerate_replacesTheStoredPlaceholder verifies the force path
// re-encodes an existing placeholder rather than skipping it — how a photo whose
// rendering changed gets a stand-in that matches the new one.
func TestForceRegenerate_replacesTheStoredPlaceholder(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	photo := h.storeJPEG(t, "forced", color.RGBA{R: 30, G: 160, B: 90, A: 255})
	svc := h.newService()

	const stale = "LEHV6nWB2yk8pyo0adR*.7kCMdnj"
	if err := h.photos.SaveBlurhash(ctx, photo.UID, stale); err != nil {
		t.Fatalf("SaveBlurhash: %v", err)
	}
	if _, err := svc.ForceRegenerate(ctx, photo.UID); err != nil {
		t.Fatalf("ForceRegenerate: %v", err)
	}
	got, err := h.photos.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Blurhash == stale {
		t.Error("the force path left the stale placeholder in place")
	}
	if got.Blurhash == "" {
		t.Error("the force path cleared the placeholder instead of replacing it")
	}
}
