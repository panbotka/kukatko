package photoapi

import (
	"net/http/httptest"
	"testing"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
)

// TestMasterVariants projects the recorded renditions onto what a master
// playlist advertises, keeping the store's order — a player takes the first
// variant it can play, so that order is the preference order.
func TestMasterVariants(t *testing.T) {
	t.Parallel()

	list := []hlsjob.Encoded{
		{Rendition: "1080p", Width: 1920, Height: 1080, Bandwidth: 5_128_000, Codecs: "avc1.640028,mp4a.40.2"},
		{Rendition: "720p", Width: 1280, Height: 720, Bandwidth: 3_128_000, Codecs: "avc1.64001f,mp4a.40.2"},
	}
	got := masterVariants(list, func(rendition string) string { return rendition + "/index.m3u8" })

	if len(got) != len(list) {
		t.Fatalf("len = %d, want %d", len(got), len(list))
	}
	if got[0].URL != "1080p/index.m3u8" || got[1].URL != "720p/index.m3u8" {
		t.Errorf("URLs = %q/%q, want the per-rendition media playlists", got[0].URL, got[1].URL)
	}
	if got[0].Width != 1920 || got[0].Height != 1080 || got[0].Bandwidth != 5_128_000 {
		t.Errorf("variant[0] = %+v, want the recorded geometry and bandwidth", got[0])
	}
	if got[1].Codecs != "avc1.64001f,mp4a.40.2" {
		t.Errorf("variant[1].Codecs = %q, want the recorded codecs string", got[1].Codecs)
	}
}

// TestMasterVariants_empty checks an empty listing yields no variants, which is
// what makes hls.BuildMaster refuse rather than emit a playlist no player can
// play — the caller answers 404 before it gets that far.
func TestMasterVariants_empty(t *testing.T) {
	t.Parallel()

	if got := masterVariants(nil, func(string) string { return "x" }); len(got) != 0 {
		t.Errorf("variants = %v, want none", got)
	}
}

// TestSegmentMIME pins the two media types: the initialisation segment is a
// plain fragmented MP4, every media fragment a CMAF segment.
func TestSegmentMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: hls.InitName, want: "video/mp4"},
		{name: "00000.m4s", want: "video/iso.segment"},
		{name: "01234.m4s", want: "video/iso.segment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := segmentMIME(tt.name); got != tt.want {
				t.Errorf("segmentMIME(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestPlaylistURIs_carryTheDownloadToken covers what makes a cookie-less
// `<video>` tag work: a player fetches every URI a playlist names as a plain GET
// with nothing else attached, so a request that authenticated with a download
// token must hand that token on.
func TestPlaylistURIs_carryTheDownloadToken(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), "GET",
		"/api/v1/photos/p1/hls/master.m3u8?t=abc%2F1", nil)
	if got := mediaPlaylistURI(req)("1080p"); got != "1080p/index.m3u8?t=abc%2F1" {
		t.Errorf("media playlist URI = %q, want the token repeated and escaped", got)
	}
	if got := segmentURI(req)("00007.m4s"); got != "00007.m4s?t=abc%2F1" {
		t.Errorf("segment URI = %q, want the token repeated and escaped", got)
	}
}

// TestPlaylistURIs_withoutAToken keeps the URIs bare for a cookie-authenticated
// caller: the browser sends the cookie by itself, and a query string nobody
// needs would only make the URIs harder to read.
func TestPlaylistURIs_withoutAToken(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), "GET",
		"/api/v1/photos/p1/hls/master.m3u8", nil)
	if got := mediaPlaylistURI(req)("1080p"); got != "1080p/index.m3u8" {
		t.Errorf("media playlist URI = %q, want a bare relative URI", got)
	}
	if got := segmentURI(req)(hls.InitName); got != hls.InitName {
		t.Errorf("segment URI = %q, want a bare relative URI", got)
	}
}
