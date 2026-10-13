package family

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The project scope of the genealogy.
//
// subject_families carries project_id; subject_family_children carries none and
// belongs to the project of its family. Every Store method takes the project uid
// as its first argument after the context and never defaults it: an empty uid is
// ErrNoProject. A subject or family of another project is answered exactly like a
// missing one (ErrSubjectNotFound, ErrFamilyNotFound, ErrRelationNotFound), so
// the HTTP layer gives the same 404 for both.
//
// The scope is enforced where a request enters the graph. Every write proves
// each subject it names inside the project before it touches a row, and a family
// it creates takes the project of the subject it is created for (insertFamilySQL)
// — so a family only ever ties people of one project together, and every walk
// that starts from a subject of the project stays inside it by construction. The
// read statements still filter on the project as well: the invariant is what
// makes the answer right, the filter is what keeps a broken row from leaking.

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every exported Store method calls it first.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// subjectInProjectSQL reports whether a subject row exists in the project.
const subjectInProjectSQL = "SELECT 1 FROM subjects WHERE uid = $1 AND project_id = $2"

// requireSubjects returns ErrSubjectNotFound unless every named subject exists in
// the project. A subject of another project is not found, word for word as a
// subject nobody ever created. The foreign keys would refuse a missing subject
// anyway, but a sentinel the caller can map to a 404 is worth the extra round
// trip: an FK violation names a constraint, not a person — and it would not
// refuse a subject that merely lives in another project at all.
func requireSubjects(ctx context.Context, q querier, projectUID string, uids ...string) error {
	for _, uid := range uids {
		var one int
		err := q.QueryRow(ctx, subjectInProjectSQL, uid, projectUID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrSubjectNotFound, uid)
		}
		if err != nil {
			return fmt.Errorf("family: checking subject %s: %w", uid, err)
		}
	}
	return nil
}
