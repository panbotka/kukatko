//go:build integration

package importapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importapi"
	"github.com/panbotka/kukatko/internal/importer"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.
//
// What they guard: the PhotoPrism/photo-sorter migration was removed in August
// 2026 and with it every trigger this API used to carry. The two read endpoints
// stayed, because `kukatko import dir` keeps writing runs and failures and the
// finished migration's rows are the catalogue's provenance record. A removal
// that took the whole package with it would be silent otherwise.

const testPassword = "correct horse battery staple"

// env wires the auth and import APIs behind an httptest server over the
// integration database, with a real import-run store.
type env struct {
	baseURL string
	authSvc *auth.Service
	runs    *importer.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	runStore := importer.NewStore(db.Pool())
	api := importapi.NewAPI(importapi.Config{
		Runs:              runStore,
		Failures:          runStore,
		RequireMaintainer: authAPI.RequireMaintainer,
	})

	r := chi.NewRouter()
	// Mirrors the real server: a forwarding header is only believed from a
	// trusted proxy, and this harness trusts none.
	r.Use(clientip.Middleware(nil))
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{baseURL: server.URL, authSvc: authSvc, runs: runStore}
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

// runsBody is the JSON shape of GET /import/runs.
type runsBody struct {
	Runs   []importer.Run `json:"runs"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// failuresBody is the JSON shape of GET /import/failures.
type failuresBody struct {
	Failures []importer.Failure `json:"failures"`
	Limit    int                `json:"limit"`
	Offset   int                `json:"offset"`
}

// TestImportRuns_servesFolderAndMigrationHistory verifies the run history still
// answers over HTTP and returns both what still writes it (a `kukatko import
// dir` run) and what no longer does (the finished PhotoPrism migration), most
// recently started first.
func TestImportRuns_servesFolderAndMigrationHistory(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)
	ctx := t.Context()

	// A finished migration run: nothing writes this source any more, but the row
	// is the catalogue's provenance record and must keep coming back.
	migration, err := env.runs.Start(ctx, importer.SourcePhotoPrism)
	if err != nil {
		t.Fatalf("start migration run: %v", err)
	}
	watermark := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	if err := env.runs.Complete(ctx, migration.ID, &watermark,
		importer.Counts{Imported: 20647, Deduplicated: 13}); err != nil {
		t.Fatalf("complete migration run: %v", err)
	}

	// A folder import: the only source still produced.
	folder, err := env.runs.Start(ctx, importer.SourceFolder)
	if err != nil {
		t.Fatalf("start folder run: %v", err)
	}
	if err := env.runs.Complete(ctx, folder.ID, nil, importer.Counts{Imported: 3, Skipped: 1}); err != nil {
		t.Fatalf("complete folder run: %v", err)
	}

	resp := do(t, maint, http.MethodGet, env.baseURL+"/api/v1/import/runs", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got runsBody
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got.Runs) != 2 {
		t.Fatalf("runs = %+v, want 2", got.Runs)
	}
	if got.Runs[0].Source != importer.SourceFolder || got.Runs[0].Counts.Imported != 3 {
		t.Errorf("newest run = %+v, want the folder import", got.Runs[0])
	}
	if got.Runs[1].Source != importer.SourcePhotoPrism || got.Runs[1].Counts.Deduplicated != 13 {
		t.Errorf("older run = %+v, want the photoprism migration", got.Runs[1])
	}
	if got.Runs[1].HighWatermark == nil || !got.Runs[1].HighWatermark.Equal(watermark) {
		t.Errorf("migration watermark = %v, want %v", got.Runs[1].HighWatermark, watermark)
	}
	if got.Limit != 50 || got.Offset != 0 {
		t.Errorf("paging = (%d,%d), want the defaults (50,0)", got.Limit, got.Offset)
	}
}

// TestImportFailures_servesFolderFailures verifies the failures listing still
// answers over HTTP, filters by source, and marks the run `partial`.
func TestImportFailures_servesFolderFailures(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)
	ctx := t.Context()

	run, err := env.runs.Start(ctx, importer.SourceFolder)
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if err := env.runs.RecordFailures(ctx, []importer.Failure{
		importer.NewFailure(run.ID, importer.SourceFolder, importer.StagePhoto, "",
			"/srv/incoming/beach.jpg", "", errBoom{}),
	}); err != nil {
		t.Fatalf("RecordFailures: %v", err)
	}
	if err := env.runs.Complete(ctx, run.ID, nil, importer.Counts{Imported: 2, Failed: 1}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	resp := do(t, maint, http.MethodGet,
		env.baseURL+"/api/v1/import/failures?source=folder&unresolved=true", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got failuresBody
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got.Failures) != 1 {
		t.Fatalf("failures = %+v, want 1", got.Failures)
	}
	if got.Failures[0].SourceRef != "/srv/incoming/beach.jpg" || got.Failures[0].Stage != importer.StagePhoto {
		t.Errorf("failure = %+v, want the recorded photo-stage defect", got.Failures[0])
	}

	// A run with an unresolved failure closes as partial, not done.
	stored, err := env.runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Status != importer.StatusPartial {
		t.Errorf("run status = %q, want partial", stored.Status)
	}
}

// TestImportAPI_hasNoTriggers verifies the retired migration triggers and the
// completeness check are gone from a fully wired server, not merely unwired: a
// maintainer — the role that used to be allowed to run them — gets 404.
func TestImportAPI_hasNoTriggers(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)

	for _, path := range []string{"/import/photoprism", "/import/photosorter", "/import/photosorter-feeds"} {
		resp := do(t, maint, http.MethodPost, env.baseURL+"/api/v1"+path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s status = %d, want 404", path, resp.StatusCode)
		}
	}
	verify := do(t, maint, http.MethodGet, env.baseURL+"/api/v1/import/verify", nil)
	defer func() { _ = verify.Body.Close() }()
	if verify.StatusCode != http.StatusNotFound {
		t.Errorf("GET /import/verify status = %d, want 404", verify.StatusCode)
	}
}

// TestImportRuns_requiresMaintainer verifies both read endpoints stay behind the
// maintainer guard: import bookkeeping is an operations surface.
func TestImportRuns_requiresMaintainer(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)

	for _, path := range []string{"/import/runs", "/import/failures"} {
		resp := do(t, admin, http.MethodGet, env.baseURL+"/api/v1"+path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("admin GET %s status = %d, want 403", path, resp.StatusCode)
		}
	}
}

// errBoom is a fixed error used as the cause of a recorded import failure.
type errBoom struct{}

// Error returns the fixed message.
func (errBoom) Error() string { return "decode failed" }

// do issues a request with the given method and optional JSON body.
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
