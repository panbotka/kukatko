package photoapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/storyboardjob"
)

// storyboardCacheControl caches a sprite for a long time: its bytes are a pure
// function of the clip's content hash and are never rewritten in place. It stays
// private to the authenticated caller, like every other media response here.
const storyboardCacheControl = "private, max-age=31536000, immutable"

// StoryboardService answers whether a video's scrub-preview sprite exists,
// schedules its lazy generation, and serves its bytes. It is satisfied by
// *storyboardjob.Service.
type StoryboardService interface {
	// Status reports ready/pending/unavailable for the photo's storyboard and
	// schedules the generation when it is merely not there yet.
	Status(ctx context.Context, photoUID string) (storyboardjob.Status, error)
	// Open returns a reader over the generated sprite plus its layout, or
	// storyboard.ErrNotGenerated when it has not been rendered.
	Open(ctx context.Context, photoUID string) (io.ReadCloser, storyboard.Spec, error)
	// FileHash returns the content hash the sprite is keyed by, for the ETag.
	FileHash(ctx context.Context, photoUID string) (string, error)
}

// storyboardResponse is the JSON body of the storyboard status endpoint. Only a
// ready storyboard carries a layout, so every geometry field is omitted while the
// sprite is pending or will never exist — a client that reads `status` first can
// never accidentally place a preview against a zero grid.
type storyboardResponse struct {
	// Status is "ready", "pending" or "unavailable".
	Status string `json:"status"`
	// Columns, Rows and Count describe the sprite's frame grid (row-major).
	Columns int `json:"columns,omitempty"`
	Rows    int `json:"rows,omitempty"`
	Count   int `json:"count,omitempty"`
	// TileWidth and TileHeight are one frame's pixel size inside the sprite.
	TileWidth  int `json:"tile_width,omitempty"`
	TileHeight int `json:"tile_height,omitempty"`
	// IntervalMs is the playback time one tile covers.
	IntervalMs int `json:"interval_ms,omitempty"`
}

// handleStoryboard reports whether the photo's scrub-preview sprite is ready and,
// when it is not, schedules its generation in the background queue. It always
// answers 200 for a photo that exists: "no preview" is a normal state of the
// player, not an error, and the three states are carried in the body.
//
// This is a GET that may schedule work, deliberately. The alternative — a POST the
// client fires before every playback — would either be denied to viewers (who are
// exactly the people watching) or duplicate the read. The scheduling is idempotent
// (the queue dedups per photo) and bounded (one clip, at most one active job), so
// repeating the GET costs nothing. Photos with no storyboard answer "unavailable"
// and the player stops asking.
func (a *API) handleStoryboard(w http.ResponseWriter, r *http.Request) {
	if a.storyboards == nil {
		writeJSON(w, http.StatusOK, storyboardResponse{Status: string(storyboardjob.StateUnavailable)})
		return
	}
	uid := chi.URLParam(r, "uid")
	status, err := a.storyboards.Status(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, "resolving storyboard failed")
		return
	}
	writeJSON(w, http.StatusOK, storyboardView(status))
}

// storyboardView projects a service status onto the wire shape, attaching the
// layout only for a ready sprite.
func storyboardView(status storyboardjob.Status) storyboardResponse {
	view := storyboardResponse{Status: string(status.State)}
	if status.State != storyboardjob.StateReady {
		return view
	}
	view.Columns = status.Spec.Columns
	view.Rows = status.Spec.Rows
	view.Count = status.Spec.Count
	view.TileWidth = status.Spec.TileWidth
	view.TileHeight = status.Spec.TileHeight
	view.IntervalMs = status.Spec.IntervalMs
	return view
}

// handleStoryboardSprite streams the generated sprite JPEG. A photo whose sprite
// has not been rendered yet — or that will never have one — is answered with 404,
// which the player treats as "no preview" and nothing else. The bytes are streamed
// with an ETag so a repeat fetch is a 304.
func (a *API) handleStoryboardSprite(w http.ResponseWriter, r *http.Request) {
	if a.storyboards == nil {
		writeError(w, http.StatusNotFound, "no storyboard for this photo")
		return
	}
	uid := chi.URLParam(r, "uid")
	reader, _, err := a.storyboards.Open(r.Context(), uid)
	if err != nil {
		writeStoryboardError(w, err)
		return
	}
	defer func() { _ = reader.Close() }()

	etag := storyboardETag(r.Context(), a.storyboards, uid)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", storyboardCacheControl)
	streamMedia(w, r, reader, etag, 0)
}

// storyboardETag builds the sprite's entity tag from the photo's content hash,
// falling back to the uid when the hash cannot be read. The sprite is derived from
// exactly that content, so the hash is the strongest validator available without
// digesting the sprite itself.
func storyboardETag(ctx context.Context, svc StoryboardService, uid string) string {
	hash, err := svc.FileHash(ctx, uid)
	if err != nil || hash == "" {
		return strconv.Quote(uid + "-sb")
	}
	return strconv.Quote(hash + "-sb")
}

// writeStoryboardError maps a sprite read failure to an HTTP response: 404 for a
// missing photo and for every "there is no sprite" condition (not generated yet,
// not a scrubbable video, no known duration), otherwise 500.
func writeStoryboardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, photos.ErrPhotoNotFound),
		errors.Is(err, storyboard.ErrNotGenerated),
		errors.Is(err, storyboard.ErrNoDuration),
		errors.Is(err, storyboardjob.ErrNotAVideo):
		writeError(w, http.StatusNotFound, "no storyboard for this photo")
	default:
		writeError(w, http.StatusInternalServerError, "reading storyboard failed")
	}
}
