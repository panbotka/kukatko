package searchhistory

// The project scope of the search history.
//
// search_history is a root table keyed by (user_uid, project_id, query): one
// user's history is a separate ring of MaxEntries in every project they search
// in. Every Store method takes the project uid as its first argument after the
// context and never defaults it — an empty uid is ErrNoProject, a programming
// error rather than "the whole instance". Recording in one project never prunes,
// lists or clears the history of another, and Record always writes project_id
// explicitly: the column's transitional default (migration 0102) is never relied
// on.

// userProjectClause confines a query over search_history to one user ($1) inside
// one project ($2). Every statement binds the user as $1 and the project as $2,
// so the clause is shared verbatim.
const userProjectClause = "search_history.user_uid = $1 AND search_history.project_id = $2"

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}
