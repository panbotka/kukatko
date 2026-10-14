//go:build integration

package settingsapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/settingsapi"
)

// isolationEnv is the two seeded projects behind one server: registration and
// the instance-level public settings unprefixed, the project's settings under
// /p/{project}, both projects open for registration with secrets of their own.
type isolationEnv struct {
	f        *projecttest.Fixture
	pool     *pgxpool.Pool
	server   *httptest.Server
	settings *settings.Store
	projects *project.Store
}

// The two projects' registration secrets.
const (
	secretA = "alpha-secret"
	secretB = "beta-secret"
)

// newIsolationEnv seeds the two tenants, opens registration in each with its
// own secret and builds the server.
func newIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	e := &isolationEnv{
		f: projecttest.Seed(t, pool), pool: pool,
		settings: settings.NewStore(pool), projects: project.NewStore(pool),
	}
	e.open(t, projecttest.SideA, secretA, "# Vítej v A")
	e.open(t, projecttest.SideB, secretB, "# Vítej v B")

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{
		Service: svc, Limiter: auth.NewLimiter(100, time.Minute), Projects: e.projects,
		Registration: auth.NewRegistration(auth.RegistrationConfig{
			Service: svc, Settings: e.settings, Mail: mailjob.NewEnqueuer(mailjob.EnqueuerConfig{}),
		}),
	})
	api := settingsapi.NewAPI(settingsapi.Config{
		Store: e.settings, Usage: e.projects,
		RequireActiveProject: auth.RequireActiveProject,
		RequireAuth:          authAPI.RequireAuth, RequireAdmin: authAPI.RequireAdmin,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterInstanceRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(e.projects))
			api.RegisterRoutes(r)
		})
	})
	e.server = httptest.NewServer(r)
	t.Cleanup(e.server.Close)
	return e
}

// open switches registration on in which's project with secret and welcome,
// as that project's own member.
func (e *isolationEnv) open(t *testing.T, which projecttest.Side, secret, welcome string) {
	t.Helper()
	tenant := e.f.Tenant(which)
	if _, err := e.settings.Set(t.Context(), tenant.ProjectUID, settings.Update{
		RegistrationEnabled: true, RegistrationSecret: secret, WelcomeMarkdown: welcome,
	}, tenant.UserUID, audit.Entry{Action: audit.ActionSettingsUpdate, TargetType: "settings"}); err != nil {
		t.Fatalf("opening registration in %s: %v", which, err)
	}
}

// send issues method path with a JSON body anonymously and returns the answer
// with its body read into memory, the way projecttest.AssertSameShape wants it.
func (e *isolationEnv) send(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding the %s %s body: %v", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, payload)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, path, err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(answer))
	return resp
}

// registration is a POST /auth/register body into the project slug with
// secret, for the account username.
func registration(username, slug, secret string) map[string]string {
	return map[string]string{
		"username": username, "display_name": username, "email": username + "@example.test",
		"password": testPassword, "secret": secret, "project": slug,
	}
}

// TestRegistration_secretOfAnotherProjectIsAWrongSecret: A's secret posted to
// B's registration is refused exactly as a made-up secret is — same status,
// same body — and creates nothing; the same secret posted to A registers an
// unapproved viewer of A and of nothing else.
func TestRegistration_secretOfAnotherProjectIsAWrongSecret(t *testing.T) {
	e := newIsolationEnv(t)
	slugB := e.f.Tenant(projecttest.SideB).Slug

	foreign := e.send(t, http.MethodPost, "/api/v1/auth/register", registration("cizinec", slugB, secretA))
	wrong := e.send(t, http.MethodPost, "/api/v1/auth/register", registration("hadac", slugB, "nonsense"))
	if foreign.StatusCode != http.StatusForbidden {
		t.Fatalf("A's secret into B = %d, want 403", foreign.StatusCode)
	}
	projecttest.AssertSameShape(t, foreign, wrong)
	var accounts int
	if err := e.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM users WHERE username IN ('cizinec', 'hadac')`).Scan(&accounts); err != nil {
		t.Fatalf("counting accounts: %v", err)
	}
	if accounts != 0 {
		t.Fatalf("refused registrations created %d accounts, want 0", accounts)
	}

	resp := e.send(t, http.MethodPost, "/api/v1/auth/register",
		registration("soused", e.f.Tenant(projecttest.SideA).Slug, secretA))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("A's secret into A = %d, want 201", resp.StatusCode)
	}
	var userUID string
	if err := e.pool.QueryRow(t.Context(), `SELECT uid FROM users WHERE username = 'soused'`).Scan(&userUID); err != nil {
		t.Fatalf("reading the new account: %v", err)
	}
	member, err := e.projects.Membership(t.Context(), e.f.Tenant(projecttest.SideA).ProjectUID, userUID)
	if err != nil {
		t.Fatalf("membership in A: %v", err)
	}
	if member.Role != project.RoleViewer || member.ApprovedAt != nil {
		t.Fatalf("membership in A = %+v, want an unapproved viewer", member)
	}
	if _, err := e.projects.Membership(t.Context(), e.f.Tenant(projecttest.SideB).ProjectUID, userUID); err == nil {
		t.Fatal("the account registered into A is a member of B too")
	}
}

// TestRegistration_unknownProjectIsClosed: a registration naming a slug no
// project has is refused as a closed door, and creates nothing.
func TestRegistration_unknownProjectIsClosed(t *testing.T) {
	e := newIsolationEnv(t)
	resp := e.send(t, http.MethodPost, "/api/v1/auth/register", registration("nikdo", "neexistuje", secretA))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("registration into an unknown project = %d, want 403", resp.StatusCode)
	}
}

// TestPublicSettings_perProjectAndInactiveIs404: each project's public
// settings name that project; a deactivated project answers the same 404 as
// a slug nobody has, and refuses registrations too.
func TestPublicSettings_perProjectAndInactiveIs404(t *testing.T) {
	e := newIsolationEnv(t)
	for _, which := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		resp := e.send(t, http.MethodGet, e.f.PathFor(which, "/settings/public"), nil)
		body := decodeBody(t, resp)
		var name string
		if err := e.pool.QueryRow(t.Context(), `SELECT name FROM projects WHERE uid = $1`,
			e.f.Tenant(which).ProjectUID).Scan(&name); err != nil {
			t.Fatalf("reading %s's name: %v", which, err)
		}
		if resp.StatusCode != http.StatusOK || body["project_name"] != name || body["registration_enabled"] != true {
			t.Fatalf("%s public settings = %d %v, want %q open", which, resp.StatusCode, body, name)
		}
	}

	inactive := false
	if _, err := e.projects.Update(t.Context(), audit.Meta{}, e.f.Tenant(projecttest.SideB).ProjectUID,
		project.UpdatePatch{Active: &inactive}); err != nil {
		t.Fatalf("deactivating B: %v", err)
	}
	deactivated := e.send(t, http.MethodGet, e.f.PathFor(projecttest.SideB, "/settings/public"), nil)
	unknown := e.send(t, http.MethodGet, "/api/v1/p/neexistuje/settings/public", nil)
	if deactivated.StatusCode != http.StatusNotFound {
		t.Fatalf("deactivated project's public settings = %d, want 404", deactivated.StatusCode)
	}
	projecttest.AssertSameShape(t, deactivated, unknown)

	resp := e.send(t, http.MethodPost, "/api/v1/auth/register",
		registration("pozde", e.f.Tenant(projecttest.SideB).Slug, secretB))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("registration into a deactivated project = %d, want 403", resp.StatusCode)
	}
}

// TestPublicSettings_closedRegistrationIsLikeNoProject: a project whose
// registration is closed answers its public settings exactly like a slug
// nobody has — status, content type and body — so the anonymous endpoint
// cannot be used to tell existing slugs from missing ones; reopening it
// brings the name back.
func TestPublicSettings_closedRegistrationIsLikeNoProject(t *testing.T) {
	e := newIsolationEnv(t)
	b := e.f.Tenant(projecttest.SideB).ProjectUID
	if _, err := e.settings.Set(t.Context(), b, settings.Update{RegistrationSecret: secretB}, "",
		audit.Entry{Action: audit.ActionSettingsUpdate, TargetType: "settings", ProjectUID: b}); err != nil {
		t.Fatalf("closing B's registration: %v", err)
	}

	closed := e.send(t, http.MethodGet, e.f.PathFor(projecttest.SideB, "/settings/public"), nil)
	unknown := e.send(t, http.MethodGet, "/api/v1/p/neexistuje/settings/public", nil)
	if closed.StatusCode != http.StatusNotFound {
		t.Fatalf("closed project's public settings = %d, want 404", closed.StatusCode)
	}
	projecttest.AssertSameShape(t, closed, unknown)

	// A, still open, is unaffected.
	if resp := e.send(t, http.MethodGet, e.f.PathFor(projecttest.SideA, "/settings/public"), nil); resp.StatusCode != http.StatusOK {
		t.Errorf("open project's public settings = %d, want 200", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
}

// TestSettings_adminSeesOnlyTheirOwnProject: each project's admin reads their
// own secret and welcome, never the other's, and a member reads their own
// project's welcome.
func TestSettings_adminSeesOnlyTheirOwnProject(t *testing.T) {
	e := newIsolationEnv(t)
	for _, which := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		client := e.f.ClientFor(t, e.server, which)
		want, wantWelcome := secretA, "# Vítej v A"
		if which == projecttest.SideB {
			want, wantWelcome = secretB, "# Vítej v B"
		}
		body := decodeBody(t, e.f.Get(t, client, e.f.PathFor(which, "/settings")))
		if body["registration_secret"] != want || body["welcome_markdown"] != wantWelcome {
			t.Fatalf("%s's own settings = %v, want secret %q and welcome %q", which, body, want, wantWelcome)
		}
		welcome := decodeBody(t, e.f.Get(t, client, e.f.PathFor(which, "/settings/welcome")))
		if welcome["welcome_markdown"] != wantWelcome {
			t.Fatalf("%s's welcome = %v, want %q", which, welcome, wantWelcome)
		}
		foreign := e.f.Get(t, client, e.f.PathFor(which.Other(), "/settings"))
		if foreign.StatusCode != http.StatusNotFound {
			t.Fatalf("%s reading %s's settings = %d, want 404", which, which.Other(), foreign.StatusCode)
		}
		_ = foreign.Body.Close()
		foreignFooter := e.f.Get(t, client, e.f.PathFor(which.Other(), "/settings/footer"))
		if foreignFooter.StatusCode != http.StatusNotFound {
			t.Fatalf("%s reading %s's footer = %d, want 404", which, which.Other(), foreignFooter.StatusCode)
		}
		_ = foreignFooter.Body.Close()
	}
}
