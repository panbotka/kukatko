// Package jobs is Kukátko's persistent, Postgres-backed job queue: the typed
// models and the pgx-backed store implementing durable enqueue, SKIP LOCKED
// claiming, retry with exponential backoff, dead-lettering and stale-lock
// recovery. It is the core robustness improvement over photo-sorter's in-memory
// jobs — work survives restarts and simply waits in the queue while the
// embeddings box is offline.
//
// This package owns only the storage and claim semantics; the execution loop
// that drains the queue and calls the embedding service lives in the worker
// runtime.
package jobs

import (
	"encoding/json"
	"errors"
	"time"
)

// Sentinel errors returned by the store so callers (workers, admin handlers,
// tests) can branch with errors.Is.
var (
	// ErrDuplicate indicates an active (queued or running) job already exists for
	// the same (type, photo_uid) dedup key, so the enqueue was a no-op.
	ErrDuplicate = errors.New("jobs: active job already exists for this type and photo")
	// ErrNoJobs indicates Claim found no runnable job (the queue is empty or every
	// candidate is locked or not yet due).
	ErrNoJobs = errors.New("jobs: no runnable job available")
	// ErrJobNotFound indicates no job matched the given id (or the job was not in
	// the state the operation requires).
	ErrJobNotFound = errors.New("jobs: job not found")
	// ErrNotDead indicates a requeue was attempted on a job that is not in the
	// dead-letter state.
	ErrNotDead = errors.New("jobs: job is not dead")
	// ErrLockLost indicates the job exists but is not running under the worker id
	// that tried to finish it — typically because stale-lock recovery requeued it
	// and another worker owns it now. The late result must be dropped rather than
	// written, or it would clobber the new owner's run.
	ErrLockLost = errors.New("jobs: job lock lost to another worker")
)

// State is the lifecycle state of a job, mirrored by the SQL CHECK constraint on
// jobs.state.
type State string

// The recognised job states.
const (
	// StateQueued is a job waiting to be claimed (and runnable once run_after is
	// due).
	StateQueued State = "queued"
	// StateRunning is a job claimed by a worker and currently being processed.
	StateRunning State = "running"
	// StateDone is a successfully completed job.
	StateDone State = "done"
	// StateFailed is reserved for terminal non-retryable failures; the queue uses
	// StateQueued (retry) and StateDead (exhausted) for ordinary failures.
	StateFailed State = "failed"
	// StateDead is a job that exhausted its attempts and was dead-lettered.
	StateDead State = "dead"
)

// The recognised job types, mirroring the asynchronous work described in
// docs/ARCHITECTURE.md §8. image_embed and face_detect require the embeddings
// box; the rest run locally.
const (
	// TypeImageEmbed computes the CLIP image embedding for a photo.
	TypeImageEmbed = "image_embed"
	// TypeFaceDetect runs face detection and clustering for a photo.
	TypeFaceDetect = "face_detect"
	// TypeThumbnail (re)generates a photo's thumbnails locally.
	TypeThumbnail = "thumbnail"
	// TypePlaces reverse-geocodes a photo's GPS coordinates into a place.
	TypePlaces = "places"
	// TypeMetadata re-reads a photo's original file and fills the metadata columns
	// the file itself is the authority on (IPTC/XMP credit fields, image codec,
	// colour profile, …). It runs locally.
	TypeMetadata = "metadata"
	// TypeSidecar writes a photo's metadata sidecar — the YAML file next to the
	// originals that holds its metadata and curation, so the catalogue can be
	// rebuilt from storage alone. It runs locally. See internal/sidecarexport.
	TypeSidecar = "sidecar"
	// TypePPImport imports a batch from PhotoPrism.
	TypePPImport = "pp_import"
	// TypePSMigrate migrates data from photo-sorter.
	TypePSMigrate = "ps_migrate"
	// TypePSFeedsImport enriches PhotoPrism-imported photos with photo-sorter's
	// pre-computed embeddings and faces, copied 1:1 from its HTTP migration feeds.
	TypePSFeedsImport = "ps_feeds_import"
	// TypeBackup runs a backup.
	TypeBackup = "backup"
)

// Job is one row of the persistent queue. Payload holds the job's opaque
// arguments as JSONB (typically {"photo_uid": "..."}). LockedBy/LockedAt are set
// only while the job is running and are nil otherwise.
type Job struct {
	ID          int64           `json:"id"`
	Type        string          `json:"type"`
	State       State           `json:"state"`
	Priority    int             `json:"priority"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	LastError   string          `json:"last_error,omitempty"`
	RunAfter    time.Time       `json:"run_after"`
	LockedBy    *string         `json:"locked_by,omitempty"`
	LockedAt    *time.Time      `json:"locked_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// EnqueueOptions carries the optional knobs for Store.Enqueue. The zero value is
// valid: it enqueues a priority-0 job, runnable immediately, with the default
// maximum attempts.
type EnqueueOptions struct {
	// Priority orders claiming: higher is claimed first. Defaults to 0.
	Priority int
	// MaxAttempts caps retries before dead-lettering. A value <= 0 uses
	// DefaultMaxAttempts.
	MaxAttempts int
	// RunAfter delays first execution until the given time. Nil runs immediately.
	RunAfter *time.Time
}
