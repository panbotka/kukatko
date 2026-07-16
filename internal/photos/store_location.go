package photos

import (
	"context"
	"fmt"
	"time"
)

// The vocabulary of Photo.LocationSource — where a photo's coordinates came
// from. The zero value, "", means the provenance is unknown: legacy rows, and
// every photo nobody has decided anything about.
//
// The SQL in this file spells these values out as literals, because a query
// cannot interpolate a constant without giving up being a compile-time constant
// itself. Changing a value here means grepping this file.
const (
	// LocationSourceExif marks coordinates read from the file's own GPS tags.
	LocationSourceExif = "exif"
	// LocationSourceManual marks a location the user decided on. It also marks a
	// photo whose location the user cleared: "manual" with a NULL lat/lng is a
	// deliberate tombstone recording the decision "this photo has no location", so
	// the estimator never hands back a guess the user threw away.
	LocationSourceManual = "manual"
	// LocationSourceEstimate marks coordinates inferred from photos taken nearby in
	// time. It is the only value the UI marks as an estimate, the only one the
	// estimator may overwrite, and the only one accept/clear act on.
	LocationSourceEstimate = "estimate"
)

// LocationCandidate is a photo eligible for location estimation: it has a known
// capture time but no coordinates, and nobody has decided otherwise.
type LocationCandidate struct {
	// UID identifies the photo.
	UID string
	// TakenAt is the photo's capture time, always non-nil for a candidate (a photo
	// with no date has no neighbours to be near in the first place).
	TakenAt time.Time
}

// LocatedPoint is a photo's measured position, the raw material an estimate is
// built from.
type LocatedPoint struct {
	Lat float64
	Lng float64
}

// listLocationCandidatesSQL selects the photos the estimator may fill in.
//
// Every clause is a rule the feature must not break, so they are worth reading
// as such rather than as query noise:
//
//   - lat IS NULL AND lng IS NULL — only a photo with no location at all. A
//     measured coordinate is never overwritten, which is what keeps "never
//     overwrite EXIF or the user" true no matter what the estimator does.
//   - an empty location_source — nobody has decided. This is what makes the backfill
//     safe to re-run: clearing an estimate stamps 'manual' (a tombstone), so a
//     cleared photo drops out of this set permanently instead of having the guess
//     re-added on every pass.
//   - taken_at IS NOT NULL — no date, no neighbours in time.
//   - NOT taken_at_estimated — the date is itself a guess, so a location derived
//     from it would be a guess about a guess. Refuse to compound them.
//   - NOT scan — a scanned print was digitised whenever, and its capture date
//     says nothing about where the scanner's other photos that day were.
//   - archived_at IS NULL — archived photos are on their way out; spending
//     geocoder credits on them is waste.
//
// The ordering is by uid rather than taken_at so a limited run is a stable
// prefix, and it rides the primary key.
const listLocationCandidatesSQL = `SELECT uid, taken_at FROM photos
	WHERE lat IS NULL AND lng IS NULL
	  AND location_source = ''
	  AND taken_at IS NOT NULL
	  AND NOT taken_at_estimated
	  AND NOT scan
	  AND archived_at IS NULL
	ORDER BY uid`

// ListLocationCandidates returns the photos the estimator may fill in: no
// coordinates, no decision recorded against them, a known and non-estimated
// capture date, not a scan and not archived. A limit above zero caps the result;
// zero or less returns every candidate.
//
// The set shrinks as photos are estimated or decided, which is what makes the
// backfill idempotent and cheap to re-run.
func (s *Store) ListLocationCandidates(ctx context.Context, limit int) ([]LocationCandidate, error) {
	sql := listLocationCandidatesSQL
	args := []any{}
	if limit > 0 {
		sql += " LIMIT $1"
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("photos: listing location candidates: %w", err)
	}
	defer rows.Close()

	var out []LocationCandidate
	for rows.Next() {
		var c LocationCandidate
		if err := rows.Scan(&c.UID, &c.TakenAt); err != nil {
			return nil, fmt.Errorf("photos: scanning location candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating location candidates: %w", err)
	}
	return out, nil
}

// listLocatedNeighboursSQL selects the measured positions in a time window.
//
// location_source <> 'estimate' is the load-bearing clause: an estimate must be
// built only from photos that actually know where they were. Letting estimates
// seed further estimates would let one guess propagate across a whole library,
// each hop looking exactly as confident as the last. Rows with an empty source
// are included on purpose — a legacy row's coordinates are real, only their
// provenance is unrecorded.
const listLocatedNeighboursSQL = `SELECT lat, lng FROM photos
	WHERE lat IS NOT NULL AND lng IS NOT NULL
	  AND location_source <> 'estimate'
	  AND archived_at IS NULL
	  AND taken_at >= $1 AND taken_at <= $2`

// ListLocatedNeighbours returns the measured positions of the non-archived
// photos captured between from and to inclusive. Estimated locations are
// excluded, so an estimate is never derived from another estimate.
func (s *Store) ListLocatedNeighbours(ctx context.Context, from, to time.Time) ([]LocatedPoint, error) {
	rows, err := s.pool.Query(ctx, listLocatedNeighboursSQL, from, to)
	if err != nil {
		return nil, fmt.Errorf("photos: listing located neighbours: %w", err)
	}
	defer rows.Close()

	var out []LocatedPoint
	for rows.Next() {
		var p LocatedPoint
		if err := rows.Scan(&p.Lat, &p.Lng); err != nil {
			return nil, fmt.Errorf("photos: scanning located neighbour: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photos: iterating located neighbours: %w", err)
	}
	return out, nil
}

// setEstimatedLocationSQL writes an estimate only onto a photo that still has no
// location and no decision. The WHERE clause repeats the candidate guards rather
// than trusting the caller's earlier read: between listing a candidate and
// writing to it the user may have typed a location in or cleared one, and the
// estimate must lose that race every time. It also makes a concurrent second
// backfill harmless.
const setEstimatedLocationSQL = `UPDATE photos
	SET lat = $2, lng = $3, location_source = 'estimate', updated_at = now()
	WHERE uid = $1 AND lat IS NULL AND lng IS NULL AND location_source = ''`

// SetEstimatedLocation stores lat/lng on the photo identified by uid and marks
// them an estimate. It reports whether the row was written: false means the
// photo gained a location or a decision since it was listed, in which case the
// estimate is dropped rather than applied — a measured or user-set location
// always wins.
func (s *Store) SetEstimatedLocation(ctx context.Context, uid string, lat, lng float64) (bool, error) {
	tag, err := s.pool.Exec(ctx, setEstimatedLocationSQL, uid, lat, lng)
	if err != nil {
		return false, fmt.Errorf("photos: setting estimated location for %s: %w", uid, err)
	}
	return tag.RowsAffected() > 0, nil
}
