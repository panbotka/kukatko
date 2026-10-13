//go:build integration

package reviewapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/reviewapi"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// faceVec builds a FaceDim unit vector with the given index set to 1.
func faceVec(index int) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[index] = 1
	return v
}

// newReviewServer mounts the real auth API and the review routes, over the
// real review game, under /api/v1/p/{project} the way the server does.
func newReviewServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	faces := vectors.NewStore(pool)
	peopleStore := people.NewStore(pool)
	photoStore := photos.NewStore(pool)
	feedbackStore := feedback.NewStore(pool)
	organizeStore := organize.NewStore(pool)
	cand := candidates.New(candidates.Config{
		Faces: faces, People: peopleStore, Feedback: feedbackStore, Photos: photoStore,
		Media: mediaurl.NewBuilder(nil),
	})
	svc := review.New(review.Config{
		Sweeper: sweep.New(sweep.Config{Subjects: peopleStore, Finder: cand}),
		Expander: expand.New(expand.Config{
			Vectors: noVectors{faces}, Organize: organizeStore, Feedback: feedbackStore, Photos: photoStore,
			Media: mediaurl.NewBuilder(nil),
		}),
		Organize: organizeStore,
		Faces:    faces,
		Feedback: feedbackStore,
		Assigner: facematch.New(facematch.Config{Photos: photoStore, Faces: faces, People: peopleStore}),
		Scope:    review.NewScopeStore(pool),
		Skips:    review.NewSkipRecorder(pool),
	})
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := reviewapi.NewAPI(reviewapi.Config{
		Service: svc, RequireCurator: authAPI.RequireCurator, RequireAuth: authAPI.RequireAuth,
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

// noVectors is the vector store with a similarity search that finds nothing:
// these tests ask no label questions, and expand is scoped in a later step.
type noVectors struct{ *vectors.Store }

// FindSimilar finds nothing.
func (noVectors) FindSimilar(_ context.Context, _ string, _ []float32, _ int, _ float64) ([]vectors.Match, error) {
	return nil, nil
}

// TestAnswerRoute_foreignFaceIsMissing has B's member answer "yes, that is my
// person" about the unnamed face on A's photo, and "skip" and "no" about A's
// person: each answers the very 404 a question about a photo that does not
// exist gets, and nothing is written — no marker on A's photo, no face
// assigned, no rejection recorded, no skip remembered.
func TestAnswerRoute_foreignFaceIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	if err := vectors.NewStore(pool).SaveFaces(t.Context(), f.A.PhotoUID, []vectors.Face{{
		FaceIndex: 1, Vector: faceVec(4), BBox: [4]float64{0.6, 0.6, 0.2, 0.2}, DetScore: 0.9,
	}}); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
	server := newReviewServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	path := f.PathFor(projecttest.SideB, "review/answer")

	answers := []struct {
		name    string
		foreign string
		missing string
		answer  string
	}{
		{
			name:    "yes about A's face",
			foreign: fmt.Sprintf("face:%s:1:%s", f.A.PhotoUID, f.B.SubjectUID),
			missing: fmt.Sprintf("face:%s:1:%s", projecttest.MissingUID("ph"), f.B.SubjectUID),
			answer:  "yes",
		},
		{
			name:    "no about A's person",
			foreign: fmt.Sprintf("face:%s:0:%s", f.B.PhotoUID, f.A.SubjectUID),
			missing: fmt.Sprintf("face:%s:0:%s", f.B.PhotoUID, projecttest.MissingUID("su")),
			answer:  "no",
		},
		{
			name:    "skip about A's person",
			foreign: fmt.Sprintf("face:%s:0:%s", f.B.PhotoUID, f.A.SubjectUID),
			missing: fmt.Sprintf("face:%s:0:%s", f.B.PhotoUID, projecttest.MissingUID("su")),
			answer:  "skip",
		},
	}
	for _, tt := range answers {
		foreign := f.Send(t, client, http.MethodPost, path, map[string]string{"question_id": tt.foreign, "answer": tt.answer})
		missing := f.Send(t, client, http.MethodPost, path, map[string]string{"question_id": tt.missing, "answer": tt.answer})
		if foreign.StatusCode != http.StatusNotFound {
			t.Errorf("%s from B = %d, want 404", tt.name, foreign.StatusCode)
		}
		projecttest.AssertSameShape(t, foreign, missing)
	}

	for what, query := range map[string]string{
		"markers on A's photo":   "SELECT count(*) FROM markers WHERE photo_uid = $1 AND uid <> $2",
		"assigned faces of A's":  "SELECT count(*) FROM faces WHERE photo_uid = $1 AND subject_uid IS NOT NULL AND $2 <> ''",
		"face rejections of A's": "SELECT count(*) FROM face_rejections WHERE photo_uid = $1 AND $2 <> ''",
	} {
		var n int
		if err := pool.QueryRow(t.Context(), query, f.A.PhotoUID, f.A.MarkerUID).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", what, err)
		}
		if n != 0 {
			t.Errorf("%d %s after B's answers, want none", n, what)
		}
	}
	var skips int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM review_skips").Scan(&skips); err != nil {
		t.Fatalf("counting skips: %v", err)
	}
	if skips != 0 {
		t.Errorf("%d skips remembered after B's answers, want none", skips)
	}
}
