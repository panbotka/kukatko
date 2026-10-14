package mapsapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mapy"
)

// projectHeader names the header the per-project tests carry their project in,
// standing in for the project the route guard resolves in production.
const projectHeader = "X-Test-Project"

// countingGeocoder answers every lookup in both directions and counts how many
// reached it, so a test can tell a cache hit from an upstream call.
type countingGeocoder struct {
	mu      sync.Mutex
	reverse int
	forward int
}

// ReverseGeocode counts the call and answers a fixed place.
func (g *countingGeocoder) ReverseGeocode(context.Context, float64, float64) (*mapy.GeocodeResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reverse++
	return &mapy.GeocodeResult{Name: "Veselice"}, nil
}

// Geocode counts the call and answers one fixed suggestion.
func (g *countingGeocoder) Geocode(context.Context, string, int) ([]mapy.Place, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forward++
	return []mapy.Place{{Name: "Veselice"}}, nil
}

// calls returns the reverse and forward upstream call counts.
func (g *countingGeocoder) calls() (reverse, forward int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reverse, g.forward
}

// newProjectRouter builds the maps routes over cfg (with the counting geocoder
// behind both directions), reading each request's project from projectHeader.
func newProjectRouter(t *testing.T, cfg Config) (http.Handler, *countingGeocoder) {
	t.Helper()
	geo := &countingGeocoder{}
	cfg.Geocoder, cfg.Places = geo, geo
	cfg.RequireAuth = func(next http.Handler) http.Handler { return next }
	api := NewAPI(cfg)
	api.projectOf = func(r *http.Request) string { return r.Header.Get(projectHeader) }
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r, geo
}

// serve runs one GET against h as a member of projectUID and returns the status.
func serve(t *testing.T, h http.Handler, projectUID, path string) int {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	req.Header.Set(projectHeader, projectUID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestGeocode_projectShareLeavesTheInstanceBudget verifies one project spending
// its whole share is refused before it can drain the instance bucket: with a
// project burst of 2 under an instance burst of 3, project A gets two lookups
// and a 429, and project B still gets its lookup — in both directions. Without
// the per-project bucket A's third lookup would have taken the instance's last
// token and B would be the one refused.
func TestGeocode_projectShareLeavesTheInstanceBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path func(i int) string
	}{
		{"reverse", func(i int) string { return "/api/v1/map/rgeocode?lat=49." + strconv.Itoa(i) + "&lng=17" }},
		{"forward", func(i int) string { return "/api/v1/map/geocode?q=place" + strconv.Itoa(i) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, _ := newProjectRouter(t, Config{
				GeocodeRatePerSec: 0.001, GeocodeRateBurst: 3,
				GeocodeProjectRatePerSec: 0.001, GeocodeProjectRateBurst: 2,
			})
			want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests}
			for i, code := range want {
				if got := serve(t, h, "pj-a", tt.path(i)); got != code {
					t.Errorf("project A lookup %d status = %d, want %d", i, got, code)
				}
			}
			if got := serve(t, h, "pj-b", tt.path(10)); got != http.StatusOK {
				t.Errorf("project B lookup status = %d, want 200 (A must not drain the instance)", got)
			}
			// The instance cap still bounds the sum: its three tokens are spent.
			if got := serve(t, h, "pj-c", tt.path(20)); got != http.StatusTooManyRequests {
				t.Errorf("project C lookup status = %d, want 429 (instance budget spent)", got)
			}
		})
	}
}

// TestGeocode_cacheIsPerProject verifies a geocode answer cached in one project
// is not served to another: the second project's identical question goes
// upstream (so its latency reveals nothing about the first project's lookups),
// while a repeat inside the same project is still a free cache hit.
func TestGeocode_cacheIsPerProject(t *testing.T) {
	t.Parallel()

	h, geo := newProjectRouter(t, Config{})
	const reverse = "/api/v1/map/rgeocode?lat=49.1&lng=17.1"
	const forward = "/api/v1/map/geocode?q=Veselice"
	for _, step := range []struct{ project, path string }{
		{"pj-a", reverse}, {"pj-a", reverse}, {"pj-b", reverse},
		{"pj-a", forward}, {"pj-a", forward}, {"pj-b", forward},
	} {
		if got := serve(t, h, step.project, step.path); got != http.StatusOK {
			t.Fatalf("GET %s in %s status = %d, want 200", step.path, step.project, got)
		}
	}
	if rev, fwd := geo.calls(); rev != 2 || fwd != 2 {
		t.Errorf("upstream calls = %d reverse, %d forward; want 2 and 2 (one per project)", rev, fwd)
	}
}

// TestNewAPI_projectGeocodeDefaults verifies the per-project share takes its
// defaults when left zero and is switched off by a negative rate, leaving the
// instance cap alone in charge.
func TestNewAPI_projectGeocodeDefaults(t *testing.T) {
	t.Parallel()

	defaulted := NewAPI(Config{})
	for i := range defaultGeocodeProjectRateBurst {
		if !defaulted.projectGeocodeLimiter.Allow("pj") {
			t.Fatalf("default project bucket refused lookup %d of its burst %d", i, defaultGeocodeProjectRateBurst)
		}
	}
	if defaulted.projectGeocodeLimiter.Allow("pj") {
		t.Error("default project bucket allowed a lookup past its burst")
	}

	disabled := NewAPI(Config{GeocodeProjectRatePerSec: -1})
	for range 3 * defaultGeocodeProjectRateBurst {
		if !disabled.projectGeocodeLimiter.Allow("pj") {
			t.Fatal("a disabled project share refused a lookup")
		}
	}
}

// TestProjectCacheKey verifies the same question asked in two projects is two
// cache entries, and that the project cannot be confused with the key.
func TestProjectCacheKey(t *testing.T) {
	t.Parallel()

	if projectCacheKey("pj-a", "10:brno") == projectCacheKey("pj-b", "10:brno") {
		t.Error("two projects share one cache key")
	}
	if got, want := projectCacheKey("pj-a", "10:brno"), "pj-a|10:brno"; got != want {
		t.Errorf("projectCacheKey = %q, want %q", got, want)
	}
}
