//go:build integration

package photoapi_test

import (
	"bytes"
	"encoding/json"
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
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and photo APIs behind an httptest server over the
// integration database, plus the storage used to seed real files.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	store   *photos.Store
	fs      *storage.FS
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
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
	store := photos.NewStore(db.Pool())
	api := photoapi.NewAPI(photoapi.Config{
		Store:           store,
		Storage:         fs,
		Thumbnailer:     thumb.New(fs, t.TempDir()),
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
	return &env{server: server, authSvc: authSvc, store: store, fs: fs}
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

// listResp mirrors the list endpoint's JSON body.
type listResp struct {
	Photos     []photos.Photo `json:"photos"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"next_offset"`
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
	privatePhoto := env.seedPhoto(t, photos.Photo{
		Title: "Secret", TakenAt: ptrTime(jun), TakenAtSource: "exif", Private: true,
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
	t.Run("private filter", func(t *testing.T) {
		got := getList(t, client, base, "private=true")
		if got.Total != 1 || got.Photos[0].UID != privatePhoto.UID {
			t.Errorf("private=true returned %v, want [%s]", uids(got.Photos), privatePhoto.UID)
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
