//go:build integration

package feedbackapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/feedbackapi"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// newFeedbackServer mounts the real auth API and the feedback routes, over the
// real feedback store, under /api/v1/p/{project} the way the server does.
func newFeedbackServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := feedbackapi.NewAPI(feedbackapi.Config{
		Store: feedback.NewStore(pool), RequireCurator: authAPI.RequireCurator, RequireWrite: authAPI.RequireWrite,
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
	return server
}

// TestLabelAndPairOpinions_foreignTargetIsMissing has B's member reject a label
// on A's photo, reject A's label on B's photo, and dismiss and confirm a pair
// holding A's photo: each answers the very 404 the same request about a uid that
// exists nowhere gets, and no opinion row is written.
func TestLabelAndPairOpinions_foreignTargetIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	server := newFeedbackServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	missingPhoto := projecttest.MissingUID("ph")
	missingLabel := projecttest.MissingUID("lb")

	cases := []struct {
		name    string
		method  string
		route   string
		foreign map[string]string
		missing map[string]string
	}{
		{
			name: "label on A's photo", method: http.MethodPost, route: "feedback/label-rejections",
			foreign: map[string]string{"photo_uid": f.A.PhotoUID, "label_uid": f.B.LabelUID},
			missing: map[string]string{"photo_uid": missingPhoto, "label_uid": f.B.LabelUID},
		},
		{
			name: "A's label", method: http.MethodPost, route: "feedback/label-rejections",
			foreign: map[string]string{"photo_uid": f.B.PhotoUID, "label_uid": f.A.LabelUID},
			missing: map[string]string{"photo_uid": f.B.PhotoUID, "label_uid": missingLabel},
		},
		{
			name: "take back A's label", method: http.MethodDelete, route: "feedback/label-rejections",
			foreign: map[string]string{"photo_uid": f.B.PhotoUID, "label_uid": f.A.LabelUID},
			missing: map[string]string{"photo_uid": f.B.PhotoUID, "label_uid": missingLabel},
		},
		{
			name: "dismiss a pair with A's photo", method: http.MethodPost, route: "feedback/duplicate-dismissals",
			foreign: map[string]string{"photo_uid": f.B.PhotoUID, "other_uid": f.A.PhotoUID},
			missing: map[string]string{"photo_uid": f.B.PhotoUID, "other_uid": missingPhoto},
		},
		{
			name: "confirm a pair with A's photo", method: http.MethodPost, route: "feedback/duplicate-confirmations",
			foreign: map[string]string{"photo_uid": f.B.PhotoUID, "other_uid": f.A.PhotoUID},
			missing: map[string]string{"photo_uid": f.B.PhotoUID, "other_uid": missingPhoto},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := f.PathFor(projecttest.SideB, c.route)
			foreign := f.Send(t, client, c.method, path, c.foreign)
			defer func() { _ = foreign.Body.Close() }() // in memory; AssertSameShape reads it
			if foreign.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s about A's row = %d, want 404", c.method, path, foreign.StatusCode)
			}
			missing := f.Send(t, client, c.method, path, c.missing)
			defer func() { _ = missing.Body.Close() }()
			projecttest.AssertSameShape(t, foreign, missing)
		})
	}

	var rows int
	if err := pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM label_rejections)
		+ (SELECT count(*) FROM duplicate_dismissals) + (SELECT count(*) FROM duplicate_confirmations)`,
	).Scan(&rows); err != nil {
		t.Fatalf("counting opinions: %v", err)
	}
	if rows != 0 {
		t.Errorf("refused opinions left %d rows", rows)
	}
}
