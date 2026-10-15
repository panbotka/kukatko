// Package rekey moves one project's objects in the store from the root layout
// (storage prefix "") under the project's own prefix, projects/<uid>/, one photo
// at a time while the application keeps running.
//
// # The safety rule
//
// For every photo, in one order and no other: copy the original to its new key
// (server-side where the backend can), verify the copy — its size, and either
// its ETag against the source's content ETag or its SHA-256 against
// photos.file_hash — move the row (photos.file_path and photo_files) in one
// transaction, copy the photo's derived objects (thumbnails, streaming segments,
// the metadata sidecar), and only then delete the old original and the old
// derived objects. The old original is never deleted before a committed row
// points at a verified copy, so there is no instant at which a photo's bytes
// exist only in a place nothing answers for.
//
// # Resuming
//
// Every step is idempotent. A crash before the row moves leaves both copies and
// a row pointing at the old one; the next pass copies again (or finds the copy),
// verifies it and carries on. A crash after the row moves leaves both copies and
// a row pointing at the new one; the transaction that moved the row also named
// the photo in the run's pending_* columns, so the next step verifies the new
// copy again and finishes the deletion. The run's last_key is the file_path
// cursor of the pass, and a photo that fails is recorded in the run's error ring
// and skipped, so one unreadable file never blocks the library.
//
// # Derived objects that did not move
//
// A thumbnail or streaming segment that cannot be copied after the row moved
// does not undo the photo: its original is safe under the new prefix either way.
// The photo is recorded in storage_rekey_derived_failures with the kinds still at
// the root, and none of its root thumbnails or segments are deleted. Once the
// pass has no photo left in the old layout, every further step re-plans the
// derived objects of the recorded photos (derived.go): it copies what the new
// prefix lacks, deletes the root copies only once the prefixed ones exist, and
// forgets the photo on success. Finish first sweeps the root for derived objects
// of moved photos nobody recorded, then ends the run done only when no record of
// the project is left — failed with ErrDerivedRemain otherwise, for Retry.
//
// # Shape
//
// Plan is pure: it lists the keys one photo occupies and where each must go.
// Service owns the storage_rekey_runs table and the work: Start switches the
// project's prefix — new uploads land in the new layout at once — and records
// the run; Step is the unit of work a worker job calls repeatedly; Finish closes
// the run once no photo is left in the old layout. Pause, Resume and Retry move
// the run between its states.
package rekey

import (
	"errors"
	"time"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// State is where a run is in its life.
type State string

// The states of a run, matching the CHECK constraint of storage_rekey_runs.
const (
	// StateRunning is a run the worker steps.
	StateRunning State = "running"
	// StatePaused is a run an operator stopped; Resume puts it back to running.
	StatePaused State = "paused"
	// StateDone is a finished run: no photo of the project is left in the old
	// layout.
	StateDone State = "done"
	// StateFailed is a run that met too many errors, or finished its pass with
	// photos it could not move; Retry puts it back to running.
	StateFailed State = "failed"
)

// Limits of a run's error bookkeeping.
const (
	// MaxErrorRing is how many of the most recent errors a run keeps.
	MaxErrorRing = 50
	// MaxErrors is how many errors a run tolerates; one more flips it to failed.
	MaxErrors = 100
	// DefaultBatchSize is how many photos Step takes when asked for none.
	DefaultBatchSize = 10
)

// Sentinel errors, matchable with errors.Is.
var (
	// ErrProjectNotFound is returned by Start for an unknown project.
	ErrProjectNotFound = errors.New("rekey: project not found")
	// ErrAlreadyPrefixed is returned by Start for a project that already has a
	// storage prefix of its own and was never moved — a created project, whose
	// objects were always in their own tree.
	ErrAlreadyPrefixed = errors.New("rekey: project already has its own storage prefix")
	// ErrRunActive is returned by Start when the project already has a running
	// or paused run.
	ErrRunActive = errors.New("rekey: a storage move of this project is already running or paused")
	// ErrNoRun is returned when the project has never had a storage move.
	ErrNoRun = errors.New("rekey: no storage move for this project")
	// ErrInvalidState is returned by Pause, Resume and Retry when the run is not
	// in the state the transition starts from.
	ErrInvalidState = errors.New("rekey: the storage move is not in a state that allows this")
	// ErrNotRunning is returned by Step and Finish for a run that is not running.
	ErrNotRunning = errors.New("rekey: the storage move is not running")
	// ErrRunFailed is returned by Step when the error it just recorded tipped
	// the run over MaxErrors and the run is now failed.
	ErrRunFailed = errors.New("rekey: the storage move failed")
	// ErrPhotosRemain is returned by Finish when photos are still in the old
	// layout after a pass that recorded errors — the ones it could not move,
	// and any it never saw. The run is failed; Retry starts another pass over
	// them.
	ErrPhotosRemain = errors.New("rekey: photos are still in the old layout")
	// ErrPendingCleanup is returned by Finish while work for Step is left — a
	// crashed step's pending photo, derived objects its root sweep just found
	// and recorded, or photos in the old layout an error-free pass never saw
	// (uploads in flight at Start), for which it rewound the pass; Step
	// finishes that first.
	ErrPendingCleanup = errors.New("rekey: a moved photo's old objects are not deleted yet")
	// ErrDerivedRemain is returned by Finish when moved photos still have
	// thumbnails or streaming segments at the root that the pass could not move
	// (Run.DerivedFailed). The run is failed; Retry tries them again.
	ErrDerivedRemain = errors.New("rekey: derived objects are still in the old layout")
	// ErrVerifyFailed is the per-photo error of a copy that does not hold the
	// bytes the catalogue promises. The old original is kept.
	ErrVerifyFailed = errors.New("rekey: the copy does not match the original")
	// ErrPhotoChanged is the per-photo error of a row whose file_path changed
	// (or which vanished) while it was being moved.
	ErrPhotoChanged = errors.New("rekey: the photo changed during the move")
)

// RunError is one error a run met: the key it was working on, what went wrong
// and when.
type RunError struct {
	// Key is the object key (usually the original's old file_path).
	Key string `json:"key"`
	// Message is the error text.
	Message string `json:"message"`
	// At is when it happened.
	At time.Time `json:"at"`
}

// Run is one storage move of one project: a row of storage_rekey_runs.
type Run struct {
	// UID identifies the run ("rk…").
	UID string `json:"uid"`
	// ProjectUID is the project being moved.
	ProjectUID string `json:"project_uid"`
	// State is where the run is.
	State State `json:"state"`
	// TargetPrefix is the storage prefix the objects move under:
	// projects/<project uid>/.
	TargetPrefix string `json:"target_prefix"`
	// TotalPhotos and TotalBytes are what was in the old layout when the run
	// started: the photo count and the summed size of their originals.
	TotalPhotos int64 `json:"total_photos"`
	TotalBytes  int64 `json:"total_bytes"`
	// DonePhotos and DoneBytes are what has moved so far.
	DonePhotos int64 `json:"done_photos"`
	DoneBytes  int64 `json:"done_bytes"`
	// LastKey is the old file_path of the last photo the pass finished: the
	// cursor a resumed run continues after.
	LastKey string `json:"last_key"`
	// PendingPhotoUID and PendingOldKey name the photo whose row already points
	// at its new key but whose old objects are not deleted yet, or are empty.
	PendingPhotoUID string `json:"pending_photo_uid,omitempty"`
	PendingOldKey   string `json:"pending_old_key,omitempty"`
	// Errors are the most recent errors, oldest first, at most MaxErrorRing.
	Errors []RunError `json:"errors"`
	// ErrorCount counts every error the run ever met.
	ErrorCount int `json:"error_count"`
	// DerivedFailed counts the project's moved photos whose thumbnails or
	// streaming segments are still at the root, waiting for a retry: the rows
	// of storage_rekey_derived_failures. A run is done only when it is zero.
	DerivedFailed int64 `json:"derived_failed"`
	// DerivedCursor is the photo uid the retry of derived objects continues
	// after within a pass; Resume and Retry reset it.
	DerivedCursor string `json:"-"`
	// StartedAt, UpdatedAt and FinishedAt are the run's timestamps; FinishedAt
	// is nil until the run is done.
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Rendition is one recorded HLS rendition of a video, as the plan needs it.
type Rendition struct {
	// Name is the rendition's name ("1080p"), a path segment of its keys.
	Name string
	// Playlist is the media playlist ffmpeg wrote; it names every object.
	Playlist string
	// SegmentCount is how many media segments were published.
	SegmentCount int
}

// PhotoFiles is what the plan needs to know about one photo: where its original
// is, the content hash its derived keys are built from, and the streaming
// renditions recorded for it.
type PhotoFiles struct {
	// UID identifies the photo.
	UID string
	// FilePath is the original's key.
	FilePath string
	// FileHash is the original's SHA-256, hex.
	FileHash string
	// FileSize is the original's size in bytes.
	FileSize int64
	// Renditions are the photo's recorded HLS renditions (videos only).
	Renditions []Rendition
}

// Move is one object to re-key: its current key, its key under the new prefix,
// and the kind of object it is.
type Move struct {
	From string
	// To is the key under the new prefix. Empty only for a drop: a root
	// thumbnail or streaming object no layout builds any more, which the final
	// sweep's retry deletes rather than moves (see rootDerivedMove).
	To   string
	Kind storekeys.Kind
}
