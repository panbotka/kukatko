package feedback

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// insertFaceConfirmationSQL records that a face really IS a subject. ON CONFLICT
// DO NOTHING makes a repeated confirmation a no-op rather than a
// unique-constraint error.
const insertFaceConfirmationSQL = `
INSERT INTO face_confirmations (photo_uid, face_index, subject_uid, confirmed_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (photo_uid, face_index, subject_uid) DO NOTHING`

// deleteFaceConfirmationSQL removes a face confirmation; deleting a pair that
// was never confirmed affects no rows and is a no-op.
const deleteFaceConfirmationSQL = `
DELETE FROM face_confirmations
WHERE photo_uid = $1 AND face_index = $2 AND subject_uid = $3`

// existsFaceConfirmationSQL checks whether a face is confirmed for a subject.
const existsFaceConfirmationSQL = `
SELECT EXISTS (
    SELECT 1 FROM face_confirmations
    WHERE photo_uid = $1 AND face_index = $2 AND subject_uid = $3)`

// listFaceConfirmationsBySubjectSQL reads every face confirmed for a subject as
// the (photo_uid, face_index) exclusion keys, in a deterministic order.
const listFaceConfirmationsBySubjectSQL = `
SELECT photo_uid, face_index
FROM face_confirmations
WHERE subject_uid = $1
ORDER BY photo_uid, face_index`

// ConfirmFace records that the face identified by key really IS key.SubjectUID
// and writes entry in the same transaction. The write is idempotent: confirming
// the same (face, subject) twice is a no-op, not an error. confirmed_by is taken
// from entry.ActorUID (empty stored as NULL). It never mutates the face — the
// assignment already exists; this only remembers the "yes" so outlier review can
// exclude the face. It returns ErrEmptyKey if the key lacks a photo or subject
// UID, or ErrTargetNotFound if the subject/photo foreign keys reject the row.
func (s *Store) ConfirmFace(ctx context.Context, key FaceConfirmationKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertFaceConfirmationSQL,
			key.PhotoUID, key.FaceIndex, key.SubjectUID, nullable(entry.ActorUID))
		if isForeignKeyViolation(err) {
			return ErrTargetNotFound
		}
		if err != nil {
			return fmt.Errorf("feedback: confirming face %s#%d for %s: %w",
				key.PhotoUID, key.FaceIndex, key.SubjectUID, err)
		}
		return nil
	})
}

// UnconfirmFace removes the face confirmation identified by key and writes entry
// in the same transaction, letting a user take a confirmation back. Un-confirming
// a pair that was never confirmed still records the action but changes no rows.
// It returns ErrEmptyKey if the key lacks a photo or subject UID.
func (s *Store) UnconfirmFace(ctx context.Context, key FaceConfirmationKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, deleteFaceConfirmationSQL, key.PhotoUID, key.FaceIndex, key.SubjectUID)
		if err != nil {
			return fmt.Errorf("feedback: un-confirming face %s#%d for %s: %w",
				key.PhotoUID, key.FaceIndex, key.SubjectUID, err)
		}
		return nil
	})
}

// IsFaceConfirmed reports whether the face identified by key has been confirmed
// for key.SubjectUID. It returns ErrEmptyKey if the key lacks a photo or subject
// UID.
func (s *Store) IsFaceConfirmed(ctx context.Context, key FaceConfirmationKey) (bool, error) {
	if !key.valid() {
		return false, ErrEmptyKey
	}
	var exists bool
	err := s.pool.QueryRow(ctx, existsFaceConfirmationSQL,
		key.PhotoUID, key.FaceIndex, key.SubjectUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("feedback: checking face confirmation for %s: %w", key.SubjectUID, err)
	}
	return exists, nil
}

// FaceConfirmationsForSubject returns every face confirmed for subjectUID as
// (photo UID, face index) exclusion keys, ordered deterministically. It is the
// bulk lookup outlier review uses to exclude already-confirmed faces without an
// N+1. A subject with no confirmations yields an empty (non-nil) slice, nil
// error.
func (s *Store) FaceConfirmationsForSubject(ctx context.Context, subjectUID string) ([]FaceRef, error) {
	if subjectUID == "" {
		return nil, ErrEmptyKey
	}
	rows, err := s.pool.Query(ctx, listFaceConfirmationsBySubjectSQL, subjectUID)
	if err != nil {
		return nil, fmt.Errorf("feedback: listing face confirmations for %s: %w", subjectUID, err)
	}
	defer rows.Close()

	refs := []FaceRef{}
	for rows.Next() {
		var ref FaceRef
		if err := rows.Scan(&ref.PhotoUID, &ref.FaceIndex); err != nil {
			return nil, fmt.Errorf("feedback: scanning face confirmation: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback: iterating face confirmations for %s: %w", subjectUID, err)
	}
	return refs, nil
}
