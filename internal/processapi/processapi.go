// Package processapi exposes admin-only HTTP endpoints that kick off bulk
// catalogue processing. Its first action is the embedding backfill, which
// enqueues an image_embed job for every photo that still lacks an embedding —
// the recovery path for photos uploaded while the embeddings box was offline or
// imported before embeddings existed. It depends only on a Backfiller behaviour
// and an admin guard, both injected, so it stays decoupled from the job and
// vector layers.
package processapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Backfiller enqueues background work for photos missing derived data. It is
// satisfied by embedjob.Service.
type Backfiller interface {
	// BackfillEmbeddings enqueues an image_embed job for every photo missing an
	// embedding and returns how many were scheduled.
	BackfillEmbeddings(ctx context.Context) (int, error)
}

// API exposes the processing endpoints over HTTP. The admin guard is supplied by
// the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	backfiller   Backfiller
	requireAdmin func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Both fields are required.
type Config struct {
	// Backfiller runs the bulk backfill actions.
	Backfiller Backfiller
	// RequireAdmin guards every endpoint for administrators only.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		backfiller:   cfg.Backfiller,
		requireAdmin: cfg.RequireAdmin,
	}
}

// RegisterRoutes mounts the processing endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	POST /process/embeddings  RequireAdmin  backfill missing image embeddings
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/process", func(r chi.Router) {
		r.With(a.requireAdmin).Post("/embeddings", a.handleBackfillEmbeddings)
	})
}

// backfillResponse is the JSON body returned by the embedding-backfill endpoint.
type backfillResponse struct {
	// Enqueued is the number of image_embed jobs scheduled by this call.
	Enqueued int `json:"enqueued"`
}

// handleBackfillEmbeddings enqueues image_embed jobs for all photos missing an
// embedding and reports how many were scheduled.
func (a *API) handleBackfillEmbeddings(w http.ResponseWriter, r *http.Request) {
	enqueued, err := a.backfiller.BackfillEmbeddings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling embeddings failed")
		return
	}
	writeJSON(w, http.StatusOK, backfillResponse{Enqueued: enqueued})
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
		log.Printf("processapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
