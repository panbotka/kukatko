//go:build integration

package familyapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
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
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyapi"
	"github.com/panbotka/kukatko/internal/people"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and family APIs behind an httptest server over the
// integration database. The real guards are mounted rather than pass-throughs,
// because who may write here is half of what these routes promise.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	db      *database.DB
	people  *people.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := familyapi.NewAPI(familyapi.Config{
		Store:          family.NewStore(db.Pool()),
		RequireAuth:    authAPI.RequireAuth,
		RequireCurator: authAPI.RequireCurator,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc, db: db, people: people.NewStore(db.Pool())}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := e.do(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// do issues a request with an optional JSON body and returns the response.
func (e *env) do(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
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

// status issues a request and returns only its status code, closing the body.
func (e *env) status(t *testing.T, c *http.Client, method, path string, body []byte) int {
	t.Helper()
	resp := e.do(t, c, method, path, body)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// decode issues a request, asserts the status and decodes the JSON body into out.
func (e *env) decode(t *testing.T, c *http.Client, method, path string, body []byte, want int, out any) {
	t.Helper()
	resp := e.do(t, c, method, path, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s status = %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	if out == nil {
		return
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s %s: %v", method, path, err)
	}
}

// subject inserts a named subject and returns its UID.
func (e *env) subject(t *testing.T, name string) string {
	t.Helper()
	subj, err := e.people.CreateSubject(t.Context(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("CreateSubject(%s): %v", name, err)
	}
	return subj.UID
}

// countSubjects returns how many subjects carry the given name, which is how the
// atomicity tests prove that no orphan was left behind.
func (e *env) countSubjects(t *testing.T, name string) int {
	t.Helper()
	var n int
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT COUNT(*) FROM subjects WHERE name = $1", name).Scan(&n); err != nil {
		t.Fatalf("counting subjects named %q: %v", name, err)
	}
	return n
}

// countAudit returns how many audit rows exist for the given action.
func (e *env) countAudit(t *testing.T, action string) int {
	t.Helper()
	n, err := audit.NewStore(e.db.Pool()).Count(t.Context(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	return n
}

// addRelation posts one relation and returns the decoded result.
func (e *env) addRelation(t *testing.T, c *http.Client, uid string, body string) family.AddResult {
	t.Helper()
	var result family.AddResult
	e.decode(t, c, http.MethodPost, "/api/v1/subjects/"+uid+"/relations",
		[]byte(body), http.StatusCreated, &result)
	return result
}

// TestRelations_roundTrip records a three-generation family through the API and
// reads each of the four derived lists back, so what the strip renders is checked
// against what actually landed in Postgres.
func TestRelations_roundTrip(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	father := env.subject(t, "Bohumil Nečas st.")
	mother := env.subject(t, "Marie Nečasová")
	son := env.subject(t, "Bohumil Nečas ml.")
	daughter := env.subject(t, "Anna Nečasová")

	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+father+`"}`)
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+mother+`"}`)
	env.addRelation(t, editor, daughter, `{"role":"parent","subject_uid":"`+father+`"}`)
	env.addRelation(t, editor, daughter, `{"role":"parent","subject_uid":"`+mother+`"}`)

	var relations family.Relations
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+son+"/relations",
		nil, http.StatusOK, &relations)

	if len(relations.Parents) != 2 {
		t.Errorf("parents = %d, want 2: %+v", len(relations.Parents), relations.Parents)
	}
	if len(relations.Siblings) != 1 || relations.Siblings[0].UID != daughter {
		t.Errorf("siblings = %+v, want only the daughter", relations.Siblings)
	}

	// The father's own page sees the couple and both children.
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+father+"/relations",
		nil, http.StatusOK, &relations)
	if len(relations.Children) != 2 {
		t.Errorf("children = %d, want 2: %+v", len(relations.Children), relations.Children)
	}
	if len(relations.Partners) != 1 || relations.Partners[0].Partner == nil ||
		relations.Partners[0].Partner.UID != mother {
		t.Errorf("partners = %+v, want the mother", relations.Partners)
	}
}

// TestAddRelation_newSubjectIsCreatedAndAudited creates the person and the
// relation in one call — the affordance the whole data-entry story rests on — and
// checks the audit row records which family it landed in.
func TestAddRelation_newSubjectIsCreatedAndAudited(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)
	son := env.subject(t, "Bohumil Nečas ml.")

	result := env.addRelation(t, editor, son,
		`{"role":"parent","new_subject":{"name":"Marie Nečasová","birth_year":1921}}`)
	if !result.Created {
		t.Error("created = false, want true for an inline new subject")
	}
	if result.Relative.Name != "Marie Nečasová" || result.Relative.BirthYear == nil ||
		*result.Relative.BirthYear != 1921 {
		t.Errorf("relative mismatch: %+v", result.Relative)
	}
	if result.Family.UID == "" {
		t.Error("no family uid returned")
	}
	if got := env.countSubjects(t, "Marie Nečasová"); got != 1 {
		t.Errorf("subjects named Marie Nečasová = %d, want 1", got)
	}

	records, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionSubjectRelationAdd, Limit: 10})
	if err != nil {
		t.Fatalf("listing audit entries: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(records))
	}
	if records[0].Details["family_uid"] != result.Family.UID {
		t.Errorf("audit family_uid = %v, want %s", records[0].Details["family_uid"], result.Family.UID)
	}
	if records[0].Details["other_uid"] != result.Relative.UID {
		t.Errorf("audit other_uid = %v, want %s", records[0].Details["other_uid"], result.Relative.UID)
	}
}

// TestAddRelation_newSubjectIsAtomic is the promise the inline-create path has to
// keep: when the relation is refused, the subject it would have been recorded for
// must not survive. A third parent is refused because a person is a child in at
// most one family, and that refusal happens after the subject row was written —
// which is exactly the case an orphan would fall out of.
func TestAddRelation_newSubjectIsAtomic(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	son := env.subject(t, "Bohumil Nečas ml.")
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+env.subject(t, "Otec")+`"}`)
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+env.subject(t, "Matka")+`"}`)

	got := env.status(t, editor, http.MethodPost, "/api/v1/subjects/"+son+"/relations",
		[]byte(`{"role":"parent","new_subject":{"name":"Třetí Rodič"}}`))
	if got != http.StatusConflict {
		t.Fatalf("third parent status = %d, want 409", got)
	}
	if n := env.countSubjects(t, "Třetí Rodič"); n != 0 {
		t.Errorf("orphan subjects left behind = %d, want 0", n)
	}
	if n := env.countAudit(t, audit.ActionSubjectRelationAdd); n != 2 {
		t.Errorf("audit entries = %d, want 2 (the refused one must not be recorded)", n)
	}
}

// TestAddRelation_cycleIsConflict refuses to make a person their own ancestor,
// and says so as a 409 rather than as a 500: the request is well formed, the tree
// is what stands in its way.
func TestAddRelation_cycleIsConflict(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	father := env.subject(t, "Otec")
	son := env.subject(t, "Syn")
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+father+`"}`)

	got := env.status(t, editor, http.MethodPost, "/api/v1/subjects/"+father+"/relations",
		[]byte(`{"role":"parent","subject_uid":"`+son+`"}`))
	if got != http.StatusConflict {
		t.Fatalf("cycle status = %d, want 409", got)
	}
}

// TestAddRelation_unknownSubject answers 404 for a subject that does not exist,
// whether it is the one in the path or the one in the body.
func TestAddRelation_unknownSubject(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)
	known := env.subject(t, "Známý")

	if got := env.status(t, editor, http.MethodPost, "/api/v1/subjects/su_ghost/relations",
		[]byte(`{"role":"parent","subject_uid":"`+known+`"}`)); got != http.StatusNotFound {
		t.Errorf("unknown path subject status = %d, want 404", got)
	}
	if got := env.status(t, editor, http.MethodPost, "/api/v1/subjects/"+known+"/relations",
		[]byte(`{"role":"parent","subject_uid":"su_ghost"}`)); got != http.StatusNotFound {
		t.Errorf("unknown body subject status = %d, want 404", got)
	}
	if got := env.status(t, editor, http.MethodGet, "/api/v1/subjects/su_ghost/relations",
		nil); got != http.StatusNotFound {
		t.Errorf("relations of an unknown subject status = %d, want 404", got)
	}
	if got := env.status(t, editor, http.MethodGet, "/api/v1/subjects/su_ghost/tree",
		nil); got != http.StatusNotFound {
		t.Errorf("tree of an unknown subject status = %d, want 404", got)
	}
}

// TestRemoveRelation_roundTrip removes a recorded relation and then finds nothing
// left to remove, so a repeated click answers 404 instead of pretending.
func TestRemoveRelation_roundTrip(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	father := env.subject(t, "Otec")
	son := env.subject(t, "Syn")
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+father+`"}`)

	path := "/api/v1/subjects/" + son + "/relations/" + father
	if got := env.status(t, editor, http.MethodDelete, path, nil); got != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", got)
	}
	var relations family.Relations
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+son+"/relations",
		nil, http.StatusOK, &relations)
	if len(relations.Parents) != 0 {
		t.Errorf("parents after removal = %+v, want none", relations.Parents)
	}
	if got := env.status(t, editor, http.MethodDelete, path, nil); got != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", got)
	}
	if n := env.countAudit(t, audit.ActionSubjectRelationRemove); n != 1 {
		t.Errorf("audit entries = %d, want 1", n)
	}
}

// TestTree_ignoresTheRetiredParams sends the direction and generations the
// endpoint used to take — a pedigree, a one-generation bound, a direction that
// never existed — and gets the same whole network every time: the parameters
// are gone, and a stale client carrying them is neither obeyed nor refused.
func TestTree_ignoresTheRetiredParams(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	grandfather := env.subject(t, "Děd")
	father := env.subject(t, "Otec")
	child := env.subject(t, "Vnuk")
	env.addRelation(t, editor, father, `{"role":"parent","subject_uid":"`+grandfather+`"}`)
	env.addRelation(t, editor, child, `{"role":"parent","subject_uid":"`+father+`"}`)

	var plain family.Tree
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+child+"/tree",
		nil, http.StatusOK, &plain)
	want := map[string]int{grandfather: -2, father: -1, child: 0}
	if got := memberGenerations(plain.Members); !maps.Equal(got, want) {
		t.Fatalf("generations = %v, want %v", got, want)
	}
	if len(plain.Families) != 2 || plain.Total != 3 || plain.Truncated {
		t.Errorf("families = %d, total = %d, truncated = %v; want 2, 3, false",
			len(plain.Families), plain.Total, plain.Truncated)
	}

	for _, query := range []string{
		"?direction=ancestors&generations=1",
		"?direction=descendants",
		"?direction=network",
		"?generations=1",
		"?direction=sideways&generations=-2",
	} {
		var tree family.Tree
		env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+child+"/tree"+query,
			nil, http.StatusOK, &tree)
		if got := memberGenerations(tree.Members); !maps.Equal(got, want) || len(tree.Families) != 2 {
			t.Errorf("%s: generations = %v with %d families, want the whole network %v",
				query, got, len(tree.Families), want)
		}
	}
}

// memberGenerations maps a tree's members to their signed generations.
func memberGenerations(members []family.Member) map[string]int {
	out := make(map[string]int, len(members))
	for _, m := range members {
		out[m.UID] = m.Generation
	}
	return out
}

// TestTree_network walks sideways through a parentless sibling group to an aunt
// and her daughter, and checks the wire shape the page reads: a signed
// generation on every member and nothing of the retired walks — no depth, no
// partner flag, no direction — beside the explicit truncated flag and total.
func TestTree_network(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	mother := env.subject(t, "Ludmila")
	father := env.subject(t, "Aleš")
	son := env.subject(t, "Tomáš")
	aunt := env.subject(t, "Dagmar")
	cousin := env.subject(t, "Petra")
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+mother+`"}`)
	env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+father+`"}`)
	env.addRelation(t, editor, mother, `{"role":"sibling","subject_uid":"`+aunt+`"}`)
	env.addRelation(t, editor, cousin, `{"role":"parent","subject_uid":"`+aunt+`"}`)

	var raw struct {
		Truncated *bool               `json:"truncated"`
		Total     int                 `json:"total"`
		Members   []map[string]any    `json:"members"`
		Families  []family.TreeFamily `json:"families"`
		Direction *string             `json:"direction"`
	}
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+son+"/tree",
		nil, http.StatusOK, &raw)
	if raw.Direction != nil {
		t.Errorf("the payload still names a direction: %q", *raw.Direction)
	}
	if raw.Truncated == nil || *raw.Truncated {
		t.Errorf("truncated = %v, want an explicit false", raw.Truncated)
	}
	if raw.Total != 5 {
		t.Errorf("total = %d, want the whole five-person component", raw.Total)
	}
	want := map[string]int{son: 0, mother: -1, father: -1, aunt: -1, cousin: 0}
	got := map[string]int{}
	for _, m := range raw.Members {
		uid, _ := m["uid"].(string)
		generation, ok := m["generation"].(float64)
		if !ok {
			t.Fatalf("member %s lacks a generation on the wire", uid)
		}
		for _, retired := range []string{"depth", "partner"} {
			if _, ok := m[retired]; ok {
				t.Errorf("member %s still carries %q", uid, retired)
			}
		}
		got[uid] = int(generation)
	}
	if !maps.Equal(got, want) {
		t.Errorf("generations = %v, want %v", got, want)
	}
	if len(raw.Families) != 3 {
		t.Errorf("families = %d, want 3", len(raw.Families))
	}
}

// TestUpdateFamily_roundTrip edits the family row itself and records the diff.
func TestUpdateFamily_roundTrip(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)

	husband := env.subject(t, "Bohumil")
	result := env.addRelation(t, editor, husband,
		`{"role":"partner","new_subject":{"name":"Marie"}}`)

	var updated family.Family
	env.decode(t, editor, http.MethodPatch, "/api/v1/families/"+result.Family.UID,
		[]byte(`{"kind":"marriage","from_year":1948,"note":"oddáni v Křtinách"}`),
		http.StatusOK, &updated)
	if updated.Kind != family.KindMarriage || updated.FromYear == nil || *updated.FromYear != 1948 {
		t.Errorf("updated family mismatch: %+v", updated)
	}
	if updated.Note != "oddáni v Křtinách" {
		t.Errorf("note = %q, want the wedding note", updated.Note)
	}

	if got := env.status(t, editor, http.MethodPatch, "/api/v1/families/fm_ghost",
		[]byte(`{"kind":"marriage"}`)); got != http.StatusNotFound {
		t.Errorf("unknown family status = %d, want 404", got)
	}
	if got := env.status(t, editor, http.MethodPatch, "/api/v1/families/"+result.Family.UID,
		[]byte(`{"kind":"marriage","from_year":1300}`)); got != http.StatusBadRequest {
		t.Errorf("impossible year status = %d, want 400", got)
	}
	if n := env.countAudit(t, audit.ActionFamilyUpdate); n != 1 {
		t.Errorf("audit entries = %d, want 1", n)
	}
}

// TestRBAC_curatorMayWrite proves the wiring rather than the intent: all three
// mutations hang on RequireCurator, so a curator records a relation (creating its
// subject inline), edits the family and removes the relation again. The package
// has no route that stays on RequireWrite, so there is no refusal to assert here.
func TestRBAC_curatorMayWrite(t *testing.T) {
	env := newEnv(t)
	curator := env.login(t, "curator", auth.RoleCurator)
	son := env.subject(t, "Syn")

	result := env.addRelation(t, curator, son, `{"role":"parent","new_subject":{"name":"Otec"}}`)
	father := result.Relative.UID

	if got := env.status(t, curator, http.MethodPatch, "/api/v1/families/"+result.Family.UID,
		[]byte(`{"kind":"marriage"}`)); got != http.StatusOK {
		t.Errorf("curator PATCH family = %d, want 200", got)
	}
	if got := env.status(t, curator, http.MethodDelete,
		"/api/v1/subjects/"+son+"/relations/"+father, nil); got >= http.StatusMultipleChoices {
		t.Errorf("curator DELETE relation = %d, want 2xx", got)
	}
}

// TestRBAC_viewerMayReadButNotWrite is the guard contract of this package: a
// viewer sees the family of everybody and changes nobody's.
func TestRBAC_viewerMayReadButNotWrite(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "editor", auth.RoleEditor)
	viewer := env.login(t, "viewer", auth.RoleViewer)

	father := env.subject(t, "Otec")
	son := env.subject(t, "Syn")
	result := env.addRelation(t, editor, son, `{"role":"parent","subject_uid":"`+father+`"}`)

	for _, path := range []string{
		"/api/v1/subjects/" + son + "/relations",
		"/api/v1/subjects/" + son + "/tree",
	} {
		if got := env.status(t, viewer, http.MethodGet, path, nil); got != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, got)
		}
	}

	for _, tc := range []struct {
		method, path string
		body         []byte
	}{
		{
			method: http.MethodPost, path: "/api/v1/subjects/" + son + "/relations",
			body: []byte(`{"role":"partner","new_subject":{"name":"Nikdo"}}`),
		},
		{method: http.MethodDelete, path: "/api/v1/subjects/" + son + "/relations/" + father},
		{
			method: http.MethodPatch, path: "/api/v1/families/" + result.Family.UID,
			body: []byte(`{"kind":"marriage"}`),
		},
	} {
		if got := env.status(t, viewer, tc.method, tc.path, tc.body); got != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", tc.method, tc.path, got)
		}
	}

	if n := env.countSubjects(t, "Nikdo"); n != 0 {
		t.Errorf("a refused viewer still created %d subjects, want 0", n)
	}
	var relations family.Relations
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+son+"/relations",
		nil, http.StatusOK, &relations)
	if len(relations.Parents) != 1 {
		t.Errorf("parents after the refused writes = %+v, want the father still there", relations.Parents)
	}
}

// TestUnauthenticated_isRefused keeps the read routes behind a session: this is a
// family archive, and who is whose mother is not public.
func TestUnauthenticated_isRefused(t *testing.T) {
	env := newEnv(t)
	anonymous := &http.Client{}
	for _, path := range []string{
		"/api/v1/subjects/su_a/relations",
		"/api/v1/subjects/su_a/tree",
	} {
		if got := env.status(t, anonymous, http.MethodGet, path, nil); got != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d, want 401", path, got)
		}
	}
}

// TestAddRelation_siblingWithNoParents verifies the endpoint takes the sibling
// role and records the one relation that has nowhere else to go: two people whose
// parents are not in the library. The response names a family with no partners,
// the people list gains no phantom, and removing the link again is allowed —
// which it is not once a parent hangs on the family.
func TestAddRelation_siblingWithNoParents(t *testing.T) {
	env := newEnv(t)
	editor := env.login(t, "sibling-editor", auth.RoleEditor)
	josef := env.subject(t, "Josef Nečas")
	anna := env.subject(t, "Anna Nečasová")

	result := env.addRelation(t, editor, josef, `{"role":"sibling","subject_uid":"`+anna+`"}`)
	if result.Family.PartnerA != nil || result.Family.PartnerB != nil {
		t.Errorf("family = %+v, want no partner recorded", result.Family)
	}
	if result.Relative.UID != anna || result.Created {
		t.Errorf("relative = %+v, created = %v, want the existing Anna", result.Relative, result.Created)
	}

	var relations family.Relations
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+josef+"/relations", nil, http.StatusOK, &relations)
	if len(relations.Siblings) != 1 || relations.Siblings[0].UID != anna {
		t.Errorf("siblings = %+v, want Anna", relations.Siblings)
	}
	if len(relations.Parents) != 0 {
		t.Errorf("parents = %+v, want none: no placeholder parent may be invented", relations.Parents)
	}

	// A sibling created inline is still one audited transaction.
	created := env.addRelation(t, editor, josef, `{"role":"sibling","new_subject":{"name":"Marie Nečasová"}}`)
	if !created.Created || created.Family.UID != result.Family.UID {
		t.Errorf("inline sibling = %+v, want a new person in the same group", created)
	}
	if got := env.countSubjects(t, "Marie Nečasová"); got != 1 {
		t.Errorf("subjects named Marie = %d, want exactly the one created", got)
	}

	// Parentless: the link is the whole relation, so it can be taken back.
	if got := env.status(t, editor, http.MethodDelete,
		"/api/v1/subjects/"+josef+"/relations/"+created.Relative.UID, nil); got != http.StatusNoContent {
		t.Errorf("removing a parentless sibling = %d, want 204", got)
	}

	// With a parent on the family they are siblings *through* that parent, and
	// the refusal is about the state of the tree rather than about the request.
	mother := env.subject(t, "Ludmila Nečasová")
	env.addRelation(t, editor, josef, `{"role":"parent","subject_uid":"`+mother+`"}`)
	if got := env.status(t, editor, http.MethodDelete,
		"/api/v1/subjects/"+josef+"/relations/"+anna, nil); got != http.StatusConflict {
		t.Errorf("removing a derived sibling = %d, want 409", got)
	}
	env.decode(t, editor, http.MethodGet, "/api/v1/subjects/"+anna+"/relations", nil, http.StatusOK, &relations)
	if len(relations.Parents) != 1 || relations.Parents[0].UID != mother {
		t.Errorf("Anna's parents = %+v, want the mother her brother gained", relations.Parents)
	}
}
