//go:build integration

package clusterapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/clusterapi"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestClusterRoutes_foreignClusterIsMissing groups each seeded project's two
// look-alike faces into a cluster of its own and then has B's member aim the
// cluster routes at A's cluster: the listing in B never shows it, and assigning
// it or removing a face from it answers the very 404 a cluster that does not
// exist gets — leaving A's cluster and faces exactly as they were.
func TestClusterRoutes_foreignClusterIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	ctx := t.Context()

	faces := vectors.NewStore(pool)
	photoStore := photos.NewStore(pool)
	peopleStore := people.NewStore(pool)
	svc := cluster.New(cluster.Config{
		Store: cluster.NewStore(pool), Faces: faces,
		Assigner: facematch.New(facematch.Config{Photos: photoStore, Faces: faces, People: peopleStore}),
	})
	clusterOf := map[projecttest.Side]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		if err := faces.SaveFaces(ctx, tn.PhotoUID, []vectors.Face{
			{FaceIndex: 0, Vector: faceVec(9), BBox: [4]float64{0.6, 0.6, 0.1, 0.1}, DetScore: 0.9},
			{FaceIndex: 1, Vector: faceVec(9), BBox: [4]float64{0.02, 0.02, 0.1, 0.1}, DetScore: 0.9},
		}); err != nil {
			t.Fatalf("SaveFaces %s: %v", side, err)
		}
		if _, err := svc.Recluster(ctx, tn.ProjectUID); err != nil {
			t.Fatalf("Recluster %s: %v", side, err)
		}
		if _, err := svc.BuildSummaries(ctx, tn.ProjectUID, 0); err != nil {
			t.Fatalf("BuildSummaries %s: %v", side, err)
		}
		listing, err := svc.ListPage(ctx, tn.ProjectUID, cluster.PageRequest{})
		if err != nil || len(listing.Clusters) != 1 {
			t.Fatalf("ListPage %s = %+v, %v; want one cluster", side, listing.Clusters, err)
		}
		clusterOf[side] = listing.Clusters[0].UID
	}

	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := clusterapi.NewAPI(clusterapi.Config{Service: svc, RequireCurator: authAPI.RequireCurator})
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

	client := f.ClientFor(t, server, projecttest.SideB)
	foreign := clusterOf[projecttest.SideA]
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "faces/clusters"), foreign, f.A.PhotoUID)

	assign := map[string]string{"subject_uid": f.B.SubjectUID}
	foreignAssign := f.Send(t, client, http.MethodPost,
		f.PathFor(projecttest.SideB, "faces/clusters/"+foreign+"/assign"), assign)
	missingAssign := f.Send(t, client, http.MethodPost,
		f.PathFor(projecttest.SideB, "faces/clusters/"+projecttest.MissingUID("fc")+"/assign"), assign)
	if foreignAssign.StatusCode != http.StatusNotFound {
		t.Errorf("assigning A's cluster from B = %d, want 404", foreignAssign.StatusCode)
	}
	projecttest.AssertSameShape(t, foreignAssign, missingAssign)

	remove := map[string]any{"photo_uid": f.A.PhotoUID, "face_index": 0}
	foreignRemove := f.Send(t, client, http.MethodPost,
		f.PathFor(projecttest.SideB, "faces/clusters/"+foreign+"/remove-face"), remove)
	missingRemove := f.Send(t, client, http.MethodPost,
		f.PathFor(projecttest.SideB, "faces/clusters/"+projecttest.MissingUID("fc")+"/remove-face"), remove)
	if foreignRemove.StatusCode != http.StatusNotFound {
		t.Errorf("removing a face from A's cluster from B = %d, want 404", foreignRemove.StatusCode)
	}
	projecttest.AssertSameShape(t, foreignRemove, missingRemove)

	members, err := cluster.NewStore(pool).ListClusterFaces(ctx, f.A.ProjectUID, foreign)
	if err != nil || len(members) != 2 {
		t.Errorf("A's cluster after B's attempts = %d faces (%v), want its 2 untouched", len(members), err)
	}
	for _, m := range members {
		got, err := faces.FacesByKeys(ctx, []vectors.FaceKey{{PhotoUID: m.PhotoUID, FaceIndex: m.FaceIndex}})
		if err != nil || len(got) != 1 || got[0].SubjectUID != nil {
			t.Errorf("A's face %s/%d was assigned from B: %+v (%v)", m.PhotoUID, m.FaceIndex, got, err)
		}
	}
}

// faceVec builds a FaceDim unit vector with the given index set to 1.
func faceVec(index int) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[index] = 1
	return v
}
