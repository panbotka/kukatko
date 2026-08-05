package dupmarkersapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/dupmarkersapi"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
)

// passthrough is a guard that lets every request through, so the handlers can be
// exercised without the auth subsystem.
func passthrough(next http.Handler) http.Handler {
	return next
}

// fakeService is a dupmarkersapi.Service returning a canned result or an error.
type fakeService struct {
	result dupmarkers.Result
	err    error
	limit  int
	offset int
}

// FindGroups implements dupmarkersapi.Service, recording the paging it was asked
// for so a test can assert the query parameters reached the service.
func (f *fakeService) FindGroups(_ context.Context, limit, offset int) (dupmarkers.Result, error) {
	f.limit, f.offset = limit, offset
	return f.result, f.err
}

// fakeMarkers is a dupmarkersapi.MarkerStore over an in-memory marker table.
type fakeMarkers struct {
	markers    []people.Marker
	listErr    error
	getErr     error
	invalidErr error
	// invalidated records the uids SetMarkerInvalidAudited was called for, and
	// entries the audit entries it was handed.
	invalidated []string
	entries     []audit.Entry
}

// ListMarkersByPhoto implements dupmarkersapi.MarkerStore.
func (f *fakeMarkers) ListMarkersByPhoto(_ context.Context, photoUID string) ([]people.Marker, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := []people.Marker{}
	for _, m := range f.markers {
		if m.PhotoUID == photoUID {
			out = append(out, m)
		}
	}
	return out, nil
}

// GetMarkerByUID implements dupmarkersapi.MarkerStore.
func (f *fakeMarkers) GetMarkerByUID(_ context.Context, uid string) (people.Marker, error) {
	if f.getErr != nil {
		return people.Marker{}, f.getErr
	}
	for _, m := range f.markers {
		if m.UID == uid {
			return m, nil
		}
	}
	return people.Marker{}, people.ErrMarkerNotFound
}

// SetMarkerInvalidAudited implements dupmarkersapi.MarkerStore.
func (f *fakeMarkers) SetMarkerInvalidAudited(
	_ context.Context, uid string, invalid bool, entry audit.Entry,
) (people.Marker, error) {
	if f.invalidErr != nil {
		return people.Marker{}, f.invalidErr
	}
	f.invalidated = append(f.invalidated, uid)
	f.entries = append(f.entries, entry)
	for i := range f.markers {
		if f.markers[i].UID == uid {
			f.markers[i].Invalid = invalid
			return f.markers[i], nil
		}
	}
	return people.Marker{}, people.ErrMarkerNotFound
}

// fakeAssigner is a dupmarkersapi.Assigner recording every transition asked of it.
type fakeAssigner struct {
	requests []facematch.AssignRequest
	err      error
}

// Apply implements dupmarkersapi.Assigner.
func (f *fakeAssigner) Apply(
	_ context.Context, req facematch.AssignRequest, _ audit.Meta,
) (facematch.AssignResult, error) {
	if f.err != nil {
		return facematch.AssignResult{}, f.err
	}
	f.requests = append(f.requests, req)
	return facematch.AssignResult{Action: req.Action}, nil
}

// marker builds a valid face marker of subjectUID on photoUID.
func marker(uid, photoUID, subjectUID string) people.Marker {
	m := people.Marker{UID: uid, PhotoUID: photoUID, Type: people.MarkerFace}
	if subjectUID != "" {
		m.SubjectUID = &subjectUID
	}
	return m
}

// newRouter mounts the API on a chi router with pass-through guards.
func newRouter(cfg dupmarkersapi.Config) chi.Router {
	cfg.RequireAuth = passthrough
	cfg.RequireWrite = passthrough
	r := chi.NewRouter()
	dupmarkersapi.NewAPI(cfg).RegisterRoutes(r)
	return r
}

// do runs one request against the router and returns the recorder.
func do(r chi.Router, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestList_returnsGroups(t *testing.T) {
	t.Parallel()

	svc := &fakeService{result: dupmarkers.Result{
		Groups: []dupmarkers.Group{{
			PhotoUID: "p1", SubjectUID: "s1", SubjectName: "Marie",
			Markers: []dupmarkers.Marker{{UID: "m1"}, {UID: "m2"}},
		}},
		Total: 1, Limit: 50,
	}}
	r := newRouter(dupmarkersapi.Config{Service: svc})

	rec := do(r, http.MethodGet, "/duplicate-markers?limit=5&offset=10", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got dupmarkers.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got.Groups) != 1 || got.Groups[0].SubjectName != "Marie" {
		t.Errorf("groups = %+v, want the single Marie group", got.Groups)
	}
	if svc.limit != 5 || svc.offset != 10 {
		t.Errorf("service got limit=%d offset=%d, want 5 and 10", svc.limit, svc.offset)
	}
}

func TestList_withoutServiceIsUnavailable(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{})

	if rec := do(r, http.MethodGet, "/duplicate-markers", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestList_rejectsBadPaging(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Service: &fakeService{}})

	for _, target := range []string{"/duplicate-markers?limit=x", "/duplicate-markers?offset=-1"} {
		if rec := do(r, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", target, rec.Code)
		}
	}
}

func TestList_serviceFailureIs500(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Service: &fakeService{err: errors.New("boom")}})

	if rec := do(r, http.MethodGet, "/duplicate-markers", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestKeep_detachesEveryOtherMarkerOfThatPerson(t *testing.T) {
	t.Parallel()

	markers := &fakeMarkers{markers: []people.Marker{
		marker("m1", "p1", "s1"),
		marker("m2", "p1", "s1"),
		marker("m3", "p1", "s1"),
		// Somebody else on the same photo, and the same person on another photo:
		// neither belongs to the group and neither may be touched.
		marker("m4", "p1", "s2"),
		marker("m5", "p2", "s1"),
	}}
	assigner := &fakeAssigner{}
	r := newRouter(dupmarkersapi.Config{Markers: markers, Assigner: assigner})

	rec := do(r, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"m2"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if len(assigner.requests) != 2 {
		t.Fatalf("applied %d transitions, want 2", len(assigner.requests))
	}
	detached := map[string]bool{}
	for _, req := range assigner.requests {
		if req.Action != facematch.ActionUnassignPerson {
			t.Errorf("action = %q, want %q", req.Action, facematch.ActionUnassignPerson)
		}
		if req.PhotoUID != "p1" {
			t.Errorf("photo = %q, want p1", req.PhotoUID)
		}
		detached[req.MarkerUID] = true
	}
	if !detached["m1"] || !detached["m3"] || detached["m2"] {
		t.Errorf("detached = %v, want m1 and m3 but not the kept m2", detached)
	}
}

func TestKeep_reportsWhatItDetached(t *testing.T) {
	t.Parallel()

	markers := &fakeMarkers{markers: []people.Marker{
		marker("m1", "p1", "s1"),
		marker("m2", "p1", "s1"),
	}}
	r := newRouter(dupmarkersapi.Config{Markers: markers, Assigner: &fakeAssigner{}})

	rec := do(r, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"m1"}`)

	var got struct {
		PhotoUID      string   `json:"photo_uid"`
		SubjectUID    string   `json:"subject_uid"`
		KeepMarkerUID string   `json:"keep_marker_uid"`
		Detached      []string `json:"detached"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.PhotoUID != "p1" || got.SubjectUID != "s1" || got.KeepMarkerUID != "m1" {
		t.Errorf("response = %+v, want it to echo the request", got)
	}
	if len(got.Detached) != 1 || got.Detached[0] != "m2" {
		t.Errorf("detached = %v, want [m2]", got.Detached)
	}
}

func TestKeep_ignoresInvalidAndLabelMarkers(t *testing.T) {
	t.Parallel()

	invalid := marker("m2", "p1", "s1")
	invalid.Invalid = true
	label := marker("m3", "p1", "s1")
	label.Type = people.MarkerLabel
	markers := &fakeMarkers{markers: []people.Marker{marker("m1", "p1", "s1"), invalid, label}}
	assigner := &fakeAssigner{}
	r := newRouter(dupmarkersapi.Config{Markers: markers, Assigner: assigner})

	rec := do(r, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"m1"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(assigner.requests) != 0 {
		t.Errorf("applied %d transitions, want 0 (nothing else is a valid face)", len(assigner.requests))
	}
}

func TestKeep_unknownKeeperIs404(t *testing.T) {
	t.Parallel()

	markers := &fakeMarkers{markers: []people.Marker{marker("m1", "p1", "s1"), marker("m2", "p1", "s1")}}
	assigner := &fakeAssigner{}
	r := newRouter(dupmarkersapi.Config{Markers: markers, Assigner: assigner})

	rec := do(r, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"nope"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if len(assigner.requests) != 0 {
		t.Errorf("applied %d transitions, want 0 — nothing may be detached on a rejected request",
			len(assigner.requests))
	}
}

func TestKeep_rejectsIncompleteBodies(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Markers: &fakeMarkers{}, Assigner: &fakeAssigner{}})

	bodies := []string{
		`{}`,
		`{"photo_uid":"p1"}`,
		`{"photo_uid":"p1","subject_uid":"s1"}`,
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"m1","extra":true}`,
		`not json`,
	}
	for _, body := range bodies {
		if rec := do(r, http.MethodPost, "/duplicate-markers/keep", body); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want 400", body, rec.Code)
		}
	}
}

func TestKeep_withoutBackendIsUnavailable(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Service: &fakeService{}})

	rec := do(r, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"m1"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestKeep_detachFailureIs500(t *testing.T) {
	t.Parallel()

	markers := &fakeMarkers{markers: []people.Marker{marker("m1", "p1", "s1"), marker("m2", "p1", "s1")}}
	r := newRouter(dupmarkersapi.Config{
		Markers:  markers,
		Assigner: &fakeAssigner{err: errors.New("boom")},
	})

	rec := do(r, http.MethodPost, "/duplicate-markers/keep",
		`{"photo_uid":"p1","subject_uid":"s1","keep_marker_uid":"m1"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestInvalid_flagsTheMarker(t *testing.T) {
	t.Parallel()

	markers := &fakeMarkers{markers: []people.Marker{marker("m1", "p1", "s1")}}
	r := newRouter(dupmarkersapi.Config{Markers: markers})

	rec := do(r, http.MethodPost, "/duplicate-markers/invalid", `{"marker_uid":"m1"}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s, want 204", rec.Code, rec.Body.String())
	}
	if len(markers.invalidated) != 1 || markers.invalidated[0] != "m1" {
		t.Fatalf("invalidated = %v, want [m1]", markers.invalidated)
	}
	entry := markers.entries[0]
	if entry.Action != audit.ActionMarkerInvalidate {
		t.Errorf("audit action = %q, want %q", entry.Action, audit.ActionMarkerInvalidate)
	}
	if entry.TargetUID != "m1" {
		t.Errorf("audit target = %q, want m1", entry.TargetUID)
	}
	if entry.Details["photo_uid"] != "p1" || entry.Details["subject_uid"] != "s1" {
		t.Errorf("audit details = %v, want the photo and subject", entry.Details)
	}
}

func TestInvalid_unknownMarkerIs404(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Markers: &fakeMarkers{}})

	rec := do(r, http.MethodPost, "/duplicate-markers/invalid", `{"marker_uid":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestInvalid_rejectsIncompleteBodies(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Markers: &fakeMarkers{}})

	for _, body := range []string{`{}`, `{"marker_uid":"  "}`, `{"marker_uid":"m1","extra":1}`, `oops`} {
		if rec := do(r, http.MethodPost, "/duplicate-markers/invalid", body); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s status = %d, want 400", body, rec.Code)
		}
	}
}

func TestInvalid_withoutBackendIsUnavailable(t *testing.T) {
	t.Parallel()

	r := newRouter(dupmarkersapi.Config{Service: &fakeService{}})

	rec := do(r, http.MethodPost, "/duplicate-markers/invalid", `{"marker_uid":"m1"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestInvalid_storeFailureIs500(t *testing.T) {
	t.Parallel()

	markers := &fakeMarkers{
		markers:    []people.Marker{marker("m1", "p1", "s1")},
		invalidErr: errors.New("boom"),
	}
	r := newRouter(dupmarkersapi.Config{Markers: markers})

	rec := do(r, http.MethodPost, "/duplicate-markers/invalid", `{"marker_uid":"m1"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
