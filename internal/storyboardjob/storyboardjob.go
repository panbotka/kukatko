// Package storyboardjob is the worker handler and read service for video
// storyboards: the scrub-preview sprite the player shows next to the cursor while
// the timeline is hovered.
//
// Generation is lazy and on demand. Nothing enqueues a storyboard for the whole
// library — a sprite costs one full decode of its clip, and most videos are never
// watched. Instead the player asks for the storyboard when playback starts;
// Status answers "pending" and schedules the job the first time, "ready" with the
// layout once the sprite exists. The queue's dedup index keeps at most one active
// job per photo, so asking repeatedly (a reload, a second viewer, a poll while the
// job runs) schedules nothing extra.
//
// Everything degrades rather than fails. A still image, a live photo, a clip with
// no known duration and a host with no ffmpeg all answer "unavailable", and the
// player simply shows no preview. Nothing here writes to the catalogue: the sprite
// is derived media, regenerable from the original.
package storyboardjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/video"
)

// ErrMissingPhotoUID indicates a storyboard job payload carried no photo uid, a
// permanent error so the job dead-letters rather than retrying forever.
var ErrMissingPhotoUID = errors.New("storyboardjob: job payload missing photo_uid")

// ErrNotAVideo indicates the photo has no clip a storyboard could be built from —
// a still image, or a live photo (whose motion clip is a hover preview, never
// played on a scrubbable timeline). It is a permanent condition, not a failure to
// retry.
var ErrNotAVideo = errors.New("storyboardjob: photo has no scrubbable video")

// PhotoStore is the subset of the photo catalogue this package needs: loading one
// photo. It is satisfied by *photos.Store.
type PhotoStore interface {
	// GetByUID returns the photo identified by uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// Generator renders and reads storyboard sprites. It is satisfied by
// *storyboard.Generator; tests supply a fake so no ffmpeg is needed.
type Generator interface {
	// Exists reports whether the sprite for the file hash is already cached.
	Exists(hash string) (bool, error)
	// Open returns a reader over the cached sprite, or storyboard.ErrNotGenerated.
	Open(hash string) (io.ReadCloser, error)
	// Generate renders the sprite for the video stored at srcRelPath, keyed by
	// hash and laid out by spec. It is a no-op when the sprite already exists.
	Generate(ctx context.Context, hash, srcRelPath string, spec storyboard.Spec) error
}

// Enqueuer schedules storyboard jobs. It is satisfied by *jobs.Enqueuer.
type Enqueuer interface {
	// EnqueueStoryboard schedules sprite generation for photoUID, treating an
	// existing active job as a no-op so repeated requests never pile up.
	EnqueueStoryboard(ctx context.Context, photoUID string) error
}

// Config bundles the collaborators a Service needs. Photos and Generator are
// required; Enqueuer is optional — without it Status reports an unbuilt sprite as
// unavailable rather than scheduling it, which is what a read-only wiring wants.
type Config struct {
	// Photos is the catalogue repository.
	Photos PhotoStore
	// Generator renders and reads sprites.
	Generator Generator
	// Enqueuer schedules the lazy generation (optional).
	Enqueuer Enqueuer
	// FFmpegAvailable reports whether the tool that renders sprites is installed.
	// Nil asks internal/video, which is what production wants; tests pin it so the
	// scheduling decision does not depend on the machine running them.
	FFmpegAvailable func() bool
}

// Service answers "what does this video's storyboard look like" and runs the job
// that produces it. It is safe for concurrent use.
type Service struct {
	photos    PhotoStore
	generator Generator
	enqueuer  Enqueuer
	ffmpeg    func() bool
}

// New returns a Service from cfg. It panics when Photos or Generator is nil,
// since neither the handler nor the read path can run without them.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Generator == nil {
		panic("storyboardjob: Photos and Generator are required")
	}
	ffmpeg := cfg.FFmpegAvailable
	if ffmpeg == nil {
		ffmpeg = video.FFmpegAvailable
	}
	return &Service{
		photos:    cfg.Photos,
		generator: cfg.Generator,
		enqueuer:  cfg.Enqueuer,
		ffmpeg:    ffmpeg,
	}
}

// State is what a caller can do with a photo's storyboard right now.
type State string

const (
	// StateReady means the sprite is cached and Spec describes it.
	StateReady State = "ready"
	// StatePending means the sprite is being (or has just been) scheduled. The
	// caller may ask again later; there is nothing to show yet.
	StatePending State = "pending"
	// StateUnavailable means this photo will never have a storyboard: it is not a
	// scrubbable video, or its duration is unknown. The caller should stop asking.
	StateUnavailable State = "unavailable"
)

// Status is the answer to "is there a scrub preview for this photo": the state
// plus, when ready, the sprite's layout.
type Status struct {
	// State is ready, pending or unavailable.
	State State
	// Spec is the sprite layout; the zero value unless State is StateReady.
	Spec storyboard.Spec
}

// Status reports whether the photo's storyboard sprite is ready and, when it is
// not yet but could be, schedules its generation. It never blocks on ffmpeg: the
// rendering happens in the queue.
//
// A photo that is not a scrubbable video, or whose duration the catalogue does not
// know, is StateUnavailable — a permanent answer the client can cache for the
// session. Anything else is StatePending until the sprite lands, then StateReady
// with the layout. A missing photo returns photos.ErrPhotoNotFound.
func (s *Service) Status(ctx context.Context, photoUID string) (Status, error) {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return Status{}, fmt.Errorf("storyboardjob: loading photo %s: %w", photoUID, err)
	}
	spec, err := planFor(photo)
	if err != nil {
		// Not a scrubbable video, or no known duration: a permanent, expected
		// answer rather than a failure — the player just shows no preview.
		return Status{State: StateUnavailable}, nil //nolint:nilerr // the "why" is not actionable for the client.
	}
	ready, err := s.generator.Exists(photo.FileHash)
	if err != nil {
		return Status{}, fmt.Errorf("storyboardjob: checking sprite for %s: %w", photoUID, err)
	}
	if ready {
		return Status{State: StateReady, Spec: spec}, nil
	}
	if s.enqueuer == nil || !s.ffmpeg() {
		// Nothing can produce the sprite here, so saying "pending" would invite the
		// client to poll a promise this instance cannot keep.
		return Status{State: StateUnavailable}, nil
	}
	if err := s.enqueuer.EnqueueStoryboard(ctx, photoUID); err != nil {
		return Status{}, fmt.Errorf("storyboardjob: scheduling storyboard for %s: %w", photoUID, err)
	}
	return Status{State: StatePending}, nil
}

// Open returns a reader over the photo's generated sprite together with its
// layout. The caller owns the reader and must close it. It returns
// storyboard.ErrNotGenerated when the sprite is not cached (the client then shows
// no preview), ErrNotAVideo or storyboard.ErrNoDuration when the photo can never
// have one, and photos.ErrPhotoNotFound for an unknown photo.
func (s *Service) Open(ctx context.Context, photoUID string) (io.ReadCloser, storyboard.Spec, error) {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return nil, storyboard.Spec{}, fmt.Errorf("storyboardjob: loading photo %s: %w", photoUID, err)
	}
	spec, err := planFor(photo)
	if err != nil {
		return nil, storyboard.Spec{}, err
	}
	reader, err := s.generator.Open(photo.FileHash)
	if err != nil {
		return nil, storyboard.Spec{}, fmt.Errorf("storyboardjob: opening sprite for %s: %w", photoUID, err)
	}
	return reader, spec, nil
}

// FileHash returns the content hash the photo's sprite is keyed by, so a caller
// that serves the bytes can build a strong ETag without a second catalogue read.
// It returns photos.ErrPhotoNotFound for an unknown photo.
func (s *Service) FileHash(ctx context.Context, photoUID string) (string, error) {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return "", fmt.Errorf("storyboardjob: loading photo %s: %w", photoUID, err)
	}
	return photo.FileHash, nil
}

// jobPayload is the JSON shape of a storyboard job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
}

// Handle is the worker.HandlerFunc for storyboard jobs: it decodes the photo uid
// from the payload and renders the sprite. A malformed or empty payload is a
// permanent error, so the job dead-letters rather than retrying something that can
// never succeed.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var payload jobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("storyboardjob: decoding payload: %w", err)
	}
	if payload.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	return s.Generate(ctx, payload.PhotoUID)
}

// Generate renders the storyboard sprite for one photo, skipping the work when it
// is already cached. A photo that can never have one (not a scrubbable video, no
// known duration) is a no-op rather than an error: the job was scheduled from a
// state that has since changed, and failing it would only dead-letter noise.
func (s *Service) Generate(ctx context.Context, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("storyboardjob: loading photo %s: %w", photoUID, err)
	}
	spec, err := planFor(photo)
	if err != nil {
		// The job was scheduled from a state that no longer holds (or never did).
		// Dead-lettering it would only add noise; there is simply nothing to render.
		return nil //nolint:nilerr // a photo with no plannable clip has nothing to generate.
	}
	if err := s.generator.Generate(ctx, photo.FileHash, photo.FilePath, spec); err != nil {
		return fmt.Errorf("storyboardjob: generating storyboard for %s: %w", photoUID, err)
	}
	return nil
}

// planFor computes the sprite layout for a photo, or reports why it can have
// none: ErrNotAVideo for anything but a standalone video clip, and (wrapped)
// storyboard.ErrNoDuration when the catalogue does not know how long it is.
//
// Live photos are deliberately excluded. Their motion clip is a one-to-three
// second hover preview with no scrubbable timeline, so a storyboard would be a
// full decode spent on a control that is never shown.
func planFor(photo photos.Photo) (storyboard.Spec, error) {
	if photo.MediaType != photos.MediaVideo {
		return storyboard.Spec{}, fmt.Errorf("%w: %s is %s", ErrNotAVideo, photo.UID, photo.MediaType)
	}
	duration := 0
	if photo.DurationMs != nil {
		duration = *photo.DurationMs
	}
	spec, err := storyboard.Plan(duration, photo.FileWidth, photo.FileHeight)
	if err != nil {
		return storyboard.Spec{}, fmt.Errorf("storyboardjob: planning %s: %w", photo.UID, err)
	}
	return spec, nil
}
