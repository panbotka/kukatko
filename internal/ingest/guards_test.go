package ingest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/ingest"
)

// stubGuard stands in for the auth middleware handed to NewAPI: refused answers
// 403, admitted answers 204 right at the guard instead of handing on. The
// handler never runs, so the test pins which guard a route hangs on — the
// wiring — without needing any of the handler's dependencies.
func stubGuard(allowed bool) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if allowed {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusForbidden)
		})
	}
}

// TestGuards_uploadHangsOnGivenGuard proves POST /upload is decided by the guard
// passed to NewAPI (in production auth.API.RequireAuth, so every signed-in role
// may upload): an admitting guard lets the request through, a refusing one
// answers 403.
func TestGuards_uploadHangsOnGivenGuard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		allowed bool
		want    int
	}{
		{"admitted", true, http.StatusNoContent},
		{"refused", false, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			router := chi.NewRouter()
			router.Route("/api/v1", ingest.NewAPI(nil, stubGuard(tc.allowed), nil).RegisterRoutes)
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
				"/api/v1/upload", strings.NewReader(`{}`))
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("POST /upload = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
