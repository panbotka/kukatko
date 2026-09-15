package photoapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// stackRouter mounts the four stack endpoints without any auth middleware, so the
// handlers' own answers can be exercised directly.
func stackRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Post("/photos/stack", api.handleStackSelection)
	r.Post("/photos/{uid}/stack/primary", api.handleStackSetPrimary)
	r.Post("/photos/{uid}/unstack", api.handleUnstackMember)
	r.Post("/photos/{uid}/unstack-all", api.handleUnstackAll)
	return r
}

// TestStackEndpoints_unwired verifies an instance with stacking switched off
// answers 503 on every stack endpoint rather than panicking. The three
// single-photo handlers take their operation off the Stacker only after the nil
// check, because a method value read from a nil interface panics where it is
// written — which is what turned "the feature is off" into a 500.
func TestStackEndpoints_unwired(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/photos/stack",
		"/photos/pht1/stack/primary",
		"/photos/pht1/unstack",
		"/photos/pht1/unstack-all",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			stackRouter(NewAPI(Config{})).ServeHTTP(rec,
				httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503 (%s)", rec.Code, rec.Body)
			}
		})
	}
}
