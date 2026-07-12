package people

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// maxSlugAttempts bounds how many numeric suffixes the store tries when making a
// slug unique. The cap is far above any realistic number of name collisions; it
// exists only so a pathological loop terminates.
const maxSlugAttempts = 1000

// subjectColumns is the canonical, ordered column list for subject reads, matched
// by scanSubject.
const subjectColumns = "uid, slug, name, type, favorite, private, notes, " +
	"cover_photo_uid, created_at, updated_at"

// insertSubjectSQL inserts a subject and returns the stored row.
const insertSubjectSQL = `
INSERT INTO subjects (uid, slug, name, type, favorite, private, notes, cover_photo_uid)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING ` + subjectColumns

// scanSubject reads one subject row in subjectColumns order, wrapping any scan
// error (including pgx.ErrNoRows, which callers translate to ErrSubjectNotFound).
func scanSubject(row pgx.Row) (Subject, error) {
	var subj Subject
	if err := row.Scan(
		&subj.UID, &subj.Slug, &subj.Name, &subj.Type, &subj.Favorite,
		&subj.Private, &subj.Notes, &subj.CoverPhotoUID, &subj.CreatedAt, &subj.UpdatedAt,
	); err != nil {
		return Subject{}, fmt.Errorf("people: scanning subject: %w", err)
	}
	return subj, nil
}

// CreateSubject inserts subj and returns it refreshed with the generated UID,
// unique slug and timestamps. The slug is derived from subj.Name (Slugify) and a
// numeric suffix is appended on collision. An empty type defaults to
// SubjectPerson; an unrecognised type returns ErrInvalidType.
func (s *Store) CreateSubject(ctx context.Context, subj Subject) (Subject, error) {
	prepared, base, err := prepareSubjectInsert(subj)
	if err != nil {
		return Subject{}, err
	}
	return insertWithUniqueSlug(base, func(slug string) (Subject, error) {
		prepared.Slug = slug
		return scanSubject(s.pool.QueryRow(ctx, insertSubjectSQL,
			prepared.UID, prepared.Slug, prepared.Name, prepared.Type, prepared.Favorite,
			prepared.Private, prepared.Notes, prepared.CoverPhotoUID))
	})
}

// prepareSubjectInsert defaults and validates subj for insertion and assigns a
// generated UID when none is set, returning the prepared subject and the base slug
// derived from its name. It is shared by CreateSubject and CreateSubjectAudited so
// both apply identical validation and UID rules. It returns ErrInvalidType for an
// unrecognised type.
func prepareSubjectInsert(subj Subject) (Subject, string, error) {
	if subj.Type == "" {
		subj.Type = SubjectPerson
	}
	if !subj.Type.valid() {
		return Subject{}, "", fmt.Errorf("%w: subject type %q", ErrInvalidType, subj.Type)
	}
	if subj.UID == "" {
		uid, err := newSubjectUID()
		if err != nil {
			return Subject{}, "", err
		}
		subj.UID = uid
	}
	return subj, Slugify(subj.Name), nil
}

// insertWithUniqueSlug calls write with successive candidate slugs (base, base-2,
// base-3, …) until a write avoids a slug unique-constraint violation, returning
// that write's result. Any non-slug error aborts immediately; ErrSlugExhausted is
// returned if every attempt collides.
func insertWithUniqueSlug[T any](base string, write func(slug string) (T, error)) (T, error) {
	var zero T
	for attempt := range maxSlugAttempts {
		out, err := write(candidateSlug(base, attempt))
		if name, ok := isUniqueViolation(err); ok && strings.Contains(name, "slug") {
			continue
		}
		if err != nil {
			return zero, err
		}
		return out, nil
	}
	return zero, ErrSlugExhausted
}

// GetSubjectByUID returns the subject with the given UID, or ErrSubjectNotFound.
func (s *Store) GetSubjectByUID(ctx context.Context, uid string) (Subject, error) {
	return s.getSubject(ctx, "uid", uid)
}

// GetSubjectBySlug returns the subject with the given slug, or ErrSubjectNotFound.
func (s *Store) GetSubjectBySlug(ctx context.Context, slug string) (Subject, error) {
	return s.getSubject(ctx, "slug", slug)
}

// getSubject fetches a single subject filtered by an equality on the trusted
// column name col (an internal constant, never user input), translating
// pgx.ErrNoRows into ErrSubjectNotFound.
func (s *Store) getSubject(ctx context.Context, col, val string) (Subject, error) {
	q := "SELECT " + subjectColumns + " FROM subjects WHERE " + col + " = $1"
	subj, err := scanSubject(s.pool.QueryRow(ctx, q, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Subject{}, ErrSubjectNotFound
		}
		return Subject{}, err
	}
	return subj, nil
}

// updateSubjectSQL rewrites a subject's editable fields (including a re-derived
// slug) and returns the refreshed row.
const updateSubjectSQL = `
UPDATE subjects SET
    slug = $2, name = $3, type = $4, favorite = $5, private = $6,
    notes = $7, cover_photo_uid = $8, updated_at = now()
WHERE uid = $1
RETURNING ` + subjectColumns

// UpdateSubject applies upd to the subject identified by uid: it re-slugs from the
// new name (kept unique), rewrites the editable fields, and refreshes the cached
// subject_name on the photo's faces. It returns ErrSubjectNotFound if no such
// subject exists, or ErrInvalidType for an unrecognised type.
func (s *Store) UpdateSubject(ctx context.Context, uid string, upd SubjectUpdate) (Subject, error) {
	if upd.Type == "" {
		upd.Type = SubjectPerson
	}
	if !upd.Type.valid() {
		return Subject{}, fmt.Errorf("%w: subject type %q", ErrInvalidType, upd.Type)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Subject{}, fmt.Errorf("people: begin update subject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := updateSubjectTx(ctx, tx, uid, upd)
	if err != nil {
		return Subject{}, err
	}
	if _, err := tx.Exec(ctx,
		"UPDATE faces SET subject_name = $2 WHERE subject_uid = $1", uid, updated.Name,
	); err != nil {
		return Subject{}, fmt.Errorf("people: refreshing faces cache for %s: %w", uid, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Subject{}, fmt.Errorf("people: commit update subject %s: %w", uid, err)
	}
	return updated, nil
}

// updateSubjectTx performs the slug-unique UPDATE inside tx, translating a missing
// row to ErrSubjectNotFound. It is the transactional core of UpdateSubject.
func updateSubjectTx(ctx context.Context, tx pgx.Tx, uid string, upd SubjectUpdate) (Subject, error) {
	base := Slugify(upd.Name)
	updated, err := insertWithUniqueSlug(base, func(slug string) (Subject, error) {
		return scanSubject(tx.QueryRow(ctx, updateSubjectSQL,
			uid, slug, upd.Name, upd.Type, upd.Favorite,
			upd.Private, upd.Notes, upd.CoverPhotoUID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, ErrSubjectNotFound
	}
	return updated, err
}

// listSubjectsSQL reads every subject with its count of valid (non-invalid)
// markers, ordered by name then uid for a stable people-index display. The
// subject columns are alias-qualified because the markers join also exposes a
// uid column.
const listSubjectsSQL = `
SELECT s.uid, s.slug, s.name, s.type, s.favorite, s.private, s.notes,
       s.cover_photo_uid, s.created_at, s.updated_at, COUNT(m.uid) AS marker_count
FROM subjects s
LEFT JOIN markers m ON m.subject_uid = s.uid AND m.invalid = FALSE
GROUP BY s.uid
ORDER BY s.name, s.uid`

// ListSubjects returns every subject together with how many non-invalid markers
// reference it, ordered by name then uid. A store with no subjects yields an
// empty slice and a nil error.
func (s *Store) ListSubjects(ctx context.Context) ([]SubjectCount, error) {
	rows, err := s.pool.Query(ctx, listSubjectsSQL)
	if err != nil {
		return nil, fmt.Errorf("people: listing subjects: %w", err)
	}
	defer rows.Close()

	out := make([]SubjectCount, 0)
	for rows.Next() {
		var sc SubjectCount
		if err := rows.Scan(
			&sc.UID, &sc.Slug, &sc.Name, &sc.Type, &sc.Favorite, &sc.Private,
			&sc.Notes, &sc.CoverPhotoUID, &sc.CreatedAt, &sc.UpdatedAt, &sc.MarkerCount,
		); err != nil {
			return nil, fmt.Errorf("people: scanning subject count: %w", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating subjects: %w", err)
	}
	return out, nil
}

// listSubjectPhotoUIDsSQL returns the distinct photos that carry at least one
// non-invalid marker assigned to a subject, newest first (by capture time, with
// undated photos last), then by uid for a stable order. Archived photos are
// excluded so a subject's gallery mirrors the default library view.
const listSubjectPhotoUIDsSQL = `
SELECT DISTINCT m.photo_uid, p.taken_at
FROM markers m
JOIN photos p ON p.uid = m.photo_uid
WHERE m.subject_uid = $1 AND m.invalid = FALSE AND p.archived_at IS NULL
ORDER BY p.taken_at DESC NULLS LAST, m.photo_uid`

// ListPhotoUIDsBySubject returns the UIDs of every non-archived photo that has a
// non-invalid marker assigned to the subject identified by subjectUID, ordered
// newest first. A subject with no such photos yields an empty slice and a nil
// error. The caller paginates and resolves the UIDs to full photo records.
func (s *Store) ListPhotoUIDsBySubject(ctx context.Context, subjectUID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, listSubjectPhotoUIDsSQL, subjectUID)
	if err != nil {
		return nil, fmt.Errorf("people: listing photos for subject %s: %w", subjectUID, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var uid string
		var takenAt *time.Time
		if err := rows.Scan(&uid, &takenAt); err != nil {
			return nil, fmt.Errorf("people: scanning subject photo uid: %w", err)
		}
		out = append(out, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: iterating subject photos for %s: %w", subjectUID, err)
	}
	return out, nil
}

// DeleteSubject removes the subject identified by uid. Its markers are detached
// (markers.subject_uid is set NULL by the foreign key) and the cached
// subject_uid/subject_name on any faces pointing at it are cleared in the same
// transaction. It returns ErrSubjectNotFound if no such subject exists.
func (s *Store) DeleteSubject(ctx context.Context, uid string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: begin delete subject: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		"UPDATE faces SET subject_uid = NULL, subject_name = '' WHERE subject_uid = $1", uid,
	); err != nil {
		return fmt.Errorf("people: clearing faces cache for subject %s: %w", uid, err)
	}
	tag, err := tx.Exec(ctx, "DELETE FROM subjects WHERE uid = $1", uid)
	if err != nil {
		return fmt.Errorf("people: deleting subject %s: %w", uid, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSubjectNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("people: commit delete subject %s: %w", uid, err)
	}
	return nil
}
