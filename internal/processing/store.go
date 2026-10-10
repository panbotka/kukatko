package processing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/photos"
)

// evidenceColumns is everything the database knows about the work already done
// on a photo, read in a single round trip. Every side table it touches is keyed by
// photo_uid (primary key), so the four LEFT JOINs add at most one row each and
// the whole report never degenerates into a query per step.
//
// The HLS column is a lateral aggregate rather than another LEFT JOIN, because
// photo_hls_renditions holds one row per rendition and a plain join would
// multiply the whole report by however many qualities the video was encoded
// into. The aggregate is served by the table's primary key, so it stays one
// index lookup.
//
// The place column is deliberately conditional: photo_places also holds
// coordinate-less marker rows, written for a photo with no GPS so the geocoder
// never retries it, and those are not evidence of a place.
const evidenceColumns = `p.media_type,
       (p.lat IS NOT NULL AND p.lng IS NOT NULL) AS has_gps,
       p.metadata_extracted_at,
       CASE WHEN ph.photo_uid IS NOT NULL THEN p.thumbnails_at END AS thumbnail_at,
       e.created_at   AS embedding_at,
       fd.detected_at AS face_at,
       COALESCE(fd.face_count, 0) AS face_count,
       p.ocr_at,
       (p.ocr_text <> '') AS ocr_text_found,
       CASE WHEN pl.lat IS NOT NULL AND pl.lng IS NOT NULL THEN pl.geocoded_at END AS place_at,
       p.sidecar_written_at,
       hls.encoded_at AS hls_at`

// evidenceFrom is the FROM clause behind evidenceColumns: the photo and every
// side table its evidence lives in.
const evidenceFrom = `FROM photos p
LEFT JOIN photo_phashes   ph ON ph.photo_uid = p.uid
LEFT JOIN embeddings      e  ON e.photo_uid  = p.uid
LEFT JOIN face_detections fd ON fd.photo_uid = p.uid
LEFT JOIN photo_places    pl ON pl.photo_uid = p.uid
LEFT JOIN LATERAL (
    SELECT max(h.encoded_at) AS encoded_at
    FROM photo_hls_renditions h
    WHERE h.photo_uid = p.uid
) hls ON true`

// evidenceSQL reads one photo's evidence; see evidenceColumns.
const evidenceSQL = "SELECT " + evidenceColumns + "\n" + evidenceFrom + "\nWHERE p.uid = $1"

// liveEvidenceSQL reads the same evidence for every live (non-archived) photo,
// keyed by uid, for the library-wide gap scan. Archived photos are left out the
// way every backfill leaves them out: nothing schedules work for them, so a step
// they never got is not a gap anybody should fill.
const liveEvidenceSQL = "SELECT p.uid, " + evidenceColumns + "\n" + evidenceFrom +
	"\nWHERE p.archived_at IS NULL\nORDER BY p.uid"

// unfinishedStepsSQL lists, for every photo, the job types it has a job of that
// has not completed — the queue's half of "is anything going to deliver this
// step?", in one scan of the queue rather than one query per photo. It agrees
// with jobs.Store.UnfinishedForPhoto on what unfinished means (any state but
// done), so the gap scan and the per-photo report cannot disagree.
const unfinishedStepsSQL = `
SELECT DISTINCT payload ->> 'photo_uid', type
FROM jobs
WHERE state <> 'done' AND payload ->> 'photo_uid' IS NOT NULL`

// Store reads the per-photo processing evidence. It owns no connection; it
// borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Evidence returns what has already been computed about the photo identified by
// photoUID, in one round trip. It returns photos.ErrPhotoNotFound when no such
// photo exists — including an archived one, which still has a row and still has
// a processing history.
func (s *Store) Evidence(ctx context.Context, photoUID string) (Evidence, error) {
	ev, err := scanEvidence(s.pool.QueryRow(ctx, evidenceSQL, photoUID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Evidence{}, photos.ErrPhotoNotFound
	}
	if err != nil {
		return Evidence{}, fmt.Errorf("processing: reading evidence for %s: %w", photoUID, err)
	}
	return ev, nil
}

// scanEvidence scans one row of evidenceColumns into an Evidence. Both readers
// share it, so a column added to the select list cannot be scanned in one place
// and forgotten in the other.
func scanEvidence(row pgx.Row, prefix ...any) (Evidence, error) {
	var ev Evidence
	dest := make([]any, 0, len(prefix)+12)
	dest = append(dest, prefix...)
	dest = append(dest,
		&ev.MediaType, &ev.HasGPS, &ev.MetadataAt, &ev.ThumbnailAt, &ev.EmbeddingAt,
		&ev.FaceAt, &ev.FaceCount, &ev.OCRAt, &ev.OCRTextFound, &ev.PlaceAt, &ev.SidecarAt,
		&ev.HLSAt,
	)
	if err := row.Scan(dest...); err != nil {
		return Evidence{}, fmt.Errorf("scanning evidence: %w", err)
	}
	return ev, nil
}

// EachLiveEvidence calls fn with the evidence of every live (non-archived)
// photo, in uid order, streaming the rows rather than collecting them. A non-nil
// error from fn stops the walk and is returned as is.
func (s *Store) EachLiveEvidence(ctx context.Context, fn func(photoUID string, ev Evidence) error) error {
	rows, err := s.pool.Query(ctx, liveEvidenceSQL)
	if err != nil {
		return fmt.Errorf("processing: reading library evidence: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		ev, err := scanEvidence(rows, &uid)
		if err != nil {
			return fmt.Errorf("processing: library: %w", err)
		}
		if err := fn(uid, ev); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("processing: iterating library evidence: %w", err)
	}
	return nil
}

// UnfinishedSteps returns, per photo uid, the job types that photo has an
// unfinished job of. A photo with no such job is absent from the map.
func (s *Store) UnfinishedSteps(ctx context.Context) (map[string]map[Step]bool, error) {
	rows, err := s.pool.Query(ctx, unfinishedStepsSQL)
	if err != nil {
		return nil, fmt.Errorf("processing: listing unfinished jobs: %w", err)
	}
	defer rows.Close()
	out := make(map[string]map[Step]bool)
	for rows.Next() {
		var uid, jobType string
		if err := rows.Scan(&uid, &jobType); err != nil {
			return nil, fmt.Errorf("processing: scanning unfinished jobs: %w", err)
		}
		if out[uid] == nil {
			out[uid] = make(map[Step]bool)
		}
		out[uid][Step(jobType)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("processing: iterating unfinished jobs: %w", err)
	}
	return out, nil
}
