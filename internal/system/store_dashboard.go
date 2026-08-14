package system

import (
	"context"
	"fmt"
)

// countDashboardSQL gathers every dashboard aggregate in one round trip as a row
// of scalar subqueries, the same shape countLibrarySQL uses. Each one is a plain
// COUNT or SUM over an indexed or cheap predicate: the status page polls, so
// nothing here may walk a directory tree, run the near-duplicate finder or touch
// an HNSW index.
//
// What each group leans on:
//
//   - The library/trash split rides idx_photos_archived_at, the partial index
//     over the archived minority; the video count rides idx_photos_media_type,
//     which excludes the majority value 'image'.
//   - The upload windows are counted against now() on the database's clock rather
//     than a timestamp passed in, so the four windows of one snapshot cannot
//     disagree with each other. They include archived photos: a photo that
//     arrived and was thrown away still arrived.
//   - The two byte sums are the catalogue's own arithmetic over file_size, which
//     is what makes them meaningful on an instance whose originals live in an
//     object store and whose local disk therefore holds nothing to measure.
//   - The OCR backlog matches idx_photos_ocr_pending exactly (never OCR'd, not
//     archived, not a video), so scheduling and reporting can never disagree
//     about what is left.
//   - The duplicate-marker count mirrors internal/dupmarkers.GroupMarkers: a
//     valid face marker of a named subject on a live photo, grouped by (photo,
//     subject), groups of two or more, minus the pairs a curator has already
//     dismissed. It counts the findings the review page would show, not the
//     markers behind them.
const countDashboardSQL = `
SELECT
    (SELECT count(*) FROM photos WHERE archived_at IS NULL),
    (SELECT count(*) FROM photos WHERE archived_at IS NULL AND media_type = 'video'),
    (SELECT count(*) FROM photos WHERE archived_at IS NOT NULL),
    (SELECT count(*) FROM photos WHERE archived_at IS NULL AND hidden_from_library),
    (SELECT count(*) FROM photos WHERE archived_at IS NULL AND private),
    (SELECT count(*) FROM photos WHERE created_at >= now() - interval '24 hours'),
    (SELECT count(*) FROM photos WHERE created_at >= now() - interval '7 days'),
    (SELECT count(*) FROM photos WHERE created_at >= now() - interval '30 days'),
    (SELECT count(*) FROM photos WHERE created_at >= now() - interval '365 days'),
    (SELECT count(*) FROM albums),
    (SELECT count(*) FROM labels),
    (SELECT count(*) FROM subjects WHERE type = 'person'),
    (SELECT count(*) FROM faces),
    (SELECT count(*) FROM embeddings),
    (SELECT coalesce(sum(file_size), 0) FROM photos WHERE archived_at IS NULL),
    (SELECT coalesce(sum(file_size), 0) FROM photos WHERE archived_at IS NOT NULL),
    (SELECT count(*) FROM faces WHERE subject_uid IS NULL),
    (SELECT count(*) FROM face_clusters),
    (SELECT count(*) FROM photos WHERE archived_at IS NULL AND taken_at IS NULL),
    (SELECT count(*) FROM photos WHERE archived_at IS NULL AND (lat IS NULL OR lng IS NULL)),
    (SELECT count(*) FROM photos p
        WHERE p.archived_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM photo_places pp WHERE pp.photo_uid = p.uid)),
    (SELECT count(*) FROM photos
        WHERE ocr_at IS NULL AND archived_at IS NULL AND media_type <> 'video'),
    (SELECT count(*) FROM (
        SELECT m.photo_uid, m.subject_uid
        FROM markers m
        JOIN subjects s ON s.uid = m.subject_uid
        JOIN photos p ON p.uid = m.photo_uid
        WHERE m.invalid = FALSE
          AND m.type = 'face'
          AND s.name <> ''
          AND p.archived_at IS NULL
          AND NOT EXISTS (
              SELECT 1 FROM duplicate_marker_dismissals d
              WHERE d.photo_uid = m.photo_uid AND d.subject_uid = m.subject_uid)
        GROUP BY m.photo_uid, m.subject_uid
        HAVING count(*) > 1) g)`

// CountDashboard returns the library summary and the remaining-work counts in a
// single query. DerivedBytes and the duplicate scan are left zero: neither is a
// count the database can answer, so the service fills them in — this method
// reports only what the catalogue actually holds. It returns an error when the
// query fails; callers must not treat a failure as an empty library.
func (s *Store) CountDashboard(ctx context.Context) (Dashboard, error) {
	var dash Dashboard
	lib, rem := &dash.Library, &dash.Remaining
	err := s.pool.QueryRow(ctx, countDashboardSQL).Scan(
		&lib.Photos,
		&lib.Videos,
		&lib.Trashed,
		&lib.Hidden,
		&lib.Private,
		&lib.Uploads.Day,
		&lib.Uploads.Week,
		&lib.Uploads.Month,
		&lib.Uploads.Year,
		&lib.Albums,
		&lib.Labels,
		&lib.People,
		&lib.Faces,
		&lib.Embeddings,
		&lib.LibraryBytes,
		&lib.TrashBytes,
		&rem.FacesUnassigned,
		&rem.Clusters,
		&rem.PhotosWithoutTakenAt,
		&rem.PhotosWithoutGPS,
		&rem.PhotosWithoutPlace,
		&rem.PhotosWithoutOCR,
		&rem.DuplicateMarkers,
	)
	if err != nil {
		return Dashboard{}, fmt.Errorf("system: counting dashboard: %w", err)
	}
	return dash, nil
}
