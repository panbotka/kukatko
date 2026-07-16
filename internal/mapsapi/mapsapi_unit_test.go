package mapsapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mapsapi"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/photos"
)

const testAPIKey = "secret-mapy-key"

// fakeLister is a controllable PhotoLister capturing the params it was called
// with so tests can assert the forced/derived filters.
type fakeLister struct {
	photos    []photos.Photo
	gotParams photos.ListParams
	err       error
}

// List records params and returns the canned photos (or error).
func (f *fakeLister) List(_ context.Context, params photos.ListParams) ([]photos.Photo, error) {
	f.gotParams = params
	return f.photos, f.err
}

// passthroughAuth is a no-op auth middleware for tests (every request is allowed).
func passthroughAuth(next http.Handler) http.Handler {
	return next
}

// testServer wires a maps API (backed by a real mapy client pointed at fakeMapy)
// behind an httptest server, returning the server, the fake upstream, the photo
// lister and the health tracker so tests can drive and inspect them.
type testServer struct {
	server    *httptest.Server
	lister    *fakeLister
	upstream  *httptest.Server
	upstreamN *atomic.Int32
	health    *mapy.Health
}

// newTestServer builds a maps API whose mapy client targets a fake upstream
// running upstreamHandler, with the given photo lister. The upstream call count
// is exposed for caching assertions.
func newTestServer(t *testing.T, lister *fakeLister, upstreamHandler http.HandlerFunc) *testServer {
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
		Tiles:       client,
		Geocoder:    client,
		Places:      client,
		Photos:      lister,
		Health:      health,
		RequireAuth: passthroughAuth,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &testServer{
		server: srv, lister: lister, upstream: upstream, upstreamN: &calls, health: health,
	}
}

// get performs a GET against the test server and returns the response.
func (ts *testServer) get(t *testing.T, path string) *http.Response {
	t.Helper()
	return httpGet(t, ts.server.URL+path)
}

// httpGet performs a context-bearing GET against urlStr, failing on a transport
// error.
func httpGet(t *testing.T, urlStr string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, urlStr, nil)
	if err != nil {
		t.Fatalf("NewRequest %s: %v", urlStr, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", urlStr, err)
	}
	return resp
}

// TestTileProxy_forwardsKeyAndStreams checks the proxy adds the key header
// upstream (never to the URL), streams the tile bytes through unchanged, and sets
// a long-lived cache header — without the key appearing in the client response.
func TestTileProxy_forwardsKeyAndStreams(t *testing.T) {
	t.Parallel()
	const payload = "fake-tile-bytes"
	var gotKey, gotPath, gotQuery string
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath, gotQuery = r.Header.Get("X-Mapy-Api-Key"), r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, payload)
	})

	resp := ts.get(t, "/api/v1/map/tiles/basic/3/4/5")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != payload {
		t.Errorf("body = %q, want %q", body, payload)
	}
	if gotKey != testAPIKey {
		t.Errorf("upstream key header = %q, want %q", gotKey, testAPIKey)
	}
	if gotQuery != "" {
		t.Errorf("upstream query = %q, want empty (key must stay in header)", gotQuery)
	}
	if want := "/v1/maptiles/basic/256/3/4/5"; gotPath != want {
		t.Errorf("upstream path = %q, want %q", gotPath, want)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age=") {
		t.Errorf("Cache-Control = %q, want a max-age", cc)
	}
	if strings.Contains(string(body), testAPIKey) {
		t.Error("client response body leaks the API key")
	}
}

// TestTileProxy_retina checks the @2x suffix on the y segment requests a retina
// tile for a supporting mapset.
func TestTileProxy_retina(t *testing.T) {
	t.Parallel()
	var gotPath string
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "x")
	})
	resp := ts.get(t, "/api/v1/map/tiles/basic/3/4/5@2x")
	_ = resp.Body.Close()
	if !strings.Contains(gotPath, "/256@2x/") {
		t.Errorf("upstream path = %q, want a 256@2x segment", gotPath)
	}
}

// TestTileProxy_mapsetAllowlist checks an off-allow-list mapset is rejected with
// 400 before any upstream call.
func TestTileProxy_mapsetAllowlist(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	resp := ts.get(t, "/api/v1/map/tiles/satellite/1/2/3")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if n := ts.upstreamN.Load(); n != 0 {
		t.Errorf("upstream calls = %d, want 0 (allow-list must reject first)", n)
	}
}

// TestTileProxy_badCoordinates checks a non-integer coordinate is a 400.
func TestTileProxy_badCoordinates(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	resp := ts.get(t, "/api/v1/map/tiles/basic/3/x/5")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestTileProxy_upstreamErrors checks upstream statuses map to client-facing
// statuses and never leak the key.
func TestTileProxy_upstreamErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		upstream int
		want     int
	}{
		{"bad key", http.StatusForbidden, mapsapi.StatusMapKeyRejected},
		{"expired key", http.StatusUnauthorized, mapsapi.StatusMapKeyRejected},
		{"missing tile", http.StatusNotFound, http.StatusNotFound},
		{"unexpected upstream failure", http.StatusInternalServerError, http.StatusBadGateway},
		{"rate limited", http.StatusTooManyRequests, http.StatusTooManyRequests},
		{"provider down", http.StatusServiceUnavailable, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.upstream)
				_, _ = io.WriteString(w, "denied key "+testAPIKey)
			})
			resp := ts.get(t, "/api/v1/map/tiles/basic/1/1/1")
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if strings.Contains(string(body), testAPIKey) {
				t.Errorf("response leaks the API key: %s", body)
			}
		})
	}
}

// TestTileProxy_notConfigured checks the tile endpoint answers 503 when no mapy
// client is wired (unconfigured key).
func TestTileProxy_notConfigured(t *testing.T) {
	t.Parallel()
	api := mapsapi.NewAPI(mapsapi.Config{Photos: &fakeLister{}, RequireAuth: passthroughAuth})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := httpGet(t, srv.URL+"/api/v1/map/tiles/basic/1/1/1")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// geocodeBody is the simplified reverse-geocode response shape the tests decode.
type geocodeBody struct {
	Name              string `json:"name"`
	Location          string `json:"location"`
	RegionalStructure []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"regional_structure"`
}

const rgeocodePayload = `{"items":[
	{"name":"Staré Město","location":"Praha, Hlavní město Praha, Česko",
	 "regionalStructure":[{"name":"Staré Město","type":"regional.municipality_part"}]}]}`

// TestReverseGeocode_simplifiesAndCaches checks the proxy returns the simplified
// best match, forwards the key, and serves a repeat coordinate from cache (no
// second upstream call), with the key never leaking.
func TestReverseGeocode_simplifiesAndCaches(t *testing.T) {
	t.Parallel()
	var gotKey string
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Mapy-Api-Key")
		_, _ = io.WriteString(w, rgeocodePayload)
	})

	resp := ts.get(t, "/api/v1/map/rgeocode?lat=50.08&lng=14.42")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var decoded geocodeBody
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Name != "Staré Město" || decoded.Location == "" || len(decoded.RegionalStructure) != 1 {
		t.Errorf("simplified body = %+v", decoded)
	}
	if gotKey != testAPIKey {
		t.Errorf("upstream key header = %q, want %q", gotKey, testAPIKey)
	}

	// A repeat for the same coordinate must be served from cache.
	resp2 := ts.get(t, "/api/v1/map/rgeocode?lat=50.08&lng=14.42")
	_ = resp2.Body.Close()
	if n := ts.upstreamN.Load(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (second lookup must be cached)", n)
	}
}

// TestReverseGeocode_badParams checks missing/invalid coordinates are 400s.
func TestReverseGeocode_badParams(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, rgeocodePayload)
	})
	for _, path := range []string{
		"/api/v1/map/rgeocode",
		"/api/v1/map/rgeocode?lat=50.08",
		"/api/v1/map/rgeocode?lat=abc&lng=14.42",
		"/api/v1/map/rgeocode?lat=200&lng=14.42",
	} {
		resp := ts.get(t, path)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", path, resp.StatusCode)
		}
	}
	if n := ts.upstreamN.Load(); n != 0 {
		t.Errorf("upstream calls = %d, want 0 for invalid params", n)
	}
}

// TestReverseGeocode_rateLimited checks the per-second limiter returns 429 once
// the burst is spent on distinct coordinates.
func TestReverseGeocode_rateLimited(t *testing.T) {
	t.Parallel()
	lister := &fakeLister{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, rgeocodePayload)
	}))
	t.Cleanup(upstream.Close)
	client, err := mapy.New(mapy.Config{BaseURL: upstream.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("mapy.New: %v", err)
	}
	api := mapsapi.NewAPI(mapsapi.Config{
		Tiles: client, Geocoder: client, Photos: lister, RequireAuth: passthroughAuth,
		GeocodeRatePerSec: 1, GeocodeRateBurst: 2,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	statuses := make([]int, 0, 4)
	for _, coord := range []string{"1.1,2.1", "1.2,2.2", "1.3,2.3", "1.4,2.4"} {
		parts := strings.Split(coord, ",")
		resp := httpGet(t, srv.URL+"/api/v1/map/rgeocode?lat="+parts[0]+"&lng="+parts[1])
		_ = resp.Body.Close()
		statuses = append(statuses, resp.StatusCode)
	}
	// Burst of 2 allows the first two; the rest are throttled within the same second.
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusOK {
		t.Errorf("first two statuses = %v, want 200,200", statuses[:2])
	}
	if statuses[3] != http.StatusTooManyRequests {
		t.Errorf("fourth status = %d, want 429", statuses[3])
	}
}

// TestGeoJSON_shapeAndFilters checks the GeoJSON feed forces has-GPS, threads the
// standard filters through to the lister, emits RFC 7946 [lng,lat] coordinates,
// and skips a photo missing a coordinate.
func TestGeoJSON_shapeAndFilters(t *testing.T) {
	t.Parallel()
	lat1, lng1 := 50.0, 14.0
	lister := &fakeLister{photos: []photos.Photo{
		{UID: "ph_a", Title: "A", Lat: &lat1, Lng: &lng1},
		{UID: "ph_b"}, // no coordinates: must be skipped
	}}
	ts := newTestServer(t, lister, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	resp := ts.get(t, "/api/v1/map/photos?album=al_1&taken_after=2020-01-01&archived=only")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Type     string `json:"type"`
			Geometry struct {
				Type        string     `json:"type"`
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				UID   string `json:"uid"`
				Thumb string `json:"thumb"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 1 {
		t.Fatalf("collection = %s with %d features, want FeatureCollection with 1", fc.Type, len(fc.Features))
	}
	f := fc.Features[0]
	if f.Properties.UID != "ph_a" || f.Geometry.Type != "Point" {
		t.Errorf("feature = %+v", f)
	}
	if f.Geometry.Coordinates != [2]float64{lng1, lat1} {
		t.Errorf("coordinates = %v, want [lng,lat]=[%v,%v]", f.Geometry.Coordinates, lng1, lat1)
	}
	if !strings.Contains(f.Properties.Thumb, "ph_a/thumb/") {
		t.Errorf("thumb = %q, want a thumb path for ph_a", f.Properties.Thumb)
	}

	got := lister.gotParams
	if got.HasGPS == nil || !*got.HasGPS {
		t.Error("HasGPS not forced true on the GeoJSON query")
	}
	if len(got.AlbumUIDs) != 1 || got.AlbumUIDs[0] != "al_1" {
		t.Errorf("AlbumUIDs = %v, want [al_1]", got.AlbumUIDs)
	}
	if got.TakenAfter == nil {
		t.Error("TakenAfter not parsed")
	}
	if !got.OnlyArchived {
		t.Error("archived=only did not set OnlyArchived")
	}
}

// TestGeoJSON_badFilter checks an invalid filter value is a 400.
func TestGeoJSON_badFilter(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {})
	resp := ts.get(t, "/api/v1/map/photos?taken_after=not-a-date")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
