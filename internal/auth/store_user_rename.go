package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// renameUserQuery gives one account a new username and returns the refreshed
// row. Only the name and updated_at move: every row that belongs to the account —
// its sessions, API tokens, passkeys, linked subject, comments, uploads and its
// audit history — references users.uid, never the username, so all of it stays
// attached without being touched.
const renameUserQuery = `UPDATE users SET username = $2, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// usernameHeldElsewhereQuery reports whether any other account holds username,
// compared case-insensitively. Every write path stores the name lower-cased, so
// the unique index alone would do for those; the lower() is for an account that
// predates that normalization, whose name must not be duplicated by a rename
// differing from it only in case.
const usernameHeldElsewhereQuery = `SELECT EXISTS (
	SELECT 1 FROM users WHERE lower(username) = lower($2) AND uid <> $1)`

// RenameUserAudited gives the account identified by uid the new name username
// (already normalized and validated by the caller) and writes entry in the same
// transaction, returning the refreshed user. entry's TargetUID defaults to uid,
// and its details are stamped with old_username and new_username as read under
// the row lock. alongside, when non-nil, runs on the same transaction with the
// renamed account, after the update and before the audit entry — it is how the
// "your username is now X" mail is scheduled if and only if the rename commits.
//
// Renaming an account to the name it already has is not a change: the stored
// user is returned, nothing is updated, alongside does not run and no audit entry
// is written.
//
// It returns ErrUserNotFound when no such account exists and ErrUsernameTaken
// when another account already holds the name (in any letter case).
func (s *Store) RenameUserAudited(
	ctx context.Context, uid, username string, entry audit.Entry,
	alongside func(ctx context.Context, tx pgx.Tx, user User) error,
) (User, error) {
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	// inAuditedTx takes the entry by value, so a field set inside the closure
	// would be lost; the details map, though, is shared with that copy, which is
	// what lets the old name — known only once the row is locked — reach the
	// entry that is written.
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	details := entry.Details
	var user User
	err := s.inAuditedTx(ctx, entry, func(tx pgx.Tx) error {
		current, err := lockUser(ctx, tx, uid)
		if err != nil {
			return err
		}
		if current.Username == username {
			user = current
			// Nothing to change, so nothing to record: inAuditedTx rolls the
			// transaction back and reports success.
			return errNoAuditableChange
		}
		renamed, err := renameLocked(ctx, tx, uid, username)
		if err != nil {
			return err
		}
		user = renamed
		details["old_username"] = current.Username
		details["new_username"] = renamed.Username
		if alongside == nil {
			return nil
		}
		return alongside(ctx, tx, renamed)
	})
	if err != nil {
		return User{}, err
	}
	return user, nil
}

// renameLocked writes the new username of the account uid, whose row the caller
// has already locked on tx, refusing with ErrUsernameTaken a name another
// account holds. The explicit check catches a case-only clash the unique index
// cannot see; the unique violation catches a concurrent rename or creation that
// took the name after the check.
func renameLocked(ctx context.Context, tx pgx.Tx, uid, username string) (User, error) {
	var held bool
	if err := tx.QueryRow(ctx, usernameHeldElsewhereQuery, uid, username).Scan(&held); err != nil {
		return User{}, fmt.Errorf("auth: checking the username is free: %w", err)
	}
	if held {
		return User{}, ErrUsernameTaken
	}
	renamed, err := scanUpdatedUser(ctx, tx, renameUserQuery, uid, username)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	return renamed, nil
}
