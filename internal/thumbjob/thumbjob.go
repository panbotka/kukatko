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

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/phash"
	"github.com/panbotka/kukatko/internal/photos"
)

// ErrMissingPhotoUID indicates a thumbnail job payload carried no photo uid, a
// permanent error so the job dead-letters rather than retrying forever.
var ErrMissingPhotoUID = errors.New("thumbjob: job payload missing photo_uid")

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
// those already cached.
type Thumbnailer interface {
	// GenerateAll generates every registered thumbnail size for photo, returning a
	// map from size name to its absolute cache path.
	GenerateAll(ctx context.Context, photo photos.Photo) (map[string]string, error)
}

// Decoder resolves a photo's stored original to a decoded image so the handler
// can recompute its perceptual hashes. It is satisfied by *StorageDecoder; tests
// supply a fake to avoid touching disk.
type Decoder interface {
	// DecodeOriginal decodes the photo's stored original, returning the image and a
	// cleanup the caller must invoke when done with it.
	DecodeOriginal(ctx context.Context, photo photos.Photo) (image.Image, func(), error)
}

// Config bundles the collaborators a Service needs. All three are required.
type Config struct {
	// Photos is the catalogue repository.
	Photos PhotoStore
	// Thumbnailer renders derived images.
	Thumbnailer Thumbnailer
	// Decoder decodes originals for perceptual hashing.
	Decoder Decoder
}

// Service regenerates a photo's derived data. It is safe for concurrent use: the
// thumbnailer and catalogue layers tolerate concurrent calls for distinct photos.
type Service struct {
	photos  PhotoStore
	thumbs  Thumbnailer
	decoder Decoder
}

// New returns a Service from cfg. It panics if any collaborator is nil, since a
// thumbnail job cannot run without all three.
func New(cfg Config) *Service {
	if cfg.Photos == nil || cfg.Thumbnailer == nil || cfg.Decoder == nil {
		panic("thumbjob: Photos, Thumbnailer and Decoder are required")
	}
	return &Service{photos: cfg.Photos, thumbs: cfg.Thumbnailer, decoder: cfg.Decoder}
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
