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
// background grouping pass, and reporting the canned answer it gives back.
type fakePreparer struct {
	calls    int
	grouping bool
	err      error
}

// EnsureGrouping counts the call and returns the canned outcome.
func (f *fakePreparer) EnsureGrouping(context.Context) (bool, error) {
	f.calls++
	return f.grouping, f.err
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

// TestHandleList_schedulesGrouping asks the scheduler on every listing — a
// library with no groups at all is exactly the one whose listing reports nothing
// pending, so the request itself, not the counts in its answer, is what decides
// to ask — and passes its verdict on to the reader as `grouping`.
func TestHandleList_schedulesGrouping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		listing  cluster.Listing
		grouping bool
	}{
		{name: "an empty page whose groups are being worked out", grouping: true},
		{name: "an empty page with nothing to do", grouping: false},
		{
			name:     "groups still being prepared",
			listing:  cluster.Listing{Pending: 7},
			grouping: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prep := &fakePreparer{grouping: tt.grouping}
			srv := newServerWith(t, &fakeService{listing: tt.listing}, prep)
			resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
			defer func() { _ = resp.Body.Close() }()
			if prep.calls != 1 {
				t.Errorf("grouping scheduled %d times, want 1", prep.calls)
			}
			var out struct {
				Grouping bool `json:"grouping"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Grouping != tt.grouping {
				t.Errorf("grouping = %t, want %t", out.Grouping, tt.grouping)
			}
		})
	}
}

// TestHandleList_noPreparerStillAnswers serves the listing with no scheduler
// wired at all, reporting no pass rather than promising work nobody will do.
func TestHandleList_noPreparerStillAnswers(t *testing.T) {
	t.Parallel()

	srv := newServer(t, &fakeService{listing: cluster.Listing{Pending: 2}})
	resp := do(t, http.MethodGet, srv.URL+"/api/v1/faces/clusters", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Grouping bool `json:"grouping"`
		Pending  int  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Grouping || out.Pending != 2 {
		t.Errorf("listing = %+v, want two pending groups and no pass", out)
	}
}

// TestHandleList_schedulingFailureStillAnswers keeps serving the prepared
// groups when the pass could not be scheduled: a queue hiccup must not turn a
// readable page into an error.
func TestHandleList_schedulingFailureStillAnswers(t *testing.T) {
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
