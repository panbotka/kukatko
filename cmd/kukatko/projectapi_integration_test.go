//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/apitest"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/server"
)

// projectAPITestPassword is the password of every account the tests create;
// they authenticate with API tokens, so it is never sent.
const projectAPITestPassword = "correct horse battery staple"

// projectAPIEnv is the whole serve wiring — buildServices, appendOpsAPIs and
// requestOptions, exactly as runServe assembles them — over the integration
// database, behind an httptest server.
type projectAPIEnv struct {
	db      *database.DB
	authSvc *auth.Service
	server  *httptest.Server
}

// newProjectAPIEnv builds the server over a truncated test database, with
// local-disk storage in a temp dir and the MCP server switched on.
func newProjectAPIEnv(t *testing.T) *projectAPIEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	dir := t.TempDir()
	for key, value := range map[string]string{
		"KUKATKO_CONFIG":                 filepath.Join(dir, "absent.yaml"),
		"KUKATKO_DATABASE_URL":           "postgres://unused", // the pool is dbtest's
		"KUKATKO_STORAGE_BACKEND":        config.StorageBackendFS,
		"KUKATKO_STORAGE_ORIGINALS_PATH": filepath.Join(dir, "originals"),
		"KUKATKO_STORAGE_CACHE_PATH":     filepath.Join(dir, "cache"),
		"KUKATKO_STORAGE_TEMP_PATH":      filepath.Join(dir, "tmp"),
		"KUKATKO_MCP_ENABLED":            "true",
		"KUKATKO_BACKUP_S3_BUCKET":       "",
	} {
		t.Setenv(key, value)
	}
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	authAPI, authSvc, err := buildAuth(cfg, db)
	if err != nil {
		t.Fatalf("buildAuth: %v", err)
	}
	mapsHealth, budget := newMapsHealth(cfg), newGeocodeBudget(cfg)
	apis, projectAPIs, _, err := buildServices(cfg, db, authAPI, nil, mapsHealth, budget)
	if err != nil {
		t.Fatalf("buildServices: %v", err)
	}
	apis, _, _, err = appendOpsAPIs(cfg, db, authAPI, apis, projectAPIs, nil, mapsHealth, budget)
	if err != nil {
		t.Fatalf("appendOpsAPIs: %v", err)
	}
	apis = append(apis, requestOptions(cfg, db, slog.New(slog.DiscardHandler), projectAPIs)...)
	ts := httptest.NewServer(server.New("", apis...).Handler())
	t.Cleanup(ts.Close)
	return &projectAPIEnv{db: db, authSvc: authSvc, server: ts}
}

// token creates username with role (a member of the bootstrap project) and
// returns an API token for it.
func (e *projectAPIEnv) token(t *testing.T, username string, role auth.AccountRole) (auth.User, string) {
	t.Helper()
	user, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: projectAPITestPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	_, plaintext, err := e.authSvc.CreateAPIToken(t.Context(), user, auth.CreateAPITokenInput{Name: "test"},
		audit.Entry{ActorUID: user.UID, Action: audit.ActionAPITokenCreate, TargetType: "api_tokens"})
	if err != nil {
		t.Fatalf("CreateAPIToken(%s): %v", username, err)
	}
	return user, plaintext
}

// seedPhoto catalogues a photo record (no bytes are needed for a listing),
// placed on the map.
func (e *projectAPIEnv) seedPhoto(t *testing.T, hash, name string) photos.Photo {
	t.Helper()
	lat, lng := 49.05, 17.36
	created, err := photos.NewStore(e.db.Pool()).Create(t.Context(), photos.Photo{
		ProjectID: project.BootstrapUID,
		FileHash:  hash, FilePath: "2024/05/" + name, FileName: name, FileSize: 1,
		FileMime: "image/jpeg", FileWidth: 64, FileHeight: 48, FileOrientation: 1,
		TakenAtSource: "unknown", Lat: &lat, Lng: &lng,
	})
	if err != nil {
		t.Fatalf("seeding %s: %v", name, err)
	}
	return created
}

// do sends method path with the bearer token (none when empty) and returns the
// status, headers and body.
func (e *projectAPIEnv) do(t *testing.T, method, path, bearer string, body []byte) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, e.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := apitest.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header, raw
}

// get is do for a GET without a body that must answer 200.
func (e *projectAPIEnv) get(t *testing.T, path, bearer string) []byte {
	t.Helper()
	status, _, body := e.do(t, http.MethodGet, path, bearer, nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d, body %s", path, status, body)
	}
	return body
}

// TestProjectAPI_onlyThePrefixServesAProject proves, through the whole serve
// wiring, that a project-scoped route answers only under /api/v1/p/<slug> —
// with media URLs leading back through that prefix — while the same path
// without the prefix is the API's JSON 404, never somebody's library and
// never the SPA.
func TestProjectAPI_onlyThePrefixServesAProject(t *testing.T) {
	e := newProjectAPIEnv(t)
	_, bearer := e.token(t, "eva", auth.AccountEditor)
	photo := e.seedPhoto(t, strings.Repeat("ab", 32), "a.jpg")

	for _, path := range []string{"/photos", "/map/photos", "/albums", "/mcp"} {
		status, header, body := e.do(t, http.MethodGet, "/api/v1"+path, bearer, nil)
		if status != http.StatusNotFound || !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
			t.Errorf("GET /api/v1%s = %d (%s) %s, want the API's JSON 404", path, status,
				header.Get("Content-Type"), body)
		}
	}
	for _, path := range []string{"/photos", "/map/photos"} {
		prefixed := e.get(t, "/api/v1/p/v"+path, bearer)
		if !bytes.Contains(prefixed, []byte(`"/api/v1/p/v/photos/`+photo.UID+`/thumb/`)) {
			t.Errorf("%s through the prefix carries no prefixed thumbnail URL: %s", path, prefixed)
		}
	}
}

// TestProjectAPI_instanceRoutesEnterNoProject proves the instance-wide routes
// work without any project: an account that belongs to no project reads
// /auth/me (and lists no membership), and a maintainer reaches /jobs/stats —
// neither request names a project, and neither is refused for it.
func TestProjectAPI_instanceRoutesEnterNoProject(t *testing.T) {
	e := newProjectAPIEnv(t)
	loner, bearer := e.token(t, "samotar", auth.AccountViewer)
	_, maint := e.token(t, "marek", auth.AccountMaintainer)
	if _, err := e.db.Pool().Exec(t.Context(), `DELETE FROM project_members WHERE user_uid = $1`,
		loner.UID); err != nil {
		t.Fatalf("removing samotar from every project: %v", err)
	}

	var me struct {
		Projects []json.RawMessage `json:"projects"`
	}
	if err := json.Unmarshal(e.get(t, "/api/v1/auth/me", bearer), &me); err != nil || len(me.Projects) != 0 {
		t.Errorf("GET /auth/me of an account in no project = %+v, %v; want no memberships", me, err)
	}
	e.get(t, "/api/v1/jobs/stats", maint)
	if status, _, body := e.do(t, http.MethodGet, "/api/v1/p/v/photos", bearer, nil); status != http.StatusNotFound {
		t.Errorf("GET /p/v/photos by an account in no project = %d %s, want 404", status, body)
	}
}

// TestProjectAPI_unknownAndForeignProjectsAreIndistinguishable proves a slug
// nobody uses and a project the caller is no member of answer the very same
// 404, while the caller's own project answers under its slug.
func TestProjectAPI_unknownAndForeignProjectsAreIndistinguishable(t *testing.T) {
	e := newProjectAPIEnv(t)
	_, insider := e.token(t, "eva", auth.AccountEditor)
	stranger, outsider := e.token(t, "cizinec", auth.AccountEditor)

	projects := project.NewStore(e.db.Pool())
	alfa, err := projects.Create(t.Context(), audit.Meta{}, project.CreateParams{Slug: "alfa", Name: "Alfa"})
	if err != nil {
		t.Fatalf("creating project alfa: %v", err)
	}
	if _, err := projects.AddMember(t.Context(), audit.Meta{}, alfa.UID, stranger.UID, project.RoleEditor,
		true); err != nil {
		t.Fatalf("adding cizinec to alfa: %v", err)
	}
	if _, err := e.db.Pool().Exec(t.Context(),
		`DELETE FROM project_members WHERE project_uid = $1 AND user_uid = $2`,
		project.BootstrapUID, stranger.UID); err != nil {
		t.Fatalf("removing cizinec from v: %v", err)
	}

	unknownStatus, _, unknownBody := e.do(t, http.MethodGet, "/api/v1/p/nope/photos", insider, nil)
	if unknownStatus != http.StatusNotFound {
		t.Fatalf("unknown project = %d %s, want 404", unknownStatus, unknownBody)
	}
	foreignStatus, _, foreignBody := e.do(t, http.MethodGet, "/api/v1/p/v/photos", outsider, nil)
	if foreignStatus != http.StatusNotFound || !bytes.Equal(foreignBody, unknownBody) {
		t.Errorf("foreign project = %d %s, want the unknown project's 404 %s", foreignStatus, foreignBody, unknownBody)
	}
	// The same caller is at home in its own project.
	e.get(t, "/api/v1/p/alfa/photos", outsider)
}

// TestProjectAPI_mcpUnderThePrefix proves the MCP server answers under the
// project prefix: the Bearer challenge naming the endpoint called on an
// anonymous call, a working handshake for a token, and the foreign-project
// 404 — and that the unprefixed path of the transition is gone.
func TestProjectAPI_mcpUnderThePrefix(t *testing.T) {
	e := newProjectAPIEnv(t)
	_, bearer := e.token(t, "agent", auth.AccountEditor)
	initialize, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "test", "version": "1"},
		},
	})
	if err != nil {
		t.Fatalf("marshal initialize: %v", err)
	}

	if status, _, body := e.do(t, http.MethodPost, "/api/v1/mcp", bearer, initialize); status != http.StatusNotFound {
		t.Errorf("initialize at the unprefixed /api/v1/mcp = %d %s, want 404", status, body)
	}
	for _, path := range []string{"/api/v1/p/v/mcp"} {
		status, header, body := e.do(t, http.MethodPost, path, "", initialize)
		want := auth.Challenge + ` realm="` + e.server.URL + path + `"`
		if status != http.StatusUnauthorized || header.Get("WWW-Authenticate") != want {
			t.Errorf("anonymous %s = %d (WWW-Authenticate %q) %s, want 401 with %q",
				path, status, header.Get("WWW-Authenticate"), body, want)
		}
		if status, _, body := e.do(t, http.MethodPost, path, bearer, initialize); status != http.StatusOK {
			t.Errorf("initialize at %s = %d %s, want 200", path, status, body)
		}
	}
	if status, _, _ := e.do(t, http.MethodPost, "/api/v1/p/nope/mcp", bearer, initialize); status != http.StatusNotFound {
		t.Errorf("initialize in an unknown project = %d, want 404", status)
	}
	// An OAuth discovery probe for the prefixed endpoint finds nothing either.
	if status, _, _ := e.do(t, http.MethodGet, "/.well-known/oauth-protected-resource/api/v1/p/v/mcp", "",
		nil); status != http.StatusNotFound {
		t.Errorf("protected-resource probe = %d, want 404", status)
	}
}

// TestProjectAPI_usersSplitsDirectoryAndAccountList proves, through the whole
// serve wiring, that the unprefixed GET /users is the system administrator's
// account list (a viewer is refused, a maintainer gets every account with its
// memberships) while the name directory stays reachable for everybody under
// the project prefix; and that /projects is mounted instance-level.
func TestProjectAPI_usersSplitsDirectoryAndAccountList(t *testing.T) {
	e := newProjectAPIEnv(t)
	_, viewer := e.token(t, "vera", auth.AccountViewer)
	_, maint := e.token(t, "marek", auth.AccountMaintainer)

	if status, _, body := e.do(t, http.MethodGet, "/api/v1/users", viewer, nil); status != http.StatusForbidden {
		t.Errorf("GET /api/v1/users as a viewer = %d, want 403 (body %s)", status, body)
	}
	var accounts []struct {
		Username    string            `json:"username"`
		Memberships []json.RawMessage `json:"memberships"`
	}
	if err := json.Unmarshal(e.get(t, "/api/v1/users", maint), &accounts); err != nil || len(accounts) != 2 {
		t.Errorf("GET /api/v1/users as a maintainer = %+v, %v; want both accounts", accounts, err)
	}
	directory := e.get(t, "/api/v1/p/v/users", viewer)
	if !bytes.Contains(directory, []byte(`"users"`)) || !bytes.Contains(directory, []byte("vera")) {
		t.Errorf("the name directory under the prefix = %s", directory)
	}
	if status, _, body := e.do(t, http.MethodGet, "/api/v1/projects", viewer, nil); status != http.StatusForbidden {
		t.Errorf("GET /api/v1/projects as a viewer = %d, want 403 (body %s)", status, body)
	}
	if list := e.get(t, "/api/v1/projects", maint); !bytes.Contains(list, []byte(`"slug":"v"`)) {
		t.Errorf("GET /api/v1/projects = %s, want the bootstrap project", list)
	}
}
