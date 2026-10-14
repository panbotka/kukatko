package auth

import (
	"context"
	"fmt"
	"net/http"
)

// DirectoryEntry is one account as everybody in the library may see it: a name
// and the uid to address it by, and nothing else.
//
// It is a type of its own rather than a trimmed User on purpose. A User carries
// the e-mail address, the role, the approval state and the password hash, and a
// handler that had to remember to strip those before answering would eventually
// forget. Here the columns never leave the database in the first place.
type DirectoryEntry struct {
	UID string `json:"uid"`
	// Name is the display name, falling back to the username.
	Name string `json:"name"`
}

// directoryQuery lists the accounts a signed-in person of the project $1 may
// name: its members. Disabled accounts and memberships still waiting for a
// project admin are left out: neither can be asked a question, and both would
// be a stranger in a picker. So is a member whose identity is withheld (an
// exact-match add that has not entered the project yet): the add must not
// tell the project who the account is. An account of another project is never
// listed — a tenant must not learn who else uses the instance.
const directoryQuery = `
SELECT u.uid, COALESCE(NULLIF(u.display_name, ''), u.username)
FROM project_members m JOIN users u ON u.uid = m.user_uid
WHERE m.project_uid = $1 AND m.approved_at IS NOT NULL AND NOT m.identity_withheld AND NOT u.disabled
ORDER BY COALESCE(NULLIF(u.display_name, ''), u.username), u.uid`

// Directory returns the people of the project projectUID — its approved members
// that are not disabled, the accounts a signed-in person there may name —
// ordered by the name they are shown under. An empty projectUID is refused
// with ErrNoProject: there is no default project. The slice is empty, not nil,
// when there are none.
func (s *Store) Directory(ctx context.Context, projectUID string) ([]DirectoryEntry, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	rows, err := s.pool.Query(ctx, directoryQuery, projectUID)
	if err != nil {
		return nil, fmt.Errorf("auth: querying the directory: %w", err)
	}
	defer rows.Close()

	out := make([]DirectoryEntry, 0)
	for rows.Next() {
		var entry DirectoryEntry
		if err := rows.Scan(&entry.UID, &entry.Name); err != nil {
			return nil, fmt.Errorf("auth: scanning directory entry: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating the directory: %w", err)
	}
	return out, nil
}

// Directory returns the accounts a signed-in person of the project projectUID
// may name — its approved members — ordered by name.
func (s *Service) Directory(ctx context.Context, projectUID string) ([]DirectoryEntry, error) {
	return s.store.Directory(ctx, projectUID)
}

// handleDirectory answers with the people of the request's project: uid and
// name, for every approved member whose account is active.
//
// It is open to every signed-in role, which is a deliberate widening of what
// used to be admin-only. The reason is that naming a person is now something an
// ordinary user does — putting somebody on a task is how a question reaches the
// one relative who would know the answer — and it exposes nothing a thread does
// not already: every comment in the library is shown with its author's name and
// picture. What stays admin-only is everything that makes the list an
// administrative record: the e-mail addresses, the roles, who is disabled and
// who is still waiting to be let in.
func (a *API) handleDirectory(w http.ResponseWriter, r *http.Request) {
	people, err := a.svc.Directory(r.Context(), ProjectUIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing people failed")
		return
	}
	writeJSON(w, http.StatusOK, directoryResponse{Users: people})
}

// directoryResponse is the JSON body of the directory endpoint. Users is always
// an array, never null.
type directoryResponse struct {
	Users []DirectoryEntry `json:"users"`
}
