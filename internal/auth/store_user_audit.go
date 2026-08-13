package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// CreateUserAudited inserts u and writes entry to the audit log in the same
// transaction, so the new account and the record of who created it commit
// atomically (the durable-audit convention; see internal/audit). entry's
// TargetUID defaults to u.UID. It returns ErrUsernameTaken on a duplicate
// username, ErrSubjectNotFound when u.SubjectUID names no subject, or a wrapped
// error otherwise.
func (s *Store) CreateUserAudited(ctx context.Context, u User, entry audit.Entry) error {
	const q = `INSERT INTO users
			(uid, username, display_name, email, password_hash, role, disabled, note, subject_uid)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	if entry.TargetUID == "" {
		entry.TargetUID = u.UID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, q,
			u.UID, u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, u.Disabled, u.Note,
			normalizeSubjectUID(u.SubjectUID))
		if err != nil {
			if isUniqueViolation(err) {
				return ErrUsernameTaken
			}
			if isForeignKeyViolation(err) {
				return ErrSubjectNotFound
			}
			return fmt.Errorf("auth: inserting user: %w", err)
		}
		return nil
	})
}

// UpdateUserProfileAudited updates the mutable profile fields of the user
// identified by uid and writes entry in the same transaction, returning the
// refreshed user. See CreateUserAudited for the atomicity guarantee. entry's
// TargetUID defaults to uid. It returns ErrUserNotFound if no such user exists,
// ErrSubjectNotFound when in.SubjectUID names no subject, or ErrLastMaintainer
// when the change would leave the instance without a single enabled maintainer
// (see withMaintainerGuard); in every case nothing changes and no audit row is
// written.
func (s *Store) UpdateUserProfileAudited(
	ctx context.Context, uid string, in UpdateUserInput, entry audit.Entry,
) (User, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.updateUserReturningAudited(ctx, entry, updateUserProfileQuery,
		uid, in.DisplayName, in.Email, in.Role, in.Disabled, in.Note,
		normalizeSubjectUID(in.SubjectUID))
}

// SetUserDisabledAudited flips the disabled flag for the user identified by uid,
// bumps updated_at, and writes entry in the same transaction, returning the
// refreshed user. See CreateUserAudited for the atomicity guarantee. entry's
// TargetUID defaults to uid. It returns ErrUserNotFound if no such user exists,
// or ErrLastMaintainer when disabling would leave the instance without a single
// enabled maintainer (see withMaintainerGuard).
func (s *Store) SetUserDisabledAudited(
	ctx context.Context, uid string, disabled bool, entry audit.Entry,
) (User, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.updateUserReturningAudited(ctx, entry, setUserDisabledQuery, uid, disabled)
}

// SetPasswordHashAudited replaces the password hash for the user identified by
// uid, bumps updated_at, and writes entry in the same transaction. See
// CreateUserAudited for the atomicity guarantee. entry's TargetUID defaults to
// uid. It returns ErrUserNotFound if no row was affected (and writes no audit
// row).
func (s *Store) SetPasswordHashAudited(ctx context.Context, uid, hash string, entry audit.Entry) error {
	const q = `UPDATE users SET password_hash = $2, updated_at = now() WHERE uid = $1`
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, q, uid, hash)
		if err != nil {
			return fmt.Errorf("auth: updating password hash: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrUserNotFound
		}
		return nil
	})
}

// updateUserReturningAudited runs an "UPDATE ... RETURNING userColumns" statement
// inside an audited transaction, under the last-maintainer guard, and returns the
// refreshed user. A missing user (pgx.ErrNoRows) becomes ErrUserNotFound and a
// change that would strand the instance becomes ErrLastMaintainer; either rolls
// the transaction back, so neither the change nor its audit row is written. It is
// shared by the profile-update and disable audited writes, which differ only in
// their SQL and arguments.
func (s *Store) updateUserReturningAudited(
	ctx context.Context, entry audit.Entry, query string, args ...any,
) (User, error) {
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return withMaintainerGuard(ctx, tx, func(tx pgx.Tx) error {
			u, scanErr := scanUpdatedUser(ctx, tx, query, args...)
			if scanErr != nil {
				return scanErr
			}
			user = u
			return nil
		})
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}
