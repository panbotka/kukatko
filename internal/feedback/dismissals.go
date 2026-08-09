// The four exported methods below mirror the confirmation's line for line, and
// deliberately so: the shared machinery already lives in pairs.go, and what is
// left here is the documented public surface of one table — the key type it
// takes, the sentinels it returns and the paragraph explaining why dismissing
// mutates nothing. Folding two documented APIs into one generic pair would make
// both harder to read than the repetition is.
//
//nolint:dupl // the shared implementation is in pairs.go; this is the API surface.
package feedback

import (
	"context"

	"github.com/panbotka/kukatko/internal/audit"
)

// duplicateDismissals is the "these two photos are NOT duplicates" opinion: the
// table's four statements plus the wording of its errors. The insert is
// ON CONFLICT DO NOTHING, so a repeated dismissal is a no-op rather than a
// unique-constraint error, and the uids are normalised by the caller, so they
// always satisfy the table's ordering CHECK.
var duplicateDismissals = pairOpinion{
	insertSQL: `
INSERT INTO duplicate_dismissals (photo_uid, other_uid, dismissed_by)
VALUES ($1, $2, $3)
ON CONFLICT (photo_uid, other_uid) DO NOTHING`,
	deleteSQL: `
DELETE FROM duplicate_dismissals WHERE photo_uid = $1 AND other_uid = $2`,
	existsSQL: `
SELECT EXISTS (
    SELECT 1 FROM duplicate_dismissals WHERE photo_uid = $1 AND other_uid = $2)`,
	listSQL: `
SELECT photo_uid, other_uid
FROM duplicate_dismissals
ORDER BY photo_uid, other_uid`,
	record: "dismissing duplicate",
	remove: "un-dismissing duplicate",
	check:  "checking duplicate dismissal",
	list:   "duplicate dismissals",
}

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
	pair, err := checkPairKey(key.PhotoUID, key.OtherUID)
	if err != nil {
		return err
	}
	return s.recordPair(ctx, duplicateDismissals, pair, entry)
}

// UndismissDuplicate removes the duplicate dismissal identified by key and writes
// entry in the same transaction, letting a user take the decision back so the pair
// is offered for review again. Un-dismissing a pair that was never dismissed still
// records the action but changes no rows. It returns ErrEmptyKey if the key lacks
// a uid, or ErrSamePhoto if both uids name the same photo.
func (s *Store) UndismissDuplicate(ctx context.Context, key DuplicateDismissalKey, entry audit.Entry) error {
	pair, err := checkPairKey(key.PhotoUID, key.OtherUID)
	if err != nil {
		return err
	}
	return s.removePair(ctx, duplicateDismissals, pair, entry)
}

// IsDuplicateDismissed reports whether the pair identified by key has been
// dismissed. The pair is unordered, so the argument order does not matter. It
// returns ErrEmptyKey if the key lacks a uid, or ErrSamePhoto if both uids name
// the same photo.
func (s *Store) IsDuplicateDismissed(ctx context.Context, key DuplicateDismissalKey) (bool, error) {
	pair, err := checkPairKey(key.PhotoUID, key.OtherUID)
	if err != nil {
		return false, err
	}
	return s.hasPair(ctx, duplicateDismissals, pair)
}

// DismissedDuplicatePairs returns every dismissed pair in canonical (smaller uid
// first) order. It is the bulk lookup duplicate detection uses to drop the
// dismissed edges from its similarity graph in one read, without an N+1. No
// dismissals yields an empty (non-nil) slice, nil error.
func (s *Store) DismissedDuplicatePairs(ctx context.Context) ([]DuplicateDismissalKey, error) {
	pairs, err := s.listPairs(ctx, duplicateDismissals)
	if err != nil {
		return nil, err
	}
	out := make([]DuplicateDismissalKey, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, DuplicateDismissalKey(pair))
	}
	return out, nil
}
