//go:build integration

package rekeyapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/rekeyapi"
	"github.com/panbotka/kukatko/internal/storage"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// noJobs drops the rewrites Finish schedules; these tests never finish a run.
type noJobs struct{}

func (noJobs) EnqueueFamilyExport(context.Context, string) error { return nil }
func (noJobs) EnqueueSidecar(context.Context, string) error      { return nil }

// env is the rekey API and the auth API behind an httptest server over the
// integration database and a store in a temporary directory.
type env struct {
	baseURL string
	db      *database.DB
	authSvc *auth.Service
}

// newEnv builds the server over a freshly truncated database, mounted the way
// the serve command mounts it: once, instance-level, under /api/v1.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	mover := rekey.New(rekey.Config{
		Pool: db.Pool(), Store: store, Jobs: noJobs{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	api := rekeyapi.NewAPI(rekeyapi.Config{Mover: mover, RequireMaintainer: authAPI.RequireMaintainer})

	r := chi.NewRouter()
	r.Use(clientip.Middleware(nil))
	r.Route("/api/v1", api.RegisterRoutes)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{baseURL: server.URL, db: db, authSvc: authSvc}
}

// user creates username with role and returns it with an API token.
func (e *env) user(t *testing.T, username string, role auth.AccountRole) (auth.User, string) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	_, token, err := e.authSvc.CreateAPIToken(t.Context(), user, auth.CreateAPITokenInput{Name: "test"},
		audit.Entry{ActorUID: user.UID, Action: audit.ActionAPITokenCreate, TargetType: "api_tokens"})
	if err != nil {
		t.Fatalf("CreateAPIToken(%s): %v", username, err)
	}
	return user, token
}

// response is one answer: its status, its run (when it is one) and its error
// code (when it is a refusal).
type response struct {
	status int
	run    rekey.Run
	code   string
}

// do sends method path as token and decodes the answer.
func (e *env) do(t *testing.T, token, method, path string) response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.baseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, path, err)
	}
	out := response{status: resp.StatusCode}
	var refusal struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &refusal) == nil && refusal.Error != "" {
		out.code = refusal.Error
		return out
	}
	_ = json.Unmarshal(data, &out.run)
	return out
}

// expect fails the test unless method path answers status (and, for a refusal,
// code), returning the answer.
func (e *env) expect(t *testing.T, token, method, path string, status int, code string) response {
	t.Helper()
	got := e.do(t, token, method, path)
	if got.status != status || got.code != code {
		t.Fatalf("%s %s = %d %q, want %d %q", method, path, got.status, got.code, status, code)
	}
	return got
}

// count runs a count(*) query.
func (e *env) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.Pool().QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("counting %q: %v", sql, err)
	}
	return n
}

// path renders a rekey route of the bootstrap project.
func path(suffix string) string {
	return "/api/v1/projects/" + project.BootstrapUID + "/rekey" + suffix
}

func TestRekeyAPI_onlyTheSystemAdministrator(t *testing.T) {
	e := newEnv(t)
	_, admin := e.user(t, "padmin", auth.AccountAdmin)
	for _, call := range []struct{ method, suffix string }{
		{http.MethodGet, ""}, {http.MethodPost, ""}, {http.MethodPost, "/pause"},
		{http.MethodPost, "/resume"}, {http.MethodPost, "/retry"},
	} {
		if got := e.do(t, admin, call.method, path(call.suffix)); got.status != http.StatusForbidden {
			t.Errorf("project admin %s %s = %d, want 403", call.method, call.suffix, got.status)
		}
		if got := e.do(t, "", call.method, path(call.suffix)); got.status != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", call.method, call.suffix, got.status)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM storage_rekey_runs`); n != 0 {
		t.Errorf("refused calls left %d runs", n)
	}
}

func TestRekeyAPI_lifecycle(t *testing.T) {
	e := newEnv(t)
	maint, token := e.user(t, "maint", auth.AccountMaintainer)

	e.expect(t, token, http.MethodGet, path(""), http.StatusNotFound, "not_found")
	e.expect(t, token, http.MethodPost, path("/pause"), http.StatusNotFound, "not_found")

	started := e.expect(t, token, http.MethodPost, path(""), http.StatusCreated, "")
	if started.run.State != rekey.StateRunning || started.run.ProjectUID != project.BootstrapUID {
		t.Fatalf("started run = %+v, want running for the bootstrap project", started.run)
	}
	if n := e.count(t, `SELECT count(*) FROM jobs WHERE type = 'storage_rekey' AND state = 'queued'`); n != 1 {
		t.Errorf("queued storage_rekey jobs after the start = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND actor_uid = $2 AND project_id = $3`,
		audit.ActionProjectRekeyStart, maint.UID, project.BootstrapUID); n != 1 {
		t.Errorf("start audit entries by the maintainer = %d, want 1", n)
	}
	e.expect(t, token, http.MethodPost, path(""), http.StatusConflict, "already_running")
	e.expect(t, token, http.MethodPost, path("/resume"), http.StatusConflict, "invalid_state")
	e.expect(t, token, http.MethodPost, path("/retry"), http.StatusConflict, "invalid_state")

	if got := e.expect(t, token, http.MethodPost, path("/pause"), http.StatusOK, ""); got.run.State != rekey.StatePaused {
		t.Errorf("paused run = %s, want paused", got.run.State)
	}
	e.expect(t, token, http.MethodPost, path(""), http.StatusConflict, "already_running")
	e.expect(t, token, http.MethodPost, path("/pause"), http.StatusConflict, "invalid_state")
	if got := e.expect(t, token, http.MethodGet, path(""), http.StatusOK, ""); got.run.UID != started.run.UID {
		t.Errorf("GET run = %s, want %s", got.run.UID, started.run.UID)
	}
	if got := e.expect(t, token, http.MethodPost, path("/resume"), http.StatusOK, ""); got.run.State != rekey.StateRunning {
		t.Errorf("resumed run = %s, want running", got.run.State)
	}

	if _, err := e.db.Pool().Exec(t.Context(), `UPDATE storage_rekey_runs SET state = 'failed'`); err != nil {
		t.Fatalf("failing the run: %v", err)
	}
	if got := e.expect(t, token, http.MethodPost, path("/retry"), http.StatusOK, ""); got.run.State != rekey.StateRunning {
		t.Errorf("retried run = %s, want running", got.run.State)
	}
	for _, action := range []string{
		audit.ActionProjectRekeyPause, audit.ActionProjectRekeyResume, audit.ActionProjectRekeyRetry,
	} {
		if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = $1 AND project_id = $2`,
			action, project.BootstrapUID); n != 1 {
			t.Errorf("%s audit entries = %d, want 1", action, n)
		}
	}
}

func TestRekeyAPI_refusesAProjectWithItsOwnPrefix(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "maint", auth.AccountMaintainer)
	created, err := project.NewStore(e.db.Pool()).Create(t.Context(), audit.Meta{},
		project.CreateParams{Slug: "hasici", Name: "Hasiči"})
	if err != nil {
		t.Fatalf("creating a project: %v", err)
	}
	e.expect(t, token, http.MethodPost, "/api/v1/projects/"+created.UID+"/rekey", http.StatusConflict,
		"already_prefixed")
	e.expect(t, token, http.MethodPost, "/api/v1/projects/pjnosuchprojectatall00000/rekey", http.StatusNotFound,
		"not_found")
}
