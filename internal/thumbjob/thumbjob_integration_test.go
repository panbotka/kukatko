//go:build integration

package thumbjob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
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
	"github.com/panbotka/kukatko/internal/video"
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

// storeVideo renders a clip with ffmpeg from a lavfi filter description, stores
// it through the originals store and inserts a video row referencing it. It
// skips the test when ffmpeg is not installed, since there is no other way to
// make a video original.
func (h *harness) storeVideo(t *testing.T, name, filter string, width, height int) photos.Photo {
	t.Helper()
	if !video.FFmpegAvailable() {
		t.Skip("ffmpeg not installed; skipping video poster test")
	}
	path := filepath.Join(t.TempDir(), name+".mp4")
	// #nosec G204 -- filter and path are constant test inputs.
	cmd := exec.CommandContext(t.Context(), "ffmpeg",
		"-nostdin", "-y", "-f", "lavfi", "-i", filter,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendering %s: %v (%s)", name, err, out)
	}
	file, err := os.Open(path) //nolint:gosec // G304: our own temp file.
	if err != nil {
		t.Fatalf("open rendered clip: %v", err)
	}
	defer func() { _ = file.Close() }()

	sf, err := h.storage.Store(context.Background(), file, time.Time{}, name+".mp4")
	if err != nil {
		t.Fatalf("store original: %v", err)
	}
	created, err := h.photos.Create(context.Background(), photos.Photo{
		FileHash:        sf.Hash,
		FilePath:        sf.RelPath,
		FileName:        name + ".mp4",
		FileSize:        sf.Size,
		FileMime:        "video/mp4",
		FileWidth:       width,
		FileHeight:      height,
		MediaType:       photos.MediaVideo,
		FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	return created
}

// meanLuminance returns the average Rec. 601 luma of a rendition, 0 (black) to 1
// (white) — the cheap stand-in for looking at a thumbnail and calling it black.
func (h *harness) meanLuminance(t *testing.T, photo photos.Photo, size string) float64 {
	t.Helper()
	reader, err := h.thumbnailer.OpenCached(photo.FileHash, size)
	if err != nil {
		t.Fatalf("opening the %s rendition: %v", size, err)
	}
	defer func() { _ = reader.Close() }()

	img, err := jpeg.Decode(reader)
	if err != nil {
		t.Fatalf("decoding the %s rendition: %v", size, err)
	}
	bounds := img.Bounds()
	sum, n := 0.0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			sum += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 65535
			n++
		}
	}
	if n == 0 {
		t.Fatal("the rendition has no pixels")
	}
	return sum / float64(n)
}

// plantBlackRendition writes a solid black JPEG at the cache path the given
// rendition lives at, standing in for the tile the old fixed-one-second rule left
// behind for a clip that opens on darkness.
func (h *harness) plantBlackRendition(t *testing.T, photo photos.Photo, size string) {
	t.Helper()
	abs, err := h.thumbnailer.Path(photo.FileHash, size)
	if err != nil {
		t.Fatalf("thumbnail path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("creating the cache dir: %v", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 64, 48)), nil); err != nil {
		t.Fatalf("encoding the black tile: %v", err)
	}
	if err := os.WriteFile(abs, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("planting the black tile: %v", err)
	}
}

// TestRegenerate_videoPosterSkipsTheBlackOpening is the claim the poster rule
// exists for, end to end through the real thumbnailer: a clip whose first two
// seconds are black gets a thumbnail that shows something, not a black tile.
func TestRegenerate_videoPosterSkipsTheBlackOpening(t *testing.T) {
	h := newHarness(t)
	clip := h.storeVideo(t, "dark-opening",
		"color=c=black:s=320x240:r=15:d=2[a];testsrc=s=320x240:r=15:d=4[b];[a][b]concat=n=2:v=1",
		320, 240)

	if err := h.newService().Regenerate(t.Context(), clip.UID); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if mean := h.meanLuminance(t, clip, thumbjob.PlaceholderSize); mean < 0.10 {
		t.Errorf("the video's poster has mean luminance %.3f — that is a black tile", mean)
	}
}

// TestForceRegenerate_repicksTheVideoPoster is the repair path for the videos
// already in the library: a rebuild re-derives the poster from the original with
// the current rule, so a black tile becomes a picture without a re-upload. It
// starts from a thumbnail deliberately rendered from the clip's black opening.
func TestForceRegenerate_repicksTheVideoPoster(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	clip := h.storeVideo(t, "night",
		"color=c=black:s=320x240:r=15:d=2[a];testsrc=s=320x240:r=15:d=4[b];[a][b]concat=n=2:v=1",
		320, 240)

	// Plant the tile the old fixed-one-second rule produced: a solid black
	// rendition at the very cache path the thumbnailer would skip as "already
	// generated".
	h.plantBlackRendition(t, clip, thumbjob.PlaceholderSize)
	if mean := h.meanLuminance(t, clip, thumbjob.PlaceholderSize); mean > 0.01 {
		t.Fatalf("the planted rendition is not black (mean %.3f)", mean)
	}

	// The repair path skips it — which is why the backfill forces the rebuild.
	if err := h.newService().Regenerate(ctx, clip.UID); err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if mean := h.meanLuminance(t, clip, thumbjob.PlaceholderSize); mean > 0.01 {
		t.Errorf("the plain job rewrote a cached size (mean %.3f); it should skip it", mean)
	}

	// The rebuild does not.
	if _, err := h.newService().ForceRegenerate(ctx, clip.UID); err != nil {
		t.Fatalf("ForceRegenerate: %v", err)
	}
	if mean := h.meanLuminance(t, clip, thumbjob.PlaceholderSize); mean < 0.10 {
		t.Errorf("after the rebuild the poster is still black (mean %.3f)", mean)
	}
}
