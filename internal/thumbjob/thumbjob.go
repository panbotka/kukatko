// Package thumbjob is the worker handler for thumbnail jobs: it regenerates a
// photo's derived data that can be rebuilt from its original — the cached
// thumbnails and, when absent, the perceptual hashes. It is the repair path for
// the library-maintenance scan, which enqueues a thumbnail job per photo whose
// thumbnails or pHash are missing.
//
// The handler is idempotent: thumbnail sizes already on disk are skipped by the
// thumbnailer, and a pHash that already exists is left untouched, so a job can be
// retried or re-enqueued safely. Everything it produces is regenerable from the
// original, so a failure never corrupts the catalogue — at worst the photo stays
// in its degraded state until the job is retried.
package thumbjob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"sort"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/phash"
	"github.com/panbotka/kukatko/internal/photos"
)

// ErrMissingPhotoUID indicates a thumbnail job payload carried no photo uid, a
// permanent error so the job dead-letters rather than retrying forever.
var ErrMissingPhotoUID = errors.New("thumbjob: job payload missing photo_uid")

// ErrRegenerateFailed wraps a failure to rebuild a photo's thumbnails from its
// original because the source cannot be used — the original is missing from
// storage or its bytes cannot be decoded into an image. ForceRegenerate wraps
// such failures with it (via %w) so the on-demand HTTP layer can answer "the
// source cannot be turned into a thumbnail" (422 Unprocessable Entity)
// distinctly from a missing photo (404) or an unexpected server error (500).
var ErrRegenerateFailed = errors.New("thumbjob: thumbnail regeneration failed")

// ErrBackfillUnavailable indicates BackfillThumbnails was called on a Service
// built without the backfill collaborators (a PhotoLister and an Enqueuer). The
// on-demand and worker-handler Services omit them, since they never backfill; a
// Service that must back the /process/thumbnails endpoint is wired with both.
var ErrBackfillUnavailable = errors.New("thumbjob: thumbnail backfill not configured")

// PhotoStore is the subset of the photo catalogue the handler needs: loading the
// photo and reading/writing its perceptual hashes. It is satisfied by
// *photos.Store.
type PhotoStore interface {
	// GetByUID returns the photo identified by uid, or photos.ErrPhotoNotFound.
	GetByUID(ctx context.Context, uid string) (photos.Photo, error)
	// GetPhash returns the photo's perceptual hashes, or photos.ErrPhashNotFound.
	GetPhash(ctx context.Context, photoUID string) (photos.Phash, error)
	// SetPhash upserts the photo's perceptual hashes.
	SetPhash(ctx context.Context, p photos.Phash) error
}

// Thumbnailer renders a photo's derived images. It is satisfied by
// *thumb.Thumbnailer; GenerateAll regenerates every registered size, skipping
// those already cached, while RegenerateAll rebuilds them all, overwriting the
// cache.
type Thumbnailer interface {
	// GenerateAll generates every registered thumbnail size for photo, returning a
	// map from size name to its absolute cache path, skipping sizes already cached.
	GenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error)
	// RegenerateAll forces regeneration of every registered thumbnail size for
	// photo, overwriting any already-cached sizes, and returns the same map.
	RegenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error)
}

// Decoder resolves a photo's stored original to a decoded image so the handler
// can recompute its perceptual hashes. It is satisfied by *StorageDecoder; tests
// supply a fake to avoid touching disk.
type Decoder interface {
	// DecodeOriginal decodes the photo's stored original, returning the image and a
	// cleanup the caller must invoke when done with it.
	DecodeOriginal(ctx context.Context, photo photos.Photo) (image.Image, func(), error)
}

// PhotoLister enumerates the photos a thumbnail backfill should schedule. It is
// satisfied by *photos.Store and is optional (only the Service backing the
// /process/thumbnails endpoint needs it).
type PhotoLister interface {
	// ListPhotosMissingPhash returns the uids of non-archived photos that have no
	// perceptual hash yet (limit <= 0 returns all). The thumbnail job computes the
	// pHash alongside the thumbnail, so a missing pHash marks a photo whose
	// thumbnail was never generated — the narrow "missing thumbnail" predicate.
	ListPhotosMissingPhash(ctx context.Context, limit int) ([]string, error)
	// ListActiveUIDs returns the uids of every non-archived photo, for a forced
	// full re-run that re-checks thumbnails across the whole library.
	ListActiveUIDs(ctx context.Context) ([]string, error)
}

// Enqueuer schedules thumbnail jobs for the backfill. It is satisfied by
// jobs.Enqueuer and is optional (only the Service backing the /process/thumbnails
// endpoint needs it).
type Enqueuer interface {
	// EnqueueThumbnail schedules thumbnail regeneration for photoUID, treating an
	// existing active job as a no-op so repeated backfills do not pile up.
	EnqueueThumbnail(ctx context.Context, photoUID string) error
}

// Config bundles the collaborators a Service needs. Photos, Thumbnailer and
// Decoder are required; Lister and Enqueuer are optional and enable the thumbnail
// backfill (BackfillThumbnails) when both are supplied.
type Config struct {
	// Photos is the catalogue repository.
	Photos PhotoStore
	// Thumbnailer renders derived images.
	Thumbnailer Thumbnailer
	// Decoder decodes originals for perceptual hashing.
	Decoder Decoder
	// Lister enumerates photos for the thumbnail backfill (optional).
	Lister PhotoLister
	// Enqueuer schedules thumbnail backfill jobs (optional).
	Enqueuer Enqueuer
}

// Service regenerates a photo's derived data. It is safe for concurrent use: the
// thumbnailer and catalogue layers tolerate concurrent calls for distinct photos.
type Service struct {
	photos   PhotoStore
	thumbs   Thumbnailer
	decoder  Decoder
	lister   PhotoLister
	enqueuer Enqueuer
}

// New returns a Service from cfg. It panics if any of the three required
// collaborators (Photos, Thumbnailer, Decoder) is nil, since a thumbnail job
// cannot run without them. Lister and Enqueuer are optional; when both are
// supplied the Service can also drive the thumbnail backfill.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Thumbnailer == nil || cfg.Decoder == nil {
		panic("thumbjob: Photos, Thumbnailer and Decoder are required")
	}
	return &Service{
		photos:   cfg.Photos,
		thumbs:   cfg.Thumbnailer,
		decoder:  cfg.Decoder,
		lister:   cfg.Lister,
		enqueuer: cfg.Enqueuer,
	}
}

// jobPayload is the JSON shape of a thumbnail job's payload.
type jobPayload struct {
	PhotoUID string `json:"photo_uid"`
}

// Handle is the worker.HandlerFunc for thumbnail jobs: it decodes the photo uid
// from the job payload and regenerates the photo's derived data. A malformed or
// empty payload is a permanent error (the job dead-letters rather than retrying a
// payload that can never succeed).
func (s *Service) Handle(ctx context.Context, job jobs.Job) error {
	var p jobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("thumbjob: decoding payload: %w", err)
	}
	if p.PhotoUID == "" {
		return ErrMissingPhotoUID
	}
	return s.Regenerate(ctx, p.PhotoUID)
}

// Regenerate rebuilds the regenerable derived data for the photo identified by
// photoUID: it (re)generates any missing thumbnail sizes and, when the photo has
// no perceptual hashes, recomputes and stores them. It is idempotent — cached
// sizes and an existing pHash are left untouched. A missing photo is returned as
// an error so the job dead-letters rather than looping on a uid that will never
// resolve.
func (s *Service) Regenerate(ctx context.Context, photoUID string) error {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return fmt.Errorf("thumbjob: loading photo %s: %w", photoUID, err)
	}
	if _, err := s.thumbs.GenerateAll(ctx, photo); err != nil {
		return fmt.Errorf("thumbjob: generating thumbnails for %s: %w", photoUID, err)
	}
	return s.ensurePhash(ctx, photo)
}

// ForceRegenerate unconditionally rebuilds the photo's derived data on demand: it
// regenerates every thumbnail size — overwriting any already cached — and
// recomputes and stores the perceptual hashes even when they already exist,
// returning the regenerated size names (sorted) so the caller can report a clear
// result. It is the counterpart to Regenerate (the idempotent repair path the
// job handler runs): where Regenerate skips data already present, ForceRegenerate
// rebuilds a stale or corrupt thumbnail/pHash from the original. The original
// file is never modified.
//
// A missing photo is returned as photos.ErrPhotoNotFound (which the HTTP layer
// maps to 404); a missing or undecodable original is wrapped with
// ErrRegenerateFailed (mapped to 422); a storage/database failure is an ordinary
// wrapped error (mapped to 500).
func (s *Service) ForceRegenerate(ctx context.Context, photoUID string) ([]string, error) {
	photo, err := s.photos.GetByUID(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("thumbjob: loading photo %s: %w", photoUID, err)
	}
	sizes, err := s.thumbs.RegenerateAll(ctx, photo)
	if err != nil {
		return nil, fmt.Errorf("%w: regenerating thumbnails for %s: %w", ErrRegenerateFailed, photoUID, err)
	}
	if err := s.recomputePhash(ctx, photo); err != nil {
		return nil, err
	}
	return sortedSizeNames(sizes), nil
}

// BackfillThumbnails enqueues a thumbnail job for every photo that currently
// lacks a generated thumbnail, returning how many uids it scheduled. "Missing a
// thumbnail" is defined as having no perceptual hash — the pHash is computed by
// the thumbnail job alongside the thumbnail, so its absence marks a photo whose
// thumbnailing never completed (an undecodable format at ingest, an offline box,
// or a transient error). When all is true it instead schedules every
// non-archived photo, so any thumbnail size missing from an otherwise-hashed
// photo is regenerated too; the thumbnail handler skips sizes already cached, so
// the full re-run stays cheap and never rewrites an original.
//
// It only enqueues jobs — the actual thumbnailing runs later in the local
// thumbnail worker, so the backfill proceeds regardless of whether the
// embeddings box is online. It is idempotent: a photo whose thumbnail job is
// already queued is a harmless no-op (the enqueuer dedupes), so concurrent or
// repeated runs never pile up redundant jobs. It returns ErrBackfillUnavailable
// when the Service was built without the Lister and Enqueuer collaborators.
func (s *Service) BackfillThumbnails(ctx context.Context, all bool) (int, error) {
	if s.lister == nil || s.enqueuer == nil {
		return 0, ErrBackfillUnavailable
	}
	uids, err := s.backfillCandidates(ctx, all)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, uid := range uids {
		if err := s.enqueuer.EnqueueThumbnail(ctx, uid); err != nil {
			return enqueued, fmt.Errorf("thumbjob: enqueuing thumbnail for %s: %w", uid, err)
		}
		enqueued++
	}
	return enqueued, nil
}

// backfillCandidates returns the uids the backfill should schedule: every
// non-archived photo when all is set, otherwise only those missing a perceptual
// hash (the narrow "no thumbnail generated" predicate).
func (s *Service) backfillCandidates(ctx context.Context, all bool) ([]string, error) {
	if all {
		uids, err := s.lister.ListActiveUIDs(ctx)
		if err != nil {
			return nil, fmt.Errorf("thumbjob: listing active photos: %w", err)
		}
		return uids, nil
	}
	uids, err := s.lister.ListPhotosMissingPhash(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("thumbjob: listing photos missing thumbnail: %w", err)
	}
	return uids, nil
}

// sortedSizeNames returns the keys of a size→path map in sorted order, so a
// regenerated-sizes result is deterministic regardless of map iteration order.
func sortedSizeNames(sizes map[string]string) []string {
	names := make([]string, 0, len(sizes))
	for name := range sizes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ensurePhash recomputes and stores the photo's perceptual hashes when they are
// absent. A photo that already has a pHash is a no-op. The original is decoded
// only when needed, so the common case (thumbnails missing but pHash present)
// avoids the decode entirely.
func (s *Service) ensurePhash(ctx context.Context, photo photos.Photo) error {
	_, err := s.photos.GetPhash(ctx, photo.UID)
	if err == nil {
		return nil // already hashed — idempotent skip
	}
	if !errors.Is(err, photos.ErrPhashNotFound) {
		return fmt.Errorf("thumbjob: checking phash for %s: %w", photo.UID, err)
	}
	return s.recomputePhash(ctx, photo)
}

// recomputePhash decodes the photo's original, computes its perceptual hashes and
// stores them, overwriting any hashes already present. It backs both the repair
// path (ensurePhash, only when absent) and the force path (ForceRegenerate,
// always), so the caller decides whether a recompute is needed.
func (s *Service) recomputePhash(ctx context.Context, photo photos.Photo) error {
	img, cleanup, err := s.decoder.DecodeOriginal(ctx, photo)
	if err != nil {
		return fmt.Errorf("thumbjob: decoding %s for phash: %w", photo.UID, err)
	}
	defer cleanup()

	hashes := phash.Compute(img)
	if err := s.photos.SetPhash(ctx, photos.Phash{
		PhotoUID: photo.UID, Phash: hashes.Phash, Dhash: hashes.Dhash,
	}); err != nil {
		return fmt.Errorf("thumbjob: storing phash for %s: %w", photo.UID, err)
	}
	return nil
}
