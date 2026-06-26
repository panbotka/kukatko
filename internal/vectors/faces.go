package vectors

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgvector/pgvector-go"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// insertFaceSQL inserts a single face row and returns its assigned id and
// created_at.
const insertFaceSQL = `
INSERT INTO faces (
    photo_uid, face_index, embedding, bbox, det_score, model, dim,
    marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING id, created_at`

// listFacesSQL reads every face of a photo in face_index order.
const listFacesSQL = `
SELECT id, photo_uid, face_index, embedding, bbox, det_score, model, dim, created_at,
       marker_uid, subject_uid, subject_name, photo_width, photo_height, orientation
FROM faces
WHERE photo_uid = $1
ORDER BY face_index`

// SaveFaces replaces all faces of photoUID with the supplied set, atomically:
// existing rows are deleted and the new ones inserted in one transaction, so
// re-running face detection for a photo is idempotent. Each vector is validated
// against FaceDim (ErrDimMismatch otherwise). Two faces sharing a face_index
// violate the UNIQUE(photo_uid, face_index) constraint and yield ErrFaceIndexTaken.
func (s *Store) SaveFaces(ctx context.Context, photoUID string, faces []Face) error {
	for i := range faces {
		if len(faces[i].Vector) != FaceDim {
			return fmt.Errorf("%w: face %d got %d, want %d",
				ErrDimMismatch, faces[i].FaceIndex, len(faces[i].Vector), FaceDim)
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM faces WHERE photo_uid = $1", photoUID); err != nil {
		return fmt.Errorf("clearing faces for %s: %w", photoUID, err)
	}
	if err := insertFaces(ctx, tx, photoUID, faces); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing faces for %s: %w", photoUID, err)
	}
	return nil
}

// insertFaces inserts each face row within tx, mapping a unique-constraint
// violation on (photo_uid, face_index) to ErrFaceIndexTaken.
func insertFaces(ctx context.Context, tx pgx.Tx, photoUID string, faces []Face) error {
	for i := range faces {
		face := faces[i]
		bbox := face.BBox
		err := tx.QueryRow(ctx, insertFaceSQL,
			photoUID, face.FaceIndex, ToHalfVec(face.Vector), bbox[:], face.DetScore,
			face.Model, len(face.Vector), face.MarkerUID, face.SubjectUID,
			face.SubjectName, face.PhotoWidth, face.PhotoHeight, face.Orientation,
		).Scan(&faces[i].ID, &faces[i].CreatedAt)
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: photo %s face %d", ErrFaceIndexTaken, photoUID, face.FaceIndex)
		}
		if err != nil {
			return fmt.Errorf("inserting face %d for %s: %w", face.FaceIndex, photoUID, err)
		}
		faces[i].Dim = len(face.Vector)
	}
	return nil
}

// ListFaces returns every face stored for photoUID, ordered by face_index. A
// photo with no faces yields an empty slice and a nil error.
func (s *Store) ListFaces(ctx context.Context, photoUID string) ([]Face, error) {
	rows, err := s.pool.Query(ctx, listFacesSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("listing faces for %s: %w", photoUID, err)
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
		return nil, fmt.Errorf("iterating faces for %s: %w", photoUID, err)
	}
	return faces, nil
}

// scanFace reads one face row in listFacesSQL column order, decoding the halfvec
// embedding and the bounding-box array into the Face struct.
func scanFace(rows pgx.Rows) (Face, error) {
	var (
		face Face
		hv   pgvector.HalfVector
		bbox []float64
	)
	err := rows.Scan(
		&face.ID, &face.PhotoUID, &face.FaceIndex, &hv, &bbox, &face.DetScore,
		&face.Model, &face.Dim, &face.CreatedAt, &face.MarkerUID, &face.SubjectUID,
		&face.SubjectName, &face.PhotoWidth, &face.PhotoHeight, &face.Orientation)
	if err != nil {
		return Face{}, fmt.Errorf("scanning face row: %w", err)
	}
	face.Vector = FromHalfVec(hv)
	copy(face.BBox[:], bbox)
	return face, nil
}

// DeleteFaces removes every face of photoUID and returns how many rows were
// deleted (zero when the photo had no faces).
func (s *Store) DeleteFaces(ctx context.Context, photoUID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM faces WHERE photo_uid = $1", photoUID)
	if err != nil {
		return 0, fmt.Errorf("deleting faces for %s: %w", photoUID, err)
	}
	return tag.RowsAffected(), nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
