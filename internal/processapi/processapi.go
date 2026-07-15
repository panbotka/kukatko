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
	"strings"

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

// PlacesBackfiller enqueues a `places` job for every geotagged photo missing
// place data. It is satisfied by placesjob.Service. A nil PlacesBackfiller
// disables the /process/places endpoint (it answers 503), which is how the server
// degrades when no mapy.com key is configured.
type PlacesBackfiller interface {
	// BackfillPlaces enqueues a `places` job for every geotagged photo missing a
	// cached place and returns how many were scheduled.
	BackfillPlaces(ctx context.Context) (int, error)
}

// ThumbnailBackfiller enqueues a thumbnail job for every photo missing a
// generated thumbnail. It is satisfied by thumbjob.Service. When all is true it
// schedules every non-archived photo instead (a forced full re-run). Thumbnail
// jobs run locally, so the backfill works regardless of the embeddings box being
// offline. A nil ThumbnailBackfiller disables the /process/thumbnails endpoint
// (it answers 503).
type ThumbnailBackfiller interface {
	// BackfillThumbnails enqueues a thumbnail job for every photo missing a
	// thumbnail (or, when all is true, for every non-archived photo) and returns
	// how many were scheduled.
	BackfillThumbnails(ctx context.Context, all bool) (int, error)
}

// MetadataBackfiller enqueues a `metadata` job for every photo whose original has
// never been read out into the IPTC/XMP and file-technical columns. It is satisfied
// by metajob.Service. When all is true it schedules every non-archived photo
// instead (a forced full re-read). Metadata jobs run locally, so the backfill works
// regardless of the embeddings box being offline. A nil MetadataBackfiller disables
// the /process/metadata endpoint (it answers 503).
type MetadataBackfiller interface {
	// BackfillMetadata enqueues a `metadata` job for every photo whose file metadata
	// has never been read (or, when all is true, for every non-archived photo) and
	// returns how many were scheduled.
	BackfillMetadata(ctx context.Context, all bool) (int, error)
}

// StacksDetector groups the several files of one shot (RAW+JPEG, exported edits,
// …) into stacks by the enabled detection rules. It is satisfied by
// stacks.Service and runs synchronously like the reclusterer (the grouping is a
// couple of indexed queries). A nil StacksDetector — the feature disabled in
// config — makes the /process/stacks endpoint answer 503.
type StacksDetector interface {
	// DetectStacks groups the currently unstacked photos and returns how many
	// stacks were created. It is idempotent: a re-run over a settled library
	// creates nothing.
	DetectStacks(ctx context.Context) (int, error)
}

// API exposes the processing endpoints over HTTP. The admin guard is supplied by
// the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	backfiller       Backfiller
	faceBackfiller   FaceBackfiller
	reclusterer      Reclusterer
	placesBackfiller PlacesBackfiller
	thumbBackfiller  ThumbnailBackfiller
	metaBackfiller   MetadataBackfiller
	stacksDetector   StacksDetector
	requireAdmin     func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Backfiller, FaceBackfiller and
// RequireAdmin are required; Reclusterer and PlacesBackfiller are optional (a nil
// value disables the corresponding endpoint, which answers 503).
type Config struct {
	// Backfiller runs the embedding backfill.
	Backfiller Backfiller
	// FaceBackfiller runs the face-detection backfill.
	FaceBackfiller FaceBackfiller
	// Reclusterer runs the face auto-clustering pass.
	Reclusterer Reclusterer
	// PlacesBackfiller runs the reverse-geocode (place) backfill.
	PlacesBackfiller PlacesBackfiller
	// ThumbnailBackfiller runs the missing-thumbnail backfill.
	ThumbnailBackfiller ThumbnailBackfiller
	// MetadataBackfiller runs the file-metadata (IPTC/XMP) backfill.
	MetadataBackfiller MetadataBackfiller
	// StacksDetector runs the automatic stack-detection pass.
	StacksDetector StacksDetector
	// RequireAdmin guards every endpoint for administrators only.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		backfiller:       cfg.Backfiller,
		faceBackfiller:   cfg.FaceBackfiller,
		reclusterer:      cfg.Reclusterer,
		placesBackfiller: cfg.PlacesBackfiller,
		thumbBackfiller:  cfg.ThumbnailBackfiller,
		metaBackfiller:   cfg.MetadataBackfiller,
		stacksDetector:   cfg.StacksDetector,
		requireAdmin:     cfg.RequireAdmin,
	}
}

// RegisterRoutes mounts the processing endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	POST /process/embeddings  RequireAdmin  backfill missing image embeddings
//	POST /process/faces       RequireAdmin  backfill missing face detections
//	POST /process/clusters    RequireAdmin  rebuild face clusters from unassigned faces
//	POST /process/places      RequireAdmin  backfill missing reverse-geocoded places
//	POST /process/thumbnails  RequireAdmin  backfill missing thumbnails (?all=true forces a full re-run)
//	POST /process/metadata    RequireAdmin  backfill unread file metadata (?all=true forces a full re-read)
//	POST /process/stacks      RequireAdmin  detect and form stacks over the library
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/process", func(r chi.Router) {
		r.With(a.requireAdmin).Post("/embeddings", a.handleBackfillEmbeddings)
		r.With(a.requireAdmin).Post("/faces", a.handleBackfillFaces)
		r.With(a.requireAdmin).Post("/clusters", a.handleRecluster)
		r.With(a.requireAdmin).Post("/places", a.handleBackfillPlaces)
		r.With(a.requireAdmin).Post("/thumbnails", a.handleBackfillThumbnails)
		r.With(a.requireAdmin).Post("/metadata", a.handleBackfillMetadata)
		r.With(a.requireAdmin).Post("/stacks", a.handleDetectStacks)
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

// stacksResponse is the JSON body returned by the stack-detection endpoint.
type stacksResponse struct {
	// Created is the number of stacks formed by this call.
	Created int `json:"created"`
}

// handleDetectStacks groups the currently unstacked photos into stacks by the
// enabled rules and reports how many stacks were created. It answers 503 when the
// stacking feature is disabled.
func (a *API) handleDetectStacks(w http.ResponseWriter, r *http.Request) {
	if a.stacksDetector == nil {
		writeError(w, http.StatusServiceUnavailable, "stacking not available")
		return
	}
	created, err := a.stacksDetector.DetectStacks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "detecting stacks failed")
		return
	}
	writeJSON(w, http.StatusOK, stacksResponse{Created: created})
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

// handleBackfillPlaces enqueues `places` jobs for all geotagged photos missing a
// cached place and reports how many were scheduled. It answers 503 when no
// geocoding backend is wired (no mapy.com key configured).
func (a *API) handleBackfillPlaces(w http.ResponseWriter, r *http.Request) {
	if a.placesBackfiller == nil {
		writeError(w, http.StatusServiceUnavailable, "place geocoding not available")
		return
	}
	enqueued, err := a.placesBackfiller.BackfillPlaces(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling places failed")
		return
	}
	writeJSON(w, http.StatusOK, backfillResponse{Enqueued: enqueued})
}

// handleBackfillThumbnails enqueues thumbnail jobs for all photos missing a
// generated thumbnail and reports how many were scheduled. With ?all=true it
// schedules every non-archived photo (a forced full re-run). It answers 503 when
// no thumbnail backfiller is wired.
func (a *API) handleBackfillThumbnails(w http.ResponseWriter, r *http.Request) {
	if a.thumbBackfiller == nil {
		writeError(w, http.StatusServiceUnavailable, "thumbnail backfill not available")
		return
	}
	enqueued, err := a.thumbBackfiller.BackfillThumbnails(r.Context(), queryFlag(r, "all"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling thumbnails failed")
		return
	}
	writeJSON(w, http.StatusOK, backfillResponse{Enqueued: enqueued})
}

// handleBackfillMetadata enqueues `metadata` jobs for all photos whose original
// has never been read out into the IPTC/XMP and file-technical columns, and reports
// how many were scheduled. With ?all=true it schedules every non-archived photo (a
// forced full re-read, which is how the library picks up fields a newer extractor
// learned to read). It answers 503 when no metadata backfiller is wired.
func (a *API) handleBackfillMetadata(w http.ResponseWriter, r *http.Request) {
	if a.metaBackfiller == nil {
		writeError(w, http.StatusServiceUnavailable, "metadata backfill not available")
		return
	}
	enqueued, err := a.metaBackfiller.BackfillMetadata(r.Context(), queryFlag(r, "all"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling metadata failed")
		return
	}
	writeJSON(w, http.StatusOK, backfillResponse{Enqueued: enqueued})
}

// queryFlag reports whether the request's query parameter name is set to a truthy
// value ("true", "1", "yes", "on"; case-insensitive). A malformed or absent value
// reads as false, so the flag is opt-in.
func queryFlag(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
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
