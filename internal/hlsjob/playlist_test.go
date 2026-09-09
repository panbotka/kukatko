package hlsjob

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// ffmpegPlaylist is a media playlist in exactly the shape ffmpeg writes one for
// a VOD fMP4 encode: bare object names, an EXT-X-MAP for the initialisation
// segment and one EXTINF per media segment.
const ffmpegPlaylist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.000000,
00000.m4s
#EXTINF:1.480000,
00001.m4s
#EXT-X-ENDLIST
`

// TestParsePlaylist reads a real ffmpeg playlist and verifies everything the
// publish step and the catalogue row take from it: the initialisation segment,
// the media segments in playback order, and the total duration summed from the
// EXTINF values rather than guessed from the segment length.
func TestParsePlaylist(t *testing.T) {
	t.Parallel()

	got, err := parsePlaylist(ffmpegPlaylist)
	if err != nil {
		t.Fatalf("parsePlaylist: %v", err)
	}
	if got.initName != "init.mp4" {
		t.Errorf("initName = %q, want init.mp4", got.initName)
	}
	if want := []string{"00000.m4s", "00001.m4s"}; !slices.Equal(got.segments, want) {
		t.Errorf("segments = %v, want %v", got.segments, want)
	}
	if got.durationMs != 7480 {
		t.Errorf("durationMs = %d, want 7480", got.durationMs)
	}
}

// TestParsePlaylist_crlfAndPaths verifies the two shapes a playlist may arrive
// in without meaning anything different: CRLF line endings, and URIs that carry
// a directory in front of the object's name.
func TestParsePlaylist_crlfAndPaths(t *testing.T) {
	t.Parallel()

	playlist := strings.ReplaceAll(ffmpegPlaylist, "\n", "\r\n")
	playlist = strings.ReplaceAll(playlist, `URI="init.mp4"`, `URI="/tmp/out/init.mp4"`)
	playlist = strings.ReplaceAll(playlist, "00000.m4s", "/tmp/out/00000.m4s")

	got, err := parsePlaylist(playlist)
	if err != nil {
		t.Fatalf("parsePlaylist: %v", err)
	}
	if got.initName != "init.mp4" {
		t.Errorf("initName = %q, want init.mp4", got.initName)
	}
	if want := []string{"00000.m4s", "00001.m4s"}; !slices.Equal(got.segments, want) {
		t.Errorf("segments = %v, want %v", got.segments, want)
	}
}

// TestParsePlaylist_rejects verifies every shape that must fail the run rather
// than be stored: a playlist naming no initialisation segment, one naming no
// segments, a segment with no or an unreadable duration, and a URI naming
// something the object layout cannot hold.
func TestParsePlaylist_rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		playlist string
	}{
		{
			name:     "no EXT-X-MAP",
			playlist: "#EXTM3U\n#EXTINF:6.0,\n00000.m4s\n#EXT-X-ENDLIST\n",
		},
		{
			name:     "EXT-X-MAP without a URI",
			playlist: "#EXTM3U\n#EXT-X-MAP:BYTERANGE=\"1@0\"\n#EXTINF:6.0,\n00000.m4s\n",
		},
		{
			name:     "EXT-X-MAP with an unterminated URI",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\n#EXTINF:6.0,\n00000.m4s\n",
		},
		{
			name:     "no segments",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-ENDLIST\n",
		},
		{
			name:     "segment without an EXTINF",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n00000.m4s\n",
		},
		{
			name:     "unreadable EXTINF",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:soon,\n00000.m4s\n",
		},
		{
			name:     "zero EXTINF",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:0,\n00000.m4s\n",
		},
		{
			name:     "a segment name the layout cannot hold",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6.0,\n0.m4s\n",
		},
		{
			name:     "an initialisation segment the layout cannot hold",
			playlist: "#EXTM3U\n#EXT-X-MAP:URI=\"../init.mp4x\"\n#EXTINF:6.0,\n00000.m4s\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := parsePlaylist(tt.playlist); !errors.Is(err, ErrUnusablePlaylist) {
				t.Errorf("parsePlaylist(%q) error = %v, want ErrUnusablePlaylist", tt.playlist, err)
			}
		})
	}
}
