package ppimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// outcome classifies how one PhotoPrism photo was handled.
type outcome int

const (
	// outcomeImported means a new photo was downloaded and catalogued.
	outcomeImported outcome = iota
	// outcomeUpdated means an already-imported photo's metadata changed.
	outcomeUpdated
	// outcomeSkipped means nothing changed: the content was already catalogued or
	// the metadata was already up to date.
	outcomeSkipped
)

// importPhotos walks every page of the incremental photo listing, importing each
// photo and checkpointing the run's counts after every page. A listing error is
// an infrastructure failure (returned to fail the run); a per-photo failure is
// recorded in the run state and never aborts the walk.
func (s *Service) importPhotos(ctx context.Context, runID int64, state *runState) error {
	for offset := 0; ; {
		page, err := s.client.ListPhotos(ctx, photoprism.PhotoListParams{
			Count:        s.pageSize,
			Offset:       offset,
			UpdatedSince: state.since,
		})
		if err != nil {
			return fmt.Errorf("ppimport: listing photos at offset %d: %w", offset, err)
		}
		for i := range page {
			s.importOnePhoto(ctx, page[i], state)
		}
		if err := s.runs.UpdateCounts(ctx, runID, state.counts); err != nil {
			return fmt.Errorf("ppimport: checkpointing counts: %w", err)
		}
		if len(page) < s.pageSize {
			return nil
		}
		offset += len(page)
	}
}

// importOnePhoto processes a single photo, translating its outcome (or failure)
// into the run state. A failure is logged and tallied; it never propagates.
func (s *Service) importOnePhoto(ctx context.Context, pp photoprism.Photo, state *runState) {
	result, err := s.processPhoto(ctx, pp)
	if err != nil {
		s.log.Warn("ppimport: photo failed", "pp_uid", pp.UID, "err", err)
		state.recordFailure(pp.UpdatedAt)
		return
	}
	state.recordSuccess(pp.UpdatedAt)
	switch result {
	case outcomeImported:
		state.counts.Imported++
	case outcomeUpdated:
		state.counts.Updated++
	case outcomeSkipped:
		state.counts.Skipped++
	}
}

// processPhoto dedups a photo by its PhotoPrism UID — updating an already-imported
// photo's metadata when it changed — and otherwise imports it as new. A photo
// with no primary file cannot be downloaded and is a per-photo failure.
func (s *Service) processPhoto(ctx context.Context, pp photoprism.Photo) (outcome, error) {
	primary, ok := pp.PrimaryFile()
	if !ok {
		return outcomeSkipped, fmt.Errorf("ppimport: photo %s has no primary file", pp.UID)
	}
	existing, err := s.photos.GetByPhotoprismUID(ctx, pp.UID)
	switch {
	case err == nil:
		return s.updateExisting(ctx, existing, pp)
	case errors.Is(err, photos.ErrPhotoNotFound):
		return s.importNew(ctx, pp, primary)
	default:
		return outcomeSkipped, fmt.Errorf("ppimport: looking up %s: %w", pp.UID, err)
	}
}

// updateExisting applies PhotoPrism's current metadata to an already-imported
// photo, returning outcomeSkipped when nothing changed (the common case on a
// re-run) so the import stays idempotent. Markers are seeded only on first import,
// so a metadata update does not re-create them.
func (s *Service) updateExisting(ctx context.Context, existing photos.Photo, pp photoprism.Photo) (outcome, error) {
	update := metadataUpdate(existing, pp)
	if metadataUnchanged(existing, update) {
		return outcomeSkipped, nil
	}
	if _, err := s.photos.UpdateMetadata(ctx, existing.UID, update); err != nil {
		return outcomeSkipped, fmt.Errorf("ppimport: updating metadata for %s: %w", existing.UID, err)
	}
	return outcomeUpdated, nil
}

// importNew downloads, dedups, stores and catalogues a not-yet-imported photo. A
// content hash that already exists (an identical file uploaded directly or
// migrated from photo-sorter) skips creation and backfills the PhotoPrism
// references so the next run dedups on the UID without re-downloading.
func (s *Service) importNew(ctx context.Context, pp photoprism.Photo, primary photoprism.File) (outcome, error) {
	staged, err := s.download(ctx, primary.Hash)
	if err != nil {
		return outcomeSkipped, err
	}
	defer staged.cleanup()

	if dup, err := s.dedupByContent(ctx, staged.hash, pp, primary); err != nil {
		return outcomeSkipped, err
	} else if dup {
		return outcomeSkipped, nil
	}

	photo, created, err := s.catalogue(ctx, pp, primary, staged)
	if err != nil {
		return outcomeSkipped, err
	}
	if !created {
		return outcomeSkipped, nil
	}
	s.postProcess(ctx, photo, pp)
	return outcomeImported, nil
}

// dedupByContent reports whether a photo with the staged content hash already
// exists, backfilling the PhotoPrism references onto it when they are not yet set
// so future incremental runs short-circuit on the UID lookup.
func (s *Service) dedupByContent(
	ctx context.Context, hash string, pp photoprism.Photo, primary photoprism.File,
) (bool, error) {
	existing, err := s.photos.GetByFileHash(ctx, hash)
	if errors.Is(err, photos.ErrPhotoNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ppimport: content dedup for %s: %w", pp.UID, err)
	}
	if existing.PhotoprismUID == nil {
		if _, err := s.photos.SetPhotoprismRef(ctx, existing.UID, pp.UID, primary.Hash); err != nil {
			return false, fmt.Errorf("ppimport: backfilling refs onto %s: %w", existing.UID, err)
		}
	}
	return true, nil
}

// catalogue stores the original and inserts the photos + primary photo_files
// rows. A unique-content race (the same bytes catalogued concurrently) is not an
// error: it returns created=false so the caller treats it as a duplicate. The
// stored original is published before the row so a failed insert leaves only a
// reclaimable content-addressed file behind.
func (s *Service) catalogue(
	ctx context.Context, pp photoprism.Photo, primary photoprism.File, staged *stagedFile,
) (photos.Photo, bool, error) {
	stored, err := s.storeOriginal(ctx, pp, primary, staged)
	if err != nil {
		return photos.Photo{}, false, err
	}
	meta := extractFileMeta(ctx, staged.path)
	created, err := s.photos.Create(ctx, buildPhoto(pp, primary, stored, meta))
	if errors.Is(err, photos.ErrFileHashTaken) {
		return photos.Photo{}, false, nil
	}
	if err != nil {
		return photos.Photo{}, false, fmt.Errorf("ppimport: cataloguing %s: %w", pp.UID, err)
	}
	if err := s.createPrimaryFile(ctx, created, stored); err != nil {
		_ = s.photos.Delete(ctx, created.UID)
		return photos.Photo{}, false, err
	}
	return created, true, nil
}

// createPrimaryFile inserts the stored original as the photo's primary file row.
func (s *Service) createPrimaryFile(ctx context.Context, photo photos.Photo, stored storage.StoredFile) error {
	_, err := s.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID:  photo.UID,
		FilePath:  stored.RelPath,
		FileHash:  stored.Hash,
		FileSize:  stored.Size,
		FileMime:  photo.FileMime,
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	})
	if err != nil {
		return fmt.Errorf("ppimport: creating primary file for %s: %w", photo.UID, err)
	}
	return nil
}

// postProcess runs the regenerable side effects of a freshly imported photo —
// thumbnails, background jobs and people markers — collecting failures as logged
// warnings. None of them undo the import: a missing thumbnail, unqueued job or
// skipped marker is a degraded but repairable state.
func (s *Service) postProcess(ctx context.Context, photo photos.Photo, pp photoprism.Photo) {
	if _, err := s.thumbs.GenerateAll(ctx, photo); err != nil {
		s.log.Warn("ppimport: thumbnails failed", "photo", photo.UID, "err", err)
	}
	s.enqueueJobs(ctx, photo.UID)
	s.importMarkers(ctx, photo.UID, pp)
}

// enqueueJobs schedules the image_embed and face_detect jobs for a new photo so
// embeddings and faces are computed once the box is reachable. A duplicate active
// job is a no-op the enqueuer swallows; any other error is logged.
func (s *Service) enqueueJobs(ctx context.Context, photoUID string) {
	if err := s.enqueuer.EnqueueImageEmbed(ctx, photoUID); err != nil {
		s.log.Warn("ppimport: enqueue image_embed failed", "photo", photoUID, "err", err)
	}
	if err := s.enqueuer.EnqueueFaceDetect(ctx, photoUID); err != nil {
		s.log.Warn("ppimport: enqueue face_detect failed", "photo", photoUID, "err", err)
	}
}

// storeOriginal reopens the staged temp file and publishes it into the storage
// layout under the photo's capture month (or the import month when the capture
// time is unknown). A storage ErrAlreadyExists is treated as success: the
// byte-identical original is already in place.
func (s *Service) storeOriginal(
	ctx context.Context, pp photoprism.Photo, primary photoprism.File, staged *stagedFile,
) (storage.StoredFile, error) {
	file, err := os.Open(staged.path)
	if err != nil {
		return storage.StoredFile{}, fmt.Errorf("ppimport: reopening staged file: %w", err)
	}
	defer func() { _ = file.Close() }()

	out, err := s.storage.Store(ctx, file, pp.TakenAt, originalName(pp, primary))
	if err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		return storage.StoredFile{}, fmt.Errorf("ppimport: storing original for %s: %w", pp.UID, err)
	}
	return out, nil
}

// stagedFile is a downloaded original streamed to a temp file, with its SHA256
// content hash and byte size computed during the copy.
type stagedFile struct {
	path string
	hash string
	size int64
}

// cleanup removes the temp file; it is safe to defer immediately after staging.
func (f *stagedFile) cleanup() {
	if f != nil && f.path != "" {
		_ = os.Remove(f.path)
	}
}

// download streams a PhotoPrism original (by its SHA1 file hash) into a temp file
// while computing its SHA256 hash and size, never buffering the file whole in
// memory. An oversized download (past MaxFileSize) is rejected.
func (s *Service) download(ctx context.Context, fileHash string) (*stagedFile, error) {
	dl, err := s.client.DownloadOriginal(ctx, fileHash)
	if err != nil {
		return nil, fmt.Errorf("ppimport: downloading %s: %w", fileHash, err)
	}
	defer func() { _ = dl.Body.Close() }()

	tmp, err := os.CreateTemp(s.tempDir, "kukatko-ppimport-*")
	if err != nil {
		return nil, fmt.Errorf("ppimport: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	var reader io.Reader = dl.Body
	if s.maxFileSize > 0 {
		reader = io.LimitReader(reader, s.maxFileSize+1)
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, hasher), reader)
	closeErr := tmp.Close()
	if err := firstErr(copyErr, closeErr); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("ppimport: streaming download %s: %w", fileHash, err)
	}
	if s.maxFileSize > 0 && size > s.maxFileSize {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("ppimport: original %s exceeds max size %d", fileHash, s.maxFileSize)
	}
	return &stagedFile{path: tmpPath, hash: hex.EncodeToString(hasher.Sum(nil)), size: size}, nil
}

// firstErr returns the first non-nil error among its arguments, or nil.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
