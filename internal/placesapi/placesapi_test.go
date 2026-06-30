package placesapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
)

// fakeStore is an in-memory Store for handler tests: it records the country it
// was asked for and returns a canned hierarchy or error.
type fakeStore struct {
	gotCountry string
	result     []photos.CountryPlaces
	err        error
}

// AggregatePlaces records the requested country and returns the canned result.
func (f *fakeStore) AggregatePlaces(_ context.Context, country string) ([]photos.CountryPlaces, error) {
	f.gotCountry = country
	return f.result, f.err
}

// passthrough is an auth guard stand-in that admits every request, isolating the
// handler logic from the real auth middleware.
func passthrough(next http.Handler) http.Handler { return next }

// newTestServer mounts the places API backed by store under /api/v1.
func newTestServer(store Store) *httptest.Server {
	api := NewAPI(Config{Store: store, RequireAuth: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return httptest.NewServer(r)
}

// getPlaces issues a context-aware GET against the places endpoint at url.
func getPlaces(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// TestHandleList_returnsHierarchy verifies the endpoint serialises the store's
// hierarchy under the {places:[…]} envelope.
func TestHandleList_returnsHierarchy(t *testing.T) {
	t.Parallel()
	store := &fakeStore{result: []photos.CountryPlaces{
		{Country: "Czechia", Count: 5, Cities: []photos.CityCount{{City: "Praha", Count: 5}}},
	}}
	srv := newTestServer(store)
	defer srv.Close()

	resp := getPlaces(t, srv.URL+"/api/v1/places")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body placesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Places) != 1 || body.Places[0].Country != "Czechia" || body.Places[0].Count != 5 {
		t.Fatalf("places = %+v, want one Czechia/5 entry", body.Places)
	}
	if len(body.Places[0].Cities) != 1 || body.Places[0].Cities[0].City != "Praha" {
		t.Fatalf("cities = %+v, want [Praha]", body.Places[0].Cities)
	}
}

// TestHandleList_passesCountryFilter verifies the country query parameter reaches
// the store unchanged so a drill-down scopes the aggregation.
func TestHandleList_passesCountryFilter(t *testing.T) {
	t.Parallel()
	store := &fakeStore{result: []photos.CountryPlaces{}}
	srv := newTestServer(store)
	defer srv.Close()

	resp := getPlaces(t, srv.URL+"/api/v1/places?country=Czechia")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if store.gotCountry != "Czechia" {
		t.Fatalf("store saw country %q, want Czechia", store.gotCountry)
	}
}

// TestHandleList_storeError verifies a store failure becomes a 500.
func TestHandleList_storeError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{err: errors.New("boom")}
	srv := newTestServer(store)
	defer srv.Close()

	resp := getPlaces(t, srv.URL+"/api/v1/places")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
