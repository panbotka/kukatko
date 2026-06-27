//go:build integration

package organizeapi_test

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

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and organize APIs behind an httptest server over the
// integration database, plus the photos store used to seed real photo rows.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	photos  *photos.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	store := organize.NewStore(db.Pool())
	api := organizeapi.NewAPI(organizeapi.Config{
		Albums:       store,
		Labels:       store,
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc, photos: photos.NewStore(db.Pool())}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
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
	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
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

// TestAlbumLifecycle exercises create, list, get, update and delete over HTTP.
func TestAlbumLifecycle(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/albums",
		[]byte(`{"title":"Léto u Řeky","description":"summer"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created organize.Album
	decodeBody(t, resp, &created)
	if created.UID == "" || created.Slug != "leto-u-reky" || created.Type != organize.AlbumManual {
		t.Fatalf("unexpected created album: %+v", created)
	}

	resp = env.mustDo(t, editor, http.MethodGet, "/api/v1/albums", nil)
	var list struct {
		Albums []organize.AlbumCount `json:"albums"`
	}
	decodeBody(t, resp, &list)
	if len(list.Albums) != 1 || list.Albums[0].UID != created.UID {
		t.Fatalf("list mismatch: %+v", list.Albums)
	}

	resp = env.mustDo(t, editor, http.MethodPatch, "/api/v1/albums/"+created.UID,
		[]byte(`{"title":"Hory","private":true,"order_by":"oldest"}`))
	var updated organize.Album
	decodeBody(t, resp, &updated)
	if updated.Title != "Hory" || updated.Slug != "hory" || !updated.Private ||
		updated.OrderBy != "oldest" || updated.Type != organize.AlbumManual {
		t.Fatalf("unexpected updated album: %+v", updated)
	}

	resp = env.mustDo(t, editor, http.MethodDelete, "/api/v1/albums/"+created.UID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = env.mustDo(t, editor, http.MethodGet, "/api/v1/albums/"+created.UID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestAlbumMembership covers add, reorder and remove of an album's photos.
func TestAlbumMembership(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)
	p1 := env.seedPhoto(t, "m1")
	p2 := env.seedPhoto(t, "m2")
	p3 := env.seedPhoto(t, "m3")

	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/albums", []byte(`{"title":"Trip"}`))
	var album organize.Album
	decodeBody(t, resp, &album)

	// Add all three; they keep insertion order.
	resp = env.mustDo(t, editor, http.MethodPost, "/api/v1/albums/"+album.UID+"/photos",
		[]byte(`{"photo_uids":["`+p1+`","`+p2+`","`+p3+`"]}`))
	var order struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	decodeBody(t, resp, &order)
	if len(order.PhotoUIDs) != 3 || order.PhotoUIDs[0] != p1 || order.PhotoUIDs[2] != p3 {
		t.Fatalf("after add order = %v, want [%s %s %s]", order.PhotoUIDs, p1, p2, p3)
	}

	// Reorder.
	resp = env.mustDo(t, editor, http.MethodPatch, "/api/v1/albums/"+album.UID+"/order",
		[]byte(`{"photo_uids":["`+p3+`","`+p1+`","`+p2+`"]}`))
	decodeBody(t, resp, &order)
	if order.PhotoUIDs[0] != p3 || order.PhotoUIDs[1] != p1 || order.PhotoUIDs[2] != p2 {
		t.Fatalf("after reorder order = %v, want [%s %s %s]", order.PhotoUIDs, p3, p1, p2)
	}

	// Remove one.
	resp = env.mustDo(t, editor, http.MethodDelete, "/api/v1/albums/"+album.UID+"/photos",
		[]byte(`{"photo_uids":["`+p1+`"]}`))
	decodeBody(t, resp, &order)
	if len(order.PhotoUIDs) != 2 || order.PhotoUIDs[0] != p3 || order.PhotoUIDs[1] != p2 {
		t.Fatalf("after remove order = %v, want [%s %s]", order.PhotoUIDs, p3, p2)
	}
}

// TestAlbumMembershipNotFound maps a missing album/photo to 404.
func TestAlbumMembershipNotFound(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/albums/al_missing/photos",
		[]byte(`{"photo_uids":["ph_x"]}`))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing album status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.mustDo(t, editor, http.MethodPost, "/api/v1/albums", []byte(`{"title":"Trip"}`))
	var album organize.Album
	decodeBody(t, resp, &album)
	resp = env.mustDo(t, editor, http.MethodPost, "/api/v1/albums/"+album.UID+"/photos",
		[]byte(`{"photo_uids":["ph_missing"]}`))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing photo status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestLabelLifecycleAndAttach covers label CRUD plus attach/detach to a photo.
func TestLabelLifecycleAndAttach(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)
	photo := env.seedPhoto(t, "lbl1")

	resp := env.mustDo(t, editor, http.MethodPost, "/api/v1/labels",
		[]byte(`{"name":"Pláž","priority":3}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create label status = %d, want 201", resp.StatusCode)
	}
	var label organize.Label
	decodeBody(t, resp, &label)
	if label.Slug != "plaz" || label.Priority != 3 {
		t.Fatalf("unexpected label: %+v", label)
	}

	// Attach to the photo.
	resp = env.mustDo(t, editor, http.MethodPost, "/api/v1/labels/"+label.UID+"/photos",
		[]byte(`{"photo_uid":"`+photo+`","source":"manual"}`))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("attach status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// The label's photo count now reflects the attachment.
	resp = env.mustDo(t, editor, http.MethodGet, "/api/v1/labels", nil)
	var list struct {
		Labels []organize.LabelCount `json:"labels"`
	}
	decodeBody(t, resp, &list)
	if len(list.Labels) != 1 || list.Labels[0].PhotoCount != 1 {
		t.Fatalf("label count mismatch: %+v", list.Labels)
	}

	// Detach.
	resp = env.mustDo(t, editor, http.MethodDelete, "/api/v1/labels/"+label.UID+"/photos",
		[]byte(`{"photo_uid":"`+photo+`"}`))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("detach status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.mustDo(t, editor, http.MethodDelete, "/api/v1/labels/"+label.UID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete label status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestRoleEnforcement verifies a viewer can read but not mutate, while an editor
// can do both.
func TestRoleEnforcement(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	editor := env.login(t, "editor", auth.RoleEditor)

	// Viewer can read.
	for _, path := range []string{"/api/v1/albums", "/api/v1/labels"} {
		resp := env.mustDo(t, viewer, http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("viewer GET %s = %d, want 200", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// Viewer cannot create.
	resp := env.mustDo(t, viewer, http.MethodPost, "/api/v1/albums", []byte(`{"title":"Trip"}`))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer POST /albums = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = env.mustDo(t, viewer, http.MethodPost, "/api/v1/labels", []byte(`{"name":"Beach"}`))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer POST /labels = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Editor can create.
	resp = env.mustDo(t, editor, http.MethodPost, "/api/v1/albums", []byte(`{"title":"Trip"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("editor POST /albums = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Unauthenticated cannot even read.
	resp = env.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/albums", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /albums = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
