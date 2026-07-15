package feedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation. A
// rejection insert trips it when the referenced photo, subject or label is absent.
const foreignKeyViolation = "23503"

// Store is the database access layer for persisted rejections. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// inAuditedTx opens a transaction, runs mutate, writes entry on the same
// transaction and commits, so a rejection change and its audit row are atomic: if
// either fails the transaction rolls back and neither persists. It mirrors the
// durable-audit convention used by internal/organize and internal/auth.
func (s *Store) inAuditedTx(ctx context.Context, entry audit.Entry, mutate func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("feedback: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mutate(tx); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("feedback: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("feedback: commit audited transaction: %w", err)
	}
	return nil
}

// nullable returns nil for an empty string so the column stores SQL NULL, or the
// value otherwise. It keeps rejected_by NULL for a system/unknown actor.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// isForeignKeyViolation reports whether err is a PostgreSQL foreign-key violation,
// which a rejection insert raises when the referenced photo, subject or label does
// not exist.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation
}
