package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// passwordResetColumns is the canonical, ordered column list for reset-token
// reads, matched by scanPasswordReset.
//
//nolint:gosec // G101: a list of column names, not a credential; "token_hash" is a column.
const passwordResetColumns = `id, user_uid, token_hash, created_at, expires_at, used_at, issued_by_uid`

// scanPasswordReset reads one password_reset_tokens row in
// passwordResetColumns order, returning a wrapped error on failure.
func scanPasswordReset(row pgx.Row) (PasswordResetToken, error) {
	var t PasswordResetToken
	if err := row.Scan(
		&t.ID, &t.UserUID, &t.TokenHash, &t.CreatedAt, &t.ExpiresAt, &t.UsedAt, &t.IssuedByUID,
	); err != nil {
		return PasswordResetToken{}, fmt.Errorf("auth: scanning password reset: %w", err)
	}
	return t, nil
}

// insertPasswordResetQuery writes one reset token. created_at and expires_at
// come from the caller so the link's timeline follows the service clock, the way
// a session's does.
const insertPasswordResetQuery = `INSERT INTO password_reset_tokens
	(id, user_uid, token_hash, created_at, expires_at, issued_by_uid)
	VALUES ($1, $2, $3, $4, $5, $6)`

// dropUnusedPasswordResetsQuery removes the account's outstanding links. It runs
// before every insert, which is what makes "only the most recent link works" a
// property of the table rather than of a check somebody has to remember: the
// superseded rows are simply gone. Used rows are left alone — they are the
// record that a link was consumed, and the periodic cleanup prunes them.
const dropUnusedPasswordResetsQuery = `DELETE FROM password_reset_tokens
	WHERE user_uid = $1 AND used_at IS NULL`

// usePasswordResetQuery stamps the consumption time on one link, but only while
// it is still unused, so two requests racing on the same token cannot both win.
const usePasswordResetQuery = `UPDATE password_reset_tokens SET used_at = $2
	WHERE id = $1 AND used_at IS NULL`

// setPasswordHashQuery replaces one account's password hash. updated_at moves
// with it, as it does on every other write to the row.
const setPasswordHashQuery = `UPDATE users SET password_hash = $2, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// IssuePasswordResetAudited stores tok and writes entry in the same transaction,
// returning the account the link belongs to. See CreateUserAudited for the
// atomicity guarantee. entry's TargetUID defaults to the token's user.
//
// The account's row is locked for the length of the transaction, so two
// administrators clicking at the same moment queue instead of both inserting: the
// second one's insert drops the first one's link, and exactly one outstanding
// link survives. Whatever unused links the account still held are deleted before
// the new one is written, which is the whole of the "only the most recent link
// works" rule.
//
// alongside, when non-nil, runs on the same transaction with the account, after
// the insert and before the audit entry — it is how the mail carrying the link is
// scheduled if and only if the token commits.
//
// It returns ErrUserNotFound when no such account exists and ErrUserDisabled when
// it is blocked: a link that could never be used is worse than no link, because
// somebody would wait for it.
func (s *Store) IssuePasswordResetAudited(
	ctx context.Context, tok PasswordResetToken, entry audit.Entry,
	alongside func(ctx context.Context, tx pgx.Tx, user User) error,
) (User, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = tok.UserUID
	}
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		current, err := lockUser(ctx, tx, tok.UserUID)
		if err != nil {
			return err
		}
		if current.Disabled {
			return ErrUserDisabled
		}
		if _, err := tx.Exec(ctx, dropUnusedPasswordResetsQuery, tok.UserUID); err != nil {
			return fmt.Errorf("auth: dropping earlier password resets: %w", err)
		}
		if _, err := tx.Exec(ctx, insertPasswordResetQuery,
			tok.ID, tok.UserUID, tok.TokenHash, tok.CreatedAt, tok.ExpiresAt, tok.IssuedByUID,
		); err != nil {
			return fmt.Errorf("auth: inserting password reset: %w", err)
		}
		user = current
		if alongside == nil {
			return nil
		}
		return alongside(ctx, tx, current)
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}

// GetPasswordResetByToken returns the link whose token hashes to tokenHash,
// together with the account it belongs to. A hash that matches nothing is
// ErrPasswordResetInvalid rather than a "not found" of its own: every caller is
// unauthenticated and must not learn which of the ways a link can be unusable it
// hit.
//
// It reports the row exactly as stored, expiry and consumption included — the
// judgement of whether it is still good is the caller's
// (PasswordResetToken.Usable), because the answer depends on the service clock.
// The account is read in a second statement rather than joined, because both
// tables carry a created_at and a joined column list that drifts apart from
// either scan is a bug waiting to happen.
func (s *Store) GetPasswordResetByToken(
	ctx context.Context, tokenHash string,
) (PasswordResetToken, User, error) {
	q := `SELECT ` + passwordResetColumns + ` FROM password_reset_tokens WHERE token_hash = $1`
	row, err := scanPasswordReset(s.pool.QueryRow(ctx, q, tokenHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PasswordResetToken{}, User{}, ErrPasswordResetInvalid
		}
		return PasswordResetToken{}, User{}, err
	}
	user, err := s.GetUserByUID(ctx, row.UserUID)
	if err != nil {
		return PasswordResetToken{}, User{}, err
	}
	return row, user, nil
}

// ConsumePasswordResetAudited spends the link whose token hashes to tokenHash:
// it stores passwordHash on the account, stamps the link used at `at`, deletes
// every session the account has, and writes entry — all in one transaction, so a
// link is burnt if and only if the password it set is really stored. See
// CreateUserAudited for the atomicity guarantee.
//
// The link and the account are both locked for the length of the transaction, so
// two requests carrying the same token queue and the second finds it already
// used. It returns ErrPasswordResetInvalid for a link that is unknown, already
// used, expired or whose account has been blocked since it was issued.
func (s *Store) ConsumePasswordResetAudited(
	ctx context.Context, tokenHash, passwordHash string, at time.Time, entry audit.Entry,
) (User, error) {
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		row, err := lockPasswordReset(ctx, tx, tokenHash)
		if err != nil {
			return err
		}
		if !row.Usable(at) {
			return ErrPasswordResetInvalid
		}
		current, err := lockUser(ctx, tx, row.UserUID)
		if err != nil {
			return err
		}
		if current.Disabled {
			return ErrPasswordResetInvalid
		}
		user, err = consumePasswordReset(ctx, tx, row, passwordHash, at)
		return err
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}

// consumePasswordReset performs the three writes of a consumed link on tx: the
// new password hash, the used stamp, and the removal of every session the account
// holds. The used stamp is conditional on the link still being unused, so a
// racing second request that got past the lock finds nothing to update and is
// refused with ErrPasswordResetInvalid.
func consumePasswordReset(
	ctx context.Context, tx pgx.Tx, row PasswordResetToken, passwordHash string, at time.Time,
) (User, error) {
	tag, err := tx.Exec(ctx, usePasswordResetQuery, row.ID, at)
	if err != nil {
		return User{}, fmt.Errorf("auth: consuming password reset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return User{}, ErrPasswordResetInvalid
	}
	user, err := scanUpdatedUser(ctx, tx, setPasswordHashQuery, row.UserUID, passwordHash)
	if err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_uid = $1", row.UserUID); err != nil {
		return User{}, fmt.Errorf("auth: deleting user sessions: %w", err)
	}
	return user, nil
}

// lockPasswordReset reads one reset token by hash and locks its row for the rest
// of the transaction, so the decision that follows is made on a row nobody else
// can consume underneath it. An unknown hash is ErrPasswordResetInvalid.
func lockPasswordReset(ctx context.Context, tx pgx.Tx, tokenHash string) (PasswordResetToken, error) {
	q := `SELECT ` + passwordResetColumns + ` FROM password_reset_tokens WHERE token_hash = $1 FOR UPDATE`
	row, err := scanPasswordReset(tx.QueryRow(ctx, q, tokenHash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PasswordResetToken{}, ErrPasswordResetInvalid
		}
		return PasswordResetToken{}, err
	}
	return row, nil
}

// DeleteFinishedPasswordResets removes every reset link that can no longer be
// used as of now — expired, or already consumed — returning how many were
// deleted. It runs beside the expired-session cleanup, on the same schedule: both
// are rows whose only remaining purpose was to be refused.
func (s *Store) DeleteFinishedPasswordResets(ctx context.Context, now time.Time) (int64, error) {
	const q = `DELETE FROM password_reset_tokens WHERE expires_at <= $1 OR used_at IS NOT NULL`
	tag, err := s.pool.Exec(ctx, q, now)
	if err != nil {
		return 0, fmt.Errorf("auth: deleting finished password resets: %w", err)
	}
	return tag.RowsAffected(), nil
}
