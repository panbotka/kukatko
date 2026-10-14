package auth

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
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

// UpdateUserProfileAudited applies the patch in to the account identified by
// uid — only the fields in sets; a nil field keeps what the row holds when the
// UPDATE takes its lock, so an edit never overwrites a change another
// administrator made meanwhile — and writes entry in the same transaction,
// returning the refreshed user. An empty patch writes nothing (no audit row,
// updated_at untouched) and returns the account as it is. See
// CreateUserAudited for the atomicity guarantee. entry's TargetUID defaults to
// uid. It returns ErrUserNotFound if no such user exists, or ErrLastMaintainer
// when the change would leave the instance without a single enabled
// maintainer (see withMaintainerGuard); in every case nothing changes and no
// audit row is written. A patch that disables the last working admin of a
// project is allowed and noted (see updateUserNotingStranded).
func (s *Store) UpdateUserProfileAudited(
	ctx context.Context, uid string, in UpdateUserInput, entry audit.Entry,
) (User, error) {
	if in.empty() {
		return s.GetUserByUID(ctx, uid)
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.updateUserNotingStranded(ctx, uid, in.Disabled, entry, profileUpdate(ctx, uid, in))
}

// SetUserDisabledAudited flips the disabled flag for the user identified by uid,
// bumps updated_at, and writes entry in the same transaction, returning the
// refreshed user. See CreateUserAudited for the atomicity guarantee. entry's
// TargetUID defaults to uid. It returns ErrUserNotFound if no such user exists,
// or ErrLastMaintainer when disabling would leave the instance without a single
// enabled maintainer (see withMaintainerGuard). Disabling the last working admin
// of a project is allowed and noted (see updateUserNotingStranded).
func (s *Store) SetUserDisabledAudited(
	ctx context.Context, uid string, disabled bool, entry audit.Entry,
) (User, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	return s.updateUserNotingStranded(ctx, uid, &disabled, entry, func(tx pgx.Tx) (User, error) {
		return scanUpdatedUser(ctx, tx, setUserDisabledQuery, uid, disabled)
	})
}

// strandedProjectsQuery lists, by uid, the projects in which the account $1 is
// a working admin (an approved admin membership) and no other working admin —
// approved, account not disabled — remains: the projects a disable of $1
// leaves without anybody to govern them but a maintainer.
const strandedProjectsQuery = `SELECT m.project_uid FROM project_members m
WHERE m.user_uid = $1 AND m.role = $2 AND m.approved_at IS NOT NULL
	AND NOT EXISTS (
		SELECT 1 FROM project_members o JOIN users u ON u.uid = o.user_uid
		WHERE o.project_uid = m.project_uid AND o.user_uid <> $1 AND o.role = $2
			AND o.approved_at IS NOT NULL AND NOT u.disabled)
ORDER BY m.project_uid`

// strandedProjectsKey is the audit detail naming the projects a disable left
// without a working admin.
const strandedProjectsKey = "stranded_projects"

// updateUserNotingStranded runs update like updateUserAudited and, when the
// write sets the disabled flag (disabled non-nil), first takes the locks the
// membership changes take: the row of every project the account is a member of
// (see lockMemberProjects), then — under the maintainer guard — the account's
// own row. Holding them, a disable that turns an enabled account disabled lists
// the projects it is the last working admin of (see strandedProjectsQuery).
//
// The project locks are what make the list true. The last-admin guard of a
// role change or a removal (internal/project's ensureAnotherAdmin) runs under
// the project row's lock, so a disable that takes the same lock is serialised
// with it: either the demotion of the other admin commits first and this
// disable sees the project stranded, or this disable commits first and the
// demotion is refused. Without it the two could each see the other admin still
// working and leave the project with nobody to govern it, unnoted.
//
// Disabling such an account is allowed — a maintainer locking somebody out
// must be able to, and may name a new admin afterwards — but never silent: the
// list rides the audit entry under "stranded_projects" and is logged as a
// warning once the change commits. It is noted only on the enabled → disabled
// transition: re-disabling a blocked account, or an edit that leaves it
// blocked, takes no working admin away and notes nothing. entry.Details is
// copied first, so the caller's map is never written.
func (s *Store) updateUserNotingStranded(
	ctx context.Context, uid string, disabled *bool, entry audit.Entry, update func(pgx.Tx) (User, error),
) (User, error) {
	if disabled == nil {
		return s.updateUserAudited(ctx, entry, update)
	}
	entry.Details = maps.Clone(entry.Details)
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	var stranded []string
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		if err := lockMemberProjects(ctx, tx, uid); err != nil {
			return err
		}
		return withMaintainerGuard(ctx, tx, func(tx pgx.Tx) error {
			var err error
			if stranded, err = strandedByDisable(ctx, tx, uid, *disabled); err != nil {
				return err
			}
			if len(stranded) > 0 {
				entry.Details[strandedProjectsKey] = stranded
			}
			user, err = update(tx)
			return err
		})
	})
	if err != nil {
		return User{}, err
	}
	if len(stranded) > 0 {
		slog.WarnContext(ctx, "auth: disabled account was the last working admin of projects",
			"user_uid", uid, "projects", stranded)
	}
	return user, nil
}

// lockMemberProjectsQuery locks the row of every project the account $1 is a
// member of, in uid order — the lock internal/project's membership changes
// take before they touch a member (lockProject), in the order two multi-project
// lockers need so they queue instead of deadlocking. Any role counts, not only
// admin: an existing member promoted to admin meanwhile is promoted under that
// same lock.
const lockMemberProjectsQuery = `SELECT p.uid FROM projects p
WHERE EXISTS (SELECT 1 FROM project_members m WHERE m.project_uid = p.uid AND m.user_uid = $1)
ORDER BY p.uid FOR UPDATE OF p`

// lockMemberProjects runs lockMemberProjectsQuery on tx until it locks no
// project it had not locked before. The repeat covers a membership created
// while the first pass waited (an admin adding the account to another project
// commits under that project's lock, which the first pass never asked for), so
// once it returns every project the account belongs to is held. A later pass
// may lock out of uid order; PostgreSQL detects the deadlock that could cause
// and fails one transaction rather than hanging, and it needs a membership
// created mid-disable to happen at all.
func lockMemberProjects(ctx context.Context, tx pgx.Tx, uid string) error {
	held := map[string]bool{}
	for {
		rows, err := tx.Query(ctx, lockMemberProjectsQuery, uid)
		if err != nil {
			return fmt.Errorf("auth: locking the projects of %s: %w", uid, err)
		}
		locked, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("auth: locking the projects of %s: %w", uid, err)
		}
		fresh := false
		for _, p := range locked {
			if !held[p] {
				held[p], fresh = true, true
			}
		}
		if !fresh {
			return nil
		}
	}
}

// strandedByDisable locks the account uid's row on tx and, when the write about
// to set its disabled flag to disabled turns an enabled account disabled,
// returns the projects it leaves without a working admin (see
// strandedProjects). Any other write of the flag — an enable, or a disable of
// an account already blocked — strands nothing new and returns none. A missing
// account is ErrUserNotFound.
func strandedByDisable(ctx context.Context, tx pgx.Tx, uid string, disabled bool) ([]string, error) {
	current, err := lockUser(ctx, tx, uid)
	if err != nil {
		return nil, err
	}
	if !disabled || current.Disabled {
		return nil, nil
	}
	return strandedProjects(ctx, tx, uid)
}

// strandedProjects runs strandedProjectsQuery for uid on tx; none is an empty
// (nil) slice.
func strandedProjects(ctx context.Context, tx pgx.Tx, uid string) ([]string, error) {
	rows, err := tx.Query(ctx, strandedProjectsQuery, uid, string(project.RoleAdmin))
	if err != nil {
		return nil, fmt.Errorf("auth: listing the projects %s alone governs: %w", uid, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("auth: listing the projects %s alone governs: %w", uid, err)
	}
	return out, nil
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
// instance-role write and the profile updates that leave the disabled flag
// alone; a write of the flag goes through updateUserNotingStranded.
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
