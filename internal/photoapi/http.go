// Package photoapi exposes the read and curation HTTP API for the photo
// catalogue: browsing the library with filters/sorting/pagination, fetching a
// photo's full detail, updating its metadata, archiving (soft-deleting) it, and
// streaming its thumbnails and original. It depends on the photos repository for
// data, on storage and thumb for media, and on the auth subsystem only for the
// route guards, which are injected as middleware.
package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// API exposes the photo catalogue over HTTP. Route guards are supplied by the
// caller (the auth subsystem) so this package depends on auth for the caller's
// identity, not its wiring.
type API struct {
	store           *photos.Store
	storage         storage.Storage
	thumbnailer     *thumb.Thumbnailer
	similar         SimilarSearcher
	embedder        TextEmbedder
	faces           FaceService
	requireAuth     func(http.Handler) http.Handler
	requireWrite    func(http.Handler) http.Handler
	requireDownload func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every field is required.
type Config struct {
	// Store is the photo repository backing reads and metadata updates.
	Store *photos.Store
	// Storage serves original files for download.
	Storage storage.Storage
	// Thumbnailer serves (and generates on miss) cached thumbnails.
	Thumbnailer *thumb.Thumbnailer
	// Similar backs the similar-photos endpoint and the vector half of semantic
	// and hybrid search. When nil those modes degrade to full-text search.
	Similar SimilarSearcher
	// Embedder embeds the query text into the CLIP vector space for semantic and
	// hybrid search. When nil, or when it reports the sidecar unavailable, those
	// modes degrade gracefully to full-text search with a degraded flag.
	Embedder TextEmbedder
	// Faces backs the per-photo faces endpoint and the face-assignment endpoint
	// (face↔marker matching, suggestions and the assignment state machine). When
	// nil those endpoints answer 503.
	Faces FaceService
	// RequireAuth guards read endpoints for any authenticated user.
	RequireAuth func(http.Handler) http.Handler
	// RequireWrite guards metadata and archive endpoints for editors and admins.
	RequireWrite func(http.Handler) http.Handler
	// RequireDownload guards media endpoints, accepting a session cookie or a
	// download token so cookie-less <img>/<video> tags work.
	RequireDownload func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store:           cfg.Store,
		storage:         cfg.Storage,
		thumbnailer:     cfg.Thumbnailer,
		similar:         cfg.Similar,
		embedder:        cfg.Embedder,
		faces:           cfg.Faces,
		requireAuth:     cfg.RequireAuth,
		requireWrite:    cfg.RequireWrite,
		requireDownload: cfg.RequireDownload,
	}
}

// RegisterRoutes mounts the photo endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET    /search                    RequireAuth      full-text search (ranked)
//	GET    /photos                    RequireAuth      list with filters/sort/page
//	GET    /photos/{uid}              RequireAuth      full detail
//	GET    /photos/{uid}/similar      RequireAuth      visually similar photos
//	GET    /photos/{uid}/faces        RequireAuth      faces + assignment + suggestions
//	POST   /photos/{uid}/faces/assign RequireWrite     create/assign/unassign marker
//	PATCH  /photos/{uid}              RequireWrite     update metadata
//	POST   /photos/{uid}/archive      RequireWrite     soft-delete
//	POST   /photos/{uid}/unarchive    RequireWrite     restore
//	GET    /photos/{uid}/thumb/{size} RequireDownload  cached thumbnail
//	GET    /photos/{uid}/download     RequireDownload  original file
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/search", a.handleSearch)
	r.Route("/photos", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleList)
		r.With(a.requireAuth).Get("/{uid}", a.handleDetail)
		r.With(a.requireAuth).Get("/{uid}/similar", a.handleSimilar)
		r.With(a.requireAuth).Get("/{uid}/faces", a.handleFaces)
		r.With(a.requireWrite).Post("/{uid}/faces/assign", a.handleFaceAssign)
		r.With(a.requireWrite).Patch("/{uid}", a.handleUpdate)
		r.With(a.requireWrite).Post("/{uid}/archive", a.handleArchive)
		r.With(a.requireWrite).Post("/{uid}/unarchive", a.handleUnarchive)
		r.With(a.requireDownload).Get("/{uid}/thumb/{size}", a.handleThumb)
		r.With(a.requireDownload).Get("/{uid}/download", a.handleDownload)
	})
}

// listResponse is the JSON body returned by the list endpoint. NextOffset is the
// offset to request for the following page, or null when the current page is the
// last one — letting an infinite-scroll client page until it is absent.
type listResponse struct {
	Photos     []photos.Photo `json:"photos"`
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Offset     int            `json:"offset"`
	NextOffset *int           `json:"next_offset"`
	// Mode is the effective search mode (fulltext/semantic/hybrid). It is only
	// set by the search endpoint and omitted from a plain list response.
	Mode string `json:"mode,omitempty"`
	// Degraded is true when a semantic or hybrid search fell back to full-text
	// because the embeddings sidecar was unavailable, so the UI can tell the user
	// that semantic ranking was skipped. Omitted when false.
	Degraded bool `json:"degraded,omitempty"`
}

// handleList parses the query filters, returns the matching page of photos plus
// the total count and the next-page offset for infinite scroll. Invalid filter,
// sort or pagination values are answered with 400.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	params, err := parseListParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := a.store.List(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing photos failed")
		return
	}
	total, err := a.store.Count(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting photos failed")
		return
	}
	writePage(w, params, list, total)
}

// pageResponse builds the paginated listResponse for a page of photos, computing
// the effective limit and the next-page offset (nil on the last page) used by an
// infinite-scroll client. The search endpoint reuses it and then sets the Mode
// and Degraded fields, which a plain list leaves empty.
func pageResponse(params photos.ListParams, list []photos.Photo, total int) listResponse {
	limit := params.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	}
	resp := listResponse{
		Photos: list,
		Total:  total,
		Limit:  limit,
		Offset: params.Offset,
	}
	if next := params.Offset + len(list); next < total && len(list) > 0 {
		resp.NextOffset = &next
	}
	return resp
}

// writePage writes a paginated page of photos as a listResponse. It is used by
// the list endpoint; the search endpoint builds the response via pageResponse so
// it can annotate it with the mode and degraded flag.
func writePage(w http.ResponseWriter, params photos.ListParams, list []photos.Photo, total int) {
	writeJSON(w, http.StatusOK, pageResponse(params, list, total))
}

// handleSearch searches the photo catalogue in one of three modes selected by
// the `mode` query parameter — `fulltext`, `semantic` or `hybrid` (the default).
// The `q` parameter carries the search text (required; empty or whitespace-only
// yields 400). Full-text matching is Czech-aware and diacritics-insensitive;
// semantic matching embeds the query via the sidecar and ranks by CLIP vector
// similarity; hybrid fuses the two with Reciprocal Rank Fusion. Every list filter
// (date range, GPS, private, camera, …) and the limit/offset pagination apply in
// all modes; the `sort`/`order` params are ignored because results are always
// ranked. When the sidecar is unavailable, semantic and hybrid fall back to
// full-text and the response sets `degraded: true`. The response otherwise
// mirrors the list endpoint (photos, total, limit, offset, next_offset) plus the
// effective `mode`.
func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	params, err := parseListParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	mode, err := parseSearchMode(r.URL.Query().Get("mode"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// q is the full-text query here, not the list's substring filter.
	params.FullText = query
	params.Search = ""

	result, err := a.runSearch(r.Context(), mode, query, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching photos failed")
		return
	}
	resp := pageResponse(params, result.photos, result.total)
	resp.Mode = string(mode)
	resp.Degraded = result.degraded
	writeJSON(w, http.StatusOK, resp)
}

// defaultPageLimit mirrors the store's default page size for reporting the
// effective limit back to the client when the request did not set one.
const defaultPageLimit = 100

// photoDetail is the JSON body returned by the detail endpoint: the photo (with
// its metadata, EXIF and GPS) plus the list of its stored files.
type photoDetail struct {
	photos.Photo
	Files []photos.PhotoFile `json:"files"`
}

// handleDetail returns a photo's full detail, including its file list. A missing
// photo is answered with 404.
func (a *API) handleDetail(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}
	files, err := a.store.ListFiles(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fetching photo files failed")
		return
	}
	writeJSON(w, http.StatusOK, photoDetail{Photo: photo, Files: files})
}

// handleArchive soft-deletes the photo (sets archived_at) and returns the
// refreshed photo. A missing photo is answered with 404.
func (a *API) handleArchive(w http.ResponseWriter, r *http.Request) {
	runArchive(w, r, a.store.Archive, "archiving photo failed")
}

// handleUnarchive restores an archived photo (clears archived_at) and returns
// the refreshed photo. A missing photo is answered with 404.
func (a *API) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	runArchive(w, r, a.store.Unarchive, "unarchiving photo failed")
}

// runArchive applies the archive-state transition op to the photo named in the
// request path and writes the refreshed photo, mapping a missing photo to 404
// and any other failure to 500 with failMsg.
func runArchive(
	w http.ResponseWriter, r *http.Request,
	op func(ctx context.Context, uid string) (photos.Photo, error),
	failMsg string,
) {
	uid := chi.URLParam(r, "uid")
	photo, err := op(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, failMsg)
		return
	}
	writeJSON(w, http.StatusOK, photo)
}

// writePhotoError maps a store error to an HTTP response: 404 for a missing
// photo, otherwise 500 with failMsg.
func writePhotoError(w http.ResponseWriter, err error, failMsg string) {
	if errors.Is(err, photos.ErrPhotoNotFound) {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}
	writeError(w, http.StatusInternalServerError, failMsg)
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
		log.Printf("photoapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
