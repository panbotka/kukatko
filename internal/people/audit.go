package people

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// inAuditedTx opens a transaction, runs mutate, writes entry on the same
// transaction and commits, so a mutation and its audit row are atomic: if either
// fails the transaction rolls back and neither persists. It mirrors the
// durable-audit convention used by internal/photos and internal/organize.
func (s *Store) inAuditedTx(ctx context.Context, entry audit.Entry, mutate func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mutate(tx); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("people: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("people: commit audited transaction: %w", err)
	}
	return nil
}

// mutateAudited opens a transaction, runs mutate, writes entry on the same
// transaction and commits, returning the row mutate produced. It is the
// row-returning counterpart of inAuditedTx: the mutation and its audit record
// commit atomically, and any failure rolls both back.
func mutateAudited[T any](
	ctx context.Context, pool *pgxpool.Pool, entry audit.Entry, mutate func(tx pgx.Tx) (T, error),
) (T, error) {
	var zero T
	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("people: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := mutate(tx)
	if err != nil {
		return zero, err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return zero, fmt.Errorf("people: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("people: commit audited transaction: %w", err)
	}
	return row, nil
}

// insertAuditedWithUniqueSlug runs write with successive candidate slugs (base,
// base-2, base-3, …), each in its own transaction, and on the first attempt that
// avoids a slug unique-constraint violation also writes entry on that transaction
// and commits — so the row and its audit record land atomically. A per-attempt
// transaction is used instead of a savepoint because a colliding insert aborts its
// transaction; the next attempt starts a fresh one. A non-slug error aborts
// immediately (rolling back, so no audit row); ErrSlugExhausted is returned if
// every attempt collides.
func insertAuditedWithUniqueSlug[T any](
	ctx context.Context, pool *pgxpool.Pool, base string, entry audit.Entry,
	write func(tx pgx.Tx, slug string) (T, error),
) (T, error) {
	var zero T
	for attempt := range maxSlugAttempts {
		out, err := insertAuditedAttempt(ctx, pool, candidateSlug(base, attempt), entry, write)
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

// insertAuditedAttempt runs one slug attempt in its own transaction: it writes the
// row, then the audit entry, then commits, rolling everything back on any error.
// The write's error (which may be a slug unique-constraint violation the caller
// inspects) is returned unchanged.
func insertAuditedAttempt[T any](
	ctx context.Context, pool *pgxpool.Pool, slug string, entry audit.Entry,
	write func(tx pgx.Tx, slug string) (T, error),
) (T, error) {
	var zero T
	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("people: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := write(tx, slug)
	if err != nil {
		return zero, err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return zero, fmt.Errorf("people: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("people: commit audited transaction: %w", err)
	}
	return row, nil
}

// CreateSubjectAudited inserts subj and writes entry to the audit log in the same
// transaction, so the subject and the record of who created it commit atomically
// (the durable-audit convention; see internal/audit). entry's TargetUID defaults
// to the subject's generated UID. It behaves like CreateSubject otherwise (unique
// slug, ErrInvalidType for a bad type).
func (s *Store) CreateSubjectAudited(ctx context.Context, subj Subject, entry audit.Entry) (Subject, error) {
	prepared, base, err := prepareSubjectInsert(subj)
	if err != nil {
		return Subject{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = prepared.UID
	}
	return insertAuditedWithUniqueSlug(ctx, s.pool, base, entry, func(tx pgx.Tx, slug string) (Subject, error) {
		prepared.Slug = slug
		return scanSubject(tx.QueryRow(ctx, insertSubjectSQL,
			prepared.UID, prepared.Slug, prepared.Name, prepared.Type, prepared.Favorite,
			prepared.Private, prepared.Notes, prepared.CoverPhotoUID))
	})
}

// UpdateSubjectAudited applies upd to the subject identified by uid and writes
// entry in the same transaction. entry's TargetUID defaults to uid. It behaves like
// UpdateSubject otherwise — re-slugging from the new name, refreshing the cached
// subject_name on the photo's faces, and returning ErrSubjectNotFound (writing no
// audit row) if no such subject exists, or ErrInvalidType for a bad type.
func (s *Store) UpdateSubjectAudited(
	ctx context.Context, uid string, upd SubjectUpdate, entry audit.Entry,
) (Subject, error) {
	if upd.Type == "" {
		upd.Type = SubjectPerson
	}
	if !upd.Type.valid() {
		return Subject{}, fmt.Errorf("%w: subject type %q", ErrInvalidType, upd.Type)
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	base := Slugify(upd.Name)
	updated, err := insertAuditedWithUniqueSlug(ctx, s.pool, base, entry, func(tx pgx.Tx, slug string) (Subject, error) {
		row, err := scanSubject(tx.QueryRow(ctx, updateSubjectSQL,
			uid, slug, upd.Name, upd.Type, upd.Favorite, upd.Private, upd.Notes, upd.CoverPhotoUID))
		if err != nil {
			return Subject{}, err
		}
		if _, err := tx.Exec(ctx,
			"UPDATE faces SET subject_name = $2 WHERE subject_uid = $1", uid, row.Name,
		); err != nil {
			return Subject{}, fmt.Errorf("people: refreshing faces cache for %s: %w", uid, err)
		}
		return row, nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, ErrSubjectNotFound
	}
	return updated, err
}

// DeleteSubjectAudited removes the subject identified by uid and writes entry in
// the same transaction. entry's TargetUID defaults to uid. It behaves like
// DeleteSubject otherwise (detaching markers, clearing the faces cache). A missing
// subject returns ErrSubjectNotFound and writes no audit row.
func (s *Store) DeleteSubjectAudited(ctx context.Context, uid string, entry audit.Entry) error {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
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
		return nil
	})
}

// CreateMarkerAudited inserts m and writes entry in the same transaction, so a
// marker created while assigning a face to a subject and the record of who did it
// commit atomically. entry's TargetUID defaults to the marker's UID. It behaves
// like CreateMarker otherwise (type/bounds validation, faces cache refresh when a
// subject is named, ErrSubjectNotFound for a missing subject).
func (s *Store) CreateMarkerAudited(ctx context.Context, m Marker, entry audit.Entry) (Marker, error) {
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
	if entry.TargetUID == "" {
		entry.TargetUID = m.UID
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (Marker, error) {
		return insertMarkerTx(ctx, tx, m)
	})
}

// AssignSubjectAudited assigns the marker identified by markerUID to subjectUID and
// writes entry in the same transaction, so the face assignment and the record of
// who made it commit atomically. entry's TargetUID defaults to markerUID. It behaves
// like AssignSubject otherwise (refreshing the faces cache, ErrMarkerNotFound or
// ErrSubjectNotFound when either side is missing — each rolls back, writing no
// audit row).
func (s *Store) AssignSubjectAudited(
	ctx context.Context, markerUID, subjectUID string, entry audit.Entry,
) (Marker, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = markerUID
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (Marker, error) {
		return assignSubjectTx(ctx, tx, markerUID, subjectUID)
	})
}

// UnassignSubjectAudited clears the subject of the marker identified by markerUID
// and writes entry in the same transaction. entry's TargetUID defaults to markerUID.
// It behaves like UnassignSubject otherwise (resetting the faces cache,
// ErrMarkerNotFound for a missing marker — which rolls back, writing no audit row).
func (s *Store) UnassignSubjectAudited(
	ctx context.Context, markerUID string, entry audit.Entry,
) (Marker, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = markerUID
	}
	return mutateAudited(ctx, s.pool, entry, func(tx pgx.Tx) (Marker, error) {
		return unassignSubjectTx(ctx, tx, markerUID)
	})
}
