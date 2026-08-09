// Package searchhistoryapi exposes each user's recent search queries over HTTP:
// listing them, recording one that was just run, and clearing the lot. It is what
// makes the search box able to offer "what you searched for last time" on any
// device the user signs in from.
//
// Every operation is scoped to the acting user taken from the auth context. There
// is no path parameter and no owner in any body, so there is nothing to tamper
// with: a caller can only ever read, extend or clear their own history. Every
// signed-in role may use it, viewers included — searching is not curation.
//
// The store is an interface and the auth guard is injected, so the package stays
// decoupled from the concrete store and from auth's wiring, and is unit-testable
// with fakes.
package searchhistoryapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/searchhistory"
)

// maxBodyBytes caps the request body of a record call. A query is bounded by
// searchhistory.MaxQueryLength, so the limit only has to be comfortably above
// that plus the JSON envelope.
const maxBodyBytes = 8 << 10 // 8 KiB

// Store is the subset of searchhistory.Store the endpoints need. It is an
// interface so the handlers depend on behaviour rather than the concrete store,
// keeping them unit-testable with fakes.
type Store interface {
	// List returns userUID's recent searches, most recent first.
	List(ctx context.Context, userUID string) ([]searchhistory.Entry, error)
	// Record moves query to the front of userUID's history, pruning the overflow.
	// It returns searchhistory.ErrEmptyQuery for a blank query.
	Record(ctx context.Context, userUID, query string) error
	// Clear forgets userUID's whole history.
	Clear(ctx context.Context, userUID string) error
}

// API exposes the search-history endpoints over HTTP. The auth guard is supplied
// by the caller so this package depends on auth's behaviour, not its wiring.
type API struct {
	store       Store
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the history reads and writes.
	Store Store
	// RequireAuth guards every endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireAuth: cfg.RequireAuth}
}

// RegisterRoutes mounts the search-history endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1). Every route requires auth
// and acts only on the caller's own history:
//
//	GET    /search-history   list the caller's recent searches, newest first
//	POST   /search-history   remember a query that was just run
//	DELETE /search-history   forget the caller's whole history
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/search-history", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleList)
		r.With(a.requireAuth).Post("/", a.handleRecord)
		r.With(a.requireAuth).Delete("/", a.handleClear)
	})
}

// handleList writes the acting user's recent searches as {searches:[…]}.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	entries, err := a.store.List(r.Context(), user.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing search history failed")
		return
	}
	writeJSON(w, http.StatusOK, listResponse(entries))
}

// handleRecord remembers one query the acting user just ran and answers 204.
//
// It is deliberately a fire-and-forget write with no body: the client already
// knows what it searched for, and the refreshed list is only wanted when the
// dropdown is next opened, which is a GET.
func (a *API) handleRecord(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	in, err := decodeRecord(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.Record(r.Context(), user.UID, in.Query); err != nil {
		if errors.Is(err, searchhistory.ErrEmptyQuery) {
			writeError(w, http.StatusBadRequest, errBlankQuery.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "recording the search failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleClear forgets the acting user's whole history and answers 204. It is
// idempotent: clearing an already-empty history is a success.
func (a *API) handleClear(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if err := a.store.Clear(r.Context(), user.UID); err != nil {
		writeError(w, http.StatusInternalServerError, "clearing search history failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// errBlankQuery is returned when a record request supplies a query that holds
// nothing but whitespace, so there is no search to remember.
var errBlankQuery = errors.New("query is required")

// recordInput is the JSON body accepted by the record endpoint.
type recordInput struct {
	Query string `json:"query"`
}

// decodeRecord decodes a record body, rejecting unknown fields, an oversized body
// and a query that is blank before it ever reaches the store. The returned error
// message is safe to surface to the client.
func decodeRecord(r *http.Request) (recordInput, error) {
	var in recordInput
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return recordInput{}, errors.New("invalid request body: " + err.Error())
	}
	if searchhistory.Normalize(in.Query) == "" {
		return recordInput{}, errBlankQuery
	}
	return in, nil
}

// listEnvelope wraps the recent searches under the searches key.
type listEnvelope struct {
	Searches []searchhistory.Entry `json:"searches"`
}

// listResponse builds the list endpoint envelope, turning a nil slice into an
// empty JSON array so the client always parses one shape.
func listResponse(entries []searchhistory.Entry) listEnvelope {
	if entries == nil {
		entries = []searchhistory.Entry{}
	}
	return listEnvelope{Searches: entries}
}

// currentUser returns the authenticated user from the request context, writing a
// 401 and reporting ok=false when none is present (a defensive guard; RequireAuth
// should have already rejected the request).
func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return auth.User{}, false
	}
	return user, true
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
		log.Printf("searchhistoryapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
