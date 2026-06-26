package video

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPosterArgs verifies the ffmpeg poster command: fast input seek, a single
// frame, the quality flag and the destination last.
func TestPosterArgs(t *testing.T) {
	t.Parallel()
	got := posterArgs("/tmp/in.mp4", "/tmp/out.jpg", 1)
	want := []string{
		"-nostdin", "-y", "-ss", "1", "-i", "/tmp/in.mp4",
		"-frames:v", "1", "-q:v", "3", "/tmp/out.jpg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("posterArgs = %v, want %v", got, want)
	}
}

// makeSampleVideo renders a tiny 1-second test clip (with audio) via ffmpeg,
// skipping the test when ffmpeg is not installed. It returns the file path.
func makeSampleVideo(t *testing.T) string {
	t.Helper()
	if !FFmpegAvailable() {
		t.Skip("ffmpeg not installed; skipping video tooling test")
	}
	path := filepath.Join(t.TempDir(), "sample.mp4")
	// #nosec G204 -- all arguments are constant test inputs.
	cmd := exec.CommandContext(t.Context(), ffmpegBinary,
		"-nostdin", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=160x120:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendering sample video: %v (%s)", err, out)
	}
	return path
}

// TestExtractPoster_realClip verifies a poster JPEG is produced from a real
// clip. It runs only when ffmpeg is available.
func TestExtractPoster_realClip(t *testing.T) {
	t.Parallel()
	src := makeSampleVideo(t)

	posterPath, cleanup, err := ExtractPoster(t.Context(), src)
	if err != nil {
		t.Fatalf("ExtractPoster: %v", err)
	}
	defer cleanup()

	info, err := os.Stat(posterPath)
	if err != nil {
		t.Fatalf("stat poster: %v", err)
	}
	if info.Size() == 0 {
		t.Error("poster file is empty")
	}
	if filepath.Ext(posterPath) != ".jpg" {
		t.Errorf("poster ext = %q, want .jpg", filepath.Ext(posterPath))
	}
}

// TestProbe_realClip verifies ffprobe metadata of a real clip: media dimensions,
// a positive duration, a video codec and an audio stream. Runs only when both
// ffmpeg (to render) and ffprobe (to probe) are present.
func TestProbe_realClip(t *testing.T) {
	t.Parallel()
	src := makeSampleVideo(t)
	if !FFprobeAvailable() {
		t.Skip("ffprobe not installed; skipping probe test")
	}

	meta, err := Probe(t.Context(), src)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if meta.Width != 160 || meta.Height != 120 {
		t.Errorf("dimensions = %dx%d, want 160x120", meta.Width, meta.Height)
	}
	if meta.DurationMs == nil || *meta.DurationMs <= 0 {
		t.Errorf("DurationMs = %v, want > 0", meta.DurationMs)
	}
	if meta.VideoCodec == "" {
		t.Error("VideoCodec is empty")
	}
	if !meta.HasAudio {
		t.Error("HasAudio = false, want true")
	}
	if meta.FPS == nil || *meta.FPS <= 0 {
		t.Errorf("FPS = %v, want > 0", meta.FPS)
	}
}

// TestExtractPoster_missingFFmpeg verifies a clear, wrapped error when ffmpeg is
// absent. It runs only when ffmpeg is genuinely missing so it never interferes
// with a real install.
func TestExtractPoster_missingFFmpeg(t *testing.T) {
	t.Parallel()
	if FFmpegAvailable() {
		t.Skip("ffmpeg installed; cannot exercise the missing-tool path")
	}
	_, _, err := ExtractPoster(context.Background(), "/tmp/whatever.mp4")
	if err == nil {
		t.Fatal("ExtractPoster without ffmpeg = nil error, want ErrFFmpegMissing")
	}
}
