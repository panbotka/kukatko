package video

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestIsWebFriendlyCodec covers the browser-playable codecs, the codecs that
// need transcoding, case-insensitivity and the unknown (empty) codec.
func TestIsWebFriendlyCodec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		codec string
		want  bool
	}{
		{"h264", true},
		{"H264", true},
		{" avc1 ", true},
		{"vp9", true},
		{"av1", true},
		{"theora", true},
		{"hevc", false},
		{"h265", false},
		{"mpeg4", false},
		{"prores", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsWebFriendlyCodec(tt.codec); got != tt.want {
			t.Errorf("IsWebFriendlyCodec(%q) = %v, want %v", tt.codec, got, tt.want)
		}
	}
}

// TestTranscodeArgs verifies the ffmpeg command targets the source, produces a
// streamable fragmented H.264/AAC MP4 on stdout, and maps audio optionally.
func TestTranscodeArgs(t *testing.T) {
	t.Parallel()
	const src = "/originals/2024/05/clip.mov"
	args := TranscodeArgs(src)

	if !slices.Contains(args, src) {
		t.Errorf("args %v do not reference the source %q", args, src)
	}
	if got := args[len(args)-1]; got != "pipe:1" {
		t.Errorf("output target = %q, want pipe:1 (stream to stdout)", got)
	}

	want := map[string]string{
		"-c:v":      "libx264",
		"-c:a":      "aac",
		"-f":        "mp4",
		"-pix_fmt":  "yuv420p",
		"-movflags": "frag_keyframe+empty_moov+default_base_moof",
	}
	for flag, value := range want {
		idx := slices.Index(args, flag)
		if idx < 0 || idx+1 >= len(args) {
			t.Errorf("missing flag %q in %v", flag, args)
			continue
		}
		if args[idx+1] != value {
			t.Errorf("flag %q = %q, want %q", flag, args[idx+1], value)
		}
	}
	if idx := slices.Index(args, "-map"); idx < 0 {
		t.Errorf("expected an explicit stream -map in %v", args)
	}
	if !slices.Contains(args, "0:a?") {
		t.Errorf("expected optional audio map 0:a? in %v", args)
	}
}

// TestTranscode_missingFFmpeg verifies Transcode reports ErrFFmpegMissing when
// ffmpeg is not installed. It is skipped on hosts that have ffmpeg, where the
// missing-binary branch cannot be exercised without altering PATH.
func TestTranscode_missingFFmpeg(t *testing.T) {
	t.Parallel()
	if FFmpegAvailable() {
		t.Skip("ffmpeg is installed; cannot exercise the missing-binary path")
	}
	_, err := Transcode(t.Context(), "/tmp/whatever.mov")
	if !errors.Is(err, ErrFFmpegMissing) {
		t.Fatalf("Transcode error = %v, want ErrFFmpegMissing", err)
	}
}

// TestTranscodeArgs_noShellInjection is a guard that the source path is passed
// as a single argv element, never interpolated into a shell string.
func TestTranscodeArgs_noShellInjection(t *testing.T) {
	t.Parallel()
	const src = "a b; rm -rf /.mov"
	args := TranscodeArgs(src)
	if !slices.Contains(args, src) {
		t.Fatalf("source %q not passed verbatim as one argv element: %v", src, args)
	}
	for _, a := range args {
		if a != src && strings.Contains(a, "rm -rf") {
			t.Fatalf("source leaked into another argument: %q", a)
		}
	}
}
