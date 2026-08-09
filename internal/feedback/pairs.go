package feedback

// The shared machinery behind the two opinions about an unordered pair of
// photos: "these two are genuinely different" (a dismissal) and "yes, this
// really is the same photo twice" (a confirmation).
//
// The two tables are the same shape down to the ordered-pair CHECK, and the four
// operations on them differ only in which statements they run and what they are
// called in an error. Writing them twice is how the second one silently drifts
// from the first — a missed normalisation, a foreign-key violation mapped to the
// wrong sentinel — so the bodies live here once and each opinion keeps only its
// SQL, its key type and its documentation.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// photoPair is an unordered pair of photos in its canonical form: the
// lexicographically smaller uid first, matching what the tables' CHECK
// constraints enforce (with COLLATE "C", so the database orders a pair exactly
// as Go's `<` does).
type photoPair struct {
	// PhotoUID is the smaller uid of the pair, OtherUID the larger.
	PhotoUID string
	OtherUID string
}

// pairOpinion is one unordered-pair opinion table: its four statements plus the
// noun its error messages use ("dismissing", "confirming", …).
type pairOpinion struct {
	// insertSQL records the opinion, ON CONFLICT DO NOTHING.
	insertSQL string
	// deleteSQL takes it back; deleting what was never recorded is a no-op.
	deleteSQL string
	// existsSQL checks one pair.
	existsSQL string
	// listSQL reads the whole table in a deterministic order.
	listSQL string
	// record, remove, check and list are the gerunds the four operations use in
	// their error messages, so a failure names what was being attempted.
	record, remove, check, list string
}

// recordPair inserts the opinion for k and writes entry in the same
// transaction. It is idempotent, and a foreign-key violation (a photo that does
// not exist) becomes ErrTargetNotFound rather than an opaque database error.
func (s *Store) recordPair(
	ctx context.Context, op pairOpinion, k photoPair, entry audit.Entry,
) error {
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, op.insertSQL, k.PhotoUID, k.OtherUID, nullable(entry.ActorUID))
		if isForeignKeyViolation(err) {
			return ErrTargetNotFound
		}
		if err != nil {
			return fmt.Errorf("feedback: %s pair %s/%s: %w", op.record, k.PhotoUID, k.OtherUID, err)
		}
		return nil
	})
}

// removePair deletes the opinion for k and writes entry in the same
// transaction. Removing what was never recorded still records the action but
// changes no rows.
func (s *Store) removePair(
	ctx context.Context, op pairOpinion, k photoPair, entry audit.Entry,
) error {
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, op.deleteSQL, k.PhotoUID, k.OtherUID)
		if err != nil {
			return fmt.Errorf("feedback: %s pair %s/%s: %w", op.remove, k.PhotoUID, k.OtherUID, err)
		}
		return nil
	})
}

// hasPair reports whether the opinion is recorded for k.
func (s *Store) hasPair(ctx context.Context, op pairOpinion, k photoPair) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, op.existsSQL, k.PhotoUID, k.OtherUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("feedback: %s %s/%s: %w", op.check, k.PhotoUID, k.OtherUID, err)
	}
	return exists, nil
}

// listPairs reads every recorded pair in canonical order. It is the bulk lookup
// duplicate detection uses, because the scan walks the catalogue in one pass and
// needs the whole set up front — there is no per-photo entry point to filter by.
// An empty table yields an empty (non-nil) slice.
func (s *Store) listPairs(ctx context.Context, op pairOpinion) ([]photoPair, error) {
	rows, err := s.pool.Query(ctx, op.listSQL)
	if err != nil {
		return nil, fmt.Errorf("feedback: listing %s: %w", op.list, err)
	}
	defer rows.Close()

	pairs := []photoPair{}
	for rows.Next() {
		var pair photoPair
		if err := rows.Scan(&pair.PhotoUID, &pair.OtherUID); err != nil {
			return nil, fmt.Errorf("feedback: scanning %s: %w", op.list, err)
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback: iterating %s: %w", op.list, err)
	}
	return pairs, nil
}

// checkPairKey validates the two identifiers every pair opinion needs and
// returns the canonical form. An incomplete key is ErrEmptyKey and a key naming
// one photo twice is ErrSamePhoto — told apart deliberately, so a caller can
// distinguish a missing field from an impossible pair.
func checkPairKey(photoUID, otherUID string) (photoPair, error) {
	if photoUID == "" || otherUID == "" {
		return photoPair{}, ErrEmptyKey
	}
	if photoUID == otherUID {
		return photoPair{}, ErrSamePhoto
	}
	if otherUID < photoUID {
		photoUID, otherUID = otherUID, photoUID
	}
	return photoPair{PhotoUID: photoUID, OtherUID: otherUID}, nil
}
