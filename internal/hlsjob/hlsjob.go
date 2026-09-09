// Package hlsjob is the worker handler that turns an uploaded video into
// something a browser can stream: it runs ffmpeg over the original, publishes the
// resulting fragmented-MP4 segments to the object store under
// hls/<file_hash>/<rendition>/ and records what a player's playlists must
// advertise in photo_hls_renditions.
//
// It owns none of those decisions. The object layout, the encoding plan and the
// playlists themselves are internal/hls, which is pure; this package is the part
// that acts — it runs the process, moves the bytes and writes the row. That split
// is what lets the whole plan be tested on a machine with no ffmpeg while the
// acting half is exercised end to end where there is one.
//
// One job encodes one photo into every configured rendition. A re-run replaces
// what it finds rather than adding to it: the segments go back to the same keys,
// the row is upserted on (photo, rendition), and objects the previous encode left
// behind that this one did not produce are swept. A run that fails part-way
// leaves neither a row nor the objects it had already uploaded, so the queue's
// retry starts from the state it would have found had the run never happened.
//
// The encode is by far the most expensive job in the queue, which is why it has a
// one-slot pool of its own (internal/worker) and a timeout that scales with the
// clip rather than a flat constant: a two-hour family video is expected to take
// hours, and a deadline that assumed otherwise would simply never let it finish.
package hlsjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

// ErrMissingPhotoUID indicates an hls_transcode payload carried no photo uid. It
// is permanent, so the job dead-letters rather than retrying something that can
// never succeed.
var ErrMissingPhotoUID = errors.New("hlsjob: job payload missing photo_uid")

// ErrBackfillUnavailable indicates BackfillHLS was called on a Service built
// without the backfill collaborators (a Lister and an Enqueuer). A Service that
// only runs the worker handler omits them; the one behind POST /process/hls is
// wired with both.
var ErrBackfillUnavailable = errors.New("hlsjob: HLS backfill not configured")

// ErrNoRenditions indicates a Service was configured with an empty encoding
// plan. Nothing would be produced, so it is a wiring mistake rather than a
// switched-off feature — which is expressed by registering no handler at all.
var ErrNoRenditions = errors.New("hlsjob: no renditions configured")

// PhotoStore is the subset of the photo catalogue this package needs: loading one
// photo. It is satisfied by *photos.Store.
type PhotoStore interface {
	// GetByUID returns the photo identified by uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
}

// ObjectStore is the subset of the original store this package needs: a local
// copy of the video to feed ffmpeg, and the publication and removal of the
// objects the encode produces. It is satisfied by storage.Storage.
//
// A store that also implements storage.PrefixLister lets the job see what a
// previous encode of the same rendition left behind, which is what makes a
// re-encode replace it exactly — see existingKeys. One that does not still
// works; it simply cannot notice objects nobody references any more.
type ObjectStore interface {
	// Materialize yields a real local file for relPath, since ffmpeg takes a
	// filename. The caller must always call cleanup.
	Materialize(ctx context.Context, relPath string) (path string, cleanup func(), err error)
	// Put streams src into the store at file.RelPath, verifying it against the
	// declared identity.
	Put(ctx context.Context, src io.Reader, file storage.StoredFile) error
	// Delete removes the object at relPath, reporting an os.ErrNotExist-wrapping
	// error when there is nothing there.
	Delete(ctx context.Context, relPath string) error
}

// PhotoLister enumerates the videos an HLS backfill should schedule. It is
// satisfied by *photos.Store and is optional: only the Service behind
// POST /process/hls needs it.
type PhotoLister interface {
	// ListVideosMissingHLS returns the uids of non-archived videos with no
	// recorded rendition (limit <= 0 returns all).
	ListVideosMissingHLS(ctx context.Context, limit int) ([]string, error)
	// ListActiveVideoUIDs returns the uids of every non-archived video, for a
	// forced full re-encode.
	ListActiveVideoUIDs(ctx context.Context) ([]string, error)
}

// Enqueuer schedules `hls_transcode` jobs for the backfill. It is satisfied by
// *jobs.Enqueuer and is optional, like PhotoLister.
type Enqueuer interface {
	// EnqueueHLSTranscode schedules the streaming encode of photoUID, treating an
	// existing active job as a no-op so repeated backfills do not pile up.
	EnqueueHLSTranscode(ctx context.Context, photoUID string) error
}

// RenditionStore records what an encode produced. It is satisfied by *Store.
type RenditionStore interface {
	// Save upserts one rendition on (photo_uid, rendition) and returns the stored
	// row.
	Save(ctx context.Context, enc Encoded) (Encoded, error)
}

// Config bundles the collaborators and tunables a Service needs. Photos, Objects
// and Renditions are required, and so is a non-empty Plan. Lister and Enqueuer
// are optional and enable the backfill (BackfillHLS) when both are supplied.
type Config struct {
	// Photos is the catalogue repository.
	Photos PhotoStore
	// Objects publishes the segments and materializes the original.
	Objects ObjectStore
	// Renditions records what was published.
	Renditions RenditionStore
	// Lister enumerates the videos the backfill schedules (optional).
	Lister PhotoLister
	// Enqueuer schedules the backfill's jobs (optional).
	Enqueuer Enqueuer
	// Plan is the ordered list of renditions every video is encoded into. It is
	// resolved from configuration by the caller, so an instance can produce fewer
	// qualities than the encoder knows about without this package reading config.
	Plan []hls.Rendition
	// SegmentSeconds is the length segments are cut to; <= 0 means
	// hls.DefaultSegmentSeconds.
	SegmentSeconds int
	// TempDir is where ffmpeg's output directory is created; "" uses the OS temp
	// directory. A rendition of a long video is gigabytes, so an instance whose
	// /tmp is a small tmpfs needs this pointed somewhere with room.
	TempDir string
	// FFmpegAvailable reports whether the encoder is installed. Nil asks
	// internal/video, which is what production wants; tests pin it so a decision
	// under test does not depend on the machine running it.
	FFmpegAvailable func() bool
}

// Service runs the hls_transcode job. It is safe for concurrent use, though the
// worker gives it a single slot on purpose.
type Service struct {
	photos     PhotoStore
	objects    ObjectStore
	renditions RenditionStore
	lister     PhotoLister
	enqueuer   Enqueuer
	plan       []hls.Rendition
	segment    int
	tempDir    string
	ffmpeg     func() bool
}

// New returns a Service from cfg. It panics when Photos, Objects or Renditions is
// nil, since the handler cannot run without any of them; an empty Plan is
// reported by Transcode as ErrNoRenditions rather than panicking, because it can
// come from a configuration file.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Objects == nil || cfg.Renditions == nil {
		panic("hlsjob: Photos, Objects and Renditions are required")
	}
	ffmpeg := cfg.FFmpegAvailable
	if ffmpeg == nil {
		ffmpeg = video.FFmpegAvailable
	}
	return &Service{
		photos:     cfg.Photos,
		objects:    cfg.Objects,
		renditions: cfg.Renditions,
		lister:     cfg.Lister,
		enqueuer:   cfg.Enqueuer,
		plan:       cfg.Plan,
		segment:    hls.SegmentLength(cfg.SegmentSeconds),
		tempDir:    cfg.TempDir,
		ffmpeg:     ffmpeg,
	}
}

// jobPayload is the JSON shape of an hls_transcode job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
}

// Handle is the worker.HandlerFunc for hls_transcode jobs: it decodes the photo
// uid from the payload and encodes that video. A malformed or empty payload is a
// permanent error, so the job dead-letters instead of retrying forever.
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var payload jobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("hlsjob: decoding payload: %w", err)
	}
	if payload.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	return s.Transcode(ctx, payload.PhotoUID)
}

// Transcode encodes one photo into every configured rendition and records each
// of them.
//
// A photo that is not a standalone video is a no-op rather than an error: the job
// was scheduled from a state that has since changed (or never held), and
// dead-lettering it would only add noise. Live photos are excluded with the
// stills — their motion clip is a one-to-three second hover preview, not
// something anybody streams.
//
// It returns a wrapped video.ErrFFmpegMissing on a host with no encoder,
// photos.ErrPhotoNotFound for an unknown photo, ErrNoRenditions when nothing is
// configured to be produced, and whatever the encode, the upload or the write
// failed with — in every case leaving neither a row nor an object of the
// rendition that failed. Renditions already finished by the same run stay: each
// is independently valid, and the retry redoes only what is missing.
func (s *Service) Transcode(ctx context.Context, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("hlsjob: loading photo %s: %w", photoUID, err)
	}
	if photo.MediaType != photos.MediaVideo {
		return nil
	}
	if len(s.plan) == 0 {
		return ErrNoRenditions
	}
	if !s.ffmpeg() {
		return fmt.Errorf("hlsjob: encoding %s: %w", photoUID, video.ErrFFmpegMissing)
	}
	srcPath, cleanup, err := s.objects.Materialize(ctx, photo.FilePath)
	defer cleanup()
	if err != nil {
		return fmt.Errorf("hlsjob: materializing %s: %w", photo.FilePath, err)
	}
	for _, rendition := range s.plan {
		if err := s.encodeOne(ctx, photo, srcPath, rendition); err != nil {
			return err
		}
	}
	return nil
}

const (
	// encodeTimeoutBase is the fixed part of one rendition's deadline: the
	// process start, the probe of the source and the segment uploads, none of
	// which scale with the clip.
	encodeTimeoutBase = 5 * time.Minute
	// encodeTimeoutFactor multiplies the clip's own length. Ten times realtime is
	// far more than libx264 at preset medium needs on any machine that would be
	// asked to do this, which is the point: the deadline exists to stop a wedged
	// process, not to decide how long an encode may reasonably take.
	encodeTimeoutFactor = 10
	// encodeTimeoutUnknown is the deadline for a clip whose length the catalogue
	// does not know — an unprobed container, a video ingested before durations
	// were recorded. It has to be generous for the same reason the factor is.
	encodeTimeoutUnknown = 4 * time.Hour
)

// encodeTimeout returns how long one rendition's ffmpeg run may take for a clip
// of the given length, nil meaning the length is unknown.
//
// It scales with the clip because the alternative does not work: a constant
// generous enough for a two-hour video is no protection at all for a ten-second
// one, and a constant tight enough for the short clip kills every long one at the
// same point every time — which looks exactly like a video that "cannot be
// transcoded" while being nothing of the sort.
func encodeTimeout(durationMs *int) time.Duration {
	if durationMs == nil || *durationMs <= 0 {
		return encodeTimeoutUnknown
	}
	return encodeTimeoutBase + encodeTimeoutFactor*time.Duration(*durationMs)*time.Millisecond
}
