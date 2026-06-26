//go:build integration

package cluster_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they intentionally do not run in parallel. They exercise the cluster service
// over the real stores and the facematch assignment state machine.

// env bundles the stores and the cluster service over the integration database.
type env struct {
	photos  *photos.Store
	vectors *vectors.Store
	people  *people.Store
	svc     *cluster.Service
}

// newEnv builds the cluster service and its collaborators over a freshly
// truncated database. The default tunables are used (threshold 0.4, min size 2).
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	matchSvc := facematch.New(facematch.Config{
		Photos: photoStore, Faces: vectorStore, People: peopleStore,
	})
	svc := cluster.New(cluster.Config{
		Store:    cluster.NewStore(db.Pool()),
		Faces:    vectorStore,
		Assigner: matchSvc,
	})
	return &env{photos: photoStore, vectors: vectorStore, people: peopleStore, svc: svc}
}

// makePhoto inserts a photo with stored dimensions and returns its uid.
func (e *env) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 1000, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo %s: %v", hash, err)
	}
	return created.UID
}

// faceVec builds a FaceDim unit vector with the given index set to 1.
func faceVec(index int) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[index] = 1
	return v
}

// seedFace stores one face on its own fresh photo, optionally with a cached
// subject assignment, and returns the photo uid. Each face lives on its own photo
// so face indexes never collide and a face stands for the same person across
// photos.
func (e *env) seedFace(t *testing.T, hash string, index int, subjectUID, subjectName string) string {
	t.Helper()
	uid := e.makePhoto(t, hash)
	face := vectors.Face{FaceIndex: 0, Vector: faceVec(index), BBox: [4]float64{0.4, 0.4, 0.2, 0.2}}
	if subjectUID != "" {
		face.SubjectUID = &subjectUID
		face.SubjectName = subjectName
	}
	if err := e.vectors.SaveFaces(t.Context(), uid, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces %s: %v", hash, err)
	}
	return uid
}

// listClusters returns the current clusters, failing the test on error.
func (e *env) listClusters(t *testing.T) []cluster.View {
	t.Helper()
	views, err := e.svc.ListClusters(t.Context())
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	return views
}

// TestRecluster_GroupsSimilarSeparatesDissimilar verifies that faces with similar
// embeddings land in one cluster, dissimilar faces in another, and a lone face
// below the minimum size stays unclustered.
func TestRecluster_GroupsSimilarSeparatesDissimilar(t *testing.T) {
	env := newEnv(t)
	ctx := t.Context()

	// Person A: three near-identical faces (axis 0). Person B: two faces (axis 5).
	for _, h := range []string{"a1", "a2", "a3"} {
		env.seedFace(t, h, 0, "", "")
	}
	for _, h := range []string{"b1", "b2"} {
		env.seedFace(t, h, 5, "", "")
	}
	// A single dissimilar face (axis 10) must not form a cluster (below min size).
	env.seedFace(t, "lone", 10, "", "")

	created, err := env.svc.Recluster(ctx)
	if err != nil {
		t.Fatalf("Recluster: %v", err)
	}
	if created != 2 {
		t.Fatalf("created = %d, want 2", created)
	}

	views := env.listClusters(t)
	sizes := map[int]int{}
	for _, v := range views {
		sizes[v.Size]++
	}
	if sizes[3] != 1 || sizes[2] != 1 || len(views) != 2 {
		t.Fatalf("cluster sizes = %v (views %d), want one of size 3 and one of size 2", sizes, len(views))
	}
}

// TestAssignCluster_CreatesMarkersForAllFaces verifies assigning a cluster creates
// a face marker assigned to the subject for every member face and consumes the
// cluster.
func TestAssignCluster_CreatesMarkersForAllFaces(t *testing.T) {
	env := newEnv(t)
	ctx := t.Context()

	photoUIDs := []string{
		env.seedFace(t, "p1", 0, "", ""),
		env.seedFace(t, "p2", 0, "", ""),
		env.seedFace(t, "p3", 0, "", ""),
	}
	if _, err := env.svc.Recluster(ctx); err != nil {
		t.Fatalf("Recluster: %v", err)
	}
	views := env.listClusters(t)
	if len(views) != 1 {
		t.Fatalf("got %d clusters, want 1", len(views))
	}

	result, err := env.svc.AssignCluster(ctx, cluster.AssignRequest{
		ClusterUID: views[0].UID, SubjectName: "Alice",
	})
	if err != nil {
		t.Fatalf("AssignCluster: %v", err)
	}
	if result.Subject.Name != "Alice" || len(result.Markers) != 3 {
		t.Fatalf("result = %+v, want subject Alice with 3 markers", result)
	}

	// Every photo now has a face marker assigned to Alice, and its face row caches
	// the subject.
	for _, uid := range photoUIDs {
		markers, mErr := env.people.ListMarkersByPhoto(ctx, uid)
		if mErr != nil {
			t.Fatalf("ListMarkersByPhoto %s: %v", uid, mErr)
		}
		if len(markers) != 1 || markers[0].SubjectUID == nil || *markers[0].SubjectUID != result.Subject.UID {
			t.Fatalf("photo %s markers = %+v, want one assigned to %s", uid, markers, result.Subject.UID)
		}
		faces, fErr := env.vectors.ListFaces(ctx, uid)
		if fErr != nil {
			t.Fatalf("ListFaces %s: %v", uid, fErr)
		}
		if len(faces) != 1 || faces[0].SubjectUID == nil || *faces[0].SubjectUID != result.Subject.UID {
			t.Fatalf("photo %s face cache = %+v, want subject %s", uid, faces, result.Subject.UID)
		}
	}

	// The consumed cluster is gone.
	if got := env.listClusters(t); len(got) != 0 {
		t.Fatalf("clusters after assign = %d, want 0", len(got))
	}
}

// TestRecluster_Incremental_OnlyTouchesUnassigned verifies that re-clustering
// leaves assigned faces and already-clustered faces untouched and only groups the
// newly added unassigned faces.
func TestRecluster_Incremental_OnlyTouchesUnassigned(t *testing.T) {
	env := newEnv(t)
	ctx := t.Context()

	// Round 1: person A (3 faces) and person B (2 faces).
	for _, h := range []string{"a1", "a2", "a3"} {
		env.seedFace(t, h, 0, "", "")
	}
	for _, h := range []string{"b1", "b2"} {
		env.seedFace(t, h, 5, "", "")
	}
	if _, err := env.svc.Recluster(ctx); err != nil {
		t.Fatalf("Recluster round 1: %v", err)
	}
	clusterA, clusterB := env.clustersBySize(t, 3, 2)

	// Assign cluster A to a subject — those faces are now named.
	if _, err := env.svc.AssignCluster(ctx, cluster.AssignRequest{
		ClusterUID: clusterA.UID, SubjectName: "Anna",
	}); err != nil {
		t.Fatalf("AssignCluster A: %v", err)
	}

	// Round 2: add person C (2 new unassigned faces) and recluster.
	for _, h := range []string{"c1", "c2"} {
		env.seedFace(t, h, 9, "", "")
	}
	created, err := env.svc.Recluster(ctx)
	if err != nil {
		t.Fatalf("Recluster round 2: %v", err)
	}
	if created != 1 {
		t.Fatalf("round 2 created = %d, want 1 (only person C)", created)
	}

	// Cluster B is unchanged (already clustered, not re-touched).
	stillB, err := env.svc.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters: %v", err)
	}
	if !containsCluster(stillB, clusterB.UID, 2) {
		t.Fatalf("cluster B %s (size 2) missing after re-cluster: %+v", clusterB.UID, stillB)
	}
	// A new size-2 cluster for person C exists alongside B (two clusters total).
	if len(stillB) != 2 {
		t.Fatalf("clusters after round 2 = %d, want 2 (B and C)", len(stillB))
	}

	// Anna's faces stay assigned and were not pulled back into a cluster.
	assertAssigned(t, env, "a1")
	assertAssigned(t, env, "a2")
	assertAssigned(t, env, "a3")
}

// TestCluster_SuggestsNearestNamedSubject verifies a cluster surfaces the nearest
// already-named subject as its suggestion.
func TestCluster_SuggestsNearestNamedSubject(t *testing.T) {
	env := newEnv(t)
	ctx := t.Context()

	subj, err := env.people.CreateSubject(ctx, people.Subject{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	// An anchor face already assigned to Bob, on axis 0.
	env.seedFace(t, "bob-anchor", 0, subj.UID, subj.Name)
	// Two unassigned faces of the same person (axis 0) form a cluster.
	env.seedFace(t, "u1", 0, "", "")
	env.seedFace(t, "u2", 0, "", "")

	if _, err := env.svc.Recluster(ctx); err != nil {
		t.Fatalf("Recluster: %v", err)
	}
	views := env.listClusters(t)
	if len(views) != 1 {
		t.Fatalf("got %d clusters, want 1", len(views))
	}
	if views[0].Suggestion == nil || views[0].Suggestion.SubjectUID != subj.UID {
		t.Fatalf("suggestion = %+v, want subject %s (Bob)", views[0].Suggestion, subj.UID)
	}
}

// TestRemoveFace_DetachesStrayFace verifies a face can be removed from a cluster
// before assignment, shrinking the cluster.
func TestRemoveFace_DetachesStrayFace(t *testing.T) {
	env := newEnv(t)
	ctx := t.Context()

	strayPhoto := env.seedFace(t, "s1", 0, "", "")
	env.seedFace(t, "s2", 0, "", "")
	env.seedFace(t, "s3", 0, "", "")
	if _, err := env.svc.Recluster(ctx); err != nil {
		t.Fatalf("Recluster: %v", err)
	}
	view := env.listClusters(t)[0]
	if view.Size != 3 {
		t.Fatalf("cluster size = %d, want 3", view.Size)
	}

	refreshed, deleted, err := env.svc.RemoveFace(ctx, view.UID, cluster.Ref{PhotoUID: strayPhoto, FaceIndex: 0})
	if err != nil {
		t.Fatalf("RemoveFace: %v", err)
	}
	if deleted || refreshed.Size != 2 {
		t.Fatalf("after remove: deleted=%v size=%d, want deleted=false size=2", deleted, refreshed.Size)
	}
}

// clustersBySize fetches the current clusters and returns the ones matching the
// two requested sizes (first match each), failing if either is missing.
func (e *env) clustersBySize(t *testing.T, sizeA, sizeB int) (cluster.View, cluster.View) {
	t.Helper()
	var a, b cluster.View
	var okA, okB bool
	for _, v := range e.listClusters(t) {
		if v.Size == sizeA && !okA {
			a, okA = v, true
		} else if v.Size == sizeB && !okB {
			b, okB = v, true
		}
	}
	if !okA || !okB {
		t.Fatalf("clusters of size %d and %d not both found", sizeA, sizeB)
	}
	return a, b
}

// containsCluster reports whether views holds a cluster with the given uid and size.
func containsCluster(views []cluster.View, uid string, size int) bool {
	for _, v := range views {
		if v.UID == uid && v.Size == size {
			return true
		}
	}
	return false
}

// assertAssigned fails the test unless the single face on the photo has a cached
// subject (it was named and not pulled back into clustering).
func assertAssigned(t *testing.T, e *env, hashUID string) {
	t.Helper()
	faces, err := e.vectors.ListFaces(t.Context(), e.photoUID(t, hashUID))
	if err != nil {
		t.Fatalf("ListFaces %s: %v", hashUID, err)
	}
	if len(faces) != 1 || faces[0].SubjectUID == nil {
		t.Fatalf("photo %s face = %+v, want an assigned subject", hashUID, faces)
	}
}

// photoUID resolves a seeded photo's uid from its file hash.
func (e *env) photoUID(t *testing.T, hash string) string {
	t.Helper()
	photo, err := e.photos.GetByFileHash(t.Context(), hash)
	if err != nil {
		t.Fatalf("GetByFileHash %s: %v", hash, err)
	}
	return photo.UID
}
