package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// photoEnqueuer is the subset of Store the Enqueuer depends on, kept as an
// interface so the adapter can be unit-tested with a fake.
type photoEnqueuer interface {
	Enqueue(ctx context.Context, jobType string, payload json.RawMessage, opts EnqueueOptions) (Job, error)
	UpgradeToForced(ctx context.Context, jobType, photoUID string, payload json.RawMessage) (ForceOutcome, error)
}

// Enqueuer adapts the queue Store to the post-ingest scheduling interface used by
// the upload pipeline (ingest.JobEnqueuer). It enqueues image_embed and
// face_detect jobs keyed by photo UID and treats a dedup hit as success, so
// re-uploading the same photo schedules each kind of work at most once.
type Enqueuer struct {
	store photoEnqueuer
	// clock reads the current time for the sidecar debounce. Nil means the wall
	// clock; only tests set it.
	clock func() time.Time
}

// NewEnqueuer returns an Enqueuer backed by store.
func NewEnqueuer(store *Store) *Enqueuer {
	return &Enqueuer{store: store}
}

// EnqueueImageEmbed schedules image embedding for the photo identified by
// photoUID. A pre-existing active job for the same photo is a no-op (nil error).
func (e *Enqueuer) EnqueueImageEmbed(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeImageEmbed, photoUID)
}

// EnqueueFaceDetect schedules face detection for the photo identified by
// photoUID. A pre-existing active job for the same photo is a no-op (nil error).
func (e *Enqueuer) EnqueueFaceDetect(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeFaceDetect, photoUID)
}

// EnqueueOCR schedules text recognition for the photo identified by photoUID. A
// pre-existing active job for the same photo is a no-op (nil error).
func (e *Enqueuer) EnqueueOCR(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeOCR, photoUID)
}

// EnqueueThumbnail schedules thumbnail regeneration (and pHash recompute when
// missing) for the photo identified by photoUID. A pre-existing active job for
// the same photo is a no-op (nil error). It backs the library-maintenance
// thumbnail and pHash repairs.
func (e *Enqueuer) EnqueueThumbnail(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeThumbnail, photoUID)
}

// EnqueueThumbnailRebuild schedules a *forced* thumbnail rebuild for the photo
// identified by photoUID: unlike EnqueueThumbnail, the handler overwrites the
// sizes already cached instead of skipping them. It is what a change to the
// photo's rendering — saving or resetting a non-destructive edit — schedules,
// because the cache is keyed by the original's file hash and would otherwise keep
// serving the previous rendering forever.
//
// The forced flag rides in the payload, so dedup (keyed on type + photo_uid) is
// unchanged: at most one active thumbnail job per photo. A plain thumbnail job
// already queued for the same photo is upgraded to this one rather than
// absorbing it, so the edit is rendered by the job that was already waiting. A
// job that is already *running* keeps the collision it always had — its outcome
// is dropped here rather than reported, because a caller saving an edit has
// nothing to do with it and the running job re-reads the photo anyway.
func (e *Enqueuer) EnqueueThumbnailRebuild(ctx context.Context, photoUID string) error {
	_, err := e.enqueueForcedPhotoJob(ctx, TypeThumbnail, photoUID)
	return err
}

// EnqueueImageEmbedRebuild schedules a *forced* re-embedding of the photo
// identified by photoUID: unlike EnqueueImageEmbed, the handler recomputes the
// vector instead of skipping a photo that already has one. It is what an operator
// reaches for when the stored embedding describes the wrong picture — computed
// from a preview that has since been corrected, or by a model that has since
// changed — because nothing about the photo says its embedding is stale.
//
// The forced flag rides in the payload, so dedup (keyed on type + photo_uid) is
// unchanged: at most one active image_embed job per photo, exactly as for the
// plain enqueue. The returned ForceOutcome says which of the collisions that
// leaves happened, so the endpoint behind it can answer "queued" for a force that
// is really going to run and refuse the one that is not — see
// enqueueForcedPhotoJob.
func (e *Enqueuer) EnqueueImageEmbedRebuild(
	ctx context.Context, photoUID string,
) (ForceOutcome, error) {
	return e.enqueueForcedPhotoJob(ctx, TypeImageEmbed, photoUID)
}

// EnqueueFaceDetectRebuild schedules a *forced* re-detection of the faces on the
// photo identified by photoUID: unlike EnqueueFaceDetect, the handler runs the
// detector again instead of skipping a photo whose detection is already recorded,
// and the faces it finds replace the ones stored before. It is the counterpart of
// EnqueueImageEmbedRebuild for the second thing the sidecar computes about a
// photo, and dedupes the same way.
//
// It reports its ForceOutcome for the same reason EnqueueImageEmbedRebuild does.
func (e *Enqueuer) EnqueueFaceDetectRebuild(
	ctx context.Context, photoUID string,
) (ForceOutcome, error) {
	return e.enqueueForcedPhotoJob(ctx, TypeFaceDetect, photoUID)
}

// EnqueuePlacesRebuild schedules a *forced* re-geocode of the photo identified by
// photoUID: unlike EnqueuePlaces, the handler asks mapy.com again instead of
// skipping a coordinate it has already resolved. Every geocode costs a credit, so
// this is deliberately the manual path — the backfill and the upload pipeline
// keep using the plain, skipping enqueue.
//
// It reports its ForceOutcome for the same reason EnqueueImageEmbedRebuild does.
func (e *Enqueuer) EnqueuePlacesRebuild(
	ctx context.Context, photoUID string,
) (ForceOutcome, error) {
	return e.enqueueForcedPhotoJob(ctx, TypePlaces, photoUID)
}

// EnqueuePlaces schedules reverse geocoding for the photo identified by photoUID.
// A pre-existing active job for the same photo is a no-op (nil error). It backs
// the place backfill that fills the location cache for geotagged photos.
func (e *Enqueuer) EnqueuePlaces(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypePlaces, photoUID)
}

// EnqueueMetadata schedules a re-read of the photo's original file into the
// metadata columns it is the authority on. A pre-existing active job for the same
// photo is a no-op (nil error). It backs the metadata backfill over the photos that
// were catalogued before the extractor could read those tags.
func (e *Enqueuer) EnqueueMetadata(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeMetadata, photoUID)
}

// EnqueueStoryboard schedules generation of the photo's scrub-preview sprite. A
// pre-existing active job for the same photo is a no-op (nil error), which is what
// makes the player's "ask on every playback" free: the first request schedules the
// render and every later one is absorbed while it is queued or running.
func (e *Enqueuer) EnqueueStoryboard(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeStoryboard, photoUID)
}

// SidecarDebounce is how long a sidecar job waits before it may run. It is the
// coalescing window: the dedup index keeps at most one queued sidecar job per
// photo, so every edit landing within this window of the first one is absorbed by
// the job already waiting and the file is written once, after the user stops
// typing — rather than once per keystroke-sized PATCH. It is short enough that a
// sidecar is current within seconds of an edit, which is all "the curation
// survives the database" needs.
const SidecarDebounce = 5 * time.Second

// EnqueueSidecar schedules a rewrite of the photo's metadata sidecar. Sidecar
// dedup is scoped to the queued state (idx_jobs_dedup, migration 0044): a
// pre-existing *queued* job for the same photo is a no-op (nil error), which is
// what debounces a burst of edits into a single file write. An edit that lands
// while a sidecar job is already *running* is not swallowed — it schedules a fresh
// follow-up, because the running job read and wrote the photo before that edit and
// would otherwise leave the on-disk sidecar stale. The job is delayed by
// SidecarDebounce so that burst has a window to collapse into.
//
// Callers enqueue this after their mutation has committed: the job re-reads the
// photo, so enqueuing after the write is what makes it serialise the new value
// rather than the old one.
func (e *Enqueuer) EnqueueSidecar(ctx context.Context, photoUID string) error {
	runAfter := e.now().Add(SidecarDebounce)
	return e.enqueuePhotoJobOpts(ctx, TypeSidecar, photoUID, EnqueueOptions{RunAfter: &runAfter})
}

// now returns the current time, indirected through the Enqueuer so tests can pin
// it. A nil clock (the normal case) reads the wall clock.
func (e *Enqueuer) now() time.Time {
	if e.clock == nil {
		return time.Now()
	}
	return e.clock()
}

// enqueuePhotoJob enqueues a job of jobType carrying {"photo_uid": photoUID} with
// the default options, swallowing ErrDuplicate so the call is idempotent per
// photo.
func (e *Enqueuer) enqueuePhotoJob(ctx context.Context, jobType, photoUID string) error {
	return e.enqueuePhotoJobOpts(ctx, jobType, photoUID, EnqueueOptions{})
}

// enqueuePhotoJobOpts enqueues a job of jobType carrying {"photo_uid": photoUID}
// with opts, swallowing ErrDuplicate so the call is idempotent per photo.
func (e *Enqueuer) enqueuePhotoJobOpts(
	ctx context.Context, jobType, photoUID string, opts EnqueueOptions,
) error {
	payload, err := photoPayload(photoUID)
	if err != nil {
		return err
	}
	return e.enqueuePayload(ctx, jobType, photoUID, payload, opts)
}

// forceEnqueueAttempts bounds the insert/upgrade retry of a forced enqueue. Each
// round loses the insert to the dedup index and then finds the colliding job
// already finished; one repeat covers that window comfortably, and a third round
// only exists so a busy queue is never reported as a failure.
const forceEnqueueAttempts = 3

// enqueueForcedPhotoJob schedules a job of jobType carrying
// {"photo_uid": photoUID, "force": true} and reports what became of it. It is the
// shared body of every rebuild enqueue: they differ only in the job type, since
// what "force" means is the handler's business.
//
// Dedup is keyed on type + photo_uid, so a photo the queue is already working on
// leaves no room for a second job — and dropping the forced payload there is what
// used to make a rebuild a silent no-op, because the plain job that survived took
// its idempotent skip. So a collision is resolved rather than swallowed: a queued
// job is upgraded to the forced payload (ForceUpgraded), an already-forced one
// absorbs the request (ForceAbsorbed), and a running one cannot be touched at all
// (ForceInFlight) — that run is using the payload it was claimed with, so the
// force has not been scheduled and the caller has to be told.
//
// The retry covers the one case where neither the insert nor the upgrade applies:
// the colliding job finished in between, leaving nothing to upgrade and room to
// insert after all.
func (e *Enqueuer) enqueueForcedPhotoJob(
	ctx context.Context, jobType, photoUID string,
) (ForceOutcome, error) {
	payload, err := forcedPhotoPayload(photoUID)
	if err != nil {
		return "", err
	}
	for range forceEnqueueAttempts {
		_, enqueueErr := e.store.Enqueue(ctx, jobType, payload, EnqueueOptions{})
		if enqueueErr == nil {
			return ForceScheduled, nil
		}
		if !errors.Is(enqueueErr, ErrDuplicate) {
			return "", fmt.Errorf("jobs: enqueuing %s for %s: %w", jobType, photoUID, enqueueErr)
		}
		outcome, upgradeErr := e.store.UpgradeToForced(ctx, jobType, photoUID, payload)
		if upgradeErr == nil {
			return outcome, nil
		}
		if !errors.Is(upgradeErr, ErrNoActiveJob) {
			return "", fmt.Errorf("jobs: forced enqueue: %w", upgradeErr)
		}
	}
	return "", fmt.Errorf("jobs: forcing %s for %s: %w", jobType, photoUID, ErrEnqueueRaced)
}

// enqueuePayload enqueues a job of jobType carrying an already-built payload,
// swallowing ErrDuplicate so the call is idempotent per photo. photoUID is used
// only to describe the failure.
func (e *Enqueuer) enqueuePayload(
	ctx context.Context, jobType, photoUID string, payload json.RawMessage, opts EnqueueOptions,
) error {
	if _, err := e.store.Enqueue(ctx, jobType, payload, opts); err != nil {
		if errors.Is(err, ErrDuplicate) {
			return nil
		}
		return fmt.Errorf("jobs: enqueuing %s for %s: %w", jobType, photoUID, err)
	}
	return nil
}

// photoPayload builds the canonical {"photo_uid": uid} JSON payload that the
// dedup index keys on.
func photoPayload(uid string) (json.RawMessage, error) {
	raw, err := json.Marshal(map[string]string{"photo_uid": uid})
	if err != nil {
		return nil, fmt.Errorf("jobs: marshaling photo payload: %w", err)
	}
	return raw, nil
}

// forcedPhotoPayload builds the {"photo_uid": uid, "force": true} payload of a
// job that must redo work already done. It keeps the `photo_uid` key the dedup
// index reads, so a forced job dedupes against a plain one exactly as two plain
// jobs would.
func forcedPhotoPayload(uid string) (json.RawMessage, error) {
	raw, err := json.Marshal(map[string]any{"photo_uid": uid, "force": true})
	if err != nil {
		return nil, fmt.Errorf("jobs: marshaling forced photo payload: %w", err)
	}
	return raw, nil
}
