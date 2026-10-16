package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/project"
)

// AwaitingMember is one member of a project whose account still has no
// password — set up for somebody who has not followed their link yet.
type AwaitingMember struct {
	UserUID     string       `json:"user_uid"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Email       string       `json:"email"`
	Role        project.Role `json:"role"`
	// LinkExpiresAt is when the account's outstanding set-password link stops
	// working, or nil when no usable link is left (it expired, or none was
	// ever issued) — then only a fresh one helps.
	LinkExpiresAt *time.Time `json:"link_expires_at"`
}

// inTx runs fn in a transaction on the pool, committing only when it succeeds.
// It is for a flow that writes its own audit entries as it goes (ProjectSetup);
// a single mutation with one entry uses inAuditedTx.
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit transaction: %w", err)
	}
	return nil
}

// lockAccountsByEmailQuery reads (and locks) the accounts whose address is $1,
// ignoring case as an exact-match member add does; two rows are enough to know
// the address is ambiguous.
const lockAccountsByEmailQuery = `SELECT ` + userColumns + ` FROM users
	WHERE lower(email) = lower($1) ORDER BY uid LIMIT 2 FOR UPDATE`

// lockAccountsByEmail returns, locked on tx, at most two accounts whose e-mail
// address is email (case-insensitive).
func lockAccountsByEmail(ctx context.Context, tx pgx.Tx, email string) ([]User, error) {
	rows, err := tx.Query(ctx, lockAccountsByEmailQuery, email)
	if err != nil {
		return nil, fmt.Errorf("auth: finding accounts by e-mail: %w", err)
	}
	defer rows.Close()
	var found []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating accounts by e-mail: %w", err)
	}
	return found, nil
}

// usernameTaken reports whether an account already signs in as username (the
// normalized form, as stored).
func usernameTaken(ctx context.Context, tx pgx.Tx, username string) (bool, error) {
	var taken bool
	const q = `SELECT EXISTS (SELECT 1 FROM users WHERE username = $1)`
	if err := tx.QueryRow(ctx, q, username).Scan(&taken); err != nil {
		return false, fmt.Errorf("auth: checking username %q: %w", username, err)
	}
	return taken, nil
}

// readProjectTx reads the uid, slug and name of the project uid on tx, or
// project.ErrNotFound.
func readProjectTx(ctx context.Context, tx pgx.Tx, uid string) (project.Project, error) {
	var p project.Project
	const q = `SELECT uid, slug, name FROM projects WHERE uid = $1`
	err := tx.QueryRow(ctx, q, uid).Scan(&p.UID, &p.Slug, &p.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return project.Project{}, project.ErrNotFound
	}
	if err != nil {
		return project.Project{}, fmt.Errorf("auth: reading project %s: %w", uid, err)
	}
	return p, nil
}

// checkAwaitingMember returns nil when account is a member of the project
// projectUID, enabled, and still has no password — the one case a
// set-password link is re-sent for — and otherwise project.ErrNotMember,
// ErrUserDisabled or ErrPasswordAlreadySet.
func checkAwaitingMember(ctx context.Context, tx pgx.Tx, projectUID string, account User) error {
	var member bool
	const q = `SELECT EXISTS (SELECT 1 FROM project_members WHERE project_uid = $1 AND user_uid = $2)`
	if err := tx.QueryRow(ctx, q, projectUID, account.UID).Scan(&member); err != nil {
		return fmt.Errorf("auth: checking membership of %s: %w", account.UID, err)
	}
	switch {
	case !member:
		return fmt.Errorf("%w: %s", project.ErrNotMember, account.UID)
	case account.Disabled:
		return ErrUserDisabled
	case account.HasPassword():
		return ErrPasswordAlreadySet
	}
	return nil
}

// awaitingPasswordQuery lists the enabled members of the project $1 whose
// accounts have no password, with the expiry of the account's outstanding
// link as of $2 (NULL when none is usable).
//
//nolint:gosec // G101: SQL over column names, not a credential; "password_hash" is a column.
const awaitingPasswordQuery = `SELECT u.uid, u.username, u.display_name, u.email, m.role,
	(SELECT max(t.expires_at) FROM password_reset_tokens t
	 WHERE t.user_uid = u.uid AND t.used_at IS NULL AND t.expires_at > $2)
FROM project_members m JOIN users u ON u.uid = m.user_uid
WHERE m.project_uid = $1 AND u.password_hash = '' AND NOT u.disabled
ORDER BY u.username`

// AwaitingPassword returns the enabled members of the project projectUID whose
// accounts still have no password, by username, each with the expiry (as of
// now) of its outstanding set-password link. An unknown project lists nobody.
func (s *Store) AwaitingPassword(ctx context.Context, projectUID string, now time.Time) ([]AwaitingMember, error) {
	rows, err := s.pool.Query(ctx, awaitingPasswordQuery, projectUID, now)
	if err != nil {
		return nil, fmt.Errorf("auth: listing members awaiting a password: %w", err)
	}
	defer rows.Close()
	out := []AwaitingMember{}
	for rows.Next() {
		var m AwaitingMember
		if err := rows.Scan(&m.UserUID, &m.Username, &m.DisplayName, &m.Email, &m.Role, &m.LinkExpiresAt); err != nil {
			return nil, fmt.Errorf("auth: scanning a member awaiting a password: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating members awaiting a password: %w", err)
	}
	return out, nil
}
