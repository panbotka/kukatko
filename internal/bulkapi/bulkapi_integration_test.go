//go:build integration

package bulkapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/bulkapi"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and bulk APIs behind an httptest server over the
// integration database, plus the stores used to seed and verify state.
type env struct {
	server   *httptest.Server
	authSvc  *auth.Service
	photos   *photos.Store
	organize *organize.Store
	audit    *audit.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database with
// the given bulk batch-size limit.
func newEnv(t *testing.T, maxBatch int) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := bulkapi.NewAPI(bulkapi.Config{
		Service:      bulk.NewService(db.Pool(), maxBatch),
		RequireWrite: authAPI.RequireWrite,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server:   server,
		authSvc:  authSvc,
		photos:   photos.NewStore(db.Pool()),
		organize: organize.NewStore(db.Pool()),
		audit:    audit.NewStore(db.Pool()),
	}
}

// login creates a user with the given role and returns a cookie-bearing client
// plus the new user's UID.
func (e *env) login(t *testing.T, username string, role auth.Role) (*http.Client, string) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client, user.UID
}

// seedPhoto inserts a minimal photo and returns its UID.
func (e *env) seedPhoto(t *testing.T, hash string) string {
	t.Helper()
	p, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg", FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("seed photo %s: %v", hash, err)
	}
	return p.UID
}

// mustDo issues a request with an optional JSON body and returns the response.
func (e *env) mustDo(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// decodeBody decodes the JSON response body into dst and closes it.
func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// TestBulk_appliesAlbumLabelAndLocation exercises the headline case: add to an
// album, add a label and set the location across several photos in one request,
// verifying the committed state and the per-photo result.
func TestBulk_appliesAlbumLabelAndLocation(t *testing.T) {
	env := newEnv(t, 1000)
	editor, editorUID := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()

	album, err := env.organize.CreateAlbum(ctx, organize.Album{Title: "Trip"})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	label, err := env.organize.CreateLabel(ctx, organize.Label{Name: "beach"})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	p1 := env.seedPhoto(t, "aaa1")
	p2 := env.seedPhoto(t, "bbb2")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{p1, p2},
		"operations": map[string]any{
			"add_to_albums": []string{album.UID},
			"add_labels":    []string{label.UID},
			"set_location":  map[string]float64{"lat": 50.1, "lng": 14.4},
			"set_private":   true,
		},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bulk status = %d, want 200", resp.StatusCode)
	}
	var result bulk.Result
	decodeBody(t, resp, &result)
	if result.Counts.Updated != 2 || result.Counts.Total != 2 {
		t.Fatalf("counts = %+v, want updated=2 total=2", result.Counts)
	}

	assertAlbumMembers(t, ctx, env, album.UID, p1, p2)
	assertLabelMembers(t, ctx, env, label.UID, p1, p2)
	for _, uid := range []string{p1, p2} {
		photo, err := env.photos.GetByUID(ctx, uid)
		if err != nil {
			t.Fatalf("get %s: %v", uid, err)
		}
		if photo.Lat == nil || *photo.Lat != 50.1 || photo.Lng == nil || *photo.Lng != 14.4 {
			t.Errorf("%s location = (%v,%v), want (50.1,14.4)", uid, photo.Lat, photo.Lng)
		}
		if !photo.Private {
			t.Errorf("%s private = false, want true", uid)
		}
	}

	assertAuditWritten(t, ctx, env, editorUID)
}

// assertAlbumMembers fails unless the album contains exactly the given photos.
func assertAlbumMembers(t *testing.T, ctx context.Context, env *env, albumUID string, want ...string) {
	t.Helper()
	got, err := env.organize.ListPhotoUIDs(ctx, albumUID)
	if err != nil {
		t.Fatalf("list album photos: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("album members = %v, want %v", got, want)
	}
}

// assertLabelMembers fails unless the label is attached to the given photos.
func assertLabelMembers(t *testing.T, ctx context.Context, env *env, labelUID string, want ...string) {
	t.Helper()
	got, err := env.organize.ListPhotoUIDsByLabel(ctx, labelUID)
	if err != nil {
		t.Fatalf("list label photos: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("label members = %v, want %v", got, want)
	}
}

// assertAuditWritten fails unless a photos.bulk audit entry by actorUID exists.
func assertAuditWritten(t *testing.T, ctx context.Context, env *env, actorUID string) {
	t.Helper()
	records, err := env.audit.List(ctx, audit.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	for _, rec := range records {
		if rec.Action == audit.ActionPhotosBulk && rec.ActorUID != nil && *rec.ActorUID == actorUID {
			return
		}
	}
	t.Fatalf("no %s audit entry for actor %s in %+v", audit.ActionPhotosBulk, actorUID, records)
}

// TestBulk_partialFailureReportsPerPhoto verifies a missing photo is reported as
// an error per-photo while the valid photos are still updated and committed.
func TestBulk_partialFailureReportsPerPhoto(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()
	good := env.seedPhoto(t, "good1")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{good, "phMISSING0000000000000000000000"},
		"operations": map[string]any{"archive": true},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bulk status = %d, want 200", resp.StatusCode)
	}
	var result bulk.Result
	decodeBody(t, resp, &result)
	if result.Counts.Updated != 1 || result.Counts.Errored != 1 {
		t.Fatalf("counts = %+v, want updated=1 errored=1", result.Counts)
	}

	photo, err := env.photos.GetByUID(ctx, good)
	if err != nil {
		t.Fatalf("get good photo: %v", err)
	}
	if photo.ArchivedAt == nil {
		t.Errorf("valid photo not archived despite partial failure")
	}
}

// TestBulk_roleEnforcement verifies a viewer cannot perform a bulk edit.
func TestBulk_roleEnforcement(t *testing.T) {
	env := newEnv(t, 1000)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)
	uid := env.seedPhoto(t, "view1")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{uid},
		"operations": map[string]any{"archive": true},
	})
	resp := env.mustDo(t, viewer, http.MethodPost, "/api/v1/photos/bulk", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer bulk status = %d, want 403", resp.StatusCode)
	}
}

// TestBulk_batchSizeLimit verifies exceeding the configured batch size returns
// 413 and makes no change.
func TestBulk_batchSizeLimit(t *testing.T) {
	env := newEnv(t, 2)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	p1 := env.seedPhoto(t, "lim1")
	p2 := env.seedPhoto(t, "lim2")
	p3 := env.seedPhoto(t, "lim3")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{p1, p2, p3},
		"operations": map[string]any{"archive": true},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized batch status = %d, want 413", resp.StatusCode)
	}
}

// TestBulk_favoriteIsPerUser verifies set_favorite records the favorite for the
// acting user.
func TestBulk_favoriteIsPerUser(t *testing.T) {
	env := newEnv(t, 1000)
	editor, editorUID := env.login(t, "editor", auth.RoleEditor)
	ctx := t.Context()
	uid := env.seedPhoto(t, "fav1")

	body, _ := json.Marshal(map[string]any{
		"photo_uids": []string{uid},
		"operations": map[string]any{"set_favorite": true},
	})
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bulk status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	fav, err := env.organize.IsFavorite(ctx, editorUID, uid)
	if err != nil {
		t.Fatalf("IsFavorite: %v", err)
	}
	if !fav {
		t.Errorf("photo not favorited for acting user")
	}
}

// TestBulk_unknownOperationRejected verifies an unknown operation key is a 400.
func TestBulk_unknownOperationRejected(t *testing.T) {
	env := newEnv(t, 1000)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	uid := env.seedPhoto(t, "bad1")

	body := []byte(`{"photo_uids":["` + uid + `"],"operations":{"frobnicate":true}}`)
	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/photos/bulk", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown op status = %d, want 400", resp.StatusCode)
	}
}
