//go:build integration

package auth_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/obs"
	"github.com/panbotka/kukatko/internal/project"
)

// projectEnv is an httpEnv plus a second server whose routes carry the
// {project} URL parameter, the way the next step mounts every project route, and
// the project store to arrange memberships with.
type projectEnv struct {
	*httpEnv
	projects *project.Store
	server   *httptest.Server
}

// projectProbe answers what the guard decided: the request's project and the
// caller's role in it.
type projectProbe struct {
	ProjectUID string `json:"project_uid"`
	Role       string `json:"role"`
	APIBase    string `json:"api_base"`
	// LoggedProject is the project uid the access log and audit.FromRequest
	// see (obs.ProjectUID).
	LoggedProject string `json:"logged_project"`
}

// writeProjectProbe is the handler behind the probe routes.
func writeProjectProbe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projectProbe{
		ProjectUID: auth.ProjectUIDFromContext(r.Context()),
		Role:       string(auth.ProjectRoleFromContext(r.Context())),
		APIBase:    auth.APIBase(r.Context()),

		LoggedProject: obs.ProjectUID(r.Context()),
	})
}

// newProjectEnv builds the environment the way the server is wired:
// /api/v1/p/{project} names its project through WithProject, /api/v1 names
// none (the probes are mounted there too, to prove a guard there has no
// project to fall back to).
func newProjectEnv(t *testing.T) *projectEnv {
	t.Helper()
	base := newHTTPEnv(t, 50)
	projects := project.NewStore(base.db.Pool())
	api := auth.NewAPI(auth.APIConfig{Service: base.svc, Limiter: auth.NewLimiter(50, time.Minute)})

	probes := func(r chi.Router) {
		r.With(api.RequireAuth).Get("/probe/auth", writeProjectProbe)
		r.With(api.RequireAdmin).Get("/probe/admin", writeProjectProbe)
		r.With(api.RequireMaintainer).Get("/probe/maintain", writeProjectProbe)
		r.With(api.RequireAuthOrDownloadToken).Get("/probe/media", writeProjectProbe)
	}
	r := chi.NewRouter()
	r.Use(obs.AccessLog(slog.New(slog.DiscardHandler)))
	r.Route("/api/v1", func(r chi.Router) {
		api.RegisterInstanceRoutes(r)
		probes(r)
		r.Route("/p/{project}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			probes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &projectEnv{httpEnv: base, projects: projects, server: server}
}

// get issues a GET against the project server as client.
func (e *projectEnv) get(t *testing.T, client *http.Client, path string) (int, []byte) {
	t.Helper()
	other := *e.httpEnv
	other.server = e.server
	return other.do(t, client, http.MethodGet, path, "")
}

// signIn creates username with role and returns a client signed in on the
// project server.
func (e *projectEnv) signIn(t *testing.T, username string, role auth.AccountRole) (*http.Client, auth.User) {
	t.Helper()
	user := e.mustCreate(t, username, role)
	other := *e.httpEnv
	other.server = e.server
	return other.loginClient(t, username), user
}

// createProject makes a project with slug next to the bootstrap one.
func (e *projectEnv) createProject(t *testing.T, slug string) *project.Project {
	t.Helper()
	p, err := e.projects.Create(t.Context(), audit.Meta{}, project.CreateParams{Slug: slug, Name: "Projekt " + slug})
	if err != nil {
		t.Fatalf("creating project %q: %v", slug, err)
	}
	return p
}

// addMember makes user a member of p with role, approved or not.
func (e *projectEnv) addMember(t *testing.T, p *project.Project, user auth.User, role project.Role, approved bool) {
	t.Helper()
	if _, err := e.projects.AddMember(t.Context(), audit.Meta{}, p.UID, user.UID, role, approved); err != nil {
		t.Fatalf("adding %s to %s: %v", user.Username, p.Slug, err)
	}
}

// decodeProbe decodes a probe answer.
func decodeProbe(t *testing.T, body []byte) projectProbe {
	t.Helper()
	var p projectProbe
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decoding probe %s: %v", body, err)
	}
	return p
}

// TestProject_noDefaultProject proves there is no default project: a
// project-scoped guard on a route that names none is a wiring bug and answers
// 500 — never the bootstrap project — while the instance-level guard needs no
// project and checks the instance role alone.
func TestProject_noDefaultProject(t *testing.T) {
	env := newProjectEnv(t)
	client, _ := env.signIn(t, "eva", auth.AccountAdmin)
	maint, _ := env.signIn(t, "ops", auth.AccountMaintainer)

	for _, path := range []string{"/api/v1/probe/auth", "/api/v1/probe/admin", "/api/v1/probe/media"} {
		if status, body := env.get(t, maint, path); status != http.StatusInternalServerError {
			t.Errorf("%s without a project = %d %s, want 500", path, status, body)
		}
	}
	status, body := env.get(t, maint, "/api/v1/probe/maintain")
	if status != http.StatusOK {
		t.Fatalf("instance-level probe = %d, body %s", status, body)
	}
	if got := decodeProbe(t, body); got.ProjectUID != "" || got.Role != "" || got.APIBase != "/api/v1" {
		t.Errorf("instance-level probe = %+v, want no project at all", got)
	}
	if status, _ := env.get(t, client, "/api/v1/probe/maintain"); status != http.StatusForbidden {
		t.Errorf("instance-level probe as a project admin = %d, want 403", status)
	}
	if status, _ := env.get(t, client, "/api/v1/p/v/probe/auth"); status != http.StatusOK {
		t.Errorf("the bootstrap project by slug = %d, want 200", status)
	}
}

// TestProject_apiBaseFollowsTheRoute proves the API base a handler builds its
// links on is the prefix the request came through.
func TestProject_apiBaseFollowsTheRoute(t *testing.T) {
	env := newProjectEnv(t)
	client, _ := env.signIn(t, "eva", auth.AccountEditor)

	for path, want := range map[string]string{
		"/api/v1/p/v/probe/auth":  "/api/v1/p/v",
		"/api/v1/p/v/probe/media": "/api/v1/p/v",
	} {
		status, body := env.get(t, client, path)
		if status != http.StatusOK {
			t.Fatalf("%s = %d, body %s", path, status, body)
		}
		if got := decodeProbe(t, body); got.APIBase != want || got.ProjectUID != project.BootstrapUID {
			t.Errorf("%s probe = %+v, want api_base %q in the bootstrap project", path, got, want)
		} else if got.LoggedProject != project.BootstrapUID {
			t.Errorf("%s logged project = %q, want the bootstrap project", path, got.LoggedProject)
		}
	}
}

// TestProject_foreignIsIndistinguishableFromUnknown proves a tenant cannot
// learn another project exists: a project it is no member of answers exactly
// what a slug nobody uses answers.
func TestProject_foreignIsIndistinguishableFromUnknown(t *testing.T) {
	env := newProjectEnv(t)
	env.createProject(t, "alfa")
	client, _ := env.signIn(t, "eva", auth.AccountAdmin)

	foreignStatus, foreignBody := env.get(t, client, "/api/v1/p/alfa/probe/auth")
	unknownStatus, unknownBody := env.get(t, client, "/api/v1/p/nikde/probe/auth")
	if foreignStatus != http.StatusNotFound || unknownStatus != http.StatusNotFound {
		t.Fatalf("foreign = %d, unknown = %d, want 404 for both", foreignStatus, unknownStatus)
	}
	if string(foreignBody) != string(unknownBody) {
		t.Errorf("foreign body %s differs from unknown body %s", foreignBody, unknownBody)
	}
	// The media guard applies the same rule.
	if status, body := env.get(t, client, "/api/v1/p/alfa/probe/media"); status != http.StatusNotFound ||
		string(body) != string(unknownBody) {
		t.Errorf("media guard on a foreign project = %d %s, want the same 404", status, body)
	}
}

// TestProject_anonymousGets401Regardless proves an anonymous caller learns
// nothing either: 401 for an existing and an unknown project alike.
func TestProject_anonymousGets401Regardless(t *testing.T) {
	env := newProjectEnv(t)
	env.createProject(t, "alfa")
	for _, path := range []string{"/api/v1/p/alfa/probe/auth", "/api/v1/p/nikde/probe/auth"} {
		if status, _ := env.get(t, newClient(t), path); status != http.StatusUnauthorized {
			t.Errorf("anonymous %s = %d, want 401", path, status)
		}
	}
}

// TestProject_pendingMembershipIs403 proves a member nobody has approved yet
// gets 403 pending_approval.
func TestProject_pendingMembershipIs403(t *testing.T) {
	env := newProjectEnv(t)
	alfa := env.createProject(t, "alfa")
	client, user := env.signIn(t, "eva", auth.AccountViewer)
	env.addMember(t, alfa, user, project.RoleViewer, false)

	status, body := env.get(t, client, "/api/v1/p/alfa/probe/auth")
	if status != http.StatusForbidden {
		t.Fatalf("pending member = %d, want 403 (body %s)", status, body)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &got); err != nil || got.Error != auth.ErrorPendingApproval {
		t.Errorf("body = %s, want {\"error\":%q}", body, auth.ErrorPendingApproval)
	}
}

// TestProject_inactiveProject proves a deactivated project is 404 for its own
// members and still open to a maintainer.
func TestProject_inactiveProject(t *testing.T) {
	env := newProjectEnv(t)
	alfa := env.createProject(t, "alfa")
	member, user := env.signIn(t, "eva", auth.AccountViewer)
	env.addMember(t, alfa, user, project.RoleEditor, true)
	maint, _ := env.signIn(t, "ops", auth.AccountMaintainer)

	if status, body := env.get(t, member, "/api/v1/p/alfa/probe/auth"); status != http.StatusOK {
		t.Fatalf("member of an active project = %d (body %s)", status, body)
	}
	inactive := false
	if _, err := env.projects.Update(t.Context(), audit.Meta{}, alfa.UID, project.UpdatePatch{Active: &inactive}); err != nil {
		t.Fatalf("deactivating: %v", err)
	}
	if status, _ := env.get(t, member, "/api/v1/p/alfa/probe/auth"); status != http.StatusNotFound {
		t.Errorf("member of a deactivated project = %d, want 404", status)
	}
	if status, body := env.get(t, maint, "/api/v1/p/alfa/probe/auth"); status != http.StatusOK {
		t.Errorf("maintainer in a deactivated project = %d, want 200 (body %s)", status, body)
	}
}

// TestProject_maintainerIsAdminEverywhere proves a maintainer enters a project
// it is no member of as its admin, while the instance guard still reads the
// instance role alone.
func TestProject_maintainerIsAdminEverywhere(t *testing.T) {
	env := newProjectEnv(t)
	alfa := env.createProject(t, "alfa")
	maint, _ := env.signIn(t, "ops", auth.AccountMaintainer)
	admin, adminUser := env.signIn(t, "boss", auth.AccountAdmin)
	env.addMember(t, alfa, adminUser, project.RoleAdmin, true)

	status, body := env.get(t, maint, "/api/v1/p/alfa/probe/admin")
	if status != http.StatusOK {
		t.Fatalf("maintainer admin probe = %d (body %s)", status, body)
	}
	if got := decodeProbe(t, body); got.ProjectUID != alfa.UID || got.Role != string(project.RoleAdmin) {
		t.Errorf("maintainer probe = %+v, want alfa as admin", got)
	}
	if status, _ := env.get(t, maint, "/api/v1/p/alfa/probe/maintain"); status != http.StatusOK {
		t.Errorf("maintainer on the instance guard = %d, want 200", status)
	}
	// A project admin is no maintainer.
	if status, _ := env.get(t, admin, "/api/v1/p/alfa/probe/maintain"); status != http.StatusForbidden {
		t.Errorf("project admin on the instance guard = %d, want 403", status)
	}
}

// TestProject_lastProjectRemembered proves entering a project remembers it as
// the account's last one.
func TestProject_lastProjectRemembered(t *testing.T) {
	env := newProjectEnv(t)
	alfa := env.createProject(t, "alfa")
	client, user := env.signIn(t, "eva", auth.AccountViewer)
	env.addMember(t, alfa, user, project.RoleViewer, true)

	if status, _ := env.get(t, client, "/api/v1/p/alfa/probe/auth"); status != http.StatusOK {
		t.Fatalf("entering alfa = %d", status)
	}
	slug, err := env.store.LastProjectSlug(t.Context(), user.UID)
	if err != nil || slug != "alfa" {
		t.Errorf("LastProjectSlug = %q, %v, want alfa", slug, err)
	}
}

// lastProjectOf reads last_project_slug from GET /auth/me as client.
func (e *projectEnv) lastProjectOf(t *testing.T, client *http.Client) string {
	t.Helper()
	status, body := e.get(t, client, "/api/v1/auth/me")
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d (body %s)", status, body)
	}
	var me meBody
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	return me.LastProjectSlug
}

// TestProject_meForgetsAProjectItCannotEnter proves GET /auth/me names the
// last project only while the account can still enter it: removed from it,
// waiting for approval in it, or locked out by its deactivation, the account is
// told nothing — while a maintainer, who enters even a deactivated project,
// keeps being pointed at it.
func TestProject_meForgetsAProjectItCannotEnter(t *testing.T) {
	env := newProjectEnv(t)
	alfa := env.createProject(t, "alfa")
	client, user := env.signIn(t, "eva", auth.AccountViewer)
	env.addMember(t, alfa, user, project.RoleViewer, true)
	if status, _ := env.get(t, client, "/api/v1/p/alfa/probe/auth"); status != http.StatusOK {
		t.Fatalf("entering alfa = %d", status)
	}
	if got := env.lastProjectOf(t, client); got != "alfa" {
		t.Fatalf("last project while a member = %q, want alfa", got)
	}
	pool := env.db.Pool()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	exec(`DELETE FROM project_members WHERE project_uid = $1 AND user_uid = $2`, alfa.UID, user.UID)
	if got := env.lastProjectOf(t, client); got != "" {
		t.Errorf("last project after the removal = %q, want none", got)
	}
	env.addMember(t, alfa, user, project.RoleViewer, false)
	if got := env.lastProjectOf(t, client); got != "" {
		t.Errorf("last project while waiting for approval = %q, want none", got)
	}
	exec(`UPDATE project_members SET approved_at = now() WHERE project_uid = $1 AND user_uid = $2`,
		alfa.UID, user.UID)
	exec(`UPDATE projects SET active = false WHERE uid = $1`, alfa.UID)
	if got := env.lastProjectOf(t, client); got != "" {
		t.Errorf("last project after the deactivation = %q, want none", got)
	}

	maint, _ := env.signIn(t, "ops", auth.AccountMaintainer)
	if status, _ := env.get(t, maint, "/api/v1/p/alfa/probe/auth"); status != http.StatusOK {
		t.Fatalf("maintainer entering the deactivated alfa = %d", status)
	}
	if got := env.lastProjectOf(t, maint); got != "alfa" {
		t.Errorf("maintainer's last project = %q, want the deactivated alfa it enters", got)
	}
}

// meBody is the part of GET /auth/me these tests read.
type meBody struct {
	// User is decoded as a map so a test can assert a key is absent: the
	// account record carries no project-shaped "role" or "subject_uid".
	User            map[string]any `json:"user"`
	InstanceRole    string         `json:"instance_role"`
	LastProjectSlug string         `json:"last_project_slug"`
	Projects        []struct {
		UID        string  `json:"uid"`
		Slug       string  `json:"slug"`
		Name       string  `json:"name"`
		Active     bool    `json:"active"`
		Role       string  `json:"role"`
		Approved   bool    `json:"approved"`
		SubjectUID *string `json:"subject_uid"`
	} `json:"projects"`
}

// TestProject_meShape pins GET /auth/me: the instance role, every membership
// with its role, approval and "this is me" person, the remembered last
// project, and a user object that is the bare account record — its instance
// role, but no "role" and no "subject_uid", which exist only per membership.
func TestProject_meShape(t *testing.T) {
	env := newProjectEnv(t)
	alfa := env.createProject(t, "alfa")
	client, user := env.signIn(t, "eva", auth.AccountCurator)
	env.addMember(t, alfa, user, project.RoleAdmin, false)
	// /auth/me enters no project; visiting one is what remembers it.
	if status, body := env.get(t, client, "/api/v1/p/v/probe/auth"); status != http.StatusOK {
		t.Fatalf("visiting v = %d %s", status, body)
	}
	if _, err := env.db.Pool().Exec(t.Context(), `INSERT INTO subjects (uid, project_id, slug, name, type)
		VALUES ('sub_eva', $1, 'eva', 'Eva', 'person')`, project.BootstrapUID); err != nil {
		t.Fatalf("seeding a subject: %v", err)
	}
	if _, err := env.db.Pool().Exec(t.Context(), `UPDATE project_members SET subject_uid = 'sub_eva'
		WHERE project_uid = $1 AND user_uid = $2`, project.BootstrapUID, user.UID); err != nil {
		t.Fatalf("linking eva in v: %v", err)
	}

	status, body := env.get(t, client, "/api/v1/auth/me")
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d (body %s)", status, body)
	}
	var me meBody
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	if me.InstanceRole != "user" || me.User["instance_role"] != "user" || me.LastProjectSlug != project.BootstrapSlug {
		t.Errorf("instance_role=%q user.instance_role=%v last=%q, want user/user/v",
			me.InstanceRole, me.User["instance_role"], me.LastProjectSlug)
	}
	assertNoProjectKeys(t, "curator /auth/me user", me.User)
	if len(me.Projects) != 2 {
		t.Fatalf("projects = %+v, want two memberships", me.Projects)
	}
	byslug := map[string]int{}
	for i, p := range me.Projects {
		byslug[p.Slug] = i
	}
	if p := me.Projects[byslug["alfa"]]; p.UID != alfa.UID || p.Role != "admin" || p.Approved || !p.Active ||
		p.SubjectUID != nil {
		t.Errorf("alfa membership = %+v, want an unapproved admin of an active project, nobody linked", p)
	}
	if p := me.Projects[byslug["v"]]; p.UID != project.BootstrapUID || p.Role != "curator" || !p.Approved ||
		p.SubjectUID == nil || *p.SubjectUID != "sub_eva" {
		t.Errorf("bootstrap membership = %+v, want an approved curator linked to sub_eva", p)
	}

	maint, _ := env.signIn(t, "ops", auth.AccountMaintainer)
	_, body = env.get(t, maint, "/api/v1/auth/me")
	me = meBody{} // a fresh map, so a key the first answer had cannot linger
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decoding %s: %v", body, err)
	}
	if me.InstanceRole != "maintainer" || me.User["instance_role"] != "maintainer" {
		t.Errorf("maintainer /auth/me instance_role=%q user.instance_role=%v, want maintainer twice",
			me.InstanceRole, me.User["instance_role"])
	}
	assertNoProjectKeys(t, "maintainer /auth/me user", me.User)
}

// TestProject_registrationCreatesAPendingMembership proves a self-service
// registration creates a plain user with an unapproved viewer membership of the
// bootstrap project, and that approving the membership approves the account.
func TestProject_registrationCreatesAPendingMembership(t *testing.T) {
	env := newHTTPEnv(t, 10)
	projects := project.NewStore(env.db.Pool())
	env.mustCreate(t, "boss", auth.AccountAdmin)
	waiting := waitingAccount(t, env, "newcomer")

	if waiting.Role != auth.RoleUser {
		t.Errorf("registered instance role = %q, want user", waiting.Role)
	}
	member, err := projects.Membership(t.Context(), project.BootstrapUID, waiting.UID)
	if err != nil {
		t.Fatalf("reading the membership: %v", err)
	}
	if member.Role != project.RoleViewer || member.ApprovedAt != nil {
		t.Errorf("membership = %s approved %v, want an unapproved viewer", member.Role, member.ApprovedAt)
	}

	approve(t, env, waiting.UID)
	member, err = projects.Membership(t.Context(), project.BootstrapUID, waiting.UID)
	if err != nil {
		t.Fatalf("reading the membership: %v", err)
	}
	if member.ApprovedAt == nil {
		t.Error("approving left the membership pending")
	}
	user, err := env.store.GetUserByUID(t.Context(), waiting.UID)
	if err != nil || user.ApprovedAt == nil {
		t.Errorf("account approval after the membership's = %v (%v), want derived from it", user.ApprovedAt, err)
	}
}

// TestProject_adminUpdateLeavesTheMembership proves the system administrator's
// account update speaks only the instance role: neither a plain update nor a
// promotion to maintainer moves the account's membership of the bootstrap
// project, which is that project admin's to change.
func TestProject_adminUpdateLeavesTheMembership(t *testing.T) {
	env := newHTTPEnv(t, 10)
	projects := project.NewStore(env.db.Pool())
	maintUser := env.mustCreate(t, "ops", auth.AccountMaintainer)
	target := env.mustCreate(t, "eva", auth.AccountViewer)

	updated, err := env.svc.UpdateUserAudited(t.Context(), target.UID,
		auth.UpdateUserInput{Email: target.Email, InstanceRole: auth.RoleUser}, maintUser.Role,
		audit.Meta{ActorUID: maintUser.UID}.Entry(audit.ActionUserUpdate, "users", "", nil))
	if err != nil {
		t.Fatalf("UpdateUserAudited: %v", err)
	}
	if updated.Role != auth.RoleUser {
		t.Errorf("updated instance role = %q, want user", updated.Role)
	}
	member, err := projects.Membership(t.Context(), project.BootstrapUID, target.UID)
	if err != nil || member.Role != project.RoleViewer {
		t.Errorf("membership after update = %+v (%v), want viewer (untouched)", member, err)
	}

	promoted, err := env.svc.UpdateUserAudited(t.Context(), target.UID,
		auth.UpdateUserInput{Email: target.Email, InstanceRole: auth.RoleMaintainer}, maintUser.Role,
		audit.Meta{ActorUID: maintUser.UID}.Entry(audit.ActionUserUpdate, "users", "", nil))
	if err != nil {
		t.Fatalf("promoting: %v", err)
	}
	if promoted.Role != auth.RoleMaintainer {
		t.Errorf("promoted instance role = %q, want maintainer", promoted.Role)
	}
	member, err = projects.Membership(t.Context(), project.BootstrapUID, target.UID)
	if err != nil || member.Role != project.RoleViewer {
		t.Errorf("membership after promotion = %+v (%v), want viewer (untouched)", member, err)
	}
}
