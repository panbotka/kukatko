package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// markerColumns is the canonical, ordered column list for marker reads, matched
// by scanMarker.
const markerColumns = "uid, photo_uid, subject_uid, type, x, y, w, h, score, " +
	"invalid, reviewed, created_at, updated_at"

// insertMarkerSQL inserts a marker and returns the stored row.
const insertMarkerSQL = `
INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h, score, invalid, reviewed)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING ` + markerColumns

// scanMarker reads one marker row in markerColumns order, wrapping any scan error
// (including pgx.ErrNoRows, which callers translate to ErrMarkerNotFound).
func scanMarker(row pgx.Row) (Marker, error) {
	var m Marker
	if err := row.Scan(
		&m.UID, &m.PhotoUID, &m.SubjectUID, &m.Type, &m.X, &m.Y, &m.W, &m.H,
		&m.Score, &m.Invalid, &m.Reviewed, &m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return Marker{}, fmt.Errorf("people: scanning marker: %w", err)
	}
	return m, nil
}

// CreateMarker inserts m and returns it refreshed with the generated UID and
// timestamps. An empty type defaults to MarkerFace; an unrecognised type returns
// ErrInvalidType and an out-of-range box returns ErrInvalidBounds. When
// m.SubjectUID is set the subject must exist (ErrSubjectNotFound otherwise) and
// the photo's faces cache is updated in the same transaction.
func (s *Store) CreateMarker(ctx context.Context, m Marker) (Marker, error) {
	if m.Type == "" {
		m.Type = MarkerFace
	}
	if !m.Type.valid() {
		return Marker{}, fmt.Errorf("%w: marker type %q", ErrInvalidType, m.Type)
	}
	if !m.validBounds() {
		return Marker{}, fmt.Errorf("%w: [%g %g %g %g]", ErrInvalidBounds, m.X, m.Y, m.W, m.H)
	}
	if m.UID == "" {
		uid, err := newMarkerUID()
		if err != nil {
			return Marker{}, err
		}
		m.UID = uid
	}
	if m.SubjectUID == nil {
		return scanMarker(s.pool.QueryRow(ctx, insertMarkerSQL,
			m.UID, m.PhotoUID, m.SubjectUID, m.Type, m.X, m.Y, m.W, m.H,
			m.Score, m.Invalid, m.Reviewed))
	}
	return s.createMarkerWithSubject(ctx, m)
}

// createMarkerWithSubject inserts a marker that already names a subject, resolving
// the subject's name and refreshing the faces cache atomically.
func (s *Store) createMarkerWithSubject(ctx context.Context, m Marker) (Marker, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Marker{}, fmt.Errorf("people: begin create marker: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	name, err := subjectName(ctx, tx, *m.SubjectUID)
	if err != nil {
		return Marker{}, err
	}
	created, err := scanMarker(tx.QueryRow(ctx, insertMarkerSQL,
		m.UID, m.PhotoUID, m.SubjectUID, m.Type, m.X, m.Y, m.W, m.H,
		m.Score, m.Invalid, m.Reviewed))
	if err != nil {
		return Marker{}, err
	}
	if err := assignFacesCache(ctx, tx, created.UID, *m.SubjectUID, name); err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Marker{}, fmt.Errorf("people: commit create marker: %w", err)
	}
	return created, nil
}

// GetMarkerByUID returns the marker with the given UID, or ErrMarkerNotFound.
func (s *Store) GetMarkerByUID(ctx context.Context, uid string) (Marker, error) {
	q := "SELECT " + markerColumns + " FROM markers WHERE uid = $1"
	m, err := scanMarker(s.pool.QueryRow(ctx, q, uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Marker{}, ErrMarkerNotFound
		}
		return Marker{}, err
	}
	return m, nil
}

// listMarkersByPhotoSQL reads every marker of a photo, oldest first then by uid.
const listMarkersByPhotoSQL = "SELECT " + markerColumns +
	" FROM markers WHERE photo_uid = $1 ORDER BY created_at, uid"

// ListMarkersByPhoto returns every marker on the photo identified by photoUID,
// ordered oldest first. A photo with no markers yields an empty slice and a nil
// error.
func (s *Store) ListMarkersByPhoto(ctx context.Context, photoUID string) ([]Marker, error) {
	rows, err := s.pool.Query(ctx, listMarkersByPhotoSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("people: listing markers for %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]Marker, 0)
	for rows.Next() {
		m, scanErr := scanMarker(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating markers for %s: %w", photoUID, err)
	}
	return out, nil
}

// assignMarkerSQL points a marker at a subject and returns the refreshed row.
const assignMarkerSQL = "UPDATE markers SET subject_uid = $2, updated_at = now() " +
	"WHERE uid = $1 RETURNING " + markerColumns

// AssignSubject assigns the marker identified by markerUID to subjectUID and
// refreshes the cached subject_uid/subject_name on any faces tied to the marker,
// atomically. It returns ErrMarkerNotFound or ErrSubjectNotFound when either side
// is missing.
func (s *Store) AssignSubject(ctx context.Context, markerUID, subjectUID string) (Marker, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Marker{}, fmt.Errorf("people: begin assign subject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	name, err := subjectName(ctx, tx, subjectUID)
	if err != nil {
		return Marker{}, err
	}
	updated, err := scanMarker(tx.QueryRow(ctx, assignMarkerSQL, markerUID, subjectUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Marker{}, ErrMarkerNotFound
		}
		return Marker{}, err
	}
	if err := assignFacesCache(ctx, tx, markerUID, subjectUID, name); err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Marker{}, fmt.Errorf("people: commit assign subject: %w", err)
	}
	return updated, nil
}

// unassignMarkerSQL clears a marker's subject and returns the refreshed row.
const unassignMarkerSQL = "UPDATE markers SET subject_uid = NULL, updated_at = now() " +
	"WHERE uid = $1 RETURNING " + markerColumns

// UnassignSubject clears the subject of the marker identified by markerUID and
// resets the cached subject_uid/subject_name on any faces tied to it, atomically.
// It returns ErrMarkerNotFound if no such marker exists.
func (s *Store) UnassignSubject(ctx context.Context, markerUID string) (Marker, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Marker{}, fmt.Errorf("people: begin unassign subject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := scanMarker(tx.QueryRow(ctx, unassignMarkerSQL, markerUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Marker{}, ErrMarkerNotFound
		}
		return Marker{}, err
	}
	if err := clearFacesSubject(ctx, tx, markerUID); err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Marker{}, fmt.Errorf("people: commit unassign subject: %w", err)
	}
	return updated, nil
}

// SetMarkerInvalid sets or clears the invalid flag on the marker identified by
// uid and returns the refreshed marker, or ErrMarkerNotFound.
func (s *Store) SetMarkerInvalid(ctx context.Context, uid string, invalid bool) (Marker, error) {
	return s.updateMarkerFlag(ctx, "invalid", uid, invalid)
}

// SetMarkerReviewed sets or clears the reviewed flag on the marker identified by
// uid and returns the refreshed marker, or ErrMarkerNotFound.
func (s *Store) SetMarkerReviewed(ctx context.Context, uid string, reviewed bool) (Marker, error) {
	return s.updateMarkerFlag(ctx, "reviewed", uid, reviewed)
}

// updateMarkerFlag sets the boolean column col (a trusted internal constant,
// never user input) on the marker identified by uid and returns the refreshed
// row, translating pgx.ErrNoRows into ErrMarkerNotFound.
func (s *Store) updateMarkerFlag(ctx context.Context, col, uid string, val bool) (Marker, error) {
	q := "UPDATE markers SET " + col + " = $2, updated_at = now() " +
		"WHERE uid = $1 RETURNING " + markerColumns
	m, err := scanMarker(s.pool.QueryRow(ctx, q, uid, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Marker{}, ErrMarkerNotFound
		}
		return Marker{}, err
	}
	return m, nil
}

// DeleteMarker removes the marker identified by uid and clears the cached
// marker_uid/subject_uid/subject_name on any faces that referenced it, in one
// transaction. It returns ErrMarkerNotFound if no such marker exists.
func (s *Store) DeleteMarker(ctx context.Context, uid string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: begin delete marker: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		"UPDATE faces SET marker_uid = NULL, subject_uid = NULL, subject_name = '' "+
			"WHERE marker_uid = $1", uid,
	); err != nil {
		return fmt.Errorf("people: clearing faces cache for marker %s: %w", uid, err)
	}
	tag, err := tx.Exec(ctx, "DELETE FROM markers WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("people: deleting marker %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMarkerNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("people: commit delete marker %s: %w", uid, err)
	}
	return nil
}

// subjectName returns the name of the subject identified by uid within tx,
// translating a missing subject into ErrSubjectNotFound. It is used to keep the
// denormalised faces.subject_name cache in step when a marker is (re)assigned.
func subjectName(ctx context.Context, tx pgx.Tx, uid string) (string, error) {
	var name string
	err := tx.QueryRow(ctx, "SELECT name FROM subjects WHERE uid = $1", uid).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSubjectNotFound
	}
	if err != nil {
		return "", fmt.Errorf("people: looking up subject %s: %w", uid, err)
	}
	return name, nil
}

// assignFacesCache writes subjectUID/subjectName into the denormalised cache of
// every face tied to markerUID. It is a no-op when no face references the marker.
func assignFacesCache(ctx context.Context, tx pgx.Tx, markerUID, subjectUID, subjectName string) error {
	_, err := tx.Exec(ctx,
		"UPDATE faces SET subject_uid = $2, subject_name = $3 WHERE marker_uid = $1",
		markerUID, subjectUID, subjectName)
	if err != nil {
		return fmt.Errorf("people: updating faces cache for marker %s: %w", markerUID, err)
	}
	return nil
}

// clearFacesSubject resets the cached subject_uid/subject_name on every face tied
// to markerUID, leaving the marker_uid link intact.
func clearFacesSubject(ctx context.Context, tx pgx.Tx, markerUID string) error {
	_, err := tx.Exec(ctx,
		"UPDATE faces SET subject_uid = NULL, subject_name = '' WHERE marker_uid = $1",
		markerUID)
	if err != nil {
		return fmt.Errorf("people: clearing faces cache for marker %s: %w", markerUID, err)
	}
	return nil
}
