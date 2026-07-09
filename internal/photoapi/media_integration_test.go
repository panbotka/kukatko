//go:build integration

package photoapi_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// The R2 backend under test never reaches the network: only its URL method is
// exercised, which signs locally. These are the settings the signer needs.
const (
	testMediaBaseURL = "https://media.example.test"
	testSignSecret   = "integration signing secret"
	testURLTTL       = time.Hour
)

// newSigningR2 returns an R2 backend configured to mint signed media URLs. Its
// endpoint and credentials are placeholders: nothing in these tests fetches an
// object through it, because answering a media route with a redirect is precisely
// the behaviour that means the application never touches the bytes.
func newSigningR2(t *testing.T) *storage.R2 {
	t.Helper()
	store, err := storage.NewR2(storage.R2Options{
		Endpoint:         "s3.example.test",
		Region:           "auto",
		Bucket:           "kukatko-test",
		AccessKey:        "access",
		SecretKey:        "secret",
		MediaBaseURL:     testMediaBaseURL,
		URLSigningSecret: testSignSecret,
		URLTTL:           testURLTTL,
		TempPath:         t.TempDir(),
	})
	if err != nil {
		t.Fatalf("storage.NewR2: %v", err)
	}
	return store
}

// noRedirectClient makes client return the redirect response itself rather than
// following it — the test asserts on the 302, and the media domain does not exist.
func noRedirectClient(client *http.Client) *http.Client {
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

// assertSignedURL parses raw, checks it addresses wantKey on the media domain, and
// verifies its signature against the signing secret. It returns the parsed URL.
func assertSignedURL(t *testing.T, raw, wantKey string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing signed URL %q: %v", raw, err)
	}
	if got, want := parsed.Scheme+"://"+parsed.Host, testMediaBaseURL; got != want {
		t.Errorf("signed URL origin = %q, want %q", got, want)
	}
	key := strings.TrimPrefix(parsed.Path, "/")
	if key != wantKey {
		t.Errorf("signed URL object key = %q, want %q", key, wantKey)
	}

	signer, err := storage.NewURLSigner(testMediaBaseURL, testSignSecret, "", testURLTTL)
	if err != nil {
		t.Fatalf("storage.NewURLSigner: %v", err)
	}
	query := parsed.Query()
	if err := signer.Verify(key, query.Get(storage.QueryExpires), query.Get(storage.QuerySignature)); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
	return parsed
}

// TestThumbRoute_filesystemBackendStreamsBytes proves the thumb route still
// answers with the JPEG itself when originals live on a local disk, so nothing
// breaks for a deployment that never moved to object storage.
func TestThumbRoute_filesystemBackendStreamsBytes(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Local", TakenAtSource: "unknown"}, "local.jpg", 10, 20, 30)

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+seeded.UID+"/thumb/tile_100", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumb status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("thumb content-type = %q, want image/jpeg", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading thumb body: %v", err)
	}
	if len(body) < 2 || body[0] != 0xFF || body[1] != 0xD8 {
		t.Errorf("thumb body is not JPEG (len %d)", len(body))
	}
}

// TestThumbRoute_publishedBackendRedirectsToSignedURL proves the thumb route
// answers with a redirect to the edge Worker when the backend publishes its
// objects, that the target addresses the thumbnail's object key, and that its
// signature verifies. No image bytes cross the application.
func TestThumbRoute_publishedBackendRedirectsToSignedURL(t *testing.T) {
	env := newEnvWithMedia(t, newSigningR2(t))
	client, _ := env.login(t, "editor", auth.RoleEditor)
	client = noRedirectClient(client)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Remote", TakenAtSource: "unknown"}, "remote.jpg", 40, 50, 60)

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+seeded.UID+"/thumb/tile_100", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("thumb status = %d, want 302", resp.StatusCode)
	}
	wantKey, err := thumb.RelPath(seeded.FileHash, "tile_100")
	if err != nil {
		t.Fatalf("thumb.RelPath: %v", err)
	}
	assertSignedURL(t, resp.Header.Get("Location"), wantKey)

	// A cached redirect would outlive the signature it points at.
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("redirect Cache-Control = %q, want no-store", cc)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading redirect body: %v", err)
	}
	if len(body) > 0 && (body[0] == 0xFF && body[1] == 0xD8) {
		t.Error("redirect response carried JPEG bytes")
	}
}

// TestDownloadRoute_publishedBackendRedirectsToSignedURL proves the download route
// redirects to the original's own object key, which is photos.file_path verbatim.
func TestDownloadRoute_publishedBackendRedirectsToSignedURL(t *testing.T) {
	env := newEnvWithMedia(t, newSigningR2(t))
	client, _ := env.login(t, "editor", auth.RoleEditor)
	client = noRedirectClient(client)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Remote", TakenAtSource: "unknown"}, "orig.jpg", 70, 80, 90)

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+seeded.UID+"/download", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("download status = %d, want 302", resp.StatusCode)
	}
	assertSignedURL(t, resp.Header.Get("Location"), seeded.FilePath)
}

// TestVideoRoute_publishedBackendRedirectsToSignedURL proves video playback points
// at the Worker, which streams the Range requests straight from the bucket — the
// seekable local file http.ServeContent needed is no longer materialized.
func TestVideoRoute_publishedBackendRedirectsToSignedURL(t *testing.T) {
	env := newEnvWithMedia(t, newSigningR2(t))
	client, _ := env.login(t, "editor", auth.RoleEditor)
	client = noRedirectClient(client)
	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Clip", TakenAtSource: "unknown", MediaType: photos.MediaVideo,
	}, "clip.mp4", 15, 25, 35)

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+seeded.UID+"/video", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("video status = %d, want 302", resp.StatusCode)
	}
	assertSignedURL(t, resp.Header.Get("Location"), seeded.FilePath)
}

// TestPhotoPayload_carriesRouteURLsOnFilesystemBackend proves the list payload
// hands the client this application's own media routes when the backend publishes
// nothing, so a local deployment keeps working with the same frontend code.
func TestPhotoPayload_carriesRouteURLsOnFilesystemBackend(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Local", TakenAtSource: "unknown"}, "local.jpg", 11, 22, 33)

	view := fetchFirstPhoto(t, env, client)

	if got, want := view.ThumbURL, "/api/v1/photos/"+seeded.UID+"/thumb/"+thumb.GridSize; got != want {
		t.Errorf("thumb_url = %q, want %q", got, want)
	}
	if got, want := view.DownloadURL, "/api/v1/photos/"+seeded.UID+"/download?original=true"; got != want {
		t.Errorf("download_url = %q, want %q", got, want)
	}
}

// TestPhotoPayload_carriesSignedURLsOnPublishedBackend proves the list payload
// hands the client short-lived signed URLs at the Worker's domain, so a tile is
// rendered without the application transferring the thumbnail.
func TestPhotoPayload_carriesSignedURLsOnPublishedBackend(t *testing.T) {
	env := newEnvWithMedia(t, newSigningR2(t))
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Remote", TakenAtSource: "unknown"}, "remote.jpg", 12, 23, 34)

	view := fetchFirstPhoto(t, env, client)

	wantThumbKey, err := thumb.RelPath(seeded.FileHash, thumb.GridSize)
	if err != nil {
		t.Fatalf("thumb.RelPath: %v", err)
	}
	assertSignedURL(t, view.ThumbURL, wantThumbKey)
	assertSignedURL(t, view.DownloadURL, seeded.FilePath)
}

// fetchFirstPhoto lists the library and returns its single photo, failing the test
// when the page does not hold exactly one.
func fetchFirstPhoto(t *testing.T, env *env, client *http.Client) photos.Photo {
	t.Helper()
	list := getList(t, client, env.server.URL, "")
	if len(list.Photos) != 1 {
		t.Fatalf("listed %d photos, want 1", len(list.Photos))
	}
	return list.Photos[0]
}
