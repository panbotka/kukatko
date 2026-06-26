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
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
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

	api := clusterapi.NewAPI(clusterapi.Config{Service: svc, RequireWrite: authAPI.RequireWrite})
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc, photos: photoStore, vectors: vectorStore, svc: svc}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
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

// firstClusterUID seeds a cluster and returns its uid after a recluster pass.
func (e *env) firstClusterUID(t *testing.T) string {
	t.Helper()
	e.seedFace(t, "f1")
	e.seedFace(t, "f2")
	if _, err := e.svc.Recluster(t.Context()); err != nil {
		t.Fatalf("Recluster: %v", err)
	}
	views, err := e.svc.ListClusters(t.Context())
	if err != nil || len(views) != 1 {
		t.Fatalf("ListClusters = %v, %v; want 1 cluster", views, err)
	}
	return views[0].UID
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

	views, err := env.svc.ListClusters(t.Context())
	if err != nil || len(views) != 0 {
		t.Fatalf("clusters after assign = %v, %v; want none", views, err)
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
	views, _ := env.svc.ListClusters(t.Context())
	uid := views[0].UID
	strayPhoto := views[0].Representative.PhotoUID
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
