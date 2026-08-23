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
)

// linkEnv is an httptest server over the auth API together with the pool, so a
// test can create and delete the subjects the accounts point at.
type linkEnv struct {
	*httpEnv
	pool *pgxpool.Pool
}

// newLinkEnv builds the environment for the account↔person link tests: the same
// routes newHTTPEnv mounts, plus the database handle.
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
	r.Route("/api/v1", func(r chi.Router) { api.RegisterRoutes(r) })

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &linkEnv{httpEnv: &httpEnv{server: server, svc: svc}, pool: db.Pool()}
}

// addSubject inserts a person into the library and returns its UID.
func (e *linkEnv) addSubject(t *testing.T, uid, name string) string {
	t.Helper()
	_, err := e.pool.Exec(t.Context(),
		`INSERT INTO subjects (uid, slug, name, type) VALUES ($1, $2, $3, 'person')`,
		uid, name, name)
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
func (e *linkEnv) seedUser(t *testing.T, username string, role auth.Role) auth.User {
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

// meSubject reads the caller's own linked subject off GET /auth/me, which is
// where the client gets it from.
func (e *linkEnv) meSubject(t *testing.T, client *http.Client) *string {
	t.Helper()
	status, body := e.do(t, client, http.MethodGet, "/api/v1/auth/me", "")
	if status != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, body %s", status, body)
	}
	var resp struct {
		User struct {
			SubjectUID *string `json:"subject_uid"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decoding /auth/me: %v", err)
	}
	return resp.User.SubjectUID
}

// TestSubjectLink_selfService covers the account page's whole loop: a user says
// which person they are, reads it back off /auth/me, and takes it back again.
func TestSubjectLink_selfService(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_self", "Babička")
	env.seedUser(t, "viewer", auth.RoleViewer)
	client := env.signIn(t, "viewer")

	if got := env.meSubject(t, client); got != nil {
		t.Fatalf("a fresh account is linked to %q, want no link", *got)
	}

	status, body := env.do(t, client, http.MethodPut, "/api/v1/auth/subject",
		`{"subject_uid":"sub_self"}`)
	if status != http.StatusOK {
		t.Fatalf("PUT /auth/subject = %d, body %s", status, body)
	}
	if got := env.meSubject(t, client); got == nil || *got != "sub_self" {
		t.Fatalf("after linking, /auth/me subject = %v, want sub_self", got)
	}

	// Clearing it is how a user says "that is not me after all".
	if status, body = env.do(t, client, http.MethodPut, "/api/v1/auth/subject",
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
	env.seedUser(t, "viewer", auth.RoleViewer)
	client := env.signIn(t, "viewer")

	status, body := env.do(t, client, http.MethodPut, "/api/v1/auth/subject",
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

	status, _ := env.do(t, newClient(t), http.MethodPut, "/api/v1/auth/subject",
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
	env.seedUser(t, "family", auth.RoleViewer)
	env.seedUser(t, "babicka", auth.RoleViewer)

	for _, username := range []string{"family", "babicka"} {
		client := env.signIn(t, username)
		status, body := env.do(t, client, http.MethodPut, "/api/v1/auth/subject",
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
	env.seedUser(t, "viewer", auth.RoleViewer)
	client := env.signIn(t, "viewer")

	if status, body := env.do(t, client, http.MethodPut, "/api/v1/auth/subject",
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

// TestSubjectLink_adminSetsAndAudits covers the administrator's half: creating
// an account already linked, changing the link afterwards, and finding both in
// the audit trail — which is what makes a change to somebody else's account
// accountable.
func TestSubjectLink_adminSetsAndAudits(t *testing.T) {
	env := newLinkEnv(t)
	env.addSubject(t, "sub_a", "Babička")
	env.addSubject(t, "sub_b", "Dědeček")
	env.seedUser(t, "admin", auth.RoleAdmin)
	admin := env.signIn(t, "admin")

	status, body := env.do(t, admin, http.MethodPost, "/api/v1/admin/users",
		`{"username":"novy","password":"correct horse battery","role":"viewer",`+
			`"display_name":"","email":"novy@example.test","note":"","subject_uid":"sub_a"}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d, body %s", status, body)
	}
	var created struct {
		UID        string  `json:"uid"`
		SubjectUID *string `json:"subject_uid"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decoding created user: %v", err)
	}
	if created.SubjectUID == nil || *created.SubjectUID != "sub_a" {
		t.Fatalf("created user linked to %v, want sub_a", created.SubjectUID)
	}

	status, body = env.do(t, admin, http.MethodPatch, "/api/v1/admin/users/"+created.UID,
		`{"display_name":"","email":"novy@example.test","role":"viewer","disabled":false,"subject_uid":"sub_b"}`)
	if status != http.StatusOK {
		t.Fatalf("PATCH /admin/users = %d, body %s", status, body)
	}
	var updated struct {
		SubjectUID *string `json:"subject_uid"`
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decoding updated user: %v", err)
	}
	if updated.SubjectUID == nil || *updated.SubjectUID != "sub_b" {
		t.Fatalf("updated user linked to %v, want sub_b", updated.SubjectUID)
	}

	assertAuditRecords(t, env.pool, created.UID, "sub_a", "sub_b")

	// An omitted subject clears the link, because the update replaces the profile.
	if status, body = env.do(t, admin, http.MethodPatch, "/api/v1/admin/users/"+created.UID,
		`{"display_name":"","email":"novy@example.test","role":"viewer","disabled":false}`); status != http.StatusOK {
		t.Fatalf("PATCH /admin/users (clear) = %d, body %s", status, body)
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decoding cleared user: %v", err)
	}
	if updated.SubjectUID != nil {
		t.Fatalf("after an omitted subject the link is %q, want none", *updated.SubjectUID)
	}
}

// assertAuditRecords checks that the user.create and user.update entries for
// targetUID recorded the subject each one set.
func assertAuditRecords(t *testing.T, pool *pgxpool.Pool, targetUID, wantCreate, wantUpdate string) {
	t.Helper()
	rows, err := pool.Query(t.Context(),
		`SELECT action, details ->> 'subject_uid' FROM audit_log
		 WHERE target_uid = $1 ORDER BY created_at, action`, targetUID)
	if err != nil {
		t.Fatalf("reading the audit trail: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var action string
		var subject *string
		if err := rows.Scan(&action, &subject); err != nil {
			t.Fatalf("scanning an audit row: %v", err)
		}
		if subject != nil {
			got[action] = *subject
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit trail: %v", err)
	}
	if got["user.create"] != wantCreate {
		t.Errorf("user.create recorded subject %q, want %q", got["user.create"], wantCreate)
	}
	if got["user.update"] != wantUpdate {
		t.Errorf("user.update recorded subject %q, want %q", got["user.update"], wantUpdate)
	}
}
