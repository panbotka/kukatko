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
	// Similar backs the similar-photos endpoint with embedding search. When nil
	// the endpoint degrades to an empty result instead of failing.
	Similar SimilarSearcher
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

// writePage writes a paginated page of photos as a listResponse, computing the
// effective limit and the next-page offset (nil on the last page) used by an
// infinite-scroll client. It is shared by the list and search endpoints, whose
// page shape is identical.
func writePage(w http.ResponseWriter, params photos.ListParams, list []photos.Photo, total int) {
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
	writeJSON(w, http.StatusOK, resp)
}

// handleSearch runs a Czech-aware, diacritics-insensitive full-text search over
// the photo catalogue, ranked by relevance. The `q` query parameter carries the
// search text (required; empty or whitespace-only yields 400). Every list filter
// (date range, GPS, private, camera, …) and the limit/offset pagination apply,
// so a search can be scoped exactly like a browse; the `sort`/`order` params are
// ignored because results are always ranked. The response mirrors the list
// endpoint (photos, total, limit, offset, next_offset) for infinite scroll.
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
	// q is the full-text query here, not the list's substring filter.
	params.FullText = query
	params.Search = ""

	list, err := a.store.Search(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching photos failed")
		return
	}
	total, err := a.store.Count(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting search results failed")
		return
	}
	writePage(w, params, list, total)
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
