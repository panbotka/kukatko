//go:build integration

package cluster_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// plantGroup gives each tenant's seeded photo two unassigned faces with the SAME
// vector in both projects, away from the seeded marker: four faces that are all
// each other's perfect neighbours, of which only a project's own two may ever
// form its cluster.
func plantGroup(t *testing.T, e *env, f *projecttest.Fixture) {
	t.Helper()
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		if err := e.vectors.SaveFaces(t.Context(), tn.PhotoUID, []vectors.Face{
			{FaceIndex: 0, Vector: faceVec(7), BBox: [4]float64{0.6, 0.6, 0.1, 0.1}, DetScore: 0.9},
			{FaceIndex: 1, Vector: faceVec(7), BBox: [4]float64{0.02, 0.02, 0.1, 0.1}, DetScore: 0.9},
		}); err != nil {
			t.Fatalf("SaveFaces %s: %v", tn.Slug, err)
		}
	}
}

// TestClusters_neverCrossProjects groups and lists each project on its own:
// each gets exactly one cluster of exactly its own two faces, the listing of one
// never shows the other's, and every cluster operation aimed at the other
// project's cluster is the not-found a missing cluster gets, changing nothing.
func TestClusters_neverCrossProjects(t *testing.T) {
	e := newEnv(t)
	f := projecttest.Seed(t, e.pool)
	plantGroup(t, e, f)
	ctx := t.Context()
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		created, err := e.svc.Recluster(ctx, tn.ProjectUID)
		if err != nil {
			t.Fatalf("Recluster %s: %v", tn.Slug, err)
		}
		if created != 1 {
			t.Errorf("Recluster %s created %d clusters, want 1", tn.Slug, created)
		}
		if _, err := e.svc.BuildSummaries(ctx, tn.ProjectUID, 0); err != nil {
			t.Fatalf("BuildSummaries %s: %v", tn.Slug, err)
		}
	}

	clusterOf := map[string]cluster.View{}
	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		listing, err := e.svc.ListPage(ctx, projectUID, cluster.PageRequest{})
		out := make([]string, 0, len(listing.Clusters))
		for _, view := range listing.Clusters {
			out = append(out, view.UID)
			clusterOf[projectUID] = view
		}
		return out, err
	})
	store := cluster.NewStore(e.pool)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		view := clusterOf[tn.ProjectUID]
		members, err := store.ListClusterFaces(ctx, tn.ProjectUID, view.UID)
		if err != nil {
			t.Fatalf("ListClusterFaces %s: %v", tn.Slug, err)
		}
		if len(members) != 2 || members[0].PhotoUID != tn.PhotoUID || members[1].PhotoUID != tn.PhotoUID {
			t.Errorf("cluster of %s holds %+v, want exactly its own two faces", tn.Slug, members)
		}
	}

	foreign := clusterOf[f.A.ProjectUID].UID
	if _, err := e.svc.AssignCluster(ctx, f.B.ProjectUID, cluster.AssignRequest{
		ClusterUID: foreign, SubjectUID: f.B.SubjectUID,
	}); !errors.Is(err, cluster.ErrClusterNotFound) {
		t.Errorf("assigning A's cluster inside B = %v, want ErrClusterNotFound", err)
	}
	if _, _, err := e.svc.RemoveFace(ctx, f.B.ProjectUID, foreign,
		cluster.Ref{PhotoUID: f.A.PhotoUID, FaceIndex: 0}); !errors.Is(err, cluster.ErrClusterNotFound) {
		t.Errorf("removing a face from A's cluster inside B = %v, want ErrClusterNotFound", err)
	}
	if members, err := store.ListClusterFaces(ctx, f.A.ProjectUID, foreign); err != nil || len(members) != 2 {
		t.Errorf("A's cluster after B's attempts = %d faces (%v), want its 2 untouched", len(members), err)
	}
	if _, err := e.svc.ListPage(ctx, "", cluster.PageRequest{}); !errors.Is(err, cluster.ErrNoProject) {
		t.Errorf("ListPage without a project = %v, want ErrNoProject", err)
	}
}
