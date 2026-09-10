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
//   - The place backlog is the same predicate as LibrarySummary.PhotosPendingGeocode
//     and as internal/places.ListPhotosMissingPlaces: a live photo that carries
//     coordinates and has no photo_places row. Photos with no coordinates are
//     deliberately out — no geocode can ever give them a place, so counting them
//     would make the tile a backlog nothing can work down. They are the tile above
//     it, PhotosWithoutGPS.
//   - The OCR backlog matches idx_photos_ocr_pending exactly (never OCR'd, not
//     archived, not a video), so scheduling and reporting can never disagree
//     about what is left.
//   - The video-encoding backlog is the one part that cannot be written as a
//     scalar subquery over photos, because classifying a video needs both its
//     renditions and its encode jobs. Both are folded once, in the CTEs above the
//     statement: hls_encoded reduces photo_hls_renditions to one row per encoded
//     video, hls_encodes groups the hls_transcode queue rows by the photo uid in
//     their payload (one pass over each), and video_streaming joins them onto
//     the browsable videos. The counts below then read that one materialised relation, so
//     adding this section costs no per-video query — which is what keeps the
//     dashboard the single round trip a polled page can afford.
//   - The duplicate-marker count mirrors internal/dupmarkers.GroupMarkers: a
//     valid face marker of a named subject on a live photo, grouped by (photo,
//     subject), groups of two or more, minus the pairs a curator has already
//     dismissed. It counts the findings the review page would show, not the
//     markers behind them.
const countDashboardSQL = `
WITH hls_encoded AS MATERIALIZED (
    -- The videos that can be streamed: one row per photo, however many
    -- qualities it was encoded into.
    SELECT DISTINCT photo_uid FROM photo_hls_renditions
), hls_encodes AS MATERIALIZED (
    -- The furthest an encode of this video has got, smallest wins: 1 running,
    -- 2 queued, 3 broken (failed or dead-lettered), 4 finished. A video keeps
    -- its finished job rows forever, so without the ordering a finished row
    -- would hide the encode that is running now.
    SELECT payload ->> 'photo_uid' AS photo_uid,
           min(CASE state
                   WHEN 'running' THEN 1
                   WHEN 'queued'  THEN 2
                   WHEN 'failed'  THEN 3
                   WHEN 'dead'    THEN 3
                   ELSE 4
               END) AS stage
    FROM jobs
    WHERE type = 'hls_transcode' AND payload ->> 'photo_uid' IS NOT NULL
    GROUP BY 1
), video_streaming AS MATERIALIZED (
    SELECT e.photo_uid IS NOT NULL AS streamable, coalesce(j.stage, 4) AS stage
    FROM photos p
    LEFT JOIN hls_encoded e ON e.photo_uid = p.uid
    LEFT JOIN hls_encodes j ON j.photo_uid = p.uid
    WHERE p.archived_at IS NULL AND p.media_type = 'video'
)
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
        WHERE p.archived_at IS NULL AND p.lat IS NOT NULL AND p.lng IS NOT NULL
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
        HAVING count(*) > 1) g),
    (SELECT count(*) FROM video_streaming),
    (SELECT count(*) FROM video_streaming WHERE streamable),
    (SELECT count(*) FROM video_streaming WHERE NOT streamable),
    (SELECT count(*) FROM video_streaming WHERE NOT streamable AND stage = 2),
    (SELECT count(*) FROM video_streaming WHERE NOT streamable AND stage = 1),
    (SELECT count(*) FROM video_streaming WHERE NOT streamable AND stage = 3),
    (SELECT count(*) FROM video_streaming WHERE NOT streamable AND stage = 4),
    (SELECT count(*) FROM photo_hls_renditions),
    (SELECT min(created_at) FROM jobs WHERE type = 'hls_transcode' AND state = 'queued')`

// CountDashboard returns the library summary, the remaining-work counts and the
// video-encoding backlog in a single query. DerivedBytes, the duplicate scan and
// Video.StreamingEnabled are left zero: none of them is a count the database can
// answer, so the service fills them in — this method reports only what the
// catalogue actually holds. It returns an error when the query fails; callers
// must not treat a failure as an empty library.
func (s *Store) CountDashboard(ctx context.Context) (Dashboard, error) {
	var dash Dashboard
	lib, rem, vid := &dash.Library, &dash.Remaining, &dash.Video
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
		&vid.Videos,
		&vid.Streamable,
		&vid.Missing,
		&vid.EncodeQueued,
		&vid.EncodeRunning,
		&vid.EncodeFailed,
		&vid.NotScheduled,
		&vid.Renditions,
		&vid.OldestQueuedAt,
	)
	if err != nil {
		return Dashboard{}, fmt.Errorf("system: counting dashboard: %w", err)
	}
	// The backlog list and the video card must never disagree about how many clips
	// still play as the whole original, so the one count answers both.
	rem.VideosWithoutStreaming = vid.Missing
	return dash, nil
}
