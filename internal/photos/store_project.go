package photos

import (
	"context"
	"fmt"
	"slices"
)

// The project scope of the photo catalogue.
//
// Every read that touches the photos root table takes the project uid as its
// first argument after the context and never defaults it: an empty uid is
// ErrNoProject, a programming error rather than "the whole instance". The
// predicate it compiles to is projectClause, and in the shared listing path
// (whereClauses) it is always the first clause and always bound as $1, so every
// filter of the search query language is evaluated inside the project.
//
// Writes follow the same rule: a mutation takes the project uid after the
// context and touches a row only if it belongs to that project. A
// single-statement write carries projectClause in its WHERE (0 rows affected =
// ErrPhotoNotFound); a transactional one — and every write to a child table such
// as photo_files or photo_edits, which carries no project of its own — first
// proves the photos in the same transaction (requirePhotosInProject). A foreign
// uid therefore changes nothing and is answered exactly like a missing one.
//
// Background jobs are the one deliberate exception: they have no request and so
// no project, load through the explicitly named GetForJob/FilesForJob and learn
// the project from the row they loaded.

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

// requirePhotosInProject returns ErrPhotoNotFound unless every uid in uids names
// a photo of the project, run on q — the pool, or the transaction of the write
// it guards, so the proof and the write see the same rows. Duplicates in uids
// count once. A photo's project never changes once it is catalogued, so no lock
// is taken.
func requirePhotosInProject(ctx context.Context, q rowQuerier, projectUID string, uids []string) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	distinct := slices.Clone(uids)
	slices.Sort(distinct)
	distinct = slices.Compact(distinct)
	var found int
	err := q.QueryRow(ctx,
		"SELECT count(*) FROM photos WHERE "+projectClause("$1")+" AND uid = ANY($2)",
		projectUID, distinct).Scan(&found)
	if err != nil {
		return fmt.Errorf("photos: checking the photos' project: %w", err)
	}
	if found != len(distinct) {
		return ErrPhotoNotFound
	}
	return nil
}
