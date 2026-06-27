package photosorter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

// Embedding returns the CLIP image embedding stored for photoUID. The boolean is
// false when photo-sorter has no embedding for the photo (it was never embedded),
// in which case the migration may enqueue Kukátko's own embedding job instead.
func (r *Reader) Embedding(ctx context.Context, photoUID string) (Embedding, bool, error) {
	const q = `SELECT photo_uid, embedding, model, pretrained
		FROM embeddings WHERE photo_uid = $1`
	var (
		emb Embedding
		vec pgvector.Vector
	)
	err := r.pool.QueryRow(ctx, q, photoUID).Scan(&emb.PhotoUID, &vec, &emb.Model, &emb.Pretrained)
	if errors.Is(err, pgx.ErrNoRows) {
		return Embedding{}, false, nil
	}
	if err != nil {
		return Embedding{}, false, fmt.Errorf("photosorter: reading embedding for %s: %w", photoUID, err)
	}
	emb.Vector = vec.Slice()
	return emb, true, nil
}

// facesColumns is the column list shared by the faces SELECT, matched by
// scanFace. model is nullable in photo-sorter, so it is coalesced to the empty
// string.
const facesColumns = `photo_uid, face_index, embedding, bbox, det_score,
	coalesce(model, ''), marker_uid, subject_uid, subject_name,
	photo_width, photo_height, orientation`

// Faces returns every detected face for photoUID in face_index order, with the
// embedding decoded to []float32 and the denormalised cache columns carried
// through. A photo with no faces yields an empty slice and a nil error.
func (r *Reader) Faces(ctx context.Context, photoUID string) ([]Face, error) {
	q := `SELECT ` + facesColumns + ` FROM faces WHERE photo_uid = $1 ORDER BY face_index`
	rows, err := r.pool.Query(ctx, q, photoUID)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing faces for %s: %w", photoUID, err)
	}
	defer rows.Close()

	var faces []Face
	for rows.Next() {
		face, scanErr := scanFace(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		faces = append(faces, face)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating faces for %s: %w", photoUID, err)
	}
	return faces, nil
}

// scanFace reads one faces row in facesColumns order, decoding the embedding and
// bounding-box array.
func scanFace(row pgx.Row) (Face, error) {
	var (
		face Face
		vec  pgvector.Vector
		bbox []float64
	)
	if err := row.Scan(
		&face.PhotoUID, &face.FaceIndex, &vec, &bbox, &face.DetScore, &face.Model,
		&face.MarkerUID, &face.SubjectUID, &face.SubjectName,
		&face.PhotoWidth, &face.PhotoHeight, &face.Orientation,
	); err != nil {
		return Face{}, fmt.Errorf("photosorter: scanning face: %w", err)
	}
	face.Vector = vec.Slice()
	copy(face.BBox[:], bbox)
	return face, nil
}

// FacesProcessed reports whether photo-sorter recorded a face-detection event for
// photoUID and, if so, the face count. A photo with no faces_processed row was
// never processed, so the migration leaves it for Kukátko's own detection rather
// than recording a misleading zero-face detection.
func (r *Reader) FacesProcessed(ctx context.Context, photoUID string) (int, bool, error) {
	const q = `SELECT face_count FROM faces_processed WHERE photo_uid = $1`
	var count int
	err := r.pool.QueryRow(ctx, q, photoUID).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("photosorter: reading faces_processed for %s: %w", photoUID, err)
	}
	return count, true, nil
}
