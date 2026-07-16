//go:build integration

package photoapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/stacks"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and photo APIs behind an httptest server over the
// integration database, plus the storage used to seed real files.
type env struct {
	server   *httptest.Server
	authSvc  *auth.Service
	store    *photos.Store
	fs       *storage.FS
	vectors  *vectors.Store
	embedder *fakeEmbedder
	organize *organize.Store
	places   *places.Store
}

// fakeEmbedder is a controllable photoapi.TextEmbedder for the search tests: it
// maps a query string to a fixed embedding vector so semantic ranking is
// deterministic, and can be flipped offline to exercise the degraded fall-back.
type fakeEmbedder struct {
	// byQuery maps query text to the vector returned for it. A query with no
	// entry returns defaultVec, a non-zero unit vector (so cosine distance is
	// never NaN against seeded embeddings).
	byQuery map[string][]float32
	// unavailable, when true, makes TextEmbedding report the sidecar offline.
	unavailable bool
	// calls counts every TextEmbedding invocation, so a test can assert the
	// sidecar was (not) consulted — e.g. a pure filter query must never embed.
	calls int
}

// TextEmbedding returns the configured vector for text, or an error wrapping
// embedding.ErrUnavailable when the fake is offline, mirroring the real client's
// contract so the degraded path can be exercised.
func (f *fakeEmbedder) TextEmbedding(
	_ context.Context, text string,
) ([]float32, string, string, error) {
	f.calls++
	if f.unavailable {
		return nil, "", "", fmt.Errorf("fake sidecar offline: %w", embedding.ErrUnavailable)
	}
	if v, ok := f.byQuery[text]; ok {
		return v, "fake-model", "fake-pretrained", nil
	}
	return imageVecAt(map[int]float32{0: 1}), "fake-model", "fake-pretrained", nil
}

// newEnv builds the HTTP test environment over a freshly truncated database, with
// the filesystem backend serving media.
func newEnv(t *testing.T) *env {
	t.Helper()
	return newEnvWithMedia(t, nil)
}

// newEnvWithMedia builds the HTTP test environment with media served by the given
// storage backend, which decides whether the media routes stream bytes or redirect
// to a signed URL. A nil media backend uses the filesystem store. Seeding always
// writes through the filesystem store (env.fs), so a photo row exists and its
// object key is real whichever backend answers for it.
func newEnvWithMedia(t *testing.T, media storage.Storage) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	mediaStore := storage.Storage(fs)
	if media != nil {
		mediaStore = media
	}
	store := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())
	organizeStore := organize.NewStore(db.Pool())
	placeStore := places.NewStore(db.Pool())
	embedder := &fakeEmbedder{byQuery: map[string][]float32{}}
	api := photoapi.NewAPI(photoapi.Config{
		Store:           store,
		Storage:         mediaStore,
		Thumbnailer:     thumb.New(fs, t.TempDir()),
		Similar:         vectorStore,
		Embedder:        embedder,
		Favorites:       organizeStore,
		Ratings:         organizeStore,
		Organizer:       organizeStore,
		Users:           authStore,
		Places:          placeStore,
		Stacker:         stacks.New(store, stacks.Config{Enabled: true, Rules: stacks.RuleSet{BaseName: true}}),
		RequireAuth:     authAPI.RequireAuth,
		RequireWrite:    authAPI.RequireWrite,
		RequireDownload: authAPI.RequireAuthOrDownloadToken,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server: server, authSvc: authSvc, store: store,
		fs: fs, vectors: vectorStore, embedder: embedder, organize: organizeStore,
		places: placeStore,
	}
}

// login creates a user with the given role and returns a cookie-bearing client
// plus the session's download token.
func (e *env) login(t *testing.T, username string, role auth.Role) (*http.Client, string) {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := mustDo(t, client, http.MethodPost, e.server.URL+"/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	var lr struct {
		DownloadToken string `json:"download_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if lr.DownloadToken == "" {
		t.Fatal("login returned empty download token")
	}
	return client, lr.DownloadToken
}

// seedPhoto stores a real JPEG of the given colour and catalogues a photo (with a
// primary file) carrying the template's metadata. The colour must be unique per
// photo so the content hashes differ.
func (e *env) seedPhoto(t *testing.T, template photos.Photo, name string, r, g, b uint8) photos.Photo {
	t.Helper()
	data := jpegBytes(t, r, g, b)
	var takenAt time.Time
	if template.TakenAt != nil {
		takenAt = *template.TakenAt
	}
	stored, err := e.fs.Store(t.Context(), bytes.NewReader(data), takenAt, name)
	if err != nil {
		t.Fatalf("storage.Store: %v", err)
	}

	template.FileHash = stored.Hash
	template.FilePath = stored.RelPath
	template.FileName = name
	template.FileSize = stored.Size
	template.FileMime = stored.MIME
	if template.FileWidth == 0 {
		template.FileWidth, template.FileHeight, template.FileOrientation = 64, 48, 1
	}

	created, err := e.store.Create(t.Context(), template)
	if err != nil {
		t.Fatalf("store.Create(%s): %v", name, err)
	}
	if _, err := e.store.CreateFile(t.Context(), photos.PhotoFile{
		PhotoUID:  created.UID,
		FilePath:  created.FilePath,
		FileHash:  created.FileHash,
		FileSize:  created.FileSize,
		FileMime:  created.FileMime,
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("store.CreateFile(%s): %v", name, err)
	}
	return created
}

// jpegBytes encodes a small distinct JPEG so each seeded photo gets a unique
// content hash.
func jpegBytes(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	const w, h = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			ramp := uint8(x * 255 / (w - 1))
			img.Set(x, y, color.RGBA{R: uint8((int(r) + int(ramp)) / 2), G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}

// mustDo performs an HTTP request with an optional JSON body and fails the test
// on a transport error.
func mustDo(t *testing.T, client *http.Client, method, urlStr string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, urlStr, reader)
	if err != nil {
		t.Fatalf("NewRequest %s %s: %v", method, urlStr, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

// listResp mirrors the list and search endpoints' JSON body. Mode and Degraded
// are only populated by the search endpoint.
type listResp struct {
	Photos        []photos.Photo `json:"photos"`
	Total         int            `json:"total"`
	Limit         int            `json:"limit"`
	Offset        int            `json:"offset"`
	NextOffset    *int           `json:"next_offset"`
	Mode          string         `json:"mode"`
	Degraded      bool           `json:"degraded"`
	UnknownTokens []string       `json:"unknown_tokens"`
}

// getList fetches the list endpoint with the given query and decodes the body.
func getList(t *testing.T, client *http.Client, base, query string) listResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos?"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d for %q, want 200", resp.StatusCode, query)
	}
	var out listResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

// uids returns the UIDs of the photos in order, for compact ordering assertions.
func uids(list []photos.Photo) []string {
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = p.UID
	}
	return out
}

// ptrTime is a small helper to take the address of a time literal.
func ptrTime(t time.Time) *time.Time { return &t }

// TestList_filters exercises every list filter against a seeded library.
func TestList_filters(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	jan := time.Date(2022, 1, 15, 12, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	withGPS := env.seedPhoto(t, photos.Photo{
		Title: "Prague", TakenAt: ptrTime(jun), TakenAtSource: "exif",
		Lat: new(50.08), Lng: new(14.42), CameraMake: "Canon", CameraModel: "EOS R6", LensModel: "RF 50",
	}, "prague.jpg", 200, 10, 10)
	noGPS := env.seedPhoto(t, photos.Photo{
		Title: "Studio", TakenAt: ptrTime(jan), TakenAtSource: "exif", CameraMake: "Nikon", CameraModel: "Z6",
	}, "studio.jpg", 10, 200, 10)
	env.seedPhoto(t, photos.Photo{
		Title: "Secret", TakenAt: ptrTime(jun), TakenAtSource: "exif",
	}, "secret.jpg", 10, 10, 200)

	t.Run("default returns all live", func(t *testing.T) {
		got := getList(t, client, base, "")
		if got.Total != 3 || len(got.Photos) != 3 {
			t.Fatalf("default list total=%d len=%d, want 3/3", got.Total, len(got.Photos))
		}
	})
	t.Run("has_gps true", func(t *testing.T) {
		got := getList(t, client, base, "has_gps=true")
		if got.Total != 1 || got.Photos[0].UID != withGPS.UID {
			t.Errorf("has_gps=true returned %v, want [%s]", uids(got.Photos), withGPS.UID)
		}
	})
	t.Run("has_gps false", func(t *testing.T) {
		got := getList(t, client, base, "has_gps=false")
		if got.Total != 2 {
			t.Errorf("has_gps=false total=%d, want 2", got.Total)
		}
	})
	t.Run("camera filter", func(t *testing.T) {
		got := getList(t, client, base, "camera=nikon")
		if got.Total != 1 || got.Photos[0].UID != noGPS.UID {
			t.Errorf("camera=nikon returned %v, want [%s]", uids(got.Photos), noGPS.UID)
		}
	})
	t.Run("lens filter", func(t *testing.T) {
		got := getList(t, client, base, "lens=RF")
		if got.Total != 1 || got.Photos[0].UID != withGPS.UID {
			t.Errorf("lens=RF returned %v, want [%s]", uids(got.Photos), withGPS.UID)
		}
	})
	t.Run("a stale private param is ignored", func(t *testing.T) {
		// The private filter is gone. A bookmarked URL that still carries it must
		// answer the unfiltered list rather than 400 on an unknown key.
		got := getList(t, client, base, "private=true")
		if got.Total != 3 {
			t.Errorf("private=true total=%d, want all 3 (the param is ignored)", got.Total)
		}
	})
	t.Run("search filter", func(t *testing.T) {
		got := getList(t, client, base, "q=studio")
		if got.Total != 1 || got.Photos[0].UID != noGPS.UID {
			t.Errorf("q=studio returned %v, want [%s]", uids(got.Photos), noGPS.UID)
		}
	})
	t.Run("date range", func(t *testing.T) {
		got := getList(t, client, base, "taken_after=2023-01-01&taken_before=2023-12-31")
		if got.Total != 2 {
			t.Errorf("date range total=%d, want 2 (June photos)", got.Total)
		}
	})
	t.Run("invalid filter is 400", func(t *testing.T) {
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos?limit=lots", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 for invalid limit", resp.StatusCode)
		}
	})
}

// TestList_sortAndPagination verifies ordering and cursor/total pagination.
func TestList_sortAndPagination(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	// Seed three photos with strictly increasing capture times and titles.
	t1 := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	pa := env.seedPhoto(t, photos.Photo{Title: "Alpha", TakenAt: ptrTime(t1), TakenAtSource: "exif"}, "a.jpg", 200, 1, 1)
	pb := env.seedPhoto(t, photos.Photo{Title: "Bravo", TakenAt: ptrTime(t2), TakenAtSource: "exif"}, "b.jpg", 1, 200, 1)
	pc := env.seedPhoto(t, photos.Photo{Title: "Charlie", TakenAt: ptrTime(t3), TakenAtSource: "exif"}, "c.jpg", 1, 1, 200)

	t.Run("newest first", func(t *testing.T) {
		got := getList(t, client, base, "sort=newest")
		want := []string{pc.UID, pb.UID, pa.UID}
		if g := uids(got.Photos); !equalStrings(g, want) {
			t.Errorf("newest order = %v, want %v", g, want)
		}
	})
	t.Run("oldest first", func(t *testing.T) {
		got := getList(t, client, base, "sort=oldest")
		want := []string{pa.UID, pb.UID, pc.UID}
		if g := uids(got.Photos); !equalStrings(g, want) {
			t.Errorf("oldest order = %v, want %v", g, want)
		}
	})
	t.Run("title ascending", func(t *testing.T) {
		got := getList(t, client, base, "sort=title")
		want := []string{pa.UID, pb.UID, pc.UID}
		if g := uids(got.Photos); !equalStrings(g, want) {
			t.Errorf("title order = %v, want %v", g, want)
		}
	})
	t.Run("pagination with total and next offset", func(t *testing.T) {
		first := getList(t, client, base, "sort=oldest&limit=2&offset=0")
		if first.Total != 3 || len(first.Photos) != 2 {
			t.Fatalf("page 1 total=%d len=%d, want 3/2", first.Total, len(first.Photos))
		}
		if first.NextOffset == nil || *first.NextOffset != 2 {
			t.Fatalf("page 1 next_offset = %v, want 2", first.NextOffset)
		}
		second := getList(t, client, base, "sort=oldest&limit=2&offset=2")
		if len(second.Photos) != 1 || second.NextOffset != nil {
			t.Fatalf("page 2 len=%d next=%v, want 1/nil", len(second.Photos), second.NextOffset)
		}
		// Together the pages cover every photo exactly once, in order.
		all := append(uids(first.Photos), uids(second.Photos)...)
		want := []string{pa.UID, pb.UID, pc.UID}
		if !equalStrings(all, want) {
			t.Errorf("paged order = %v, want %v", all, want)
		}
	})
}

// TestList_albumScopeChronological verifies that an album-scoped listing is
// always chronological — oldest capture time first, a photo with no capture
// time falling back to its upload time (sorting last here, as it was uploaded
// just now) — no matter what sort or order parameters the query carries.
func TestList_albumScopeChronological(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	tOld := time.Date(2019, 5, 1, 9, 0, 0, 0, time.UTC)
	tNew := time.Date(2023, 5, 1, 9, 0, 0, 0, time.UTC)
	newest := env.seedPhoto(t, photos.Photo{Title: "Newest", TakenAt: ptrTime(tNew), TakenAtSource: "exif"}, "n.jpg", 210, 5, 5)
	oldest := env.seedPhoto(t, photos.Photo{Title: "Oldest", TakenAt: ptrTime(tOld), TakenAtSource: "exif"}, "o.jpg", 5, 210, 5)
	undated := env.seedPhoto(t, photos.Photo{Title: "Undated"}, "u.jpg", 5, 5, 210)
	outside := env.seedPhoto(t, photos.Photo{Title: "Outside", TakenAt: ptrTime(tOld), TakenAtSource: "exif"}, "x.jpg", 120, 120, 5)

	album, err := env.organize.CreateAlbum(t.Context(), organize.Album{Title: "Trip"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	// Add newest-first so the chronological result cannot be insertion order.
	for _, uid := range []string{newest.UID, undated.UID, oldest.UID} {
		if err := env.organize.AddPhoto(t.Context(), album.UID, uid); err != nil {
			t.Fatalf("AddPhoto(%s): %v", uid, err)
		}
	}

	want := []string{oldest.UID, newest.UID, undated.UID}
	queries := []string{
		"album=" + album.UID,
		"album=" + album.UID + "&sort=newest",
		"album=" + album.UID + "&sort=newest&order=desc",
		"album=" + album.UID + "&sort=title&order=desc",
		"album=" + album.UID + "&sort=added",
	}
	for _, query := range queries {
		got := getList(t, client, base, query)
		if g := uids(got.Photos); !equalStrings(g, want) {
			t.Errorf("order for %q = %v, want chronological %v", query, g, want)
		}
		for _, p := range got.Photos {
			if p.UID == outside.UID {
				t.Errorf("query %q leaked photo outside the album", query)
			}
		}
	}
}

// equalStrings reports whether two string slices are equal element-wise.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDetail verifies full detail retrieval including files, and the 404 path.
func TestDetail(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Detail", TakenAtSource: "exif", Lat: new(48.2), Lng: new(16.3),
		Exif: json.RawMessage(`{"Make":"Canon"}`),
	}, "detail.jpg", 30, 60, 90)

	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+seeded.UID, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var detail struct {
		photos.Photo
		Files []photos.PhotoFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.UID != seeded.UID || detail.Title != "Detail" {
		t.Errorf("detail metadata mismatch: %+v", detail.Photo)
	}
	if detail.Lat == nil || detail.Lng == nil {
		t.Errorf("detail missing GPS: %+v", detail.Photo)
	}
	if len(detail.Exif) == 0 {
		t.Error("detail missing EXIF")
	}
	if len(detail.Files) != 1 || !detail.Files[0].IsPrimary {
		t.Errorf("detail files = %+v, want one primary file", detail.Files)
	}

	missing := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/ph_nope", nil)
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("missing detail status = %d, want 404", missing.StatusCode)
	}
}

// TestDetailUploader verifies the detail response resolves a photo's uploader to
// a human-readable name, and omits the uploader for a photo with no uploader.
func TestDetailUploader(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	uploader, err := env.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "cameraman", Password: testPassword, DisplayName: "Camera Man", Role: auth.RoleEditor,
	})
	if err != nil {
		t.Fatalf("CreateUser(uploader): %v", err)
	}

	type uploaderRef struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	}
	decodeUploader := func(t *testing.T, uid string) *uploaderRef {
		t.Helper()
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("detail status = %d, want 200", resp.StatusCode)
		}
		var detail struct {
			Uploader *uploaderRef `json:"uploader"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		return detail.Uploader
	}

	withUploader := env.seedPhoto(t, photos.Photo{
		Title: "Uploaded", TakenAtSource: "exif", UploadedBy: &uploader.UID,
	}, "uploaded.jpg", 10, 20, 30)
	if got := decodeUploader(t, withUploader.UID); got == nil {
		t.Fatal("uploader = nil, want resolved reference")
	} else if got.UID != uploader.UID || got.Name != "Camera Man" {
		t.Errorf("uploader = %+v, want {uid=%s name=Camera Man}", got, uploader.UID)
	}

	noUploader := env.seedPhoto(t, photos.Photo{
		Title: "Imported", TakenAtSource: "exif",
	}, "imported.jpg", 40, 50, 60)
	if got := decodeUploader(t, noUploader.UID); got != nil {
		t.Errorf("uploader = %+v, want nil for a photo with no uploader", got)
	}
}

// TestUpdateMetadata verifies the PATCH endpoint applies partial updates, clears
// via null, validates input, and enforces write access.
func TestUpdateMetadata(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Before", Description: "keep", TakenAtSource: "exif",
		Lat: new(10.0), Lng: new(20.0),
	}, "edit.jpg", 70, 80, 90)
	url := base + "/api/v1/photos/" + seeded.UID

	t.Run("editor updates title and clears gps", func(t *testing.T) {
		body := []byte(`{"title":"After","lat":null,"lng":null}`)
		resp := mustDo(t, editor, http.MethodPatch, url, body)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("patch status = %d, want 200", resp.StatusCode)
		}
		var got photos.Photo
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Title != "After" {
			t.Errorf("title = %q, want After", got.Title)
		}
		if got.Description != "keep" {
			t.Errorf("description = %q, want unchanged 'keep'", got.Description)
		}
		if got.Lat != nil || got.Lng != nil {
			t.Errorf("gps not cleared: lat=%v lng=%v", got.Lat, got.Lng)
		}
	})

	t.Run("editor sets ai_note and detail returns it", func(t *testing.T) {
		body := []byte(`{"ai_note":"detected: dog, beach, ball"}`)
		resp := mustDo(t, editor, http.MethodPatch, url, body)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("patch status = %d, want 200", resp.StatusCode)
		}
		var got photos.Photo
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode patch: %v", err)
		}
		if got.AiNote != "detected: dog, beach, ball" {
			t.Errorf("patch ai_note = %q, want %q", got.AiNote, "detected: dog, beach, ball")
		}

		// The detail response must carry the persisted ai_note as well.
		detailResp := mustDo(t, editor, http.MethodGet, url, nil)
		defer func() { _ = detailResp.Body.Close() }()
		var detail photos.Photo
		if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
			t.Fatalf("decode detail: %v", err)
		}
		if detail.AiNote != "detected: dog, beach, ball" {
			t.Errorf("detail ai_note = %q, want %q", detail.AiNote, "detected: dog, beach, ball")
		}
	})

	t.Run("invalid coordinate is 400", func(t *testing.T) {
		resp := mustDo(t, editor, http.MethodPatch, url, []byte(`{"lat":200}`))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("viewer is forbidden", func(t *testing.T) {
		resp := mustDo(t, viewer, http.MethodPatch, url, []byte(`{"title":"Nope"}`))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer patch status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("unknown photo is 404", func(t *testing.T) {
		resp := mustDo(t, editor, http.MethodPatch, base+"/api/v1/photos/ph_missing", []byte(`{"title":"x"}`))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})
}

// TestUpdateMetadata_returnsFullDetail pins the PATCH response to the very body
// the detail endpoint answers with. The client replaces the photo detail it holds
// with this response, so a bare photo — no files, albums, labels or is_favorite —
// blanks the organization panel and crashes the detail page.
func TestUpdateMetadata_returnsFullDetail(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Before", Description: "old caption", TakenAtSource: "exif",
	}, "detail-patch.jpg", 100, 110, 120)
	url := base + "/api/v1/photos/" + seeded.UID

	// The photo belongs to an album, carries a label and is favorited: exactly the
	// state the buggy PATCH response used to drop.
	album, err := env.organize.CreateAlbum(t.Context(), organize.Album{Title: "Trip"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if err := env.organize.AddPhoto(t.Context(), album.UID, seeded.UID); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}
	label, err := env.organize.CreateLabel(t.Context(), organize.Label{Name: "beach"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if err := env.organize.AttachLabel(
		t.Context(), seeded.UID, label.UID, organize.SourceManual, 0,
	); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}
	fav := mustDo(t, editor, http.MethodPut, url+"/favorite", nil)
	defer func() { _ = fav.Body.Close() }()
	if fav.StatusCode != http.StatusOK && fav.StatusCode != http.StatusNoContent {
		t.Fatalf("favorite status = %d, want 200/204", fav.StatusCode)
	}

	patched, patchedRaw := decodeDetail(t, editor, http.MethodPatch, url,
		[]byte(`{"description":"new caption","notes":"shot at dusk"}`))

	if patched.Description != "new caption" || patched.Notes != "shot at dusk" {
		t.Errorf("patched metadata = %q/%q, want 'new caption'/'shot at dusk'",
			patched.Description, patched.Notes)
	}
	if len(patched.Files) != 1 || !patched.Files[0].IsPrimary {
		t.Errorf("patch files = %+v, want one primary file", patched.Files)
	}
	if len(patched.Albums) != 1 || patched.Albums[0].UID != album.UID || patched.Albums[0].Title != "Trip" {
		t.Errorf("patch albums = %+v, want the photo's album", patched.Albums)
	}
	if len(patched.Labels) != 1 || patched.Labels[0].UID != label.UID || patched.Labels[0].Name != "beach" {
		t.Errorf("patch labels = %+v, want the photo's label", patched.Labels)
	}
	if !patched.IsFavorite {
		t.Error("patch is_favorite = false, want true")
	}
	if patched.ThumbURL == "" {
		t.Error("patch thumb_url is empty, want the media URL stamped on")
	}

	// The strongest guarantee: PATCH and GET answer with the identical document,
	// so the client can swap one for the other without losing a single field.
	_, detailRaw := decodeDetail(t, editor, http.MethodGet, url, nil)
	if !bytes.Equal(patchedRaw, detailRaw) {
		t.Errorf("PATCH body differs from GET body:\n patch  = %s\n detail = %s", patchedRaw, detailRaw)
	}
}

// detailResp mirrors the photo detail body shared by GET /photos/{uid} and the
// metadata PATCH: the photo, its per-user annotation, files and memberships.
type detailResp struct {
	photos.Photo
	IsFavorite bool               `json:"is_favorite"`
	Files      []photos.PhotoFile `json:"files"`
	Albums     []struct {
		UID   string `json:"uid"`
		Title string `json:"title"`
	} `json:"albums"`
	Labels []struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"labels"`
}

// decodeDetail performs the request, requires 200 and returns the decoded detail
// body together with its raw JSON, so two responses can be compared byte for byte.
func decodeDetail(
	t *testing.T, client *http.Client, method, urlStr string, body []byte,
) (detailResp, []byte) {
	t.Helper()
	resp := mustDo(t, client, method, urlStr, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s status = %d, want 200", method, urlStr, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s body: %v", method, err)
	}
	var out detailResp
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s body: %v", method, err)
	}
	return out, raw
}

// TestArchive verifies archiving hides a photo from the default listing, that it
// can be filtered for and restored, and that viewers cannot archive.
func TestArchive(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	base := env.server.URL

	keep := env.seedPhoto(t, photos.Photo{Title: "Keep", TakenAtSource: "unknown"}, "keep.jpg", 5, 5, 200)
	trash := env.seedPhoto(t, photos.Photo{Title: "Trash", TakenAtSource: "unknown"}, "trash.jpg", 200, 5, 5)

	t.Run("viewer cannot archive", func(t *testing.T) {
		resp := mustDo(t, viewer, http.MethodPost, base+"/api/v1/photos/"+trash.UID+"/archive", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer archive status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("editor archives and it leaves the default list", func(t *testing.T) {
		resp := mustDo(t, editor, http.MethodPost, base+"/api/v1/photos/"+trash.UID+"/archive", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("archive status = %d, want 200", resp.StatusCode)
		}

		def := getList(t, editor, base, "")
		if def.Total != 1 || def.Photos[0].UID != keep.UID {
			t.Errorf("default list = %v, want only [%s]", uids(def.Photos), keep.UID)
		}
		only := getList(t, editor, base, "archived=only")
		if only.Total != 1 || only.Photos[0].UID != trash.UID {
			t.Errorf("archived=only = %v, want [%s]", uids(only.Photos), trash.UID)
		}
		all := getList(t, editor, base, "archived=true")
		if all.Total != 2 {
			t.Errorf("archived=true total = %d, want 2", all.Total)
		}
	})

	t.Run("unarchive restores it", func(t *testing.T) {
		resp := mustDo(t, editor, http.MethodPost, base+"/api/v1/photos/"+trash.UID+"/unarchive", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unarchive status = %d, want 200", resp.StatusCode)
		}
		def := getList(t, editor, base, "")
		if def.Total != 2 {
			t.Errorf("after unarchive default total = %d, want 2", def.Total)
		}
	})
}

// TestThumbnail verifies thumbnail generation, streaming, caching headers, and
// size validation.
func TestThumbnail(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	seeded := env.seedPhoto(t, photos.Photo{Title: "Thumb", TakenAtSource: "unknown"}, "thumb.jpg", 120, 60, 30)
	url := base + "/api/v1/photos/" + seeded.UID + "/thumb/tile_100"

	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumb status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("thumb content-type = %q, want image/jpeg", ct)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Error("thumb missing ETag")
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 2 || body[0] != 0xFF || body[1] != 0xD8 {
		t.Errorf("thumb body is not JPEG (len %d)", len(body))
	}

	t.Run("conditional request yields 304", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		req.Header.Set("If-None-Match", etag)
		r2, err := client.Do(req)
		if err != nil {
			t.Fatalf("conditional GET: %v", err)
		}
		defer func() { _ = r2.Body.Close() }()
		if r2.StatusCode != http.StatusNotModified {
			t.Errorf("conditional status = %d, want 304", r2.StatusCode)
		}
	})

	t.Run("unknown size is 400", func(t *testing.T) {
		r2 := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+seeded.UID+"/thumb/huge", nil)
		defer func() { _ = r2.Body.Close() }()
		if r2.StatusCode != http.StatusBadRequest {
			t.Errorf("unknown size status = %d, want 400", r2.StatusCode)
		}
	})
}

// TestDownload verifies the original streams back byte-for-byte with the right
// headers, that the download token authorises a cookie-less request, and that an
// unauthenticated request is rejected.
func TestDownload(t *testing.T) {
	env := newEnv(t)
	client, token := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	original := jpegBytes(t, 33, 66, 99)
	stored, err := env.fs.Store(t.Context(), bytes.NewReader(original), time.Time{}, "orig.jpg")
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}
	created, err := env.store.Create(t.Context(), photos.Photo{
		FileHash: stored.Hash, FilePath: stored.RelPath, FileName: "orig.jpg",
		FileSize: stored.Size, FileMime: stored.MIME, FileWidth: 64, FileHeight: 48,
		FileOrientation: 1, TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	url := base + "/api/v1/photos/" + created.UID + "/download"

	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != stored.MIME {
		t.Errorf("download content-type = %q, want %q", ct, stored.MIME)
	}
	// Content-Length set from the known size proves we did not buffer-then-measure.
	if cl := resp.Header.Get("Content-Length"); cl != strconv.FormatInt(stored.Size, 10) {
		t.Errorf("download Content-Length = %q, want %d", cl, stored.Size)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Error("download missing Content-Disposition")
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, original) {
		t.Errorf("downloaded %d bytes, want the %d original bytes", len(body), len(original))
	}

	t.Run("download token authorises without cookie", func(t *testing.T) {
		anon := &http.Client{}
		r2 := mustDo(t, anon, http.MethodGet, url+"?t="+token, nil)
		defer func() { _ = r2.Body.Close() }()
		if r2.StatusCode != http.StatusOK {
			t.Errorf("token download status = %d, want 200", r2.StatusCode)
		}
	})

	t.Run("no cookie and no token is 401", func(t *testing.T) {
		anon := &http.Client{}
		r2 := mustDo(t, anon, http.MethodGet, url, nil)
		defer func() { _ = r2.Body.Close() }()
		if r2.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous download status = %d, want 401", r2.StatusCode)
		}
	})

	t.Run("invalid token is 401", func(t *testing.T) {
		anon := &http.Client{}
		r2 := mustDo(t, anon, http.MethodGet, url+"?t=bogus", nil)
		defer func() { _ = r2.Body.Close() }()
		if r2.StatusCode != http.StatusUnauthorized {
			t.Errorf("bad-token download status = %d, want 401", r2.StatusCode)
		}
	})
}

// TestList_requiresAuth verifies that list and detail reject anonymous callers.
func TestList_requiresAuth(t *testing.T) {
	env := newEnv(t)
	base := env.server.URL
	anon := &http.Client{}

	for _, path := range []string{"/api/v1/photos", "/api/v1/photos/ph_x"} {
		resp := mustDo(t, anon, http.MethodGet, base+path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s anonymous status = %d, want 401", path, resp.StatusCode)
		}
	}
}

// timelineResp mirrors the timeline endpoint's JSON body.
type timelineResp struct {
	Buckets []struct {
		Year       int `json:"year"`
		Month      int `json:"month"`
		Count      int `json:"count"`
		Cumulative int `json:"cumulative"`
	} `json:"buckets"`
	Total int `json:"total"`
}

// TestTimeline exercises the month-histogram endpoint: default buckets with
// cumulative counts and archived excluded, and the filter query params honoured
// so the buckets match the same-filtered list.
func TestTimeline(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	base := env.server.URL

	dec := time.Date(2023, 12, 20, 9, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	env.seedPhoto(t, photos.Photo{Title: "Dec A", TakenAt: ptrTime(dec), TakenAtSource: "exif"},
		"deca.jpg", 200, 10, 10)
	env.seedPhoto(t, photos.Photo{Title: "Dec B", TakenAt: ptrTime(dec), TakenAtSource: "exif"},
		"decb.jpg", 10, 200, 10)
	jun1 := env.seedPhoto(t, photos.Photo{Title: "Jun", TakenAt: ptrTime(jun), TakenAtSource: "exif"},
		"jun.jpg", 10, 10, 200)
	archived := env.seedPhoto(t, photos.Photo{Title: "Old", TakenAt: ptrTime(dec), TakenAtSource: "exif"},
		"old.jpg", 30, 30, 30)
	if _, err := env.store.Archive(t.Context(), archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	t.Run("default histogram excludes archived", func(t *testing.T) {
		got := getTimeline(t, client, base, "")
		if got.Total != 3 {
			t.Fatalf("total = %d, want 3 (archived excluded)", got.Total)
		}
		if len(got.Buckets) != 2 {
			t.Fatalf("buckets = %+v, want 2 months", got.Buckets)
		}
		if got.Buckets[0].Year != 2023 || got.Buckets[0].Month != 12 ||
			got.Buckets[0].Count != 2 || got.Buckets[0].Cumulative != 0 {
			t.Errorf("bucket[0] = %+v, want 2023-12 count 2 cumulative 0", got.Buckets[0])
		}
		if got.Buckets[1].Year != 2023 || got.Buckets[1].Month != 6 ||
			got.Buckets[1].Count != 1 || got.Buckets[1].Cumulative != 2 {
			t.Errorf("bucket[1] = %+v, want 2023-06 count 1 cumulative 2", got.Buckets[1])
		}
	})

	t.Run("filter scopes the histogram like the list", func(t *testing.T) {
		const juneOnly = "taken_after=2023-06-01&taken_before=2023-06-30"
		got := getTimeline(t, client, base, juneOnly)
		if got.Total != 1 || len(got.Buckets) != 1 {
			t.Fatalf("scoped timeline = %+v total=%d, want the single June photo", got.Buckets, got.Total)
		}
		if got.Buckets[0].Month != 6 || got.Buckets[0].Count != 1 {
			t.Errorf("bucket = %+v, want 2023-06 count 1", got.Buckets[0])
		}
		// The scoped list agrees on the total and the single member.
		list := getList(t, client, base, juneOnly)
		if list.Total != got.Total || len(list.Photos) != 1 || list.Photos[0].UID != jun1.UID {
			t.Errorf("list total=%d photos=%v, want 1/[%s]", list.Total, uids(list.Photos), jun1.UID)
		}
	})

	t.Run("invalid filter is 400", func(t *testing.T) {
		resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/timeline?archived=maybe", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 for invalid archived", resp.StatusCode)
		}
	})

	t.Run("requires auth", func(t *testing.T) {
		resp := mustDo(t, &http.Client{}, http.MethodGet, base+"/api/v1/photos/timeline", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous status = %d, want 401", resp.StatusCode)
		}
	})
}

// getTimeline fetches the timeline endpoint with the given query and decodes the
// body, failing the test on a non-200 status.
func getTimeline(t *testing.T, client *http.Client, base, query string) timelineResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/timeline?"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("timeline status = %d for %q, want 200", resp.StatusCode, query)
	}
	var out timelineResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	return out
}
