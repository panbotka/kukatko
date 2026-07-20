package photos

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// StackCandidate is the minimal projection of a photo the stack detector needs to
// decide whether two rows are the same shot: its name (for the base-name, copy
// and edit rules), its capture time and GPS (for the loose time+GPS rule), its
// EXIF unique id (for the reliable identifier rule) and the file characteristics
// that pick the primary. Only unstacked, non-archived photos are candidates.
type StackCandidate struct {
	UID          string
	FileName     string
	OriginalName string
	TakenAt      *time.Time
	Lat          *float64
	Lng          *float64
	MediaType    string
	FileWidth    int
	FileHeight   int
	FileSize     int64
	FileMime     string
	// UniqueID is the photo's EXIF ImageUniqueID, or its XMP InstanceID when the
	// former is absent, or "" when neither exists. Read from the raw EXIF document.
	UniqueID string
}

// primaryElectionOrder orders a stack's members from most to least suitable as
// the primary for the SQL fallback used when a stack loses its primary: a still
// before a video, then higher resolution, then the larger file, then uid for a
// total order. It intentionally omits the RAW-vs-rendered preference (awkward in
// SQL); the detector and manual stacking pick the primary in Go with the full
// rule (see internal/stacks). This fallback only re-elects after a removal.
const primaryElectionOrder = `(media_type = 'video') ASC, ` +
	`(file_width::bigint * file_height::bigint) DESC, file_size DESC, uid ASC`

// stackCandidateColumns is the shared SELECT projection for a StackCandidate: the
// name/time/GPS/EXIF-id fields the detection rules group on plus the file
// characteristics that pick the primary. The EXIF unique id prefers ImageUniqueID
// and falls back to XMP InstanceID, both read out of the raw EXIF document.
const stackCandidateColumns = `uid, file_name, original_name, taken_at, lat, lng, media_type,
       file_width, file_height, file_size, file_mime,
       COALESCE(NULLIF(exif ->> 'ImageUniqueID', ''), NULLIF(exif ->> 'InstanceID', ''), '')`

// ListStackCandidates returns every photo eligible for automatic stacking: the
// unstacked, non-archived rows, each projected to the fields the detection rules
// need. Already-stacked photos are excluded so a re-run never disturbs existing
// (or manually curated) stacks — the property that makes the backfill idempotent
// and resumable. The result is empty (not nil) when nothing is stackable.
func (s *Store) ListStackCandidates(ctx context.Context) ([]StackCandidate, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+stackCandidateColumns+" FROM photos WHERE archived_at IS NULL AND stack_uid IS NULL ORDER BY uid")
	if err != nil {
		return nil, fmt.Errorf("photos: querying stack candidates: %w", err)
	}
	return scanStackCandidates(rows)
}

// StackInfoByUIDs returns the stack-candidate projection for the given (non-
// archived) uids, in uid order. Unlike ListStackCandidates it does not exclude
// already-stacked photos, so manual stacking can rank a selection that mixes
// standalone and previously-stacked photos. Missing or archived uids are simply
// absent, letting the caller detect a bad selection by the returned count.
func (s *Store) StackInfoByUIDs(ctx context.Context, uids []string) ([]StackCandidate, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT "+stackCandidateColumns+" FROM photos WHERE uid = ANY($1) AND archived_at IS NULL ORDER BY uid", uids)
	if err != nil {
		return nil, fmt.Errorf("photos: querying stack info: %w", err)
	}
	return scanStackCandidates(rows)
}

// scanStackCandidates reads every StackCandidate row from rows (closing it) and
// returns them, empty (not nil) when there are none.
func scanStackCandidates(rows pgx.Rows) ([]StackCandidate, error) {
	defer rows.Close()

	out := make([]StackCandidate, 0)
	for rows.Next() {
		var c StackCandidate
		if err := rows.Scan(
			&c.UID, &c.FileName, &c.OriginalName, &c.TakenAt, &c.Lat, &c.Lng, &c.MediaType,
			&c.FileWidth, &c.FileHeight, &c.FileSize, &c.FileMime, &c.UniqueID,
		); err != nil {
			return nil, fmt.Errorf("photos: scanning stack candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating stack candidates: %w", err)
	}
	return out, nil
}

// ListStackMembers returns every photo belonging to the stack, the primary first
// then by uid, each as a full row so callers can present the variants strip and
// re-elect a primary. The result is empty (not nil) when no photo carries the uid.
func (s *Store) ListStackMembers(ctx context.Context, stackUID string) ([]Photo, error) {
	q := "SELECT " + photoColumns + " FROM photos WHERE stack_uid = $1 ORDER BY stack_primary DESC, uid"
	rows, err := s.pool.Query(ctx, q, stackUID)
	if err != nil {
		return nil, fmt.Errorf("photos: querying stack members: %w", err)
	}
	defer rows.Close()

	out := make([]Photo, 0)
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating stack members: %w", err)
	}
	return out, nil
}

// StackCounts returns, for each of the given stack uids that exists, how many
// members it has. Uids with no rows are simply absent from the map. It backs the
// grid's member-count badge, resolving a whole page's stacks in one query. An
// empty input yields an empty map.
func (s *Store) StackCounts(ctx context.Context, stackUIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(stackUIDs))
	if len(stackUIDs) == 0 {
		return counts, nil
	}
	const q = `SELECT stack_uid, count(*) FROM photos WHERE stack_uid = ANY($1) GROUP BY stack_uid`
	rows, err := s.pool.Query(ctx, q, stackUIDs)
	if err != nil {
		return nil, fmt.Errorf("photos: querying stack counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid string
		var n int
		if err := rows.Scan(&uid, &n); err != nil {
			return nil, fmt.Errorf("photos: scanning stack count: %w", err)
		}
		counts[uid] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating stack counts: %w", err)
	}
	return counts, nil
}

// CreateStack groups memberUIDs into one new stack whose primary is primaryUID,
// returning the fresh stack_uid. It is transactional and invariant-preserving:
// members that already belonged to another stack are moved out, and any stack
// they leave is repaired (a singleton remnant is dissolved, a primary-less
// remnant re-elects one). It returns ErrStackTooSmall for fewer than two distinct
// members, ErrPhotoNotFound when a member (or the primary) is missing or archived.
func (s *Store) CreateStack(ctx context.Context, primaryUID string, memberUIDs []string) (string, error) {
	uids := dedupeStrings(memberUIDs)
	if len(uids) < 2 {
		return "", ErrStackTooSmall
	}
	if !slices.Contains(uids, primaryUID) {
		return "", ErrPhotoNotFound
	}
	stackUID, err := newStackUID()
	if err != nil {
		return "", err
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		return applyNewStackTx(ctx, tx, stackUID, primaryUID, uids)
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// applyNewStackTx assigns the fresh stack to its members and repairs the stacks
// they leave, all on tx. It errors with ErrPhotoNotFound when the number of rows
// updated does not match the number of members (one was missing or archived).
func applyNewStackTx(ctx context.Context, tx pgx.Tx, stackUID, primaryUID string, uids []string) error {
	old, err := affectedStacksTx(ctx, tx, uids)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE photos SET stack_uid = $1, stack_primary = (uid = $2), updated_at = now()
		 WHERE uid = ANY($3) AND archived_at IS NULL`,
		stackUID, primaryUID, uids)
	if err != nil {
		return fmt.Errorf("photos: assigning stack: %w", err)
	}
	if tag.RowsAffected() != int64(len(uids)) {
		return ErrPhotoNotFound
	}
	for _, prev := range old {
		if prev == stackUID {
			continue
		}
		if err := repairStackTx(ctx, tx, prev); err != nil {
			return err
		}
	}
	return nil
}

// SetStackPrimary makes memberUID the primary of its stack and returns the
// stack_uid. It clears the previous primary and sets the new one in two
// statements so the one-primary-per-stack index never sees two primaries at once.
// It returns ErrPhotoNotFound when the photo does not exist and ErrPhotoNotStacked
// when it is not a member of any stack.
func (s *Store) SetStackPrimary(ctx context.Context, memberUID string) (string, error) {
	var stackUID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		su, err := memberStackTx(ctx, tx, memberUID)
		if err != nil {
			return err
		}
		stackUID = su
		if _, err := tx.Exec(ctx,
			`UPDATE photos SET stack_primary = false, updated_at = now()
			 WHERE stack_uid = $1 AND stack_primary`, su); err != nil {
			return fmt.Errorf("photos: clearing stack primary: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE photos SET stack_primary = true, updated_at = now() WHERE uid = $1`, memberUID); err != nil {
			return fmt.Errorf("photos: setting stack primary: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// UnstackMember removes memberUID from its stack, turning it back into an ordinary
// standalone photo, and returns the stack_uid it left. The remaining stack is
// repaired: a two-member stack that drops to one is dissolved entirely, and a
// stack that loses its primary re-elects one. It returns ErrPhotoNotFound or
// ErrPhotoNotStacked.
func (s *Store) UnstackMember(ctx context.Context, memberUID string) (string, error) {
	var stackUID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		su, err := memberStackTx(ctx, tx, memberUID)
		if err != nil {
			return err
		}
		stackUID = su
		if _, err := tx.Exec(ctx,
			`UPDATE photos SET stack_uid = NULL, stack_primary = false, updated_at = now()
			 WHERE uid = $1`, memberUID); err != nil {
			return fmt.Errorf("photos: unstacking member: %w", err)
		}
		return repairStackTx(ctx, tx, su)
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// UnstackAll dissolves the entire stack that memberUID belongs to — every member
// becomes a standalone photo again — and returns the dissolved stack_uid. It
// returns ErrPhotoNotFound or ErrPhotoNotStacked.
func (s *Store) UnstackAll(ctx context.Context, memberUID string) (string, error) {
	var stackUID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		su, err := memberStackTx(ctx, tx, memberUID)
		if err != nil {
			return err
		}
		stackUID = su
		if _, err := tx.Exec(ctx,
			`UPDATE photos SET stack_uid = NULL, stack_primary = false, updated_at = now()
			 WHERE stack_uid = $1`, su); err != nil {
			return fmt.Errorf("photos: dissolving stack: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return stackUID, nil
}

// LeaveStackTx takes uid out of whatever stack it belongs to and repairs that
// stack, on tx. It is the shared entry point for the mutations that remove a
// photo from circulation without going through an explicit stack operation —
// archiving, purging and dupmerge's copy-archival. Those must not leave the
// row's stack_uid behind: the default visibility gate is
// (stack_uid IS NULL OR stack_primary), so a stack whose primary left has no
// visible member at all and its still-live siblings silently vanish from every
// default view. A photo that carries no stack, or that does not exist, is a
// no-op, which makes the call safe to issue unconditionally before the mutation.
func LeaveStackTx(ctx context.Context, tx pgx.Tx, uid string) error {
	var stackUID *string
	err := tx.QueryRow(ctx, `SELECT stack_uid FROM photos WHERE uid = $1`, uid).Scan(&stackUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("photos: reading stack membership: %w", err)
	}
	if stackUID == nil {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE photos SET stack_uid = NULL, stack_primary = false, updated_at = now()
		 WHERE uid = $1`, uid); err != nil {
		return fmt.Errorf("photos: unstacking leaving member: %w", err)
	}
	return repairStackTx(ctx, tx, *stackUID)
}

// inTx runs fn inside a transaction, committing on success and rolling back on
// any error, so a mutation and its stack-invariant repair are atomic.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("photos: begin stack transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("photos: commit stack transaction: %w", err)
	}
	return nil
}

// memberStackTx returns the stack_uid of memberUID on tx, or ErrPhotoNotFound
// when the photo is absent and ErrPhotoNotStacked when it carries no stack.
func memberStackTx(ctx context.Context, tx pgx.Tx, memberUID string) (string, error) {
	var stackUID *string
	err := tx.QueryRow(ctx, `SELECT stack_uid FROM photos WHERE uid = $1`, memberUID).Scan(&stackUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPhotoNotFound
	}
	if err != nil {
		return "", fmt.Errorf("photos: reading stack membership: %w", err)
	}
	if stackUID == nil {
		return "", ErrPhotoNotStacked
	}
	return *stackUID, nil
}

// affectedStacksTx returns the distinct non-null stack_uids currently carried by
// uids on tx — the stacks that will lose members and therefore need repair.
func affectedStacksTx(ctx context.Context, tx pgx.Tx, uids []string) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT DISTINCT stack_uid FROM photos WHERE uid = ANY($1) AND stack_uid IS NOT NULL`, uids)
	if err != nil {
		return nil, fmt.Errorf("photos: reading affected stacks: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var su string
		if err := rows.Scan(&su); err != nil {
			return nil, fmt.Errorf("photos: scanning affected stack: %w", err)
		}
		out = append(out, su)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating affected stacks: %w", err)
	}
	return out, nil
}

// repairStackTx restores the stack invariants after members left it: a stack that
// dropped below two members is dissolved (its lone remnant becomes standalone),
// and a stack left without a primary re-elects the most suitable member (see
// primaryElectionOrder). A stack that is still whole is left untouched.
func repairStackTx(ctx context.Context, tx pgx.Tx, stackUID string) error {
	var members, primaries int
	err := tx.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE stack_primary) FROM photos WHERE stack_uid = $1`,
		stackUID).Scan(&members, &primaries)
	if err != nil {
		return fmt.Errorf("photos: counting stack members: %w", err)
	}
	switch {
	case members == 0:
		return nil
	case members == 1:
		_, err = tx.Exec(ctx,
			`UPDATE photos SET stack_uid = NULL, stack_primary = false, updated_at = now()
			 WHERE stack_uid = $1`, stackUID)
	case primaries == 0:
		_, err = tx.Exec(ctx,
			`UPDATE photos SET stack_primary = true, updated_at = now()
			 WHERE uid = (SELECT uid FROM photos WHERE stack_uid = $1 ORDER BY `+primaryElectionOrder+` LIMIT 1)`,
			stackUID)
	}
	if err != nil {
		return fmt.Errorf("photos: repairing stack: %w", err)
	}
	return nil
}

// dedupeStrings returns the distinct values of in, preserving first-seen order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
