package mapsapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mapsapi"
	"github.com/panbotka/kukatko/internal/mapy"
)

// tileHandler serves payload as a PNG tile for every upstream request.
func tileHandler(payload string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, payload)
	}
}

// newTestServerWithCache is newTestServer with an explicit server-side tile-cache
// budget (a non-positive value disables the cache).
func newTestServerWithCache(t *testing.T, upstreamHandler http.HandlerFunc, cacheBytes int64) *testServer {
	t.Helper()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		upstreamHandler(w, r)
	}))
	t.Cleanup(upstream.Close)

	client, err := mapy.New(mapy.Config{BaseURL: upstream.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("mapy.New: %v", err)
	}
	health := mapy.NewHealth()
	api := mapsapi.NewAPI(mapsapi.Config{
		Tiles:          client,
		Geocoder:       client,
		Photos:         &fakeLister{},
		Health:         health,
		RequireAuth:    passthroughAuth,
		TileCacheBytes: cacheBytes,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &testServer{
		server: srv, lister: &fakeLister{}, upstream: upstream, upstreamN: &calls, health: health,
	}
}

// TestTileCache_HitSkipsUpstream verifies a tile already fetched is served from
// the server-side cache without touching mapy.com again — every hit is a credit
// not spent, which is the whole point of the cache (the free tier bills one
// credit per tile).
func TestTileCache_HitSkipsUpstream(t *testing.T) {
	t.Parallel()
	const payload = "tile-bytes"
	ts := newTestServer(t, &fakeLister{}, tileHandler(payload))

	first := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
	firstBody, _ := io.ReadAll(first.Body)
	_ = first.Body.Close()
	second := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
	secondBody, _ := io.ReadAll(second.Body)
	_ = second.Body.Close()

	if n := ts.upstreamN.Load(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (the second tile must come from the cache)", n)
	}
	if string(firstBody) != payload || string(secondBody) != payload {
		t.Errorf("bodies = %q / %q, want both %q", firstBody, secondBody, payload)
	}
	if got := first.Header.Get("X-Tile-Cache"); got != "miss" {
		t.Errorf("first X-Tile-Cache = %q, want miss", got)
	}
	if got := second.Header.Get("X-Tile-Cache"); got != "hit" {
		t.Errorf("second X-Tile-Cache = %q, want hit", got)
	}
	if got := second.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("cached Content-Type = %q, want image/png", got)
	}
}

// TestTileCache_DistinctTilesAreDistinctEntries verifies the cache keys on the
// full tile identity, so a different coordinate, mapset or retina variant is
// fetched rather than answered with the wrong image.
func TestTileCache_DistinctTilesAreDistinctEntries(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, tileHandler("tile"))

	for _, path := range []string{
		"/api/v1/map/tiles/basic/3/4/5",
		"/api/v1/map/tiles/basic/3/4/6",
		"/api/v1/map/tiles/outdoor/3/4/5",
		"/api/v1/map/tiles/basic/3/4/5@2x",
	} {
		resp := ts.get(t, path)
		_ = resp.Body.Close()
	}

	if n := ts.upstreamN.Load(); n != 4 {
		t.Errorf("upstream calls = %d, want 4 (each tile identity is its own entry)", n)
	}
}

// TestTileCache_NeverCachesFailures verifies an upstream failure is not cached:
// a transient outage — or a key that has just been fixed in the mapy.com console
// — must not freeze into a hole in the map for the whole cache TTL.
func TestTileCache_NeverCachesFailures(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	for range 2 {
		resp := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
		_ = resp.Body.Close()
		if resp.StatusCode != mapsapi.StatusMapKeyRejected {
			t.Fatalf("status = %d, want %d", resp.StatusCode, mapsapi.StatusMapKeyRejected)
		}
	}

	if n := ts.upstreamN.Load(); n != 2 {
		t.Errorf("upstream calls = %d, want 2 (a failure must never be cached)", n)
	}
}

// TestTileCache_Disabled verifies a non-positive byte budget turns the cache off
// entirely, so every request goes upstream.
func TestTileCache_Disabled(t *testing.T) {
	t.Parallel()
	ts := newTestServerWithCache(t, tileHandler("tile"), -1)

	for range 2 {
		resp := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
		_ = resp.Body.Close()
	}

	if n := ts.upstreamN.Load(); n != 2 {
		t.Errorf("upstream calls = %d, want 2 (the cache is disabled)", n)
	}
}

// TestTileProxy_KeyRejectedIsDistinctAndDegrades verifies mapy.com's 401/403 —
// our key is expired, revoked or over quota — surfaces as its own status the
// frontend can recognise (never the raw upstream 403, which would blame the
// caller), leaves the API key out of the response, and marks the map backend
// degraded so the admin dashboard shows it without anyone opening the map.
func TestTileProxy_KeyRejectedIsDistinctAndDegrades(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		// mapy.com sometimes echoes the key back in its error payload.
		_, _ = io.WriteString(w, "rejected key "+testAPIKey)
	})

	resp := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != mapsapi.StatusMapKeyRejected {
		t.Errorf("status = %d, want %d (a rejected key, not a generic upstream error)",
			resp.StatusCode, mapsapi.StatusMapKeyRejected)
	}
	if resp.StatusCode == http.StatusForbidden {
		t.Error("the raw upstream 403 was passed through; the caller's request is not the problem")
	}
	if strings.Contains(string(body), testAPIKey) {
		t.Errorf("response leaks the API key: %s", body)
	}

	health := ts.health.Snapshot()
	if health.State != mapy.HealthKeyRejected {
		t.Errorf("health state = %q, want %q", health.State, mapy.HealthKeyRejected)
	}
	if !health.State.Degraded() {
		t.Error("health state is not degraded, want the map backend reported as degraded")
	}
	if strings.Contains(health.Detail, testAPIKey) {
		t.Errorf("health detail leaks the API key: %s", health.Detail)
	}
}

// TestTileProxy_SuccessKeepsHealthy verifies a served tile records the upstream
// as healthy, so a recovered key clears the degraded state.
func TestTileProxy_SuccessKeepsHealthy(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, tileHandler("tile"))

	resp := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
	_ = resp.Body.Close()

	if got := ts.health.Snapshot().State; got != mapy.HealthOK {
		t.Errorf("health state = %q, want %q", got, mapy.HealthOK)
	}
}

// TestReverseGeocode_KeyRejected verifies the geocode proxy classifies a rejected
// key exactly like the tile proxy does, rather than reporting a generic 502.
func TestReverseGeocode_KeyRejected(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	resp := ts.get(t, "/api/v1/map/rgeocode?lat=50.08&lng=14.44")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != mapsapi.StatusMapKeyRejected {
		t.Errorf("status = %d, want %d", resp.StatusCode, mapsapi.StatusMapKeyRejected)
	}
	if got := ts.health.Snapshot().State; got != mapy.HealthKeyRejected {
		t.Errorf("health state = %q, want %q", got, mapy.HealthKeyRejected)
	}
}
