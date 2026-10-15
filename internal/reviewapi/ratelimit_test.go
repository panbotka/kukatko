package reviewapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestRateLimit_queueOnlyInsideTheGuard verifies the review queue (a rebuild scans the whole
// project) carries the heavy vector throttle, inside
// the curator guard, and that the answer and the leaderboard do not: a refused caller is answered by
// the guard without spending a token, an admitted one reaches the throttle (which
// here refuses everything with 429) — but only on the throttled route, and only
// after the guard ran. The throttle keys on the identity and the project the guard
// resolved, so mounted the other way round it would key everybody as nobody.
func TestRateLimit_queueOnlyInsideTheGuard(t *testing.T) {
	t.Parallel()
	routes := []struct {
		method, path string
		throttled    bool
	}{
		{http.MethodGet, "/review/queue", true},
		{http.MethodPost, "/review/answer", false},
		{http.MethodGet, "/review/leaderboard", false},
	}
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
		router.Route("/api/v1", NewAPI(Config{RequireCurator: guard, RequireAuth: guard, RateLimit: throttle}).RegisterRoutes)
		wantCharged := 0
		for _, route := range routes {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), route.method,
				"/api/v1"+route.path, strings.NewReader("{}"))
			router.ServeHTTP(rec, req)
			throttled := rec.Code == http.StatusTooManyRequests
			switch {
			case !admitted && rec.Code != http.StatusForbidden:
				t.Errorf("refused %s %s = %d, want 403", route.method, route.path, rec.Code)
			case admitted && throttled != route.throttled:
				t.Errorf("admitted %s %s = %d, throttled %v, want throttled %v",
					route.method, route.path, rec.Code, throttled, route.throttled)
			}
			if admitted && route.throttled {
				wantCharged++
			}
		}
		if charged != wantCharged {
			t.Errorf("admitted=%v: throttle charged %d times, want %d", admitted, charged, wantCharged)
		}
	}
}
