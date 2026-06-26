package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// sessionColumns is the canonical, ordered column list for session reads,
// matched by scanSession.
const sessionColumns = `id, token, download_token, user_uid, role, created_at, expires_at`

// scanSession reads one session row in sessionColumns order, returning a wrapped
// error on failure.
func scanSession(row pgx.Row) (Session, error) {
	var sess Session
	if err := row.Scan(
		&sess.ID, &sess.Token, &sess.DownloadToken, &sess.UserUID,
		&sess.Role, &sess.CreatedAt, &sess.ExpiresAt,
	); err != nil {
		return Session{}, fmt.Errorf("auth: scanning session: %w", err)
	}
	return sess, nil
}

// CreateSession inserts sess. Both created_at and expires_at are written
// explicitly from the caller-supplied values so a session's timeline is governed
// by the service clock (which keeps the sliding-expiry cap consistent and
// testable) rather than the database wall clock. It returns a wrapped error on
// failure.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	const q = `INSERT INTO sessions (id, token, download_token, user_uid, role, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := s.pool.Exec(ctx, q,
		sess.ID, sess.Token, sess.DownloadToken, sess.UserUID, sess.Role, sess.CreatedAt, sess.ExpiresAt)
	if err != nil {
		return fmt.Errorf("auth: inserting session: %w", err)
	}
	return nil
}

// GetSessionByToken returns the session with the given opaque token, or
// ErrSessionNotFound if none matches.
func (s *Store) GetSessionByToken(ctx context.Context, token string) (Session, error) {
	q := "SELECT " + sessionColumns + " FROM sessions WHERE token = $1"
	sess, err := scanSession(s.pool.QueryRow(ctx, q, token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, err
	}
	return sess, nil
}

// UpdateSessionExpiry sets a new expiry for the session identified by id. It
// returns ErrSessionNotFound if no row was affected.
func (s *Store) UpdateSessionExpiry(ctx context.Context, id string, expiresAt time.Time) error {
	const q = `UPDATE sessions SET expires_at = $2 WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id, expiresAt)
	if err != nil {
		return fmt.Errorf("auth: updating session expiry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// DeleteSessionByToken removes the session with the given token (logout). A
// missing session is not an error: logout is idempotent.
func (s *Store) DeleteSessionByToken(ctx context.Context, token string) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token = $1", token); err != nil {
		return fmt.Errorf("auth: deleting session: %w", err)
	}
	return nil
}

// DeleteUserSessionsExcept removes every session for userUID except the one
// whose token equals keepToken, returning how many were deleted. It backs the
// "password change invalidates this user's other sessions" rule.
func (s *Store) DeleteUserSessionsExcept(
	ctx context.Context, userUID, keepToken string,
) (int64, error) {
	const q = `DELETE FROM sessions WHERE user_uid = $1 AND token <> $2`
	tag, err := s.pool.Exec(ctx, q, userUID, keepToken)
	if err != nil {
		return 0, fmt.Errorf("auth: deleting other user sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteUserSessions removes every session for userUID, returning how many were
// deleted. It backs admin password reset and account disabling.
func (s *Store) DeleteUserSessions(ctx context.Context, userUID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE user_uid = $1", userUID)
	if err != nil {
		return 0, fmt.Errorf("auth: deleting user sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpiredSessions removes all sessions whose expiry is at or before now,
// returning how many were deleted. It backs the hourly cleanup job.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= $1", now)
	if err != nil {
		return 0, fmt.Errorf("auth: deleting expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
