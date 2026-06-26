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
	if err := validateFaceDims(faces); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := replaceFaces(ctx, tx, photoUID, faces); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing faces for %s: %w", photoUID, err)
	}
	return nil
}

// upsertFaceDetectionSQL records (or refreshes) the face-detection event for a
// photo, overwriting the previous count/model/timestamp on re-detection.
const upsertFaceDetectionSQL = `
INSERT INTO face_detections (photo_uid, face_count, model, detected_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (photo_uid) DO UPDATE SET
    face_count  = EXCLUDED.face_count,
    model       = EXCLUDED.model,
    detected_at = now()`

// RecordFaceDetection stores the detected faces for photoUID and marks the photo
// as face-detected in one transaction: existing faces are replaced and a
// face_detections row is upserted with the face count and model. Recording the
// detection even when faces is empty is what lets a photo with no faces be told
// apart from one that was never processed, so the job stays idempotent and the
// backfill skips it. Vectors are validated against FaceDim (ErrDimMismatch) and a
// duplicate face_index yields ErrFaceIndexTaken.
func (s *Store) RecordFaceDetection(ctx context.Context, photoUID string, faces []Face, model string) error {
	if err := validateFaceDims(faces); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := replaceFaces(ctx, tx, photoUID, faces); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, upsertFaceDetectionSQL, photoUID, len(faces), model); err != nil {
		return fmt.Errorf("recording face detection for %s: %w", photoUID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing face detection for %s: %w", photoUID, err)
	}
	return nil
}

// FacesDetected reports whether face detection has already been recorded for
// photoUID (a face_detections row exists), regardless of how many faces were
// found. It is the idempotency check the face_detect handler uses to skip a photo
// it has already processed.
func (s *Store) FacesDetected(ctx context.Context, photoUID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM face_detections WHERE photo_uid = $1)", photoUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking face detection for %s: %w", photoUID, err)
	}
	return exists, nil
}

// listMissingFacesSQL selects the uids of non-archived photos that have no
// face_detections row, newest first. The %s placeholder is replaced with a LIMIT
// clause only when a positive limit is requested.
const listMissingFacesSQL = `
SELECT p.uid
FROM photos p
LEFT JOIN face_detections fd ON fd.photo_uid = p.uid
WHERE fd.photo_uid IS NULL AND p.archived_at IS NULL
ORDER BY p.created_at DESC, p.uid DESC%s`

// ListPhotosMissingFaces returns the uids of non-archived photos that have not
// yet had face detection run, newest first. A positive limit caps the result; a
// non-positive limit returns every unprocessed photo. It backs the face-detection
// backfill, which enqueues a face_detect job per returned uid.
func (s *Store) ListPhotosMissingFaces(ctx context.Context, limit int) ([]string, error) {
	return s.queryPhotoUIDs(ctx, listMissingFacesSQL, limit)
}

// validateFaceDims returns ErrDimMismatch if any face's vector does not have
// exactly FaceDim elements.
func validateFaceDims(faces []Face) error {
	for i := range faces {
		if len(faces[i].Vector) != FaceDim {
			return fmt.Errorf("%w: face %d got %d, want %d",
				ErrDimMismatch, faces[i].FaceIndex, len(faces[i].Vector), FaceDim)
		}
	}
	return nil
}

// replaceFaces deletes every existing face of photoUID and inserts the supplied
// set within tx. Callers wrap it in a transaction they commit.
func replaceFaces(ctx context.Context, tx pgx.Tx, photoUID string, faces []Face) error {
	if _, err := tx.Exec(ctx, "DELETE FROM faces WHERE photo_uid = $1", photoUID); err != nil {
		return fmt.Errorf("clearing faces for %s: %w", photoUID, err)
	}
	return insertFaces(ctx, tx, photoUID, faces)
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
