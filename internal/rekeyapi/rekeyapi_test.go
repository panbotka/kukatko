package rekeyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/rekey"
)

// fakeMover answers every call with run and err, recording what it was asked.
type fakeMover struct {
	run   rekey.Run
	err   error
	calls []string
	uid   string
}

// record notes one call and returns the canned answer.
func (f *fakeMover) record(call, projectUID string) (rekey.Run, error) {
	f.calls = append(f.calls, call)
	f.uid = projectUID
	return f.run, f.err
}

func (f *fakeMover) Status(_ context.Context, projectUID string) (rekey.Run, error) {
	return f.record("status", projectUID)
}

func (f *fakeMover) Start(_ context.Context, _ audit.Meta, projectUID string) (rekey.Run, error) {
	return f.record("start", projectUID)
}

func (f *fakeMover) Pause(_ context.Context, _ audit.Meta, projectUID string) (rekey.Run, error) {
	return f.record("pause", projectUID)
}

func (f *fakeMover) Resume(_ context.Context, _ audit.Meta, projectUID string) (rekey.Run, error) {
	return f.record("resume", projectUID)
}

func (f *fakeMover) Retry(_ context.Context, _ audit.Meta, projectUID string) (rekey.Run, error) {
	return f.record("retry", projectUID)
}

// allow is a guard that lets every request through.
func allow(next http.Handler) http.Handler { return next }

// deny is a guard that refuses every request, as RequireMaintainer refuses a
// member who is not a system administrator.
func deny(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
}

// serve sends method path to the API over mover behind guard.
func serve(t *testing.T, mover *fakeMover, guard func(http.Handler) http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	NewAPI(Config{Mover: mover, RequireMaintainer: guard}).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))
	return rec
}

func TestRoutes_dispatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method, path string
		call         string
		status       int
	}{
		{http.MethodGet, "/projects/pj1/rekey", "status", http.StatusOK},
		{http.MethodPost, "/projects/pj1/rekey", "start", http.StatusCreated},
		{http.MethodPost, "/projects/pj1/rekey/pause", "pause", http.StatusOK},
		{http.MethodPost, "/projects/pj1/rekey/resume", "resume", http.StatusOK},
		{http.MethodPost, "/projects/pj1/rekey/retry", "retry", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			t.Parallel()
			mover := &fakeMover{run: rekey.Run{UID: "rk1", ProjectUID: "pj1", State: rekey.StateRunning}}
			rec := serve(t, mover, allow, tt.method, tt.path)
			if rec.Code != tt.status {
				t.Fatalf("%s %s = %d, want %d (%s)", tt.method, tt.path, rec.Code, tt.status, rec.Body)
			}
			if len(mover.calls) != 1 || mover.calls[0] != tt.call || mover.uid != "pj1" {
				t.Errorf("calls = %v on %q, want one %s on pj1", mover.calls, mover.uid, tt.call)
			}
			var run rekey.Run
			if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil || run.UID != "rk1" {
				t.Errorf("body = %s (%v), want the run", rec.Body, err)
			}
		})
	}
}

func TestRoutes_guarded(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/projects/pj1/rekey", "/projects/pj1/rekey/pause"} {
		mover := &fakeMover{}
		method := http.MethodPost
		if rec := serve(t, mover, deny, method, path); rec.Code != http.StatusForbidden || len(mover.calls) != 0 {
			t.Errorf("%s %s behind a refusing guard = %d with calls %v, want 403 and none", method, path,
				rec.Code, mover.calls)
		}
	}
}

func TestRoutes_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"no run", rekey.ErrNoRun, http.StatusNotFound, codeNotFound},
		{"unknown project", rekey.ErrProjectNotFound, http.StatusNotFound, codeNotFound},
		{"already running", rekey.ErrRunActive, http.StatusConflict, codeAlreadyRunning},
		{"already prefixed", rekey.ErrAlreadyPrefixed, http.StatusConflict, codeAlreadyPrefixed},
		{"wrong state", rekey.ErrInvalidState, http.StatusConflict, codeInvalidState},
		{"anything else", errors.New("database down"), http.StatusInternalServerError, codeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := serve(t, &fakeMover{err: tt.err}, allow, http.MethodPost, "/projects/pj1/rekey")
			var body errorBody
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if rec.Code != tt.status || body.Error != tt.code {
				t.Errorf("POST = %d %q, want %d %q", rec.Code, body.Error, tt.status, tt.code)
			}
		})
	}
}

// TestRoutes_besideTheProjectsSubRouter verifies the flat routes coexist with a
// /projects sub-router mounted on the same router — the way internal/projectapi
// mounts its own — in either registration order: the sub-router keeps its
// /projects/{uid} routes and the move's routes still answer.
func TestRoutes_besideTheProjectsSubRouter(t *testing.T) {
	t.Parallel()
	projects := func(r chi.Router) {
		r.Route("/projects", func(r chi.Router) {
			r.Get("/{uid}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
			r.Patch("/{uid}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
		})
	}
	for _, rekeyFirst := range []bool{true, false} {
		mover := &fakeMover{run: rekey.Run{UID: "rk1"}}
		api := NewAPI(Config{Mover: mover, RequireMaintainer: allow})
		r := chi.NewRouter()
		if rekeyFirst {
			api.RegisterRoutes(r)
			projects(r)
		} else {
			projects(r)
			api.RegisterRoutes(r)
		}
		for _, tc := range []struct {
			method, path string
			want         int
		}{
			{http.MethodGet, "/projects/pj1", http.StatusTeapot},
			{http.MethodPatch, "/projects/pj1", http.StatusTeapot},
			{http.MethodGet, "/projects/pj1/rekey", http.StatusOK},
			{http.MethodPost, "/projects/pj1/rekey/retry", http.StatusOK},
		} {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
			if rec.Code != tc.want {
				t.Errorf("rekey first %v: %s %s = %d, want %d", rekeyFirst, tc.method, tc.path, rec.Code, tc.want)
			}
		}
	}
}
