package organize

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// rowQuerier runs a single-row query. Both *pgxpool.Pool and pgx.Tx satisfy it,
// so an insert/update that returns a row can run on the pool or inside a caller's
// transaction unchanged — the basis for the audited mutations below, which run
// the change and its audit row in one transaction.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// execer runs a statement that returns no rows. Both *pgxpool.Pool and pgx.Tx
// satisfy it, so a delete/insert can run on the pool or inside a transaction.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// inAuditedTx opens a transaction, runs mutate, writes entry on the same
// transaction and commits, so a mutation and its audit row are atomic: if either
// fails the transaction rolls back and neither persists. It mirrors the
// durable-audit convention used by internal/photos and internal/auth.
func (s *Store) inAuditedTx(ctx context.Context, entry audit.Entry, mutate func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("organize: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mutate(tx); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("organize: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("organize: commit audited transaction: %w", err)
	}
	return nil
}

// insertAuditedWithUniqueSlug runs write with successive candidate slugs (base,
// base-2, base-3, …), each in its own transaction, and on the first attempt that
// avoids a slug unique-constraint violation also writes entry on that transaction
// and commits — so the row and its audit record land atomically. A per-attempt
// transaction is used instead of a savepoint because a colliding insert aborts its
// transaction; the next attempt simply starts a fresh one. A non-slug error aborts
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
		return zero, fmt.Errorf("organize: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := write(tx, slug)
	if err != nil {
		return zero, err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return zero, fmt.Errorf("organize: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("organize: commit audited transaction: %w", err)
	}
	return row, nil
}

// CreateAlbumAudited inserts a and writes entry to the audit log in the same
// transaction, so the album and the record of who created it commit atomically.
// entry's TargetUID defaults to the album's generated UID. It behaves like
// CreateAlbum otherwise (unique slug, ErrInvalidType for a bad type).
func (s *Store) CreateAlbumAudited(ctx context.Context, a Album, entry audit.Entry) (Album, error) {
	prepared, base, err := prepareAlbumInsert(a)
	if err != nil {
		return Album{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = prepared.UID
	}
	return insertAuditedWithUniqueSlug(ctx, s.pool, base, entry, func(tx pgx.Tx, slug string) (Album, error) {
		return insertAlbumRow(ctx, tx, prepared, slug)
	})
}

// UpdateAlbumAudited applies upd to the album identified by uid and writes entry
// in the same transaction. entry's TargetUID defaults to uid. It behaves like
// UpdateAlbum otherwise (ErrAlbumNotFound if missing, ErrInvalidType for a bad
// type). A rolled-back update writes no audit row.
func (s *Store) UpdateAlbumAudited(
	ctx context.Context, uid string, upd AlbumUpdate, entry audit.Entry,
) (Album, error) {
	prepared, base, err := prepareAlbumUpdate(upd)
	if err != nil {
		return Album{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	updated, err := insertAuditedWithUniqueSlug(ctx, s.pool, base, entry, func(tx pgx.Tx, slug string) (Album, error) {
		return updateAlbumRow(ctx, tx, uid, prepared, slug)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Album{}, ErrAlbumNotFound
		}
		return Album{}, err
	}
	return updated, nil
}

// DeleteAlbumAudited removes the album identified by uid and writes entry in the
// same transaction. entry's TargetUID defaults to uid. A missing album returns
// ErrAlbumNotFound and writes no audit row.
func (s *Store) DeleteAlbumAudited(ctx context.Context, uid string, entry audit.Entry) error {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return deleteAlbumRow(ctx, tx, uid)
	})
}

// AddPhotosAudited adds every photo in photoUIDs to the album identified by
// albumUID and writes a single entry in the same transaction, so the whole batch
// and its audit record commit atomically. entry's TargetUID defaults to albumUID.
// A missing album or photo returns ErrAlbumNotFound/ErrPhotoNotFound, rolls the
// batch back and writes no audit row. Adding a photo already a member is a no-op.
func (s *Store) AddPhotosAudited(
	ctx context.Context, albumUID string, photoUIDs []string, entry audit.Entry,
) error {
	if entry.TargetUID == "" {
		entry.TargetUID = albumUID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		for _, photoUID := range photoUIDs {
			if _, err := tx.Exec(ctx, addPhotoSQL, albumUID, photoUID); err != nil {
				return translateMembershipFK(err)
			}
		}
		return nil
	})
}

// RemovePhotosAudited removes every photo in photoUIDs from the album identified
// by albumUID and writes a single entry in the same transaction. entry's TargetUID
// defaults to albumUID. Removing a photo that is not a member is a no-op.
func (s *Store) RemovePhotosAudited(
	ctx context.Context, albumUID string, photoUIDs []string, entry audit.Entry,
) error {
	if entry.TargetUID == "" {
		entry.TargetUID = albumUID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		for _, photoUID := range photoUIDs {
			if err := removeAlbumPhotoRow(ctx, tx, albumUID, photoUID); err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateLabelAudited inserts l and writes entry to the audit log in the same
// transaction. entry's TargetUID defaults to the label's generated UID. It behaves
// like CreateLabel otherwise (unique slug).
func (s *Store) CreateLabelAudited(ctx context.Context, l Label, entry audit.Entry) (Label, error) {
	prepared, base, err := prepareLabelInsert(l)
	if err != nil {
		return Label{}, err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = prepared.UID
	}
	return insertAuditedWithUniqueSlug(ctx, s.pool, base, entry, func(tx pgx.Tx, slug string) (Label, error) {
		return insertLabelRow(ctx, tx, prepared, slug)
	})
}

// UpdateLabelAudited applies upd to the label identified by uid and writes entry in
// the same transaction. entry's TargetUID defaults to uid. It returns
// ErrLabelNotFound if no such label exists, writing no audit row.
func (s *Store) UpdateLabelAudited(
	ctx context.Context, uid string, upd LabelUpdate, entry audit.Entry,
) (Label, error) {
	base := slugify(upd.Name, labelFallbackSlug)
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	updated, err := insertAuditedWithUniqueSlug(ctx, s.pool, base, entry, func(tx pgx.Tx, slug string) (Label, error) {
		return updateLabelRow(ctx, tx, uid, upd, slug)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Label{}, ErrLabelNotFound
		}
		return Label{}, err
	}
	return updated, nil
}

// DeleteLabelAudited removes the label identified by uid and writes entry in the
// same transaction. entry's TargetUID defaults to uid. A missing label returns
// ErrLabelNotFound and writes no audit row.
func (s *Store) DeleteLabelAudited(ctx context.Context, uid string, entry audit.Entry) error {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return deleteLabelRow(ctx, tx, uid)
	})
}

// AttachLabelAudited attaches labelUID to photoUID with source and uncertainty and
// writes entry in the same transaction. entry's TargetUID defaults to labelUID. It
// behaves like AttachLabel otherwise (ErrInvalidSource for a bad source,
// ErrLabelNotFound/ErrPhotoNotFound when either side is missing — each rolls back
// and writes no audit row).
func (s *Store) AttachLabelAudited(
	ctx context.Context, photoUID, labelUID string, source LabelSource, uncertainty int, entry audit.Entry,
) error {
	normalized, err := normalizeLabelSource(source)
	if err != nil {
		return err
	}
	if entry.TargetUID == "" {
		entry.TargetUID = labelUID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return attachLabelRow(ctx, tx, photoUID, labelUID, normalized, uncertainty)
	})
}

// DetachLabelAudited removes labelUID from photoUID and writes entry in the same
// transaction. entry's TargetUID defaults to labelUID. Detaching a label that is
// not attached is a no-op but still records the action.
func (s *Store) DetachLabelAudited(ctx context.Context, photoUID, labelUID string, entry audit.Entry) error {
	if entry.TargetUID == "" {
		entry.TargetUID = labelUID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return detachLabelRow(ctx, tx, photoUID, labelUID)
	})
}
