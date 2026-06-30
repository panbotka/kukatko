package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// enqueuePhotoJob enqueues a job of jobType carrying {"photo_uid": photoUID},
// swallowing ErrDuplicate so the call is idempotent per photo.
func (e *Enqueuer) enqueuePhotoJob(ctx context.Context, jobType, photoUID string) error {
	payload, err := photoPayload(photoUID)
	if err != nil {
		return err
	}
	if _, err := e.store.Enqueue(ctx, jobType, payload, EnqueueOptions{}); err != nil {
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
