package people

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// The audited store methods mirror the plain mutations but run the change and its
// audit row on one pgx.Tx, so the audit record commits atomically with the change
// and rolls back with it on failure — the durable-audit convention shared with
// internal/photos and internal/organize (see internal/audit). Each takes a
// pre-built audit.Entry the handler stamped with the actor, client IP and
// User-Agent; the store defaults the entry's TargetUID to the affected entity.

// inAuditedTx opens a transaction, runs mutate, writes entry on the same
// transaction and commits, so the mutation and its audit row are atomic: if either
// fails the transaction rolls back and neither persists.
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

// CreateSubjectAudited inserts subj and writes entry to the audit log in the same
// transaction, so the subject and the record of who created it commit atomically.
// entry's TargetUID defaults to the generated subject UID. It behaves like
// CreateSubject otherwise (unique slug, ErrInvalidType for a bad type).
func (s *Store) CreateSubjectAudited(ctx context.Context, subj Subject, entry audit.Entry) (Subject, error) {
	if subj.Type == "" {
		subj.Type = SubjectPerson
	}
	if !subj.Type.valid() {
		return Subject{}, fmt.Errorf("%w: subject type %q", ErrInvalidType, subj.Type)
	}
	if subj.UID == "" {
		uid, err := newSubjectUID()
		if err != nil {
			return Subject{}, err
		}
		subj.UID = uid
	}
	if entry.TargetUID == "" {
		entry.TargetUID = subj.UID
	}
	base := Slugify(subj.Name)
	return s.insertSubjectAuditedWithUniqueSlug(ctx, base, entry, func(tx pgx.Tx, slug string) (Subject, error) {
		subj.Slug = slug
		return scanSubject(tx.QueryRow(ctx, insertSubjectSQL,
			subj.UID, subj.Slug, subj.Name, subj.Type, subj.Favorite,
			subj.Private, subj.Notes, subj.CoverPhotoUID))
	})
}

// insertSubjectAuditedWithUniqueSlug runs write with successive candidate slugs
// (base, base-2, …), each in its own transaction, and on the first attempt that
// avoids a slug unique-constraint violation also writes entry on that transaction
// and commits — so the row and its audit record land atomically. A per-attempt
// transaction is used because a colliding insert aborts its transaction; the next
// attempt starts a fresh one. A non-slug error aborts immediately (rolling back,
// so no audit row); ErrSlugExhausted is returned if every attempt collides.
func (s *Store) insertSubjectAuditedWithUniqueSlug(
	ctx context.Context, base string, entry audit.Entry, write func(tx pgx.Tx, slug string) (Subject, error),
) (Subject, error) {
	for attempt := range maxSlugAttempts {
		out, err := s.insertSubjectAuditedAttempt(ctx, candidateSlug(base, attempt), entry, write)
		if name, ok := isUniqueViolation(err); ok && strings.Contains(name, "slug") {
			continue
		}
		if err != nil {
			return Subject{}, err
		}
		return out, nil
	}
	return Subject{}, ErrSlugExhausted
}

// insertSubjectAuditedAttempt runs one slug attempt in its own transaction: it
// writes the row, then the audit entry, then commits, rolling everything back on
// any error. The write's error (which may be a slug unique-constraint violation the
// caller inspects) is returned unchanged.
func (s *Store) insertSubjectAuditedAttempt(
	ctx context.Context, slug string, entry audit.Entry, write func(tx pgx.Tx, slug string) (Subject, error),
) (Subject, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Subject{}, fmt.Errorf("people: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	subj, err := write(tx, slug)
	if err != nil {
		return Subject{}, err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return Subject{}, fmt.Errorf("people: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Subject{}, fmt.Errorf("people: commit audited transaction: %w", err)
	}
	return subj, nil
}

// UpdateSubjectAudited applies upd to the subject identified by uid and writes entry
// in the same transaction. entry's TargetUID defaults to uid. It behaves like
// UpdateSubject otherwise (ErrSubjectNotFound if missing, ErrInvalidType for a bad
// type); a rolled-back update writes no audit row.
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
	var updated Subject
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		var mErr error
		updated, mErr = updateSubjectWithCacheTx(ctx, tx, uid, upd)
		return mErr
	})
	if err != nil {
		return Subject{}, err
	}
	return updated, nil
}

// DeleteSubjectAudited removes the subject identified by uid and writes entry in the
// same transaction. entry's TargetUID defaults to uid. A missing subject returns
// ErrSubjectNotFound and writes no audit row.
func (s *Store) DeleteSubjectAudited(ctx context.Context, uid string, entry audit.Entry) error {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return deleteSubjectTx(ctx, tx, uid)
	})
}

// CreateMarkerAudited inserts m (which must already name a subject via
// m.SubjectUID) and writes entry in the same transaction, so the marker, its faces
// cache and the audit record commit atomically. It generates m.UID when empty and
// defaults entry's TargetUID to it. It is the audited create used by the face
// assignment state machine when a face has no existing marker; a rolled-back insert
// writes no audit row.
func (s *Store) CreateMarkerAudited(ctx context.Context, m Marker, entry audit.Entry) (Marker, error) {
	if m.SubjectUID == nil {
		return Marker{}, fmt.Errorf("%w: audited marker create requires a subject", ErrInvalidType)
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
	if entry.TargetUID == "" {
		entry.TargetUID = m.UID
	}
	var created Marker
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		var mErr error
		created, mErr = createMarkerWithSubjectTx(ctx, tx, m)
		return mErr
	})
	if err != nil {
		return Marker{}, err
	}
	return created, nil
}

// AssignSubjectAudited assigns the marker identified by markerUID to subjectUID and
// writes entry in the same transaction, refreshing the faces cache alongside.
// entry's TargetUID defaults to markerUID. It returns ErrMarkerNotFound or
// ErrSubjectNotFound when either side is missing, each rolling back with no audit
// row.
func (s *Store) AssignSubjectAudited(
	ctx context.Context, markerUID, subjectUID string, entry audit.Entry,
) (Marker, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = markerUID
	}
	var updated Marker
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		var mErr error
		updated, mErr = assignSubjectTx(ctx, tx, markerUID, subjectUID)
		return mErr
	})
	if err != nil {
		return Marker{}, err
	}
	return updated, nil
}

// UnassignSubjectAudited clears the subject of the marker identified by markerUID
// and writes entry in the same transaction, resetting the faces cache alongside.
// entry's TargetUID defaults to markerUID. It returns ErrMarkerNotFound if no such
// marker exists, rolling back with no audit row.
func (s *Store) UnassignSubjectAudited(ctx context.Context, markerUID string, entry audit.Entry) (Marker, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = markerUID
	}
	var updated Marker
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		var mErr error
		updated, mErr = unassignSubjectTx(ctx, tx, markerUID)
		return mErr
	})
	if err != nil {
		return Marker{}, err
	}
	return updated, nil
}
