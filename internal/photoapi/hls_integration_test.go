//go:build integration

package photoapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// testPlaylist is a media playlist shaped exactly as ffmpeg writes one: relative
// object names, an EXT-X-MAP naming the initialisation segment, and a measured
// duration per segment. The routes must serve it back with only its URIs changed.
const testPlaylist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.000000,
00000.m4s
#EXTINF:4.000000,
00001.m4s
#EXT-X-ENDLIST
`

// seedRendition records one HLS rendition for the photo and, when store is not
// nil, publishes its objects there so a segment request has bytes to answer
// with. It returns the photo it encoded.
func seedRendition(t *testing.T, e *env, store storage.Storage, photo photos.Photo) photos.Photo {
	t.Helper()
	if _, err := hlsjob.NewStore(e.db.Pool()).Save(t.Context(), hlsjob.Encoded{
		PhotoUID: photo.UID, Rendition: hls.Rendition1080p, Playlist: testPlaylist,
		Width: 1920, Height: 1080, Bandwidth: 5_128_000,
		Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 2, DurationMs: 10_000,
	}); err != nil {
		t.Fatalf("recording rendition for %s: %v", photo.UID, err)
	}
	if store == nil {
		return photo
	}
	for _, name := range []string{hls.InitName, "00000.m4s", "00001.m4s"} {
		putSegment(t, e, store, photo, name)
	}
	return photo
}

// putSegment writes one segment's bytes into store under the key the layout
// gives it, the way the encode job publishes them.
func putSegment(t *testing.T, e *env, store storage.Storage, photo photos.Photo, name string) {
	t.Helper()
	key, err := hls.Key(photo.FileHash, hls.Rendition1080p, name)
	if err != nil {
		t.Fatalf("hls.Key(%s): %v", name, err)
	}
	data := []byte("segment-bytes-of-" + name)
	sum := sha256.Sum256(data)
	if err := store.Put(t.Context(), bytes.NewReader(data), storage.StoredFile{
		Hash: hex.EncodeToString(sum[:]), RelPath: key, Size: int64(len(data)), MIME: "video/iso.segment",
	}); err != nil {
		t.Fatalf("publishing %s: %v", key, err)
	}
	_ = e
}

// hlsURL builds the address of one of the three streaming routes for a photo.
func hlsURL(e *env, uid, suffix string) string {
	return e.server.URL + "/api/v1/photos/" + uid + "/hls/" + suffix
}

// TestHLSMaster_advertisesEveryRendition checks the master playlist: the HLS
// media type, one variant per recorded rendition carrying its geometry and
// bandwidth, and a *relative* URI pointing back at this application rather than
// at the object store — the whole reason segments are authorised per request.
func TestHLSMaster_advertisesEveryRendition(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	resp := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, "master.m3u8"), nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Errorf("Content-Type = %q, want application/vnd.apple.mpegurl", got)
	}
	body := readBody(t, resp)
	if !strings.HasPrefix(body, "#EXTM3U\n") {
		t.Fatalf("body does not start with #EXTM3U:\n%s", body)
	}
	if !strings.Contains(body, "RESOLUTION=1920x1080") || !strings.Contains(body, "BANDWIDTH=5128000") {
		t.Errorf("master playlist does not advertise the rendition:\n%s", body)
	}
	if !strings.Contains(body, "\n1080p/index.m3u8\n") {
		t.Errorf("master playlist does not point at the media playlist relatively:\n%s", body)
	}
}

// TestHLSMaster_notFound covers the two "there is nothing to stream" cases: a
// still image, and a photo that does not exist at all. Both are 404 so a player
// stops asking without an error to interpret.
func TestHLSMaster_notFound(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	still := env.seedPhoto(t, photos.Photo{Title: "still", TakenAtSource: "unknown"}, "still.jpg", 9, 9, 9)
	// A video the encoder has never reached: the row exists, the renditions do not.
	unencoded := env.seedVideo(t, "pending.mp4", []byte("not-encoded-yet"))

	for _, uid := range []string{still.UID, unencoded.UID, "ptnosuchphotoatall000000000000000"} {
		resp := mustDo(t, client, http.MethodGet, hlsURL(env, uid, "master.m3u8"), nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("master for %s: status = %d, want 404", uid, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestHLSMedia_servesTheWrittenPlaylist checks the media playlist is ffmpeg's own
// text with only its URIs rewritten: the measured durations and every tag survive
// byte for byte, while the segment names become URIs relative to the playlist.
func TestHLSMedia_servesTheWrittenPlaylist(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	resp := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, "1080p/index.m3u8"), nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Errorf("Content-Type = %q, want application/vnd.apple.mpegurl", got)
	}
	body := readBody(t, resp)
	if body != testPlaylist {
		t.Errorf("media playlist changed more than its URIs:\ngot:\n%s\nwant:\n%s", body, testPlaylist)
	}
}

// TestHLSMedia_notFound covers a rendition name that could never be one (it is
// refused before any lookup), and a well-formed one this video was not encoded
// into. Both are 404: neither exists, and telling them apart would only describe
// the encoder's plan to a caller who cannot use the answer.
func TestHLSMedia_notFound(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	for _, rendition := range []string{"720p", "1080P", "not.a.rendition"} {
		resp := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, rendition+"/index.m3u8"), nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("media playlist for %q: status = %d, want 404", rendition, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestHLSSegment_streamsFromLocalStore proves a deployment that never moved to
// object storage still streams: the bytes come through the application with the
// segment's own media type.
func TestHLSSegment_streamsFromLocalStore(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	tests := []struct {
		name string
		mime string
	}{
		{name: hls.InitName, mime: "video/mp4"},
		{name: "00000.m4s", mime: "video/iso.segment"},
	}
	for _, tt := range tests {
		resp := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, "1080p/"+tt.name), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("segment %s: status = %d, want 200", tt.name, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != tt.mime {
			t.Errorf("segment %s: Content-Type = %q, want %q", tt.name, got, tt.mime)
		}
		if got := readBody(t, resp); got != "segment-bytes-of-"+tt.name {
			t.Errorf("segment %s: body = %q", tt.name, got)
		}
		_ = resp.Body.Close()
	}
}

// TestHLSSegment_redirectsToSignedURL is the shape of the redirect the design
// rests on: a 302 to a freshly signed URL of the segment's own object key, marked
// so no cache may keep it — a stored redirect would outlive its signature and
// send the player to a 403 mid-clip.
func TestHLSSegment_redirectsToSignedURL(t *testing.T) {
	env := newEnvWithMedia(t, newSigningR2(t))
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	client = noRedirectClient(client)
	video := seedRendition(t, env, nil, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	resp := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, "1080p/00001.m4s"), nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", got)
	}
	key, err := hls.Key(video.FileHash, hls.Rendition1080p, "00001.m4s")
	if err != nil {
		t.Fatalf("hls.Key: %v", err)
	}
	assertSignedURL(t, resp.Header.Get("Location"), key)
}

// TestHLSSegment_rejectsMalformedNames pins the guard between a request path and
// an object key: anything the layout could not hold — a traversal, wrong padding,
// a stray extension, the playlist itself — is a 404 and never reaches the store.
func TestHLSSegment_rejectsMalformedNames(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	for _, name := range []string{"0.m4s", "000000.m4s", "00000.mp4", "init.m4s", "%2e%2e%2fsecret"} {
		resp := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, "1080p/"+name), nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("segment %q: status = %d, want 404", name, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestHLSSegment_unknownRenditionOrPhoto checks the two lookups behind a segment:
// a well-formed segment name of a rendition this video was never encoded into,
// and one of a photo that does not exist, are both 404 rather than a redirect to
// an object nobody ever published.
func TestHLSSegment_unknownRenditionOrPhoto(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	client = noRedirectClient(client)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	unencoded := mustDo(t, client, http.MethodGet, hlsURL(env, video.UID, "720p/00000.m4s"), nil)
	defer func() { _ = unencoded.Body.Close() }()
	if unencoded.StatusCode != http.StatusNotFound {
		t.Errorf("unencoded rendition: status = %d, want 404", unencoded.StatusCode)
	}

	missing := mustDo(t, client, http.MethodGet,
		hlsURL(env, "ptnosuchphotoatall000000000000000", "1080p/00000.m4s"), nil)
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("unknown photo: status = %d, want 404", missing.StatusCode)
	}
}

// TestHLSRoutes_requireAuthentication asserts the three routes sit behind the
// same guard as the video endpoint: an anonymous client gets 401, and a download
// token — which is what a cookie-less `<video>` tag carries — gets in.
func TestHLSRoutes_requireAuthentication(t *testing.T) {
	env := newEnv(t)
	client, token := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	for _, suffix := range []string{"master.m3u8", "1080p/index.m3u8", "1080p/00000.m4s"} {
		anon := mustDo(t, &http.Client{}, http.MethodGet, hlsURL(env, video.UID, suffix), nil)
		if anon.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous %s: status = %d, want 401", suffix, anon.StatusCode)
		}
		_ = anon.Body.Close()

		tokened := mustDo(t, &http.Client{}, http.MethodGet,
			hlsURL(env, video.UID, suffix)+"?t="+token, nil)
		if tokened.StatusCode != http.StatusOK {
			t.Errorf("token-authenticated %s: status = %d, want 200", suffix, tokened.StatusCode)
		}
		_ = tokened.Body.Close()
	}
	_ = client
}

// TestHLSPlaylists_repeatTheDownloadToken is what makes a cookie-less `<video>`
// tag play: a player fetches each URI a playlist names as a plain GET, so a
// master fetched with a download token must hand that token down to the media
// playlist, and that one down to every segment.
func TestHLSPlaylists_repeatTheDownloadToken(t *testing.T) {
	env := newEnv(t)
	_, token := env.login(t, "viewer", auth.RoleViewer)
	video := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	master := mustDo(t, &http.Client{}, http.MethodGet,
		hlsURL(env, video.UID, "master.m3u8")+"?t="+token, nil)
	defer func() { _ = master.Body.Close() }()
	if !strings.Contains(readBody(t, master), "1080p/index.m3u8?t="+token) {
		t.Error("master playlist did not repeat the download token onto its variant URI")
	}

	media := mustDo(t, &http.Client{}, http.MethodGet,
		hlsURL(env, video.UID, "1080p/index.m3u8")+"?t="+token, nil)
	defer func() { _ = media.Body.Close() }()
	body := readBody(t, media)
	if !strings.Contains(body, "00000.m4s?t="+token) ||
		!strings.Contains(body, `URI="init.mp4?t=`+token+`"`) {
		t.Errorf("media playlist did not repeat the download token onto its URIs:\n%s", body)
	}
}

// TestPhotoDetail_reportsStreamingAvailability covers the payload flag: the
// player reads it instead of probing, so an encoded video must report true and
// everything else false.
func TestPhotoDetail_reportsStreamingAvailability(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	encoded := seedRendition(t, env, env.fs, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))
	still := env.seedPhoto(t, photos.Photo{Title: "still", TakenAtSource: "unknown"}, "still.jpg", 1, 2, 3)

	tests := []struct {
		uid  string
		want bool
	}{
		{uid: encoded.UID, want: true},
		{uid: still.UID, want: false},
	}
	for _, tt := range tests {
		resp := mustDo(t, client, http.MethodGet, env.server.URL+"/api/v1/photos/"+tt.uid, nil)
		var body struct {
			HLS bool `json:"hls"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode detail of %s: %v", tt.uid, err)
		}
		_ = resp.Body.Close()
		if body.HLS != tt.want {
			t.Errorf("detail of %s: hls = %v, want %v", tt.uid, body.HLS, tt.want)
		}
	}
}

// readBody reads a response body whole, for the small text payloads the playlist
// routes answer with.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(body)
}
