//go:build integration

package projectapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/panbotka/kukatko/internal/projectapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

const testPassword = "correct horse battery staple"

// env is the project API and the auth API behind an httptest server over the
// integration database.
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

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := projectapi.NewAPI(projectapi.Config{
		Projects:          project.NewStore(db.Pool()),
		Accounts:          authStore,
		RequireMaintainer: authAPI.RequireMaintainer,
	})

	r := chi.NewRouter()
	r.Use(clientip.Middleware(nil))
	r.Route("/api/v1", api.RegisterRoutes)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{baseURL: server.URL, db: db, authSvc: authSvc}
}

// user creates username with role (a member of the bootstrap project unless it
// is a maintainer) and returns it with an API token.
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

// do sends method path with body (none when empty) as token and returns the
// status and the body.
func (e *env) do(t *testing.T, token, method, path, body string) (int, []byte) {
	t.Helper()
	var payload io.Reader
	if body != "" {
		payload = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.baseURL+path, payload)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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
	return resp.StatusCode, data
}

// mustDo is do that fails the test unless the status is want, decoding the body
// into out when out is not nil.
func (e *env) mustDo(t *testing.T, token, method, path, body string, want int, out any) {
	t.Helper()
	status, data := e.do(t, token, method, path, body)
	if status != want {
		t.Fatalf("%s %s = %d, want %d (body %s)", method, path, status, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decoding %s %s: %v (body %s)", method, path, err, data)
		}
	}
}

// expectError asserts that method path answers status with the error code.
func (e *env) expectError(t *testing.T, token, method, path, body string, status int, code string) {
	t.Helper()
	got, data := e.do(t, token, method, path, body)
	var refusal struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &refusal)
	if got != status || refusal.Error != code {
		t.Errorf("%s %s = %d %q, want %d %q (body %s)", method, path, got, refusal.Error, status, code, data)
	}
}

// overview is the wire shape of one project.
type overview struct {
	UID            string `json:"uid"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Active         bool   `json:"active"`
	StoragePrefix  string `json:"storage_prefix"`
	MaxFileBytes   *int64 `json:"max_file_bytes"`
	MaxTotalBytes  *int64 `json:"max_total_bytes"`
	MembersCount   int64  `json:"members_count"`
	PhotosCount    int64  `json:"photos_count"`
	OriginalsBytes int64  `json:"originals_bytes"`
	CreatedAt      string `json:"created_at"`
}

// account is the wire shape of one /users entry.
type account struct {
	UID          string `json:"uid"`
	Username     string `json:"username"`
	InstanceRole string `json:"instance_role"`
	Disabled     bool   `json:"disabled"`
	Memberships  []struct {
		ProjectSlug string `json:"project_slug"`
		Role        string `json:"role"`
		Approved    bool   `json:"approved"`
	} `json:"memberships"`
}

// auditProjects returns the project_id of every audit row with action, oldest
// first ("" for a NULL).
func (e *env) auditProjects(t *testing.T, action string) []string {
	t.Helper()
	rows, err := e.db.Pool().Query(t.Context(),
		`SELECT COALESCE(project_id, '') FROM audit_log WHERE action = $1 ORDER BY id`, action)
	if err != nil {
		t.Fatalf("reading audit rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scanning audit row: %v", err)
		}
		out = append(out, id)
	}
	return out
}

// createProject creates slug as token and returns it.
func (e *env) createProject(t *testing.T, token, slug string) overview {
	t.Helper()
	var created overview
	e.mustDo(t, token, http.MethodPost, "/api/v1/projects",
		`{"slug":"`+slug+`","name":"Project `+slug+`"}`, http.StatusCreated, &created)
	return created
}

// TestRoutes_maintainerOnly proves every route refuses a project admin who is
// not a system administrator, a viewer and an anonymous caller.
func TestRoutes_maintainerOnly(t *testing.T) {
	e := newEnv(t)
	_, admin := e.user(t, "padmin", auth.AccountAdmin)
	viewer, viewerToken := e.user(t, "pviewer", auth.AccountViewer)
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/projects", ""},
		{http.MethodPost, "/api/v1/projects", `{"slug":"x","name":"X"}`},
		{http.MethodGet, "/api/v1/projects/" + project.BootstrapUID, ""},
		{http.MethodPatch, "/api/v1/projects/" + project.BootstrapUID, `{"name":"Y"}`},
		{http.MethodPost, "/api/v1/projects/" + project.BootstrapUID + "/members",
			`{"user_uid":"` + viewer.UID + `","role":"admin"}`},
		{http.MethodDelete, "/api/v1/projects/" + project.BootstrapUID + "/members/" + viewer.UID, ""},
		{http.MethodGet, "/api/v1/users", ""},
		{http.MethodPatch, "/api/v1/users/" + viewer.UID, `{"instance_role":"maintainer"}`},
	}
	for _, route := range routes {
		for _, caller := range []struct {
			name, token string
			want        int
		}{{"project admin", admin, http.StatusForbidden}, {"viewer", viewerToken, http.StatusForbidden},
			{"anonymous", "", http.StatusUnauthorized}} {
			if status, data := e.do(t, caller.token, route.method, route.path, route.body); status != caller.want {
				t.Errorf("%s %s as %s = %d, want %d (body %s)",
					route.method, route.path, caller.name, status, caller.want, data)
			}
		}
	}
	// Nothing the refused calls asked for happened.
	if n := len(e.auditProjects(t, audit.ActionProjectCreate)); n != 0 {
		t.Errorf("project.create rows after refused calls = %d, want 0", n)
	}
}

// TestCreate_listGetAndSeeding covers creating a project: 201 with the
// project, the creator as its approved admin, its settings row, the audit rows
// carrying the project, and the listing with counts.
func TestCreate_listGetAndSeeding(t *testing.T) {
	e := newEnv(t)
	maint, token := e.user(t, "maint", auth.AccountMaintainer)

	var created overview
	e.mustDo(t, token, http.MethodPost, "/api/v1/projects",
		`{"slug":"hasici","name":" Hasiči ","description":"Sbor","max_file_bytes":1000}`,
		http.StatusCreated, &created)
	if created.Slug != "hasici" || created.Name != "Hasiči" || !created.Active || created.MembersCount != 1 ||
		created.StoragePrefix != "projects/"+created.UID+"/" || created.MaxFileBytes == nil ||
		*created.MaxFileBytes != 1000 || created.MaxTotalBytes != nil {
		t.Errorf("created = %+v", created)
	}

	member, err := project.NewStore(e.db.Pool()).Membership(t.Context(), created.UID, maint.UID)
	if err != nil || member.Role != project.RoleAdmin || member.ApprovedAt == nil {
		t.Errorf("creator membership = %+v, %v; want an approved admin", member, err)
	}
	var registration bool
	var secret, welcome string
	if err := e.db.Pool().QueryRow(t.Context(),
		`SELECT registration_enabled, registration_secret, welcome_markdown FROM instance_settings
		 WHERE project_id = $1`, created.UID).Scan(&registration, &secret, &welcome); err != nil ||
		registration || secret != "" || welcome != "" {
		t.Errorf("settings row = %v %q %q, %v; want closed, empty", registration, secret, welcome, err)
	}
	for _, action := range []string{audit.ActionProjectCreate, audit.ActionProjectMemberAdd} {
		if got := e.auditProjects(t, action); len(got) != 1 || got[0] != created.UID {
			t.Errorf("%s audit project ids = %v, want [%s]", action, got, created.UID)
		}
	}

	var list []overview
	e.mustDo(t, token, http.MethodGet, "/api/v1/projects", "", http.StatusOK, &list)
	slugs := map[string]overview{}
	for _, p := range list {
		slugs[p.Slug] = p
	}
	if len(list) != 2 || slugs["hasici"].MembersCount != 1 || slugs[project.BootstrapSlug].UID != project.BootstrapUID {
		t.Errorf("list = %+v, want the bootstrap project and hasici", list)
	}

	var one overview
	e.mustDo(t, token, http.MethodGet, "/api/v1/projects/"+created.UID, "", http.StatusOK, &one)
	if one.UID != created.UID || one.Description != "Sbor" || one.CreatedAt == "" {
		t.Errorf("GET = %+v", one)
	}
	e.expectError(t, token, http.MethodGet, "/api/v1/projects/pjmissing", "", http.StatusNotFound, "not_found")
}

// TestCreate_refusals covers every refusal of POST /projects; none of them
// leaves a project behind.
func TestCreate_refusals(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "maint", auth.AccountMaintainer)
	e.createProject(t, token, "taken")

	cases := []struct {
		name, body, code string
		status           int
	}{
		{"slug taken", `{"slug":"taken","name":"Again"}`, "slug_taken", http.StatusConflict},
		{"bootstrap slug taken", `{"slug":"v","name":"Again"}`, "slug_taken", http.StatusConflict},
		{"bad slug", `{"slug":"Bad Slug","name":"N"}`, "invalid_slug", http.StatusUnprocessableEntity},
		{"reserved slug", `{"slug":"api","name":"N"}`, "invalid_slug", http.StatusUnprocessableEntity},
		{"empty name", `{"slug":"noname","name":"  "}`, "invalid_name", http.StatusUnprocessableEntity},
		{"zero limit", `{"slug":"zero","name":"N","max_total_bytes":0}`, "invalid_limit",
			http.StatusUnprocessableEntity},
		{"unknown field", `{"slug":"odd","name":"N","owner":"me"}`, "invalid_body", http.StatusBadRequest},
	}
	for _, tc := range cases {
		e.expectError(t, token, http.MethodPost, "/api/v1/projects", tc.body, tc.status, tc.code)
	}

	_, data := e.do(t, token, http.MethodPost, "/api/v1/projects", `{"slug":"-x","name":"N"}`)
	var refusal struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &refusal); err != nil || !strings.Contains(refusal.Message, "lowercase") {
		t.Errorf("invalid_slug message = %q (%v), want the slug rule", refusal.Message, err)
	}
	var list []overview
	e.mustDo(t, token, http.MethodGet, "/api/v1/projects", "", http.StatusOK, &list)
	if len(list) != 2 {
		t.Errorf("projects after refusals = %d, want 2 (bootstrap + taken)", len(list))
	}
}

// TestUpdate_limitsSetAndClear covers PATCH /projects/{uid}: limits set, one
// cleared with null while the other stays, deactivation, and the slug refused.
func TestUpdate_limitsSetAndClear(t *testing.T) {
	e := newEnv(t)
	_, token := e.user(t, "maint", auth.AccountMaintainer)
	created := e.createProject(t, token, "limits")
	path := "/api/v1/projects/" + created.UID

	var got overview
	e.mustDo(t, token, http.MethodPatch, path, `{"max_file_bytes":500,"max_total_bytes":9000}`, http.StatusOK, &got)
	if got.MaxFileBytes == nil || *got.MaxFileBytes != 500 || got.MaxTotalBytes == nil || *got.MaxTotalBytes != 9000 {
		t.Fatalf("after setting limits = %+v", got)
	}
	e.mustDo(t, token, http.MethodPatch, path, `{"max_file_bytes":null,"description":"d"}`, http.StatusOK, &got)
	if got.MaxFileBytes != nil || got.MaxTotalBytes == nil || *got.MaxTotalBytes != 9000 || got.Description != "d" {
		t.Errorf("after clearing the file limit = %+v, want only the total limit kept", got)
	}
	e.mustDo(t, token, http.MethodPatch, path, `{"active":false}`, http.StatusOK, &got)
	if got.Active {
		t.Error("project still active after deactivation")
	}

	e.expectError(t, token, http.MethodPatch, path, `{"slug":"renamed"}`, http.StatusUnprocessableEntity,
		"slug_immutable")
	e.expectError(t, token, http.MethodPatch, path, `{"max_total_bytes":-1}`, http.StatusUnprocessableEntity,
		"invalid_limit")
	e.expectError(t, token, http.MethodPatch, path, `{"name":""}`, http.StatusUnprocessableEntity, "invalid_name")
	e.expectError(t, token, http.MethodPatch, "/api/v1/projects/pjmissing", `{"name":"N"}`,
		http.StatusNotFound, "not_found")

	if got := e.auditProjects(t, audit.ActionProjectUpdate); len(got) != 3 || got[0] != created.UID {
		t.Errorf("project.update audit project ids = %v, want three rows of %s", got, created.UID)
	}
}

// TestMembers_addAndRemove covers the maintainer's membership routes: an
// approved add, the duplicate, the bad inputs, and the last-admin guard.
func TestMembers_addAndRemove(t *testing.T) {
	e := newEnv(t)
	maint, token := e.user(t, "maint", auth.AccountMaintainer)
	guest, _ := e.user(t, "guest", auth.AccountViewer)
	created := e.createProject(t, token, "club")
	members := "/api/v1/projects/" + created.UID + "/members"

	var added struct {
		UserUID    string  `json:"user_uid"`
		Role       string  `json:"role"`
		ApprovedAt *string `json:"approved_at"`
	}
	e.mustDo(t, token, http.MethodPost, members, `{"user_uid":"`+guest.UID+`","role":"editor"}`,
		http.StatusCreated, &added)
	if added.UserUID != guest.UID || added.Role != "editor" || added.ApprovedAt == nil {
		t.Errorf("added = %+v, want an approved editor", added)
	}
	e.expectError(t, token, http.MethodPost, members, `{"user_uid":"`+guest.UID+`","role":"viewer"}`,
		http.StatusConflict, "already_member")
	e.expectError(t, token, http.MethodPost, members, `{"user_uid":"usmissing","role":"viewer"}`,
		http.StatusUnprocessableEntity, "user_not_found")
	e.expectError(t, token, http.MethodPost, members, `{"user_uid":"`+guest.UID+`","role":"maintainer"}`,
		http.StatusUnprocessableEntity, "invalid_role")
	e.expectError(t, token, http.MethodPost, "/api/v1/projects/pjmissing/members",
		`{"user_uid":"`+guest.UID+`","role":"viewer"}`, http.StatusNotFound, "not_found")

	// The creator is the club's only admin.
	e.expectError(t, token, http.MethodDelete, members+"/"+maint.UID, "", http.StatusConflict, "last_admin")
	e.mustDo(t, token, http.MethodDelete, members+"/"+guest.UID, "", http.StatusNoContent, nil)
	e.expectError(t, token, http.MethodDelete, members+"/"+guest.UID, "", http.StatusNotFound, "not_member")

	got := e.auditProjects(t, audit.ActionProjectMemberRemove)
	if len(got) != 1 || got[0] != created.UID {
		t.Errorf("project.member.remove audit project ids = %v, want [%s]", got, created.UID)
	}
}

// TestUsers_listPromoteDemote covers GET /users with memberships and the
// instance-role PATCH: promote, demote, idempotence, self-demotion, bad input.
func TestUsers_listPromoteDemote(t *testing.T) {
	e := newEnv(t)
	maint, token := e.user(t, "maint", auth.AccountMaintainer)
	editor, _ := e.user(t, "editor", auth.AccountEditor)
	e.createProject(t, token, "club")

	var list []account
	e.mustDo(t, token, http.MethodGet, "/api/v1/users", "", http.StatusOK, &list)
	byName := map[string]account{}
	for _, a := range list {
		byName[a.Username] = a
	}
	if got := byName["editor"]; got.InstanceRole != "user" || len(got.Memberships) != 1 ||
		got.Memberships[0].ProjectSlug != project.BootstrapSlug || got.Memberships[0].Role != "editor" ||
		!got.Memberships[0].Approved {
		t.Errorf("editor = %+v, want a user who edits the bootstrap project", got)
	}
	// A maintainer is created as the bootstrap project's admin, and creating
	// the club made it the club's; the list is in project-name order.
	if got := byName["maint"]; got.InstanceRole != "maintainer" || len(got.Memberships) != 2 ||
		got.Memberships[0].ProjectSlug != "club" || got.Memberships[0].Role != "admin" ||
		got.Memberships[1].ProjectSlug != project.BootstrapSlug {
		t.Errorf("maint = %+v, want a maintainer who is the club's and the bootstrap project's admin", got)
	}

	var changed account
	path := "/api/v1/users/" + editor.UID
	e.mustDo(t, token, http.MethodPatch, path, `{"instance_role":"maintainer"}`, http.StatusOK, &changed)
	if changed.InstanceRole != "maintainer" || len(changed.Memberships) != 1 {
		t.Errorf("promoted = %+v", changed)
	}
	e.mustDo(t, token, http.MethodPatch, path, `{"instance_role":"maintainer"}`, http.StatusOK, &changed)
	e.mustDo(t, token, http.MethodPatch, path, `{"instance_role":"user"}`, http.StatusOK, &changed)
	if changed.InstanceRole != "user" {
		t.Errorf("demoted = %+v", changed)
	}
	if n := len(e.auditProjects(t, audit.ActionUserInstanceRole)); n != 2 {
		t.Errorf("user.instance_role audit rows = %d, want 2 (the repeat records nothing)", n)
	}

	e.expectError(t, token, http.MethodPatch, "/api/v1/users/"+maint.UID, `{"instance_role":"user"}`,
		http.StatusConflict, "self")
	e.expectError(t, token, http.MethodPatch, path, `{"instance_role":"admin"}`,
		http.StatusUnprocessableEntity, "invalid_role")
	e.expectError(t, token, http.MethodPatch, "/api/v1/users/usmissing", `{"instance_role":"user"}`,
		http.StatusNotFound, "not_found")
	e.expectError(t, token, http.MethodPatch, path, `{"role":"user"}`, http.StatusBadRequest, "invalid_body")
}
