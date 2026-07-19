package capabilitiesapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fakeReachability is a Reachability returning a fixed cached result.
type fakeReachability struct {
	reachable bool
}

// Reachable returns the configured cached result.
func (f fakeReachability) Reachable() bool { return f.reachable }

// passThrough is a no-op auth guard so the handler logic can be tested without
// the auth subsystem; the guard wiring itself is covered by
// TestHandleGet_RequiresAuth.
func passThrough(next http.Handler) http.Handler { return next }

// blockAnonymous is an auth guard standing in for RequireAuth: it answers 401
// unless the request carries a principal marker header, so a test can assert the
// route is actually mounted behind the guard.
func blockAnonymous(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Principal") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// newRouter mounts the capabilities API with the given reachability source and
// auth guard, returning a router ready for httptest requests.
func newRouter(reach Reachability, guard func(http.Handler) http.Handler) chi.Router {
	api := NewAPI(Config{Embeddings: reach, RequireAuth: guard})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r
}

// TestHandleGet_ReflectsFlag verifies semantic_search mirrors the cached
// reachability flag in both states.
func TestHandleGet_ReflectsFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reachable bool
	}{
		{name: "reachable advertises semantic search", reachable: true},
		{name: "unreachable hides semantic search", reachable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRouter(fakeReachability{reachable: tt.reachable}, passThrough)
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/api/v1/capabilities", nil)
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var body struct {
				SemanticSearch bool `json:"semantic_search"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decoding body: %v", err)
			}
			if body.SemanticSearch != tt.reachable {
				t.Errorf("semantic_search = %v, want %v", body.SemanticSearch, tt.reachable)
			}
		})
	}
}

// TestHandleGet_RequiresAuth verifies the route sits behind the injected auth
// guard: an unauthenticated request is rejected with 401, an authenticated one
// succeeds.
func TestHandleGet_RequiresAuth(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeReachability{reachable: true}, blockAnonymous)

	anon := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/capabilities", nil)
	r.ServeHTTP(anon, req)
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", anon.Code)
	}

	authed := httptest.NewRecorder()
	req = httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/capabilities", nil)
	req.Header.Set("X-Test-Principal", "viewer")
	r.ServeHTTP(authed, req)
	if authed.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", authed.Code)
	}
}
