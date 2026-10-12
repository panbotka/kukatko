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
// the photo's faces cache is updated in the same transaction. The photo (and the
// subject, when named) must belong to projectUID: a foreign photo is
// ErrPhotoNotFound, a foreign subject ErrSubjectNotFound, and nothing is written.
func (s *Store) CreateMarker(ctx context.Context, projectUID string, m Marker) (Marker, error) {
	if err := requireProject(projectUID); err != nil {
		return Marker{}, err
	}
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Marker{}, fmt.Errorf("people: begin create marker: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := s.insertMarkerTx(ctx, tx, projectUID, m, "")
	if err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Marker{}, fmt.Errorf("people: commit create marker: %w", err)
	}
	return created, nil
}

// insertMarkerTx inserts m within tx and, when it names a subject, refreshes the
// denormalised faces cache for the new marker in the same transaction. It is the
// shared core of the plain and audited marker-create paths, so both keep the
// faces cache consistent identically. A marker created already naming a subject
// is a tagging, reported to the store's observer with actorUID. The photo must
// belong to projectUID (ErrPhotoNotFound otherwise) and a named subject too
// (ErrSubjectNotFound otherwise).
func (s *Store) insertMarkerTx(
	ctx context.Context, tx pgx.Tx, projectUID string, m Marker, actorUID string,
) (Marker, error) {
	if err := requirePhotoInProject(ctx, tx, projectUID, m.PhotoUID); err != nil {
		return Marker{}, err
	}
	var name string
	if m.SubjectUID != nil {
		resolved, err := subjectName(ctx, tx, projectUID, *m.SubjectUID)
		if err != nil {
			return Marker{}, err
		}
		name = resolved
	}
	created, err := scanMarker(tx.QueryRow(ctx, insertMarkerSQL,
		m.UID, m.PhotoUID, m.SubjectUID, m.Type, m.X, m.Y, m.W, m.H,
		m.Score, m.Invalid, m.Reviewed))
	if err != nil {
		return Marker{}, err
	}
	if m.SubjectUID != nil {
		if err := assignFacesCache(ctx, tx, created.UID, *m.SubjectUID, name); err != nil {
			return Marker{}, err
		}
		if err := s.tagged(ctx, tx, Tagging{
			ProjectUID: projectUID, PhotoUID: created.PhotoUID, SubjectUID: *m.SubjectUID, ActorUID: actorUID,
		}); err != nil {
			return Marker{}, err
		}
	}
	return created, nil
}

// GetMarkerByUID returns the marker with the given UID sitting on a photo of
// projectUID, or ErrMarkerNotFound.
func (s *Store) GetMarkerByUID(ctx context.Context, projectUID, uid string) (Marker, error) {
	if err := requireProject(projectUID); err != nil {
		return Marker{}, err
	}
	q := "SELECT " + markerColumns + " FROM markers WHERE uid = $1 AND " + markerProjectClause("$2")
	m, err := scanMarker(s.pool.QueryRow(ctx, q, uid, projectUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Marker{}, ErrMarkerNotFound
		}
		return Marker{}, err
	}
	return m, nil
}

// listMarkersByPhotoSQL reads every marker of a photo, oldest first then by uid.
var listMarkersByPhotoSQL = "SELECT " + markerColumns +
	" FROM markers WHERE photo_uid = $1 AND " + markerProjectClause("$2") + " ORDER BY created_at, uid"

// ListMarkersByPhoto returns every marker on the photo identified by photoUID,
// ordered oldest first. A photo with no markers — or one of another project than
// projectUID — yields an empty slice and a nil error.
func (s *Store) ListMarkersByPhoto(ctx context.Context, projectUID, photoUID string) ([]Marker, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, listMarkersByPhotoSQL, photoUID, projectUID)
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
// is missing — or belongs to another project than projectUID.
func (s *Store) AssignSubject(ctx context.Context, projectUID, markerUID, subjectUID string) (Marker, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Marker{}, fmt.Errorf("people: begin assign subject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := s.assignSubjectTx(ctx, tx, projectUID, markerUID, subjectUID, "")
	if err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Marker{}, fmt.Errorf("people: commit assign subject: %w", err)
	}
	return updated, nil
}

// assignSubjectTx points markerUID at subjectUID within tx and refreshes the
// denormalised faces cache in the same transaction. It is the shared core of the
// plain and audited assign paths. It returns ErrSubjectNotFound or ErrMarkerNotFound
// when either side is missing from projectUID; a foreign marker or subject is
// missing, so a marker never points across projects.
//
// With a tag observer the change is reported to it, attributed to actorUID: the
// subject the marker carried before (if another) as untagged, the new one as
// tagged. Re-assigning a marker to the subject it already carries reports
// nothing — nobody was put on the photo who was not on it already.
func (s *Store) assignSubjectTx(
	ctx context.Context, tx pgx.Tx, projectUID, markerUID, subjectUID, actorUID string,
) (Marker, error) {
	name, err := subjectName(ctx, tx, projectUID, subjectUID)
	if err != nil {
		return Marker{}, err
	}
	if err := requireMarkerInProject(ctx, tx, projectUID, markerUID); err != nil {
		return Marker{}, err
	}
	previous, err := s.previousSubject(ctx, tx, markerUID)
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
	if err := s.reportReassignment(ctx, tx, Tagging{
		ProjectUID: projectUID, PhotoUID: updated.PhotoUID, SubjectUID: subjectUID, ActorUID: actorUID,
	}, previous); err != nil {
		return Marker{}, err
	}
	return updated, nil
}

// reportReassignment tells the tag observer about a marker moved from previous
// to t.SubjectUID: the previous subject (if any) as untagged, the new one as
// tagged. Re-assigning a marker to the subject it already carries reports
// nothing.
func (s *Store) reportReassignment(ctx context.Context, tx pgx.Tx, t Tagging, previous string) error {
	if previous == t.SubjectUID {
		return nil
	}
	if previous != "" {
		untag := t
		untag.SubjectUID = previous
		if err := s.untagged(ctx, tx, untag); err != nil {
			return err
		}
	}
	return s.tagged(ctx, tx, t)
}

// previousSubject returns the subject markerUID carries before a re-assignment,
// locking the row so the answer holds until the update, or "" for an unassigned
// or missing marker (the update that follows reports the missing one). Without a
// tag observer nothing needs the answer, so nothing is read.
func (s *Store) previousSubject(ctx context.Context, tx pgx.Tx, markerUID string) (string, error) {
	if s.tags == nil {
		return "", nil
	}
	var previous *string
	err := tx.QueryRow(ctx, "SELECT subject_uid FROM markers WHERE uid = $1 FOR UPDATE", markerUID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("people: reading the subject of marker %s: %w", markerUID, err)
	}
	if previous == nil {
		return "", nil
	}
	return *previous, nil
}

// unassignMarkerSQL clears a marker's subject and returns the refreshed row.
const unassignMarkerSQL = "UPDATE markers SET subject_uid = NULL, updated_at = now() " +
	"WHERE uid = $1 RETURNING " + markerColumns

// UnassignSubject clears the subject of the marker identified by markerUID and
// resets the cached subject_uid/subject_name on any faces tied to it, atomically.
// It returns ErrMarkerNotFound if no such marker exists in projectUID.
func (s *Store) UnassignSubject(ctx context.Context, projectUID, markerUID string) (Marker, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Marker{}, fmt.Errorf("people: begin unassign subject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := s.unassignSubjectTx(ctx, tx, projectUID, markerUID, "")
	if err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Marker{}, fmt.Errorf("people: commit unassign subject: %w", err)
	}
	return updated, nil
}

// unassignSubjectTx clears markerUID's subject within tx and resets the cached
// subject on any faces tied to it, in the same transaction. It is the shared core
// of the plain and audited unassign paths. The subject it carried, if any, is
// reported to the store's tag observer as untagged, attributed to actorUID. It
// returns ErrMarkerNotFound if no such marker exists in projectUID.
func (s *Store) unassignSubjectTx(
	ctx context.Context, tx pgx.Tx, projectUID, markerUID, actorUID string,
) (Marker, error) {
	if err := requireMarkerInProject(ctx, tx, projectUID, markerUID); err != nil {
		return Marker{}, err
	}
	previous, err := s.previousSubject(ctx, tx, markerUID)
	if err != nil {
		return Marker{}, err
	}
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
	if previous != "" {
		if err := s.untagged(ctx, tx, Tagging{
			ProjectUID: projectUID, PhotoUID: updated.PhotoUID, SubjectUID: previous, ActorUID: actorUID,
		}); err != nil {
			return Marker{}, err
		}
	}
	return updated, nil
}

// setMarkerInvalidSQL flips a marker's invalid flag and returns the refreshed row.
// It is a named statement rather than a call to updateMarkerFlag because the
// audited path has to run it inside a caller-supplied transaction.
var setMarkerInvalidSQL = "UPDATE markers SET invalid = $2, updated_at = now() " +
	"WHERE uid = $1 AND " + markerProjectClause("$3") + " RETURNING " + markerColumns

// rowQuerier is the one method a single-row marker update needs. Both *pgxpool.Pool
// and pgx.Tx satisfy it, which is what lets the plain and audited invalid-flag
// paths run the very same statement and error mapping.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SetMarkerInvalid sets or clears the invalid flag on the marker identified by
// uid and returns the refreshed marker, or ErrMarkerNotFound (also for a marker
// of another project than projectUID). Use SetMarkerInvalidAudited when the
// change is a user's curation decision that belongs in the audit trail.
func (s *Store) SetMarkerInvalid(ctx context.Context, projectUID, uid string, invalid bool) (Marker, error) {
	return setMarkerInvalid(ctx, s.pool, projectUID, uid, invalid)
}

// setMarkerInvalid flips uid's invalid flag over q and returns the refreshed row,
// translating pgx.ErrNoRows (a missing or foreign marker) into ErrMarkerNotFound.
func setMarkerInvalid(ctx context.Context, q rowQuerier, projectUID, uid string, invalid bool) (Marker, error) {
	if err := requireProject(projectUID); err != nil {
		return Marker{}, err
	}
	m, err := scanMarker(q.QueryRow(ctx, setMarkerInvalidSQL, uid, invalid, projectUID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Marker{}, ErrMarkerNotFound
		}
		return Marker{}, err
	}
	return m, nil
}

// SetMarkerReviewed sets or clears the reviewed flag on the marker identified by
// uid and returns the refreshed marker, or ErrMarkerNotFound (also for a marker
// of another project than projectUID).
func (s *Store) SetMarkerReviewed(ctx context.Context, projectUID, uid string, reviewed bool) (Marker, error) {
	return s.updateMarkerFlag(ctx, projectUID, "reviewed", uid, reviewed)
}

// updateMarkerFlag sets the boolean column col (a trusted internal constant,
// never user input) on the marker identified by uid inside projectUID and returns
// the refreshed row, translating pgx.ErrNoRows into ErrMarkerNotFound.
func (s *Store) updateMarkerFlag(ctx context.Context, projectUID, col, uid string, val bool) (Marker, error) {
	if err := requireProject(projectUID); err != nil {
		return Marker{}, err
	}
	q := "UPDATE markers SET " + col + " = $2, updated_at = now() " +
		"WHERE uid = $1 AND " + markerProjectClause("$3") + " RETURNING " + markerColumns
	m, err := scanMarker(s.pool.QueryRow(ctx, q, uid, val, projectUID))
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
// transaction; a subject the marker carried is reported to the tag observer as
// untagged. It returns ErrMarkerNotFound if no such marker exists in projectUID,
// changing nothing.
func (s *Store) DeleteMarker(ctx context.Context, projectUID, uid string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: begin delete marker: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := requireMarkerInProject(ctx, tx, projectUID, uid); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		"UPDATE faces SET marker_uid = NULL, subject_uid = NULL, subject_name = '' "+
			"WHERE marker_uid = $1", uid,
	); err != nil {
		return fmt.Errorf("people: clearing faces cache for marker %s: %w", uid, err)
	}
	var (
		photoUID   string
		subjectUID *string
	)
	err = tx.QueryRow(ctx, "DELETE FROM markers WHERE uid = $1 RETURNING photo_uid, subject_uid", uid).
		Scan(&photoUID, &subjectUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMarkerNotFound
	}
	if err != nil {
		return fmt.Errorf("people: deleting marker %s: %w", uid, err)
	}
	if subjectUID != nil {
		if err := s.untagged(ctx, tx, Tagging{
			ProjectUID: projectUID, PhotoUID: photoUID, SubjectUID: *subjectUID,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("people: commit delete marker %s: %w", uid, err)
	}
	return nil
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
