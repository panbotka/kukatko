// Package expandapi exposes the "expand a collection" search over HTTP for editors
// and admins: GET /albums/{uid}/similar and GET /labels/{uid}/similar return the
// photos most like an album's or a label's members that are not in it yet, so a
// half-tagged library can be finished. Both are read-only — adding the found photos
// to the collection goes through the existing POST /photos/bulk path, so this
// package adds no second write path. It depends on a search behaviour and a write
// guard, both injected, so it stays decoupled from the expand package's wiring.
package expandapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/organize"
)

// Service is the search backend the endpoints delegate to. It is an interface so
// expandapi depends on the behaviour, not the expand package's wiring;
// *expand.Service satisfies it.
type Service interface {
	// Album expands an album, or returns organize.ErrAlbumNotFound.
	Album(ctx context.Context, albumUID string, req expand.Request) (expand.Result, error)
	// Label expands a label, or returns organize.ErrLabelNotFound.
	Label(ctx context.Context, labelUID string, req expand.Request) (expand.Result, error)
}

// finder is the common shape of Service.Album and Service.Label, so the two
// handlers share one implementation differing only in the not-found sentinel.
type finder func(ctx context.Context, uid string, req expand.Request) (expand.Result, error)

// API exposes the collection-expansion search over HTTP. The write guard is
// supplied by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	service      Service
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service makes the endpoints
// answer 503.
type Config struct {
	// Service backs the collection-expansion search.
	Service Service
	// RequireWrite guards the endpoints for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the expansion endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET /albums/{uid}/similar  RequireWrite  photos most like an album's members
//	GET /labels/{uid}/similar  RequireWrite  photos most like a label's members
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireWrite).Get("/albums/{uid}/similar", a.handleAlbum)
	r.With(a.requireWrite).Get("/labels/{uid}/similar", a.handleLabel)
}

// handleAlbum expands the path album.
func (a *API) handleAlbum(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "collection similarity not available")
		return
	}
	a.respond(w, r, a.service.Album, organize.ErrAlbumNotFound)
}

// handleLabel expands the path label.
func (a *API) handleLabel(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "collection similarity not available")
		return
	}
	a.respond(w, r, a.service.Label, organize.ErrLabelNotFound)
}

// respond parses the query, runs find for the path UID, and writes the result. An
// unparsable threshold or limit answers 400, the not-found sentinel 404, any other
// failure 500.
func (a *API) respond(w http.ResponseWriter, r *http.Request, find finder, notFound error) {
	req, err := parseRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := find(r.Context(), chi.URLParam(r, "uid"), req)
	if err != nil {
		if errors.Is(err, notFound) {
			writeError(w, http.StatusNotFound, "collection not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "collection similarity failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// parseRequest reads the optional ?threshold and ?limit query parameters. Both
// default (to the service's configured values) when absent; a non-numeric or
// negative value is rejected with an error safe to surface to the client.
func parseRequest(r *http.Request) (expand.Request, error) {
	query := r.URL.Query()
	threshold, err := parseNonNegativeFloat(query.Get("threshold"))
	if err != nil {
		return expand.Request{}, errors.New("threshold must be a non-negative number")
	}
	limit, err := parseNonNegativeInt(query.Get("limit"))
	if err != nil {
		return expand.Request{}, errors.New("limit must be a non-negative integer")
	}
	return expand.Request{Threshold: threshold, Limit: limit}, nil
}

// parseNonNegativeFloat parses a query float, mapping empty to zero (use the
// default) and rejecting a non-numeric or negative value.
func parseNonNegativeFloat(raw string) (float64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		return 0, errors.New("invalid value")
	}
	return value, nil
}

// parseNonNegativeInt parses a query integer, mapping empty to zero (use the
// default) and rejecting a non-numeric or negative value.
func parseNonNegativeInt(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, errors.New("invalid value")
	}
	return value, nil
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
		log.Printf("expandapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
