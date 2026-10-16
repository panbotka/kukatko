package audit

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// ErrNoProjectPerson is returned by Store.ProjectPerson for a value that names
// none of the project's people — whether it names an account outside the
// project or no account at all, deliberately one error for both.
var ErrNoProjectPerson = errors.New("audit: no such person in the project")

// projectPersonSQL finds the account $2 names — by uid or by username — among
// the people of the project $1: its current members and every account that has
// acted in it (a former member, a system administrator who worked there). A
// membership whose identity is withheld does not make its account one of the
// project's people.
// A uid match wins over a username match, as on the instance listing.
const projectPersonSQL = `SELECT u.uid FROM users u
WHERE (u.uid = $2 OR u.username = $2)
	AND (EXISTS (SELECT 1 FROM project_members m
			WHERE m.project_uid = $1 AND m.user_uid = u.uid AND NOT m.identity_withheld)
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

// actorNamesSQL reads the name a person is shown by — the display name, or the
// username when there is none — for each account of $1, the same rule the
// project directory applies.
const actorNamesSQL = `SELECT u.uid, COALESCE(NULLIF(u.display_name, ''), u.username)
FROM users u
WHERE u.uid = ANY($1)`

// ActorNames returns the display name of each account in uids that still
// exists, keyed by uid; an account since deleted is simply absent. It names the
// actors of a page the caller has already been allowed to read, so it adds no
// account to what that reader knows: a project admin learns the names of the
// people who acted in the project — former and still-waiting members too —
// and of nobody else.
func (s *Store) ActorNames(ctx context.Context, uids []string) (map[string]string, error) {
	names := make(map[string]string, len(uids))
	if len(uids) == 0 {
		return names, nil
	}
	rows, err := s.pool.Query(ctx, actorNamesSQL, uids)
	if err != nil {
		return nil, fmt.Errorf("audit: reading actor names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid, name string
		if err := rows.Scan(&uid, &name); err != nil {
			return nil, fmt.Errorf("audit: scanning an actor name: %w", err)
		}
		names[uid] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterating actor names: %w", err)
	}
	return names, nil
}

// projectActorNamesSQL is actorNamesSQL for a listing narrowed to the project
// $2: it adds whether each account is a former member there — no membership of
// the project whose identity the project may see, a system administrator
// included (the project must not learn that the role exists). The predicate is
// project.FormerMemberSQL written out: this package sits below it and does not
// import it, so the two must be kept in step by hand.
const projectActorNamesSQL = `SELECT u.uid, COALESCE(NULLIF(u.display_name, ''), u.username),
	NOT EXISTS (SELECT 1 FROM project_members fmm
		WHERE fmm.user_uid = u.uid AND fmm.project_uid = $2 AND NOT fmm.identity_withheld)
FROM users u
WHERE u.uid = ANY($1)`

// ProjectActorNames is ActorNames for a listing of the project projectUID. A
// project shows a person by name only while they belong to it: an account that
// has left (or never joined — an upload credited before the upload link
// checked membership) is a former member, reported in former and absent from
// names, so the project never reads a live name the account may have changed
// elsewhere since. The client says "former member" for it, the way comments
// and tasks do. An account since deleted is in neither.
func (s *Store) ProjectActorNames(
	ctx context.Context, projectUID string, uids []string,
) (names map[string]string, former []string, err error) {
	names = make(map[string]string, len(uids))
	former = make([]string, 0)
	if len(uids) == 0 {
		return names, former, nil
	}
	rows, err := s.pool.Query(ctx, projectActorNamesSQL, uids, projectUID)
	if err != nil {
		return nil, nil, fmt.Errorf("audit: reading project actor names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			uid, name string
			isFormer  bool
		)
		if err := rows.Scan(&uid, &name, &isFormer); err != nil {
			return nil, nil, fmt.Errorf("audit: scanning a project actor name: %w", err)
		}
		if isFormer {
			former = append(former, uid)
			continue
		}
		names[uid] = name
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("audit: iterating project actor names: %w", err)
	}
	slices.Sort(former)
	return names, former, nil
}
