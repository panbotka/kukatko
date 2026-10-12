//go:build integration

package avatarapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/avatarapi"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestAvatarRoute_foreignSubjectIsMissing asks B's prefix for the avatar of A's
// subject — a subject with a usable face in A — and asserts the answer is the
// very 404 a subject that does not exist gets, while A's own member does get the
// picture (so the 404 is the project gate, not a missing face).
func TestAvatarRoute_foreignSubjectIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := avatarapi.NewAPI(avatarapi.Config{
		Subjects:    people.NewStore(pool),
		Photos:      photos.NewStore(pool),
		Renderer:    avatar.New(&previewSource{data: sourceJPEG(t)}, t.TempDir()),
		RequireAuth: authAPI.RequireAuth,
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

	own := f.Get(t, f.ClientFor(t, server, projecttest.SideA),
		f.PathFor(projecttest.SideA, "subjects/"+f.A.SubjectUID+"/avatar"))
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("A's own avatar = %d, want 200", own.StatusCode)
	}

	client := f.ClientFor(t, server, projecttest.SideB)
	f.AssertInvisible(t, client, f.PathFor(projecttest.SideB, "subjects/"+f.A.SubjectUID+"/avatar"))
	foreign := f.Get(t, client, f.PathFor(projecttest.SideB, "subjects/"+f.A.SubjectUID+"/avatar"))
	defer func() { _ = foreign.Body.Close() }()
	missing := f.Get(t, client, f.PathFor(projecttest.SideB, "subjects/"+projecttest.MissingUID("su")+"/avatar"))
	defer func() { _ = missing.Body.Close() }()
	projecttest.AssertSameShape(t, foreign, missing)
}
