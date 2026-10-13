package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// AddOutcome is what AddByIdentifier really did. The caller never tells the
// person who asked — the answer to them is the same whatever happened, so the
// add cannot be used to learn which accounts exist — but the audit entry
// records it.
type AddOutcome string

const (
	// OutcomeAdded means the account was found and is now an approved member.
	OutcomeAdded AddOutcome = "added"
	// OutcomeAlreadyMember means the account was found and already was a
	// member; its membership was left exactly as it was.
	OutcomeAlreadyMember AddOutcome = "already_member"
	// OutcomeNoSuchAccount means no account has that username or e-mail address.
	OutcomeNoSuchAccount AddOutcome = "no_such_account"
	// OutcomeAmbiguous means no account has that username and several share that
	// e-mail address, so none was added: adding the wrong person is worse than
	// adding nobody.
	OutcomeAmbiguous AddOutcome = "ambiguous"
)

// ErrEmptyIdentifier is returned by AddByIdentifier for an identifier that is
// empty once trimmed.
var ErrEmptyIdentifier = errors.New("an account identifier is required")

// usernameMatchSQL finds the account whose username is exactly $1. Usernames are
// stored normalised (trimmed, lower case), as sign-in normalises what is typed.
const usernameMatchSQL = `SELECT uid FROM users WHERE username = $1`

// emailMatchSQL finds the accounts whose e-mail address is $1, ignoring case.
// Two rows are enough to know the address is ambiguous.
const emailMatchSQL = `SELECT uid FROM users WHERE lower(email) = lower($1) ORDER BY uid LIMIT 2`

// AddByIdentifier makes the account named by identifier — its exact username or
// its e-mail address (case-insensitive) — an approved member of the project
// projectUID with role, and records ActionProjectMemberAdd with the real outcome
// (details "outcome", see AddOutcome) in the same transaction, whatever it is.
// Nothing is ever matched partially: a typo finds nobody. A username wins over
// an e-mail address, and an address several accounts share adds nobody.
//
// The outcome is returned for the caller's tests and logs, never for the
// person who asked. It returns ErrInvalidRole for a bad role,
// ErrEmptyIdentifier for a blank identifier and ErrNotFound for an unknown
// project.
func (s *Store) AddByIdentifier(
	ctx context.Context, meta audit.Meta, projectUID, identifier string, role Role,
) (AddOutcome, error) {
	if !role.Valid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return "", ErrEmptyIdentifier
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("project: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockProject(ctx, tx, projectUID); err != nil {
		return "", err
	}
	outcome, userUID, err := addIdentified(ctx, tx, projectUID, identifier, role)
	if err != nil {
		return "", fmt.Errorf("project: adding %q to %s: %w", identifier, projectUID, err)
	}
	details := map[string]any{
		"project_uid": projectUID, "role": string(role), "approved": true, "outcome": string(outcome),
	}
	if userUID == "" {
		// Nobody to point the entry at, so it keeps what was typed: the trail
		// should still answer "whom did the admin try to add".
		details["identifier"] = identifier
	}
	entry := projectEntry(meta, projectUID, audit.ActionProjectMemberAdd, MemberTargetType, userUID, details)
	if err := audit.Write(ctx, tx, entry); err != nil {
		return "", fmt.Errorf("project: writing audit entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("project: commit transaction: %w", err)
	}
	return outcome, nil
}

// addIdentified resolves identifier to one account inside tx and, when it finds
// exactly one, inserts its approved membership. It returns the outcome and the
// account's uid ("" when none was resolved).
func addIdentified(
	ctx context.Context, tx pgx.Tx, projectUID, identifier string, role Role,
) (AddOutcome, string, error) {
	uids, err := matchAccount(ctx, tx, identifier)
	if err != nil {
		return "", "", err
	}
	switch len(uids) {
	case 0:
		return OutcomeNoSuchAccount, "", nil
	case 1:
	default:
		return OutcomeAmbiguous, "", nil
	}
	userUID := uids[0]
	var inserted string
	err = tx.QueryRow(ctx, addMemberSQL, projectUID, userUID, string(role), true).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeAlreadyMember, userUID, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("inserting membership: %w", err)
	}
	return OutcomeAdded, userUID, nil
}

// matchAccount returns the uids of the accounts identifier names: the one whose
// username it is (normalised as sign-in normalises it), or else up to two whose
// e-mail address it is.
func matchAccount(ctx context.Context, tx pgx.Tx, identifier string) ([]string, error) {
	var uid string
	err := tx.QueryRow(ctx, usernameMatchSQL, strings.ToLower(identifier)).Scan(&uid)
	if err == nil {
		return []string{uid}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("matching a username: %w", err)
	}
	rows, err := tx.Query(ctx, emailMatchSQL, identifier)
	if err != nil {
		return nil, fmt.Errorf("matching an e-mail address: %w", err)
	}
	uids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("reading the e-mail matches: %w", err)
	}
	return uids, nil
}
