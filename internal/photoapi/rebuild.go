package photoapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
	"github.com/panbotka/kukatko/internal/worker"
)

// The two outcomes of a rebuild request. They are the whole vocabulary of the
// response's status field: the work was redone here and now, or the backing
// service was not reachable and the forced job is waiting in the queue for it.
const (
	// RebuildStatusRebuilt means the computation ran and its result replaced what
	// was stored.
	RebuildStatusRebuilt = "rebuilt"
	// RebuildStatusQueued means the backing service was unavailable, so a forced job
	// was enqueued instead and will redo the work when it comes back.
	RebuildStatusQueued = "queued"
)

// PhotoReembedder recomputes a photo's image embedding and replaces the stored
// one, where the plain job would skip a photo that already has an embedding. It
// is satisfied by *embedjob.Service.
type PhotoReembedder interface {
	// ForceEmbed recomputes photoUID's embedding. It returns a worker deferral when
	// the embeddings sidecar is offline and photos.ErrPhotoNotFound for an unknown
	// photo.
	ForceEmbed(ctx context.Context, photoUID string) error
}

// PhotoRedetector runs face detection over a photo again and replaces the stored
// faces, where the plain job would skip a photo whose detection is already
// recorded. It is satisfied by *facejob.Service.
type PhotoRedetector interface {
	// ForceDetect re-detects the faces on photoUID and returns how many the photo
	// has afterwards. It returns a worker deferral when the embeddings sidecar is
	// offline and photos.ErrPhotoNotFound for an unknown photo.
	ForceDetect(ctx context.Context, photoUID string) (int, error)
}

// PhotoRegeocoder resolves a photo's coordinates again and replaces the cached
// place, where the plain job would skip a coordinate it has already resolved. It
// is satisfied by *placesjob.Service.
type PhotoRegeocoder interface {
	// ForceGeocode re-geocodes photoUID's coordinates. It returns a worker deferral
	// when mapy.com is unavailable, rate limited or out of credit budget, and
	// photos.ErrPhotoNotFound for an unknown photo.
	ForceGeocode(ctx context.Context, photoUID string) error
}

// RebuildEnqueuer schedules a forced per-photo job through the queue — the
// asynchronous half of every rebuild endpoint, used when the backing service is
// offline and the work has to wait for it. It is satisfied by jobs.Enqueuer.
//
// Each method carries the forced flag in the job payload, so queue dedup stays
// keyed on type + photo uid: two rebuild requests for the same photo collapse
// into one job exactly as two plain ones would. The jobs.ForceOutcome each of
// them returns is how that collapse is told apart from the one collision the
// queue cannot resolve — a job already running with the plain payload, which
// queueRebuild has to refuse rather than report as queued.
type RebuildEnqueuer interface {
	// EnqueueImageEmbedRebuild schedules a forced re-embedding of photoUID.
	EnqueueImageEmbedRebuild(ctx context.Context, photoUID string) (jobs.ForceOutcome, error)
	// EnqueueFaceDetectRebuild schedules a forced re-detection of photoUID's faces.
	EnqueueFaceDetectRebuild(ctx context.Context, photoUID string) (jobs.ForceOutcome, error)
	// EnqueuePlacesRebuild schedules a forced re-geocode of photoUID.
	EnqueuePlacesRebuild(ctx context.Context, photoUID string) (jobs.ForceOutcome, error)
}

// rebuildResponse is the JSON body of a successful rebuild: which computation was
// redone, whether it ran here or is queued, and — for a face re-detection — how
// many faces the photo has now. The count is the whole point of doing the work
// synchronously: "the detector ran again and this photo now has three faces" is an
// answer, "scheduled" is not.
type rebuildResponse struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Faces  *int   `json:"faces,omitempty"`
}

// rebuildSpec is what distinguishes the three rebuild endpoints: the step they
// redo, the audit action recording it, whether the service behind it is wired at
// all, and the two functions that do the work — run it now, or queue it for a
// service that is not answering.
type rebuildSpec struct {
	// step names the computation, using the processing report's own vocabulary so
	// the response and GET /photos/{uid} agree on what a step is called.
	step string
	// action is the audit action the rebuild is recorded under.
	action string
	// ready reports whether the recomputing service is wired; false answers 503.
	ready bool
	// run recomputes the step and returns the face count when the step has one.
	run func(ctx context.Context, photoUID string) (*int, error)
	// enqueue schedules the forced job, for when run reports the service is
	// offline, and reports what the queue did with it.
	enqueue func(ctx context.Context, photoUID string) (jobs.ForceOutcome, error)
}

// handleReembed recomputes the photo's image embedding and replaces the stored
// one. Unlike POST /photos/{uid}/process/image_embed — which enqueues the
// *repair* and is a silent no-op for a photo that already has an embedding — this
// discards the stored vector and computes a new one, which is what a photo
// embedded from a preview that has since been corrected needs.
//
// It answers 200 with the step and status, 404 for a missing photo, 409 when a
// job for this photo is already running and the force could only be dropped, 503
// when no embedding service is wired, and 500 otherwise. When the embeddings box is
// offline it enqueues the forced job instead of failing and answers "queued", so
// the rebuild survives a sleeping box exactly as the plain path does. Maintainers
// only (the route is guarded by RequireMaintainer): it throws stored work away.
func (a *API) handleReembed(w http.ResponseWriter, r *http.Request) {
	a.runRebuild(w, r, rebuildSpec{
		step:   string(processing.StepImageEmbed),
		action: audit.ActionPhotoEmbedding,
		ready:  a.reembedder != nil,
		run: func(ctx context.Context, photoUID string) (*int, error) {
			return nil, a.reembedder.ForceEmbed(ctx, photoUID)
		},
		enqueue: func(ctx context.Context, photoUID string) (jobs.ForceOutcome, error) {
			return a.rebuilds.EnqueueImageEmbedRebuild(ctx, photoUID)
		},
	})
}

// handleRedetectFaces runs face detection over the photo again and replaces its
// stored faces, answering with how many faces it has afterwards. The previous
// detections are deleted rather than added to, and a face that comes back in the
// same place keeps the person it was assigned to, so redoing the detection never
// leaves duplicates behind and never un-names anybody.
//
// It answers, defers and is guarded exactly as handleReembed does; see there.
func (a *API) handleRedetectFaces(w http.ResponseWriter, r *http.Request) {
	a.runRebuild(w, r, rebuildSpec{
		step:   string(processing.StepFaceDetect),
		action: audit.ActionPhotoFaces,
		ready:  a.redetector != nil,
		run: func(ctx context.Context, photoUID string) (*int, error) {
			count, err := a.redetector.ForceDetect(ctx, photoUID)
			if err != nil {
				return nil, fmt.Errorf("photoapi: re-detecting faces for %s: %w", photoUID, err)
			}
			return &count, nil
		},
		enqueue: func(ctx context.Context, photoUID string) (jobs.ForceOutcome, error) {
			return a.rebuilds.EnqueueFaceDetectRebuild(ctx, photoUID)
		},
	})
}

// handleRegeocode resolves the photo's coordinates again and replaces the cached
// place, where the repair path would skip a coordinate already resolved. It
// spends a mapy.com credit every time, which is the reason it is a deliberate
// request rather than anything automatic.
//
// It answers, defers and is guarded exactly as handleReembed does; see there. An
// exhausted credit budget and a rate-limited geocoder both read as "offline" and
// queue the forced job.
func (a *API) handleRegeocode(w http.ResponseWriter, r *http.Request) {
	a.runRebuild(w, r, rebuildSpec{
		step:   string(processing.StepPlaces),
		action: audit.ActionPhotoPlace,
		ready:  a.regeocoder != nil,
		run: func(ctx context.Context, photoUID string) (*int, error) {
			return nil, a.regeocoder.ForceGeocode(ctx, photoUID)
		},
		enqueue: func(ctx context.Context, photoUID string) (jobs.ForceOutcome, error) {
			return a.rebuilds.EnqueuePlacesRebuild(ctx, photoUID)
		},
	})
}

// runRebuild is the body every rebuild endpoint shares: refuse when the service
// is not wired, recompute, and fall back to the queue when the backing service is
// merely asleep. A successful outcome — whether it ran or was queued — is
// recorded in the audit trail, because either way the stored evidence is being
// thrown away and replaced.
func (a *API) runRebuild(w http.ResponseWriter, r *http.Request, spec rebuildSpec) {
	if !spec.ready {
		writeError(w, http.StatusServiceUnavailable, "rebuilding "+spec.step+" is not available")
		return
	}
	uid := chi.URLParam(r, "uid")
	faces, err := spec.run(r.Context(), uid)
	switch {
	case err == nil:
		a.finishRebuild(w, r, spec, uid, rebuildResponse{
			Step: spec.step, Status: RebuildStatusRebuilt, Faces: faces,
		})
	case worker.IsDeferral(err):
		a.queueRebuild(w, r, spec, uid)
	default:
		writeRebuildError(w, spec, err)
	}
}

// queueRebuild schedules the forced job for a photo whose backing service is
// offline and answers "queued", so an unreachable box costs the caller a wait
// rather than an error. When no enqueuer is wired there is nothing to fall back
// to and the outage is reported as 503 — the honest answer, since the work is
// then neither done nor scheduled.
//
// A forced job that collides with a run already in flight is the second answer
// that would otherwise be dishonest: it cannot be scheduled at all — the running
// job holds the plain payload it was claimed with and will take its idempotent
// skip — so it is refused with 409 rather than reported as queued. Every other
// collision does end in a forced job (a queued one is upgraded, an already-forced
// one absorbs the request), and answers 200.
func (a *API) queueRebuild(w http.ResponseWriter, r *http.Request, spec rebuildSpec, uid string) {
	if a.rebuilds == nil {
		writeError(w, http.StatusServiceUnavailable,
			"rebuilding "+spec.step+" is unavailable right now and cannot be queued")
		return
	}
	outcome, err := spec.enqueue(r.Context(), uid)
	if err != nil {
		log.Printf("photoapi: queueing %s rebuild for %s: %v", spec.step, uid, err)
		writeError(w, http.StatusInternalServerError, "queueing the rebuild failed")
		return
	}
	if outcome == jobs.ForceInFlight {
		writeError(w, http.StatusConflict,
			"a "+spec.step+" job for this photo is already running and cannot be forced; "+
				"retry the rebuild once it has finished")
		return
	}
	a.finishRebuild(w, r, spec, uid, rebuildResponse{Step: spec.step, Status: RebuildStatusQueued})
}

// finishRebuild records the rebuild in the audit trail and writes the response.
func (a *API) finishRebuild(
	w http.ResponseWriter, r *http.Request, spec rebuildSpec, uid string, resp rebuildResponse,
) {
	a.recordRebuildAudit(r, spec, uid, resp)
	writeJSON(w, http.StatusOK, resp)
}

// writeRebuildError maps a rebuild failure to an HTTP response: 404 for a missing
// photo, otherwise 500. A photo whose original cannot be read is a 500 rather than
// a 422: unlike a thumbnail, neither the sidecar nor the geocoder distinguishes
// "this source can never work" from "this attempt did not", so claiming the
// stronger answer would be a guess.
func writeRebuildError(w http.ResponseWriter, spec rebuildSpec, err error) {
	if errors.Is(err, photos.ErrPhotoNotFound) {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "rebuilding "+spec.step+" failed")
}

// recordRebuildAudit best-effort records the rebuild in the audit trail,
// attributing it to the acting user and carrying the outcome (and, for faces, the
// resulting count) in the entry's details. A recording failure is logged but never
// fails the request: the work has already been done, so answering with an error
// would misreport it. When no recorder is wired it is a no-op.
func (a *API) recordRebuildAudit(r *http.Request, spec rebuildSpec, uid string, resp rebuildResponse) {
	if a.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	details := map[string]any{"status": resp.Status}
	if resp.Faces != nil {
		details["faces"] = *resp.Faces
	}
	entry := audit.FromRequest(r, user.UID).Entry(spec.action, "photos", uid, details)
	if err := a.audit.Record(r.Context(), entry); err != nil {
		log.Printf("photoapi: recording %s rebuild audit for %s: %v", spec.step, uid, err)
	}
}
