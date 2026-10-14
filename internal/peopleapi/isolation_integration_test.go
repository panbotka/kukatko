//go:build integration

package peopleapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit/audittest"
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
	audittest.Guard(t, db)
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

// TestSubjectCover_foreignPhotoIsMissing names A's photo as the cover of a new
// B subject and of B's existing one, and asserts each write is byte-for-byte the
// 404 a cover uid that names nothing gets — never a 201/200 that would store
// another project's photo (and show its thumbnail in B's search), nor a 500 that
// would tell an existing uid from a made-up one. B's own photo is accepted, and
// B's subject keeps it after the refused write.
func TestSubjectCover_foreignPhotoIsMissing(t *testing.T) {
	server, f, db := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideB)
	missing := projecttest.MissingUID("ph")
	edit := f.PathFor(projecttest.SideB, "subjects/"+f.B.SubjectUID)
	body := func(cover string) map[string]any {
		return map[string]any{"name": "Obálka " + f.B.Token, "cover_photo_uid": cover}
	}

	foreign := f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "subjects"), body(f.A.PhotoUID))
	nobody := f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "subjects"), body(missing))
	if foreign.StatusCode != http.StatusNotFound {
		t.Errorf("creating a B subject with A's photo as cover = %d, want 404", foreign.StatusCode)
	}
	projecttest.AssertSameShape(t, foreign, nobody)

	own := f.Send(t, client, http.MethodPatch, edit, body(f.B.PhotoUID))
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("setting B's own photo as cover = %d, want 200", own.StatusCode)
	}
	foreign = f.Send(t, client, http.MethodPatch, edit, body(f.A.PhotoUID))
	nobody = f.Send(t, client, http.MethodPatch, edit, body(missing))
	if foreign.StatusCode != http.StatusNotFound {
		t.Errorf("setting A's photo as cover of B's subject = %d, want 404", foreign.StatusCode)
	}
	projecttest.AssertSameShape(t, foreign, nobody)

	subject, err := people.NewStore(db.Pool()).GetSubjectByUID(t.Context(), f.B.ProjectUID, f.B.SubjectUID)
	if err != nil {
		t.Fatalf("reading B's subject: %v", err)
	}
	if subject.CoverPhotoUID == nil || *subject.CoverPhotoUID != f.B.PhotoUID {
		t.Errorf("B's subject cover after the refused writes = %v, want B's photo", subject.CoverPhotoUID)
	}
	var stored int
	if err := db.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM subjects WHERE project_id = $1 AND cover_photo_uid = $2`,
		f.B.ProjectUID, f.A.PhotoUID).Scan(&stored); err != nil || stored != 0 {
		t.Errorf("B's subjects with A's photo as cover = %d (%v), want 0", stored, err)
	}
}
