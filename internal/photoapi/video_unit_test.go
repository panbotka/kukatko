package photoapi

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// TestPickMotionClip covers selecting a live photo's motion clip among its
// files: the video sidecar wins regardless of order, by MIME or by extension,
// and a set with no video yields ok=false.
func TestPickMotionClip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   []photos.PhotoFile
		wantOK  bool
		wantRel string
	}{
		{
			name: "video sidecar by mime",
			files: []photos.PhotoFile{
				{FilePath: "2024/05/img.heic", FileMime: "image/heic", IsPrimary: true, Role: photos.RoleOriginal},
				{FilePath: "2024/05/clip.bin", FileMime: "video/quicktime", Role: photos.RoleSidecar},
			},
			wantOK:  true,
			wantRel: "2024/05/clip.bin",
		},
		{
			name: "video sidecar by extension",
			files: []photos.PhotoFile{
				{FilePath: "2024/05/img.jpg", FileMime: "image/jpeg", IsPrimary: true, Role: photos.RoleOriginal},
				{FilePath: "2024/05/clip.mov", FileMime: "", Role: photos.RoleSidecar},
			},
			wantOK:  true,
			wantRel: "2024/05/clip.mov",
		},
		{
			name: "no video present",
			files: []photos.PhotoFile{
				{FilePath: "2024/05/img.jpg", FileMime: "image/jpeg", IsPrimary: true, Role: photos.RoleOriginal},
			},
			wantOK: false,
		},
		{
			name:   "empty file list",
			files:  nil,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := pickMotionClip(tt.files)
			if ok != tt.wantOK {
				t.Fatalf("pickMotionClip ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.FilePath != tt.wantRel {
				t.Errorf("pickMotionClip path = %q, want %q", got.FilePath, tt.wantRel)
			}
		})
	}
}

// TestShouldTranscode covers the gating of on-the-fly transcoding: it only fires
// when enabled, the codec is not web-friendly, and ffmpeg is present. ffmpeg's
// availability is taken from the host, so when ffmpeg is absent every case is
// false; the test asserts the codec/enabled gating relative to that.
func TestShouldTranscode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		transcode bool
		codec     string
		// wantWhenFFmpeg is the expected result assuming ffmpeg is available; with
		// ffmpeg absent the result is always false.
		wantWhenFFmpeg bool
	}{
		{name: "disabled never transcodes", transcode: false, codec: "hevc", wantWhenFFmpeg: false},
		{name: "web-friendly codec served as-is", transcode: true, codec: "h264", wantWhenFFmpeg: false},
		{name: "non-web-friendly codec transcodes", transcode: true, codec: "hevc", wantWhenFFmpeg: true},
		{name: "unknown codec served as-is", transcode: true, codec: "", wantWhenFFmpeg: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &API{videoTranscode: tt.transcode}
			got := a.shouldTranscode(photos.Photo{VideoCodec: tt.codec})
			want := tt.wantWhenFFmpeg && video.FFmpegAvailable()
			if got != want {
				t.Errorf("shouldTranscode(transcode=%v, codec=%q) = %v, want %v (ffmpeg=%v)",
					tt.transcode, tt.codec, got, want, video.FFmpegAvailable())
			}
		})
	}
}
