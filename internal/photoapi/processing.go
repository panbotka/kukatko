package photoapi

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
)

// ProcessingService answers what the library has already computed about one
// photo, and schedules a single step that has not run. It is a narrow interface
// so photoapi depends on the behaviour rather than on the processing package's
// wiring; *processing.Service satisfies it and a test fake stands in.
type ProcessingService interface {
	// Report returns the state of every step for photoUID, in a fixed order, or
	// photos.ErrPhotoNotFound.
	Report(ctx context.Context, photoUID string) ([]processing.Status, error)
	// Run schedules one step for photoUID and returns that step's new state. It
	// returns processing.ErrUnknownStep, photos.ErrPhotoNotFound or
	// processing.ErrStepNotApplicable.
	Run(ctx context.Context, photoUID string, step processing.Step) (processing.Status, error)
}

// resolveProcessing returns the photo's processing report for the detail
// response, or nil — so the response omits the block — when no processing
// service is wired or the report cannot be read. Like resolveUploader and
// resolvePlace it never fails the detail request over its own field: a photo is
// worth showing even when the account of what has been computed about it is not
// available.
func (a *API) resolveProcessing(ctx context.Context, photoUID string) []processing.Status {
	if a.processing == nil {
		return nil
	}
	report, err := a.processing.Report(ctx, photoUID)
	if err != nil {
		log.Printf("photoapi: reading processing report for %s: %v", photoUID, err)
		return nil
	}
	return report
}

// handleRunProcessingStep schedules one per-photo computation — the step named in
// the path — for the photo named in the path, and answers with that step's new
// state. It is the repair for a photo the pipeline missed: maintainers only (the
// route is guarded by RequireMaintainer), and idempotent, because the queue's
// dedup index absorbs a request for work that is already waiting or running.
//
// A step name outside the reported set is answered with 400, an unknown photo
// with 404, and a step that cannot apply to this photo (a place with no
// coordinate, faces or text on a video) with 409 — queueing it would only
// produce the same "skipped" again. When no processing service is wired it
// answers 503.
func (a *API) handleRunProcessingStep(w http.ResponseWriter, r *http.Request) {
	if a.processing == nil {
		writeError(w, http.StatusServiceUnavailable, "processing is not available")
		return
	}
	uid := chi.URLParam(r, "uid")
	step, ok := processing.ParseStep(chi.URLParam(r, "step"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown processing step")
		return
	}
	status, err := a.processing.Run(r.Context(), uid, step)
	if err != nil {
		writeProcessingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// writeProcessingError maps a scheduling error to an HTTP response: 400 for an
// unknown step, 404 for a missing photo, 409 for a step that does not apply,
// otherwise 500.
func writeProcessingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, processing.ErrUnknownStep):
		writeError(w, http.StatusBadRequest, "unknown processing step")
	case errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, processing.ErrStepNotApplicable):
		writeError(w, http.StatusConflict, "this step does not apply to this photo")
	default:
		writeError(w, http.StatusInternalServerError, "scheduling the processing step failed")
	}
}
