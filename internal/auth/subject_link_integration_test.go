//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
)

// linkEnv is an httptest server over the auth API together with the pool, so a
// test can create and delete the subjects the accounts point at.
type linkEnv struct {
	*httpEnv
	pool *pgxpool.Pool
}

// newLinkEnv builds the environment for the account↔person link tests: the same
// auth routes newHTTPEnv mounts (the link itself in the bootstrap project, see
// apiV), plus the database handle.
func newLinkEnv(t *testing.T) *linkEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	api := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})

	trustedSet, err := clientip.ParseSet(nil)
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}
	r := chi.NewRouter()
	r.Use(clientip.Middleware(trustedSet))
	r.Route("/api/v1", func(r chi.Router) {
		api.RegisterInstanceRoutes(r)
		// PUT /auth/subject is project-scoped — who you are is a fact about one
		// project's people — so it is mounted as the server mounts it.
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(db.Pool())))
			api.RegisterProjectRoutes(r)
		})
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &linkEnv{httpEnv: &httpEnv{server: server, svc: svc}, pool: db.Pool()}
}

// addSubject inserts a person into the library and returns its UID.
func (e *linkEnv) addSubject(t *testing.T, uid, name string) string {
	t.Helper()
	_, err := e.pool.Exec(t.Context(),
		`INSERT INTO subjects (uid, slug, name, type, project_id) VALUES ($1, $2, $3, 'person', $4)`,
		uid, name, name, project.BootstrapUID)
	if err != nil {
		t.Fatalf("inserting subject %s: %v", uid, err)
	}
	return uid
}

// deleteSubject removes a person from the library, as the people API does.
func (e *linkEnv) deleteSubject(t *testing.T, uid string) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(), `DELETE FROM subjects WHERE uid = $1`, uid); err != nil {
		t.Fatalf("deleting subject %s: %v", uid, err)
	}
}

// seedUser creates an account with the given role and returns it.
func (e *linkEnv) seedUser(t *testing.T, username string, role auth.AccountRole) auth.User {
	t.Helper()
	user, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: "correct horse battery", Role: role,
	})
	if err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return user
}

// signIn logs username in and returns a client carrying its session cookie.
func (e *linkEnv) signIn(t *testing.T, username string) *http.Client {
	t.Helper()
	client := newClient(t)
	status, body := e.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON(username, "correct horse battery"))
	if status != http.StatusOK {
		t.Fatalf("login %s = %d, body %s", username, status, body)
	}
	return client
}

// meSubject reads the caller's own linked subject in the bootstrap project off
// GET /auth/me — its membership there, which is where the client gets it from.
func (e *linkEnv) meSubject(t *testing.T, client *http.Client) *string {
	t.Helper()
	status, body := e.do(t, client, http.MethodGet, "/api/v1/auth/me", "")
	if status != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, body %s", status, body)
	}
	var resp struct {
		Projects []struct {
			Slug       string  `json:"slug"`
			SubjectUID *string `json:"subject_uid"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decoding /auth/me: %v", err)
	}
	for _, p := range resp.Projects {
		if p.Slug == project.BootstrapSlug {
			return p.SubjectUID
		}
	}
	t.Fatalf("GET /auth/me lists no membership of %q: %s", project.BootstrapSlug, body)
	return nil
}

// TestSubjectLink_selfService covers the account page's whole loop: a user says
// which person they are, reads it back off /auth/me, and takes it back again.
func TestSubjectLink_selfService(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_self", "Babička")
	env.seedUser(t, "viewer", auth.AccountViewer)
	client := env.signIn(t, "viewer")

	if got := env.meSubject(t, client); got != nil {
		t.Fatalf("a fresh account is linked to %q, want no link", *got)
	}

	status, body := env.do(t, client, http.MethodPut, apiV+"/auth/subject",
		`{"subject_uid":"sub_self"}`)
	if status != http.StatusOK {
		t.Fatalf("PUT /auth/subject = %d, body %s", status, body)
	}
	// The answer is the account as this project sees it: the link just written.
	if linked := decodeUser(t, body); linked["subject_uid"] != "sub_self" || linked["instance_role"] != "user" {
		t.Errorf("PUT /auth/subject answered subject_uid=%v instance_role=%v, want sub_self/user",
			linked["subject_uid"], linked["instance_role"])
	}
	if got := env.meSubject(t, client); got == nil || *got != "sub_self" {
		t.Fatalf("after linking, /auth/me subject = %v, want sub_self", got)
	}

	// Clearing it is how a user says "that is not me after all".
	if status, body = env.do(t, client, http.MethodPut, apiV+"/auth/subject",
		`{"subject_uid":null}`); status != http.StatusOK {
		t.Fatalf("PUT /auth/subject (clear) = %d, body %s", status, body)
	}
	if got := env.meSubject(t, client); got != nil {
		t.Fatalf("after clearing, /auth/me subject = %q, want no link", *got)
	}
}

// TestSubjectLink_unknownSubjectIsRejected verifies a UID that names nobody is a
// bad request rather than a stored dangling pointer.
func TestSubjectLink_unknownSubjectIsRejected(t *testing.T) {
	env := newLinkEnv(t)
	env.seedUser(t, "viewer", auth.AccountViewer)
	client := env.signIn(t, "viewer")

	status, body := env.do(t, client, http.MethodPut, apiV+"/auth/subject",
		`{"subject_uid":"sub_nope"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("PUT /auth/subject with an unknown person = %d, body %s", status, body)
	}
	if got := env.meSubject(t, client); got != nil {
		t.Fatalf("a rejected link stored %q, want no link", *got)
	}
}

// TestSubjectLink_requiresAuthentication verifies the self-service route is not
// a way for an anonymous caller to write to somebody's account.
func TestSubjectLink_requiresAuthentication(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_self", "Babička")

	status, _ := env.do(t, newClient(t), http.MethodPut, apiV+"/auth/subject",
		`{"subject_uid":"sub_self"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("PUT /auth/subject unauthenticated = %d, want 401", status)
	}
}

// TestSubjectLink_sharedBetweenAccounts verifies two accounts may name the same
// person: a household login and a personal one are both legitimately her.
func TestSubjectLink_sharedBetweenAccounts(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_shared", "Babička")
	env.seedUser(t, "family", auth.AccountViewer)
	env.seedUser(t, "babicka", auth.AccountViewer)

	for _, username := range []string{"family", "babicka"} {
		client := env.signIn(t, username)
		status, body := env.do(t, client, http.MethodPut, apiV+"/auth/subject",
			`{"subject_uid":"sub_shared"}`)
		if status != http.StatusOK {
			t.Fatalf("PUT /auth/subject for %s = %d, body %s", username, status, body)
		}
		if got := env.meSubject(t, client); got == nil || *got != "sub_shared" {
			t.Fatalf("%s is linked to %v, want sub_shared", username, got)
		}
	}
}

// TestSubjectLink_survivesSubjectDeletion is the guarantee that matters most: a
// person removed from the library must not take an account with them. The link
// simply becomes no link, and the account still logs in and reads back.
func TestSubjectLink_survivesSubjectDeletion(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_gone", "Babička")
	env.seedUser(t, "viewer", auth.AccountViewer)
	client := env.signIn(t, "viewer")

	if status, body := env.do(t, client, http.MethodPut, apiV+"/auth/subject",
		`{"subject_uid":"sub_gone"}`); status != http.StatusOK {
		t.Fatalf("PUT /auth/subject = %d, body %s", status, body)
	}

	env.deleteSubject(t, "sub_gone")

	// The existing session keeps working…
	if got := env.meSubject(t, client); got != nil {
		t.Fatalf("after the person was deleted, the link is %q, want none", *got)
	}
	// …and so does a fresh login, which re-reads the row from scratch.
	fresh := env.signIn(t, "viewer")
	if got := env.meSubject(t, fresh); got != nil {
		t.Fatalf("after re-login, the link is %q, want none", *got)
	}
}

// TestSubjectLink_adminUsersRefusesALink pins that the system administrator's
// account endpoints say nothing about who an account is: a person is a fact
// about one project's people, kept on that membership, so a subject_uid in a
// POST or PATCH /admin/users body is an unknown field — refused with 400 before
// anything is written — and an update without one leaves the membership's link
// alone instead of clearing it.
func TestSubjectLink_adminUsersRefusesALink(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_a", "Babička")
	env.addSubject(t, "sub_b", "Dědeček")
	env.seedUser(t, "admin", auth.AccountMaintainer)
	admin := env.signIn(t, "admin")

	status, body := env.do(t, admin, http.MethodPost, "/api/v1/admin/users",
		`{"username":"novy","password":"correct horse battery",`+
			`"display_name":"","email":"novy@example.test","note":"","subject_uid":"sub_a"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("POST /admin/users with a subject_uid = %d, want 400 (body %s)", status, body)
	}
	var accounts int
	if err := env.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM users WHERE username = 'novy'`).Scan(&accounts); err != nil {
		t.Fatalf("counting accounts: %v", err)
	}
	if accounts != 0 {
		t.Fatalf("a refused create left %d accounts named novy, want 0", accounts)
	}

	// A member of the bootstrap project, linked there to sub_a.
	linked := "sub_a"
	target, err := env.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "stary", Email: "stary@example.test", Password: "correct horse battery",
		Role: auth.AccountViewer, SubjectUID: &linked,
	})
	if err != nil {
		t.Fatalf("seeding the linked account: %v", err)
	}
	path := "/api/v1/admin/users/" + target.UID
	if status, body = env.do(t, admin, http.MethodPatch, path,
		`{"display_name":"Changed","email":"stary@example.test","instance_role":"user","disabled":false,`+
			`"subject_uid":"sub_b"}`); status != http.StatusBadRequest {
		t.Fatalf("PATCH /admin/users with a subject_uid = %d, want 400 (body %s)", status, body)
	}
	assertLinkedTo(t, env.pool, target.UID, "sub_a")

	// Without the field the update goes through and the link is not its to clear.
	if status, body = env.do(t, admin, http.MethodPatch, path,
		`{"display_name":"Changed","email":"stary@example.test","instance_role":"user","disabled":false}`); status != http.StatusOK {
		t.Fatalf("PATCH /admin/users = %d, body %s", status, body)
	}
	if user := decodeUser(t, body); user["display_name"] != "Changed" {
		t.Errorf("display_name = %v, want Changed", user["display_name"])
	} else if _, has := user["subject_uid"]; has {
		t.Errorf("the account record carries a subject_uid: %v", user)
	}
	assertLinkedTo(t, env.pool, target.UID, "sub_a")

	// Only the accepted update reached the trail, and it names no person.
	var entries, withSubject int
	if err := env.pool.QueryRow(t.Context(),
		`SELECT count(*), count(*) FILTER (WHERE details ? 'subject_uid') FROM audit_log
		 WHERE action IN ('user.create', 'user.update')`).Scan(&entries, &withSubject); err != nil {
		t.Fatalf("reading the audit trail: %v", err)
	}
	if entries != 1 || withSubject != 0 {
		t.Errorf("audit trail has %d user.create/update entries (%d naming a subject), want 1 (0)", entries, withSubject)
	}
}

// assertLinkedTo checks that uid's membership of the bootstrap project is
// linked to the subject want.
func assertLinkedTo(t *testing.T, pool *pgxpool.Pool, uid, want string) {
	t.Helper()
	m := bootstrapMembership(t, pool, uid)
	if m == nil {
		t.Fatalf("%s has no membership of the bootstrap project", uid)
	}
	if m.SubjectUID == nil || *m.SubjectUID != want {
		t.Errorf("%s is linked to %v in the bootstrap project, want %s", uid, m.SubjectUID, want)
	}
}
