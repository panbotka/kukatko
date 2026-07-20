//go:build integration

package jobsapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/jobsapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and jobs APIs behind an httptest server over the
// integration database.
type env struct {
	baseURL string
	authSvc *auth.Service
	store   *jobs.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	store := jobs.NewStore(db.Pool())
	api := jobsapi.NewAPI(jobsapi.Config{Store: store, RequireMaintainer: authAPI.RequireMaintainer})

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{baseURL: server.URL, authSvc: authSvc, store: store}
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

// enqueue inserts a job of jobType for photo uid and returns it.
func (e *env) enqueue(t *testing.T, jobType, uid string, opts jobs.EnqueueOptions) jobs.Job {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"photo_uid": uid})
	job, err := e.store.Enqueue(t.Context(), jobType, raw, opts)
	if err != nil {
		t.Fatalf("enqueue %s/%s: %v", jobType, uid, err)
	}
	return job
}

// deadLetter enqueues a single-attempt job and exhausts it so it is immediately
// dead-lettered, returning the dead job. It is enqueued at high priority so the
// claim picks exactly this job even when other queued jobs exist.
func (e *env) deadLetter(t *testing.T, uid string) jobs.Job {
	t.Helper()
	job := e.enqueue(t, jobs.TypeImageEmbed, uid, jobs.EnqueueOptions{MaxAttempts: 1, Priority: 100})
	claimed, err := e.store.Claim(t.Context(), "w", jobs.TypeImageEmbed)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claim returned job %d, want %d", claimed.ID, job.ID)
	}
	dead, err := e.store.Fail(t.Context(), claimed.ID, "w", errors.New("boom"))
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
	if dead.State != jobs.StateDead {
		t.Fatalf("setup: job state = %q, want dead", dead.State)
	}
	return dead
}

// TestStatsEndpoint verifies GET /jobs/stats returns per-state, per-type and
// total counts, and requires the maintainer role (jobs are an operations surface).
func TestStatsEndpoint(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)
	env.enqueue(t, jobs.TypeImageEmbed, "a", jobs.EnqueueOptions{})
	env.enqueue(t, jobs.TypeImageEmbed, "b", jobs.EnqueueOptions{})
	env.enqueue(t, jobs.TypeFaceDetect, "a", jobs.EnqueueOptions{})

	resp := do(t, maint, http.MethodGet, env.baseURL+"/api/v1/jobs/stats", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d, want 200", resp.StatusCode)
	}
	var stats struct {
		ByState map[string]int `json:"by_state"`
		ByType  map[string]int `json:"by_type"`
		Total   int            `json:"total"`
	}
	decode(t, resp, &stats)
	if stats.Total != 3 || stats.ByState["queued"] != 3 {
		t.Errorf("stats = %+v, want total 3 / queued 3", stats)
	}
	if stats.ByType[jobs.TypeImageEmbed] != 2 || stats.ByType[jobs.TypeFaceDetect] != 1 {
		t.Errorf("byType = %+v, want image_embed 2 / face_detect 1", stats.ByType)
	}
}

// TestStatsForbiddenBelowMaintainer verifies queue stats are an operations
// surface reserved to maintainers: every lesser role — including a plain admin —
// is refused. This pins the maintainer/admin split, where admin (governance) does
// not carry operations.
func TestStatsForbiddenBelowMaintainer(t *testing.T) {
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
			resp := do(t, client, http.MethodGet, env.baseURL+"/api/v1/jobs/stats", nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("stats status for %s = %d, want 403", tc.role, resp.StatusCode)
			}
		})
	}
}

// TestListEndpoint verifies GET /jobs lists jobs and filters by state.
func TestListEndpoint(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)
	env.enqueue(t, jobs.TypeImageEmbed, "a", jobs.EnqueueOptions{})
	env.enqueue(t, jobs.TypeImageEmbed, "b", jobs.EnqueueOptions{})
	env.deadLetter(t, "c")

	all := listJobs(t, maint, env.baseURL+"/api/v1/jobs")
	if len(all) != 3 {
		t.Errorf("unfiltered list len = %d, want 3", len(all))
	}
	dead := listJobs(t, maint, env.baseURL+"/api/v1/jobs?state=dead")
	if len(dead) != 1 || dead[0].State != jobs.StateDead {
		t.Errorf("dead list = %+v, want one dead job", dead)
	}

	// An invalid filter is rejected with 400.
	resp := do(t, maint, http.MethodGet, env.baseURL+"/api/v1/jobs?state=bogus", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid state status = %d, want 400", resp.StatusCode)
	}
}

// TestRequeueEndpoint verifies POST /jobs/{id}/requeue revives a dead job and
// answers 404/409 for missing and non-requeueable jobs.
func TestRequeueEndpoint(t *testing.T) {
	env := newEnv(t)
	maint := env.login(t, "maint", auth.RoleMaintainer)
	dead := env.deadLetter(t, "c")

	resp := do(t, maint, http.MethodPost,
		env.baseURL+"/api/v1/jobs/"+strconv.FormatInt(dead.ID, 10)+"/requeue", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("requeue status = %d, want 200", resp.StatusCode)
	}
	var revived jobs.Job
	decode(t, resp, &revived)
	if revived.State != jobs.StateQueued || revived.Attempts != 0 {
		t.Errorf("requeued job = %+v, want queued/attempts 0", revived)
	}

	// Missing job -> 404.
	missing := do(t, maint, http.MethodPost, env.baseURL+"/api/v1/jobs/999999/requeue", nil)
	defer func() { _ = missing.Body.Close() }()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("requeue missing status = %d, want 404", missing.StatusCode)
	}

	// Now-queued job is not requeueable -> 409.
	conflict := do(t, maint, http.MethodPost,
		env.baseURL+"/api/v1/jobs/"+strconv.FormatInt(dead.ID, 10)+"/requeue", nil)
	defer func() { _ = conflict.Body.Close() }()
	if conflict.StatusCode != http.StatusConflict {
		t.Errorf("requeue queued status = %d, want 409", conflict.StatusCode)
	}
}

// listJobs GETs url with the given client and returns the decoded jobs slice.
func listJobs(t *testing.T, client *http.Client, url string) []jobs.Job {
	t.Helper()
	resp := do(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Jobs []jobs.Job `json:"jobs"`
	}
	decode(t, resp, &body)
	return body.Jobs
}

// decode reads resp's JSON body into v.
func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decoding response: %v", err)
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
