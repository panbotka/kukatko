package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// lockEnabledMaintainersQuery locks every enabled maintainer row and reports how
// many there are, in one round trip. The lock is what makes the guard safe under
// concurrency: two simultaneous demotions of two *different* maintainers would
// otherwise each still see the other one and both commit, landing on exactly the
// zero-maintainer state the guard exists to prevent. Rows are locked in uid order
// so two such transactions queue behind one another instead of deadlocking.
//
// The count is taken over the CTE rather than the table so it describes precisely
// the set that was locked.
const lockEnabledMaintainersQuery = `
	WITH locked AS (
		SELECT uid FROM users WHERE role = $1 AND disabled = false ORDER BY uid FOR UPDATE
	)
	SELECT count(*) FROM locked`

// countEnabledMaintainersQuery counts the accounts that still carry the
// instance's operations capability: users on the maintainer role that are not
// disabled. A disabled maintainer cannot log in, so it does not count.
const countEnabledMaintainersQuery = `SELECT count(*) FROM users WHERE role = $1 AND disabled = false`

// strandsInstance reports whether a mutation that took the number of enabled
// maintainers from before to after must be refused.
//
// The rule is deliberately "would drop it to zero", not "must end above zero":
// an instance that already has no enabled maintainer (a database seeded without
// one, or a bootstrap that never ran) must stay fully manageable, otherwise the
// guard would block every unrelated user edit on it as well.
func strandsInstance(before, after int) bool {
	return before > 0 && after == 0
}

// countEnabledMaintainers returns the number of enabled maintainer accounts
// visible to tx, wrapping query failures.
func countEnabledMaintainers(ctx context.Context, tx pgx.Tx, query string) (int, error) {
	var n int
	if err := tx.QueryRow(ctx, query, RoleMaintainer).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: counting enabled maintainers: %w", err)
	}
	return n, nil
}

// withMaintainerGuard runs mutate on tx and returns ErrLastMaintainer when the
// mutation would leave an instance that still had an enabled maintainer with
// none at all. Returning an error rolls the caller's transaction back, so the
// refused change — and any audit row written alongside it — never commits.
//
// Every write that can strip the maintainer capability from an account has to go
// through here: the role change and the disable, whether audited or not, and any
// delete-user path added later. Counting enabled maintainers before and after the
// mutation keeps the guard indifferent to *how* the capability was lost, so a new
// path is covered by wrapping it rather than by restating the rule.
func withMaintainerGuard(ctx context.Context, tx pgx.Tx, mutate func(pgx.Tx) error) error {
	before, err := countEnabledMaintainers(ctx, tx, lockEnabledMaintainersQuery)
	if err != nil {
		return err
	}
	if err := mutate(tx); err != nil {
		return err
	}
	after, err := countEnabledMaintainers(ctx, tx, countEnabledMaintainersQuery)
	if err != nil {
		return err
	}
	if strandsInstance(before, after) {
		return ErrLastMaintainer
	}
	return nil
}

// CountEnabledMaintainers returns how many accounts hold the maintainer role and
// are not disabled. It is the read-only view of the invariant that
// withMaintainerGuard protects.
func (s *Store) CountEnabledMaintainers(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, countEnabledMaintainersQuery, RoleMaintainer).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: counting enabled maintainers: %w", err)
	}
	return n, nil
}

// inGuardedTx runs mutate in its own transaction under withMaintainerGuard. It
// serves the non-audited update and disable paths (Service.UpdateUser and
// Service.SetUserDisabled, kept for test seeding), which have no audit entry and
// therefore no transaction of their own; the audited paths nest the same guard
// inside inAuditedTx instead. The invariant holds either way — it is a property
// of the database, not of who happens to be writing.
func (s *Store) inGuardedTx(ctx context.Context, mutate func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin guarded transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := withMaintainerGuard(ctx, tx, mutate); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit guarded transaction: %w", err)
	}
	return nil
}

// scanUpdatedUser runs an "UPDATE ... RETURNING userColumns" statement on tx and
// returns the refreshed user, translating pgx.ErrNoRows into ErrUserNotFound and
// a foreign-key violation — only subject_uid has one — into ErrSubjectNotFound.
// It is shared by the guarded profile-update and disable writes, audited or not,
// which differ only in their SQL and arguments.
func scanUpdatedUser(ctx context.Context, tx pgx.Tx, query string, args ...any) (User, error) {
	user, err := scanUser(tx.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		if isForeignKeyViolation(err) {
			return User{}, ErrSubjectNotFound
		}
		return User{}, err
	}
	return user, nil
}

// updateUserReturningGuarded runs an "UPDATE ... RETURNING userColumns"
// statement inside a guarded transaction and returns the refreshed user. A
// change that would strand the instance without an enabled maintainer fails with
// ErrLastMaintainer and is rolled back.
func (s *Store) updateUserReturningGuarded(ctx context.Context, query string, args ...any) (User, error) {
	var user User
	err := s.inGuardedTx(ctx, func(tx pgx.Tx) error {
		u, scanErr := scanUpdatedUser(ctx, tx, query, args...)
		if scanErr != nil {
			return scanErr
		}
		user = u
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}
