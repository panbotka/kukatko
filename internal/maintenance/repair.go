package maintenance

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/panbotka/kukatko/internal/vectors"
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
	// Dimensions rewrites the pixel dimensions of quarter-turned photos whose
	// columns hold the displayed frame instead of the stored one, and the faces
	// normalised against it.
	Dimensions bool `json:"dimensions"`
	// FaceMarkers clears the surplus face↔marker links: where several faces cache
	// the same marker, the pairing is re-derived and every face but the one it
	// awards the marker to has its cached link cleared.
	FaceMarkers bool `json:"face_markers"`
}

// Any reports whether at least one repair is selected.
func (o RepairOptions) Any() bool {
	return o.Thumbnails || o.Embeddings || o.Faces || o.Phashes ||
		o.ImportOrphans || o.Dimensions || o.FaceMarkers
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
	// DimensionsFixed is the number of photos whose transposed pixel dimensions
	// were rewritten from the file's own EXIF.
	DimensionsFixed int `json:"dimensions_fixed"`
	// FaceBoxesFixed is the number of face rows re-normalised alongside them.
	FaceBoxesFixed int `json:"face_boxes_fixed"`
	// FaceBoxesSkipped is the number of face rows with the same defect that were
	// deliberately left untouched because the evidence does not say which coordinate
	// space their box is in. They stay exactly as they are, so a later run can pick
	// them up once the photo carries a marker to reconcile them against.
	FaceBoxesSkipped int `json:"face_boxes_skipped"`
	// FaceLinksCleared is the number of face rows whose surplus claim on a marker
	// another face won was cleared.
	FaceLinksCleared int `json:"face_links_cleared"`
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
	if err := s.repairDimensions(ctx, opts, &res); err != nil {
		return res, err
	}
	if err := s.repairFaceMarkers(ctx, opts, &res); err != nil {
		return res, err
	}
	return res, nil
}

// repairFaceMarkers clears the surplus face↔marker links reported by the scan,
// when the face-marker repair is selected.
//
// It visits each affected photo once and delegates to the face cache, which
// re-derives that photo's exclusive pairing and clears every face claiming a
// marker another face won. It writes nothing else: no face row and no marker is
// ever deleted, and a face whose link is the only one on its marker is left alone
// — including the genuinely duplicated markers an import created, which are a
// different problem and must not be quietly swept up here. Re-running is a no-op
// once the cache is consistent.
func (s *Service) repairFaceMarkers(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.FaceMarkers {
		return nil
	}
	duplicates, err := s.vectors.ListDuplicateFaceMarkers(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: listing duplicate face markers: %w", err)
	}
	for _, photoUID := range affectedPhotos(duplicates) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: face-marker repair interrupted: %w", ctxErr)
		}
		cleared, clearErr := s.faceCache.ClearSurplusLinks(ctx, photoUID)
		if clearErr != nil {
			return fmt.Errorf("maintenance: clearing surplus face links on %s: %w", photoUID, clearErr)
		}
		res.FaceLinksCleared += cleared
	}
	return nil
}

// affectedPhotos returns the distinct photos the duplicate markers sit on, sorted
// so a repair run visits them in a deterministic order (several markers of one
// photo are re-matched in a single pass).
func affectedPhotos(duplicates []vectors.DuplicateFaceMarker) []string {
	seen := make(map[string]struct{}, len(duplicates))
	uids := make([]string, 0, len(duplicates))
	for _, dup := range duplicates {
		if _, ok := seen[dup.PhotoUID]; ok {
			continue
		}
		seen[dup.PhotoUID] = struct{}{}
		uids = append(uids, dup.PhotoUID)
	}
	sort.Strings(uids)
	return uids
}

// repairDimensions rewrites the pixel dimensions of every photo the scan reports
// as transposed and then corrects the face boxes recorded against that transposed
// frame, when the dimension repair is selected.
//
// The photo rows are fixed first because the faces half reads the corrected pair
// as the frame it reasons against. Unlike the other repairs it writes the
// catalogue directly instead of enqueuing work — there is nothing to regenerate,
// only two columns and a bbox to correct — which is why `maintenance scan` reports
// it and the flag is opt-in.
func (s *Service) repairDimensions(ctx context.Context, opts RepairOptions, res *RepairResult) error {
	if !opts.Dimensions {
		return nil
	}
	if err := s.repairPhotoDimensions(ctx, res); err != nil {
		return err
	}
	return s.repairFaceBoxes(ctx, res)
}

// repairPhotoDimensions writes the file's own dimensions onto every photo whose
// columns hold them transposed. Each write is guarded on the exact pair it
// replaces, so an interrupted run resumes cleanly and a re-run is a no-op.
func (s *Service) repairPhotoDimensions(ctx context.Context, res *RepairResult) error {
	mismatches, err := s.photos.ListDimensionMismatches(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: listing dimension mismatches: %w", err)
	}
	for _, m := range mismatches {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("maintenance: dimension repair interrupted: %w", ctxErr)
		}
		changed, repairErr := s.photos.RepairDimensions(ctx, m)
		if repairErr != nil {
			return fmt.Errorf("maintenance: repairing dimensions of %s: %w", m.UID, repairErr)
		}
		if changed {
			res.DimensionsFixed++
		}
	}
	return nil
}

// repairFaceBoxes corrects the face rows whose cached frame is their photo's
// stored pair transposed, applying to each the transform that photo's markers
// support and leaving alone every row whose space the evidence cannot establish.
//
// It runs over the whole catalogue rather than over the photos the photos half
// just fixed: a photo corrected by an earlier run still carries face rows to
// decide, and a row skipped for want of evidence has to remain reachable once that
// evidence arrives. Both halves are guarded on the state they replace, so no box
// is ever moved twice.
func (s *Service) repairFaceBoxes(ctx context.Context, res *RepairResult) error {
	plans, err := s.vectors.PlanFaceBoxRepair(ctx)
	if err != nil {
		return fmt.Errorf("maintenance: planning face box repair: %w", err)
	}
	for _, plan := range plans {
		if plan.Transform == vectors.TransformSkip {
			res.FaceBoxesSkipped++
		}
	}
	applied, err := s.vectors.ApplyFaceBoxRepair(ctx, plans)
	if err != nil {
		return fmt.Errorf("maintenance: repairing face boxes: %w", err)
	}
	res.FaceBoxesFixed += int(applied)
	return nil
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
