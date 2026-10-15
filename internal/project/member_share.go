package project

import (
	"context"
	"fmt"
)

// sharesProjectSQL holds when the accounts $2 and $3 are both named members
// (ApprovedMemberSQL) of one active project — the project $1, or any project
// when $1 is empty. The casts pin each parameter's type once, so the same
// placeholder can sit on both sides of the OR.
var sharesProjectSQL = `SELECT EXISTS (SELECT 1 FROM projects p
WHERE p.active AND ($1::text = '' OR p.uid = $1::text)
	AND ` + ApprovedMemberSQL("p.uid", "$2::text") + `
	AND ` + ApprovedMemberSQL("p.uid", "$3::text") + `)`

// SharesProject reports whether the accounts viewerUID and accountUID are both
// approved members of one active project whose identity that project may see
// (ApprovedMemberSQL: a pending member, an exact-match add the account has not
// entered yet and an account outside the project all fail it). An empty
// projectUID asks about any project, a non-empty one about that project only.
//
// It is the rule for showing one account's profile to another: a person who
// shares a library with the account sees its face, nobody else learns even
// that it has one. Asking with the same uid twice answers whether that account
// is such a member of projectUID.
func (s *Store) SharesProject(ctx context.Context, projectUID, viewerUID, accountUID string) (bool, error) {
	var shares bool
	if err := s.pool.QueryRow(ctx, sharesProjectSQL, projectUID, viewerUID, accountUID).Scan(&shares); err != nil {
		return false, fmt.Errorf("project: checking whether %s and %s share a project: %w", viewerUID, accountUID, err)
	}
	return shares, nil
}
