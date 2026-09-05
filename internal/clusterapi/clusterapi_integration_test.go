//go:build integration

package clusterapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/clusterapi"
	"github.com/panbotka/kukatko/internal/clusterjob"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel. They exercise the clustering endpoints end-to-end
// (clusterapi HTTP layer over the real cluster.Service and stores).

const testPassword = "correct horse battery staple"

// env wires the auth and cluster APIs behind an httptest server over the
// integration database.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	photos  *photos.Store
	vectors *vectors.Store
	jobs    *jobs.Store
	svc     *cluster.Service
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	matchSvc := facematch.New(facematch.Config{Photos: photoStore, Faces: vectorStore, People: peopleStore})
	svc := cluster.New(cluster.Config{Store: cluster.NewStore(db.Pool()), Faces: vectorStore, Assigner: matchSvc})

	jobStore := jobs.NewStore(db.Pool())
	api := clusterapi.NewAPI(clusterapi.Config{
		Service:      svc,
		Preparer:     clusterjob.New(svc, jobStore, 0, nil),
		RequireWrite: authAPI.RequireWrite,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{
		server: server, authSvc: authSvc, photos: photoStore, vectors: vectorStore,
		jobs: jobStore, svc: svc,
	}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := mustDo(t, client, http.MethodPost, e.server.URL+"/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// mustDo issues an HTTP request with an optional JSON body and returns the response.
func mustDo(t *testing.T, client *http.Client, method, urlStr string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, urlStr, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

// seedFace stores one unassigned face (axis 0) on its own photo.
func (e *env) seedFace(t *testing.T, hash string) {
	t.Helper()
	created, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 1000, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo %s: %v", hash, err)
	}
	vec := make([]float32, vectors.FaceDim)
	vec[0] = 1
	face := vectors.Face{FaceIndex: 0, Vector: vec, BBox: [4]float64{0.4, 0.4, 0.2, 0.2}}
	if err := e.vectors.SaveFaces(t.Context(), created.UID, []vectors.Face{face}); err != nil {
		t.Fatalf("SaveFaces %s: %v", hash, err)
	}
}

// firstClusterUID seeds a cluster and returns its uid after a recluster pass and
// the background preparation the listing depends on.
func (e *env) firstClusterUID(t *testing.T) string {
	t.Helper()
	e.seedCluster(t)
	if _, err := e.svc.BuildSummaries(t.Context(), 0); err != nil {
		t.Fatalf("BuildSummaries: %v", err)
	}
	listing, err := e.svc.ListPage(t.Context(), cluster.PageRequest{})
	if err != nil || len(listing.Clusters) != 1 {
		t.Fatalf("ListPage = %+v, %v; want 1 cluster", listing, err)
	}
	return listing.Clusters[0].UID
}

// seedCluster seeds two faces of one person and groups them, leaving the cluster
// unprepared (no cached summary yet).
func (e *env) seedCluster(t *testing.T) {
	t.Helper()
	e.seedFace(t, "f1")
	e.seedFace(t, "f2")
	if _, err := e.svc.Recluster(t.Context()); err != nil {
		t.Fatalf("Recluster: %v", err)
	}
}

// TestListClusters_EditorSeesClusters verifies an editor can list clusters.
func TestListClusters_EditorSeesClusters(t *testing.T) {
	env := newEnv(t)
	env.firstClusterUID(t)
	client := env.login(t, "editor", auth.RoleEditor)

	resp := mustDo(t, client, http.MethodGet, env.server.URL+"/api/v1/faces/clusters", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET clusters status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Clusters []cluster.View `json:"clusters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].Size != 2 {
		t.Fatalf("clusters = %+v, want one of size 2", out.Clusters)
	}
}

// TestListClusters_ViewerForbidden verifies a viewer cannot reach the editor-only
// clustering API.
func TestListClusters_ViewerForbidden(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "viewer", auth.RoleViewer)

	resp := mustDo(t, client, http.MethodGet, env.server.URL+"/api/v1/faces/clusters", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET clusters status = %d, want 403", resp.StatusCode)
	}
}

// TestAssignCluster_Endpoint verifies assigning a cluster over HTTP names every
// member face and consumes the cluster.
func TestAssignCluster_Endpoint(t *testing.T) {
	env := newEnv(t)
	uid := env.firstClusterUID(t)
	client := env.login(t, "editor", auth.RoleEditor)

	body, _ := json.Marshal(map[string]string{"subject_name": "Carol"})
	resp := mustDo(t, client, http.MethodPost,
		env.server.URL+"/api/v1/faces/clusters/"+uid+"/assign", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign status = %d, want 200", resp.StatusCode)
	}
	var result cluster.AssignResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Subject.Name != "Carol" || len(result.Markers) != 2 {
		t.Fatalf("result = %+v, want subject Carol with 2 markers", result)
	}

	listing, err := env.svc.ListPage(t.Context(), cluster.PageRequest{})
	if err != nil || listing.Total != 0 || listing.Pending != 0 {
		t.Fatalf("clusters after assign = %+v, %v; want none", listing, err)
	}
}

// TestAssignCluster_UnknownCluster verifies an unknown cluster id answers 404.
func TestAssignCluster_UnknownCluster(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "editor", auth.RoleEditor)

	body, _ := json.Marshal(map[string]string{"subject_name": "Nobody"})
	resp := mustDo(t, client, http.MethodPost,
		env.server.URL+"/api/v1/faces/clusters/fcdoesnotexist/assign", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("assign unknown status = %d, want 404", resp.StatusCode)
	}
}

// TestRemoveFace_Endpoint verifies removing a face over HTTP shrinks the cluster.
func TestRemoveFace_Endpoint(t *testing.T) {
	env := newEnv(t)
	env.seedFace(t, "r1")
	env.seedFace(t, "r2")
	env.seedFace(t, "r3")
	if _, err := env.svc.Recluster(t.Context()); err != nil {
		t.Fatalf("Recluster: %v", err)
	}
	if _, err := env.svc.BuildSummaries(t.Context(), 0); err != nil {
		t.Fatalf("BuildSummaries: %v", err)
	}
	listing, err := env.svc.ListPage(t.Context(), cluster.PageRequest{})
	if err != nil || len(listing.Clusters) != 1 {
		t.Fatalf("ListPage = %+v, %v; want 1 cluster", listing, err)
	}
	uid := listing.Clusters[0].UID
	strayPhoto := listing.Clusters[0].Representative.PhotoUID
	client := env.login(t, "editor", auth.RoleEditor)

	body, _ := json.Marshal(map[string]any{"photo_uid": strayPhoto, "face_index": 0})
	resp := mustDo(t, client, http.MethodPost,
		env.server.URL+"/api/v1/faces/clusters/"+uid+"/remove-face", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove-face status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Cluster *cluster.View `json:"cluster"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Cluster == nil || out.Cluster.Size != 2 {
		t.Fatalf("cluster after remove = %+v, want size 2", out.Cluster)
	}
}

// TestListClusters_SchedulesPreparation verifies that opening the page on a
// library whose groups have not been prepared answers at once — with the pending
// count instead of an unbounded wait — and queues exactly one preparation pass,
// however many times it is opened.
func TestListClusters_SchedulesPreparation(t *testing.T) {
	env := newEnv(t)
	env.seedCluster(t)
	client := env.login(t, "editor", auth.RoleEditor)

	for range 3 {
		resp := mustDo(t, client, http.MethodGet, env.server.URL+"/api/v1/faces/clusters", nil)
		var out cluster.Listing
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET clusters status = %d, want 200", resp.StatusCode)
		}
		if len(out.Clusters) != 0 || out.Pending != 1 {
			t.Fatalf("listing = %+v, want nothing ready and one pending", out)
		}
	}

	pending, err := env.jobs.CountPending(t.Context(), jobs.TypeFaceCluster)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("queued %d preparation passes, want exactly 1", pending)
	}
}

// TestListClusters_Pages verifies the endpoint serves bounded pages: the reader
// asks for two groups and is told where the third one starts.
func TestListClusters_Pages(t *testing.T) {
	env := newEnv(t)
	env.firstClusterUID(t)
	client := env.login(t, "editor", auth.RoleEditor)

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/faces/clusters?limit=1&offset=0", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET clusters status = %d, want 200", resp.StatusCode)
	}
	var out cluster.Listing
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Limit != 1 || out.Total != 1 || out.NextOffset != nil {
		t.Fatalf("listing = %+v, want a page of one with no further page", out)
	}
}

// listGroups reads one listing page over HTTP, returning the decoded body.
func (e *env) listGroups(t *testing.T, client *http.Client) struct {
	Clusters []cluster.View `json:"clusters"`
	Pending  int            `json:"pending"`
	Grouping bool           `json:"grouping"`
} {
	t.Helper()
	var out struct {
		Clusters []cluster.View `json:"clusters"`
		Pending  int            `json:"pending"`
		Grouping bool           `json:"grouping"`
	}
	resp := mustDo(t, client, http.MethodGet, e.server.URL+"/api/v1/faces/clusters", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET clusters status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// pendingPasses returns how many face_cluster jobs are queued or running.
func (e *env) pendingPasses(t *testing.T) int {
	t.Helper()
	pending, err := e.jobs.CountPending(t.Context(), jobs.TypeFaceCluster)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	return pending
}

// TestListClusters_SchedulesGrouping verifies the bug this endpoint used to
// have: a library with unassigned faces and no groups at all was never grouped,
// because nothing but the maintainer-only trigger ever started a pass. Opening
// the page as a plain editor must now queue exactly one grouping pass, however
// many times it is opened, and say that it is grouping.
func TestListClusters_SchedulesGrouping(t *testing.T) {
	env := newEnv(t)
	env.seedFace(t, "u1")
	env.seedFace(t, "u2")
	client := env.login(t, "editor", auth.RoleEditor)

	for range 3 {
		out := env.listGroups(t, client)
		if len(out.Clusters) != 0 || out.Pending != 0 {
			t.Fatalf("listing = %+v, want nothing to show yet", out)
		}
		if !out.Grouping {
			t.Fatal("listing does not say a grouping pass is under way")
		}
	}
	if pending := env.pendingPasses(t); pending != 1 {
		t.Fatalf("queued %d grouping passes, want exactly 1", pending)
	}

	// The queued pass is the real one: running it groups the faces and leaves the
	// page with a group to show.
	job, err := env.jobs.Claim(t.Context(), "test", jobs.TypeFaceCluster)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := clusterjob.New(env.svc, env.jobs, 0, nil).Handle(t.Context(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out := env.listGroups(t, client); len(out.Clusters) != 1 {
		t.Fatalf("listing after the pass = %+v, want the one group it made", out)
	}
}

// TestListClusters_NoFacesSchedulesNothing verifies a library with no faces at
// all stays calm: nothing is queued and the page is told no pass is running, so
// it can show a plain empty state rather than a permanent "working…".
func TestListClusters_NoFacesSchedulesNothing(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "editor", auth.RoleEditor)

	if out := env.listGroups(t, client); out.Grouping || len(out.Clusters) != 0 {
		t.Fatalf("listing = %+v, want an empty page and no pass", out)
	}
	if pending := env.pendingPasses(t); pending != 0 {
		t.Fatalf("queued %d passes on a library with no faces, want none", pending)
	}
}

// TestListClusters_GroupedLibrarySchedulesNothing verifies a library that has
// already been grouped and prepared queues nothing more — not even with
// unassigned faces left over, which every pass leaves behind (a face whose
// component is smaller than the minimum group size stays unclustered).
func TestListClusters_GroupedLibrarySchedulesNothing(t *testing.T) {
	env := newEnv(t)
	env.firstClusterUID(t)
	env.seedFace(t, "leftover")
	client := env.login(t, "editor", auth.RoleEditor)

	out := env.listGroups(t, client)
	if len(out.Clusters) != 1 || out.Grouping {
		t.Fatalf("listing = %+v, want the one prepared group and no pass", out)
	}
	if pending := env.pendingPasses(t); pending != 0 {
		t.Fatalf("queued %d passes on an already grouped library, want none", pending)
	}
}
