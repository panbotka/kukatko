//go:build integration

package ingest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// httpEnv wires the auth and ingest APIs behind an httptest server so the
// upload endpoint can be exercised end to end (multipart streaming + RBAC).
type httpEnv struct {
	server *httptest.Server
	svc    *auth.Service
}

// newHTTPEnv builds the HTTP test environment over the integration database.
func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(50, time.Minute)})

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	ingestSvc := ingest.New(ingest.Config{
		Storage:     fs,
		Photos:      photos.NewStore(db.Pool()),
		Thumbnailer: thumb.New(fs, t.TempDir()),
		Duplicate:   config.DuplicateConfig{Enabled: false},
		TempDir:     t.TempDir(),
	})
	ingestAPI := ingest.NewAPI(ingestSvc, authAPI.RequireWrite, nil)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		ingestAPI.RegisterRoutes(r)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &httpEnv{server: server, svc: authSvc}
}

// loginClient creates a user with the given role and returns an HTTP client
// carrying its authenticated session cookie.
func (e *httpEnv) loginClient(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	const password = "correct horse battery staple"
	if _, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: password, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
		e.server.URL+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// uploadFiles posts the given named files as one multipart request and returns
// the HTTP status and decoded results.
func (e *httpEnv) uploadFiles(
	t *testing.T, client *http.Client, files map[string][]byte,
) (int, []ingest.FileResult) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, data := range files {
		part, err := writer.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("writing part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing writer: %v", err)
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
		e.server.URL+"/api/v1/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	var decoded struct {
		Results []ingest.FileResult `json:"results"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decoding response %q: %v", data, err)
	}
	return resp.StatusCode, decoded.Results
}

// TestHTTPUpload_editorCreatesPhotos verifies an editor can upload a multi-file
// batch and receives a per-file result list with mixed outcomes.
func TestHTTPUpload_editorCreatesPhotos(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "editor", auth.RoleEditor)

	red := jpegBytes(t, 210, 30, 30, 90)
	blue := jpegBytes(t, 30, 30, 210, 90)
	status, results := env.uploadFiles(t, client, map[string][]byte{
		"red.jpg": red, "blue.jpg": blue,
	})
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, res := range results {
		if res.Outcome != ingest.OutcomeCreated || res.PhotoUID == "" {
			t.Errorf("result %+v, want created with a UID", res)
		}
	}
}

// TestHTTPUpload_viewerForbidden verifies a viewer (no write access) is rejected
// with 403 by the RequireWrite guard before any ingest happens.
func TestHTTPUpload_viewerForbidden(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "viewer", auth.RoleViewer)

	status, _ := env.uploadFiles(t, client, map[string][]byte{
		"x.jpg": jpegBytes(t, 1, 2, 3, 80),
	})
	if status != http.StatusForbidden {
		t.Errorf("upload status = %d, want 403 for viewer", status)
	}
}

// TestHTTPUpload_unauthenticated verifies an anonymous upload is rejected 401.
func TestHTTPUpload_unauthenticated(t *testing.T) {
	env := newHTTPEnv(t)
	status, _ := env.uploadFiles(t, &http.Client{}, map[string][]byte{
		"x.jpg": jpegBytes(t, 1, 2, 3, 80),
	})
	if status != http.StatusUnauthorized {
		t.Errorf("upload status = %d, want 401 for anonymous", status)
	}
}
