//go:build integration

package systemapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/systemapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// offlineHealth is an EmbeddingHealth that reports the box offline, exercising
// the "box offline -> embeddings queued" state without any network traffic.
type offlineHealth struct{}

// Healthy always reports offline.
func (offlineHealth) Healthy(context.Context) bool { return false }

// env wires the auth and system APIs behind an httptest server over the
// integration database, with a real job queue and import-run store.
type env struct {
	baseURL string
	authSvc *auth.Service
	jobs    *jobs.Store
	runs    *importer.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database. The
// originals directory holds one file so the storage measurement is non-zero, and
// the backup reporter is left nil to exercise the not-configured path.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	jobStore := jobs.NewStore(db.Pool())
	runStore := importer.NewStore(db.Pool())

	originals := t.TempDir()
	if err := os.WriteFile(filepath.Join(originals, "a.bin"), make([]byte, 256), 0o644); err != nil {
		t.Fatalf("seed originals: %v", err)
	}

	svc := system.New(system.Config{
		DB:            db,
		Embeddings:    offlineHealth{},
		EmbeddingURL:  "http://box:8000",
		Jobs:          jobStore,
		Backup:        nil,
		Imports:       runStore,
		OriginalsPath: originals,
		CachePath:     filepath.Join(originals, "missing-cache"),
	})
	api := systemapi.NewAPI(systemapi.Config{Service: svc, RequireMaintainer: authAPI.RequireMaintainer})

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{baseURL: server.URL, authSvc: authSvc, jobs: jobStore, runs: runStore}
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
	resp := do(t, client, http.MethodPost, e.baseURL+"/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// TestSystemStatus_Aggregates verifies the endpoint folds the real job counts,
// the latest import run per source, the storage usage and the offline-box state
// into one snapshot.
func TestSystemStatus_Aggregates(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)
	ctx := t.Context()

	// Two queued image_embed jobs (pending embedding work) and one dead-lettered.
	if _, err := env.jobs.Enqueue(ctx, jobs.TypeImageEmbed,
		[]byte(`{"photo_uid":"a"}`), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue a: %v", err)
	}
	if _, err := env.jobs.Enqueue(ctx, jobs.TypeImageEmbed,
		[]byte(`{"photo_uid":"b"}`), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue b: %v", err)
	}

	// One completed PhotoPrism run is the latest for that source.
	run, err := env.runs.Start(ctx, importer.SourcePhotoPrism)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	watermark := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := env.runs.Complete(ctx, run.ID, &watermark, importer.Counts{Imported: 9}); err != nil {
		t.Fatalf("complete run: %v", err)
	}

	resp := do(t, maint, http.MethodGet, env.baseURL+"/api/v1/system/status", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got system.Status
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	if !got.Database.Reachable {
		t.Error("database not reachable, want reachable")
	}
	if got.Embeddings.Online {
		t.Error("embeddings online, want offline")
	}
	if got.Jobs.Total != 2 || got.Jobs.ByState["queued"] != 2 || got.Jobs.PendingEmbeddings != 2 {
		t.Errorf("jobs = %+v, want total 2 / queued 2 / pending 2", got.Jobs)
	}
	if got.Backup.Configured {
		t.Error("backup configured, want not configured")
	}
	if got.Imports.PhotoPrism == nil || got.Imports.PhotoPrism.Counts.Imported != 9 {
		t.Errorf("imports.photoprism = %+v, want imported 9", got.Imports.PhotoPrism)
	}
	if got.Imports.PhotoSorter != nil {
		t.Errorf("imports.photosorter = %+v, want nil", got.Imports.PhotoSorter)
	}
	if got.Storage.OriginalsBytes != 256 {
		t.Errorf("storage.originals = %d, want 256", got.Storage.OriginalsBytes)
	}
	if got.Storage.TotalBytes <= 0 {
		t.Errorf("storage.total = %d, want positive", got.Storage.TotalBytes)
	}
	if got.Version.Version == "" {
		t.Error("version empty, want a build version")
	}
}

// TestSystemStatus_ForbiddenBelowMaintainer verifies the status dashboard is an
// operations surface reserved to maintainers: every lesser role — including a
// plain admin — is refused, pinning the maintainer/admin split.
func TestSystemStatus_ForbiddenBelowMaintainer(t *testing.T) {
	env := newEnv(t)
	for _, tc := range []struct {
		user string
		role auth.Role
	}{
		{"viewer", auth.RoleViewer},
		{"editor", auth.RoleEditor},
		{"admin", auth.RoleAdmin},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			client := env.login(t, tc.user, tc.role)
			resp := do(t, client, http.MethodGet, env.baseURL+"/api/v1/system/status", nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("status for %s = %d, want 403", tc.role, resp.StatusCode)
			}
		})
	}
}

// do issues an HTTP request with the optional JSON body and returns the response.
func do(t *testing.T, client *http.Client, method, url string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}
