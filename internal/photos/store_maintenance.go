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
WHERE ph.photo_uid IS NULL AND p.archived_at IS NULL
ORDER BY p.created_at DESC, p.uid DESC%s`

// ListPhotosMissingPhash returns the uids of non-archived photos that do not yet
// have perceptual hashes, newest first. A positive limit caps the result; a
// non-positive limit returns every missing photo. It backs the maintenance
// pHash-recompute repair, which enqueues a thumbnail job per returned uid (the
// thumbnail handler recomputes a missing pHash while regenerating thumbnails).
func (s *Store) ListPhotosMissingPhash(ctx context.Context, limit int) ([]string, error) {
	query := fmt.Sprintf(listMissingPhashSQL, "")
	args := []any(nil)
	if limit > 0 {
		query = fmt.Sprintf(listMissingPhashSQL, "\nLIMIT $1")
		args = []any{limit}
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: listing photos missing phash: %w", err)
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
