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

// listDuplicateMarkerDismissalsSQL reads every dismissed group of one project
// ($1), in a deterministic order. The project's whole set is read at once because
// the repeated-marker listing recomputes its groups in one pass and needs the
// full exclusion set up front — there is no per-photo entry point to filter by.
// The table carries no project of its own: a group belongs to the project of its
// photo and its subject, and both must be the project's.
const listDuplicateMarkerDismissalsSQL = `
SELECT d.photo_uid, d.subject_uid
FROM duplicate_marker_dismissals d
JOIN photos p ON p.uid = d.photo_uid AND p.project_id = $1
JOIN subjects s ON s.uid = d.subject_uid AND s.project_id = $1
ORDER BY d.photo_uid, d.subject_uid`

// DismissDuplicateMarkers records that the person named by key really is marked
// more than once on key's photo — a double exposure, a mirror, a photo of a photo
// — inside projectUID and writes entry in the same transaction. The write is
// idempotent (dismissing the same group twice is a no-op, not an error),
// dismissed_by is taken from entry.ActorUID (empty stored as NULL) and the
// entry's ProjectUID defaults to projectUID.
//
// It never mutates the markers: none is detached, invalidated or deleted. It only
// records the opinion, which the repeated-marker listing reads to stop offering
// the group. It returns ErrEmptyKey if the key lacks a uid, ErrNoProject without
// a project, or ErrTargetNotFound — writing nothing — unless the photo and the
// subject both exist in projectUID.
func (s *Store) DismissDuplicateMarkers(
	ctx context.Context, projectUID string, key DuplicateMarkerDismissalKey, entry audit.Entry,
) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inProjectAuditedTx(ctx, projectUID, key.PhotoUID, key.SubjectUID, entry, func(tx pgx.Tx) error {
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
// key inside projectUID and writes entry in the same transaction, letting a user
// take the decision back so the group is offered for review again. Un-dismissing
// a group that was never dismissed still records the action but changes no rows.
// It returns ErrEmptyKey if the key lacks a uid, ErrNoProject without a project,
// or ErrTargetNotFound — removing nothing — unless the photo and the subject
// both exist in projectUID.
func (s *Store) UndismissDuplicateMarkers(
	ctx context.Context, projectUID string, key DuplicateMarkerDismissalKey, entry audit.Entry,
) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	return s.inProjectAuditedTx(ctx, projectUID, key.PhotoUID, key.SubjectUID, entry, func(tx pgx.Tx) error {
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

// DismissedDuplicateMarkerGroups returns every dismissed (photo, subject) group of
// projectUID — the photo and the subject both the project's — in a deterministic
// order. It is the bulk lookup the repeated-marker listing uses to drop the
// settled groups in one read, without an N+1. No dismissals yields an empty
// (non-nil) slice, nil error; an empty projectUID is ErrNoProject.
func (s *Store) DismissedDuplicateMarkerGroups(
	ctx context.Context, projectUID string,
) ([]DuplicateMarkerDismissalKey, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	rows, err := s.pool.Query(ctx, listDuplicateMarkerDismissalsSQL, projectUID)
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
