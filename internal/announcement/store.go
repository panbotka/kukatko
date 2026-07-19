package announcement

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// Store is the database access layer for the single instance-wide announcement.
// It owns no connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// getSQL reads the single announcement row. author_uid is coalesced to the empty
// string so a NULL author (a since-deleted user) scans without a pointer.
const getSQL = `
SELECT message, level, COALESCE(author_uid, '') AS author_uid, updated_at
FROM announcements
WHERE id = true`

// Get returns the currently published announcement, or ErrNotFound when none is
// set. The HTTP layer maps ErrNotFound to an empty-message response so the banner
// client never has to special-case a 404.
func (s *Store) Get(ctx context.Context) (Announcement, error) {
	var a Announcement
	err := s.pool.QueryRow(ctx, getSQL).Scan(&a.Message, &a.Level, &a.AuthorUID, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Announcement{}, ErrNotFound
	}
	if err != nil {
		return Announcement{}, fmt.Errorf("announcement: reading current: %w", err)
	}
	return a, nil
}

// upsertSQL publishes the announcement, replacing any existing one. The single-row
// table (id pinned to true) makes this an upsert on the fixed primary key, and
// updated_at is stamped to now() on every publish so the client can detect a fresh
// message.
const upsertSQL = `
INSERT INTO announcements (id, message, level, author_uid, updated_at)
VALUES (true, $1, $2, $3, now())
ON CONFLICT (id) DO UPDATE
SET message = EXCLUDED.message,
    level = EXCLUDED.level,
    author_uid = EXCLUDED.author_uid,
    updated_at = now()
RETURNING message, level, COALESCE(author_uid, '') AS author_uid, updated_at`

// Set publishes message at the given level authored by authorUID, replacing any
// existing announcement, and writes entry to the audit log in the same
// transaction so the banner change and the record of who made it commit
// atomically. An empty (or whitespace-only) message returns ErrEmptyMessage and
// an unrecognised level returns ErrInvalidLevel; an empty level defaults to
// LevelInfo. entry's TargetType/TargetUID and details are set by the caller.
func (s *Store) Set(
	ctx context.Context, message, level, authorUID string, entry audit.Entry,
) (Announcement, error) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return Announcement{}, ErrEmptyMessage
	}
	normLevel, err := normalizeLevel(level)
	if err != nil {
		return Announcement{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Announcement{}, fmt.Errorf("announcement: begin publish transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var a Announcement
	if scanErr := tx.QueryRow(ctx, upsertSQL, trimmed, normLevel, nullableUID(authorUID)).
		Scan(&a.Message, &a.Level, &a.AuthorUID, &a.UpdatedAt); scanErr != nil {
		return Announcement{}, fmt.Errorf("announcement: publishing: %w", scanErr)
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return Announcement{}, fmt.Errorf("announcement: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Announcement{}, fmt.Errorf("announcement: commit publish transaction: %w", err)
	}
	return a, nil
}

// Clear takes the announcement down for all users and writes entry to the audit
// log in the same transaction. Clearing when none is published is a no-op on the
// table but still records the action, so the audit trail always reflects the
// maintainer's intent (mirroring how organize records an idempotent detach).
func (s *Store) Clear(ctx context.Context, entry audit.Entry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("announcement: begin clear transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM announcements WHERE id = true"); err != nil {
		return fmt.Errorf("announcement: clearing: %w", err)
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("announcement: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("announcement: commit clear transaction: %w", err)
	}
	return nil
}

// nullableUID returns nil for an empty UID so the author_uid column stores SQL
// NULL, or the value otherwise. A maintainer guard makes a non-empty UID the norm,
// but a pass-through guard (unit tests) may leave it empty.
func nullableUID(uid string) any {
	if uid == "" {
		return nil
	}
	return uid
}
