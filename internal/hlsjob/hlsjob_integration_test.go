//go:build integration

package hlsjob_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel. The encode itself is real:
// a short clip is synthesized with ffmpeg at test time rather than checked into
// the repository, and the whole test is skipped on a host with no ffmpeg.

// clipSeconds is the length of the synthesized fixture. Two seconds is long
// enough to be cut into more than one segment at segmentSeconds and short enough
// that the encode is over in about a second.
const clipSeconds = 2

// segmentSeconds is the segment length the tests encode with. One second forces
// several segments out of a two-second clip, which is what makes the segment
// count and the summed duration worth asserting.
const segmentSeconds = 1

// recordingObserver is an hlsjob.Observer that keeps what a real encode
// reported, so the instrumentation can be asserted against numbers ffmpeg
// actually produced rather than against a fake's. The encode is sequential
// within one job, so it needs no locking.
type recordingObserver struct {
	// renditions maps a rendition name to the bytes reported for it.
	renditions map[string]int64
	// outcomes maps a rendition name to the outcome it was reported under.
	outcomes map[string]string
	// durations maps a rendition name to how long it was reported to take.
	durations map[string]time.Duration
	// footage lists the clip lengths reported, one per encoded video.
	footage []time.Duration
}

// newRecordingObserver returns an observer that has seen nothing.
func newRecordingObserver() *recordingObserver {
	return &recordingObserver{
		renditions: map[string]int64{},
		outcomes:   map[string]string{},
		durations:  map[string]time.Duration{},
	}
}

// ObserveRenditionEncode records one rendition's report.
func (o *recordingObserver) ObserveRenditionEncode(
	rendition, outcome string, d time.Duration, written int64,
) {
	o.renditions[rendition] = written
	o.outcomes[rendition] = outcome
	o.durations[rendition] = d
}

// ObserveEncodedSource records one video's length.
func (o *recordingObserver) ObserveEncodedSource(d time.Duration) {
	o.footage = append(o.footage, d)
}

// harness bundles everything one end-to-end encode needs.
type harness struct {
	// service is the handler under test.
	service *hlsjob.Service
	// metrics is what the service reported while encoding.
	metrics *recordingObserver
	// renditions reads back the rows the service wrote.
	renditions *hlsjob.Store
	// objects is the store the segments are published to.
	objects *storage.FS
	// photo is the catalogued video the fixture was stored as.
	photo photos.Photo
	// prefix is the key prefix this video's 1080p objects live under.
	prefix string
}

// newHarness prepares a clean database, a filesystem object store holding a
// freshly synthesized clip, and a Service wired over both. It skips the test
// when there is no database or no ffmpeg.
func newHarness(t *testing.T) harness {
	t.Helper()
	if !video.FFmpegAvailable() {
		t.Skip("ffmpeg not installed; skipping the HLS encode test")
	}
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	objects, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	stored := storeClip(t, objects)
	photoStore := photos.NewStore(db.Pool())
	duration := clipSeconds * 1000
	photo, err := photoStore.Create(t.Context(), photos.Photo{
		FileHash:        stored.Hash,
		FilePath:        stored.RelPath,
		FileName:        "clip.mp4",
		FileSize:        stored.Size,
		FileMime:        "video/mp4",
		FileOrientation: 1,
		MediaType:       photos.MediaVideo,
		DurationMs:      &duration,
	})
	if err != nil {
		t.Fatalf("creating the catalogued video: %v", err)
	}
	renditions := hlsjob.NewStore(db.Pool())
	observer := newRecordingObserver()
	return harness{
		service: hlsjob.New(hlsjob.Config{
			Photos:         photoStore,
			Objects:        objects,
			Renditions:     renditions,
			Plan:           hls.All(),
			SegmentSeconds: segmentSeconds,
			Metrics:        observer,
		}),
		metrics:    observer,
		renditions: renditions,
		objects:    objects,
		photo:      photo,
		prefix:     "hls/" + stored.Hash + "/" + hls.Rendition1080p + "/",
	}
}

// storeClip synthesizes a short test pattern with ffmpeg and publishes it as an
// original, returning it as the store holds it.
func storeClip(t *testing.T, objects *storage.FS) storage.StoredFile {
	t.Helper()
	abs := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.CommandContext(t.Context(), "ffmpeg",
		"-nostdin", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10:duration="+strconv.Itoa(clipSeconds),
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(clipSeconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		abs,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot synthesize a test clip here: %v (%s)", err, out)
	}
	file, err := os.Open(abs)
	if err != nil {
		t.Fatalf("opening the synthesized clip: %v", err)
	}
	defer func() { _ = file.Close() }()

	stored, err := objects.Store(t.Context(), file, time.Time{}, "clip.mp4")
	if err != nil {
		t.Fatalf("storing the synthesized clip: %v", err)
	}
	return stored
}

// keysUnderPrefix returns every object key the store holds under prefix, sorted.
func (h harness) keysUnderPrefix(t *testing.T) []string {
	t.Helper()
	var keys []string
	if err := h.objects.KeysWithPrefix(t.Context(), h.prefix, func(key string) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		t.Fatalf("listing %s: %v", h.prefix, err)
	}
	slices.Sort(keys)
	return keys
}

// TestTranscode_endToEnd encodes a real clip and verifies both halves of what the
// job promises: the objects a player will fetch are in the store under the
// rendition's prefix, and the row describes exactly them — as many segments as
// were uploaded, the picture size ffmpeg really produced, and the length summed
// from the playlist rather than assumed from the source.
func TestTranscode_endToEnd(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	if err := h.service.Transcode(ctx, h.photo.UID); err != nil {
		t.Fatalf("Transcode: %v", err)
	}

	row, err := h.renditions.Get(ctx, h.photo.UID, hls.Rendition1080p)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	keys := h.keysUnderPrefix(t)
	if !slices.Contains(keys, h.prefix+hls.InitName) {
		t.Errorf("no initialisation segment among %v", keys)
	}
	// Every object in the prefix is the init segment plus one media segment per
	// counted segment; anything else means the row and the store disagree.
	if want := row.SegmentCount + 1; len(keys) != want {
		t.Errorf("store holds %d objects (%v), want %d", len(keys), keys, want)
	}
	if row.SegmentCount < 2 {
		t.Errorf("segment_count = %d, want a %ds clip cut into at least two %ds segments",
			row.SegmentCount, clipSeconds, segmentSeconds)
	}
	if row.Width != 320 || row.Height != 240 {
		t.Errorf("picture = %dx%d, want 320x240 (the source is smaller than the box)", row.Width, row.Height)
	}
	wantMs := clipSeconds * 1000
	if row.DurationMs < wantMs-500 || row.DurationMs > wantMs+500 {
		t.Errorf("duration_ms = %d, want about %d", row.DurationMs, wantMs)
	}
	rendition, _ := hls.ByName(hls.Rendition1080p)
	if row.Codecs != rendition.Codecs || row.Bandwidth != rendition.Bandwidth() {
		t.Errorf("row advertises %s / %d bps, want %s / %d",
			row.Codecs, row.Bandwidth, rendition.Codecs, rendition.Bandwidth())
	}
	if row.Playlist == "" || row.EncodedAt.IsZero() {
		t.Errorf("row carries no playlist or no timestamp: %+v", row)
	}
	// The stored playlist is the one ffmpeg wrote — bare object names, which is
	// what the serving layer rewrites into URLs.
	for _, name := range []string{hls.InitName, "00000.m4s"} {
		if !strings.Contains(row.Playlist, name) {
			t.Errorf("playlist does not name %s:\n%s", name, row.Playlist)
		}
	}
}

// TestTranscode_rerunReplaces verifies a re-encode replaces the rendition rather
// than duplicating it: one row still, moved forward in time, the same object set,
// and an object left over from an earlier encode swept away.
func TestTranscode_rerunReplaces(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	if err := h.service.Transcode(ctx, h.photo.UID); err != nil {
		t.Fatalf("first Transcode: %v", err)
	}
	first, err := h.renditions.Get(ctx, h.photo.UID, hls.Rendition1080p)
	if err != nil {
		t.Fatalf("Get after the first run: %v", err)
	}
	keys := h.keysUnderPrefix(t)

	// A segment a longer, earlier encode would have left behind. The re-run must
	// remove it: it belongs to no playlist any more, and a player told to fetch
	// segments by name must never find a stale one.
	stale := h.prefix + "00099.m4s"
	body := []byte("stale")
	sum := sha256.Sum256(body)
	if err := h.objects.Put(ctx, bytes.NewReader(body), storage.StoredFile{
		Hash: hex.EncodeToString(sum[:]), RelPath: stale, Size: int64(len(body)), MIME: "video/iso.segment",
	}); err != nil {
		t.Fatalf("planting a stale segment: %v", err)
	}

	if err := h.service.Transcode(ctx, h.photo.UID); err != nil {
		t.Fatalf("second Transcode: %v", err)
	}

	rows, err := h.renditions.ListForPhoto(ctx, h.photo.UID)
	if err != nil {
		t.Fatalf("ListForPhoto: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("photo has %d renditions after a re-run, want 1", len(rows))
	}
	if !rows[0].EncodedAt.After(first.EncodedAt) {
		t.Errorf("encoded_at = %v, want later than %v", rows[0].EncodedAt, first.EncodedAt)
	}
	if got := h.keysUnderPrefix(t); !slices.Equal(got, keys) {
		t.Errorf("objects after the re-run = %v, want the same set as before: %v", got, keys)
	}
}

// TestTranscode_reportsWhatEachRenditionCost verifies the instrumentation over a
// real encode: every rendition of the plan is reported once, as a success, with
// a duration and with the bytes it published — the same bytes the store now
// holds — and the clip's length is counted once for the whole job rather than
// once per rendition, which is what makes the encode's cost per minute of
// footage a division rather than a guess at how many qualities were produced.
func TestTranscode_reportsWhatEachRenditionCost(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	if err := h.service.Transcode(ctx, h.photo.UID); err != nil {
		t.Fatalf("Transcode: %v", err)
	}

	if len(h.metrics.renditions) != len(hls.All()) {
		t.Fatalf("reported %d renditions, want the plan's %d: %v",
			len(h.metrics.renditions), len(hls.All()), h.metrics.renditions)
	}
	for _, rendition := range hls.All() {
		if got := h.metrics.outcomes[rendition.Name]; got != hlsjob.OutcomeSuccess {
			t.Errorf("%s reported outcome %q, want %q", rendition.Name, got, hlsjob.OutcomeSuccess)
		}
		if h.metrics.renditions[rendition.Name] <= 0 {
			t.Errorf("%s reported %d bytes, want the segments it published",
				rendition.Name, h.metrics.renditions[rendition.Name])
		}
		if h.metrics.durations[rendition.Name] <= 0 {
			t.Errorf("%s reported a duration of %v, want the time it took",
				rendition.Name, h.metrics.durations[rendition.Name])
		}
	}
	// The bytes reported for 1080p are exactly what its prefix now holds: the
	// counter must describe the objects a player will fetch, not an estimate.
	var stored int64
	for _, key := range h.keysUnderPrefix(t) {
		info, err := h.objects.Stat(ctx, key)
		if err != nil {
			t.Fatalf("stat %s: %v", key, err)
		}
		stored += info.Size()
	}
	if got := h.metrics.renditions[hls.Rendition1080p]; got != stored {
		t.Errorf("1080p reported %d bytes, want the %d its prefix holds", got, stored)
	}
	if want := []time.Duration{clipSeconds * time.Second}; !slices.Equal(h.metrics.footage, want) {
		t.Errorf("footage reported = %v, want %v — once per video, not per rendition",
			h.metrics.footage, want)
	}
}

// TestTranscode_missingPhotoDoesNotWrite verifies a job naming a photo that no
// longer exists fails without touching the store.
func TestTranscode_missingPhotoDoesNotWrite(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	if err := h.service.Transcode(ctx, "ph-gone"); err == nil {
		t.Fatal("Transcode succeeded for an unknown photo")
	}
	if keys := h.keysUnderPrefix(t); len(keys) != 0 {
		t.Errorf("store holds %v after a failed run, want nothing", keys)
	}
}
