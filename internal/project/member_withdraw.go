package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// WithdrawOutcome is what WithdrawByIdentifier really did. Like AddOutcome, the
// person who asked is never told: only the actor-less instance entry
// (audit.ActionProjectMemberWithdrawResolve) records it.
type WithdrawOutcome string

const (
	// OutcomeWithdrawn means the account was found and its withheld membership
	// — an exact-match add it had not entered yet — was removed.
	OutcomeWithdrawn WithdrawOutcome = "withdrawn"
	// OutcomeNotWithheld means the account was found but holds no withheld
	// membership of the project: none at all, or one the project already sees,
	// which only the members list's own removal may take away.
	OutcomeNotWithheld WithdrawOutcome = "not_withheld"
	// OutcomeWithdrawNoSuchAccount means no account has that username or e-mail
	// address.
	OutcomeWithdrawNoSuchAccount WithdrawOutcome = "no_such_account"
	// OutcomeWithdrawAmbiguous means no account has that username and several
	// share that e-mail address, so nothing was removed.
	OutcomeWithdrawAmbiguous WithdrawOutcome = "ambiguous"
)

// withdrawWithheldMemberSQL removes the membership of $2 in $1 only while its
// identity is withheld; RETURNING yields no row otherwise, which leaves any
// visible membership untouched.
const withdrawWithheldMemberSQL = `
DELETE FROM project_members
WHERE project_uid = $1 AND user_uid = $2 AND identity_withheld
RETURNING user_uid`

// WithdrawByIdentifier undoes an exact-match add: it removes the membership of
// the account identifier names — matched exactly as AddByIdentifier matches —
// from the project projectUID, but only while that membership's identity is
// withheld (the account has not entered the project yet). The project cannot
// see such a membership (Members leaves it out), so this is the one way an
// admin who typed the wrong address takes the access back before a stranger
// uses it. A membership the project already sees is never touched here: it is
// removed by uid from the members list, where the admin can see whom.
//
// Like the add it is blind. Two audit entries are written in the same
// transaction, whatever happened: ActionProjectMemberWithdrawRequest under the
// project, identical for every outcome (project_uid only, no target), and the
// actor-less instance entry ActionProjectMemberWithdrawResolve with the real
// outcome. The outcome is returned for tests and logs, never for the person who
// asked. It returns ErrEmptyIdentifier for a blank identifier and ErrNotFound
// for an unknown project.
func (s *Store) WithdrawByIdentifier(
	ctx context.Context, meta audit.Meta, projectUID, identifier string,
) (WithdrawOutcome, error) {
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
	outcome, userUID, err := withdrawIdentified(ctx, tx, projectUID, identifier)
	if err != nil {
		return "", fmt.Errorf("project: withdrawing %q from %s: %w", identifier, projectUID, err)
	}
	for _, entry := range withdrawEntries(meta, projectUID, identifier, outcome, userUID) {
		if err := audit.Write(ctx, tx, entry); err != nil {
			return "", fmt.Errorf("project: writing audit entry: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("project: commit transaction: %w", err)
	}
	return outcome, nil
}

// withdrawIdentified resolves identifier to one account inside tx and, when it
// finds exactly one, removes its withheld membership of projectUID. It returns
// the outcome and the account's uid ("" when none was resolved).
func withdrawIdentified(
	ctx context.Context, tx pgx.Tx, projectUID, identifier string,
) (WithdrawOutcome, string, error) {
	uids, err := matchAccount(ctx, tx, identifier)
	if err != nil {
		return "", "", err
	}
	switch len(uids) {
	case 0:
		return OutcomeWithdrawNoSuchAccount, "", nil
	case 1:
	default:
		return OutcomeWithdrawAmbiguous, "", nil
	}
	userUID := uids[0]
	var removed string
	err = tx.QueryRow(ctx, withdrawWithheldMemberSQL, projectUID, userUID).Scan(&removed)
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeNotWithheld, userUID, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("removing the withheld membership: %w", err)
	}
	return OutcomeWithdrawn, userUID, nil
}

// withdrawEntries returns the two audit entries of one blind withdrawal, shaped
// like addEntries: the project's, which says only that a withdrawal was asked
// for, and the actor-less instance entry, which says what it found and keeps
// the typed identifier only when no single account was found.
func withdrawEntries(
	meta audit.Meta, projectUID, identifier string, outcome WithdrawOutcome, userUID string,
) []audit.Entry {
	request := projectEntry(meta, projectUID, audit.ActionProjectMemberWithdrawRequest, MemberTargetType, "",
		map[string]any{"project_uid": projectUID})
	details := map[string]any{
		"project_uid": projectUID, "requested_by": meta.ActorUID, "outcome": string(outcome),
	}
	if userUID == "" {
		details["identifier"] = identifier
	}
	resolve := meta.Entry(audit.ActionProjectMemberWithdrawResolve, MemberTargetType, userUID, details)
	resolve.ActorUID = ""
	resolve.ProjectUID = ""
	return []audit.Entry{request, resolve}
}
