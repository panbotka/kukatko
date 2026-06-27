package psimport

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/storage"
)

// processPhoto migrates a single photo-sorter photo: it resolves the photo onto a
// Kukátko record (matching an existing one or copying and cataloguing a new one)
// and then transfers its satellites (embedding, faces, perceptual hashes, edits,
// markers, album/label membership). It returns the photo's outcome.
func (s *Service) processPhoto(ctx context.Context, ps photosorter.Photo, maps mappings) (outcome, error) {
	kkUID, result, err := s.resolvePhoto(ctx, ps)
	if err != nil {
		return 0, err
	}
	if err := s.transferSatellites(ctx, kkUID, ps, maps); err != nil {
		return 0, err
	}
	return result, nil
}

// resolvePhoto returns the Kukátko photo UID for a photo-sorter photo, matching
// it by photosorter_uid (already migrated) or file_hash (already catalogued, for
// example from PhotoPrism — backfilling photosorter_uid) before copying and
// cataloguing a new record. The outcome distinguishes a matched photo (skipped)
// from a newly copied one (imported).
func (s *Service) resolvePhoto(ctx context.Context, ps photosorter.Photo) (string, outcome, error) {
	existing, err := s.photos.GetByPhotosorterUID(ctx, ps.UID)
	if err == nil {
		return existing.UID, outcomeSkipped, nil
	}
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		return "", 0, fmt.Errorf("psimport: looking up photo by photosorter_uid: %w", err)
	}

	matched, err := s.photos.GetByFileHash(ctx, ps.FileHash)
	if err == nil {
		return s.attachByHash(ctx, ps, matched.UID)
	}
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		return "", 0, fmt.Errorf("psimport: looking up photo by file_hash: %w", err)
	}
	return s.createPhoto(ctx, ps)
}

// attachByHash backfills photosorter_uid onto a photo already catalogued under
// the same content hash, so its migrated embeddings and faces attach to the
// existing record without copying the original again.
func (s *Service) attachByHash(ctx context.Context, ps photosorter.Photo, uid string) (string, outcome, error) {
	if _, err := s.photos.SetPhotosorterRef(ctx, uid, ps.UID); err != nil {
		return "", 0, fmt.Errorf("psimport: backfilling photosorter_uid on %s: %w", uid, err)
	}
	return uid, outcomeSkipped, nil
}

// createPhoto copies the original from its photo-sorter file_path into Kukátko's
// storage, catalogues the photo and its primary file row, and renders thumbnails.
// A content hash already present (concurrent or earlier dedup) is resolved by
// attaching to the existing record. On a primary-file failure the half-created
// photo is rolled back.
func (s *Service) createPhoto(ctx context.Context, ps photosorter.Photo) (string, outcome, error) {
	stored, err := s.copyOriginal(ctx, ps)
	if err != nil {
		return "", 0, err
	}
	created, err := s.photos.Create(ctx, buildPhoto(ps, stored))
	if errors.Is(err, photos.ErrFileHashTaken) {
		matched, getErr := s.photos.GetByFileHash(ctx, stored.Hash)
		if getErr != nil {
			return "", 0, fmt.Errorf("psimport: resolving deduped photo: %w", getErr)
		}
		return s.attachByHash(ctx, ps, matched.UID)
	}
	if err != nil {
		return "", 0, fmt.Errorf("psimport: creating photo: %w", err)
	}
	if err := s.createPrimaryFile(ctx, created, stored); err != nil {
		_ = s.photos.Delete(ctx, created.UID)
		return "", 0, err
	}
	if _, err := s.thumbs.GenerateAll(ctx, created); err != nil {
		s.log.Warn("psimport: generating thumbnails", "photo", created.UID, "err", err)
	}
	return created.UID, outcomeImported, nil
}

// copyOriginal opens the photo-sorter original by its file_path and streams it
// into Kukátko's storage under the capture month. A byte-identical file already
// on disk (ErrAlreadyExists) is treated as a successful, deduplicated store.
func (s *Service) copyOriginal(ctx context.Context, ps photosorter.Photo) (storage.StoredFile, error) {
	src, err := s.open(ps.FilePath)
	if err != nil {
		return storage.StoredFile{}, fmt.Errorf("psimport: opening original %q: %w", ps.FilePath, err)
	}
	defer func() { _ = src.Close() }()

	takenAt := time.Time{}
	if ps.TakenAt != nil {
		takenAt = *ps.TakenAt
	}
	stored, err := s.storage.Store(ctx, src, takenAt, originalName(ps))
	if errors.Is(err, storage.ErrAlreadyExists) {
		return stored, nil
	}
	if err != nil {
		return storage.StoredFile{}, fmt.Errorf("psimport: storing original %q: %w", ps.FilePath, err)
	}
	return stored, nil
}

// createPrimaryFile inserts the primary original photo_files row for a freshly
// catalogued photo.
func (s *Service) createPrimaryFile(ctx context.Context, photo photos.Photo, stored storage.StoredFile) error {
	if _, err := s.photos.CreateFile(ctx, photos.PhotoFile{
		PhotoUID:  photo.UID,
		FilePath:  stored.RelPath,
		FileHash:  stored.Hash,
		FileSize:  stored.Size,
		FileMime:  photo.FileMime,
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	}); err != nil {
		return fmt.Errorf("psimport: creating primary file for %s: %w", photo.UID, err)
	}
	return nil
}

// buildPhoto maps a photo-sorter photo plus its stored original onto a Kukátko
// photo record, carrying the curated metadata across 1:1 and stamping
// photosorter_uid for future dedup. The content hash, path, size and MIME come
// from the freshly stored file so they describe the bytes actually on disk.
func buildPhoto(ps photosorter.Photo, stored storage.StoredFile) photos.Photo {
	psUID := ps.UID
	return photos.Photo{
		FileHash:        stored.Hash,
		FilePath:        stored.RelPath,
		FileName:        originalName(ps),
		FileSize:        stored.Size,
		FileMime:        photoMime(ps, stored),
		FileWidth:       ps.FileWidth,
		FileHeight:      ps.FileHeight,
		FileOrientation: ps.FileOrientation,
		TakenAt:         ps.TakenAt,
		TakenAtSource:   ps.TakenAtSource,
		Title:           ps.Title,
		Description:     ps.Description,
		Notes:           ps.Notes,
		Lat:             ps.Lat,
		Lng:             ps.Lng,
		Altitude:        ps.Altitude,
		CameraMake:      ps.CameraMake,
		CameraModel:     ps.CameraModel,
		LensModel:       ps.LensModel,
		ISO:             ps.ISO,
		Aperture:        ps.Aperture,
		Exposure:        ps.Exposure,
		FocalLength:     ps.FocalLength,
		Exif:            ps.Exif,
		Private:         ps.Private,
		ArchivedAt:      ps.ArchivedAt,
		PhotosorterUID:  &psUID,
	}
}

// originalName resolves the best original file name for a photo-sorter photo,
// falling back to the base of its file_path.
func originalName(ps photosorter.Photo) string {
	if ps.FileName != "" {
		return ps.FileName
	}
	if base := path.Base(ps.FilePath); base != "." && base != "/" {
		return base
	}
	return ps.UID
}

// photoMime prefers photo-sorter's recorded MIME, falling back to the type the
// storage layer sniffed from the copied bytes.
func photoMime(ps photosorter.Photo, stored storage.StoredFile) string {
	if ps.FileMime != "" {
		return ps.FileMime
	}
	return stored.MIME
}
