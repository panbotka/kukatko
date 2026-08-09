package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// Sentinel errors returned by the merge, so the HTTP layer can answer 400 before
// anything is written.
var (
	// ErrMergeIntoSelf indicates the source and the keeper are the same subject.
	ErrMergeIntoSelf = errors.New("people: cannot merge a subject into itself")
)

// MergeResult reports what one merge moved from the source onto the keeper. The
// counts are what the audit trail records and what the UI reports back, so a
// merge is legible after the fact even though the source is gone.
type MergeResult struct {
	// KeeperUID is the surviving subject.
	KeeperUID string `json:"keeper_uid"`
	// SourceUID is the subject that was merged away and deleted.
	SourceUID string `json:"source_uid"`
	// MarkersMoved is how many markers were repointed at the keeper.
	MarkersMoved int `json:"markers_moved"`
	// FacesMoved is how many rows of the denormalised faces cache were repointed.
	FacesMoved int `json:"faces_moved"`
	// ConfirmationsMoved is how many "yes, this is them" opinions were carried over.
	ConfirmationsMoved int `json:"confirmations_moved"`
	// RejectionsMoved is how many "no, this is not them" opinions were carried over.
	RejectionsMoved int `json:"rejections_moved"`
	// RejectionsDropped is how many rejections the merge discarded because the
	// merged person is assigned to — or confirmed on — that very face. See
	// MergeSubjectsAudited for the precedence rule.
	RejectionsDropped int `json:"rejections_dropped"`
	// DismissalsMoved is how many repeated-marker dismissals were carried over.
	DismissalsMoved int `json:"dismissals_moved"`
	// SharedPhotos is how many photos carried a marker of *both* subjects. Those
	// markers are all kept, so each such photo becomes a repeated-marker group for
	// `GET /duplicate-markers` to surface.
	SharedPhotos int `json:"shared_photos"`
}

// lockMergePairSQL loads the two subjects of a merge and locks their rows for the
// rest of the transaction. Both are selected in one statement ordered by uid, so
// two merges naming the same pair in opposite directions take the locks in the
// same order and queue instead of deadlocking.
const lockMergePairSQL = "SELECT " + subjectColumns +
	" FROM subjects WHERE uid = $1 OR uid = $2 ORDER BY uid FOR UPDATE"

// sharedPhotoUIDsSQL lists the photos that carry a marker of both subjects. It
// must run before the markers move, while the two subjects are still distinct.
const sharedPhotoUIDsSQL = `
SELECT DISTINCT s.photo_uid
FROM markers s
JOIN markers k ON k.photo_uid = s.photo_uid AND k.subject_uid = $2
WHERE s.subject_uid = $1`

// dropContradictedRejectionsSQL removes the keeper's "this face is not them"
// rejections for the faces the source is assigned to or has confirmed — the
// evidence about to become the keeper's own.
const dropContradictedRejectionsSQL = `
DELETE FROM face_rejections r
WHERE r.subject_uid = $2
  AND (EXISTS (SELECT 1 FROM faces f
               WHERE f.subject_uid = $1
                 AND f.photo_uid = r.photo_uid AND f.face_index = r.face_index)
       OR EXISTS (SELECT 1 FROM face_confirmations c
                  WHERE c.subject_uid = $1
                    AND c.photo_uid = r.photo_uid AND c.face_index = r.face_index))`

// moveMarkersSQL repoints every marker of the source at the keeper. Nothing is
// deduplicated: a photo that carried a marker of each subject keeps both.
const moveMarkersSQL = "UPDATE markers SET subject_uid = $2, updated_at = now() WHERE subject_uid = $1"

// moveFacesCacheSQL repoints the denormalised faces cache at the keeper. The
// faces table has no foreign key to subjects (faces are re-created on every
// re-detection), so this package keeps it in step by hand.
const moveFacesCacheSQL = "UPDATE faces SET subject_uid = $2, subject_name = $3 WHERE subject_uid = $1"

// moveConfirmationsSQL carries the source's face confirmations onto the keeper,
// keeping who confirmed and when. A confirmation the keeper already holds is left
// alone by the natural key's ON CONFLICT.
const moveConfirmationsSQL = `
INSERT INTO face_confirmations (photo_uid, face_index, subject_uid, confirmed_by, confirmed_at)
SELECT photo_uid, face_index, $2, confirmed_by, confirmed_at
FROM face_confirmations WHERE subject_uid = $1
ON CONFLICT (photo_uid, face_index, subject_uid) DO NOTHING`

// contradictedRejectionExpr is the boolean naming a rejection row `r` the
// keeper's own evidence contradicts: the keeper is assigned to that face, or has
// confirmed it. It is used twice — once to count those rows, once to skip them.
// The keeper's uid is cast explicitly because the move below also selects the
// same parameter into an inserted column, and Postgres refuses to deduce two
// types for one parameter.
const contradictedRejectionExpr = `(EXISTS (SELECT 1 FROM faces f
               WHERE f.subject_uid = $2::varchar
                 AND f.photo_uid = r.photo_uid AND f.face_index = r.face_index)
       OR EXISTS (SELECT 1 FROM face_confirmations c
                  WHERE c.subject_uid = $2::varchar
                    AND c.photo_uid = r.photo_uid AND c.face_index = r.face_index))`

// countContradictedRejectionsSQL counts the source rejections the move below
// deliberately leaves behind, so the result can report them as dropped.
const countContradictedRejectionsSQL = `
SELECT count(*) FROM face_rejections r
WHERE r.subject_uid = $1 AND ` + contradictedRejectionExpr

// moveRejectionsSQL carries the source's face rejections onto the keeper, except
// the ones the keeper's own (by now, merged) evidence contradicts.
const moveRejectionsSQL = `
INSERT INTO face_rejections (photo_uid, face_index, subject_uid, rejected_by, rejected_at)
SELECT r.photo_uid, r.face_index, $2, r.rejected_by, r.rejected_at
FROM face_rejections r
WHERE r.subject_uid = $1 AND NOT ` + contradictedRejectionExpr + `
ON CONFLICT (photo_uid, face_index, subject_uid) DO NOTHING`

// moveDismissalsSQL carries the source's repeated-marker dismissals onto the
// keeper, except on the photos where the merge itself creates a new repeated
// marker group — see MergeSubjectsAudited for why those are left undismissed.
const moveDismissalsSQL = `
INSERT INTO duplicate_marker_dismissals (photo_uid, subject_uid, dismissed_by, dismissed_at)
SELECT d.photo_uid, $2, d.dismissed_by, d.dismissed_at
FROM duplicate_marker_dismissals d
WHERE d.subject_uid = $1 AND d.photo_uid <> ALL($3::text[])
ON CONFLICT (photo_uid, subject_uid) DO NOTHING`

// fillKeeperSQL fills the keeper's empty fields from the source without ever
// overwriting one it already has: the two flags are OR-ed (a merge must not undo
// a "favorite" or weaken a "private"), and the notes and cover are filled only
// when the keeper has none.
const fillKeeperSQL = `
UPDATE subjects SET
    favorite = favorite OR $2,
    private = private OR $3,
    notes = CASE WHEN notes = '' THEN $4 ELSE notes END,
    cover_photo_uid = COALESCE(cover_photo_uid, $5),
    updated_at = now()
WHERE uid = $1`

// MergeSubjectsAudited merges the subject identified by sourceUID into keeperUID
// and writes entry in the same transaction, so the whole merge and the record of
// who made it commit atomically. entry's TargetUID defaults to keeperUID. It
// returns ErrMergeIntoSelf when the two are the same subject and
// ErrSubjectNotFound when either does not exist — each before anything is
// written.
//
// Everything the source carried ends up on the keeper: its markers, the
// denormalised faces cache, its face confirmations and rejections, and its
// repeated-marker dismissals. The source subject row is then deleted, and with it
// (by ON DELETE CASCADE) any of its feedback rows the merge deliberately did not
// carry. Nothing is left pointing at a subject that no longer exists.
//
// A subject's favorite flag is instance-wide, not per user — user_favorites keys
// photos, never people — so there is no per-user preference to carry: the flag is
// simply OR-ed onto the keeper by fillKeeper, along with the other fields it has
// none of.
//
// Three rules decide the cases where the two subjects disagree.
//
// Markers are never deduplicated. A photo that carried a marker of each subject
// keeps both, so no region and no assignment is silently thrown away; the photo
// simply becomes a repeated-marker group that `GET /duplicate-markers`
// (internal/dupmarkers) surfaces for review, which is exactly the tool for
// deciding whether two boxes on one photo are one mistake or two correct faces.
// The count of those photos is reported as SharedPhotos.
//
// Feedback conflicts are resolved in favour of the positive record: an
// assignment or a confirmation beats a rejection, whichever side each came from.
// A face the source is assigned to (or has confirmed) drops the keeper's "that is
// not them" for it, and a rejection of the source's is not carried onto a face
// the keeper is assigned to or has confirmed. The reason is asymmetry of cost: a
// rejection is only ever used to exclude a face from a *search*, while an
// assignment is a statement about the library itself, so keeping a contradicting
// rejection could only ever hide a face the merged person demonstrably owns.
//
// A repeated-marker dismissal ("yes, this person really is marked twice here") is
// carried over except on the photos where the merge is what puts the second
// marker there. Those groups are new, nobody has judged them yet, and carrying a
// dismissal about a different pair of boxes onto them would hide precisely the
// duplicates this merge just created.
func (s *Store) MergeSubjectsAudited(
	ctx context.Context, sourceUID, keeperUID string, entry audit.Entry,
) (MergeResult, error) {
	if sourceUID == keeperUID {
		return MergeResult{}, ErrMergeIntoSelf
	}
	if entry.TargetUID == "" {
		entry.TargetUID = keeperUID
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (MergeResult, error) {
		return mergeSubjectsTx(ctx, tx, sourceUID, keeperUID)
	})
}

// mergeSubjectsTx performs the whole merge inside tx. The order is load-bearing
// throughout, because every step but the first reads state an earlier one has
// already rewritten:
//
//   - the two subjects are locked, and the shared photos noted, while the markers
//     still tell the two apart;
//   - the keeper's contradicted rejections are dropped while the faces cache and
//     the confirmations still name the source, which is what identifies them;
//   - then the assignments move, so the rest of the feedback sees the merged
//     person's evidence as one set;
//   - and only then is the source removed.
func mergeSubjectsTx(ctx context.Context, tx pgx.Tx, sourceUID, keeperUID string) (MergeResult, error) {
	source, keeper, err := lockMergePair(ctx, tx, sourceUID, keeperUID)
	if err != nil {
		return MergeResult{}, err
	}
	shared, err := sharedPhotoUIDs(ctx, tx, sourceUID, keeperUID)
	if err != nil {
		return MergeResult{}, err
	}
	res := MergeResult{KeeperUID: keeperUID, SourceUID: sourceUID, SharedPhotos: len(shared)}
	if err := dropContradictedRejections(ctx, tx, sourceUID, keeperUID, &res); err != nil {
		return MergeResult{}, err
	}
	if err := moveAssignments(ctx, tx, source, keeper, &res); err != nil {
		return MergeResult{}, err
	}
	if err := moveFeedback(ctx, tx, sourceUID, keeperUID, shared, &res); err != nil {
		return MergeResult{}, err
	}
	if err := fillKeeper(ctx, tx, source, keeper); err != nil {
		return MergeResult{}, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM subjects WHERE uid = $1", sourceUID); err != nil {
		return MergeResult{}, fmt.Errorf("people: deleting merged subject %s: %w", sourceUID, err)
	}
	return res, nil
}

// lockMergePair loads both subjects and holds a row lock on each for the rest of
// the transaction, so a concurrent merge or edit of either cannot interleave with
// this one. A pair where either side is missing (or where both uids resolve to
// one row) returns ErrSubjectNotFound.
func lockMergePair(ctx context.Context, tx pgx.Tx, sourceUID, keeperUID string) (Subject, Subject, error) {
	rows, err := tx.Query(ctx, lockMergePairSQL, sourceUID, keeperUID)
	if err != nil {
		return Subject{}, Subject{}, fmt.Errorf("people: locking merge pair: %w", err)
	}
	defer rows.Close()

	byUID := make(map[string]Subject, 2)
	for rows.Next() {
		subj, scanErr := scanSubject(rows)
		if scanErr != nil {
			return Subject{}, Subject{}, scanErr
		}
		byUID[subj.UID] = subj
	}
	if err := rows.Err(); err != nil {
		return Subject{}, Subject{}, fmt.Errorf("people: iterating merge pair: %w", err)
	}
	source, okSource := byUID[sourceUID]
	keeper, okKeeper := byUID[keeperUID]
	if !okSource || !okKeeper {
		return Subject{}, Subject{}, ErrSubjectNotFound
	}
	return source, keeper, nil
}

// sharedPhotoUIDs returns the photos carrying a marker of both subjects, which
// are the photos the merge turns into repeated-marker groups.
func sharedPhotoUIDs(ctx context.Context, tx pgx.Tx, sourceUID, keeperUID string) ([]string, error) {
	rows, err := tx.Query(ctx, sharedPhotoUIDsSQL, sourceUID, keeperUID)
	if err != nil {
		return nil, fmt.Errorf("people: listing shared photos of %s and %s: %w", sourceUID, keeperUID, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("people: scanning shared photo uid: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating shared photos: %w", err)
	}
	return out, nil
}

// moveAssignments repoints the source's markers and its rows of the faces cache
// at the keeper, recording both counts on res.
func moveAssignments(ctx context.Context, tx pgx.Tx, source, keeper Subject, res *MergeResult) error {
	tag, err := tx.Exec(ctx, moveMarkersSQL, source.UID, keeper.UID)
	if err != nil {
		return fmt.Errorf("people: moving markers of %s to %s: %w", source.UID, keeper.UID, err)
	}
	res.MarkersMoved = int(tag.RowsAffected())

	tag, err = tx.Exec(ctx, moveFacesCacheSQL, source.UID, keeper.UID, keeper.Name)
	if err != nil {
		return fmt.Errorf("people: moving faces cache of %s to %s: %w", source.UID, keeper.UID, err)
	}
	res.FacesMoved = int(tag.RowsAffected())
	return nil
}

// dropContradictedRejections removes the keeper's "this face is not them" for
// every face the source is assigned to or has confirmed. It must run before the
// assignments and confirmations move, since naming the source is what identifies
// those rows.
func dropContradictedRejections(
	ctx context.Context, tx pgx.Tx, sourceUID, keeperUID string, res *MergeResult,
) error {
	dropped, err := tx.Exec(ctx, dropContradictedRejectionsSQL, sourceUID, keeperUID)
	if err != nil {
		return fmt.Errorf("people: dropping contradicted rejections of %s: %w", keeperUID, err)
	}
	res.RejectionsDropped = int(dropped.RowsAffected())
	return nil
}

// moveFeedback carries the source's opinions onto the keeper under the merge's
// precedence rules: a rejection the keeper's (by now merged) evidence contradicts
// is left behind, everything else is carried, and a dismissal is not carried onto
// a photo the merge itself turns into a repeated-marker group.
func moveFeedback(
	ctx context.Context, tx pgx.Tx, sourceUID, keeperUID string, shared []string, res *MergeResult,
) error {
	moved, err := tx.Exec(ctx, moveConfirmationsSQL, sourceUID, keeperUID)
	if err != nil {
		return fmt.Errorf("people: moving confirmations of %s to %s: %w", sourceUID, keeperUID, err)
	}
	res.ConfirmationsMoved = int(moved.RowsAffected())

	var skipped int
	if err := tx.QueryRow(ctx, countContradictedRejectionsSQL, sourceUID, keeperUID).Scan(&skipped); err != nil {
		return fmt.Errorf("people: counting contradicted rejections of %s: %w", sourceUID, err)
	}
	res.RejectionsDropped += skipped

	moved, err = tx.Exec(ctx, moveRejectionsSQL, sourceUID, keeperUID)
	if err != nil {
		return fmt.Errorf("people: moving rejections of %s to %s: %w", sourceUID, keeperUID, err)
	}
	res.RejectionsMoved = int(moved.RowsAffected())

	moved, err = tx.Exec(ctx, moveDismissalsSQL, sourceUID, keeperUID, shared)
	if err != nil {
		return fmt.Errorf("people: moving marker dismissals of %s to %s: %w", sourceUID, keeperUID, err)
	}
	res.DismissalsMoved = int(moved.RowsAffected())
	return nil
}

// fillKeeper fills the keeper's empty fields from the source. It never overwrites
// a value the keeper already carries — the keeper is the person the user chose to
// keep, so its own record wins wherever it says anything.
func fillKeeper(ctx context.Context, tx pgx.Tx, source, keeper Subject) error {
	if _, err := tx.Exec(ctx, fillKeeperSQL,
		keeper.UID, source.Favorite, source.Private, source.Notes, source.CoverPhotoUID,
	); err != nil {
		return fmt.Errorf("people: filling keeper %s from %s: %w", keeper.UID, source.UID, err)
	}
	return nil
}
