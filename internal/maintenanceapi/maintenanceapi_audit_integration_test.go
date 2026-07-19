//go:build integration

package maintenanceapi_test

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
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/maintenanceapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and maintenance APIs behind an httptest server over the
// integration database. The maintenance Service is left nil (the purge does not
// need it); only the audit store is wired.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	db      *database.DB
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := maintenanceapi.NewAPI(maintenanceapi.Config{
		Audit:             audit.NewStore(db.Pool()),
		RequireMaintainer: authAPI.RequireMaintainer,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc, db: db}
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

// seedAudit inserts an audit row with an explicit created_at so a test can place
// entries on either side of a retention cutoff.
func (e *env) seedAudit(t *testing.T, action string, createdAt time.Time) {
	t.Helper()
	if _, err := e.db.Pool().Exec(context.Background(),
		"INSERT INTO audit_log (action, target_type, created_at) VALUES ($1, 'test', $2)",
		action, createdAt); err != nil {
		t.Fatalf("seeding audit %q: %v", action, err)
	}
}

// countAudit returns how many audit_log rows exist for the given action.
func (e *env) countAudit(t *testing.T, action string) int {
	t.Helper()
	n, err := audit.NewStore(e.db.Pool()).Count(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("counting audit %q: %v", action, err)
	}
	return n
}

const purgePath = "/api/v1/maintenance/audit/purge"

// TestAuditPurgeRequiresMaintainer: viewer/editor/admin are forbidden; only a
// maintainer may purge.
func TestAuditPurgeRequiresMaintainer(t *testing.T) {
	env := newEnv(t)
	body := []byte(`{"older_than_days":365}`)

	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleEditor, auth.RoleAdmin} {
		client := env.login(t, "u_"+string(role), role)
		resp := env.mustDo(t, client, http.MethodPost, purgePath, body)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("purge as %s status = %d, want 403", role, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	maintainer := env.login(t, "boss", auth.RoleMaintainer)
	resp := env.mustDo(t, maintainer, http.MethodPost, purgePath, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("maintainer purge status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestAuditPurgeRejectsBadCutoff: a maintainer sending a non-positive window is a
// 400, and nothing is deleted.
func TestAuditPurgeRejectsBadCutoff(t *testing.T) {
	env := newEnv(t)
	maintainer := env.login(t, "boss", auth.RoleMaintainer)
	env.seedAudit(t, "old.entry", time.Now().Add(-500*24*time.Hour))

	resp := env.mustDo(t, maintainer, http.MethodPost, purgePath, []byte(`{"older_than_days":0}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad-cutoff purge status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if n := env.countAudit(t, "old.entry"); n != 1 {
		t.Fatalf("old.entry rows after rejected purge = %d, want 1 (untouched)", n)
	}
}

// TestAuditPurgeDeletesOldAndSelfAudits: a maintainer purge deletes only the
// entries older than the cutoff, returns their count, and writes exactly one
// recent audit.purge self-audit record that survives.
func TestAuditPurgeDeletesOldAndSelfAudits(t *testing.T) {
	env := newEnv(t)
	maintainer := env.login(t, "boss", auth.RoleMaintainer)

	now := time.Now()
	env.seedAudit(t, "old.one", now.Add(-500*24*time.Hour))
	env.seedAudit(t, "old.two", now.Add(-400*24*time.Hour))
	env.seedAudit(t, "recent.one", now.Add(-10*24*time.Hour))

	resp := env.mustDo(t, maintainer, http.MethodPost, purgePath, []byte(`{"older_than_days":365}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge status = %d, want 200", resp.StatusCode)
	}
	var got maintenancePurgeResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode purge response: %v", err)
	}
	_ = resp.Body.Close()
	if got.Deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (old.one + old.two)", got.Deleted)
	}
	if got.OlderThanDays != 365 || got.Cutoff == "" {
		t.Errorf("response = %+v, want older_than_days 365 and a cutoff", got)
	}

	// The recent seeded row survives; the two old ones are gone.
	if n := env.countAudit(t, "recent.one"); n != 1 {
		t.Errorf("recent.one rows = %d, want 1 (kept)", n)
	}
	if n := env.countAudit(t, "old.one") + env.countAudit(t, "old.two"); n != 0 {
		t.Errorf("old rows remaining = %d, want 0 (purged)", n)
	}
	// Exactly one self-audit record for the purge, and it survived.
	if n := env.countAudit(t, audit.ActionAuditPurge); n != 1 {
		t.Fatalf("audit.purge rows = %d, want 1 (self-audit survives)", n)
	}
}

// maintenancePurgeResponse mirrors the purge endpoint's JSON body.
type maintenancePurgeResponse struct {
	Deleted       int    `json:"deleted"`
	OlderThanDays int    `json:"older_than_days"`
	Cutoff        string `json:"cutoff"`
}
