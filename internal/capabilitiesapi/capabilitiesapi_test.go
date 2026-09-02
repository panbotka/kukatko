package capabilitiesapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/version"
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

// testBuild is the pinned build metadata the router under test reports, so the
// assertions do not depend on the linker flags of the test binary.
var testBuild = version.Info{Version: "1.2.3", Commit: "abc1234"}

// newRouter mounts the capabilities API with the given reachability source and
// auth guard (and the pinned testBuild), returning a router ready for httptest
// requests.
func newRouter(reach Reachability, guard func(http.Handler) http.Handler) chi.Router {
	return newRouterWithBuild(reach, guard, testBuild)
}

// newRouterWithBuild is newRouter with the reported build metadata pinned to
// build, so a test can cover a development build's placeholders.
func newRouterWithBuild(
	reach Reachability, guard func(http.Handler) http.Handler, build version.Info,
) chi.Router {
	return newRouterWithPasskeys(reach, guard, build, false)
}

// newRouterWithPasskeys is newRouterWithBuild with the passkey flag pinned too,
// so a test can cover both an instance that has a relying party configured and
// one that has not.
func newRouterWithPasskeys(
	reach Reachability, guard func(http.Handler) http.Handler, build version.Info, passkeys bool,
) chi.Router {
	api := NewAPI(Config{Embeddings: reach, Passkeys: passkeys, Build: build, RequireAuth: guard})
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

// TestHandleGet_passkeys pins the passkey flag onto the deployment's
// configuration rather than onto anything that can change while the process
// runs: it is what decides whether the sign-in screen offers the button at all,
// and an instance with no relying party must never advertise one.
func TestHandleGet_passkeys(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "not configured", true: "configured"}[enabled], func(t *testing.T) {
			t.Parallel()
			r := newRouterWithPasskeys(fakeReachability{}, passThrough, testBuild, enabled)
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(
				context.Background(), http.MethodGet, "/api/v1/capabilities", nil)
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var body capabilities
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decoding body: %v", err)
			}
			if body.Passkeys != enabled {
				t.Errorf("passkeys = %v, want %v", body.Passkeys, enabled)
			}
		})
	}
}

// TestHandleGet_PayloadShape pins the JSON body: the flags and the nested build
// object under the exact keys the frontend reads (the user menu prints the
// version from this response, so a renamed key would silently blank it out).
func TestHandleGet_PayloadShape(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeReachability{reachable: true}, passThrough)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/capabilities", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(body) != 3 {
		t.Errorf("body keys = %v, want exactly semantic_search, passkeys and version", body)
	}
	if got, ok := body["semantic_search"].(bool); !ok || !got {
		t.Errorf("semantic_search = %v, want true", body["semantic_search"])
	}
	if got, ok := body["passkeys"].(bool); !ok || got {
		t.Errorf("passkeys = %v, want false", body["passkeys"])
	}
	build, ok := body["version"].(map[string]any)
	if !ok {
		t.Fatalf("version = %v, want a nested object", body["version"])
	}
	if got := build["version"]; got != testBuild.Version {
		t.Errorf("version.version = %v, want %q", got, testBuild.Version)
	}
	if got := build["commit"]; got != testBuild.Commit {
		t.Errorf("version.commit = %v, want %q", got, testBuild.Commit)
	}
}

// TestHandleGet_DevBuild verifies an un-stamped (development) build is reported
// verbatim with its "dev"/"none" placeholders rather than being hidden, leaving
// it to the client to decide how to present them.
func TestHandleGet_DevBuild(t *testing.T) {
	t.Parallel()

	dev := version.Info{Version: "dev", Commit: "none"}
	r := newRouterWithBuild(fakeReachability{reachable: false}, passThrough, dev)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/capabilities", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Version version.Info `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Version != dev {
		t.Errorf("version = %+v, want %+v", body.Version, dev)
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
