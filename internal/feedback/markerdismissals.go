package feedback

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// insertDuplicateMarkerDismissalSQL records that a person really is marked more
// than once on a photo. ON CONFLICT DO NOTHING makes a repeated dismissal a no-op
// rather than a unique-constraint error.
const insertDuplicateMarkerDismissalSQL = `
INSERT INTO duplicate_marker_dismissals (photo_uid, subject_uid, dismissed_by)
VALUES ($1, $2, $3)
ON CONFLICT (photo_uid, subject_uid) DO NOTHING`

// deleteDuplicateMarkerDismissalSQL removes a repeated-marker dismissal; deleting
// a group that was never dismissed affects no rows and is a no-op.
const deleteDuplicateMarkerDismissalSQL = `
DELETE FROM duplicate_marker_dismissals WHERE photo_uid = $1 AND subject_uid = $2`

// existsDuplicateMarkerDismissalSQL checks whether a (photo, subject) group has
// been dismissed.
const existsDuplicateMarkerDismissalSQL = `
SELECT EXISTS (
    SELECT 1 FROM duplicate_marker_dismissals
    WHERE photo_uid = $1 AND subject_uid = $2)`

// listDuplicateMarkerDismissalsSQL reads every dismissed group, in a deterministic
// order. The whole table is read at once because the repeated-marker listing
// recomputes its groups in one pass and needs the full exclusion set up front —
// there is no per-photo entry point to filter by.
const listDuplicateMarkerDismissalsSQL = `
SELECT photo_uid, subject_uid
FROM duplicate_marker_dismissals
ORDER BY photo_uid, subject_uid`

// DismissDuplicateMarkers records that the person named by key really is marked
// more than once on key's photo — a double exposure, a mirror, a photo of a photo
// — and writes entry in the same transaction. The write is idempotent (dismissing
// the same group twice is a no-op, not an error) and dismissed_by is taken from
// entry.ActorUID (empty stored as NULL).
//
// It never mutates the markers: none is detached, invalidated or deleted. It only
// records the opinion, which the repeated-marker listing reads to stop offering
// the group. It returns ErrEmptyKey if the key lacks a uid, or ErrTargetNotFound
// if the photo or subject does not exist.
func (s *Store) DismissDuplicateMarkers(
	ctx context.Context, key DuplicateMarkerDismissalKey, entry audit.Entry,
) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertDuplicateMarkerDismissalSQL,
			key.PhotoUID, key.SubjectUID, nullable(entry.ActorUID))
		if isForeignKeyViolation(err) {
			return ErrTargetNotFound
		}
		if err != nil {
			return fmt.Errorf("feedback: dismissing repeated markers of %s on %s: %w",
				key.SubjectUID, key.PhotoUID, err)
		}
		return nil
	})
}

// UndismissDuplicateMarkers removes the repeated-marker dismissal identified by
// key and writes entry in the same transaction, letting a user take the decision
// back so the group is offered for review again. Un-dismissing a group that was
// never dismissed still records the action but changes no rows. It returns
// ErrEmptyKey if the key lacks a uid.
func (s *Store) UndismissDuplicateMarkers(
	ctx context.Context, key DuplicateMarkerDismissalKey, entry audit.Entry,
) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, deleteDuplicateMarkerDismissalSQL, key.PhotoUID, key.SubjectUID)
		if err != nil {
			return fmt.Errorf("feedback: un-dismissing repeated markers of %s on %s: %w",
				key.SubjectUID, key.PhotoUID, err)
		}
		return nil
	})
}

// IsDuplicateMarkersDismissed reports whether the (photo, subject) group
// identified by key has been dismissed. It returns ErrEmptyKey if the key lacks a
// uid.
func (s *Store) IsDuplicateMarkersDismissed(
	ctx context.Context, key DuplicateMarkerDismissalKey,
) (bool, error) {
	if !key.valid() {
		return false, ErrEmptyKey
	}
	var exists bool
	err := s.pool.QueryRow(ctx, existsDuplicateMarkerDismissalSQL,
		key.PhotoUID, key.SubjectUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("feedback: checking repeated-marker dismissal %s/%s: %w",
			key.PhotoUID, key.SubjectUID, err)
	}
	return exists, nil
}

// DismissedDuplicateMarkerGroups returns every dismissed (photo, subject) group in
// a deterministic order. It is the bulk lookup the repeated-marker listing uses to
// drop the settled groups in one read, without an N+1. No dismissals yields an
// empty (non-nil) slice, nil error.
func (s *Store) DismissedDuplicateMarkerGroups(
	ctx context.Context,
) ([]DuplicateMarkerDismissalKey, error) {
	rows, err := s.pool.Query(ctx, listDuplicateMarkerDismissalsSQL)
	if err != nil {
		return nil, fmt.Errorf("feedback: listing repeated-marker dismissals: %w", err)
	}
	defer rows.Close()

	groups := []DuplicateMarkerDismissalKey{}
	for rows.Next() {
		var group DuplicateMarkerDismissalKey
		if err := rows.Scan(&group.PhotoUID, &group.SubjectUID); err != nil {
			return nil, fmt.Errorf("feedback: scanning repeated-marker dismissal: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback: iterating repeated-marker dismissals: %w", err)
	}
	return groups, nil
}
