package dupmerge

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestCheckSameKind covers the guard that stops a merge from archiving across
// the video/still boundary. Detection never offers such a group, so this is
// about a request that arrives from anywhere else — and about it failing rather
// than quietly archiving a clip in favour of one frame of it.
func TestCheckSameKind(t *testing.T) {
	t.Parallel()
	still := photoRow{uid: "ph_still", mediaType: string(photos.MediaImage)}
	untyped := photoRow{uid: "ph_old"}
	clip := photoRow{uid: "ph_clip", mediaType: string(photos.MediaVideo)}
	live := photoRow{uid: "ph_live", mediaType: string(photos.MediaLive)}
	rows := map[string]photoRow{
		still.uid: still, untyped.uid: untyped, clip.uid: clip, live.uid: live,
	}

	tests := []struct {
		name    string
		keeper  photoRow
		archive []string
		wantErr error
	}{
		{name: "still keeps a still", keeper: still, archive: []string{untyped.uid}, wantErr: nil},
		{name: "video keeps a video", keeper: clip, archive: []string{clip.uid}, wantErr: nil},
		{name: "live counts as a still", keeper: still, archive: []string{live.uid}, wantErr: nil},
		{
			name: "still would archive a video", keeper: still,
			archive: []string{clip.uid}, wantErr: ErrCrossKindGroup,
		},
		{
			name: "video would archive a still", keeper: clip,
			archive: []string{still.uid}, wantErr: ErrCrossKindGroup,
		},
		{
			name: "one bad copy fails the whole merge", keeper: still,
			archive: []string{untyped.uid, clip.uid}, wantErr: ErrCrossKindGroup,
		},
		{name: "nothing to archive", keeper: still, archive: nil, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkSameKind(tt.keeper, tt.archive, rows)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("checkSameKind(%s, %v) error = %v, want %v", tt.keeper.uid, tt.archive, err, tt.wantErr)
			}
		})
	}
}

// TestPhotoRow_kind checks the row's media type is read through
// photos.MediaType, so the guard and internal/duplicates share one definition of
// what a video is.
func TestPhotoRow_kind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		mediaType string
		want      photos.MediaKind
	}{
		{name: "video", mediaType: "video", want: photos.MediaKindVideo},
		{name: "image", mediaType: "image", want: photos.MediaKindStill},
		{name: "live", mediaType: "live", want: photos.MediaKindStill},
		{name: "unset", mediaType: "", want: photos.MediaKindStill},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := (photoRow{mediaType: tt.mediaType}).kind(); got != tt.want {
				t.Errorf("photoRow{mediaType: %q}.kind() = %q, want %q", tt.mediaType, got, tt.want)
			}
		})
	}
}
