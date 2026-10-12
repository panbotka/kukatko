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
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clientip"
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
	photos *photos.Store
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
	photoStore := photos.NewStore(db.Pool())
	ingestSvc := ingest.New(ingest.Config{
		Storage:     fs,
		Photos:      photoStore,
		Thumbnailer: thumb.New(fs, t.TempDir()),
		Duplicate:   config.DuplicateConfig{Enabled: false},
		TempDir:     t.TempDir(),
	})
	ingestAPI := ingest.NewAPI(ingestSvc, authAPI.RequireAuth, nil)

	r := chi.NewRouter()
	// Mirrors the real server: a forwarding header is only believed from a
	// trusted proxy, and this harness trusts none.
	r.Use(clientip.Middleware(nil))
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		ingestAPI.RegisterRoutes(r)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &httpEnv{server: server, svc: authSvc, photos: photoStore}
}

// loginClient creates a user with the given role and returns an HTTP client
// carrying its authenticated session cookie.
func (e *httpEnv) loginClient(t *testing.T, username string, role auth.AccountRole) *http.Client {
	t.Helper()
	client, _ := e.loginUser(t, username, role)
	return client
}

// loginUser is loginClient that also returns the created user's UID, for a test
// that checks what the upload was attributed to.
func (e *httpEnv) loginUser(t *testing.T, username string, role auth.AccountRole) (*http.Client, string) {
	t.Helper()
	const password = "correct horse battery staple"
	user, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: password, Role: role,
	})
	if err != nil {
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
	return client, user.UID
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
	client := env.loginClient(t, "editor", auth.AccountEditor)

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

// TestHTTPUpload_curatorCreatesPhotos verifies a curator may add photos to the
// library, not only file them.
func TestHTTPUpload_curatorCreatesPhotos(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "curator", auth.AccountCurator)

	status, results := env.uploadFiles(t, client, map[string][]byte{
		"green.jpg": jpegBytes(t, 30, 210, 30, 90),
	})
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200 for curator", status)
	}
	if len(results) != 1 || results[0].Outcome != ingest.OutcomeCreated || results[0].PhotoUID == "" {
		t.Errorf("results = %+v, want one created photo with a UID", results)
	}
}

// TestHTTPUpload_refusesFilesThatAreNotMedia verifies the signed-in upload
// applies the same content check as the public upload link: a file that only
// names itself a photo comes back as a per-file 415 not_media beside a real
// photo in the same batch, which is still created.
func TestHTTPUpload_refusesFilesThatAreNotMedia(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "editor", auth.AccountEditor)

	status, results := env.uploadFiles(t, client, map[string][]byte{
		"broken.jpg": []byte(strings.Repeat("definitely not a jpeg ", 200)),
		"fine.jpg":   jpegBytes(t, 120, 30, 200, 90),
	})
	if status != http.StatusOK || len(results) != 2 {
		t.Fatalf("upload = %d with %d results, want 200 with 2", status, len(results))
	}
	for _, res := range results {
		switch res.Filename {
		case "broken.jpg":
			if res.Status != http.StatusUnsupportedMediaType || res.Code != ingest.CodeNotMedia || res.PhotoUID != "" {
				t.Errorf("broken.jpg = %+v, want a 415 %q refusal", res, ingest.CodeNotMedia)
			}
		case "fine.jpg":
			if res.Outcome != ingest.OutcomeCreated {
				t.Errorf("fine.jpg = %+v, want created", res)
			}
		}
	}
}

// TestHTTPUpload_refusesAVIFAndTruncatedImages verifies the signed-in upload
// refuses an AVIF as unsupported_type — it used to be created and then fail
// every thumbnail and job — and a truncated JPEG as damaged, beside a real photo
// in the same batch that is still created.
func TestHTTPUpload_refusesAVIFAndTruncatedImages(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "editor", auth.AccountEditor)

	whole := jpegBytes(t, 60, 160, 30, 90)
	status, results := env.uploadFiles(t, client, map[string][]byte{
		"f.avif":    append([]byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf"), make([]byte, 600)...),
		"trunc.jpg": whole[:len(whole)/2],
		"fine.jpg":  jpegBytes(t, 120, 30, 200, 90),
	})
	if status != http.StatusOK || len(results) != 3 {
		t.Fatalf("upload = %d with %d results, want 200 with 3", status, len(results))
	}
	want := map[string]string{"f.avif": ingest.CodeUnsupportedType, "trunc.jpg": ingest.CodeDamaged}
	for _, res := range results {
		code, refused := want[res.Filename]
		switch {
		case refused && (res.Status != http.StatusUnsupportedMediaType || res.Code != code || res.PhotoUID != ""):
			t.Errorf("%s = %+v, want a 415 %q refusal", res.Filename, res, code)
		case !refused && res.Outcome != ingest.OutcomeCreated:
			t.Errorf("%s = %+v, want created", res.Filename, res)
		}
	}
}

// TestHTTPUpload_viewerCreatesPhotos verifies the upload hangs on RequireAuth:
// a viewer, the lowest signed-in role, may add photos to the library, and the
// photo is attributed to that viewer.
func TestHTTPUpload_viewerCreatesPhotos(t *testing.T) {
	env := newHTTPEnv(t)
	client, viewerUID := env.loginUser(t, "viewer", auth.AccountViewer)

	status, results := env.uploadFiles(t, client, map[string][]byte{
		"x.jpg": jpegBytes(t, 1, 2, 3, 80),
	})
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200 for viewer", status)
	}
	if len(results) != 1 || results[0].Outcome != ingest.OutcomeCreated || results[0].PhotoUID == "" {
		t.Fatalf("results = %+v, want one created photo with a UID", results)
	}
	photo, err := env.photos.GetByUID(t.Context(), results[0].PhotoUID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if photo.UploadedBy == nil || *photo.UploadedBy != viewerUID {
		t.Errorf("uploaded_by = %v, want the viewer %s", photo.UploadedBy, viewerUID)
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
