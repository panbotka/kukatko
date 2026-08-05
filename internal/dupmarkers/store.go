package dupmarkers

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// listRepeatedMarkersSQL loads every valid face marker that shares its (photo,
// subject) pair with at least one other such marker — the rows a finding can
// possibly be built from.
//
// The window count is what keeps this cheap. Without it the query would ship
// every marker of every named person (tens of thousands of rows) so Go could
// throw almost all of them away; with it Postgres returns only the handful that
// sit in a multi-marker group — a few dozen on the reported library. It is an
// optimisation, not the rule: internal/dupmarkers.GroupMarkers applies the same
// predicates again over whatever rows it is handed, so a different loader cannot
// quietly change what counts as a finding.
//
// The filters carry the rest. invalid = FALSE drops regions a user already
// rejected; type = 'face' drops hand-drawn label boxes, which are not identity
// claims; a non-empty s.name drops the nameless catch-all subject, which exists to
// hold thousands of untagged regions and would bury every real finding; archived_at IS
// NULL drops photos already in the trash, whose markers are not worth a curator's
// time. Non-primary stack members are deliberately kept: a RAW sibling with the
// same person tagged twice is the same mistake and deserves the same fix.
const listRepeatedMarkersSQL = `
SELECT t.marker_uid, t.photo_uid, t.subject_uid, t.subject_name, t.type,
       t.invalid, t.reviewed, t.x, t.y, t.w, t.h, t.score,
       t.title, t.taken_at, t.file_width, t.file_height, t.file_orientation
FROM (
    SELECT m.uid AS marker_uid, m.photo_uid, m.subject_uid, s.name AS subject_name,
           m.type, m.invalid, m.reviewed, m.x, m.y, m.w, m.h, m.score,
           p.title, p.taken_at, p.file_width, p.file_height, p.file_orientation,
           COUNT(*) OVER (PARTITION BY m.photo_uid, m.subject_uid) AS group_size
    FROM markers m
    JOIN subjects s ON s.uid = m.subject_uid
    JOIN photos p ON p.uid = m.photo_uid
    WHERE m.invalid = FALSE
      AND m.type = 'face'
      AND s.name <> ''
      AND p.archived_at IS NULL
) t
WHERE t.group_size > 1
ORDER BY t.photo_uid, t.subject_uid, t.x, t.marker_uid`

// Store reads the repeated-marker candidates from Postgres. It owns no
// connection; it borrows the shared pgx pool supplied at construction, and it
// only ever reads.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ListRepeatedMarkers returns every valid face marker of a named subject that
// shares its photo with another marker of the same person, ordered by (photo,
// subject, x, uid). A library with no such markers yields an empty (non-nil)
// slice and a nil error.
func (s *Store) ListRepeatedMarkers(ctx context.Context) ([]MarkerRow, error) {
	rows, err := s.pool.Query(ctx, listRepeatedMarkersSQL)
	if err != nil {
		return nil, fmt.Errorf("dupmarkers: querying repeated markers: %w", err)
	}
	defer rows.Close()

	out := []MarkerRow{}
	for rows.Next() {
		var row MarkerRow
		if err := rows.Scan(
			&row.MarkerUID, &row.PhotoUID, &row.SubjectUID, &row.SubjectName, &row.Type,
			&row.Invalid, &row.Reviewed, &row.X, &row.Y, &row.W, &row.H, &row.Score,
			&row.PhotoTitle, &row.TakenAt, &row.Width, &row.Height, &row.Orientation,
		); err != nil {
			return nil, fmt.Errorf("dupmarkers: scanning repeated marker: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dupmarkers: iterating repeated markers: %w", err)
	}
	return out, nil
}
