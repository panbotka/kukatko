package photosorter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// photoColumns is the column list shared by the photo SELECT, matched by
// scanPhoto.
const photoColumns = `uid, file_hash, file_path, file_name, file_size, file_mime,
	file_width, file_height, file_orientation, taken_at, taken_at_source,
	title, description, notes, lat, lng, altitude, camera_make, camera_model,
	lens_model, iso, aperture, exposure, focal_length, exif, private,
	archived_at, updated_at`

// listPhotosSQL pages photos ordered by updated_at (then uid for a stable
// tie-break) so the migration can resume from a watermark. The $1 lower bound is
// the zero time on a full run, which every real updated_at exceeds.
const listPhotosSQL = `SELECT ` + photoColumns + `
FROM photos
WHERE updated_at > $1
ORDER BY updated_at, uid
LIMIT $2 OFFSET $3`

// ListPhotos returns one page of photos modified after params.UpdatedSince,
// ordered by updated_at. The result set is stable for the lifetime of the
// (read-only) migration run, so LIMIT/OFFSET paging is safe. A short page (fewer
// than the limit) signals the last page.
func (r *Reader) ListPhotos(ctx context.Context, params PhotoListParams) ([]Photo, error) {
	limit := pageLimit(params.Limit)
	rows, err := r.pool.Query(ctx, listPhotosSQL, params.UpdatedSince, limit, params.Offset)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing photos: %w", err)
	}
	defer rows.Close()

	var photos []Photo
	for rows.Next() {
		photo, scanErr := scanPhoto(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		photos = append(photos, photo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating photos: %w", err)
	}
	return photos, nil
}

// MaxUpdatedAt returns the largest updated_at across all photos and whether any
// photo exists. The migration uses it only as a diagnostic; the per-photo
// timestamps drive the actual resume watermark.
func (r *Reader) MaxUpdatedAt(ctx context.Context) (time.Time, bool, error) {
	var ts *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT max(updated_at) FROM photos`).Scan(&ts); err != nil {
		return time.Time{}, false, fmt.Errorf("photosorter: max updated_at: %w", err)
	}
	if ts == nil {
		return time.Time{}, false, nil
	}
	return *ts, true, nil
}

// scanPhoto reads one photos row in photoColumns order.
func scanPhoto(row pgx.Row) (Photo, error) {
	var (
		p    Photo
		exif []byte
	)
	if err := row.Scan(
		&p.UID, &p.FileHash, &p.FilePath, &p.FileName, &p.FileSize, &p.FileMime,
		&p.FileWidth, &p.FileHeight, &p.FileOrientation, &p.TakenAt, &p.TakenAtSource,
		&p.Title, &p.Description, &p.Notes, &p.Lat, &p.Lng, &p.Altitude,
		&p.CameraMake, &p.CameraModel, &p.LensModel, &p.ISO, &p.Aperture,
		&p.Exposure, &p.FocalLength, &exif, &p.Private, &p.ArchivedAt, &p.UpdatedAt,
	); err != nil {
		return Photo{}, fmt.Errorf("photosorter: scanning photo: %w", err)
	}
	p.Exif = exif
	return p, nil
}
