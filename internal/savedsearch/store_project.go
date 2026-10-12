package savedsearch

// The project scope of saved searches.
//
// saved_searches is a root table: every row carries project_id and belongs to one
// owner inside one project. Every Store method takes the project uid as its first
// argument after the context and never defaults it — an empty uid is
// ErrNoProject, a programming error rather than "the whole instance" — and every
// statement also names the owner. A saved search of another owner, or the same
// owner's saved search in another project, is answered exactly like one that does
// not exist (ErrNotFound, an empty list), so the HTTP layer gives the very same
// 404. Create always writes project_id explicitly; the column's transitional
// default (migration 0102) is never relied on.

// ownerProjectClause confines a query over saved_searches to one owner ($2)
// inside one project ($1). Every statement binds the project as $1 and the owner
// as $2, so the clause is shared verbatim.
const ownerProjectClause = "saved_searches.project_id = $1 AND saved_searches.owner_uid = $2"

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}
