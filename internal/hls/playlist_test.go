package hls

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden regenerates the golden playlists instead of asserting against
// them: go test ./internal/hls -update. The golden input media_ffmpeg.m3u8 is not
// generated — it is what ffmpeg 6.1.1 really wrote for a 14-second clip encoded
// with EncodeArgs, and it is edited only by capturing a new one.
var updateGolden = flag.Bool("update", false, "rewrite the golden playlists")

// mediaBase is the URL prefix the rewriter's resolver is asked to produce in the
// golden test: a per-object media URL, as a signed one would look.
const mediaBase = "https://media.example.test/hls/" +
	"aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899/1080p/"

// goldenURL resolves a segment name the way the golden files expect.
func goldenURL(name string) (string, error) {
	return mediaBase + name + "?sig=deadbeef", nil
}

// readGolden reads the golden file with the given name, failing the test if it
// cannot be read.
func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return string(data)
}

// assertGolden compares got against the golden file, or rewrites the file when
// -update was passed.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing golden %s: %v", name, err)
		}
		return
	}
	want := readGolden(t, name)
	if got != want {
		t.Errorf("playlist does not match %s (re-run with -update to accept):\n--- got ---\n%s\n--- want ---\n%s",
			path, got, want)
	}
}

// TestRewriteMedia_golden verifies a real ffmpeg playlist is served with every
// segment URI and the EXT-X-MAP URI pointing at the caller's URLs, and with every
// other byte — the durations above all — untouched.
func TestRewriteMedia_golden(t *testing.T) {
	t.Parallel()

	got, err := RewriteMedia(readGolden(t, "media_ffmpeg.m3u8"), goldenURL)
	if err != nil {
		t.Fatalf("RewriteMedia: %v", err)
	}
	assertGolden(t, "media_served.m3u8", got)
}

// TestRewriteMedia_preservesEverythingElse verifies the rewriter parses exactly
// the two URI cases and nothing more: a tag it has never seen, a comment, a blank
// line, an unusual duration and a CRLF ending all survive byte for byte.
func TestRewriteMedia_preservesEverythingElse(t *testing.T) {
	t.Parallel()

	in := "#EXTM3U\r\n" +
		"#EXT-X-VERSION:7\r\n" +
		"#EXT-X-SOMETHING-NEW:VALUE=\"00000.m4s\",OTHER=1\r\n" +
		"\r\n" +
		"# a bare comment mentioning 00000.m4s\r\n" +
		"#EXT-X-MAP:URI=\"init.mp4\",BYTERANGE=\"1375@0\"\r\n" +
		"#EXTINF:5.994667,\r\n" +
		"00000.m4s\r\n" +
		"#EXT-X-ENDLIST"

	got, err := RewriteMedia(in, goldenURL)
	if err != nil {
		t.Fatalf("RewriteMedia: %v", err)
	}

	want := "#EXTM3U\r\n" +
		"#EXT-X-VERSION:7\r\n" +
		"#EXT-X-SOMETHING-NEW:VALUE=\"00000.m4s\",OTHER=1\r\n" +
		"\r\n" +
		"# a bare comment mentioning 00000.m4s\r\n" +
		"#EXT-X-MAP:URI=\"" + mediaBase + "init.mp4?sig=deadbeef\",BYTERANGE=\"1375@0\"\r\n" +
		"#EXTINF:5.994667,\r\n" +
		mediaBase + "00000.m4s?sig=deadbeef\r\n" +
		"#EXT-X-ENDLIST"
	if got != want {
		t.Errorf("RewriteMedia:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

// TestRewriteMedia_segmentPaths verifies the resolver is handed the object's
// name whatever path ffmpeg wrote in front of it, since how the segment filename
// is spelled depends on flags no URL builder should have to know about.
func TestRewriteMedia_segmentPaths(t *testing.T) {
	t.Parallel()

	var asked []string
	got, err := RewriteMedia("#EXT-X-MAP:URI=\"/tmp/enc-42/init.mp4\"\n/tmp/enc-42/00007.m4s\n",
		func(name string) (string, error) {
			asked = append(asked, name)
			return "https://cdn.example.test/" + name, nil
		})
	if err != nil {
		t.Fatalf("RewriteMedia: %v", err)
	}
	want := "#EXT-X-MAP:URI=\"https://cdn.example.test/init.mp4\"\nhttps://cdn.example.test/00007.m4s\n"
	if got != want {
		t.Errorf("RewriteMedia = %q, want %q", got, want)
	}
	if len(asked) != 2 || asked[0] != InitName || asked[1] != "00007.m4s" {
		t.Errorf("resolver was asked for %v, want the two base names", asked)
	}
}

// TestRewriteMedia_errors verifies the rewrite fails loudly rather than serving a
// playlist with a hole in it.
func TestRewriteMedia_errors(t *testing.T) {
	t.Parallel()

	resolverErr := errors.New("signing failed")
	failing := func(string) (string, error) { return "", resolverErr }

	tests := []struct {
		name     string
		playlist string
		url      SegmentURL
		wantErr  error
	}{
		{name: "no resolver", playlist: "#EXTM3U\n", url: nil, wantErr: ErrNoSegmentURL},
		{
			name:     "map tag without a URI",
			playlist: "#EXT-X-MAP:BYTERANGE=\"1375@0\"\n",
			url:      goldenURL,
			wantErr:  ErrMalformedPlaylist,
		},
		{
			name:     "map tag with an unterminated URI",
			playlist: "#EXT-X-MAP:URI=\"init.mp4\n",
			url:      goldenURL,
			wantErr:  ErrMalformedPlaylist,
		},
		{name: "URI that is not an object of this layout", playlist: "sub.vtt\n", url: goldenURL, wantErr: ErrInvalidName},
		{name: "URI escaping the rendition", playlist: "../../etc/passwd\n", url: goldenURL, wantErr: ErrInvalidName},
		{name: "resolver failure", playlist: "00000.m4s\n", url: failing, wantErr: resolverErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RewriteMedia(tt.playlist, tt.url)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("RewriteMedia error = %v, want %v", err, tt.wantErr)
			}
			if got != "" {
				t.Errorf("RewriteMedia = %q, want no playlist on error", got)
			}
		})
	}
}

// TestRewriteMedia_empty verifies an empty playlist round-trips to an empty
// string rather than growing a line.
func TestRewriteMedia_empty(t *testing.T) {
	t.Parallel()

	got, err := RewriteMedia("", goldenURL)
	if err != nil {
		t.Fatalf("RewriteMedia: %v", err)
	}
	if got != "" {
		t.Errorf("RewriteMedia(\"\") = %q, want \"\"", got)
	}
}

// TestBuildMaster_golden verifies the master playlist advertises each variant
// with the bandwidth, resolution and codecs a player chooses on, in the order it
// was given.
func TestBuildMaster_golden(t *testing.T) {
	t.Parallel()

	r := rendition1080p(t)
	width, height := r.Fit(3840, 2160)
	got, err := BuildMaster([]Variant{
		{
			URL:       mediaBase + "index.m3u8?sig=deadbeef",
			Bandwidth: r.Bandwidth(),
			Width:     width,
			Height:    height,
			Codecs:    r.Codecs,
		},
		{
			URL:       strings.Replace(mediaBase, "/1080p/", "/720p/", 1) + "index.m3u8?sig=deadbeef",
			Bandwidth: 2_500_000 + AudioBitrate,
			Width:     1280,
			Height:    720,
			Codecs:    "avc1.64001f,mp4a.40.2",
		},
	})
	if err != nil {
		t.Fatalf("BuildMaster: %v", err)
	}
	assertGolden(t, "master.m3u8", got)
}

// TestBuildMaster_rejects verifies a master is never built without the parts a
// player needs to pick a variant without fetching it first.
func TestBuildMaster_rejects(t *testing.T) {
	t.Parallel()

	valid := Variant{URL: "https://example.test/1080p.m3u8", Bandwidth: 6_128_000, Width: 1920, Height: 1080,
		Codecs: "avc1.640028,mp4a.40.2"}

	tests := []struct {
		name     string
		variants []Variant
		wantErr  error
	}{
		{name: "no variants", variants: nil, wantErr: ErrNoVariants},
		{name: "empty list", variants: []Variant{}, wantErr: ErrNoVariants},
		{name: "no URL", variants: []Variant{{Bandwidth: 1, Width: 2, Height: 2, Codecs: "x"}}, wantErr: ErrInvalidVariant},
		{name: "no bandwidth", variants: []Variant{{URL: "u", Width: 2, Height: 2, Codecs: "x"}}, wantErr: ErrInvalidVariant},
		{name: "no resolution", variants: []Variant{{URL: "u", Bandwidth: 1, Codecs: "x"}}, wantErr: ErrInvalidVariant},
		{name: "no codecs", variants: []Variant{{URL: "u", Bandwidth: 1, Width: 2, Height: 2}}, wantErr: ErrInvalidVariant},
		{name: "one bad variant among good ones", variants: []Variant{valid, {URL: "u"}}, wantErr: ErrInvalidVariant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := BuildMaster(tt.variants)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("BuildMaster error = %v, want %v", err, tt.wantErr)
			}
			if got != "" {
				t.Errorf("BuildMaster = %q, want no playlist on error", got)
			}
		})
	}
}
