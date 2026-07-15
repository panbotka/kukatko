package candidatesapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/people"
)

// fakeService records the arguments Find is called with and returns a scripted
// result or error.
type fakeService struct {
	result candidates.Result
	err    error
	gotUID string
	gotReq candidates.Request
}

func (f *fakeService) Find(_ context.Context, uid string, req candidates.Request) (candidates.Result, error) {
	f.gotUID = uid
	f.gotReq = req
	return f.result, f.err
}

// passthrough is a no-op write guard for tests: it authorises every request.
func passthrough(next http.Handler) http.Handler { return next }

// newServer mounts an API backed by svc (which may be nil) and returns a test
// server. A nil svc exercises the 503 path.
func newServer(t *testing.T, svc Service) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Service: svc, RequireWrite: passthrough})
	router := chi.NewRouter()
	router.Route("/api/v1", api.RegisterRoutes)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

// post sends a POST to the candidates endpoint for subject uid with the given raw
// body and returns the response.
func post(t *testing.T, server *httptest.Server, uid, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/api/v1/subjects/"+uid+"/candidates", strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return resp
}

// TestHandleFind_success checks a 200 with the decoded body forwarded to the
// service.
func TestHandleFind_success(t *testing.T) {
	t.Parallel()
	svc := &fakeService{result: candidates.Result{SubjectUID: "su_1", MinMatchCount: 2}}
	server := newServer(t, svc)

	resp := post(t, server, "su_1", `{"threshold":0.4,"limit":10}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if svc.gotUID != "su_1" || svc.gotReq.Threshold != 0.4 || svc.gotReq.Limit != 10 {
		t.Errorf("service got uid=%q req=%+v, want su_1 {0.4 10}", svc.gotUID, svc.gotReq)
	}
	var got candidates.Result
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.MinMatchCount != 2 {
		t.Errorf("response MinMatchCount = %d, want 2", got.MinMatchCount)
	}
}

// TestHandleFind_emptyBodyDefaults checks an empty body is valid and yields a
// zero-valued (all-defaults) request.
func TestHandleFind_emptyBodyDefaults(t *testing.T) {
	t.Parallel()
	svc := &fakeService{}
	server := newServer(t, svc)

	resp := post(t, server, "su_1", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if svc.gotReq != (candidates.Request{}) {
		t.Errorf("service got req=%+v, want zero", svc.gotReq)
	}
}

// TestHandleFind_badRequests checks the body-validation 400s.
func TestHandleFind_badRequests(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, body string }{
		{"unknown field", `{"nope":1}`},
		{"malformed json", `{`},
		{"negative threshold", `{"threshold":-0.1}`},
		{"negative limit", `{"limit":-1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := newServer(t, &fakeService{})
			resp := post(t, server, "su_1", tt.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

// TestHandleFind_subjectNotFound checks the people sentinel maps to 404.
func TestHandleFind_subjectNotFound(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{err: people.ErrSubjectNotFound})
	resp := post(t, server, "su_missing", `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestHandleFind_internalError checks an unexpected error maps to 500.
func TestHandleFind_internalError(t *testing.T) {
	t.Parallel()
	server := newServer(t, &fakeService{err: errors.New("boom")})
	resp := post(t, server, "su_1", `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestHandleFind_noService checks an unwired backend answers 503.
func TestHandleFind_noService(t *testing.T) {
	t.Parallel()
	server := newServer(t, nil)
	resp := post(t, server, "su_1", `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
