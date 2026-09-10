// Package metajob is the worker handler for `metadata` jobs: it re-reads a
// photo's original file and fills the catalogue columns the file itself is the
// authority on — the IPTC/XMP credit fields (subject, keywords, artist, copyright,
// licence) and the file-technical ones (software, colour profile, image codec,
// camera serial, projection, original name).
//
// The upload pipeline already extracts all of this as it catalogues a photo, so
// this package exists for the photos that came in before it did: rows imported
// from PhotoPrism or migrated from photo-sorter, and everything ingested before
// the extractor learned to read these tags. Their originals are still in storage,
// so the metadata is still there to be read — it just never was.
//
// A video is re-read twice over: the columns above come out of its tags like any
// other file's, and its container is probed again for the fields only a probe can
// know — duration, frame size, frame rate, video and audio codec, whether it has
// sound. That second half exists because those fields were read exactly once, while
// the clip was being ingested, and a probe that failed there left them empty
// forever: a video with no duration can never get a scrub preview, and nothing
// else in the app ever asks its file again. See videorepair.go for what a re-probe
// may write and what it may not.
//
// The handler is a gap-filler, never a rewriter. It writes only into columns that
// are still empty, so an extraction that comes back blank can never erase a value
// the user typed, and it touches nothing else at all: the captions, the capture
// time, the GPS fix, the ratings and the albums are outside its reach. That makes
// it idempotent — a second run over the same photo changes nothing — and makes the
// backfill safe to re-run at any point. A video's file-derived technical fields are
// the one exception, and a deliberate one: nothing but a probe writes them, so a
// probe is allowed to correct a wrong one — while the two fields a person can own,
// the capture time and the location, stay gap-fill only.
package metajob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// ErrMissingPhotoUID indicates a metadata job payload carried no photo uid, a
// permanent error so the job dead-letters rather than retrying forever.
var ErrMissingPhotoUID = errors.New("metajob: job payload missing photo_uid")

// ErrBackfillUnavailable indicates BackfillMetadata was called on a Service built
// without the backfill collaborators (a PhotoLister and an Enqueuer). A Service
// that only runs the worker handler omits them; the one behind
// POST /process/metadata is wired with both.
var ErrBackfillUnavailable = errors.New("metajob: metadata backfill not configured")

// PhotoStore is the subset of the photo catalogue the handler needs: loading the
// photo and filling its still-empty metadata columns. It is satisfied by
// *photos.Store.
type PhotoStore interface {
	// GetByUID returns the photo identified by uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// FillFileMetadata writes m into the photo's empty metadata columns — never over
	// a value already there — stamps the extraction marker, and reports whether any
	// column was filled.
	FillFileMetadata(ctx context.Context, uid string, m photos.FileMetadata) (bool, error)
	// RepairVideoMetadata applies a re-probe's decision about a video's own fields
	// (see planVideoRepair) and reports whether a row was written. An empty repair
	// writes nothing.
	RepairVideoMetadata(ctx context.Context, uid string, r photos.VideoRepair) (bool, error)
}

// Extractor reads a photo's stored original and returns what its own metadata
// says. It is satisfied by *StorageExtractor, which streams the original through
// the storage layer (so it works for both a local FS and R2) and hands it to
// internal/exif; tests supply a fake to avoid touching storage.
type Extractor interface {
	// ExtractOriginal materialises the photo's original and extracts its metadata.
	// A missing original is reported as an error wrapping os.ErrNotExist.
	ExtractOriginal(ctx context.Context, photo photos.Photo) (exif.Metadata, error)
	// ProbeVideo materialises the photo's original and probes it as a video
	// container, which is the only way to learn a clip's duration, frame rate and
	// codecs. A missing original is reported as an error wrapping os.ErrNotExist;
	// a host with no probing tool as one wrapping video.ErrFFprobeMissing.
	ProbeVideo(ctx context.Context, photo photos.Photo) (video.Metadata, error)
}

// PhotoLister enumerates the photos a metadata backfill should schedule. It is
// satisfied by *photos.Store and is optional (only the Service behind
// POST /process/metadata needs it).
type PhotoLister interface {
	// ListPhotosMissingFileMetadata returns the uids of non-archived photos whose
	// original has never been read out into the metadata columns (limit <= 0 returns
	// all).
	ListPhotosMissingFileMetadata(ctx context.Context, limit int) ([]string, error)
	// ListActiveUIDs returns the uids of every non-archived photo, for a forced full
	// re-run that re-reads the whole library.
	ListActiveUIDs(ctx context.Context) ([]string, error)
	// ListVideosMissingTechnicalMetadata returns the uids of non-archived videos
	// whose container was evidently never read out — no duration, no codec, no
	// frame size (limit <= 0 returns all).
	ListVideosMissingTechnicalMetadata(ctx context.Context, limit int) ([]string, error)
}

// Enqueuer schedules metadata jobs for the backfill. It is satisfied by
// jobs.Enqueuer and is optional (only the Service behind POST /process/metadata
// needs it).
type Enqueuer interface {
	// EnqueueMetadata schedules a metadata re-extraction for photoUID, treating an
	// existing active job as a no-op so repeated backfills do not pile up.
	EnqueueMetadata(ctx context.Context, photoUID string) error
}

// Config bundles the collaborators a Service needs. Photos and Extractor are
// required; Lister and Enqueuer are optional and enable the backfill
// (BackfillMetadata) when both are supplied.
type Config struct {
	// Photos is the catalogue repository.
	Photos PhotoStore
	// Extractor reads metadata out of a photo's stored original.
	Extractor Extractor
	// Lister enumerates photos for the metadata backfill (optional).
	Lister PhotoLister
	// Enqueuer schedules metadata backfill jobs (optional).
	Enqueuer Enqueuer
	// Logger records skipped photos; nil uses slog.Default().
	Logger *slog.Logger
}

// Service re-extracts a photo's file metadata. It is safe for concurrent use: the
// storage and catalogue layers tolerate concurrent calls for distinct photos.
type Service struct {
	photos    PhotoStore
	extractor Extractor
	lister    PhotoLister
	enqueuer  Enqueuer
	log       *slog.Logger
}

// New returns a Service from cfg. It panics if Photos or Extractor is nil, since a
// metadata job cannot run without them. Lister and Enqueuer are optional; when both
// are supplied the Service can also drive the metadata backfill.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Extractor == nil {
		panic("metajob: Photos and Extractor are required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		photos:    cfg.Photos,
		extractor: cfg.Extractor,
		lister:    cfg.Lister,
		enqueuer:  cfg.Enqueuer,
		log:       logger,
	}
}

// jobPayload is the JSON shape of a metadata job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
}

// Handle is the worker.HandlerFunc for metadata jobs: it decodes the photo uid
// from the job payload and re-extracts that photo's file metadata. A malformed or
// empty payload is a permanent error (the job dead-letters rather than retrying a
// payload that can never succeed).
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("metajob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	return s.Reextract(ctx, p.PhotoUID)
}

// Reextract re-reads the original of the photo identified by photoUID and fills
// its still-empty metadata columns from what the file says. It reports no error
// when the file carries no metadata worth writing — "we looked and there was
// nothing" is a finished photo, and it is stamped as read so the backfill does not
// come back to it.
//
// A photo whose original is missing from storage is logged and skipped (nil
// error): the file is gone, re-reading it will never succeed, and failing the job
// would only dead-letter it and make a library-wide backfill look broken. Every
// other storage or database failure is returned so the queue retries it.
func (s *Service) Reextract(ctx context.Context, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("metajob: loading photo %s: %w", photoUID, err)
	}
	meta, err := s.extractor.ExtractOriginal(ctx, photo)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.log.WarnContext(ctx, "metadata extraction skipped: original missing",
				slog.String("photo_uid", photo.UID), slog.String("file_path", photo.FilePath))
			return nil
		}
		return fmt.Errorf("metajob: extracting metadata of %s: %w", photoUID, err)
	}
	if _, err := s.photos.FillFileMetadata(ctx, photo.UID, fileMetadata(photo, meta)); err != nil {
		return fmt.Errorf("metajob: storing metadata of %s: %w", photoUID, err)
	}
	if photo.MediaType != photos.MediaVideo {
		return nil
	}
	return s.reprobeVideo(ctx, photo)
}

// reprobeVideo probes a catalogued video's container again and applies whatever
// planVideoRepair concluded may be written. It is how a clip that arrived during a
// broken probe — or before the probe learned to read a field — gets its duration,
// frame size, frame rate and codecs back, and with the duration its scrub preview.
//
// Two failures are skips rather than job failures, for the same reason a missing
// original is: neither can be fixed by trying again, and dead-lettering them would
// only make a library-wide backfill look broken. A gone original is nothing to
// read; a host with no ffprobe and no exiftool cannot read anything at all, and
// failing every video in the library over one missing binary would bury the fact
// in queue noise instead of stating it. Both are logged. Every other probe failure
// is returned so the queue retries it and records it.
func (s *Service) reprobeVideo(ctx context.Context, photo photos.Photo) error {
	vm, err := s.extractor.ProbeVideo(ctx, photo)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			s.log.WarnContext(ctx, "video re-probe skipped: original missing",
				slog.String("photo_uid", photo.UID), slog.String("file_path", photo.FilePath))
		case errors.Is(err, video.ErrFFprobeMissing), errors.Is(err, video.ErrNoMetadataTool):
			s.log.WarnContext(ctx, "video re-probe skipped: no probing tool installed",
				slog.String("photo_uid", photo.UID), slog.String("file_path", photo.FilePath))
		default:
			return fmt.Errorf("metajob: probing video %s: %w", photo.UID, err)
		}
		return nil
	}
	repair := planVideoRepair(photo, vm)
	if repair.Empty() {
		if !vm.HasContainerMetadata() {
			// The probe ran and told us nothing about the container. That is the
			// failure this repair exists for, so it must not pass in silence.
			s.log.WarnContext(ctx, "video probe read no technical metadata",
				slog.String("photo_uid", photo.UID), slog.String("file_path", photo.FilePath))
		}
		return nil
	}
	if _, err := s.photos.RepairVideoMetadata(ctx, photo.UID, repair); err != nil {
		return fmt.Errorf("metajob: repairing video metadata of %s: %w", photo.UID, err)
	}
	s.log.InfoContext(ctx, "video technical metadata repaired",
		slog.String("photo_uid", photo.UID), slog.String("file_path", photo.FilePath))
	return nil
}

// fileMetadata maps an extraction onto the columns the catalogue fills, applying
// the two rules the extractor cannot know about on its own:
//
//   - a video has no image codec — its compression is the ffprobe-derived
//     video_codec, which this job never touches;
//   - original_name is the name the file carried before it was ingested, which the
//     file's own tags do not record. The storage layout keeps a photo under the
//     name it arrived with, so the stored file name is the closest the catalogue
//     can come to it — and it is only ever written when the column is still empty.
func fileMetadata(photo photos.Photo, meta exif.Metadata) photos.FileMetadata {
	m := photos.FileMetadata{
		Subject:      meta.Subject,
		Keywords:     meta.Keywords,
		Artist:       meta.Artist,
		Copyright:    meta.Copyright,
		License:      meta.License,
		Software:     meta.Software,
		CameraSerial: meta.CameraSerial,
		ColorProfile: meta.ColorProfile,
		ImageCodec:   meta.ImageCodec,
		Projection:   meta.Projection,
		OriginalName: photo.FileName,
	}
	if photo.MediaType == photos.MediaVideo {
		m.ImageCodec = ""
	}
	return m
}

// BackfillMetadata enqueues a `metadata` job for every photo whose original has
// never been read out into the metadata columns, returning how many uids it
// scheduled. When all is true it instead schedules every non-archived photo — a
// forced full re-run that re-reads originals the extractor has already seen, which
// is how a library picks up fields a newer extractor learned to read.
//
// It only enqueues jobs; the reading happens later in the local worker, so the
// backfill runs regardless of whether the embeddings box is online. It is
// idempotent and resumable: the queue dedupes an already-active job per photo, and
// each photo is marked as read the moment its job completes, so a run interrupted
// halfway picks up exactly where it stopped and a second run over a drained library
// enqueues nothing. It returns ErrBackfillUnavailable when the Service was built
// without the Lister and Enqueuer collaborators.
func (s *Service) BackfillMetadata(ctx context.Context, all bool) (int, error) {
	if s.lister == nil || s.enqueuer == nil {
		return 0, ErrBackfillUnavailable
	}
	uids, err := s.backfillCandidates(ctx, all)
	if err != nil {
		return 0, err
	}
	return s.enqueueAll(ctx, uids)
}

// BackfillVideoMetadata enqueues a `metadata` job for every non-archived video
// whose container was evidently never read out — it has no duration, no codec name
// or no frame size — and returns how many it scheduled. It is the repair route for
// a library that ingested clips while its probe was broken: those videos have their
// extraction marker stamped, so the ordinary backfill above passes them by forever,
// and photo-by-photo repair through the per-photo `metadata` step does not scale to
// a library.
//
// It has no "all" scope of its own: re-reading every video in the library is what
// BackfillMetadata(all) already does. Like it, it only enqueues — the probing
// happens later in the local worker — and it is idempotent and resumable, because
// the queue dedupes an already-active job per photo and a repaired clip drops out
// of the candidate set. It returns ErrBackfillUnavailable when the Service was
// built without the Lister and Enqueuer collaborators.
func (s *Service) BackfillVideoMetadata(ctx context.Context) (int, error) {
	if s.lister == nil || s.enqueuer == nil {
		return 0, ErrBackfillUnavailable
	}
	uids, err := s.lister.ListVideosMissingTechnicalMetadata(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("metajob: listing videos missing technical metadata: %w", err)
	}
	return s.enqueueAll(ctx, uids)
}

// enqueueAll schedules a `metadata` job for each uid and returns how many it
// scheduled, stopping at the first failure (with the count reached so far, so a
// partial run reports what it did).
func (s *Service) enqueueAll(ctx context.Context, uids []string) (int, error) {
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueMetadata(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("metajob: enqueuing metadata for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// backfillCandidates returns the uids the backfill should schedule: every
// non-archived photo when all is set, otherwise only those whose original has
// never been read.
func (s *Service) backfillCandidates(ctx context.Context, all bool) ([]string, error) {
	if all {
		uids, err := s.lister.ListActiveUIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("metajob: listing active photos: %w", err)
		}
		return uids, nil
	}
	uids, err := s.lister.ListPhotosMissingFileMetadata(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("metajob: listing photos missing metadata: %w", err)
	}
	return uids, nil
}
