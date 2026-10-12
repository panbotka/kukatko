//go:build integration

package peopleapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/peopleapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/storage"
)

// These are the HTTP-level tenant-isolation tests of the subject routes: a member
// of B, reading or writing through B's prefix, meets A's subject exactly as a
// subject that does not exist, and A's subject is left as it was.

// newIsolationServer mounts the real auth API and the subject routes over the
// real stores the way the server does: under /api/v1/p/{project} behind
// auth.WithProject.
func newIsolationServer(t *testing.T) (*httptest.Server, *projecttest.Fixture, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	api := peopleapi.NewAPI(peopleapi.Config{
		Subjects:       people.NewStore(pool),
		Photos:         photos.NewStore(pool),
		Storage:        fs,
		RequireAuth:    authAPI.RequireAuth,
		RequireCurator: authAPI.RequireCurator,
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
	return server, f, db
}

// TestSubjectReads_foreignSubjectIsMissing reads A's subject and its gallery
// through B's prefix: both are the 404 of a missing subject, and B's people list
// names nothing of A's.
func TestSubjectReads_foreignSubjectIsMissing(t *testing.T) {
	server, f, _ := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideB)

	for _, suffix := range []string{"", "/photos"} {
		foreignPath := f.PathFor(projecttest.SideB, "subjects/"+f.A.SubjectUID+suffix)
		f.AssertInvisible(t, client, foreignPath)
		foreign := f.Get(t, client, foreignPath)
		missing := f.Get(t, client, f.PathFor(projecttest.SideB, "subjects/"+projecttest.MissingUID("su")+suffix))
		projecttest.AssertSameShape(t, foreign, missing)
		_, _ = foreign.Body.Close(), missing.Body.Close()
	}
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "subjects"), f.A.UIDs()...)

	// The member's own subject and gallery still answer, so the 404s above are
	// the project gate and not a broken route.
	for _, suffix := range []string{"", "/photos"} {
		resp := f.Get(t, client, f.PathFor(projecttest.SideB, "subjects/"+f.B.SubjectUID+suffix))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("B's own subject%s = %d, want 200", suffix, resp.StatusCode)
		}
	}
}

// TestSubjectWrites_foreignSubjectIsMissingAndUntouched renames, merges and
// deletes A's subject through B's prefix and asserts each is refused as a
// missing subject and A's subject survives unchanged.
func TestSubjectWrites_foreignSubjectIsMissingAndUntouched(t *testing.T) {
	server, f, db := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideB)

	writes := []struct {
		name, method, path string
		body               any
	}{
		{"rename", http.MethodPatch, "subjects/" + f.A.SubjectUID, map[string]any{"name": "Přepsáno z B"}},
		{"merge into B's", http.MethodPost, "subjects/" + f.A.SubjectUID + "/merge",
			map[string]any{"keeper_uid": f.B.SubjectUID}},
		{"delete", http.MethodDelete, "subjects/" + f.A.SubjectUID, nil},
	}
	for _, w := range writes {
		resp := f.Send(t, client, w.method, f.PathFor(projecttest.SideB, w.path), w.body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s of A's subject through B = %d, want 404", w.name, resp.StatusCode)
		}
	}

	subject, err := people.NewStore(db.Pool()).GetSubjectByUID(t.Context(), f.A.ProjectUID, f.A.SubjectUID)
	if err != nil {
		t.Fatalf("A's subject is gone: %v", err)
	}
	if subject.Name != "Osoba "+f.A.Token {
		t.Errorf("A's subject was renamed to %q", subject.Name)
	}
}
