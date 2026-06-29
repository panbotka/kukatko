package maintenance

import (
	"context"
	"errors"
	"fmt"
)

// ErrOrphanImportUnavailable indicates an orphan-import repair was requested but
// no importer is configured.
var ErrOrphanImportUnavailable = errors.New("maintenance: orphan import not configured")

// RepairOptions selects which repairs to run. Every repair is opt-in; the zero
// value runs nothing. Thumbnail and pHash repairs enqueue jobs (processed by the
// background worker with bounded concurrency); embedding and face repairs enqueue
// their respective jobs; orphan import runs synchronously through the upload
// pipeline.
type RepairOptions struct {
	// Thumbnails regenerates missing thumbnails.
	Thumbnails bool `json:"thumbnails"`
	// Embeddings backfills missing CLIP image embeddings.
	Embeddings bool `json:"embeddings"`
	// Faces backfills missing face detections.
	Faces bool `json:"faces"`
	// Phashes recomputes missing perceptual hashes.
	Phashes bool `json:"phashes"`
	// ImportOrphans catalogues originals on disk that have no catalogue row.
	ImportOrphans bool `json:"import_orphans"`
}

// Any reports whether at least one repair is selected.
func (o RepairOptions) Any() bool {
	return o.Thumbnails || o.Embeddings || o.Faces || o.Phashes || o.ImportOrphans
}

// RepairResult reports what each selected repair scheduled or did. Enqueue counts
// are scheduling attempts that succeeded (a job already queued for the same photo
// is an idempotent no-op still counted), so re-running converges without error.
type RepairResult struct {
	// ThumbnailsEnqueued is the number of thumbnail jobs scheduled.
	ThumbnailsEnqueued int `json:"thumbnails_enqueued"`
	// EmbeddingsEnqueued is the number of image_embed jobs scheduled.
	EmbeddingsEnqueued int `json:"embeddings_enqueued"`
	// FacesEnqueued is the number of face_detect jobs scheduled.
	FacesEnqueued int `json:"faces_enqueued"`
	// PhashesEnqueued is the number of pHash-recompute (thumbnail) jobs scheduled.
	PhashesEnqueued int `json:"phashes_enqueued"`
	// OrphansImported is the number of orphan originals catalogued as new photos.
	OrphansImported int `json:"orphans_imported"`
	// OrphansSkipped is the number of orphans whose content was already catalogued.
	OrphansSkipped int `json:"orphans_skipped"`
	// OrphansFailed is the number of orphans that could not be imported.
	OrphansFailed int `json:"orphans_failed"`
}

// Repair runs the selected repairs and returns what each scheduled or did. It is
// idempotent and safe to re-run: enqueue steps dedupe per photo, and orphan
// import dedupes on content hash. Repairs run in a fixed order; the first
// infrastructure error aborts and is returned, while per-orphan failures are
// tallied without aborting.
func (s *Service) Repair(ctx context.Context, opts RepairOptions) (RepairResult, error) {
	var res RepairResult
	if err := s.repairThumbnails(ctx, opts, &res); err != nil {
		return res, err
	}
	if err := s.repairPhashes(ctx, opts, &res); err != nil {
		return res, err
	}
	if err := s.repairEmbeddings(ctx, opts, &res); err != nil {
		return res, err
	}
	if err := s.repairFaces(ctx, opts, &res); err != nil {
		return res, err
	}
	if err := s.repairOrphans(ctx, opts, &res); err != nil {
		return res, err
	}
	return res, nil
}

// repairThumbnails enqueues a thumbnail job for every photo whose representative
// thumbnail is missing, when the thumbnail repair is selected.
func (s *Service) repairThumbnails(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.Thumbnails {
		return nil
	}
	primary, err := s.photos.ListPrimaryFiles(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: listing primary files: %w", err)
	}
	for _, pf := range primary {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: thumbnail repair interrupted: %w", ctxErr)
		}
		cached, thumbErr := s.thumbs.HasThumbnail(pf.FileHash)
		if thumbErr != nil {
			return fmt.Errorf("maintenance: checking thumbnail for %s: %w", pf.PhotoUID, thumbErr)
		}
		if cached {
			continue
		}
		if err := s.enqueuer.EnqueueThumbnail(ctx, pf.PhotoUID); err != nil {
			return fmt.Errorf("maintenance: enqueuing thumbnail for %s: %w", pf.PhotoUID, err)
		}
		res.ThumbnailsEnqueued++
	}
	return nil
}

// repairPhashes enqueues a thumbnail job (which recomputes a missing pHash) for
// every photo with no perceptual hashes, when the pHash repair is selected.
func (s *Service) repairPhashes(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.Phashes {
		return nil
	}
	uids, err := s.photos.ListPhotosMissingPhash(ctx, 0)
	if err != nil {
		return fmt.Errorf("maintenance: listing photos missing phash: %w", err)
	}
	for _, uid := range uids {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: phash repair interrupted: %w", ctxErr)
		}
		if err := s.enqueuer.EnqueueThumbnail(ctx, uid); err != nil {
			return fmt.Errorf("maintenance: enqueuing phash recompute for %s: %w", uid, err)
		}
		res.PhashesEnqueued++
	}
	return nil
}

// repairEmbeddings backfills missing image embeddings when selected.
func (s *Service) repairEmbeddings(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.Embeddings {
		return nil
	}
	n, err := s.embed.BackfillEmbeddings(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: backfilling embeddings: %w", err)
	}
	res.EmbeddingsEnqueued = n
	return nil
}

// repairFaces backfills missing face detections when selected.
func (s *Service) repairFaces(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.Faces {
		return nil
	}
	n, err := s.faces.BackfillFaces(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: backfilling faces: %w", err)
	}
	res.FacesEnqueued = n
	return nil
}

// repairOrphans catalogues every orphan original on disk through the upload
// pipeline when selected, tallying created/duplicate/failed without aborting on a
// single file's failure. It returns ErrOrphanImportUnavailable when no importer
// is configured.
func (s *Service) repairOrphans(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.ImportOrphans {
		return nil
	}
	if s.importer == nil {
		return ErrOrphanImportUnavailable
	}
	orphans, _, _, err := s.scanOrphans(ctx)
	if err != nil {
		return err
	}
	for _, key := range orphans {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: orphan import interrupted: %w", ctxErr)
		}
		s.importOneOrphan(ctx, key, res)
	}
	return nil
}

// importOneOrphan catalogues a single orphan original, recording its outcome in
// res. A failure is tallied (not propagated) so a bad file does not abort the
// batch.
func (s *Service) importOneOrphan(ctx context.Context, key string, res *RepairResult) {
	outcome, err := s.importer.ImportOriginal(ctx, key)
	switch {
	case err != nil:
		res.OrphansFailed++
	case outcome == ImportDuplicate:
		res.OrphansSkipped++
	default:
		res.OrphansImported++
	}
}
