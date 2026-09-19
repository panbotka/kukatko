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

// directoryQuery lists the accounts a signed-in person may name. Disabled ones
// and ones still waiting for an administrator are left out: neither can be
// asked a question, and both would be a stranger in a picker.
const directoryQuery = `
SELECT uid, COALESCE(NULLIF(display_name, ''), username)
FROM users
WHERE NOT disabled AND approved_at IS NOT NULL
ORDER BY COALESCE(NULLIF(display_name, ''), username), uid`

// Directory returns the people of the library — the accounts a signed-in person
// may name — ordered by the name they are shown under. The slice is empty, not
// nil, when there are none.
func (s *Store) Directory(ctx context.Context) ([]DirectoryEntry, error) {
	rows, err := s.pool.Query(ctx, directoryQuery)
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

// Directory returns the accounts a signed-in person may name, ordered by name.
func (s *Service) Directory(ctx context.Context) ([]DirectoryEntry, error) {
	return s.store.Directory(ctx)
}

// handleDirectory answers with the people of the library: uid and name, for
// every active account.
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
	people, err := a.svc.Directory(r.Context())
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
