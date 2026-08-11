// Package processapi exposes maintainer-only HTTP endpoints that kick off bulk
// catalogue processing. Its first action is the embedding backfill, which
// enqueues an image_embed job for every photo that still lacks an embedding —
// the recovery path for photos uploaded while the embeddings box was offline or
// imported before embeddings existed. It depends only on a Backfiller behaviour
// and a maintainer guard, both injected, so it stays decoupled from the job and
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
	// CountBackfillThumbnails returns how many photos BackfillThumbnails would
	// schedule for the same value of all, scheduling nothing. It backs ?dry_run.
	CountBackfillThumbnails(ctx context.Context, all bool) (int, error)
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

// OCRBackfiller enqueues an `ocr` job for every photo the text recogniser has
// never seen. It is satisfied by ocrjob.Service. When all is true it schedules
// every non-archived still instead (a forced full re-run, which is how a library
// picks up a better recognition model). OCR jobs call the embeddings sidecar on
// the GPU box, so the backfill schedules work that drains only while the box is
// up — the enqueue itself never blocks on it. A nil OCRBackfiller disables the
// /process/ocr endpoint (it answers 503), which is what the embedding.ocr.enabled
// switch turns off.
type OCRBackfiller interface {
	// BackfillOCR enqueues an `ocr` job for every photo never recognised (or, when
	// all is true, for every non-archived still) and returns how many were
	// scheduled.
	BackfillOCR(ctx context.Context, all bool) (int, error)
}

// SidecarBackfiller enqueues a sidecar job for every photo whose metadata sidecar
// is missing or stale. It is satisfied by sidecarjob.Service. When all is true it
// schedules every non-archived photo instead (a forced full re-run), which is how
// curation that changed without touching the photo row — an album membership, a
// label — is recovered. Sidecar jobs run locally, so the backfill works
// regardless of the embeddings box being offline. A nil SidecarBackfiller
// disables the /process/sidecars endpoint (it answers 503), which is what the
// sidecar.enabled config switch turns off.
type SidecarBackfiller interface {
	// BackfillSidecars enqueues a sidecar job for every photo whose sidecar is
	// missing or stale (or, when all is true, for every non-archived photo) and
	// returns how many were scheduled.
	BackfillSidecars(ctx context.Context, all bool) (int, error)
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

// LocationEstimator infers a location for photos that have none from photos
// taken near them in time. It is satisfied by geoestimate.Service and runs
// synchronously (it is a query per candidate, and it enqueues the metered
// geocoding rather than doing it). A nil LocationEstimator — the feature
// disabled in config — makes the /process/locations endpoint answer 503.
type LocationEstimator interface {
	// BackfillLocations estimates a location for every eligible photo and returns
	// how many it filled in. It is idempotent: an estimated photo stops being a
	// candidate, and one whose estimate the user cleared is never estimated again.
	BackfillLocations(ctx context.Context) (int, error)
}

// API exposes the processing endpoints over HTTP. The maintainer guard is
// supplied by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	backfiller        Backfiller
	faceBackfiller    FaceBackfiller
	reclusterer       Reclusterer
	placesBackfiller  PlacesBackfiller
	thumbBackfiller   ThumbnailBackfiller
	metaBackfiller    MetadataBackfiller
	sidecarBackfill   SidecarBackfiller
	ocrBackfiller     OCRBackfiller
	stacksDetector    StacksDetector
	locationEstim     LocationEstimator
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Backfiller, FaceBackfiller and
// RequireMaintainer are required; Reclusterer and PlacesBackfiller are optional (a nil
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
	// SidecarBackfiller runs the metadata-sidecar export backfill.
	SidecarBackfiller SidecarBackfiller
	// OCRBackfiller runs the text-recognition backfill.
	OCRBackfiller OCRBackfiller
	// StacksDetector runs the automatic stack-detection pass.
	StacksDetector StacksDetector
	// LocationEstimator runs the missing-location estimation pass.
	LocationEstimator LocationEstimator
	// RequireMaintainer guards every endpoint for maintainers only.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		backfiller:        cfg.Backfiller,
		faceBackfiller:    cfg.FaceBackfiller,
		reclusterer:       cfg.Reclusterer,
		placesBackfiller:  cfg.PlacesBackfiller,
		thumbBackfiller:   cfg.ThumbnailBackfiller,
		metaBackfiller:    cfg.MetadataBackfiller,
		sidecarBackfill:   cfg.SidecarBackfiller,
		ocrBackfiller:     cfg.OCRBackfiller,
		stacksDetector:    cfg.StacksDetector,
		locationEstim:     cfg.LocationEstimator,
		requireMaintainer: cfg.RequireMaintainer,
	}
}

// RegisterRoutes mounts the processing endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	POST /process/embeddings  RequireMaintainer  backfill missing image embeddings
//	POST /process/faces       RequireMaintainer  backfill missing face detections
//	POST /process/clusters    RequireMaintainer  rebuild face clusters from unassigned faces
//	POST /process/places      RequireMaintainer  backfill missing reverse-geocoded places
//	POST /process/thumbnails  RequireMaintainer  backfill missing thumbnails (?all=true forces a full
//	                                             re-run, ?dry_run=true only counts)
//	POST /process/metadata    RequireMaintainer  backfill unread file metadata (?all=true forces a full re-read)
//	POST /process/sidecars    RequireMaintainer  backfill missing metadata sidecars (?all=true forces a full re-run)
//	POST /process/ocr         RequireMaintainer  backfill un-recognised photo text (?all=true forces a full re-run)
//	POST /process/stacks      RequireMaintainer  detect and form stacks over the library
//	POST /process/locations   RequireMaintainer  estimate missing locations from same-day photos
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/process", func(r chi.Router) {
		r.With(a.requireMaintainer).Post("/embeddings", a.handleBackfillEmbeddings)
		r.With(a.requireMaintainer).Post("/faces", a.handleBackfillFaces)
		r.With(a.requireMaintainer).Post("/clusters", a.handleRecluster)
		r.With(a.requireMaintainer).Post("/places", a.handleBackfillPlaces)
		r.With(a.requireMaintainer).Post("/thumbnails", a.handleBackfillThumbnails)
		r.With(a.requireMaintainer).Post("/metadata", a.handleBackfillMetadata)
		r.With(a.requireMaintainer).Post("/sidecars", a.handleBackfillSidecars)
		r.With(a.requireMaintainer).Post("/ocr", a.handleBackfillOCR)
		r.With(a.requireMaintainer).Post("/stacks", a.handleDetectStacks)
		r.With(a.requireMaintainer).Post("/locations", a.handleEstimateLocations)
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

// handleBackfillSidecars enqueues sidecar jobs for every photo whose metadata
// sidecar is missing or stale and reports how many were scheduled. With ?all=true
// it schedules every non-archived photo (a forced full re-run). It answers 503
// when the sidecar export is switched off.
func (a *API) handleBackfillSidecars(w http.ResponseWriter, r *http.Request) {
	if a.sidecarBackfill == nil {
		writeError(w, http.StatusServiceUnavailable, "sidecar backfill not available")
		return
	}
	enqueued, err := a.sidecarBackfill.BackfillSidecars(r.Context(), queryFlag(r, "all"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling sidecars failed")
		return
	}
	writeJSON(w, http.StatusOK, backfillResponse{Enqueued: enqueued})
}

// handleBackfillOCR enqueues `ocr` jobs for every photo the text recogniser has
// never seen and reports how many were scheduled. With ?all=true it schedules
// every non-archived still (a forced full re-run with the current model). It
// answers 503 when OCR is switched off.
func (a *API) handleBackfillOCR(w http.ResponseWriter, r *http.Request) {
	if a.ocrBackfiller == nil {
		writeError(w, http.StatusServiceUnavailable, "OCR not available")
		return
	}
	enqueued, err := a.ocrBackfiller.BackfillOCR(r.Context(), queryFlag(r, "all"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling OCR failed")
		return
	}
	writeJSON(w, http.StatusOK, backfillResponse{Enqueued: enqueued})
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

// locationsResponse is the JSON body returned by the location-estimate endpoint.
type locationsResponse struct {
	// Estimated is the number of photos given a location by this call. Photos whose
	// neighbours were missing or disagreed are not counted and not errors: refusing
	// is the normal outcome.
	Estimated int `json:"estimated"`
}

// handleEstimateLocations infers a location for the photos that have none from
// photos taken near them in time, and reports how many it filled in. It answers
// 503 when location estimation is disabled in config.
func (a *API) handleEstimateLocations(w http.ResponseWriter, r *http.Request) {
	if a.locationEstim == nil {
		writeError(w, http.StatusServiceUnavailable, "location estimation not available")
		return
	}
	estimated, err := a.locationEstim.BackfillLocations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "estimating locations failed")
		return
	}
	writeJSON(w, http.StatusOK, locationsResponse{Estimated: estimated})
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

// thumbnailBackfillResponse is the JSON body returned by the thumbnail-backfill
// endpoint. It carries the candidate count alongside the enqueued one so the size
// of the run is visible whether or not it was actually started.
type thumbnailBackfillResponse struct {
	// Enqueued is the number of thumbnail jobs scheduled by this call — always 0
	// for a dry run.
	Enqueued int `json:"enqueued"`
	// Pending is how many photos match the backfill's predicate, i.e. how many jobs
	// a real run would schedule.
	Pending int `json:"pending"`
	// DryRun reports whether this call only counted.
	DryRun bool `json:"dry_run"`
}

// handleBackfillThumbnails enqueues thumbnail jobs for all photos missing a
// generated thumbnail and reports how many were scheduled. With ?all=true it
// schedules every non-archived photo (a forced full re-run). With ?dry_run=true it
// schedules nothing and only reports how many photos would be covered, so the cost
// of a run can be seen before it is started — a thumbnail job re-reads an original,
// and on a library that has never been hashed the narrow predicate matches every
// photo in it. It answers 503 when no thumbnail backfiller is wired.
func (a *API) handleBackfillThumbnails(w http.ResponseWriter, r *http.Request) {
	if a.thumbBackfiller == nil {
		writeError(w, http.StatusServiceUnavailable, "thumbnail backfill not available")
		return
	}
	all := queryFlag(r, "all")
	pending, err := a.thumbBackfiller.CountBackfillThumbnails(r.Context(), all)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting thumbnail backfill failed")
		return
	}
	if queryFlag(r, "dry_run") {
		writeJSON(w, http.StatusOK, thumbnailBackfillResponse{Pending: pending, DryRun: true})
		return
	}
	enqueued, err := a.thumbBackfiller.BackfillThumbnails(r.Context(), all)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backfilling thumbnails failed")
		return
	}
	writeJSON(w, http.StatusOK, thumbnailBackfillResponse{Enqueued: enqueued, Pending: pending})
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
