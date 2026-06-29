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
