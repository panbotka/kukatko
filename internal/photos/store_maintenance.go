package photos

import (
	"context"
	"fmt"
)

// PrimaryFile is the minimal view of a photo's primary original file used by the
// library maintenance scan: the owning photo's uid plus the storage key
// (file_path, a slash-separated path relative to the originals root) and SHA256
// content hash needed to check the original's presence on disk and locate its
// cached thumbnails.
type PrimaryFile struct {
	// PhotoUID is the uid of the owning photo.
	PhotoUID string `json:"photo_uid"`
	// FilePath is the storage key of the primary original file.
	FilePath string `json:"file_path"`
	// FileHash is the lowercase hex SHA256 digest of the original, which keys the
	// thumbnail cache.
	FileHash string `json:"file_hash"`
}

// ListPrimaryFiles returns the primary original file of every photo (including
// archived photos, whose originals still occupy disk and still have thumbnails)
// as a PrimaryFile. It backs the maintenance integrity scan, which stats each
// original on disk and checks each photo's thumbnails. The slice is empty (not
// nil) when there are no photos.
func (s *Store) ListPrimaryFiles(ctx context.Context) ([]PrimaryFile, error) {
	const q = `SELECT photo_uid, file_path, file_hash
		FROM photo_files
		WHERE is_primary = true
		ORDER BY photo_uid`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("photos: querying primary files: %w", err)
	}
	defer rows.Close()

	files := make([]PrimaryFile, 0)
	for rows.Next() {
		var f PrimaryFile
		if err := rows.Scan(&f.PhotoUID, &f.FilePath, &f.FileHash); err != nil {
			return nil, fmt.Errorf("photos: scanning primary file: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating primary files: %w", err)
	}
	return files, nil
}

// listMissingPhashSQL selects the uids of non-archived photos that have no
// perceptual-hash row yet, newest first. The trailing %s receives an optional
// LIMIT clause.
const listMissingPhashSQL = `
SELECT p.uid
FROM photos p
LEFT JOIN photo_phashes ph ON ph.photo_uid = p.uid
WHERE p.project_id = $1
  AND ph.photo_uid IS NULL AND p.archived_at IS NULL
ORDER BY p.created_at DESC, p.uid DESC%s`

// ListPhotosMissingPhash returns the uids of non-archived photos that do not yet
// have perceptual hashes, newest first. A positive limit caps the result; a
// non-positive limit returns every missing photo. It backs the maintenance
// pHash-recompute repair, which enqueues a thumbnail job per returned uid (the
// thumbnail handler recomputes a missing pHash while regenerating thumbnails).
//
// It reads one project only: an empty projectUID is ErrNoProject.
func (s *Store) ListPhotosMissingPhash(ctx context.Context, projectUID string, limit int) ([]string, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(listMissingPhashSQL, "")
	args := []any{projectUID}
	if limit > 0 {
		query = fmt.Sprintf(listMissingPhashSQL, "\nLIMIT $2")
		args = append(args, limit)
	}
	return s.queryUIDs(ctx, "listing photos missing phash", query, args...)
}

// listActiveUIDsSQL selects the uids of every non-archived photo, newest first.
const listActiveUIDsSQL = `
SELECT uid
FROM photos
WHERE photos.project_id = $1
  AND archived_at IS NULL
ORDER BY created_at DESC, uid DESC`

// ListActiveUIDs returns the uids of every non-archived photo, newest first. It
// backs the forced full thumbnail backfill (`?all=true`), which enqueues a
// thumbnail job per photo so any missing thumbnail size is regenerated even for
// photos that already have a perceptual hash; the thumbnail handler skips sizes
// already cached, so a full re-run stays cheap and never touches originals.
//
// It reads one project only: an empty projectUID is ErrNoProject.
func (s *Store) ListActiveUIDs(ctx context.Context, projectUID string) ([]string, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	return s.queryUIDs(ctx, "listing active photos", listActiveUIDsSQL, projectUID)
}

// listMissingFileMetadataSQL selects the uids of non-archived photos whose
// original has never been read out into the metadata columns, newest first. The
// trailing %s receives an optional LIMIT clause. It is served by
// idx_photos_metadata_pending, the partial index over exactly this predicate.
const listMissingFileMetadataSQL = `
SELECT uid
FROM photos
WHERE photos.project_id = $1
  AND metadata_extracted_at IS NULL AND archived_at IS NULL
ORDER BY created_at DESC, uid DESC%s`

// ListPhotosMissingFileMetadata returns the uids of non-archived photos whose own
// file has never been read out into the IPTC/XMP and file-technical columns
// (metadata_extracted_at IS NULL), newest first. A positive limit caps the result;
// a non-positive limit returns every pending photo. It backs the metadata backfill,
// which enqueues a `metadata` job per returned uid.
//
// The predicate is the extraction marker rather than "the columns are empty", so
// the backfill converges: a photo whose file simply carries no IPTC tags is still
// finished once it has been read, and is not re-scheduled on every run.
//
// It reads one project only: an empty projectUID is ErrNoProject.
func (s *Store) ListPhotosMissingFileMetadata(ctx context.Context, projectUID string, limit int) ([]string, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(listMissingFileMetadataSQL, "")
	args := []any{projectUID}
	if limit > 0 {
		query = fmt.Sprintf(listMissingFileMetadataSQL, "\nLIMIT $2")
		args = append(args, limit)
	}
	return s.queryUIDs(ctx, "listing photos missing file metadata", query, args...)
}

// countMissingPhashSQL counts the photos ListPhotosMissingPhash would return.
const countMissingPhashSQL = `
SELECT count(*)
FROM photos p
LEFT JOIN photo_phashes ph ON ph.photo_uid = p.uid
WHERE p.project_id = $1
  AND ph.photo_uid IS NULL AND p.archived_at IS NULL`

// CountPhotosMissingPhash returns how many non-archived photos have no perceptual
// hashes, which is how many jobs the narrow thumbnail backfill would schedule. It
// exists so the cost of that backfill can be reported before it is paid: on a
// library whose thumbnails were never hashed the "narrow" predicate matches
// everything, and an operator should learn that from a number rather than from a
// run that takes the rest of the day.
//
// It reads one project only: an empty projectUID is ErrNoProject.
func (s *Store) CountPhotosMissingPhash(ctx context.Context, projectUID string) (int, error) {
	if err := requireProject(projectUID); err != nil {
		return 0, err
	}
	return s.queryCount(ctx, "counting photos missing phash", countMissingPhashSQL, projectUID)
}

// countActiveSQL counts the photos ListActiveUIDs would return.
const countActiveSQL = `SELECT count(*) FROM photos WHERE photos.project_id = $1 AND archived_at IS NULL`

// CountActivePhotos returns how many non-archived photos the catalogue holds,
// which is how many jobs a forced full thumbnail backfill (?all=true) would
// schedule.
//
// It reads one project only: an empty projectUID is ErrNoProject.
func (s *Store) CountActivePhotos(ctx context.Context, projectUID string) (int, error) {
	if err := requireProject(projectUID); err != nil {
		return 0, err
	}
	return s.queryCount(ctx, "counting active photos", countActiveSQL, projectUID)
}

// queryCount runs a single-value count query with args, wrapping any failure
// with the given action for context.
func (s *Store) queryCount(ctx context.Context, action, query string, args ...any) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("photos: %s: %w", action, err)
	}
	return count, nil
}

// queryUIDs runs a single-column uid query and collects the results, wrapping any
// failure with the given action for context. It is the shared scan loop behind
// the uid-listing helpers.
func (s *Store) queryUIDs(ctx context.Context, action, query string, args ...any) ([]string, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: %s: %w", action, err)
	}
	defer rows.Close()

	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("photos: scanning photo uid: %w", err)
		}
		uids = append(uids, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating photo uids: %w", err)
	}
	return uids, nil
}
