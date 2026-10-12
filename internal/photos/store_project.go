package photos

// The project scope of the photo catalogue.
//
// Every read that touches the photos root table takes the project uid as its
// first argument after the context and never defaults it: an empty uid is
// ErrNoProject, a programming error rather than "the whole instance". The
// predicate it compiles to is projectClause, and in the shared listing path
// (whereClauses) it is always the first clause and always bound as $1, so every
// filter of the search query language is evaluated inside the project.

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every scoped Store method calls it before touching the database,
// so a caller that forgot to pass the project fails loudly instead of reading
// across tenants.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// projectClause returns the predicate confining a query over photos to one
// project, given the placeholder the project uid is bound to. The column is
// qualified so the clause stays unambiguous inside a query that joins another
// table carrying project_id.
func projectClause(placeholder string) string {
	return "photos.project_id = " + placeholder
}
