package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// Store is the database access layer for the single instance-wide settings row.
// It owns no connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// selectColumns is the column list every read and the upsert's RETURNING share,
// so the two can never drift. updated_by_uid is coalesced to the empty string so
// a NULL (a since-deleted administrator) scans without a pointer.
const selectColumns = `registration_enabled, registration_secret, welcome_markdown,
       updated_at, COALESCE(updated_by_uid, '') AS updated_by_uid`

// getSQL reads the single settings row.
const getSQL = `SELECT ` + selectColumns + ` FROM instance_settings WHERE id = true`

// Get returns the instance settings. Migration 0062 seeds the row, so the read
// normally finds it; a missing row yields the zero-value defaults (registration
// closed, no secret, no welcome text) rather than an error, because the
// anonymous sign-in screen reads this and must never be blocked by a settings
// row that somebody removed by hand.
//
// The returned record includes the registration secret in readable form. Callers
// serving anyone below the admin role must not pass it on.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	var out Settings
	err := s.pool.QueryRow(ctx, getSQL).Scan(
		&out.RegistrationEnabled, &out.RegistrationSecret, &out.WelcomeMarkdown,
		&out.UpdatedAt, &out.UpdatedByUID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("settings: reading instance settings: %w", err)
	}
	return out, nil
}

// upsertSQL writes all three values at once, replacing whatever was there. The
// single-row table (id pinned to true) makes this an upsert on the fixed primary
// key, and updated_at is stamped to now() on every write.
const upsertSQL = `
INSERT INTO instance_settings (id, registration_enabled, registration_secret, welcome_markdown,
                               updated_at, updated_by_uid)
VALUES (true, $1, $2, $3, now(), $4)
ON CONFLICT (id) DO UPDATE
SET registration_enabled = EXCLUDED.registration_enabled,
    registration_secret = EXCLUDED.registration_secret,
    welcome_markdown = EXCLUDED.welcome_markdown,
    updated_at = now(),
    updated_by_uid = EXCLUDED.updated_by_uid
RETURNING ` + selectColumns

// Set replaces all three settings with in, stamped as changed by actorUID, and
// writes entry to the audit log in the same transaction so the change and the
// record of who made it commit atomically or not at all.
//
// The three values are written together because the registration flag and the
// secret guard each other: enabling registration while the secret is blank (or
// only whitespace) returns ErrSecretRequired and nothing is written. The secret
// is stored trimmed; the welcome Markdown is stored verbatim. entry's details
// are set by the caller and must not carry the secret.
func (s *Store) Set(ctx context.Context, in Update, actorUID string, entry audit.Entry) (Settings, error) {
	normalized, err := in.validate()
	if err != nil {
		return Settings{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("settings: begin update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var out Settings
	if scanErr := tx.QueryRow(ctx, upsertSQL,
		normalized.RegistrationEnabled, normalized.RegistrationSecret, normalized.WelcomeMarkdown,
		nullableUID(actorUID),
	).Scan(
		&out.RegistrationEnabled, &out.RegistrationSecret, &out.WelcomeMarkdown,
		&out.UpdatedAt, &out.UpdatedByUID,
	); scanErr != nil {
		return Settings{}, fmt.Errorf("settings: writing instance settings: %w", scanErr)
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return Settings{}, fmt.Errorf("settings: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("settings: commit update transaction: %w", err)
	}
	return out, nil
}

// nullableUID returns nil for an empty UID so the updated_by_uid column stores
// SQL NULL, or the value otherwise. An admin guard makes a non-empty UID the
// norm, but a pass-through guard (unit tests) may leave it empty.
func nullableUID(uid string) any {
	if uid == "" {
		return nil
	}
	return uid
}
