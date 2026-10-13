//go:build integration

package familyapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyapi"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// recordingExport remembers the project of every export rewrite it was asked
// for.
type recordingExport struct {
	mu       sync.Mutex
	projects []string
}

// EnqueueFamilyExport records the project.
func (r *recordingExport) EnqueueFamilyExport(_ context.Context, projectUID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects = append(r.projects, projectUID)
	return nil
}

// isolationEnv is the family API mounted under /p/{project} over two seeded
// projects, with a family recorded in A.
type isolationEnv struct {
	fx      *projecttest.Fixture
	server  *httptest.Server
	export  *recordingExport
	people  *people.Store
	family  *family.Store
	parentA string
	famA    string
}

// newIsolationEnv seeds both projects, records a parent of A's subject and
// mounts the API behind the real guards and project resolution.
func newIsolationEnv(t *testing.T) *isolationEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	fx := projecttest.Seed(t, pool)
	store := family.NewStore(pool)
	ppl := people.NewStore(pool)

	parent, err := ppl.CreateSubject(t.Context(), fx.A.ProjectUID, people.Subject{Name: "Rodič " + fx.A.Token})
	if err != nil {
		t.Fatalf("creating A's parent: %v", err)
	}
	fam, err := store.AddParentAudited(t.Context(), fx.A.ProjectUID, fx.A.SubjectUID, parent.UID, "",
		audit.Entry{ActorUID: fx.A.UserUID, Action: audit.ActionSubjectRelationAdd, TargetType: "subjects"})
	if err != nil {
		t.Fatalf("recording A's parent: %v", err)
	}

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	export := &recordingExport{}
	api := familyapi.NewAPI(familyapi.Config{
		Store: store, Export: export,
		RequireAuth: authAPI.RequireAuth, RequireCurator: authAPI.RequireCurator,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &isolationEnv{fx: fx, server: server, export: export, people: ppl, family: store, parentA: parent.UID, famA: fam.UID}
}

// send issues a write as B's member and returns the response, closed by the
// test's cleanup.
func (e *isolationEnv) send(t *testing.T, client *http.Client, method, suffix, body string) *http.Response {
	t.Helper()
	var payload any
	if body != "" {
		payload = json.RawMessage(body)
	}
	resp := e.fx.Send(t, client, method, e.fx.PathFor(projecttest.SideB, suffix), payload)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestIsolation_readsOfAForeignSubjectAreMissing asks B's prefix for the
// relations and the tree of A's subject: both are the 404 a subject that does
// not exist gets, while A's own member sees them.
func TestIsolation_readsOfAForeignSubjectAreMissing(t *testing.T) {
	e := newIsolationEnv(t)
	own := e.fx.Get(t, e.fx.ClientFor(t, e.server, projecttest.SideA),
		e.fx.PathFor(projecttest.SideA, "subjects/"+e.fx.A.SubjectUID+"/tree"))
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("A's own tree = %d, want 200", own.StatusCode)
	}

	client := e.fx.ClientFor(t, e.server, projecttest.SideB)
	for _, suffix := range []string{"/relations", "/tree"} {
		e.fx.AssertInvisible(t, client, e.fx.PathFor(projecttest.SideB, "subjects/"+e.fx.A.SubjectUID+suffix))
		foreign := e.fx.Get(t, client, e.fx.PathFor(projecttest.SideB, "subjects/"+e.fx.A.SubjectUID+suffix))
		missingUID := projecttest.MissingUID("su")
		missing := e.fx.Get(t, client, e.fx.PathFor(projecttest.SideB, "subjects/"+missingUID+suffix))
		// The message names the uid asked for, so compare it with that uid swapped.
		assertSameShapeModuloUID(t, foreign, missing, e.fx.A.SubjectUID, missingUID)
	}
}

// TestIsolation_writesNamingAForeignRowAreMissing records, removes and edits
// across the project boundary from B: every attempt is a 404, nothing is
// scheduled, and A's family is untouched.
func TestIsolation_writesNamingAForeignRowAreMissing(t *testing.T) {
	e := newIsolationEnv(t)
	client := e.fx.ClientFor(t, e.server, projecttest.SideB)

	attempts := []struct {
		name, method, suffix, body string
	}{
		{name: "relate B's subject to A's", method: http.MethodPost,
			suffix: "subjects/" + e.fx.B.SubjectUID + "/relations",
			body:   `{"role":"parent","subject_uid":"` + e.fx.A.SubjectUID + `"}`},
		{name: "relate on A's subject", method: http.MethodPost,
			suffix: "subjects/" + e.fx.A.SubjectUID + "/relations",
			body:   `{"role":"child","new_subject":{"name":"Vetřelec"}}`},
		{name: "remove A's relation", method: http.MethodDelete,
			suffix: "subjects/" + e.fx.A.SubjectUID + "/relations/" + e.parentA},
		{name: "edit A's family", method: http.MethodPatch,
			suffix: "families/" + e.famA, body: `{"kind":"marriage","note":"from B"}`},
	}
	for _, tt := range attempts {
		if resp := e.send(t, client, tt.method, tt.suffix, tt.body); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s from B = %d, want 404", tt.name, resp.StatusCode)
		}
	}
	if len(e.export.projects) != 0 {
		t.Errorf("refused writes scheduled exports for %v, want none", e.export.projects)
	}

	subjects, err := e.people.ListSubjects(t.Context(), e.fx.A.ProjectUID)
	if err != nil {
		t.Fatalf("listing A's subjects: %v", err)
	}
	for _, s := range subjects {
		if s.Name == "Vetřelec" {
			t.Errorf("B created a subject inside A: %+v", s)
		}
	}
	rel, err := e.family.Relations(t.Context(), e.fx.A.ProjectUID, e.fx.A.SubjectUID)
	if err != nil {
		t.Fatalf("A's relations: %v", err)
	}
	if len(rel.Parents) != 1 || rel.Parents[0].UID != e.parentA {
		t.Errorf("A's parents after B's attempts = %+v, want only %s", rel.Parents, e.parentA)
	}
}

// TestIsolation_aWriteSchedulesItsOwnProjectsExport records a relation in B with
// an inline subject: the subject lands in B and the export rewrite named is B's.
func TestIsolation_aWriteSchedulesItsOwnProjectsExport(t *testing.T) {
	e := newIsolationEnv(t)
	client := e.fx.ClientFor(t, e.server, projecttest.SideB)

	resp := e.send(t, client, http.MethodPost, "subjects/"+e.fx.B.SubjectUID+"/relations",
		`{"role":"parent","new_subject":{"name":"Rodič `+e.fx.B.Token+`"}}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("relate in B = %d, want 201", resp.StatusCode)
	}
	if len(e.export.projects) != 1 || e.export.projects[0] != e.fx.B.ProjectUID {
		t.Errorf("scheduled exports for %v, want only B (%s)", e.export.projects, e.fx.B.ProjectUID)
	}
	rel, err := e.family.Relations(t.Context(), e.fx.B.ProjectUID, e.fx.B.SubjectUID)
	if err != nil {
		t.Fatalf("B's relations: %v", err)
	}
	if len(rel.Parents) != 1 {
		t.Fatalf("B's parents = %+v, want the inline one", rel.Parents)
	}
	if _, err := e.people.GetSubjectByUID(t.Context(), e.fx.B.ProjectUID, rel.Parents[0].UID); err != nil {
		t.Errorf("the inline parent is not in B: %v", err)
	}
}

// assertSameShapeModuloUID asserts the two 404s are identical once the uid each
// one names is swapped for the other: a foreign subject must not answer any
// differently from a missing one.
func assertSameShapeModuloUID(t *testing.T, foreign, missing *http.Response, foreignUID, missingUID string) {
	t.Helper()
	defer func() { _ = foreign.Body.Close() }()
	defer func() { _ = missing.Body.Close() }()
	if foreign.StatusCode != http.StatusNotFound || missing.StatusCode != http.StatusNotFound {
		t.Fatalf("statuses = %d (foreign) and %d (missing), want 404 for both", foreign.StatusCode, missing.StatusCode)
	}
	fb, mb := readAll(t, foreign), readAll(t, missing)
	if strings.ReplaceAll(fb, foreignUID, missingUID) != mb {
		t.Errorf("foreign body %q differs from missing body %q beyond the uid", fb, mb)
	}
}

// readAll returns the whole body of resp.
func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	return string(data)
}
