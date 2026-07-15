package feedback

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// insertFaceRejectionSQL records that a face is NOT a subject. ON CONFLICT DO
// NOTHING makes a repeated rejection a no-op rather than a unique-constraint error.
const insertFaceRejectionSQL = `
INSERT INTO face_rejections (photo_uid, face_index, subject_uid, rejected_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (photo_uid, face_index, subject_uid) DO NOTHING`

// deleteFaceRejectionSQL removes a face rejection; deleting a pair that was never
// rejected affects no rows and is a no-op.
const deleteFaceRejectionSQL = `
DELETE FROM face_rejections
WHERE photo_uid = $1 AND face_index = $2 AND subject_uid = $3`

// existsFaceRejectionSQL checks whether a face is rejected for a subject.
const existsFaceRejectionSQL = `
SELECT EXISTS (
    SELECT 1 FROM face_rejections
    WHERE photo_uid = $1 AND face_index = $2 AND subject_uid = $3)`

// listFaceRejectionsBySubjectSQL reads every face rejected for a subject as the
// (photo_uid, face_index) exclusion keys, in a deterministic order.
const listFaceRejectionsBySubjectSQL = `
SELECT photo_uid, face_index
FROM face_rejections
WHERE subject_uid = $1
ORDER BY photo_uid, face_index`

// RejectFace records that the face identified by key is NOT key.SubjectUID and
// writes entry in the same transaction. The write is idempotent: rejecting the same
// (face, subject) twice is a no-op, not an error. rejected_by is taken from
// entry.ActorUID (empty stored as NULL). It never mutates the face — no marker is
// unassigned and no row is deleted. It returns ErrEmptyKey if the key lacks a photo
// or subject UID, or a wrapped error if the subject/photo foreign keys reject the
// row.
func (s *Store) RejectFace(ctx context.Context, key FaceRejectionKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertFaceRejectionSQL,
			key.PhotoUID, key.FaceIndex, key.SubjectUID, nullable(entry.ActorUID))
		if isForeignKeyViolation(err) {
			return ErrTargetNotFound
		}
		if err != nil {
			return fmt.Errorf("feedback: rejecting face %s#%d for %s: %w",
				key.PhotoUID, key.FaceIndex, key.SubjectUID, err)
		}
		return nil
	})
}

// UnrejectFace removes the face rejection identified by key and writes entry in the
// same transaction, letting a user take a rejection back. Un-rejecting a pair that
// was never rejected still records the action but changes no rows. It returns
// ErrEmptyKey if the key lacks a photo or subject UID.
func (s *Store) UnrejectFace(ctx context.Context, key FaceRejectionKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, deleteFaceRejectionSQL, key.PhotoUID, key.FaceIndex, key.SubjectUID)
		if err != nil {
			return fmt.Errorf("feedback: un-rejecting face %s#%d for %s: %w",
				key.PhotoUID, key.FaceIndex, key.SubjectUID, err)
		}
		return nil
	})
}

// IsFaceRejected reports whether the face identified by key has been rejected for
// key.SubjectUID. It returns ErrEmptyKey if the key lacks a photo or subject UID.
func (s *Store) IsFaceRejected(ctx context.Context, key FaceRejectionKey) (bool, error) {
	if !key.valid() {
		return false, ErrEmptyKey
	}
	var exists bool
	err := s.pool.QueryRow(ctx, existsFaceRejectionSQL,
		key.PhotoUID, key.FaceIndex, key.SubjectUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("feedback: checking face rejection for %s: %w", key.SubjectUID, err)
	}
	return exists, nil
}

// FaceRejectionsForSubject returns every face rejected for subjectUID as (photo
// UID, face index) exclusion keys, ordered deterministically. It is the bulk lookup
// the unassigned-face search uses to exclude already-rejected faces in SQL, without
// an N+1. A subject with no rejections yields an empty (non-nil) slice, nil error.
func (s *Store) FaceRejectionsForSubject(ctx context.Context, subjectUID string) ([]FaceRef, error) {
	if subjectUID == "" {
		return nil, ErrEmptyKey
	}
	rows, err := s.pool.Query(ctx, listFaceRejectionsBySubjectSQL, subjectUID)
	if err != nil {
		return nil, fmt.Errorf("feedback: listing face rejections for %s: %w", subjectUID, err)
	}
	defer rows.Close()

	refs := []FaceRef{}
	for rows.Next() {
		var ref FaceRef
		if err := rows.Scan(&ref.PhotoUID, &ref.FaceIndex); err != nil {
			return nil, fmt.Errorf("feedback: scanning face rejection: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback: iterating face rejections for %s: %w", subjectUID, err)
	}
	return refs, nil
}

// insertLabelRejectionSQL records that a photo should NOT have a label. ON CONFLICT
// DO NOTHING makes a repeated rejection a no-op.
const insertLabelRejectionSQL = `
INSERT INTO label_rejections (photo_uid, label_uid, rejected_by)
VALUES ($1, $2, $3)
ON CONFLICT (photo_uid, label_uid) DO NOTHING`

// deleteLabelRejectionSQL removes a label rejection; deleting a pair that was never
// rejected affects no rows.
const deleteLabelRejectionSQL = `
DELETE FROM label_rejections WHERE photo_uid = $1 AND label_uid = $2`

// existsLabelRejectionSQL checks whether a photo↔label pair is rejected.
const existsLabelRejectionSQL = `
SELECT EXISTS (
    SELECT 1 FROM label_rejections WHERE photo_uid = $1 AND label_uid = $2)`

// listLabelRejectionsByLabelSQL reads every photo rejected for a label, in a
// deterministic order.
const listLabelRejectionsByLabelSQL = `
SELECT photo_uid
FROM label_rejections
WHERE label_uid = $1
ORDER BY photo_uid`

// RejectLabel records that the photo identified by key should NOT carry
// key.LabelUID and writes entry in the same transaction. The write is idempotent
// and never detaches the label; it only records the opinion. rejected_by is taken
// from entry.ActorUID (empty stored as NULL). It returns ErrEmptyKey if the key
// lacks a photo or label UID.
func (s *Store) RejectLabel(ctx context.Context, key LabelRejectionKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertLabelRejectionSQL,
			key.PhotoUID, key.LabelUID, nullable(entry.ActorUID))
		if isForeignKeyViolation(err) {
			return ErrTargetNotFound
		}
		if err != nil {
			return fmt.Errorf("feedback: rejecting label %s for photo %s: %w",
				key.LabelUID, key.PhotoUID, err)
		}
		return nil
	})
}

// UnrejectLabel removes the label rejection identified by key and writes entry in
// the same transaction. Un-rejecting a pair that was never rejected still records
// the action but changes no rows. It returns ErrEmptyKey if the key lacks a photo
// or label UID.
func (s *Store) UnrejectLabel(ctx context.Context, key LabelRejectionKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, deleteLabelRejectionSQL, key.PhotoUID, key.LabelUID)
		if err != nil {
			return fmt.Errorf("feedback: un-rejecting label %s for photo %s: %w",
				key.LabelUID, key.PhotoUID, err)
		}
		return nil
	})
}

// IsLabelRejected reports whether the photo↔label pair identified by key has been
// rejected. It returns ErrEmptyKey if the key lacks a photo or label UID.
func (s *Store) IsLabelRejected(ctx context.Context, key LabelRejectionKey) (bool, error) {
	if !key.valid() {
		return false, ErrEmptyKey
	}
	var exists bool
	err := s.pool.QueryRow(ctx, existsLabelRejectionSQL, key.PhotoUID, key.LabelUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("feedback: checking label rejection for %s: %w", key.LabelUID, err)
	}
	return exists, nil
}

// LabelRejectionsForLabel returns the UIDs of every photo rejected for labelUID,
// ordered deterministically. It is the bulk lookup label-expansion uses to exclude
// already-rejected photos in SQL, without an N+1. A label with no rejections yields
// an empty (non-nil) slice, nil error.
func (s *Store) LabelRejectionsForLabel(ctx context.Context, labelUID string) ([]string, error) {
	if labelUID == "" {
		return nil, ErrEmptyKey
	}
	rows, err := s.pool.Query(ctx, listLabelRejectionsByLabelSQL, labelUID)
	if err != nil {
		return nil, fmt.Errorf("feedback: listing label rejections for %s: %w", labelUID, err)
	}
	defer rows.Close()

	photoUIDs := []string{}
	for rows.Next() {
		var photoUID string
		if err := rows.Scan(&photoUID); err != nil {
			return nil, fmt.Errorf("feedback: scanning label rejection: %w", err)
		}
		photoUIDs = append(photoUIDs, photoUID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback: iterating label rejections for %s: %w", labelUID, err)
	}
	return photoUIDs, nil
}
