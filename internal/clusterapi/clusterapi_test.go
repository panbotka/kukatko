package clusterapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/cluster"
)

// errScheduling stands in for a queue that refused the preparation job.
var errScheduling = errors.New("queue unavailable")

// fakeService is a Service stub returning canned values for handler tests. It
// records the page it was asked for, so the query-parameter plumbing is covered.
type fakeService struct {
	listing    cluster.Listing
	gotPage    cluster.PageRequest
	assignResp cluster.AssignResult
	removeView cluster.View
	removeDel  bool
	err        error
}

// ListPage records the requested page and returns the canned listing or error.
func (f *fakeService) ListPage(_ context.Context, req cluster.PageRequest) (cluster.Listing, error) {
	f.gotPage = req
	return f.listing, f.err
}

// AssignCluster returns the canned result or error.
func (f *fakeService) AssignCluster(context.Context, cluster.AssignRequest) (cluster.AssignResult, error) {
	return f.assignResp, f.err
}

// RemoveFace returns the canned view/deleted flag or error.
func (f *fakeService) RemoveFace(context.Context, string, cluster.Ref) (cluster.View, bool, error) {
	return f.removeView, f.removeDel, f.err
}

// fakePreparer is a Preparer stub counting how often the listing asked for the
// background preparation pass.
type fakePreparer struct {
	calls int
	err   error
}

// EnsureSummaries counts the call and returns the canned outcome.
func (f *fakePreparer) EnsureSummaries(context.Context) (bool, error) {
	f.calls++
	return true, f.err
}

// passthrough is a no-op middleware standing in for the write guard.
func passthrough(next http.Handler) http.Handler { return next }

// newServer mounts the API with the given service behind a passthrough guard.
func newServer(t *testing.T, svc Service) *httptest.Server {
	t.Helper()
	return newServerWith(t, svc, nil)
}

// newServerWith mounts the API with the given service and preparer.
func newServerWith(t *testing.T, svc Service, prep Preparer) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Service: svc, Preparer: prep, RequireWrite: passthrough})
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

// TestHandleList_ok returns the page of clusters as JSON, with the paging fields
// the reader needs to ask for the next one.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	next := 24
	srv := newServer(t, &fakeService{listing: cluster.Listing{
		Clusters: []cluster.View{{UID: "fc1", Size: 2}},
		Total:    30, Pending: 0, Limit: 24, Offset: 0, NextOffset: &next,
	}})

	resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out cluster.Listing
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].UID != "fc1" {
		t.Errorf("clusters = %+v, want one fc1", out.Clusters)
	}
	if out.Total != 30 || out.NextOffset == nil || *out.NextOffset != 24 {
		t.Errorf("paging = total %d next %v, want 30 / 24", out.Total, out.NextOffset)
	}
}

// TestHandleList_paging forwards the limit and offset query parameters.
func TestHandleList_paging(t *testing.T) {
	t.Parallel()
	svc := &fakeService{}
	srv := newServer(t, svc)

	resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters?limit=10&offset=20", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if svc.gotPage.Limit != 10 || svc.gotPage.Offset != 20 {
		t.Errorf("page = %+v, want limit 10 offset 20", svc.gotPage)
	}
}

// TestHandleList_badPaging rejects a limit or offset that is not a non-negative
// integer rather than silently ignoring it.
func TestHandleList_badPaging(t *testing.T) {
	t.Parallel()

	for _, query := range []string{"?limit=many", "?offset=-1", "?limit=-5"} {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			srv := newServer(t, &fakeService{})
			resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters"+query, "")
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestHandleList_schedulesPreparation asks for the background pass exactly when
// the listing reports clusters that are not prepared yet.
func TestHandleList_schedulesPreparation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pending int
		want    int
	}{
		{name: "groups still being prepared", pending: 7, want: 1},
		{name: "everything prepared", pending: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prep := &fakePreparer{}
			srv := newServerWith(t, &fakeService{listing: cluster.Listing{Pending: tt.pending}}, prep)
			resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
			defer func() { _ = resp.Body.Close() }()
			if prep.calls != tt.want {
				t.Errorf("preparation scheduled %d times, want %d", prep.calls, tt.want)
			}
		})
	}
}

// TestHandleList_preparationFailureStillAnswers keeps serving the prepared
// groups when the pass could not be scheduled: a queue hiccup must not turn a
// readable page into an error.
func TestHandleList_preparationFailureStillAnswers(t *testing.T) {
	t.Parallel()
	prep := &fakePreparer{err: errScheduling}
	srv := newServerWith(t, &fakeService{listing: cluster.Listing{
		Clusters: []cluster.View{{UID: "fc1"}}, Total: 1, Pending: 3,
	}}, prep)

	resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out cluster.Listing
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Clusters) != 1 || out.Pending != 3 {
		t.Errorf("listing = %+v, want the one ready group and three pending", out)
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
