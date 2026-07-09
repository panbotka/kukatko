package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// apiTokenColumns is the canonical, ordered column list for API-token reads,
// matched by scanAPIToken.
//
//nolint:gosec // G101: a list of column names, not a credential; "secret_hash" is a column.
const apiTokenColumns = `id, user_uid, name, secret_hash, created_at, expires_at,
	last_used_at, revoked_at`

// scanAPIToken reads one api_tokens row in apiTokenColumns order, returning a
// wrapped error on failure.
func scanAPIToken(row pgx.Row) (APIToken, error) {
	var t APIToken
	if err := row.Scan(
		&t.ID, &t.UserUID, &t.Name, &t.SecretHash, &t.CreatedAt,
		&t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt,
	); err != nil {
		return APIToken{}, fmt.Errorf("auth: scanning api token: %w", err)
	}
	return t, nil
}

// CreateAPITokenAudited inserts tok and writes entry to the audit log in the
// same transaction, so the token row and the record of who minted it commit
// atomically (the durable-audit convention; see internal/audit). entry's
// TargetUID defaults to the token's id. created_at is written from the
// caller-supplied value so the token's timeline follows the service clock.
func (s *Store) CreateAPITokenAudited(ctx context.Context, tok APIToken, entry audit.Entry) error {
	const q = `INSERT INTO api_tokens (id, user_uid, name, secret_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	if entry.TargetUID == "" {
		entry.TargetUID = tok.ID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q, tok.ID, tok.UserUID, tok.Name, tok.SecretHash, tok.CreatedAt, tok.ExpiresAt)
		if err != nil {
			return fmt.Errorf("auth: inserting api token: %w", err)
		}
		return nil
	})
}

// RevokeAPITokenAudited stamps revoked_at on the token identified by id and
// writes entry in the same transaction, reporting whether a row actually
// changed. A token that was already revoked (or vanished with its owner between
// the caller's lookup and this call) changes nothing: the function reports false
// and writes no audit entry, which keeps revocation idempotent.
func (s *Store) RevokeAPITokenAudited(
	ctx context.Context, id string, at time.Time, entry audit.Entry,
) (bool, error) {
	const q = `UPDATE api_tokens SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`
	if entry.TargetUID == "" {
		entry.TargetUID = id
	}
	revoked := false
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, q, id, at)
		if err != nil {
			return fmt.Errorf("auth: revoking api token: %w", err)
		}
		revoked = tag.RowsAffected() > 0
		if !revoked {
			return errNoAuditableChange
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return revoked, nil
}

// errNoAuditableChange is returned by an inAuditedTx mutation that decided there
// was nothing to change. It aborts the transaction without writing an audit
// entry and is swallowed by inAuditedTx rather than surfaced to the caller.
var errNoAuditableChange = errors.New("auth: no auditable change")

// inAuditedTx opens a transaction, runs mutate, writes entry on the same
// transaction and commits, so a mutation and its audit row are atomic: if either
// fails the transaction rolls back and neither persists. A mutate that returns
// errNoAuditableChange rolls back and reports success, letting a no-op mutation
// skip the audit entry.
func (s *Store) inAuditedTx(ctx context.Context, entry audit.Entry, mutate func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin audited transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mutate(tx); err != nil {
		if errors.Is(err, errNoAuditableChange) {
			return nil
		}
		return err
	}
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("auth: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit audited transaction: %w", err)
	}
	return nil
}

// GetAPITokenByID returns the token with the given id, or ErrAPITokenNotFound.
// It is the single indexed lookup behind bearer authentication: the id travels
// in the plaintext credential precisely so no scan over hashes is needed.
func (s *Store) GetAPITokenByID(ctx context.Context, id string) (APIToken, error) {
	q := "SELECT " + apiTokenColumns + " FROM api_tokens WHERE id = $1"
	tok, err := scanAPIToken(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIToken{}, ErrAPITokenNotFound
		}
		return APIToken{}, err
	}
	return tok, nil
}

// ListAPITokensByUser returns every token belonging to userUID, newest first,
// including revoked and expired ones so the owner can see their full history.
// The slice is empty (not nil) when the user has no tokens.
func (s *Store) ListAPITokensByUser(ctx context.Context, userUID string) ([]APIToken, error) {
	q := "SELECT " + apiTokenColumns + " FROM api_tokens WHERE user_uid = $1 ORDER BY created_at DESC, id"
	rows, err := s.pool.Query(ctx, q, userUID)
	if err != nil {
		return nil, fmt.Errorf("auth: querying api tokens: %w", err)
	}
	defer rows.Close()

	tokens := make([]APIToken, 0)
	for rows.Next() {
		tok, scanErr := scanAPIToken(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tokens = append(tokens, tok)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating api tokens: %w", err)
	}
	return tokens, nil
}

// TouchAPIToken records that the token identified by id was used at `at`. A
// missing token is not an error (the caller has just authenticated with it, and
// a concurrent revocation is not worth failing the request over).
func (s *Store) TouchAPIToken(ctx context.Context, id string, at time.Time) error {
	const q = `UPDATE api_tokens SET last_used_at = $2 WHERE id = $1`
	if _, err := s.pool.Exec(ctx, q, id, at); err != nil {
		return fmt.Errorf("auth: updating api token last_used_at: %w", err)
	}
	return nil
}
