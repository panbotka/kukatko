package video

import (
	"testing"
	"time"
)

// TestIsVideoExt covers recognised and unrecognised extensions, the optional
// leading dot, and case-insensitivity.
func TestIsVideoExt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ext  string
		want bool
	}{
		{".mp4", true},
		{"mp4", true},
		{".MOV", true},
		{".MKV", true},
		{".webm", true},
		{".m4v", true},
		{".jpg", false},
		{".heic", false},
		{"", false},
		{".txt", false},
	}
	for _, tt := range tests {
		if got := IsVideoExt(tt.ext); got != tt.want {
			t.Errorf("IsVideoExt(%q) = %v, want %v", tt.ext, got, tt.want)
		}
	}
}

// TestIsVideoPath verifies extension detection over full paths.
func TestIsVideoPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"/uploads/VID_20230115.mp4", true},
		{"clip.MOV", true},
		{"2024/05/movie.mkv", true},
		{"photo.jpg", false},
		{"noext", false},
	}
	for _, tt := range tests {
		if got := IsVideoPath(tt.path); got != tt.want {
			t.Errorf("IsVideoPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestMetadata_HasContainerMetadata checks the difference between a reading and a
// failure: any one of a duration, a codec name and a full frame size proves a
// container was read, while a Metadata with none of them is what a probe that read
// nothing leaves behind — the state whose zero values must never be mistaken for
// statements about the file.
func TestMetadata_HasContainerMetadata(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		meta Metadata
		want bool
	}{
		"nothing at all":       {meta: Metadata{}, want: false},
		"only a creation time": {meta: Metadata{TakenAt: new(time.Time{})}, want: false},
		"only tags and a mime": {meta: Metadata{Mime: "text/plain", HasAudio: true}, want: false},
		"half a frame size":    {meta: Metadata{Width: 1920}, want: false},
		"a duration":           {meta: Metadata{DurationMs: new(1)}, want: true},
		"a codec name":         {meta: Metadata{VideoCodec: "h264"}, want: true},
		"a full frame size":    {meta: Metadata{Width: 1920, Height: 1080}, want: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.meta.HasContainerMetadata(); got != tc.want {
				t.Errorf("HasContainerMetadata() = %v, want %v", got, tc.want)
			}
		})
	}
}
