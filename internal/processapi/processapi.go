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

// Backfiller enqueues an image_embed job for every photo missing an embedding.
// It is satisfied by embedjob.Service.
type Backfiller interface {
	// BackfillEmbeddings enqueues an image_embed job for every photo missing an
	// embedding and returns how many were scheduled.
	BackfillEmbeddings(ctx context.Context) (int, error)
}

// FaceBackfiller enqueues a face_detect job for every photo that has not yet had
// face detection run. It is satisfied by facejob.Service.
type FaceBackfiller interface {
	// BackfillFaces enqueues a face_detect job for every unprocessed photo and
	// returns how many were scheduled.
	BackfillFaces(ctx context.Context) (int, error)
}

// Reclusterer groups the currently unassigned, unclustered faces into clusters.
// It is satisfied by cluster.Service. A nil Reclusterer disables the
// /process/clusters endpoint (it answers 503).
type Reclusterer interface {
	// Recluster groups clusterable faces into clusters and returns how many
	// clusters were created.
	Recluster(ctx context.Context) (int, error)
}

// API exposes the processing endpoints over HTTP. The admin guard is supplied by
// the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	backfiller     Backfiller
	faceBackfiller FaceBackfiller
	reclusterer    Reclusterer
	requireAdmin   func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Backfiller, FaceBackfiller and
// RequireAdmin are required; Reclusterer is optional (a nil value disables the
// clustering endpoint).
type Config struct {
	// Backfiller runs the embedding backfill.
	Backfiller Backfiller
	// FaceBackfiller runs the face-detection backfill.
	FaceBackfiller FaceBackfiller
	// Reclusterer runs the face auto-clustering pass.
	Reclusterer Reclusterer
	// RequireAdmin guards every endpoint for administrators only.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		backfiller:     cfg.Backfiller,
		faceBackfiller: cfg.FaceBackfiller,
		reclusterer:    cfg.Reclusterer,
		requireAdmin:   cfg.RequireAdmin,
	}
}

// RegisterRoutes mounts the processing endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	POST /process/embeddings  RequireAdmin  backfill missing image embeddings
//	POST /process/faces       RequireAdmin  backfill missing face detections
//	POST /process/clusters    RequireAdmin  rebuild face clusters from unassigned faces
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/process", func(r chi.Router) {
		r.With(a.requireAdmin).Post("/embeddings", a.handleBackfillEmbeddings)
		r.With(a.requireAdmin).Post("/faces", a.handleBackfillFaces)
		r.With(a.requireAdmin).Post("/clusters", a.handleRecluster)
	})
}

// backfillResponse is the JSON body returned by the embedding-backfill endpoint.
type backfillResponse struct {
	// Enqueued is the number of image_embed jobs scheduled by this call.
	Enqueued int `json:"enqueued"`
}

// reclusterResponse is the JSON body returned by the clustering endpoint.
type reclusterResponse struct {
	// Created is the number of clusters formed by this call.
	Created int `json:"created"`
}

// handleRecluster groups the currently unassigned, unclustered faces into
// clusters and reports how many clusters were created. It answers 503 when no
// clustering backend is wired.
func (a *API) handleRecluster(w http.ResponseWriter, r *http.Request) {
	if a.reclusterer == nil {
		writeError(w, http.StatusServiceUnavailable, "face clustering not available")
		return
	}
	created, err := a.reclusterer.Recluster(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reclustering faces failed")
		return
	}
	writeJSON(w, http.StatusOK, reclusterResponse{Created: created})
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

// handleBackfillFaces enqueues face_detect jobs for all photos that have not yet
// had face detection run and reports how many were scheduled.
func (a *API) handleBackfillFaces(w http.ResponseWriter, r *http.Request) {
	enqueued, err := a.faceBackfiller.BackfillFaces(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling faces failed")
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
