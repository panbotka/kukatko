package photoapi

import (
	"context"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/photos"
)

// faceCropCacheControl is the caching policy for a face crop. Unlike the subject
// avatar — whose URL names a *subject*, and therefore a mapping a curator can
// change — this URL names a photo and an exact box, so the picture behind it is
// as fixed as a thumbnail's and gets the thumbnail's own policy: cache for a
// year, never revalidate. That is the whole point of the route on a page holding
// hundreds of faces, where the second visit should cost nothing at all. It is
// private because it is served only to authorized callers.
const faceCropCacheControl = "private, max-age=31536000, immutable"

// faceCropBoxParts is how many comma-separated values the box query parameter
// carries: x, y, w and h.
const faceCropBoxParts = 4

// FaceCropRenderer cuts one detected face out of a photo as a small square JPEG
// and keeps the result in the local derived-media cache. It is satisfied by
// *avatar.Renderer — the same renderer the subject avatar is cut by, which is
// deliberate: the geometry (pad, square, slide back inside the frame), the choice
// of source thumbnail and the cache layout exist once.
type FaceCropRenderer interface {
	// Open returns a reader over the rendered crop and its ETag. The caller owns
	// the reader and must close it.
	Open(ctx context.Context, photo photos.Photo, face *avatar.Box) (io.ReadCloser, string, error)
}

// handleFaceCrop streams one face of the photo named in the path as a small
// square JPEG, cut from the box given in ?box=x,y,w,h (normalised against the
// photo's display frame, the same space a marker's box lives in).
//
// It exists because showing a face used to mean downloading a photograph. A tile
// 96 px across was painted by fetching the whole `fit_1280` preview and letting
// CSS crop a window out of it — measured on one person's page, 290 previews for
// one section, over a megapixel each, to show a few thousand pixels of face. This
// route hands over exactly the crop instead.
//
// The rendition is cache-only derived media (see the avatar package): it is never
// uploaded to the object store, so unlike a thumbnail this route always streams
// the bytes rather than redirecting to a signed URL. At some 15 kB a crop that is
// a hundredth of the preview it replaces, that is a trade worth making.
//
// A box that is not four finite numbers, or that has no positive size, or that
// lies entirely outside the frame is a 400; an unknown photo a 404. A caller
// presenting the current ETag gets a 304.
func (a *API) handleFaceCrop(w http.ResponseWriter, r *http.Request) {
	if a.faceCrops == nil {
		writeError(w, http.StatusServiceUnavailable, "face crops not available")
		return
	}
	box, ok := parseFaceBox(r.URL.Query().Get("box"))
	if !ok {
		writeError(w, http.StatusBadRequest, "box must be four numbers x,y,w,h with a positive size")
		return
	}

	photo, err := a.store.GetByUID(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	reader, etag, err := a.faceCrops.Open(r.Context(), photo, &box)
	if err != nil {
		log.Printf("photoapi: rendering face crop of %s: %v", photo.UID, err)
		writeError(w, http.StatusInternalServerError, "face crop unavailable")
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", faceCropCacheControl)
	streamMedia(w, r, reader, etag, 0)
}

// parseFaceBox reads the ?box=x,y,w,h query value into the renderer's normalised
// box, reporting whether it is usable.
func parseFaceBox(raw string) (avatar.Box, bool) {
	values, ok := parseBoxValues(raw)
	if !ok {
		return avatar.Box{}, false
	}
	box := avatar.Box{X: values[0], Y: values[1], W: values[2], H: values[3]}
	if !boxMeetsFrame(box) {
		return avatar.Box{}, false
	}
	return box, true
}

// parseBoxValues splits the query value into its four numbers, reporting whether
// every one of them is a finite float. A non-finite value is refused rather than
// clamped: it is not a box somebody meant, and the geometry downstream would turn
// it into NaN pixel coordinates.
func parseBoxValues(raw string) ([faceCropBoxParts]float64, bool) {
	var values [faceCropBoxParts]float64
	parts := strings.Split(raw, ",")
	if len(parts) != faceCropBoxParts {
		return values, false
	}
	for i, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return values, false
		}
		values[i] = value
	}
	return values, true
}

// boxMeetsFrame reports whether the box has a positive size and overlaps the
// frame it is measured against.
//
// A detector's box may hang over an edge, and the renderer slides it back inside
// rather than clipping it — that geometry is deliberate and must be let through.
// One lying wholly outside is different: it names no pixels, and rendering it
// would cost a decode and a cache entry to say so.
func boxMeetsFrame(box avatar.Box) bool {
	if box.W <= 0 || box.H <= 0 {
		return false
	}
	return box.X < 1 && box.Y < 1 && box.X+box.W > 0 && box.Y+box.H > 0
}
