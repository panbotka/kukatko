package video

import "testing"

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
