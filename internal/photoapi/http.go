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
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// API exposes the photo catalogue over HTTP. Route guards are supplied by the
// caller (the auth subsystem) so this package depends on auth for the caller's
// identity, not its wiring.
type API struct {
	store   *photos.Store
	storage storage.Storage
	// media mints the client-facing thumb/download addresses stamped onto every
	// photo payload, and tells the media routes whether to redirect or stream.
	media           *mediaurl.Builder
	thumbnailer     *thumb.Thumbnailer
	regenerator     ThumbnailRegenerator
	audit           AuditRecorder
	similar         SimilarSearcher
	embedder        TextEmbedder
	faces           FaceService
	favorites       FavoriteStore
	ratings         RatingStore
	organizer       PhotoOrganizer
	users           UserResolver
	purger          Purger
	retentionDays   int
	videoTranscode  bool
	requireAuth     func(http.Handler) http.Handler
	requireWrite    func(http.Handler) http.Handler
	requireDownload func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every field is required.
type Config struct {
	// Store is the photo repository backing reads and metadata updates.
	Store *photos.Store
	// Storage serves original files for download and decides where clients fetch
	// media: a backend that publishes signed URLs (R2) turns the media routes into
	// redirects and stamps those URLs onto photo payloads, while the filesystem
	// backend keeps the application streaming the bytes itself.
	Storage storage.Storage
	// Thumbnailer serves (and generates on miss) cached thumbnails.
	Thumbnailer *thumb.Thumbnailer
	// Regenerator force-rebuilds a photo's thumbnails and perceptual hashes on
	// demand for the regenerate-thumbnail service action. When nil that endpoint
	// answers 503.
	Regenerator ThumbnailRegenerator
	// Audit records the regenerate-thumbnail action in the durable audit trail.
	// When nil the action is not audited (the regeneration still runs).
	Audit AuditRecorder
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
	// Favorites backs the per-user favorite endpoints, the is_favorite annotation
	// on list/detail responses and the favorite=true filter. When nil those
	// endpoints answer 503 and photos report is_favorite false.
	Favorites FavoriteStore
	// Ratings backs the per-user rating/flag endpoints, the rating/flag annotation
	// on list/detail responses and the min_rating/flag filters and rating sort.
	// When nil those endpoints answer 503 and photos report rating 0 / flag "none".
	Ratings RatingStore
	// Organizer backs the album/label membership chips on the detail response.
	// When nil the detail response omits the memberships.
	Organizer PhotoOrganizer
	// Users resolves a photo's uploader UID to a human-readable name for the
	// detail response's uploader object. When nil the detail response omits the
	// uploader (the client shows a neutral fallback).
	Users UserResolver
	// Purger backs the permanent-delete endpoints (purge one, empty trash). When
	// nil those endpoints answer 503.
	Purger Purger
	// RetentionDays is the trash retention window reported by the trash-info
	// endpoint so the UI can show the auto-purge countdown.
	RetentionDays int
	// VideoTranscode enables on-the-fly transcoding of non-web-friendly video
	// codecs to H.264/MP4 on the video streaming endpoint. When false (the
	// default) such videos are streamed as-is and the client offers a download.
	VideoTranscode bool
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
		media:           mediaurl.NewBuilder(cfg.Storage),
		thumbnailer:     cfg.Thumbnailer,
		regenerator:     cfg.Regenerator,
		audit:           cfg.Audit,
		similar:         cfg.Similar,
		embedder:        cfg.Embedder,
		faces:           cfg.Faces,
		favorites:       cfg.Favorites,
		ratings:         cfg.Ratings,
		organizer:       cfg.Organizer,
		users:           cfg.Users,
		purger:          cfg.Purger,
		retentionDays:   cfg.RetentionDays,
		videoTranscode:  cfg.VideoTranscode,
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
//	GET    /photos/timeline           RequireAuth      month date buckets (histogram)
//	GET    /photos/years              RequireAuth      capture years with counts (facet)
//	GET    /photos/{uid}              RequireAuth      full detail
//	GET    /photos/{uid}/similar      RequireAuth      visually similar photos
//	GET    /photos/{uid}/faces        RequireAuth      faces + assignment + suggestions
//	POST   /photos/{uid}/faces/assign RequireWrite     create/assign/unassign marker
//	PATCH  /photos/{uid}              RequireWrite     update metadata
//	GET    /photos/{uid}/edit         RequireAuth      stored non-destructive edit
//	PUT    /photos/{uid}/edit         RequireWrite     save non-destructive edit
//	POST   /photos/{uid}/archive      RequireWrite     soft-delete
//	POST   /photos/{uid}/unarchive    RequireWrite     restore
//	POST   /photos/{uid}/regenerate-thumbnail RequireWrite  rebuild thumbnail + pHash
//	GET    /photos/{uid}/thumb/{size} RequireDownload  cached thumbnail (or 302)
//	GET    /photos/{uid}/video        RequireDownload  video stream (range/206, or 302)
//	GET    /photos/{uid}/download     RequireDownload  original file (or 302)
//	POST   /photos/download-zip       RequireDownload  ZIP of originals (selection/album)
//	PUT    /photos/{uid}/favorite     RequireAuth      favorite (current user)
//	DELETE /photos/{uid}/favorite     RequireAuth      unfavorite (current user)
//	PUT    /photos/{uid}/rating       RequireAuth      set rating/flag (current user)
//	DELETE /photos/{uid}/rating       RequireAuth      clear rating/flag (current user)
//	POST   /photos/{uid}/purge        RequireWrite     permanent delete (confirm)
//	GET    /favorites                 RequireAuth      current user's favorites
//	GET    /trash/info                RequireAuth      retention window (countdown)
//	POST   /trash/empty               RequireWrite     permanent delete all (confirm)
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/search", a.handleSearch)
	r.With(a.requireAuth).Get("/favorites", a.handleFavorites)
	r.With(a.requireAuth).Get("/trash/info", a.handleTrashInfo)
	r.With(a.requireWrite).Post("/trash/empty", a.handleEmptyTrash)
	r.Route("/photos", func(r chi.Router) {
		r.With(a.requireDownload).Post("/download-zip", a.handleDownloadZip)
		r.With(a.requireAuth).Get("/", a.handleList)
		r.With(a.requireAuth).Get("/timeline", a.handleTimeline)
		r.With(a.requireAuth).Get("/years", a.handleYears)
		r.With(a.requireAuth).Get("/{uid}", a.handleDetail)
		r.With(a.requireAuth).Get("/{uid}/similar", a.handleSimilar)
		r.With(a.requireAuth).Get("/{uid}/faces", a.handleFaces)
		r.With(a.requireAuth).Put("/{uid}/favorite", a.handleAddFavorite)
		r.With(a.requireAuth).Delete("/{uid}/favorite", a.handleRemoveFavorite)
		r.With(a.requireAuth).Put("/{uid}/rating", a.handleSetRating)
		r.With(a.requireAuth).Delete("/{uid}/rating", a.handleClearRating)
		r.With(a.requireWrite).Post("/{uid}/faces/assign", a.handleFaceAssign)
		r.With(a.requireWrite).Patch("/{uid}", a.handleUpdate)
		r.With(a.requireAuth).Get("/{uid}/edit", a.handleGetEdit)
		r.With(a.requireWrite).Put("/{uid}/edit", a.handlePutEdit)
		r.With(a.requireWrite).Post("/{uid}/archive", a.handleArchive)
		r.With(a.requireWrite).Post("/{uid}/unarchive", a.handleUnarchive)
		r.With(a.requireWrite).Post("/{uid}/regenerate-thumbnail", a.handleRegenerateThumbnail)
		r.With(a.requireWrite).Post("/{uid}/purge", a.handlePurge)
		r.With(a.requireDownload).Get("/{uid}/thumb/{size}", a.handleThumb)
		r.With(a.requireDownload).Get("/{uid}/video", a.handleVideo)
		r.With(a.requireDownload).Get("/{uid}/download", a.handleDownload)
	})
}

// listResponse is the JSON body returned by the list endpoint. NextOffset is the
// offset to request for the following page, or null when the current page is the
// last one — letting an infinite-scroll client page until it is absent.
type listResponse struct {
	Photos     []photoView `json:"photos"`
	Total      int         `json:"total"`
	Limit      int         `json:"limit"`
	Offset     int         `json:"offset"`
	NextOffset *int        `json:"next_offset"`
	// Mode is the effective search mode (fulltext/semantic/hybrid). It is only
	// set by the search endpoint and omitted from a plain list response.
	Mode string `json:"mode,omitempty"`
	// Degraded is true when a semantic or hybrid search fell back to full-text
	// because the embeddings sidecar was unavailable, so the UI can tell the user
	// that semantic ranking was skipped. Omitted when false.
	Degraded bool `json:"degraded,omitempty"`
}

// handleList parses the query filters, returns the matching page of photos (each
// annotated with the current user's is_favorite flag) plus the total count and the
// next-page offset for infinite scroll. The favorite=true filter scopes the list to
// the caller's own favorites. Invalid filter, sort or pagination values are answered
// with 400.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	params, err := parseListParams(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	favorite, err := favoriteRequested(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if favorite {
		params.FavoriteOf = user.UID
	}
	a.writeFavoritePage(w, r, user.UID, params)
}

// pageResponse builds the paginated listResponse for a page of photo views,
// computing the effective limit and the next-page offset (nil on the last page)
// used by an infinite-scroll client. The search endpoint reuses it and then sets
// the Mode and Degraded fields, which a plain list leaves empty.
func pageResponse(params photos.ListParams, list []photoView, total int) listResponse {
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

// handleSearch searches the photo catalogue in one of three modes selected by
// the `mode` query parameter — `fulltext`, `semantic` or `hybrid` (the default).
// The `q` parameter carries the search text (required; empty or whitespace-only
// yields 400). Full-text matching is Czech-aware and diacritics-insensitive;
// semantic matching embeds the query via the sidecar and ranks by CLIP vector
// similarity; hybrid fuses the two with Reciprocal Rank Fusion. Every list filter
// (date range, GPS, camera, …) and the limit/offset pagination apply in
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
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// q is the full-text query here, not the list's substring filter.
	params.FullText = query
	params.Search = ""
	// Scope the per-user rating filters and the rating sort to the caller.
	params.RatedBy = &user.UID

	result, err := a.runSearch(r.Context(), mode, query, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching photos failed")
		return
	}
	views, err := a.annotate(r.Context(), user.UID, result.photos)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "annotating photos failed")
		return
	}
	resp := pageResponse(params, views, result.total)
	resp.Mode = string(mode)
	resp.Degraded = result.degraded
	writeJSON(w, http.StatusOK, resp)
}

// defaultPageLimit mirrors the store's default page size for reporting the
// effective limit back to the client when the request did not set one.
const defaultPageLimit = 100

// UserResolver resolves a user UID to the user record so the detail endpoint can
// present the uploader's human-readable name instead of a raw UID. It is a narrow
// interface so photoapi depends on the behaviour, not the auth store's wiring;
// auth.Store satisfies it and a test fake can stand in. The lookup is limited to
// the single-photo detail endpoint so list/search responses never pay a per-item
// user query (no N+1). When nil the detail response omits the uploader object.
type UserResolver interface {
	// GetUserByUID returns the user with the given UID, or auth.ErrUserNotFound.
	GetUserByUID(ctx context.Context, uid string) (auth.User, error)
}

// uploaderRef is the compact uploader reference embedded in a photo detail
// response: the uploading user's UID plus a resolved human-readable Name (the
// display name, falling back to the username when the display name is empty).
type uploaderRef struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// photoDetail is the JSON body returned by the detail endpoint: the photo (with
// its metadata, EXIF, GPS and the current user's is_favorite flag), the list of
// its stored files, its album/label memberships (empty when no organizer is
// wired), and the resolved uploader (omitted for photos with no uploader — e.g.
// imported items — or when no resolver is wired) so the detail view can show and
// edit them inline.
type photoDetail struct {
	photoView
	Files    []photos.PhotoFile `json:"files"`
	Albums   []albumRef         `json:"albums"`
	Labels   []labelRef         `json:"labels"`
	Uploader *uploaderRef       `json:"uploader,omitempty"`
}

// handleDetail returns a photo's full detail, including its file list and the
// current user's is_favorite flag. A missing photo is answered with 404.
func (a *API) handleDetail(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}
	a.writeDetail(w, r, user.UID, photo)
}

// writeDetail assembles and writes the full photoDetail body for photo: its stored
// files, the caller's per-user annotations (is_favorite, rating, flag) and media
// URLs, its album/label memberships and its resolved uploader.
//
// Every endpoint answering with a single photo the detail view then holds must go
// through here, not write the bare photos.Photo: the client replaces the detail it
// has with the response, so a body missing albums/labels/files would strip them
// from the page. The metadata PATCH shares it for exactly that reason.
func (a *API) writeDetail(w http.ResponseWriter, r *http.Request, userUID string, photo photos.Photo) {
	files, err := a.store.ListFiles(r.Context(), photo.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fetching photo files failed")
		return
	}
	views, err := a.annotate(r.Context(), userUID, []photos.Photo{photo})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "annotating photo failed")
		return
	}
	albums, labels, err := a.photoMemberships(r.Context(), photo.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fetching photo organization failed")
		return
	}
	writeJSON(w, http.StatusOK, photoDetail{
		photoView: views[0], Files: files, Albums: albums, Labels: labels,
		Uploader: a.resolveUploader(r.Context(), photo.UploadedBy),
	})
}

// resolveUploader resolves a photo's uploaded_by UID to a compact uploader
// reference carrying a human-readable name (the display name, or the username
// when the display name is empty). It returns nil — so the detail response omits
// the uploader and the client shows its neutral fallback — when the photo has no
// uploader (uploadedBy nil/empty, e.g. imported items), when no resolver is
// wired, or when the user can no longer be resolved (a deleted account or a
// lookup failure); it never fails the detail request over the uploader.
func (a *API) resolveUploader(ctx context.Context, uploadedBy *string) *uploaderRef {
	if a.users == nil || uploadedBy == nil || *uploadedBy == "" {
		return nil
	}
	user, err := a.users.GetUserByUID(ctx, *uploadedBy)
	if err != nil {
		return nil
	}
	name := user.DisplayName
	if name == "" {
		name = user.Username
	}
	return &uploaderRef{UID: user.UID, Name: name}
}

// photoMemberships returns the photo's album and label chips for the detail
// response, or empty (non-nil) slices when no organizer is wired so the JSON
// always carries arrays rather than null.
func (a *API) photoMemberships(ctx context.Context, uid string) ([]albumRef, []labelRef, error) {
	albums := make([]albumRef, 0)
	labels := make([]labelRef, 0)
	if a.organizer == nil {
		return albums, labels, nil
	}
	albumRows, err := a.organizer.AlbumsForPhoto(ctx, uid)
	if err != nil {
		return nil, nil, fmt.Errorf("photoapi: albums for photo: %w", err)
	}
	for _, album := range albumRows {
		albums = append(albums, albumRef{UID: album.UID, Title: album.Title})
	}
	labelRows, err := a.organizer.LabelsForPhoto(ctx, uid)
	if err != nil {
		return nil, nil, fmt.Errorf("photoapi: labels for photo: %w", err)
	}
	for _, label := range labelRows {
		labels = append(labels, labelRef{UID: label.UID, Name: label.Name})
	}
	return albums, labels, nil
}

// handleArchive soft-deletes the photo (sets archived_at) and returns the
// refreshed photo, recording an audit entry in the same transaction. A missing
// photo is answered with 404.
func (a *API) handleArchive(w http.ResponseWriter, r *http.Request) {
	a.runArchive(w, r, audit.ActionPhotoArchive, a.store.ArchiveAudited, "archiving photo failed")
}

// handleUnarchive restores an archived photo (clears archived_at) and returns
// the refreshed photo, recording an audit entry in the same transaction. A
// missing photo is answered with 404.
func (a *API) handleUnarchive(w http.ResponseWriter, r *http.Request) {
	a.runArchive(w, r, audit.ActionPhotoUnarchive, a.store.UnarchiveAudited, "unarchiving photo failed")
}

// runArchive applies the audited archive-state transition op to the photo named
// in the request path and writes the refreshed photo, recording action in the
// audit log within the mutation transaction. It maps a missing photo to 404 and
// any other failure to 500 with failMsg.
func (a *API) runArchive(
	w http.ResponseWriter, r *http.Request, action string,
	op func(ctx context.Context, uid string, entry audit.Entry) (photos.Photo, error),
	failMsg string,
) {
	uid := chi.URLParam(r, "uid")
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	entry := audit.FromRequest(r, user.UID).Entry(action, "photos", uid, nil)
	photo, err := op(r.Context(), uid, entry)
	if err != nil {
		writePhotoError(w, err, failMsg)
		return
	}
	a.media.DecorateOne(&photo)
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
