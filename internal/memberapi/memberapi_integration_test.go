//go:build integration

package memberapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/memberapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they do not run in parallel.

// testBaseURL stands in for mail.base_url in the approval mail's link.
const testBaseURL = "https://kukatko.example.test"

// env is one test's server, fixture and pool.
type env struct {
	server *httptest.Server
	f      *projecttest.Fixture
	pool   *pgxpool.Pool
}

// newEnv seeds the two tenants (opts as projecttest.Seed takes them) and mounts
// the real auth API, the member routes and the name directory the way the
// server does — under /api/v1/p/{project} behind auth.WithProject — with mail
// on, so an approval queues a real `mail_send` job.
func newEnv(t *testing.T, opts ...projecttest.Option) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool, opts...)
	projects := project.NewStore(pool)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := memberapi.NewAPI(memberapi.Config{
		Members:      projects,
		Accounts:     auth.NewStore(pool),
		Mail:         mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true}),
		ProjectURL:   func(slug string) string { return testBaseURL + "/p/" + slug + "/" },
		RequireAdmin: authAPI.RequireAdmin,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterInstanceRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterRoutes(r)
			authAPI.RegisterDirectoryRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, f: f, pool: pool}
}

// seedAccount creates an account with no membership at all: username, an
// address, the fixture's password, approved or still waiting. It returns the uid.
func (e *env) seedAccount(t *testing.T, username, email string, approved bool) string {
	t.Helper()
	hash, err := auth.HashPassword(projecttest.Password)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	uid := projecttest.MissingUID("us")
	user := auth.User{UID: uid, Username: username, Email: email, PasswordHash: hash, Role: auth.RoleUser}
	if approved {
		now := time.Now()
		user.ApprovedAt = &now
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

// call sends method path as client (payload encoded as JSON, none when nil)
// and returns the status, the Content-Type and the body, the response closed.
func (e *env) call(t *testing.T, client *http.Client, method, path string, payload any) (int, string, string) {
	t.Helper()
	resp := e.f.Send(t, client, method, path, payload)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
}

// TestMembers_foreignProjectIsNotFound proves an admin of A addressing B's
// members — every route — gets exactly the answer an unknown slug gets, and
// learns nothing of B's member.
func TestMembers_foreignProjectIsNotFound(t *testing.T) {
	e := newEnv(t)
	client := e.f.ClientFor(t, e.server, projecttest.SideA)
	missing := auth.APIBasePath + "/p/no-such-project/members"

	e.f.AssertInvisible(t, client, e.f.PathFor(projecttest.SideB, "members"))
	for _, tc := range []struct {
		method, suffix string
		body           any
	}{
		{http.MethodGet, "", nil},
		{http.MethodPost, "", map[string]string{"identifier": e.f.A.Username, "role": "viewer"}},
		{http.MethodPatch, "/" + e.f.B.UserUID, map[string]string{"role": "viewer"}},
		{http.MethodPost, "/" + e.f.B.UserUID + "/approve", nil},
		{http.MethodDelete, "/" + e.f.B.UserUID, nil},
	} {
		status, kind, raw := e.call(t, client, tc.method, e.f.PathFor(projecttest.SideB, "members"+tc.suffix), tc.body)
		wantStatus, wantKind, wantRaw := e.call(t, client, tc.method, missing+tc.suffix, tc.body)
		if status != http.StatusNotFound {
			t.Errorf("%s B's members%s = %d, want 404", tc.method, tc.suffix, status)
		}
		if status != wantStatus || kind != wantKind || raw != wantRaw {
			t.Errorf("%s B's members%s = %d %s %q, an unknown project %d %s %q: the answers tell them apart",
				tc.method, tc.suffix, status, kind, raw, wantStatus, wantKind, wantRaw)
		}
	}
	if _, err := project.NewStore(e.pool).Membership(t.Context(), e.f.B.ProjectUID, e.f.A.UserUID); err == nil {
		t.Error("A's admin became a member of B through B's routes")
	}
	member, err := project.NewStore(e.pool).Membership(t.Context(), e.f.B.ProjectUID, e.f.B.UserUID)
	if err != nil || member.Role != project.RoleAdmin {
		t.Errorf("B's member after the attempts = %+v, %v; want B's admin untouched", member, err)
	}
}

// TestMembers_listIsTheProjectsOwn proves the list names the project's members
// only, with the page's fields and without an e-mail address, and that a
// member who is not an admin is refused.
func TestMembers_listIsTheProjectsOwn(t *testing.T) {
	e := newEnv(t, projecttest.WithRole(projecttest.SideB, project.RoleEditor))
	client := e.f.ClientFor(t, e.server, projecttest.SideA)

	status, _, raw := e.call(t, client, http.MethodGet, e.f.PathFor(projecttest.SideA, "members"), nil)
	if status != http.StatusOK || !strings.Contains(raw, e.f.A.UserUID) {
		t.Fatalf("A's members = %d %s, want A's member listed", status, raw)
	}
	for _, key := range []string{`"username"`, `"display_name"`, `"role":"admin"`, `"approved_at"`,
		`"last_login_at"`, `"has_picture"`} {
		if !strings.Contains(raw, key) {
			t.Errorf("member list %s lacks %s", raw, key)
		}
	}
	if strings.Contains(raw, e.f.B.UserUID) || strings.Contains(raw, "@example.test") {
		t.Errorf("member list %s names B's member or an address", raw)
	}

	editor := e.f.ClientFor(t, e.server, projecttest.SideB)
	if status, _, _ := e.call(t, editor, http.MethodGet, e.f.PathFor(projecttest.SideB, "members"), nil); status !=
		http.StatusForbidden {
		t.Errorf("an editor listing members = %d, want 403", status)
	}
}

// addOutcomes reads the outcomes of the project.member.add entries of the
// project projectUID, oldest first.
func addOutcomes(t *testing.T, pool *pgxpool.Pool, projectUID string) []string {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT details->>'outcome' FROM audit_log
		WHERE action = $1 AND project_id = $2 AND details ? 'outcome' ORDER BY id`,
		audit.ActionProjectMemberAdd, projectUID)
	if err != nil {
		t.Fatalf("reading the audit trail: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var outcome string
		if err := rows.Scan(&outcome); err != nil {
			t.Fatalf("scanning an outcome: %v", err)
		}
		out = append(out, outcome)
	}
	return out
}

// TestMembers_addAnswersIdenticallyAndAuditsTheOutcome proves the add answers
// byte for byte the same for an account it adds, one already a member and one
// that does not exist (a partial name included), that the e-mail address
// matches regardless of case, and that the trail records what really happened.
func TestMembers_addAnswersIdenticallyAndAuditsTheOutcome(t *testing.T) {
	e := newEnv(t)
	client := e.f.ClientFor(t, e.server, projecttest.SideA)
	jan := e.seedAccount(t, "jan", "Jan.Novak@Example.test", false)
	path := e.f.PathFor(projecttest.SideA, "members")

	identifiers := []string{"jan.novak@EXAMPLE.test", "jan", "nobody", "ja", "jan.novak"}
	answers := make([]string, 0, len(identifiers))
	for _, identifier := range identifiers {
		status, kind, raw := e.call(t, client, http.MethodPost, path,
			map[string]string{"identifier": identifier, "role": "editor"})
		if status != http.StatusAccepted {
			t.Errorf("add %q = %d, want 202", identifier, status)
		}
		answers = append(answers, kind+" "+raw)
	}
	for _, answer := range answers[1:] {
		if answer != answers[0] {
			t.Errorf("add answered %q and %q: the answer tells them apart", answers[0], answer)
		}
	}

	member, err := project.NewStore(e.pool).Membership(t.Context(), e.f.A.ProjectUID, jan)
	if err != nil || member.Role != project.RoleEditor || member.ApprovedAt == nil {
		t.Fatalf("jan's membership = %+v, %v; want an approved editor", member, err)
	}
	account, err := auth.NewStore(e.pool).GetUserByUID(t.Context(), jan)
	if err != nil || account.ApprovedAt == nil {
		t.Errorf("jan's account after the add = approved %v (%v), want stamped", account.ApprovedAt, err)
	}
	want := []string{"added", "already_member", "no_such_account", "no_such_account", "no_such_account"}
	if got := addOutcomes(t, e.pool, e.f.A.ProjectUID); !slices.Equal(got, want) {
		t.Errorf("audited outcomes = %v, want %v", got, want)
	}
}

// TestMembers_addRefusesAnAmbiguousAddress proves an address two accounts share
// adds nobody, still answering 202.
func TestMembers_addRefusesAnAmbiguousAddress(t *testing.T) {
	e := newEnv(t)
	client := e.f.ClientFor(t, e.server, projecttest.SideA)
	one := e.seedAccount(t, "one", "shared@example.test", true)
	e.seedAccount(t, "two", "Shared@example.test", true)

	status, _, _ := e.call(t, client, http.MethodPost, e.f.PathFor(projecttest.SideA, "members"),
		map[string]string{"identifier": "shared@example.test", "role": "viewer"})
	if status != http.StatusAccepted {
		t.Errorf("add of a shared address = %d, want 202", status)
	}
	if _, err := project.NewStore(e.pool).Membership(t.Context(), e.f.A.ProjectUID, one); err == nil {
		t.Error("an ambiguous address added an account")
	}
	if got := addOutcomes(t, e.pool, e.f.A.ProjectUID); !slices.Equal(got, []string{"ambiguous"}) {
		t.Errorf("audited outcomes = %v, want [ambiguous]", got)
	}
}

// TestMembers_roleRules proves an admin changes somebody else's role, never
// their own (409 self), and that the project's only admin cannot be demoted or
// removed (409 last_admin) — shown by a system administrator, who enters B as
// its admin without being a member.
func TestMembers_roleRules(t *testing.T) {
	e := newEnv(t, projecttest.AsMaintainer(projecttest.SideA))
	client := e.f.ClientFor(t, e.server, projecttest.SideA)
	eva := e.seedAccount(t, "eva", "eva@example.test", true)
	e.addMember(t, e.f.A.ProjectUID, eva, project.RoleViewer, true)

	status, _, raw := e.call(t, client, http.MethodPatch, e.f.PathFor(projecttest.SideA, "members/"+e.f.A.UserUID),
		map[string]string{"role": "viewer"})
	if status != http.StatusConflict || !strings.Contains(raw, `"error":"self"`) {
		t.Errorf("own role change = %d %s, want 409 self", status, raw)
	}
	status, _, raw = e.call(t, client, http.MethodPatch, e.f.PathFor(projecttest.SideA, "members/"+eva),
		map[string]string{"role": "curator"})
	if status != http.StatusOK || !strings.Contains(raw, `"role":"curator"`) {
		t.Errorf("eva's role change = %d %s, want 200 curator", status, raw)
	}
	status, _, _ = e.call(t, client, http.MethodPatch, e.f.PathFor(projecttest.SideA, "members/"+eva),
		map[string]string{"role": "maintainer"})
	if status != http.StatusUnprocessableEntity {
		t.Errorf("a maintainer project role = %d, want 422", status)
	}

	lastAdmin := e.f.PathFor(projecttest.SideB, "members/"+e.f.B.UserUID)
	for _, tc := range []struct {
		method string
		body   any
	}{
		{http.MethodPatch, map[string]string{"role": "editor"}},
		{http.MethodDelete, nil},
	} {
		status, _, raw := e.call(t, client, tc.method, lastAdmin, tc.body)
		if status != http.StatusConflict || !strings.Contains(raw, "last_admin") {
			t.Errorf("%s B's only admin = %d %s, want 409 last_admin", tc.method, status, raw)
		}
	}
}

// TestMembers_removeSelfOnlyWhileAnotherAdminRemains proves the only admin
// cannot leave (409 self), and leaves — losing the project — once another
// admin is there.
func TestMembers_removeSelfOnlyWhileAnotherAdminRemains(t *testing.T) {
	e := newEnv(t)
	client := e.f.ClientFor(t, e.server, projecttest.SideA)
	self := e.f.PathFor(projecttest.SideA, "members/"+e.f.A.UserUID)

	status, _, raw := e.call(t, client, http.MethodDelete, self, nil)
	if status != http.StatusConflict || !strings.Contains(raw, `"error":"self"`) {
		t.Errorf("the only admin leaving = %d %s, want 409 self", status, raw)
	}

	other := e.seedAccount(t, "druhy", "druhy@example.test", true)
	e.addMember(t, e.f.A.ProjectUID, other, project.RoleAdmin, true)
	if status, _, raw := e.call(t, client, http.MethodDelete, self, nil); status != http.StatusNoContent {
		t.Fatalf("leaving beside another admin = %d %s, want 204", status, raw)
	}
	if status, _, _ := e.call(t, client, http.MethodGet, e.f.PathFor(projecttest.SideA, "members"), nil); status !=
		http.StatusNotFound {
		t.Errorf("the project after leaving = %d, want 404", status)
	}
}

// mailJob is the part of a queued `mail_send` payload these tests read; the
// data is the template's fields by their Go names.
type mailJob struct {
	Template string            `json:"template"`
	To       string            `json:"to"`
	Data     map[string]string `json:"data"`
}

// approvalMails returns the queued approval mails, oldest first.
func approvalMails(t *testing.T, pool *pgxpool.Pool) []mailJob {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT payload FROM jobs WHERE type = 'mail_send' ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the mail queue: %v", err)
	}
	defer rows.Close()
	var out []mailJob
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scanning a mail job: %v", err)
		}
		var job mailJob
		if err := json.Unmarshal(raw, &job); err != nil {
			t.Fatalf("decoding %s: %v", raw, err)
		}
		if job.Template == mailer.TemplateAccountApproved {
			out = append(out, job)
		}
	}
	return out
}

// TestMembers_approveLetsInAndMailsTheProject proves approving a waiting member
// stamps the membership and the account, queues one mail naming the project
// and linking to its page, and that approving again changes and mails nothing.
func TestMembers_approveLetsInAndMailsTheProject(t *testing.T) {
	e := newEnv(t)
	client := e.f.ClientFor(t, e.server, projecttest.SideB)
	petr := e.seedAccount(t, "petr", "petr@example.test", false)
	e.addMember(t, e.f.B.ProjectUID, petr, project.RoleViewer, false)
	path := e.f.PathFor(projecttest.SideB, "members/"+petr+"/approve")

	for range 2 {
		status, _, raw := e.call(t, client, http.MethodPost, path, nil)
		if status != http.StatusOK || strings.Contains(raw, `"approved_at":null`) {
			t.Fatalf("approve = %d %s, want 200 approved", status, raw)
		}
	}
	account, err := auth.NewStore(e.pool).GetUserByUID(t.Context(), petr)
	if err != nil || account.ApprovedAt == nil {
		t.Errorf("petr's account = approved %v (%v), want stamped so the account can sign in", account.ApprovedAt, err)
	}
	mails := approvalMails(t, e.pool)
	if len(mails) != 1 {
		t.Fatalf("approval mails = %+v, want exactly one", mails)
	}
	got := mails[0]
	wantURL := testBaseURL + "/p/" + e.f.B.Slug + "/"
	if got.To != "petr@example.test" || got.Data["ProjectName"] != "Projekt "+e.f.B.Slug ||
		got.Data["ProjectURL"] != wantURL {
		t.Errorf("approval mail = %+v, want to petr naming B and linking %s", got, wantURL)
	}
	status, _, _ := e.call(t, client, http.MethodPost,
		e.f.PathFor(projecttest.SideB, "members/"+projecttest.MissingUID("us")+"/approve"), nil)
	if status != http.StatusNotFound {
		t.Errorf("approving a non-member = %d, want 404", status)
	}
}

// directoryUIDs reads the name directory of which as client.
func (e *env) directoryUIDs(t *testing.T, client *http.Client, which projecttest.Side) []string {
	t.Helper()
	status, _, raw := e.call(t, client, http.MethodGet, e.f.PathFor(which, "users"), nil)
	if status != http.StatusOK {
		t.Fatalf("GET users of %s = %d %s", which, status, raw)
	}
	var out struct {
		Users []auth.DirectoryEntry `json:"users"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	uids := make([]string, 0, len(out.Users))
	for _, u := range out.Users {
		uids = append(uids, u.UID)
	}
	return uids
}

// TestDirectory_listsOnlyTheProjectsApprovedMembers proves the name directory
// of a project names its approved members only: never another project's
// member, never one still waiting — until a project admin approves them.
func TestDirectory_listsOnlyTheProjectsApprovedMembers(t *testing.T) {
	e := newEnv(t)
	client := e.f.ClientFor(t, e.server, projecttest.SideA)
	waiting := e.seedAccount(t, "cekatel", "cekatel@example.test", false)
	e.addMember(t, e.f.A.ProjectUID, waiting, project.RoleViewer, false)
	outsider := e.seedAccount(t, "outsider", "outsider@example.test", true)

	uids := e.directoryUIDs(t, client, projecttest.SideA)
	if !slices.Contains(uids, e.f.A.UserUID) {
		t.Errorf("directory %v lacks A's own member", uids)
	}
	for _, foreign := range []string{e.f.B.UserUID, waiting, outsider} {
		if slices.Contains(uids, foreign) {
			t.Errorf("directory %v names %s, who is not an approved member of A", uids, foreign)
		}
	}

	path := e.f.PathFor(projecttest.SideA, "members/"+waiting+"/approve")
	if status, _, raw := e.call(t, client, http.MethodPost, path, nil); status != http.StatusOK {
		t.Fatalf("approve = %d %s", status, raw)
	}
	if uids := e.directoryUIDs(t, client, projecttest.SideA); !slices.Contains(uids, waiting) {
		t.Errorf("directory %v after the approval lacks the approved member", uids)
	}
}
