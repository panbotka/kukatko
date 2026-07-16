package feedback

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// insertDuplicateDismissalSQL records that two photos are NOT duplicates of each
// other. ON CONFLICT DO NOTHING makes a repeated dismissal a no-op rather than a
// unique-constraint error. The uids are normalised by the caller, so they always
// satisfy the table's ordering CHECK.
const insertDuplicateDismissalSQL = `
INSERT INTO duplicate_dismissals (photo_uid, other_uid, dismissed_by)
VALUES ($1, $2, $3)
ON CONFLICT (photo_uid, other_uid) DO NOTHING`

// deleteDuplicateDismissalSQL removes a duplicate dismissal; deleting a pair that
// was never dismissed affects no rows and is a no-op.
const deleteDuplicateDismissalSQL = `
DELETE FROM duplicate_dismissals WHERE photo_uid = $1 AND other_uid = $2`

// existsDuplicateDismissalSQL checks whether a pair has been dismissed.
const existsDuplicateDismissalSQL = `
SELECT EXISTS (
    SELECT 1 FROM duplicate_dismissals WHERE photo_uid = $1 AND other_uid = $2)`

// listDuplicateDismissalsSQL reads every dismissed pair, in a deterministic order.
// The whole table is read at once because duplicate detection scans the catalogue
// in one pass and needs the full exclusion set up front — there is no per-photo
// entry point to filter by.
const listDuplicateDismissalsSQL = `
SELECT photo_uid, other_uid
FROM duplicate_dismissals
ORDER BY photo_uid, other_uid`

// DismissDuplicate records that the two photos named by key are NOT duplicates of
// each other and writes entry in the same transaction. The pair is unordered:
// dismissing (A,B) and (B,A) records the one same decision. The write is
// idempotent — dismissing the same pair twice is a no-op, not an error — and
// dismissed_by is taken from entry.ActorUID (empty stored as NULL).
//
// It never mutates either photo: nothing is archived, merged or deleted. It only
// records the opinion, which later duplicate scans read to stop linking the pair.
// It returns ErrEmptyKey if the key lacks a uid, ErrSamePhoto if both uids name
// the same photo, or ErrTargetNotFound if either photo does not exist.
func (s *Store) DismissDuplicate(ctx context.Context, key DuplicateDismissalKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	if key.PhotoUID == key.OtherUID {
		return ErrSamePhoto
	}
	k := key.normalized()
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, insertDuplicateDismissalSQL,
			k.PhotoUID, k.OtherUID, nullable(entry.ActorUID))
		if isForeignKeyViolation(err) {
			return ErrTargetNotFound
		}
		if err != nil {
			return fmt.Errorf("feedback: dismissing duplicate pair %s/%s: %w",
				k.PhotoUID, k.OtherUID, err)
		}
		return nil
	})
}

// UndismissDuplicate removes the duplicate dismissal identified by key and writes
// entry in the same transaction, letting a user take the decision back so the pair
// is offered for review again. Un-dismissing a pair that was never dismissed still
// records the action but changes no rows. It returns ErrEmptyKey if the key lacks
// a uid, or ErrSamePhoto if both uids name the same photo.
func (s *Store) UndismissDuplicate(ctx context.Context, key DuplicateDismissalKey, entry audit.Entry) error {
	if !key.valid() {
		return ErrEmptyKey
	}
	if key.PhotoUID == key.OtherUID {
		return ErrSamePhoto
	}
	k := key.normalized()
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, deleteDuplicateDismissalSQL, k.PhotoUID, k.OtherUID)
		if err != nil {
			return fmt.Errorf("feedback: un-dismissing duplicate pair %s/%s: %w",
				k.PhotoUID, k.OtherUID, err)
		}
		return nil
	})
}

// IsDuplicateDismissed reports whether the pair identified by key has been
// dismissed. The pair is unordered, so the argument order does not matter. It
// returns ErrEmptyKey if the key lacks a uid, or ErrSamePhoto if both uids name
// the same photo.
func (s *Store) IsDuplicateDismissed(ctx context.Context, key DuplicateDismissalKey) (bool, error) {
	if !key.valid() {
		return false, ErrEmptyKey
	}
	if key.PhotoUID == key.OtherUID {
		return false, ErrSamePhoto
	}
	k := key.normalized()
	var exists bool
	err := s.pool.QueryRow(ctx, existsDuplicateDismissalSQL, k.PhotoUID, k.OtherUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("feedback: checking duplicate dismissal %s/%s: %w",
			k.PhotoUID, k.OtherUID, err)
	}
	return exists, nil
}

// DismissedDuplicatePairs returns every dismissed pair in canonical (smaller uid
// first) order. It is the bulk lookup duplicate detection uses to drop the
// dismissed edges from its similarity graph in one read, without an N+1. No
// dismissals yields an empty (non-nil) slice, nil error.
func (s *Store) DismissedDuplicatePairs(ctx context.Context) ([]DuplicateDismissalKey, error) {
	rows, err := s.pool.Query(ctx, listDuplicateDismissalsSQL)
	if err != nil {
		return nil, fmt.Errorf("feedback: listing duplicate dismissals: %w", err)
	}
	defer rows.Close()

	pairs := []DuplicateDismissalKey{}
	for rows.Next() {
		var pair DuplicateDismissalKey
		if err := rows.Scan(&pair.PhotoUID, &pair.OtherUID); err != nil {
			return nil, fmt.Errorf("feedback: scanning duplicate dismissal: %w", err)
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback: iterating duplicate dismissals: %w", err)
	}
	return pairs, nil
}
