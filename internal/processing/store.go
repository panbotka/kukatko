package processing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/photos"
)

// evidenceSQL reads everything the database knows about the work already done on
// one photo, in a single round trip. Every side table it touches is keyed by
// photo_uid (primary key), so the four LEFT JOINs add at most one row each and
// the whole report never degenerates into a query per step.
//
// The place column is deliberately conditional: photo_places also holds
// coordinate-less marker rows, written for a photo with no GPS so the geocoder
// never retries it, and those are not evidence of a place.
const evidenceSQL = `
SELECT p.media_type,
       (p.lat IS NOT NULL AND p.lng IS NOT NULL) AS has_gps,
       p.metadata_extracted_at,
       ph.created_at  AS thumbnail_at,
       e.created_at   AS embedding_at,
       fd.detected_at AS face_at,
       COALESCE(fd.face_count, 0) AS face_count,
       p.ocr_at,
       (p.ocr_text <> '') AS ocr_text_found,
       CASE WHEN pl.lat IS NOT NULL AND pl.lng IS NOT NULL THEN pl.geocoded_at END AS place_at,
       p.sidecar_written_at
FROM photos p
LEFT JOIN photo_phashes   ph ON ph.photo_uid = p.uid
LEFT JOIN embeddings      e  ON e.photo_uid  = p.uid
LEFT JOIN face_detections fd ON fd.photo_uid = p.uid
LEFT JOIN photo_places    pl ON pl.photo_uid = p.uid
WHERE p.uid = $1`

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
	var ev Evidence
	err := s.pool.QueryRow(ctx, evidenceSQL, photoUID).Scan(
		&ev.MediaType, &ev.HasGPS, &ev.MetadataAt, &ev.ThumbnailAt, &ev.EmbeddingAt,
		&ev.FaceAt, &ev.FaceCount, &ev.OCRAt, &ev.OCRTextFound, &ev.PlaceAt, &ev.SidecarAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evidence{}, photos.ErrPhotoNotFound
	}
	if err != nil {
		return Evidence{}, fmt.Errorf("processing: reading evidence for %s: %w", photoUID, err)
	}
	return ev, nil
}
