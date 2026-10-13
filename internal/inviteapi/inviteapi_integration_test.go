//go:build integration

package inviteapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/invite"
	"github.com/panbotka/kukatko/internal/inviteapi"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/settings"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// testBaseURL stands in for mail.base_url in an invite's URL.
const testBaseURL = "https://kukatko.example.test"

// env is one test's server, fixture and pool.
type env struct {
	server *httptest.Server
	f      *projecttest.Fixture
	pool   *pgxpool.Pool
}

// newEnv seeds the two tenants (A = bootstrap, B = created, each with one admin)
// and mounts the real auth API — registration and the invite gate included —
// and the invite routes the way the server does: the management under
// /api/v1/p/{project} behind auth.WithProject, the public routes instance-level.
// limit throttles the public routes; nil throttles nothing.
func newEnv(t *testing.T, limit *ratelimit.Limiter) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	projects := project.NewStore(pool)
	store := invite.NewStore(pool)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{
		Service: svc, Limiter: auth.NewLimiter(100, time.Minute),
		RegisterLimiter: ratelimit.New(0, 0),
		Registration: auth.NewRegistration(auth.RegistrationConfig{
			Service: svc, Settings: settings.NewStore(pool), Mail: mailjob.NewEnqueuer(mailjob.EnqueuerConfig{}),
		}),
		Invites: inviteapi.NewRegistrationGate(store),
	})
	api := inviteapi.NewAPI(inviteapi.Config{
		Store: store, BaseURL: testBaseURL, RequireAdmin: authAPI.RequireAdmin,
		OptionalAuth: authAPI.OptionalAuth, RequireSignedIn: authAPI.RequireSignedIn, IPLimit: limit,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterInstanceRoutes(r)
		api.RegisterPublicRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, f: f, pool: pool}
}

// inviteBody is the part of an invite's wire shape the tests read.
type inviteBody struct {
	UID   string `json:"uid"`
	Code  string `json:"code"`
	URL   string `json:"url"`
	Uses  int    `json:"uses"`
	State string `json:"state"`
}

// do sends method path (under the server) as client — payload encoded as JSON,
// none when nil — and returns the response with its body buffered.
func (e *env) do(t *testing.T, client *http.Client, method, path string, payload any) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encoding the body: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, body)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp
}

// call is do returning the status and the body as a string.
func (e *env) call(t *testing.T, client *http.Client, method, path string, payload any) (int, string) {
	t.Helper()
	resp := e.do(t, client, method, path, payload)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// anonymous returns a client with no session.
func anonymous() *http.Client {
	client := *apitest.Client()
	return &client
}

// signIn returns a client signed in as username (password projecttest.Password).
func (e *env) signIn(t *testing.T, username string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := *apitest.Client()
	client.Jar = jar
	status, raw := e.call(t, &client, http.MethodPost, auth.APIBasePath+"/auth/login",
		map[string]string{"username": username, "password": projecttest.Password})
	if status != http.StatusOK {
		t.Fatalf("signing in as %s = %d %s", username, status, raw)
	}
	return &client
}

// seedAccount creates an approved account with no membership and returns its uid.
func (e *env) seedAccount(t *testing.T, username string) string {
	t.Helper()
	hash, err := auth.HashPassword(projecttest.Password)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	uid := projecttest.MissingUID("us")
	now := time.Now()
	user := auth.User{
		UID: uid, Username: username, Email: username + "@example.test", PasswordHash: hash,
		Role: auth.RoleUser, ApprovedAt: &now,
	}
	if err := auth.NewStore(e.pool).CreateUser(t.Context(), user); err != nil {
		t.Fatalf("creating %s: %v", username, err)
	}
	return uid
}

// addMember makes userUID a member of projectUID with role, approved or not.
func (e *env) addMember(t *testing.T, projectUID, userUID string, role project.Role, approved bool) {
	t.Helper()
	if _, err := project.NewStore(e.pool).AddMember(t.Context(), audit.Meta{}, projectUID, userUID,
		role, approved); err != nil {
		t.Fatalf("adding %s to %s: %v", userUID, projectUID, err)
	}
}

// createInvite has B's admin create an invite in B with body and returns it.
func (e *env) createInvite(t *testing.T, body map[string]any) inviteBody {
	t.Helper()
	admin := e.f.ClientFor(t, e.server, projecttest.SideB)
	status, raw := e.call(t, admin, http.MethodPost, e.f.PathFor(projecttest.SideB, "invites"), body)
	if status != http.StatusCreated {
		t.Fatalf("creating an invite = %d %s", status, raw)
	}
	var inv inviteBody
	if err := json.Unmarshal([]byte(raw), &inv); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return inv
}

// membership reads userUID's membership of projectUID, nil when there is none.
func (e *env) membership(t *testing.T, projectUID, userUID string) *project.Member {
	t.Helper()
	m, err := project.NewStore(e.pool).Membership(t.Context(), projectUID, userUID)
	if err != nil {
		return nil
	}
	return m
}

// uses reads the invite's use counter straight from the table.
func (e *env) uses(t *testing.T, uid string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), `SELECT uses FROM project_invites WHERE uid = $1`, uid).Scan(&n); err != nil {
		t.Fatalf("reading the uses of %s: %v", uid, err)
	}
	return n
}

// acceptCount counts the project.invite.accept entries of the invite.
func (e *env) acceptCount(t *testing.T, uid string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_log WHERE action = $1 AND target_uid = $2`,
		audit.ActionProjectInviteAccept, uid).Scan(&n); err != nil {
		t.Fatalf("counting accepts: %v", err)
	}
	return n
}

// TestAccept_addsMembership proves an account that belongs to no project at
// all can open an invite signed in and accept it: it becomes an approved member
// of the invite's project with the invite's role, the invite counts one use and
// the accept is audited in that project.
func TestAccept_addsMembership(t *testing.T) {
	e := newEnv(t, nil)
	inv := e.createInvite(t, map[string]any{"role": "curator"})
	if inv.URL != testBaseURL+"/invite/"+inv.Code || !strings.HasPrefix(inv.UID, "iv") || inv.State != "active" {
		t.Fatalf("created invite %+v; want an active iv… with its URL", inv)
	}
	userUID := e.seedAccount(t, "newcomer")
	client := e.signIn(t, "newcomer")

	status, raw := e.call(t, client, http.MethodGet, auth.APIBasePath+"/invite/"+inv.Code, nil)
	if status != http.StatusOK || !strings.Contains(raw, `"signed_in":true`) || !strings.Contains(raw, `"role":"curator"`) {
		t.Fatalf("the invite page = %d %s", status, raw)
	}
	if status, raw = e.call(t, anonymous(), http.MethodGet, auth.APIBasePath+"/invite/"+inv.Code, nil); status !=
		http.StatusOK || !strings.Contains(raw, `"signed_in":false`) {
		t.Errorf("the anonymous invite page = %d %s", status, raw)
	}
	status, raw = e.call(t, client, http.MethodPost, auth.APIBasePath+"/invite/"+inv.Code+"/accept", nil)
	if status != http.StatusOK || !strings.Contains(raw, `"slug":"`+e.f.B.Slug+`"`) {
		t.Fatalf("accept = %d %s, want B's slug", status, raw)
	}
	m := e.membership(t, e.f.B.ProjectUID, userUID)
	if m == nil || m.Role != project.RoleCurator || m.ApprovedAt == nil {
		t.Errorf("membership after accepting = %+v, want an approved curator", m)
	}
	if got := e.uses(t, inv.UID); got != 1 {
		t.Errorf("uses = %d, want 1", got)
	}
	var projectID string
	if err := e.pool.QueryRow(t.Context(), `SELECT project_id FROM audit_log WHERE action = $1 AND actor_uid = $2`,
		audit.ActionProjectInviteAccept, userUID).Scan(&projectID); err != nil || projectID != e.f.B.ProjectUID {
		t.Errorf("accept audit entry project = %q, %v; want B", projectID, err)
	}
	if status, _ := e.call(t, anonymous(), http.MethodPost,
		auth.APIBasePath+"/invite/"+inv.Code+"/accept", nil); status != http.StatusUnauthorized {
		t.Errorf("an anonymous accept = %d, want 401", status)
	}
}

// TestAccept_raisesNeverLowers proves accepting raises a lower role and
// approves a waiting membership, but leaves a higher role untouched without
// counting a use.
func TestAccept_raisesNeverLowers(t *testing.T) {
	e := newEnv(t, nil)
	inv := e.createInvite(t, map[string]any{"role": "editor"})

	viewer := e.seedAccount(t, "lowly")
	e.addMember(t, e.f.B.ProjectUID, viewer, project.RoleViewer, true)
	waiting := e.seedAccount(t, "waiting")
	e.addMember(t, e.f.B.ProjectUID, waiting, project.RoleViewer, false)
	boss := e.seedAccount(t, "boss")
	e.addMember(t, e.f.B.ProjectUID, boss, project.RoleAdmin, true)

	for _, tc := range []struct {
		username, uid string
		want          project.Role
		changed       bool
	}{
		{"lowly", viewer, project.RoleEditor, true},
		{"waiting", waiting, project.RoleEditor, true},
		{"boss", boss, project.RoleAdmin, false},
	} {
		status, raw := e.call(t, e.signIn(t, tc.username), http.MethodPost,
			auth.APIBasePath+"/invite/"+inv.Code+"/accept", nil)
		if status != http.StatusOK || !strings.Contains(raw, `"role":"`+string(tc.want)+`"`) {
			t.Errorf("%s accept = %d %s, want role %s", tc.username, status, raw, tc.want)
		}
		m := e.membership(t, e.f.B.ProjectUID, tc.uid)
		if m == nil || m.Role != tc.want || m.ApprovedAt == nil {
			t.Errorf("%s membership = %+v, want an approved %s", tc.username, m, tc.want)
		}
	}
	if got := e.uses(t, inv.UID); got != 2 {
		t.Errorf("uses = %d, want 2 (the admin's accept granted nothing)", got)
	}
}

// TestAccept_twiceIsIdempotent proves a repeated accept changes nothing and
// counts one use — even once the invite's only use is taken.
func TestAccept_twiceIsIdempotent(t *testing.T) {
	e := newEnv(t, nil)
	inv := e.createInvite(t, map[string]any{"role": "viewer", "max_uses": 1})
	userUID := e.seedAccount(t, "twice")
	client := e.signIn(t, "twice")

	for i, wantChanged := range []bool{true, false} {
		status, raw := e.call(t, client, http.MethodPost, auth.APIBasePath+"/invite/"+inv.Code+"/accept", nil)
		if status != http.StatusOK || !strings.Contains(raw, `"changed":`+map[bool]string{true: "true", false: "false"}[wantChanged]) {
			t.Errorf("accept #%d = %d %s", i+1, status, raw)
		}
	}
	if got := e.uses(t, inv.UID); got != 1 {
		t.Errorf("uses = %d, want 1", got)
	}
	if got := e.acceptCount(t, inv.UID); got != 1 {
		t.Errorf("accept audit entries = %d, want 1", got)
	}
	if m := e.membership(t, e.f.B.ProjectUID, userUID); m == nil || m.Role != project.RoleViewer {
		t.Errorf("membership = %+v, want a viewer", m)
	}
}

// TestInvalidInvites_sameShape proves an expired, a revoked and a used-up
// invite are answered — on the page and on the accept — exactly like a code
// that never existed.
func TestInvalidInvites_sameShape(t *testing.T) {
	e := newEnv(t, nil)
	expired := e.createInvite(t, map[string]any{"role": "viewer"})
	if _, err := e.pool.Exec(t.Context(), `UPDATE project_invites SET expires_at = now() - interval '1 minute'
		WHERE uid = $1`, expired.UID); err != nil {
		t.Fatalf("expiring: %v", err)
	}
	revoked := e.createInvite(t, map[string]any{"role": "viewer"})
	admin := e.f.ClientFor(t, e.server, projecttest.SideB)
	if status, raw := e.call(t, admin, http.MethodDelete,
		e.f.PathFor(projecttest.SideB, "invites/"+revoked.UID), nil); status != http.StatusNoContent {
		t.Fatalf("revoking = %d %s", status, raw)
	}
	usedUp := e.createInvite(t, map[string]any{"role": "viewer", "max_uses": 1})
	e.seedAccount(t, "first")
	if status, raw := e.call(t, e.signIn(t, "first"), http.MethodPost,
		auth.APIBasePath+"/invite/"+usedUp.Code+"/accept", nil); status != http.StatusOK {
		t.Fatalf("taking the only use = %d %s", status, raw)
	}

	e.seedAccount(t, "late")
	late := e.signIn(t, "late")
	const unknown = "Zz9zZz9z"
	for name, code := range map[string]string{"expired": expired.Code, "revoked": revoked.Code, "used up": usedUp.Code} {
		page := e.do(t, anonymous(), http.MethodGet, auth.APIBasePath+"/invite/"+code, nil)
		unknownPage := e.do(t, anonymous(), http.MethodGet, auth.APIBasePath+"/invite/"+unknown, nil)
		if page.StatusCode != http.StatusNotFound {
			t.Errorf("%s page = %d, want 404", name, page.StatusCode)
		}
		projecttest.AssertSameShape(t, page, unknownPage)
		accept := e.do(t, late, http.MethodPost, auth.APIBasePath+"/invite/"+code+"/accept", nil)
		unknownAccept := e.do(t, late, http.MethodPost, auth.APIBasePath+"/invite/"+unknown+"/accept", nil)
		if accept.StatusCode != http.StatusNotFound {
			t.Errorf("%s accept = %d, want 404", name, accept.StatusCode)
		}
		projecttest.AssertSameShape(t, accept, unknownAccept)
		_, _, _, _ = page.Body.Close(), unknownPage.Body.Close(), accept.Body.Close(), unknownAccept.Body.Close()
	}
	if got := e.uses(t, usedUp.UID); got != 1 {
		t.Errorf("used-up invite uses = %d, want 1", got)
	}
}

// TestRegister_viaInvite proves registering through an invite — with public
// registration switched off — creates an approved account that is an approved
// member of the invite's project, an admin invite yielding an editor at most,
// counts the use, and that a used-up invite refuses the next registration.
func TestRegister_viaInvite(t *testing.T) {
	e := newEnv(t, nil)
	inv := e.createInvite(t, map[string]any{"role": "admin", "max_uses": 1})

	register := func(username string) (int, string) {
		return e.call(t, anonymous(), http.MethodPost, auth.APIBasePath+"/auth/register", map[string]string{
			"username": username, "email": username + "@example.test", "password": projecttest.Password,
			"invite_code": inv.Code,
		})
	}
	status, raw := register("invited")
	if status != http.StatusCreated || !strings.Contains(raw, `"pending_approval":false`) {
		t.Fatalf("registering through the invite = %d %s", status, raw)
	}
	user, err := auth.NewStore(e.pool).GetUserByUsername(t.Context(), "invited")
	if err != nil || user.ApprovedAt == nil {
		t.Fatalf("the new account = %+v, %v; want it approved", user, err)
	}
	m := e.membership(t, e.f.B.ProjectUID, user.UID)
	if m == nil || m.Role != project.RoleEditor || m.ApprovedAt == nil {
		t.Errorf("membership = %+v, want an approved editor (an admin invite capped)", m)
	}
	if other := e.membership(t, e.f.A.ProjectUID, user.UID); other != nil {
		t.Errorf("the account also joined A: %+v", other)
	}
	if got := e.uses(t, inv.UID); got != 1 {
		t.Errorf("uses = %d, want 1", got)
	}
	if got := e.acceptCount(t, inv.UID); got != 1 {
		t.Errorf("accept audit entries = %d, want 1", got)
	}
	e.signIn(t, "invited") // an approved account signs in at once

	if status, raw := register("toolate"); status != http.StatusForbidden {
		t.Errorf("registering through a used-up invite = %d %s, want 403", status, raw)
	}
	if _, err := auth.NewStore(e.pool).GetUserByUsername(t.Context(), "toolate"); err == nil {
		t.Error("the refused registration left an account behind")
	}
}

// TestInvites_foreignProjectIsNotFound proves an admin of A cannot list B's
// invites nor revoke one of them: each answer is the very one an unknown
// project or an unknown invite gets, and A's own list never names B's invite.
func TestInvites_foreignProjectIsNotFound(t *testing.T) {
	e := newEnv(t, nil)
	bInvite := e.createInvite(t, map[string]any{"role": "viewer"})
	client := e.f.ClientFor(t, e.server, projecttest.SideA)

	foreignList := e.do(t, client, http.MethodGet, e.f.PathFor(projecttest.SideB, "invites"), nil)
	missingList := e.do(t, client, http.MethodGet, auth.APIBasePath+"/p/no-such-project/invites", nil)
	projecttest.AssertSameShape(t, foreignList, missingList)
	foreignRevoke := e.do(t, client, http.MethodDelete, e.f.PathFor(projecttest.SideA, "invites/"+bInvite.UID), nil)
	missingRevoke := e.do(t, client, http.MethodDelete,
		e.f.PathFor(projecttest.SideA, "invites/"+projecttest.MissingUID("iv")), nil)
	projecttest.AssertSameShape(t, foreignRevoke, missingRevoke)
	_, _, _, _ = foreignList.Body.Close(), missingList.Body.Close(), foreignRevoke.Body.Close(),
		missingRevoke.Body.Close()
	if foreignList.StatusCode != http.StatusNotFound || foreignRevoke.StatusCode != http.StatusNotFound {
		t.Errorf("B's invites through A = %d, revoking B's invite under A = %d; want 404 for both",
			foreignList.StatusCode, foreignRevoke.StatusCode)
	}
	e.f.AssertListExcludes(t, client, e.f.PathFor(projecttest.SideA, "invites"), bInvite.UID, bInvite.Code)

	var revoked *time.Time
	if err := e.pool.QueryRow(t.Context(), `SELECT revoked_at FROM project_invites WHERE uid = $1`,
		bInvite.UID).Scan(&revoked); err != nil || revoked != nil {
		t.Errorf("B's invite after A's attempt: revoked_at %v, %v; want untouched", revoked, err)
	}
}

// TestInvites_adminOnly proves a member below admin cannot create invites.
func TestInvites_adminOnly(t *testing.T) {
	e := newEnv(t, nil)
	editor := e.seedAccount(t, "editor")
	e.addMember(t, e.f.B.ProjectUID, editor, project.RoleEditor, true)
	status, _ := e.call(t, e.signIn(t, "editor"), http.MethodPost,
		e.f.PathFor(projecttest.SideB, "invites"), map[string]any{"role": "admin"})
	if status != http.StatusForbidden {
		t.Errorf("an editor creating an invite = %d, want 403", status)
	}
}

// TestPublic_rateLimited proves the public routes are throttled per client IP
// ahead of the lookup: past the burst even a live invite answers 429.
func TestPublic_rateLimited(t *testing.T) {
	e := newEnv(t, ratelimit.New(0.001, 3))
	inv := e.createInvite(t, map[string]any{"role": "viewer"})
	for i := range 3 {
		if status, _ := e.call(t, anonymous(), http.MethodGet, auth.APIBasePath+"/invite/Zz9zZz9z", nil); status !=
			http.StatusNotFound {
			t.Fatalf("guess #%d = %d, want 404 within the burst", i+1, status)
		}
	}
	if status, _ := e.call(t, anonymous(), http.MethodGet, auth.APIBasePath+"/invite/"+inv.Code, nil); status !=
		http.StatusTooManyRequests {
		t.Errorf("past the burst = %d, want 429", status)
	}
}
