// The four exported methods below mirror the dismissal's line for line, and
// deliberately so: the shared machinery already lives in pairs.go, and what is
// left here is the documented public surface of one table — the key type it
// takes, the sentinels it returns and the paragraph explaining why confirming
// merges nothing. Folding two documented APIs into one generic pair would make
// both harder to read than the repetition is.
//
//nolint:dupl // the shared implementation is in pairs.go; this is the API surface.
package feedback

import (
	"context"

	"github.com/panbotka/kukatko/internal/audit"
)

// duplicateConfirmations is the "yes, this really IS the same photo twice"
// opinion — the positive mirror of duplicateDismissals, on a table of the same
// shape (see migration 0054).
var duplicateConfirmations = pairOpinion{
	insertSQL: `
INSERT INTO duplicate_confirmations (photo_uid, other_uid, confirmed_by)
VALUES ($1, $2, $3)
ON CONFLICT (photo_uid, other_uid) DO NOTHING`,
	deleteSQL: `
DELETE FROM duplicate_confirmations WHERE photo_uid = $1 AND other_uid = $2`,
	existsSQL: `
SELECT EXISTS (
    SELECT 1 FROM duplicate_confirmations WHERE photo_uid = $1 AND other_uid = $2)`,
	listSQL: `
SELECT photo_uid, other_uid
FROM duplicate_confirmations
ORDER BY photo_uid, other_uid`,
	record: "confirming duplicate",
	remove: "un-confirming duplicate",
	check:  "checking duplicate confirmation",
	list:   "duplicate confirmations",
}

// ConfirmDuplicate records that the two photos named by key really are the same
// shot and writes entry in the same transaction. The pair is unordered:
// confirming (A,B) and (B,A) records the one same decision. The write is
// idempotent — confirming the same pair twice is a no-op — and confirmed_by is
// taken from entry.ActorUID (empty stored as NULL).
//
// It never mutates either photo and never merges them: agreeing that two files
// are the same shot and deciding which copy to keep are different acts, and only
// the second one destroys anything. The duplicates page reads this opinion to
// rank a group a human has already judged above the ones nobody has looked at.
// It returns ErrEmptyKey if the key lacks a uid, ErrSamePhoto if both uids name
// the same photo, or ErrTargetNotFound if either photo does not exist.
func (s *Store) ConfirmDuplicate(ctx context.Context, key DuplicateConfirmationKey, entry audit.Entry) error {
	pair, err := checkPairKey(key.PhotoUID, key.OtherUID)
	if err != nil {
		return err
	}
	return s.recordPair(ctx, duplicateConfirmations, pair, entry)
}

// UnconfirmDuplicate removes the duplicate confirmation identified by key and
// writes entry in the same transaction, letting a user take the decision back so
// the group drops out of the human-confirmed ranking. Un-confirming a pair that
// was never confirmed still records the action but changes no rows. It returns
// ErrEmptyKey if the key lacks a uid, or ErrSamePhoto if both uids name the same
// photo.
func (s *Store) UnconfirmDuplicate(ctx context.Context, key DuplicateConfirmationKey, entry audit.Entry) error {
	pair, err := checkPairKey(key.PhotoUID, key.OtherUID)
	if err != nil {
		return err
	}
	return s.removePair(ctx, duplicateConfirmations, pair, entry)
}

// IsDuplicateConfirmed reports whether the pair identified by key has been
// confirmed. The pair is unordered, so the argument order does not matter. It
// returns ErrEmptyKey if the key lacks a uid, or ErrSamePhoto if both uids name
// the same photo.
func (s *Store) IsDuplicateConfirmed(ctx context.Context, key DuplicateConfirmationKey) (bool, error) {
	pair, err := checkPairKey(key.PhotoUID, key.OtherUID)
	if err != nil {
		return false, err
	}
	return s.hasPair(ctx, duplicateConfirmations, pair)
}

// ConfirmedDuplicatePairs returns every confirmed pair in canonical (smaller uid
// first) order. It is the bulk lookup duplicate detection uses to mark the
// human-confirmed groups in one read, without an N+1. No confirmations yields an
// empty (non-nil) slice, nil error.
func (s *Store) ConfirmedDuplicatePairs(ctx context.Context) ([]DuplicateConfirmationKey, error) {
	pairs, err := s.listPairs(ctx, duplicateConfirmations)
	if err != nil {
		return nil, err
	}
	out := make([]DuplicateConfirmationKey, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, DuplicateConfirmationKey(pair))
	}
	return out, nil
}
