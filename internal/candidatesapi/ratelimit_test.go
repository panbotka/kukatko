package candidatesapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestRateLimit_mountedInsideTheGuard verifies every heavy vector route of the
// package carries the configured throttle, and carries it *inside* the curator
// guard: a refused caller is answered by the guard without spending a token, an
// admitted one reaches the throttle (which here refuses everything with 429),
// and only after the guard ran — the throttle keys on the identity the guard
// resolved, so mounted the other way round it would key everybody as nobody.
func TestRateLimit_mountedInsideTheGuard(t *testing.T) {
	t.Parallel()
	paths := []string{"/subjects/su1/candidates"}
	for _, admitted := range []bool{false, true} {
		var guarded, charged int
		guard := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !admitted {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				guarded++
				next.ServeHTTP(w, r)
			})
		}
		throttle := func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if guarded == charged {
					t.Error("throttle ran before the guard")
				}
				charged++
				w.WriteHeader(http.StatusTooManyRequests)
			})
		}
		router := chi.NewRouter()
		router.Route("/api/v1", NewAPI(Config{RequireCurator: guard, RateLimit: throttle}).RegisterRoutes)
		want := http.StatusForbidden
		if admitted {
			want = http.StatusTooManyRequests
		}
		for _, path := range paths {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
				"/api/v1"+path, strings.NewReader("{}"))
			router.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("admitted=%v %s = %d, want %d", admitted, path, rec.Code, want)
			}
		}
		if wantCharged := map[bool]int{false: 0, true: len(paths)}[admitted]; charged != wantCharged {
			t.Errorf("admitted=%v: throttle charged %d times, want %d", admitted, charged, wantCharged)
		}
	}
}
