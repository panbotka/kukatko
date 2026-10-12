// Package savedsearchapi exposes per-user saved searches ("smart albums") over
// HTTP: a signed-in user can create, list, read, edit and delete their own named
// filter/search definitions. Every operation is scoped to the acting user and the
// request's project, both taken from the auth context, and a saved search owned
// by another user — or by the same user in another project — is reported as the
// very 404 a missing one gets, so a caller can never observe or mutate a search
// outside their own (user, project).
//
// The store is an interface and the auth guard is injected, so the package stays
// decoupled from the concrete store and from auth's wiring, and is unit-testable
// with fakes.
package savedsearchapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/savedsearch"
)

// Store is the subset of savedsearch.Store the endpoints need. It is an interface
// so the handlers depend on behaviour rather than the concrete store, keeping them
// unit-testable with fakes.
type Store interface {
	// Create inserts a saved search owned by ownerUID in projectUID and returns it.
	Create(
		ctx context.Context, projectUID, ownerUID, name string, params json.RawMessage,
	) (savedsearch.SavedSearch, error)
	// List returns ownerUID's saved searches in projectUID, newest first.
	List(ctx context.Context, projectUID, ownerUID string) ([]savedsearch.SavedSearch, error)
	// Get returns one saved search ownerUID owns in projectUID, or
	// savedsearch.ErrNotFound (also for another owner's or project's search).
	Get(ctx context.Context, projectUID, ownerUID, uid string) (savedsearch.SavedSearch, error)
	// Update rewrites the name and params of a saved search ownerUID owns in
	// projectUID, or returns savedsearch.ErrNotFound.
	Update(
		ctx context.Context, projectUID, ownerUID, uid, name string, params json.RawMessage,
	) (savedsearch.SavedSearch, error)
	// Delete removes a saved search ownerUID owns in projectUID, or returns
	// savedsearch.ErrNotFound.
	Delete(ctx context.Context, projectUID, ownerUID, uid string) error
}

// API exposes the saved-search endpoints over HTTP. The auth guard is supplied by
// the caller so this package depends on auth's behaviour, not its wiring.
type API struct {
	store       Store
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the saved-search reads and mutations.
	Store Store
	// RequireAuth guards every endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireAuth: cfg.RequireAuth}
}

// RegisterRoutes mounts the saved-search endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1). Every route requires auth
// and acts inside the request's project only:
//
//	GET    /saved-searches         list the caller's saved searches in the project
//	POST   /saved-searches         create a saved search in the project
//	GET    /saved-searches/{uid}   read one (404 if missing, not owner or other project)
//	PATCH  /saved-searches/{uid}   edit name/params (404 likewise)
//	DELETE /saved-searches/{uid}   delete one (404 likewise)
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/saved-searches", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleList)
		r.With(a.requireAuth).Post("/", a.handleCreate)
		r.With(a.requireAuth).Get("/{uid}", a.handleGet)
		r.With(a.requireAuth).Patch("/{uid}", a.handleUpdate)
		r.With(a.requireAuth).Delete("/{uid}", a.handleDelete)
	})
}

// handleList writes the acting user's saved searches in the request's project as
// {saved_searches:[…]}.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	searches, err := a.store.List(r.Context(), auth.ProjectUIDFromContext(r.Context()), user.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing saved searches failed")
		return
	}
	writeJSON(w, http.StatusOK, listResponse(searches))
}

// handleCreate creates a saved search owned by the acting user in the request's
// project and writes 201.
func (a *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	in, err := decodeCreate(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := a.store.Create(r.Context(), auth.ProjectUIDFromContext(r.Context()), user.UID, in.Name, in.Params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating saved search failed")
		return
	}
	writeJSON(w, http.StatusCreated, toView(saved))
}

// handleGet writes one saved search owned by the acting user, or 404.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	saved, ok := a.ownedSearch(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toView(saved))
}

// handleUpdate edits the name and/or params of one saved search owned by the
// acting user, or answers 404/400.
func (a *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	saved, ok := a.ownedSearch(w, r)
	if !ok {
		return
	}
	name, params, err := decodeUpdate(r, saved)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.store.Update(r.Context(), saved.ProjectUID, saved.OwnerUID, saved.UID, name, params)
	if err != nil {
		writeSearchError(w, err, "updating saved search failed")
		return
	}
	writeJSON(w, http.StatusOK, toView(updated))
}

// handleDelete removes one saved search owned by the acting user, or answers 404.
func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	saved, ok := a.ownedSearch(w, r)
	if !ok {
		return
	}
	if err := a.store.Delete(r.Context(), saved.ProjectUID, saved.OwnerUID, saved.UID); err != nil {
		writeSearchError(w, err, "deleting saved search failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownedSearch resolves the acting user and the {uid} path saved search, returning
// it only when it exists, belongs to that user and lives in the request's
// project. It writes the appropriate response (401/404/500) and reports ok=false
// otherwise. A search owned by a different user, or by the same user in another
// project, is the store's ErrNotFound and so the very 404 a missing one gets —
// never revealed.
func (a *API) ownedSearch(w http.ResponseWriter, r *http.Request) (savedsearch.SavedSearch, bool) {
	user, ok := currentUser(w, r)
	if !ok {
		return savedsearch.SavedSearch{}, false
	}
	saved, err := a.store.Get(r.Context(), auth.ProjectUIDFromContext(r.Context()), user.UID,
		chi.URLParam(r, "uid"))
	if err != nil {
		writeSearchError(w, err, "reading saved search failed")
		return savedsearch.SavedSearch{}, false
	}
	return saved, true
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

// writeSearchError maps a store error to an HTTP response: a missing row is 404,
// anything else is a 500 with the generic fallback message.
func writeSearchError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, savedsearch.ErrNotFound) {
		writeError(w, http.StatusNotFound, "saved search not found")
		return
	}
	writeError(w, http.StatusInternalServerError, fallback)
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
		log.Printf("savedsearchapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
