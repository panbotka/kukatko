package photos

import (
	"context"
	"fmt"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// ImpossibleDate is a catalogued photo whose recorded capture date falls outside
// the years a photograph can have been taken in. It carries what the date was
// and where it came from, so the finding can be inspected — and the repair
// audited — without the row having to be read again.
type ImpossibleDate struct {
	// UID is the affected photo's uid.
	UID string `json:"uid"`
	// FileName is the original's file name, which is usually the culprit: a
	// download named after an asset id (90090310_638783213372240_…) reads as a
	// date, and the filename fallback believed it.
	FileName string `json:"file_name"`
	// TakenAt is the impossible capture date currently recorded, in UTC.
	TakenAt time.Time `json:"taken_at"`
	// TakenAtSource is that date's provenance ("filename", "exif", "manual", …).
	TakenAtSource string `json:"taken_at_source"`
}

// captureYearWindow turns an inclusive range of plausible years into the
// half-open instant interval it denotes in UTC: everything from the first moment
// of minYear up to (but not including) the first moment of maxYear+1.
//
// The comparison is done against instants rather than against EXTRACT(YEAR …) so
// the predicate does not depend on the session's time zone and can use an index
// on taken_at if one is ever added.
func captureYearWindow(minYear, maxYear int) (from, until time.Time) {
	return time.Date(minYear, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(maxYear+1, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// listImpossibleTakenAtSQL selects every photo dated outside the plausible
// window, oldest impossible date first so the absurd ones group together.
const listImpossibleTakenAtSQL = `
SELECT uid, file_name, taken_at, taken_at_source
FROM photos
WHERE taken_at IS NOT NULL AND (taken_at < $1 OR taken_at >= $2)
ORDER BY taken_at, uid`

// ListImpossibleTakenAt returns every photo whose capture date falls outside the
// inclusive year range [minYear, maxYear] — the range internal/exif accepts, so
// the guard on incoming photos and this listing cannot disagree about what an
// impossible date is.
//
// It is read-only, which makes it the dry run of ClearImpossibleTakenAtAudited:
// the returned list is exactly what that repair would clear. Archived photos are
// included; a wrong date is wrong in the trash too, and the repair is opt-in
// either way. The slice is empty (not nil) when every date is plausible.
func (s *Store) ListImpossibleTakenAt(ctx context.Context, minYear, maxYear int) ([]ImpossibleDate, error) {
	from, until := captureYearWindow(minYear, maxYear)
	rows, err := s.pool.Query(ctx, listImpossibleTakenAtSQL, from, until)
	if err != nil {
		return nil, fmt.Errorf("photos: listing impossible capture dates: %w", err)
	}
	defer rows.Close()

	out := make([]ImpossibleDate, 0)
	for rows.Next() {
		var d ImpossibleDate
		if err := rows.Scan(&d.UID, &d.FileName, &d.TakenAt, &d.TakenAtSource); err != nil {
			return nil, fmt.Errorf("photos: scanning impossible capture date: %w", err)
		}
		d.TakenAt = d.TakenAt.UTC()
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating impossible capture dates: %w", err)
	}
	return out, nil
}

// clearImpossibleTakenAtSQL declares the capture date unknown, guarded by the
// very predicate that found the row: a photo re-dated between the scan and the
// repair no longer matches and is left alone, and a second run is a no-op.
//
// The date is withdrawn, not replaced — nothing here guesses a substitute — and
// the outgoing value is put away by the shared taken_at_before_unknown rule, so
// the clear stays reversible and the discarded date is still readable in the
// photo's metadata panel and its sidecar.
var clearImpossibleTakenAtSQL = `
UPDATE photos SET
	` + TakenAtBeforeUnknownAssignment("NULL") + `,
	taken_at = NULL,
	taken_at_source = '` + TakenAtSourceUnknown + `',
	taken_at_precision = '` + TakenAtPrecisionDay + `',
	updated_at = now()
WHERE uid = $1 AND taken_at IS NOT NULL AND (taken_at < $2 OR taken_at >= $3)`

// ClearImpossibleTakenAtAudited clears the capture date of the photo identified
// by uid when that date is still outside the inclusive year range
// [minYear, maxYear], writing entry to the audit log in the same transaction so
// the record commits atomically with the change and rolls back with it (the
// durable-audit convention; see internal/audit). entry's TargetUID defaults to
// uid.
//
// It reports whether the row changed. A photo whose date became plausible in the
// meantime is not touched and no audit entry is written for it — the repair
// records what it did, not what it was asked to do.
func (s *Store) ClearImpossibleTakenAtAudited(
	ctx context.Context, uid string, minYear, maxYear int, entry audit.Entry,
) (bool, error) {
	from, until := captureYearWindow(minYear, maxYear)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("photos: begin impossible-date transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, clearImpossibleTakenAtSQL, uid, from, until)
	if err != nil {
		return false, fmt.Errorf("photos: clearing impossible capture date of %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return false, fmt.Errorf("photos: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("photos: commit impossible-date transaction: %w", err)
	}
	return true, nil
}
