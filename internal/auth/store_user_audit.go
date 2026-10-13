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
	return s.CreateUserAuditedWith(ctx, u, entry, nil)
}

// CreateUserAuditedWith is CreateUserAudited with one more step: alongside runs
// on the same transaction, after the insert and before the audit entry, so
// whatever it writes shares the account's fate exactly.
//
// It exists for self-service registration, which schedules its two mails through
// it (see internal/mailjob): a job enqueued on this transaction is scheduled if
// and only if the account is really created, so a registration that rolls back
// sends nothing. A nil alongside makes this plain CreateUserAudited.
func (s *Store) CreateUserAuditedWith(
	ctx context.Context, u User, entry audit.Entry, alongside func(ctx context.Context, tx pgx.Tx) error,
) error {
	if entry.TargetUID == "" {
		entry.TargetUID = u.UID
	}
	return s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		if err := insertUser(ctx, tx, u); err != nil {
			return err
		}
		if alongside == nil {
			return nil
		}
		return alongside(ctx, tx)
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
	return s.updateUserAudited(ctx, entry, profileUpdate(ctx, uid, in))
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
	return s.updateUserAudited(ctx, entry, func(tx pgx.Tx) (User, error) {
		return scanUpdatedUser(ctx, tx, setUserDisabledQuery, uid, disabled)
	})
}

// setInstanceRoleQuery replaces one account's instance role.
const setInstanceRoleQuery = `UPDATE users SET role = $2, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// SetInstanceRoleAudited sets the instance role (user or maintainer) of the
// account identified by uid, bumps updated_at, and writes entry in the same
// transaction, returning the refreshed user. It touches no project membership,
// and who may call it — a maintainer, never demoting themselves — is the
// caller's rule. See CreateUserAudited for the atomicity guarantee. entry's
// TargetUID defaults to uid. It returns ErrInvalidRole for a role that is not
// an instance role, ErrUserNotFound if no such user exists, or
// ErrLastMaintainer when the demotion would leave the instance without a single
// enabled maintainer (see withMaintainerGuard); in every case nothing changes
// and no audit row is written.
func (s *Store) SetInstanceRoleAudited(ctx context.Context, uid string, role Role, entry audit.Entry) (User, error) {
	if !role.Valid() {
		return User{}, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.updateUserAudited(ctx, entry, func(tx pgx.Tx) (User, error) {
		return scanUpdatedUser(ctx, tx, setInstanceRoleQuery, uid, string(role))
	})
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

// updateUserAudited runs update — one account write that returns the refreshed
// user — inside an audited transaction, under the last-maintainer guard. A
// missing user becomes ErrUserNotFound and a change that would strand the
// instance becomes ErrLastMaintainer; either rolls the transaction back, so
// neither the change nor its audit row is written. It is shared by the
// profile-update, instance-role and disable audited writes.
func (s *Store) updateUserAudited(
	ctx context.Context, entry audit.Entry, update func(pgx.Tx) (User, error),
) (User, error) {
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		return withMaintainerGuard(ctx, tx, func(tx pgx.Tx) error {
			u, updateErr := update(tx)
			if updateErr != nil {
				return updateErr
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
