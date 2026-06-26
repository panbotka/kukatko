package clusterapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/cluster"
)

// fakeService is a Service stub returning canned values for handler tests.
type fakeService struct {
	views      []cluster.View
	assignResp cluster.AssignResult
	removeView cluster.View
	removeDel  bool
	err        error
}

// ListClusters returns the canned views or error.
func (f *fakeService) ListClusters(context.Context) ([]cluster.View, error) {
	return f.views, f.err
}

// AssignCluster returns the canned result or error.
func (f *fakeService) AssignCluster(context.Context, cluster.AssignRequest) (cluster.AssignResult, error) {
	return f.assignResp, f.err
}

// RemoveFace returns the canned view/deleted flag or error.
func (f *fakeService) RemoveFace(context.Context, string, cluster.Ref) (cluster.View, bool, error) {
	return f.removeView, f.removeDel, f.err
}

// passthrough is a no-op middleware standing in for the write guard.
func passthrough(next http.Handler) http.Handler { return next }

// newServer mounts the API with the given service behind a passthrough guard.
func newServer(t *testing.T, svc Service) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Service: svc, RequireWrite: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// do issues an HTTP request with an optional JSON body and returns the response.
func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	} else {
		r = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

// TestHandleList_ok returns the clusters as JSON.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeService{views: []cluster.View{{UID: "fc1", Size: 2}}})

	resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out clustersResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].UID != "fc1" {
		t.Errorf("clusters = %+v, want one fc1", out.Clusters)
	}
}

// TestHandleList_unavailable answers 503 when no backend is wired.
func TestHandleList_unavailable(t *testing.T) {
	t.Parallel()
	srv := newServer(t, nil)

	resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestHandleAssign_statuses covers the body-decode, validation, not-found and
// success status mappings.
func TestHandleAssign_statuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		svc  Service
		body string
		want int
	}{
		{name: "invalid body", svc: &fakeService{}, body: "{", want: http.StatusBadRequest},
		{
			name: "missing subject", body: `{}`, want: http.StatusBadRequest,
			svc: &fakeService{err: cluster.ErrMissingSubject},
		},
		{
			name: "unknown cluster", body: `{"subject_name":"X"}`, want: http.StatusNotFound,
			svc: &fakeService{err: cluster.ErrClusterNotFound},
		},
		{
			name: "ok", body: `{"subject_name":"X"}`, want: http.StatusOK,
			svc: &fakeService{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newServer(t, tt.svc)
			resp := do(t, http.MethodPost, srv.URL+"/api/v1/faces/clusters/fc1/assign", tt.body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}

// TestHandleRemoveFace_refreshed returns the refreshed cluster when not deleted.
func TestHandleRemoveFace_refreshed(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeService{removeView: cluster.View{UID: "fc1", Size: 2}})

	resp := do(t, http.MethodPost, srv.URL+"/api/v1/faces/clusters/fc1/remove-face",
		`{"photo_uid":"ph1","face_index":0}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out removeFaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Cluster == nil || out.Cluster.Size != 2 {
		t.Errorf("cluster = %+v, want size 2", out.Cluster)
	}
}

// TestHandleRemoveFace_deleted returns a null cluster when the removal emptied it.
func TestHandleRemoveFace_deleted(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeService{removeDel: true})

	resp := do(t, http.MethodPost, srv.URL+"/api/v1/faces/clusters/fc1/remove-face",
		`{"photo_uid":"ph1","face_index":0}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out removeFaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Cluster != nil {
		t.Errorf("cluster = %+v, want null", out.Cluster)
	}
}

// TestHandleRemoveFace_notInCluster maps ErrFaceNotInCluster to 404.
func TestHandleRemoveFace_notInCluster(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeService{err: cluster.ErrFaceNotInCluster})

	resp := do(t, http.MethodPost, srv.URL+"/api/v1/faces/clusters/fc1/remove-face",
		`{"photo_uid":"ph1","face_index":0}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
