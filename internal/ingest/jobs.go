package ingest

import "context"

// JobEnqueuer schedules the asynchronous post-ingest work for a freshly created
// photo: the CLIP image embedding (image_embed) and face detection
// (face_detect). Ingest depends only on this narrow interface so the upload
// pipeline does not have to know how — or whether — the queue is implemented.
//
// The work is deferred rather than run inline because it calls the embedding
// service on the build box, which is frequently offline; uploads and browsing
// must keep working regardless, so the jobs wait in a persistent Postgres queue.
type JobEnqueuer interface {
	// EnqueueImageEmbed schedules CLIP embedding for the photo identified by
	// photoUID.
	EnqueueImageEmbed(ctx context.Context, photoUID string) error
	// EnqueueFaceDetect schedules face detection for the photo identified by
	// photoUID.
	EnqueueFaceDetect(ctx context.Context, photoUID string) error
}

// SidecarEnqueuer schedules the metadata sidecar of a freshly catalogued photo —
// the YAML file in storage holding its metadata and curation. It is separate from
// JobEnqueuer because it is separately switchable: the sidecar export has its own
// config key, and when it is off no `sidecar` handler is registered, so a job
// enqueued anyway would sit in the queue forever. A nil SidecarEnqueuer is how
// that off state reaches the pipeline.
//
// It is satisfied by jobs.Enqueuer.
type SidecarEnqueuer interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// NopEnqueuer is the no-op JobEnqueuer used until the persistent job queue
// exists. Both methods succeed without doing anything, so the pipeline runs
// end to end (stream, dedup, store, catalogue, thumbnails) with the embedding
// and face work simply not yet scheduled.
//
// Pending the jobs milestone, NopEnqueuer is replaced by the Postgres-backed
// queue so image_embed and face_detect rows are persisted here and drained by
// the background worker. This is the single wiring point the jobs task changes.
type NopEnqueuer struct{}

// EnqueueImageEmbed is a no-op that reports success.
func (NopEnqueuer) EnqueueImageEmbed(_ context.Context, _ string) error { return nil }

// EnqueueFaceDetect is a no-op that reports success.
func (NopEnqueuer) EnqueueFaceDetect(_ context.Context, _ string) error { return nil }
