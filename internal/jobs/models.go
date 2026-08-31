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
	// ErrNoActiveJob indicates no queued or running job exists for the (type,
	// photo_uid) an upgrade was asked to rewrite — the colliding job finished
	// between the enqueue that hit the dedup index and the upgrade that followed
	// it. The caller retries the insert rather than reporting a collision that is
	// no longer there.
	ErrNoActiveJob = errors.New("jobs: no active job for this type and photo")
	// ErrEnqueueRaced indicates a forced enqueue neither inserted its job nor found
	// the job it collided with, repeatedly: every insert lost to the dedup index
	// and every upgrade found the collision already gone. It takes a job finishing
	// inside that window several times in a row, so it reports a queue thrashing
	// rather than any state the caller can act on.
	ErrEnqueueRaced = errors.New("jobs: forced enqueue kept racing an active job")
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
	// TypeImageEmbed computes the image embedding for a photo.
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
	// TypeOCR reads the text printed in a photo (a sign, a shop front, a scanned
	// page) and stores it so search can find the photo by what it says. Like
	// TypeImageEmbed it calls the embeddings sidecar on the GPU box, and it runs
	// for stills only — never for a video's poster frame. See internal/ocrjob.
	TypeOCR = "ocr"
	// TypeSidecar writes a photo's metadata sidecar — the YAML file next to the
	// originals that holds its metadata and curation, so the catalogue can be
	// rebuilt from storage alone. It runs locally. See internal/sidecarexport.
	TypeSidecar = "sidecar"
	// TypeStoryboard renders a video's scrub-preview sprite — the grid of frames
	// the player shows next to the cursor while its timeline is hovered. It runs
	// locally (one ffmpeg pass over the clip) and is enqueued lazily, on the first
	// playback of a video, never for the library at large. See
	// internal/storyboardjob.
	TypeStoryboard = "storyboard"
	// TypeBackup runs a backup.
	TypeBackup = "backup"
	// TypeMailSend delivers one transactional e-mail — a registration was
	// received, an account was approved, somebody forgot their password. The
	// payload names the template and carries its data, so the message is rendered
	// when it is sent rather than when it is scheduled and a queued mail survives
	// a restart. It runs locally in the sense that Kukátko owns the process, but
	// it talks to a remote SMTP server, which is exactly why it is queued: a mail
	// host that is briefly unreachable delays the message instead of losing it.
	// See internal/mailjob.
	TypeMailSend = "mail_send"
	// TypeNamelessDetach detaches one nameless catch-all subject: the subject row
	// is deleted and every marker and cached face that pointed at it is left
	// unassigned. It runs locally, in the queue rather than in the HTTP request,
	// because clearing a five-figure number of faces moves all of them into the
	// partial "unassigned faces" HNSW index and takes minutes. See
	// internal/namelessjob.
	TypeNamelessDetach = "nameless_detach"
	// TypeNamelessRestore replays one snapshot from a nameless-subject undo file:
	// the subject is re-created under its original uid and the markers and faces
	// it owned are re-assigned to it. It is the undo of TypeNamelessDetach and
	// runs in the queue for the same reason.
	TypeNamelessRestore = "nameless_restore"
)

// ForceOutcome says what a forced enqueue did to the queue. A forced job carries
// `"force": true` beside the `photo_uid` the dedup index keys on, so it dedupes
// against the job already covering that photo exactly as a plain one would — and
// what that collision means depends on the job it collided with.
type ForceOutcome string

// The outcomes of a forced enqueue.
const (
	// ForceScheduled means nothing was covering the photo, so the forced job was
	// inserted.
	ForceScheduled ForceOutcome = "scheduled"
	// ForceUpgraded means a *queued plain* job was covering the photo and its
	// payload was rewritten to the forced one. The job that was going to take its
	// idempotent skip now redoes the work, and there is still exactly one job.
	ForceUpgraded ForceOutcome = "upgraded"
	// ForceAbsorbed means the job covering the photo is itself forced, so the
	// request collapsed into it. That is the correct no-op: the work will be redone.
	ForceAbsorbed ForceOutcome = "absorbed"
	// ForceInFlight means a job for the same photo and type is already running. It
	// read its payload when it was claimed, so it cannot be upgraded and the force
	// has not been scheduled at all — the caller must say so and be retried once
	// that run finishes.
	ForceInFlight ForceOutcome = "in_flight"
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
