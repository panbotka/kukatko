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

// EnqueueImageEmbed schedules CLIP embedding for the photo identified by
// photoUID. A pre-existing active job for the same photo is a no-op (nil error).
func (e *Enqueuer) EnqueueImageEmbed(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeImageEmbed, photoUID)
}

// EnqueueFaceDetect schedules face detection for the photo identified by
// photoUID. A pre-existing active job for the same photo is a no-op (nil error).
func (e *Enqueuer) EnqueueFaceDetect(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeFaceDetect, photoUID)
}

// EnqueueThumbnail schedules thumbnail regeneration (and pHash recompute when
// missing) for the photo identified by photoUID. A pre-existing active job for
// the same photo is a no-op (nil error). It backs the library-maintenance
// thumbnail and pHash repairs.
func (e *Enqueuer) EnqueueThumbnail(ctx context.Context, photoUID string) error {
	return e.enqueuePhotoJob(ctx, TypeThumbnail, photoUID)
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
