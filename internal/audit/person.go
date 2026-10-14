package audit

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoProjectPerson is returned by Store.ProjectPerson for a value that names
// none of the project's people — whether it names an account outside the
// project or no account at all, deliberately one error for both.
var ErrNoProjectPerson = errors.New("audit: no such person in the project")

// projectPersonSQL finds the account $2 names — by uid or by username — among
// the people of the project $1: its current members and every account that has
// acted in it (a former member, a system administrator who worked there).
// A uid match wins over a username match, as on the instance listing.
const projectPersonSQL = `SELECT u.uid FROM users u
WHERE (u.uid = $2 OR u.username = $2)
	AND (EXISTS (SELECT 1 FROM project_members m WHERE m.project_uid = $1 AND m.user_uid = u.uid)
		OR EXISTS (SELECT 1 FROM audit_log a WHERE a.project_id = $1 AND a.actor_uid = u.uid))
ORDER BY u.uid = $2 DESC
LIMIT 1`

// ProjectPerson resolves ref — an account uid or a username — to an account
// uid, but only among the people of the project projectUID: its current
// members and the accounts that appear as actors in its trail. It is the user
// filter of a project admin's audit listing, which must not tell an account
// that exists elsewhere on the instance from one that does not exist at all:
// both are ErrNoProjectPerson. Any other error is a failed lookup.
func (s *Store) ProjectPerson(ctx context.Context, projectUID, ref string) (string, error) {
	var uid string
	err := s.pool.QueryRow(ctx, projectPersonSQL, projectUID, ref).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoProjectPerson
	}
	if err != nil {
		return "", fmt.Errorf("audit: resolving %q in project %s: %w", ref, projectUID, err)
	}
	return uid, nil
}
