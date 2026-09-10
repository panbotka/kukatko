package ingest

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// TestExtractVideoMedia_probeFailureIsLogged checks the failure that used to be
// invisible: a video whose container cannot be read is still catalogued (an
// unreadable probe must not reject an upload), but the pipeline says so in the log,
// with the file it happened on — otherwise a broken probe or a missing ffprobe
// quietly produces a library of half-catalogued clips.
func TestExtractVideoMedia_probeFailureIsLogged(t *testing.T) {
	t.Parallel()
	if !video.FFmpegAvailable() {
		t.Skip("ffmpeg not installed; the video path refuses before it probes")
	}

	path := filepath.Join(t.TempDir(), "staged")
	if err := os.WriteFile(path, []byte("this is not a video at all"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var logged bytes.Buffer
	svc := New(Config{Logger: slog.New(slog.NewTextHandler(&logged, nil))})

	media, err := svc.extractVideoMedia(context.Background(), path, "holiday.mp4")
	if err != nil {
		t.Fatalf("extractVideoMedia() error = %v, want nil — the clip is still catalogued", err)
	}
	if media.kind != photos.MediaVideo {
		t.Errorf("kind = %q, want video", media.kind)
	}
	if media.video == nil || media.video.durationMs != nil || media.video.videoCodec != "" {
		t.Errorf("video fields = %+v, want empty ones", media.video)
	}

	out := logged.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("no warning logged for a failed probe:\n%s", out)
	}
	if !strings.Contains(out, "holiday.mp4") {
		t.Errorf("the log does not name the file it happened on:\n%s", out)
	}
}

// TestExtractVideoMedia_healthyProbeIsQuiet checks the log stays silent when there
// is nothing wrong: a warning per uploaded clip would train everyone to ignore the
// one that matters.
func TestExtractVideoMedia_healthyProbeIsQuiet(t *testing.T) {
	t.Parallel()
	if !video.FFmpegAvailable() || !video.FFprobeAvailable() {
		t.Skip("ffmpeg/ffprobe not installed; there is no probe to be quiet about")
	}
	clip, err := os.ReadFile(filepath.Join("testdata", "sample.mp4"))
	if err != nil {
		t.Skipf("no sample video to probe: %v", err)
	}
	path := filepath.Join(t.TempDir(), "staged")
	if err := os.WriteFile(path, clip, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var logged bytes.Buffer
	svc := New(Config{Logger: slog.New(slog.NewTextHandler(&logged, nil))})

	media, err := svc.extractVideoMedia(context.Background(), path, "clip.mp4")
	if err != nil {
		t.Fatalf("extractVideoMedia() error = %v", err)
	}
	if media.video == nil || media.video.videoCodec == "" {
		t.Fatalf("the sample video was not probed: %+v", media.video)
	}
	if logged.Len() != 0 {
		t.Errorf("a healthy probe logged something:\n%s", logged.String())
	}
}
