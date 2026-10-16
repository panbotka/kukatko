package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/project"
)

// projectUserNameSQL reads the account $1 as the project $2 shows it: its
// display name, else its username — or no name and the former flag when the
// account is no member of the project any more (project.ShownNameSQL and
// project.FormerMemberSQL, the one rule every reader of a project's people
// applies).
var projectUserNameSQL = `SELECT ` + project.ShownNameSQL("$2", "u") + `,
	` + project.FormerMemberSQL("$2", "u") + `
FROM users u WHERE u.uid = $1`

// ProjectUserName returns how the project projectUID names the account uid:
// its display name (else its username) while it belongs to the project — a
// membership whose identity the project may see, whatever its instance role —
// and otherwise no name and former true, so a project never reads the live name
// of somebody who left it (or never joined it). It returns ErrUserNotFound when
// no account has uid.
func (s *Store) ProjectUserName(ctx context.Context, projectUID, uid string) (name string, former bool, err error) {
	err = s.pool.QueryRow(ctx, projectUserNameSQL, uid, projectUID).Scan(&name, &former)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrUserNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("auth: reading the name of %s in project %s: %w", uid, projectUID, err)
	}
	return name, former, nil
}
