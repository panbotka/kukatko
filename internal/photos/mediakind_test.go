package photos

import "testing"

// TestMediaType_Kind checks the still/video boundary, including that a live
// photo counts as a still and that an unset media type — a row from before the
// column was read — does not silently become a video.
func TestMediaType_Kind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input MediaType
		want  MediaKind
	}{
		{name: "image is a still", input: MediaImage, want: MediaKindStill},
		{name: "video is a video", input: MediaVideo, want: MediaKindVideo},
		{name: "live photo is a still", input: MediaLive, want: MediaKindStill},
		{name: "unset defaults to a still", input: "", want: MediaKindStill},
		{name: "unrecognised defaults to a still", input: "audio", want: MediaKindStill},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.input.Kind(); got != tt.want {
				t.Errorf("MediaType(%q).Kind() = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
