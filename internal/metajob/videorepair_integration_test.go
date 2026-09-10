//go:build integration

package metajob_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/storyboardjob"
	"github.com/panbotka/kukatko/internal/video"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL, over real clips on a real FS store. They cover the
// repair of a video whose technical metadata is missing — the clip whose probe
// failed while it was being ingested: the file-derived fields come back, the
// curated ones (an edited capture date, a hand-set location) survive, a complete
// clip is left alone, and a repaired clip is eligible for its scrub preview again.

// clipSeconds is the length of the synthesized test clip.
const clipSeconds = 3

// clipTakenAt is the creation time written into the test clip's container.
var clipTakenAt = time.Date(2024, 5, 17, 9, 30, 0, 0, time.UTC)

// clipLat and clipLng are the coordinates written into the test clip's container
// as an ISO 6709 location tag.
const (
	clipLat = 49.3456
	clipLng = 16.7654
)

// synthesizedClip renders the test clip once per test binary and returns its
// bytes: ffmpeg costs about a second, and every case here wants the same file.
var synthesizedClip = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "kukatko-clip-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	abs := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command("ffmpeg", //nolint:gosec // G204: every argument is a constant.
		"-nostdin", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10:duration="+strconv.Itoa(clipSeconds),
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(clipSeconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		"-metadata", "creation_time="+clipTakenAt.Format(time.RFC3339),
		"-metadata", "location="+"+49.3456+016.7654/",
		abs,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, &clipError{err: err, output: string(out)}
	}
	return os.ReadFile(abs)
})

// clipError reports why ffmpeg could not synthesize the test clip, keeping its
// output so a skip says what actually went wrong.
type clipError struct {
	err    error
	output string
}

// Error renders the ffmpeg failure together with its output.
func (e *clipError) Error() string { return e.err.Error() + ": " + e.output }

// requireProbe skips the test when the tooling a video repair needs is not
// installed: ffmpeg to synthesize the clip, ffprobe to read it back.
func requireProbe(t *testing.T) {
	t.Helper()
	if !video.FFmpegAvailable() || !video.FFprobeAvailable() {
		t.Skip("ffmpeg/ffprobe not installed; there is no video to probe")
	}
}

// seedBrokenVideo publishes the synthesized clip into the store's layout and
// catalogues it the way a failed probe leaves a video: the file is intact and the
// row is marked as read, but everything only a probe could have written is empty.
// Any column can be pre-set via edit, standing in for a value somebody decided on.
//
// It returns the row and the absolute path of the original, so a test can compare
// the repair against a direct probe of the very same file.
func (e *testEnv) seedBrokenVideo(
	t *testing.T, hash string, edit func(*photos.Photo),
) (photos.Photo, string) {
	t.Helper()
	requireProbe(t)
	clip, err := synthesizedClip()
	if err != nil {
		t.Skipf("cannot synthesize a test clip here: %v", err)
	}

	relPath := "2024/05/" + hash + ".mp4"
	abs := filepath.Join(e.root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, clip, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	read := time.Now().UTC()
	photo := photos.Photo{
		FileHash:  hash,
		FilePath:  relPath,
		FileName:  hash + ".mp4",
		FileMime:  "video/mp4",
		MediaType: photos.MediaVideo,
		// The ingest pipeline stamps this even when the probe fails, which is exactly
		// why nothing ever came back for these clips.
		MetadataExtractedAt: &read,
		TakenAtSource:       photos.TakenAtSourceUnknown,
	}
	if edit != nil {
		edit(&photo)
	}
	created, err := e.store.Create(t.Context(), photo)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created, abs
}

// TestReextract_repairsVideoTechnicalMetadata is the verification the repair is
// for: a clip catalogued with empty technical metadata gets every field back, and
// each value matches a direct probe of the same file.
func TestReextract_repairsVideoTechnicalMetadata(t *testing.T) {
	env := newEnv(t, nil)
	ctx := t.Context()
	before, abs := env.seedBrokenVideo(t, "aa10", nil)

	// Before: the clip knows nothing about itself.
	if before.DurationMs != nil || before.VideoCodec != "" || before.FileWidth != 0 {
		t.Fatalf("seeded video is not blank: %+v", before)
	}

	if err := env.svc.Reextract(ctx, before.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	after, err := env.store.GetByUID(ctx, before.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	// The reference reading: the same file, probed directly.
	want, err := video.Probe(ctx, abs)
	if err != nil {
		t.Fatalf("video.Probe: %v", err)
	}
	if want.DurationMs == nil {
		t.Fatal("the reference probe read no duration; the fixture is not a video")
	}
	if after.DurationMs == nil || *after.DurationMs != *want.DurationMs {
		t.Errorf("duration_ms = %v, want %d", after.DurationMs, *want.DurationMs)
	}
	if after.FileWidth != want.Width || after.FileHeight != want.Height {
		t.Errorf("dimensions = %d×%d, want %d×%d",
			after.FileWidth, after.FileHeight, want.Width, want.Height)
	}
	if after.FPS == nil || want.FPS == nil || *after.FPS != *want.FPS {
		t.Errorf("fps = %v, want %v", after.FPS, want.FPS)
	}
	if after.VideoCodec != want.VideoCodec || after.AudioCodec != want.AudioCodec {
		t.Errorf("codecs = %q/%q, want %q/%q",
			after.VideoCodec, after.AudioCodec, want.VideoCodec, want.AudioCodec)
	}
	if after.HasAudio != want.HasAudio {
		t.Errorf("has_audio = %v, want %v", after.HasAudio, want.HasAudio)
	}
	// The before/after of the repair, logged so a `-v` run is the evidence itself.
	t.Logf("before: %s", technicalSummary(before))
	t.Logf("after:  %s", technicalSummary(after))
	t.Logf("probe:  duration=%s dimensions=%dx%d fps=%s video=%s audio=%s/%v",
		msString(want.DurationMs), want.Width, want.Height, fpsString(want.FPS),
		want.VideoCodec, want.AudioCodec, want.HasAudio)

	// The container's own creation time and coordinates fill the gaps they are.
	if after.TakenAt == nil || !after.TakenAt.Equal(clipTakenAt) {
		t.Errorf("taken_at = %v, want %v", after.TakenAt, clipTakenAt)
	}
	if after.TakenAtSource != "exif" {
		t.Errorf("taken_at_source = %q, want exif", after.TakenAtSource)
	}
	if after.Lat == nil || after.Lng == nil ||
		!closeEnough(*after.Lat, clipLat) || !closeEnough(*after.Lng, clipLng) {
		t.Errorf("location = %v/%v, want %v/%v", after.Lat, after.Lng, clipLat, clipLng)
	}
	if after.LocationSource != photos.LocationSourceExif {
		t.Errorf("location_source = %q, want exif", after.LocationSource)
	}
}

// closeEnough reports whether two coordinates agree to within a metre or so, which
// is all the precision an ISO 6709 tag carries.
func closeEnough(got, want float64) bool {
	diff := got - want
	return diff < 1e-4 && diff > -1e-4
}

// TestReextract_videoRepairKeepsCuratedValues checks the delicate half over the
// database: a capture date somebody corrected and a location somebody set by hand
// both survive a re-probe that has different values to offer, while the technical
// fields are still repaired.
func TestReextract_videoRepairKeepsCuratedValues(t *testing.T) {
	env := newEnv(t, nil)
	ctx := t.Context()

	corrected := time.Date(1998, 7, 4, 18, 0, 0, 0, time.UTC)
	photo, _ := env.seedBrokenVideo(t, "bb20", func(p *photos.Photo) {
		p.TakenAt, p.TakenAtSource = &corrected, photos.TakenAtSourceManual
		p.Lat, p.Lng = new(50.0755), new(14.4378)
		p.LocationSource = photos.LocationSourceManual
	})

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	after, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	if after.TakenAt == nil || !after.TakenAt.Equal(corrected) ||
		after.TakenAtSource != photos.TakenAtSourceManual {
		t.Errorf("capture date overruled: %v / %q", after.TakenAt, after.TakenAtSource)
	}
	if after.Lat == nil || *after.Lat != 50.0755 || after.Lng == nil || *after.Lng != 14.4378 ||
		after.LocationSource != photos.LocationSourceManual {
		t.Errorf("location overruled: %v/%v (%q)", after.Lat, after.Lng, after.LocationSource)
	}
	if after.DurationMs == nil || after.VideoCodec == "" {
		t.Errorf("technical metadata not repaired: %+v", after)
	}
}

// TestReextract_videoRepairKeepsADisownedDate checks the other side of the same
// rule: a date somebody declared unknown stays unknown, and the value that was put
// away is not re-stated from the container.
func TestReextract_videoRepairKeepsADisownedDate(t *testing.T) {
	env := newEnv(t, nil)
	ctx := t.Context()

	disowned := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	photo, _ := env.seedBrokenVideo(t, "bb21", func(p *photos.Photo) {
		p.TakenAt, p.TakenAtSource = nil, photos.TakenAtSourceUnknown
		p.TakenAtBeforeUnknown = &disowned
	})

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	after, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if after.TakenAt != nil {
		t.Errorf("taken_at = %v, want none — the date was declared unknown", after.TakenAt)
	}
	if after.TakenAtBeforeUnknown == nil || !after.TakenAtBeforeUnknown.Equal(disowned) {
		t.Errorf("taken_at_before_unknown = %v, want %v", after.TakenAtBeforeUnknown, disowned)
	}
	if after.DurationMs == nil {
		t.Error("technical metadata not repaired")
	}
}

// TestReextract_completeVideoIsUntouched checks a video whose metadata already
// matches its file is left entirely alone by a re-probe — down to updated_at, so a
// repeated backfill cannot reorder every "recently changed" listing in the library.
func TestReextract_completeVideoIsUntouched(t *testing.T) {
	env := newEnv(t, nil)
	ctx := t.Context()
	photo, _ := env.seedBrokenVideo(t, "cc30", nil)

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("first Reextract: %v", err)
	}
	repaired, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("second Reextract: %v", err)
	}
	again, err := env.store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("second GetByUID: %v", err)
	}
	if !again.UpdatedAt.Equal(repaired.UpdatedAt) {
		t.Errorf("updated_at moved on a no-op re-probe: %v → %v", repaired.UpdatedAt, again.UpdatedAt)
	}
	if *again.DurationMs != *repaired.DurationMs || again.VideoCodec != repaired.VideoCodec {
		t.Errorf("a second re-probe rewrote the metadata: %+v", again)
	}
}

// fakeSprites is a storyboardjob.Generator that has never rendered anything, so
// Status has to decide from the catalogue alone whether a sprite is possible.
type fakeSprites struct{}

// Exists reports that no sprite is cached.
func (fakeSprites) Exists(string) (bool, error) { return false, nil }

// Open reports that no sprite is cached.
func (fakeSprites) Open(string) (io.ReadCloser, error) { return nil, storyboard.ErrNotGenerated }

// Generate does nothing; these tests never render a sprite.
func (fakeSprites) Generate(context.Context, string, string, storyboard.Spec) error { return nil }

// spriteEnqueuer records the photos whose sprite generation was scheduled.
type spriteEnqueuer struct{ uids []string }

// EnqueueStoryboard records uid and reports success.
func (s *spriteEnqueuer) EnqueueStoryboard(_ context.Context, uid string) error {
	s.uids = append(s.uids, uid)
	return nil
}

// TestReextract_repairedVideoRegainsItsScrubPreview checks the consequence the
// repair is really about: a clip with no duration can never have a scrub preview,
// and after the repair the very next playback schedules one instead of the player
// being told "unavailable" forever.
func TestReextract_repairedVideoRegainsItsScrubPreview(t *testing.T) {
	env := newEnv(t, nil)
	ctx := t.Context()
	photo, _ := env.seedBrokenVideo(t, "dd40", nil)

	sprites := &spriteEnqueuer{}
	preview := storyboardjob.New(storyboardjob.Config{
		Photos:          env.store,
		Generator:       fakeSprites{},
		Enqueuer:        sprites,
		FFmpegAvailable: func() bool { return true },
	})

	status, err := preview.Status(ctx, photo.UID)
	if err != nil {
		t.Fatalf("Status before the repair: %v", err)
	}
	if status.State != storyboardjob.StateUnavailable {
		t.Fatalf("state before the repair = %q, want unavailable", status.State)
	}
	if len(sprites.uids) != 0 {
		t.Fatalf("scheduled %v before the repair, want nothing", sprites.uids)
	}

	if err := env.svc.Reextract(ctx, photo.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}

	status, err = preview.Status(ctx, photo.UID)
	if err != nil {
		t.Fatalf("Status after the repair: %v", err)
	}
	if status.State != storyboardjob.StatePending {
		t.Errorf("state after the repair = %q, want pending", status.State)
	}
	if len(sprites.uids) != 1 || sprites.uids[0] != photo.UID {
		t.Errorf("scheduled %v, want just %s", sprites.uids, photo.UID)
	}
}

// TestBackfillVideoMetadata_findsAndDrainsBrokenClips checks the operator's route:
// the video-scoped backfill schedules exactly the clips whose container was never
// read out — including the ones the ordinary backfill passes by, because they are
// marked as read — and enqueues nothing once they have been repaired.
func TestBackfillVideoMetadata_findsAndDrainsBrokenClips(t *testing.T) {
	enq := &recordingEnqueuer{}
	env := newEnv(t, enq)
	ctx := t.Context()

	broken, _ := env.seedBrokenVideo(t, "ee50", nil)
	env.seedLegacyPhoto(t, "ee51", nil) // a still: never a candidate here

	// The ordinary backfill cannot see it: the row is stamped as read.
	if enqueued, err := env.svc.BackfillMetadata(ctx, false); err != nil {
		t.Fatalf("BackfillMetadata: %v", err)
	} else if enqueued != 1 || enq.uids[0] == broken.UID {
		t.Fatalf("plain backfill scheduled %v, want only the unread still", enq.uids)
	}

	enq.uids = nil
	enqueued, err := env.svc.BackfillVideoMetadata(ctx)
	if err != nil {
		t.Fatalf("BackfillVideoMetadata: %v", err)
	}
	if enqueued != 1 || len(enq.uids) != 1 || enq.uids[0] != broken.UID {
		t.Fatalf("video backfill scheduled %v (%d), want just %s", enq.uids, enqueued, broken.UID)
	}

	if err := env.svc.Reextract(ctx, broken.UID); err != nil {
		t.Fatalf("Reextract: %v", err)
	}
	enq.uids = nil
	enqueued, err = env.svc.BackfillVideoMetadata(ctx)
	if err != nil {
		t.Fatalf("second BackfillVideoMetadata: %v", err)
	}
	if enqueued != 0 {
		t.Errorf("drained video backfill scheduled %v (%d), want none", enq.uids, enqueued)
	}
}

// technicalSummary renders a photo's video-technical columns on one line, for the
// before/after evidence of a repair.
func technicalSummary(p photos.Photo) string {
	return "duration=" + msString(p.DurationMs) +
		" dimensions=" + strconv.Itoa(p.FileWidth) + "x" + strconv.Itoa(p.FileHeight) +
		" fps=" + fpsString(p.FPS) +
		" video=" + quoted(p.VideoCodec) + " audio=" + quoted(p.AudioCodec) +
		"/" + strconv.FormatBool(p.HasAudio) +
		" taken_at=" + timeString(p.TakenAt) + "(" + p.TakenAtSource + ")" +
		" location=" + coordString(p.Lat) + "," + coordString(p.Lng) +
		"(" + quoted(p.LocationSource) + ")"
}

// msString renders an optional millisecond count, "none" when absent.
func msString(ms *int) string {
	if ms == nil {
		return "none"
	}
	return strconv.Itoa(*ms) + "ms"
}

// fpsString renders an optional frame rate, "none" when absent.
func fpsString(fps *float64) string {
	if fps == nil {
		return "none"
	}
	return strconv.FormatFloat(*fps, 'f', -1, 64)
}

// coordString renders an optional coordinate, "none" when absent.
func coordString(v *float64) string {
	if v == nil {
		return "none"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// timeString renders an optional timestamp in RFC 3339, "none" when absent.
func timeString(t *time.Time) string {
	if t == nil {
		return "none"
	}
	return t.UTC().Format(time.RFC3339)
}

// quoted renders an empty string as a visible pair of quotes, so the before/after
// evidence distinguishes "empty" from "missing from the line".
func quoted(s string) string {
	if s == "" {
		return `""`
	}
	return s
}
