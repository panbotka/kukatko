// Package placesapi exposes the reverse-geocoded place hierarchy for browsing
// over HTTP: a signed-in user can list the countries (and their cities) that the
// non-archived photo library covers, with per-place photo counts, and drill into
// a single country's cities. Browsing a place's actual photos goes through the
// shared GET /photos?country=&city= list, exactly like album and label scoping;
// this package only serves the aggregated counts.
//
// The aggregation source is an interface and the auth guard is injected, so the
// package stays decoupled from the concrete store and from auth's wiring, and is
// unit-testable with fakes.
package placesapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
)

// Store is the subset of photos.Store the endpoint needs. It is an interface so
// the handler depends on behaviour rather than the concrete store, keeping it
// unit-testable with fakes.
type Store interface {
	// AggregatePlaces returns the country/city place hierarchy with per-place
	// photo counts over non-archived photos. A non-empty country scopes the
	// result to that one country's cities.
	AggregatePlaces(ctx context.Context, country string) ([]photos.CountryPlaces, error)
}

// API exposes the places browse endpoint over HTTP. The auth guard is supplied by
// the caller so this package depends on auth's behaviour, not its wiring.
type API struct {
	store       Store
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the place aggregation.
	Store Store
	// RequireAuth guards the endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireAuth: cfg.RequireAuth}
}

// RegisterRoutes mounts the places endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1). The route requires auth:
//
//	GET /places           the country/city place hierarchy with counts
//	GET /places?country=  drill into one country's cities only
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/places", a.handleList)
}

// placesResponse is the JSON envelope for the place hierarchy.
type placesResponse struct {
	Places []photos.CountryPlaces `json:"places"`
}

// handleList writes the place hierarchy as {places:[…]}, optionally scoped to the
// country query parameter. The aggregation already excludes archived photos and
// photos without place data, and sorts countries and cities by count then name.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	country := r.URL.Query().Get("country")
	places, err := a.store.AggregatePlaces(r.Context(), country)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "aggregating places failed")
		return
	}
	writeJSON(w, http.StatusOK, placesResponse{Places: places})
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("placesapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
