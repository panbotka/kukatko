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
// markers that fall on a visible photo plus the face that illustrates it, ordered
// by name then uid for a stable people-index display. The subject columns are
// alias-qualified because the markers join also exposes a uid column. The photos
// join restricts the count to visible members (not archived, not a non-primary
// stack member) so the badge agrees with the subject's gallery: COUNT(p.uid)
// ignores the NULL rows a marker on a hidden photo joins as.
//
// The best_face CTE picks the one face per subject the tile is cropped to. The
// ordering is the whole rule, so it is worth saying why each term is there:
//
//   - w * h DESC — a tile is a small square blown up out of a crop of a cached
//     thumbnail, so the pixels behind the face are what decide whether it reads as
//     a person or as mush. The biggest box of that person wins; nothing else comes
//     close to mattering as much.
//   - score DESC — the detector's confidence, which only separates boxes of the
//     same size. Leading with it instead would hand the tile to a tiny, immaculate
//     face over a large, merely good one, which is exactly backwards for a crop.
//   - uid — makes the choice deterministic, so the same face wins on every request
//     and the page does not reshuffle itself between reloads.
//
// The filters carry the rest: invalid = FALSE drops the faces a user rejected
// (a tile is no place to re-show a false positive), type = 'face' drops manually
// drawn label boxes, which are regions but not faces, and the w/h and file
// dimension guards drop degenerate rows the crop maths cannot use. The photos
// join mirrors the count's visibility rule so a tile never points at an archived
// photo or a stack member the library hides.
const listSubjectsSQL = `
WITH best_face AS (
    SELECT DISTINCT ON (m.subject_uid)
           m.subject_uid, m.photo_uid, m.x, m.y, m.w, m.h,
           p.file_width, p.file_height, p.file_orientation
    FROM markers m
    JOIN photos p ON p.uid = m.photo_uid
    WHERE m.subject_uid IS NOT NULL
      AND m.type = 'face'
      AND m.invalid = FALSE
      AND m.w > 0 AND m.h > 0
      AND p.archived_at IS NULL
      AND (p.stack_uid IS NULL OR p.stack_primary)
      AND p.file_width > 0 AND p.file_height > 0
    ORDER BY m.subject_uid, m.w * m.h DESC, m.score DESC, m.uid
)
SELECT s.uid, s.slug, s.name, s.type, s.favorite, s.private, s.notes,
       s.cover_photo_uid, s.created_at, s.updated_at, COUNT(p.uid) AS marker_count,
       bf.photo_uid, bf.x, bf.y, bf.w, bf.h,
       bf.file_width, bf.file_height, bf.file_orientation
FROM subjects s
LEFT JOIN markers m ON m.subject_uid = s.uid AND m.invalid = FALSE
LEFT JOIN photos p ON p.uid = m.photo_uid AND p.archived_at IS NULL
    AND (p.stack_uid IS NULL OR p.stack_primary)
LEFT JOIN best_face bf ON bf.subject_uid = s.uid
GROUP BY s.uid, bf.photo_uid, bf.x, bf.y, bf.w, bf.h,
         bf.file_width, bf.file_height, bf.file_orientation
ORDER BY s.name, s.uid`

// scanSubjectCount reads one listSubjectsSQL row into a SubjectCount. The cover
// face's columns all come from the same LEFT JOIN, so they are NULL together for
// a subject with no usable face; photoUID being NULL is what says so, and the
// subject then carries no CoverFace at all rather than a zeroed one.
func scanSubjectCount(row pgx.Row) (SubjectCount, error) {
	var sc SubjectCount
	var face SubjectFace
	var photoUID *string
	var x, y, w, h *float64
	var width, height, orientation *int
	if err := row.Scan(
		&sc.UID, &sc.Slug, &sc.Name, &sc.Type, &sc.Favorite, &sc.Private,
		&sc.Notes, &sc.CoverPhotoUID, &sc.CreatedAt, &sc.UpdatedAt, &sc.MarkerCount,
		&photoUID, &x, &y, &w, &h, &width, &height, &orientation,
	); err != nil {
		return SubjectCount{}, fmt.Errorf("people: scanning subject count: %w", err)
	}
	if photoUID == nil {
		return sc, nil
	}
	face = SubjectFace{
		PhotoUID: *photoUID,
		X:        deref(x), Y: deref(y), W: deref(w), H: deref(h),
		Width: deref(width), Height: deref(height), Orientation: deref(orientation),
	}
	sc.CoverFace = &face
	return sc, nil
}

// deref returns the value ptr points at, or the type's zero value when it is nil.
// The cover-face columns are NOT NULL in their own tables and only become
// nullable by riding on a LEFT JOIN, so a nil here means "no face row", which the
// caller has already decided by looking at the photo uid.
func deref[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	}
	return *ptr
}

// ListSubjects returns every subject together with how many non-invalid markers
// reference it and the face picked to illustrate it, ordered by name then uid. A
// store with no subjects yields an empty slice and a nil error.
func (s *Store) ListSubjects(ctx context.Context) ([]SubjectCount, error) {
	rows, err := s.pool.Query(ctx, listSubjectsSQL)
	if err != nil {
		return nil, fmt.Errorf("people: listing subjects: %w", err)
	}
	defer rows.Close()

	out := make([]SubjectCount, 0)
	for rows.Next() {
		sc, err := scanSubjectCount(rows)
		if err != nil {
			return nil, err
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
// undated photos last), then by uid DESC for a stable order. Archived photos and
// the non-primary members of a stack are excluded so a subject's gallery mirrors
// the default library view (only a stack's primary appears).
//
// The tiebreaker is uid DESC, not ASC, to match the library list's default order
// (photos.orderClause emits `taken_at DESC NULLS LAST, uid DESC` for the newest
// sort). The subject-detail photo viewer pages prev/next through
// `GET /photos?person=<uid>&sort=newest`, so the two must agree down to the
// tiebreaker or the viewer would step through photos sharing a capture time — or
// with none — in the reverse of the order this gallery shows.
const listSubjectPhotoUIDsSQL = `
SELECT DISTINCT m.photo_uid, p.taken_at
FROM markers m
JOIN photos p ON p.uid = m.photo_uid
WHERE m.subject_uid = $1 AND m.invalid = FALSE AND p.archived_at IS NULL
  AND (p.stack_uid IS NULL OR p.stack_primary)
ORDER BY p.taken_at DESC NULLS LAST, m.photo_uid DESC`

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
