package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RowQuerier is the one method RequireApprovedMembers needs. *pgxpool.Pool and
// pgx.Tx both satisfy it, so the check runs inside a caller's transaction as
// readily as on the pool.
type RowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ApprovedMemberSQL returns the SQL predicate that holds when the account
// userExpr is an approved member of the project projectExpr whose identity the
// project may see — not an exact-match add the account has not entered yet
// (Member.IdentityWithheld). Both arguments are
// SQL expressions (a placeholder like "$2" or a column like "u.uid"); they are
// pasted verbatim, so they must never carry user input.
//
// It is the one definition of "may be named in this project": a write that
// stores somebody else's account uid where the project will display it (a task
// participant, a mention, an import's uploader) admits only such an account.
// Pending members and accounts outside the project — the system administrator
// included, who enters every project but belongs to none by that — are not
// admitted, so the project never learns the name of somebody outside it; nor is
// a member whose identity is withheld, whose uid only the project's admin has
// seen and whose name a mention would otherwise resolve.
func ApprovedMemberSQL(projectExpr, userExpr string) string {
	return "EXISTS (SELECT 1 FROM project_members apm WHERE apm.project_uid = " + projectExpr +
		" AND apm.user_uid = " + userExpr + " AND apm.approved_at IS NOT NULL AND NOT apm.identity_withheld)"
}

// approvedMembersFrom is the head of the statement RequireApprovedMembers
// runs: it unnests the distinct uids of $2, and the ApprovedMemberSQL predicate
// over the project $1 completes it into a count of the approved members among
// them.
const approvedMembersFrom = `
SELECT count(*) FROM (SELECT DISTINCT unnest($2::text[]) AS uid) asked
WHERE `

// RequireApprovedMembers returns ErrNotMember unless every uid in userUIDs
// names an approved member of projectUID (see ApprovedMemberSQL). A uid with no
// account at all is ErrNotMember too, so a caller that answers both the same
// way cannot be used to learn which accounts exist. Repeated uids are checked
// once; no uids at all pass without a query.
func RequireApprovedMembers(ctx context.Context, q RowQuerier, projectUID string, userUIDs ...string) error {
	if len(userUIDs) == 0 {
		return nil
	}
	distinct := make(map[string]struct{}, len(userUIDs))
	for _, uid := range userUIDs {
		distinct[uid] = struct{}{}
	}
	var found int
	query := approvedMembersFrom + ApprovedMemberSQL("$1", "asked.uid")
	if err := q.QueryRow(ctx, query, projectUID, userUIDs).Scan(&found); err != nil {
		return fmt.Errorf("project: checking members of %s: %w", projectUID, err)
	}
	if found != len(distinct) {
		return ErrNotMember
	}
	return nil
}

// IsApprovedMember reports whether userUID is an approved member of projectUID
// whose identity the project may see (ApprovedMemberSQL) — the account a
// project may credit by name. A uid with no account at all is no member.
func (s *Store) IsApprovedMember(ctx context.Context, projectUID, userUID string) (bool, error) {
	err := RequireApprovedMembers(ctx, s.pool, projectUID, userUID)
	if errors.Is(err, ErrNotMember) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
