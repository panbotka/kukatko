package auth

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// approveUserQuery stamps the approval time on one account and returns the
// refreshed row. updated_at moves with it: an administrator letting somebody in
// *is* an administrator editing that account, which is exactly what the
// user-administration screens read that column as.
const approveUserQuery = `UPDATE users SET approved_at = $2, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// ApproveUserAudited stamps at into approved_at for the account identified by
// uid and writes entry in the same transaction, returning the refreshed user.
// See CreateUserAudited for the atomicity guarantee. entry's TargetUID defaults
// to uid. alongside, when non-nil, runs on the same transaction with the
// approved account, after the update and before the audit entry — it is how the
// "your account is active" mail is scheduled if and only if the approval commits.
//
// An account that already carries an approval is left exactly as it is: the
// stored user is returned, nothing is updated, alongside does not run and no
// audit entry is written. Approving twice is a repeated click, not a failure,
// and the trail should not fill with entries for a decision that was already
// made.
//
// It returns ErrUserNotFound when no such account exists and ErrUserDisabled
// when the account is blocked — unblocking is its own action, and letting an
// approval do it as a side effect would hide which of the two the administrator
// meant.
func (s *Store) ApproveUserAudited(
	ctx context.Context, uid string, at time.Time, entry audit.Entry,
	alongside func(ctx context.Context, tx pgx.Tx, user User) error,
) (User, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		current, err := lockUser(ctx, tx, uid)
		if err != nil {
			return err
		}
		if current.Disabled {
			return ErrUserDisabled
		}
		if current.ApprovedAt != nil {
			user = current
			// Nothing to change, so nothing to record: inAuditedTx rolls the
			// transaction back and reports success.
			return errNoAuditableChange
		}
		approved, err := scanUpdatedUser(ctx, tx, approveUserQuery, uid, at)
		if err != nil {
			return err
		}
		user = approved
		if alongside == nil {
			return nil
		}
		return alongside(ctx, tx, approved)
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}
