package photosorter

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// listSubjectsSQL pages subjects ordered by uid for stable iteration.
const listSubjectsSQL = `SELECT uid, slug, name, type, favorite, private, notes
FROM subjects
ORDER BY uid
LIMIT $1 OFFSET $2`

// ListSubjects returns one page of subjects ordered by uid. A short page signals
// the last page.
func (r *Reader) ListSubjects(ctx context.Context, params ListParams) ([]Subject, error) {
	rows, err := r.pool.Query(ctx, listSubjectsSQL, pageLimit(params.Limit), params.Offset)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing subjects: %w", err)
	}
	defer rows.Close()

	var subjects []Subject
	for rows.Next() {
		var s Subject
		if scanErr := rows.Scan(
			&s.UID, &s.Slug, &s.Name, &s.Type, &s.Favorite, &s.Private, &s.Notes,
		); scanErr != nil {
			return nil, fmt.Errorf("photosorter: scanning subject: %w", scanErr)
		}
		subjects = append(subjects, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating subjects: %w", err)
	}
	return subjects, nil
}

// Markers returns every marker on photoUID, oldest first then by uid. A photo
// with no markers yields an empty slice and a nil error.
func (r *Reader) Markers(ctx context.Context, photoUID string) ([]Marker, error) {
	const q = `SELECT uid, photo_uid, subject_uid, type, x, y, w, h, score, invalid, reviewed
		FROM markers WHERE photo_uid = $1 ORDER BY created_at, uid`
	rows, err := r.pool.Query(ctx, q, photoUID)
	if err != nil {
		return nil, fmt.Errorf("photosorter: listing markers for %s: %w", photoUID, err)
	}
	defer rows.Close()

	var markers []Marker
	for rows.Next() {
		marker, scanErr := scanMarker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		markers = append(markers, marker)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("photosorter: iterating markers for %s: %w", photoUID, err)
	}
	return markers, nil
}

// scanMarker reads one markers row.
func scanMarker(row pgx.Row) (Marker, error) {
	var m Marker
	if err := row.Scan(
		&m.UID, &m.PhotoUID, &m.SubjectUID, &m.Type, &m.X, &m.Y, &m.W, &m.H,
		&m.Score, &m.Invalid, &m.Reviewed,
	); err != nil {
		return Marker{}, fmt.Errorf("photosorter: scanning marker: %w", err)
	}
	return m, nil
}
