package photos

import (
	"context"
	"fmt"
)

// CountPhotos returns the total number of rows in the photos table, including
// archived (soft-deleted) photos, since their original files still exist on disk
// and count towards the photos-vs-originals integrity check after a restore.
func (s *Store) CountPhotos(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM photos").Scan(&count); err != nil {
		return 0, fmt.Errorf("photos: counting photos: %w", err)
	}
	return count, nil
}

// ListFilePaths returns the storage key (file_path, a slash-separated path
// relative to the originals root) of every photo_files row, across all roles
// (original, sidecar, edited). It backs the post-restore integrity check that
// reconciles the catalogue against the originals on disk. The slice is empty
// (not nil) when there are no files.
func (s *Store) ListFilePaths(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, "SELECT file_path FROM photo_files")
	if err != nil {
		return nil, fmt.Errorf("photos: querying file paths: %w", err)
	}
	defer rows.Close()

	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("photos: scanning file path: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating file paths: %w", err)
	}
	return paths, nil
}

// CataloguedFile is one photo_files row as the maintenance scan reconciles it:
// the storage key and the project of the photo that owns it.
type CataloguedFile struct {
	// ProjectUID is the uid of the project the owning photo belongs to.
	ProjectUID string `json:"project_uid"`
	// FilePath is the file's storage key.
	FilePath string `json:"file_path"`
}

// ListCataloguedFiles returns every photo_files row — originals, sidecars and
// edits alike — with the project of the photo it belongs to, ordered by key. It
// is ListFilePaths with the project attached, for the maintenance scan, which
// reconciles the store against the catalogue per project and so has to know
// whose a catalogued key is. It reads every project on purpose: the scan is
// instance-wide. The slice is empty (not nil) when there are no files.
func (s *Store) ListCataloguedFiles(ctx context.Context) ([]CataloguedFile, error) {
	const q = `SELECT p.project_id, f.file_path
		FROM photo_files f
		JOIN photos p ON p.uid = f.photo_uid
		ORDER BY f.file_path`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("photos: querying catalogued files: %w", err)
	}
	defer rows.Close()

	files := make([]CataloguedFile, 0)
	for rows.Next() {
		var f CataloguedFile
		if err := rows.Scan(&f.ProjectUID, &f.FilePath); err != nil {
			return nil, fmt.Errorf("photos: scanning catalogued file: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating catalogued files: %w", err)
	}
	return files, nil
}
