package photoapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/storyboardjob"
)

// renditionView is one encoded streaming quality as the inventory endpoint
// reports it: what it is called, what picture it carries, what it demands of a
// connection and when it was produced.
//
// The media playlist is deliberately absent. It is by far the largest column of
// the row — thousands of segment durations for a long clip — and it is already
// served, rewritten, by the media-playlist route; repeating it here would turn a
// glance at a video's renditions into the biggest response in the API.
type renditionView struct {
	// Rendition is the quality's name, verbatim as it appears in object keys.
	Rendition string `json:"rendition"`
	// Width and Height are the encoded picture's real pixel size.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Bandwidth is the peak bandwidth in bits per second the master playlist
	// advertises for this quality.
	Bandwidth int `json:"bandwidth"`
	// Codecs is the RFC 6381 codecs string of the rendition's streams.
	Codecs string `json:"codecs"`
	// SegmentCount is how many media segments were published, the initialisation
	// segment excluded.
	SegmentCount int `json:"segment_count"`
	// DurationMs is the encoded length in milliseconds. It is the rendition's own
	// measurement rather than the catalogue's, so a clip whose container lied
	// about its length can be told apart from one that did not.
	DurationMs int `json:"duration_ms"`
	// EncodedAt is when this rendition was last produced.
	EncodedAt time.Time `json:"encoded_at"`
}

// renditionsResponse is the JSON body of the rendition inventory: every recorded
// quality of one video, widest picture first. The list is empty — never absent —
// for a photo that has none, which is the same answer for a still, for a video
// the encode has not reached and for an instance with streaming switched off. The
// three are told apart by the photo's own media type and by the processing report,
// not by this endpoint inventing a state word for them.
type renditionsResponse struct {
	Renditions []renditionView `json:"renditions"`
}

// handleRenditions answers with what the streaming encode has actually produced
// for one video: which qualities exist, how big each picture is, what bitrate it
// asks of the connection, how many segments it was cut into and when it was
// encoded.
//
// It exists because nothing else answers it. The master playlist names the
// qualities but says nothing about when they were made, `hls` on the photo payload
// is one boolean for the whole clip, and the processing report knows only that
// *some* rendition exists. An operator asking "is this clip encoded, and is what
// is stored still current?" had to read the database.
//
// The photo is looked up first, so an unknown uid is a 404 rather than an empty
// inventory that reads as "this video has not been encoded". It needs no more than
// RequireAuth: it is a read, and it tells the caller nothing they could not learn
// by fetching the master playlist they are already allowed to play.
func (a *API) handleRenditions(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if _, err := a.store.GetByUID(r.Context(), uid); err != nil {
		writePhotoError(w, err, "loading photo failed")
		return
	}
	if a.hls == nil {
		writeJSON(w, http.StatusOK, renditionsResponse{Renditions: []renditionView{}})
		return
	}
	encoded, err := a.hls.ListForPhoto(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing renditions failed")
		return
	}
	writeJSON(w, http.StatusOK, renditionsResponse{Renditions: renditionViews(encoded)})
}

// renditionViews projects the recorded rows onto the wire shape, dropping the
// playlist text each of them carries.
func renditionViews(encoded []hlsjob.Encoded) []renditionView {
	views := make([]renditionView, 0, len(encoded))
	for _, enc := range encoded {
		views = append(views, renditionView{
			Rendition:    enc.Rendition,
			Width:        enc.Width,
			Height:       enc.Height,
			Bandwidth:    enc.Bandwidth,
			Codecs:       enc.Codecs,
			SegmentCount: enc.SegmentCount,
			DurationMs:   enc.DurationMs,
			EncodedAt:    enc.EncodedAt,
		})
	}
	return views
}

// storyboardRebuildResponse is the JSON body of the forced storyboard rebuild. It
// borrows the processing report's vocabulary — a step and its new state — rather
// than the rebuild endpoints' `status`, because that is what it honestly is: the
// sprite is not rendered here, it is scheduled, and the caller's next question is
// the same one the report answers.
type storyboardRebuildResponse struct {
	Step  string `json:"step"`
	State string `json:"state"`
}

// storyboardStep is what the rebuild response calls the work it scheduled. It is
// the queue's own job type, and deliberately not one of processing.Steps: a
// storyboard is rendered lazily on first playback and leaves no persisted
// evidence, so the processing report has nothing to say about it.
const storyboardStep = "storyboard"

// storyboardQueued is the state a successful rebuild reports. It is
// processing.StateQueued rather than storyboardjob.StatePending on purpose: the
// two packages spell "the work is in the queue" differently, and a client reading
// this beside a processing report must not have to know which vocabulary it is
// in — there, `pending` means the opposite, that nothing is scheduled at all.
const storyboardQueued = string(processing.StateQueued)

// handleRegenerateStoryboard throws a video's cached scrub-preview sprite away and
// schedules a fresh one.
//
// It is the storyboard's missing rebuild. `GET /photos/{uid}/storyboard` schedules
// generation only when there is no sprite, and the renderer behind it is a no-op
// over one that already exists — so a clip whose preview is wrong (cut before its
// duration was corrected, or from an original that has since been replaced) has no
// way back. This endpoint discards the sprite first and only then queues the
// render, which is the whole difference.
//
// It answers 200 with `{step, state:"queued"}` — the work is in the queue; the
// sprite is not rendered here. 404 for a missing photo, 409 for a
// photo that can never have a preview — a still, a live photo, a clip of unknown
// length — and 503 when nothing on this instance could render one. Maintainers
// only: it discards stored work and schedules a full decode of the clip.
func (a *API) handleRegenerateStoryboard(w http.ResponseWriter, r *http.Request) {
	if a.storyboards == nil {
		writeError(w, http.StatusServiceUnavailable, "storyboards are not available")
		return
	}
	uid := chi.URLParam(r, "uid")
	if err := a.storyboards.Regenerate(r.Context(), uid); err != nil {
		writeStoryboardRebuildError(w, err)
		return
	}
	a.recordStoryboardAudit(r, uid)
	writeJSON(w, http.StatusOK, storyboardRebuildResponse{
		Step: storyboardStep, State: storyboardQueued,
	})
}

// writeStoryboardRebuildError maps a refused regeneration onto a status code: 404
// for a photo that is not there, 409 for one that can never have a preview
// (asking again will never help), 503 for an instance that cannot render one, and
// 500 for anything else.
func writeStoryboardRebuildError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, storyboardjob.ErrNotAVideo), errors.Is(err, storyboard.ErrNoDuration):
		writeError(w, http.StatusConflict, "this photo can have no scrub preview")
	case errors.Is(err, storyboardjob.ErrCannotRender):
		writeError(w, http.StatusServiceUnavailable, "nothing here can render a scrub preview")
	default:
		writeError(w, http.StatusInternalServerError, "rebuilding the scrub preview failed")
	}
}

// recordStoryboardAudit best-effort records the forced rebuild against the acting
// user. Like every other rebuild it is audited because stored work was discarded;
// like every other rebuild a recording failure never fails the request, since the
// sprite is already gone and the render already queued.
func (a *API) recordStoryboardAudit(r *http.Request, uid string) {
	if a.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	entry := audit.FromRequest(r, user.UID).
		Entry(audit.ActionPhotoStoryboard, "photos", uid, map[string]any{"status": "queued"})
	if err := a.audit.Record(r.Context(), entry); err != nil {
		log.Printf("photoapi: recording storyboard rebuild audit for %s: %v", uid, err)
	}
}
