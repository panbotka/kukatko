package mapsapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mapsapi"
	"github.com/panbotka/kukatko/internal/mapy"
)

// placesBody is the place-search response shape the tests decode.
type placesBody struct {
	Items []struct {
		Name     string  `json:"name"`
		Label    string  `json:"label"`
		Type     string  `json:"type"`
		Location string  `json:"location"`
		Lat      float64 `json:"lat"`
		Lng      float64 `json:"lng"`
	} `json:"items"`
}

// geocodePayload mirrors a real mapy.com /v1/geocode answer for "Veselí nad
// Moravou": the town itself, then a POI of the same name — the ambiguity the
// endpoint exists to let a user resolve.
const geocodePayload = `{"items":[
	{"name":"Veselí nad Moravou","label":"Město","type":"regional.municipality",
	 "location":"Česko","position":{"lon":17.37649,"lat":48.95363}},
	{"name":"Zámek Veselí nad Moravou","label":"Zámek","type":"poi",
	 "location":"Veselí nad Moravou, okres Hodonín, Jihomoravský kraj, Česko",
	 "position":{"lon":17.37619,"lat":48.95367}}],"locality":[]}`

// decodePlaces decodes a place-search response body, failing the test on a body
// that is not the documented shape.
func decodePlaces(t *testing.T, resp *http.Response) placesBody {
	t.Helper()
	var decoded placesBody
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

// TestGeocode_shapeAndUpstreamRequest checks a place search returns the ranked
// suggestions in the documented shape (name, label, type, disambiguating
// location, coordinates), and that the query reaches mapy.com with the key in the
// header — never in the URL — and the Czech locale, with the diacritics intact.
func TestGeocode_shapeAndUpstreamRequest(t *testing.T) {
	t.Parallel()
	var gotKey, gotPath string
	var gotQuery url.Values
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath, gotQuery = r.Header.Get("X-Mapy-Api-Key"), r.URL.Path, r.URL.Query()
		_, _ = io.WriteString(w, geocodePayload)
	})

	resp := ts.get(t, "/api/v1/map/geocode?q=Vesel%C3%AD+nad+Moravou&limit=2")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	decoded := decodePlaces(t, resp)
	if len(decoded.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(decoded.Items))
	}
	first := decoded.Items[0]
	if first.Name != "Veselí nad Moravou" || first.Label != "Město" || first.Type != "regional.municipality" {
		t.Errorf("first suggestion = %+v", first)
	}
	if first.Lat != 48.95363 || first.Lng != 17.37649 {
		t.Errorf("first coordinates = %v,%v, want 48.95363,17.37649", first.Lat, first.Lng)
	}
	// The second is what disambiguation is for: same name, different place.
	if second := decoded.Items[1]; !strings.Contains(second.Location, "okres Hodonín") {
		t.Errorf("second suggestion location = %q, want it to name its region", second.Location)
	}

	if gotKey != testAPIKey {
		t.Errorf("upstream key header = %q, want %q", gotKey, testAPIKey)
	}
	if gotPath != "/v1/geocode" {
		t.Errorf("upstream path = %q, want /v1/geocode", gotPath)
	}
	if got := gotQuery.Get("query"); got != "Veselí nad Moravou" {
		t.Errorf("upstream query = %q, want the accented name intact", got)
	}
	if got := gotQuery.Get("lang"); got != "cs" {
		t.Errorf("upstream lang = %q, want cs (Czech place names are the point)", got)
	}
	if got := gotQuery.Get("limit"); got != "2" {
		t.Errorf("upstream limit = %q, want 2", got)
	}
	if gotQuery.Has("apikey") {
		t.Error("upstream query carries the API key; it must stay in the header")
	}
}

// TestGeocode_cachesRepeatQuery checks a repeated search is served from the cache
// without a second upstream call — the whole reason a typeahead is affordable —
// and that the casefolding/whitespace normalisation of the key holds, while a
// different limit is correctly a different question.
func TestGeocode_cachesRepeatQuery(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, geocodePayload)
	})

	for _, path := range []string{
		"/api/v1/map/geocode?q=Brno",
		"/api/v1/map/geocode?q=brno",     // same question, different case
		"/api/v1/map/geocode?q=Brno++++", // same question, sloppier whitespace
	} {
		resp := ts.get(t, path)
		items := decodePlaces(t, resp).Items
		_ = resp.Body.Close()
		if len(items) != 2 {
			t.Errorf("GET %s items = %d, want 2 (a cache hit must answer in full)", path, len(items))
		}
	}
	if n := ts.upstreamN.Load(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (repeat queries must be cached)", n)
	}

	// A different limit is a different answer and must not reuse the entry.
	resp := ts.get(t, "/api/v1/map/geocode?q=Brno&limit=10")
	_ = resp.Body.Close()
	if n := ts.upstreamN.Load(); n != 2 {
		t.Errorf("upstream calls = %d, want 2 (a new limit is a new query)", n)
	}
}

// TestGeocode_noMatchIsEmptyNotError checks a name mapy.com places nowhere is an
// empty list and a 200: a half-typed name matching nothing is the normal state of
// a typeahead, not an error to shout about.
func TestGeocode_noMatchIsEmptyNotError(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"items":[],"locality":[]}`)
	})
	resp := ts.get(t, "/api/v1/map/geocode?q=qqqq")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if items := decodePlaces(t, resp).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

// TestGeocode_upstream404IsEmptyNotError checks that mapy.com answering 404 for an
// unplaceable name is likewise an empty list, not a 404 relayed to the browser.
func TestGeocode_upstream404IsEmptyNotError(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	resp := ts.get(t, "/api/v1/map/geocode?q=nikde")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if items := decodePlaces(t, resp).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

// TestGeocode_badParams checks a missing, blank or over-long query and a
// non-numeric limit are 400s answered before any upstream call — an empty
// typeahead must never cost a credit.
func TestGeocode_badParams(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, geocodePayload)
	})
	for _, path := range []string{
		"/api/v1/map/geocode",
		"/api/v1/map/geocode?q=",
		"/api/v1/map/geocode?q=+++",
		"/api/v1/map/geocode?q=Brno&limit=abc",
		"/api/v1/map/geocode?q=" + strings.Repeat("a", 201),
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

// TestGeocode_limitClamped checks an absurd limit is clamped to the cap rather
// than rejected or forwarded: it is a request worth answering with a sane number
// of rows.
func TestGeocode_limitClamped(t *testing.T) {
	t.Parallel()
	var gotLimit string
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		_, _ = io.WriteString(w, geocodePayload)
	})
	resp := ts.get(t, "/api/v1/map/geocode?q=Brno&limit=9999")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotLimit != "15" {
		t.Errorf("upstream limit = %q, want 15 (mapy.MaxGeocodeLimit)", gotLimit)
	}
}

// TestGeocode_upstreamErrors checks every upstream failure becomes a clean,
// typed client status — never a 500, never a leaked key — so the editor can say
// "place search unavailable" and carry on.
func TestGeocode_upstreamErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		upstream int
		want     int
	}{
		{"rejected key", http.StatusForbidden, mapsapi.StatusMapKeyRejected},
		{"expired key", http.StatusUnauthorized, mapsapi.StatusMapKeyRejected},
		{"out of credits", http.StatusTooManyRequests, http.StatusTooManyRequests},
		{"provider down", http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		{"unexpected upstream failure", http.StatusInternalServerError, http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.upstream)
				_, _ = io.WriteString(w, "denied key "+testAPIKey)
			})
			resp := ts.get(t, "/api/v1/map/geocode?q=Brno")
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tt.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if resp.StatusCode == http.StatusInternalServerError {
				t.Error("upstream failure surfaced as a 500")
			}
			if strings.Contains(string(body), testAPIKey) {
				t.Errorf("response leaks the API key: %s", body)
			}
		})
	}
}

// TestGeocode_failureNotCached checks an upstream failure is never cached: once
// mapy.com recovers, the very next search must reach it rather than replay the
// error for a day.
func TestGeocode_failureNotCached(t *testing.T) {
	t.Parallel()
	failing := true
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		if failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, geocodePayload)
	})
	resp := ts.get(t, "/api/v1/map/geocode?q=Brno")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}

	failing = false
	resp2 := ts.get(t, "/api/v1/map/geocode?q=Brno")
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry status = %d, want 200 (a failure must not be cached)", resp2.StatusCode)
	}
	if items := decodePlaces(t, resp2).Items; len(items) != 2 {
		t.Errorf("retry items = %d, want 2", len(items))
	}
}

// nilPlaceSearcher is a PlaceSearcher that reports "no results" as a nil slice,
// which is idiomatic Go and which the real client never produces.
type nilPlaceSearcher struct{}

// Geocode returns no places, as a nil slice.
func (nilPlaceSearcher) Geocode(_ context.Context, _ string, _ int) ([]mapy.Place, error) {
	return nil, nil
}

// TestGeocode_nilResultIsEmptyList checks a searcher returning a nil slice still
// answers `items: []`, not `items: null` — the client is promised a list, and a
// null would take its dropdown down with it.
func TestGeocode_nilResultIsEmptyList(t *testing.T) {
	t.Parallel()
	api := mapsapi.NewAPI(mapsapi.Config{
		Places: nilPlaceSearcher{}, Photos: &fakeLister{}, RequireAuth: passthroughAuth,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := httpGet(t, srv.URL+"/api/v1/map/geocode?q=Brno")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"items":[]`) {
		t.Errorf("body = %s, want items as an empty list, never null", body)
	}
}

// TestGeocode_notConfigured checks the endpoint answers 503 — not a 500 — when no
// mapy client is wired (no API key configured).
func TestGeocode_notConfigured(t *testing.T) {
	t.Parallel()
	api := mapsapi.NewAPI(mapsapi.Config{Photos: &fakeLister{}, RequireAuth: passthroughAuth})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := httpGet(t, srv.URL+"/api/v1/map/geocode?q=Brno")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// TestGeocode_rateLimited checks the shared geocode limiter throttles a burst of
// distinct searches with a 429 — the guard a client-side debounce cannot be
// trusted to provide — and that a cache hit is served regardless, since it costs
// no credit.
func TestGeocode_rateLimited(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, geocodePayload)
	}))
	t.Cleanup(upstream.Close)
	client, err := mapy.New(mapy.Config{BaseURL: upstream.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("mapy.New: %v", err)
	}
	api := mapsapi.NewAPI(mapsapi.Config{
		Tiles: client, Geocoder: client, Places: client,
		Photos: &fakeLister{}, RequireAuth: passthroughAuth,
		GeocodeRatePerSec: 1, GeocodeRateBurst: 2,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// Four distinct queries — as a per-keystroke typeahead would fire — against a
	// burst of 2.
	statuses := make([]int, 0, 4)
	for _, query := range []string{"Ves", "Vese", "Vesel", "Veselí"} {
		resp := httpGet(t, srv.URL+"/api/v1/map/geocode?q="+url.QueryEscape(query))
		_ = resp.Body.Close()
		statuses = append(statuses, resp.StatusCode)
	}
	if statuses[0] != http.StatusOK || statuses[1] != http.StatusOK {
		t.Errorf("first two statuses = %v, want 200,200", statuses[:2])
	}
	if statuses[3] != http.StatusTooManyRequests {
		t.Errorf("fourth status = %d, want 429", statuses[3])
	}

	// The first query is cached, so it costs no credit and must still be answered
	// even with the bucket empty.
	resp := httpGet(t, srv.URL+"/api/v1/map/geocode?q=Ves")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("cached query status = %d, want 200 (a cache hit spends no credit)", resp.StatusCode)
	}
}

// TestGeocode_recordsHealth checks a rejected key reaches the health tracker, so
// the admin dashboard reports it rather than leaving a user to wonder why the
// place search is quiet.
func TestGeocode_recordsHealth(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, &fakeLister{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	resp := ts.get(t, "/api/v1/map/geocode?q=Brno")
	_ = resp.Body.Close()
	if got := ts.health.Snapshot().State; got != mapy.HealthKeyRejected {
		t.Errorf("health state = %q, want %q", got, mapy.HealthKeyRejected)
	}
}
