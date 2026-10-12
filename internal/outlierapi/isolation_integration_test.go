//go:build integration

package outlierapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/outlierapi"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestOutliersRoute_foreignSubjectIsMissing asks B's prefix for the outliers of
// A's person and expects the very 404 a person who does not exist gets — while
// A's own member does get the ranking, so the 404 is the project gate.
func TestOutliersRoute_foreignSubjectIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := outlierapi.NewAPI(outlierapi.Config{
		Service: outliers.New(outliers.Config{
			Faces: vectors.NewStore(pool), People: people.NewStore(pool), Feedback: feedback.NewStore(pool),
		}),
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

	own := f.Get(t, f.ClientFor(t, server, projecttest.SideA),
		f.PathFor(projecttest.SideA, "subjects/"+f.A.SubjectUID+"/outliers"))
	_ = own.Body.Close()
	if own.StatusCode != http.StatusOK {
		t.Fatalf("A's own outliers = %d, want 200", own.StatusCode)
	}

	client := f.ClientFor(t, server, projecttest.SideB)
	f.AssertInvisible(t, client, f.PathFor(projecttest.SideB, "subjects/"+f.A.SubjectUID+"/outliers"))
	foreign := f.Get(t, client, f.PathFor(projecttest.SideB, "subjects/"+f.A.SubjectUID+"/outliers"))
	missing := f.Get(t, client, f.PathFor(projecttest.SideB, "subjects/"+projecttest.MissingUID("su")+"/outliers"))
	projecttest.AssertSameShape(t, foreign, missing)
}
