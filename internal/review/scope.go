package review

// The project scope of the review game.
//
// Every question the game serves and every answer it applies belongs to one
// project. The searches the queue composes (the recognition scan, the outlier
// ranking, the photo hydration, the breathers) take the project themselves; two
// stores the game leans on still list the whole instance — the labels and the
// estimated locations — and their output is filtered here. An answer is proven
// before it writes anything: its photo(s), subject and label must belong to the
// caller's project, or the answer is ErrQuestionNotFound — the same error for a
// uid of another project as for one that exists nowhere, so the two cannot be
// told apart, and nothing is written for either.

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ScopeStore answers the game's "which of these belong to the project?" reads
// over the shared pool. It owns no connection; it borrows the pool supplied at
// construction.
type ScopeStore struct {
	pool *pgxpool.Pool
}

// NewScopeStore returns a ScopeStore backed by pool. The pool stays owned by the
// caller.
func NewScopeStore(pool *pgxpool.Pool) *ScopeStore {
	return &ScopeStore{pool: pool}
}

// The three membership reads, one per root table the game's questions name.
// Each returns the subset of $2 that names a row of project $1.
const (
	photosInProjectSQL   = "SELECT uid FROM photos WHERE project_id = $1 AND uid = ANY($2)"
	subjectsInProjectSQL = "SELECT uid FROM subjects WHERE project_id = $1 AND uid = ANY($2)"
	labelsInProjectSQL   = "SELECT uid FROM labels WHERE project_id = $1 AND uid = ANY($2)"
)

// PhotosInProject returns the subset of uids naming photos of the project.
func (s *ScopeStore) PhotosInProject(ctx context.Context, projectUID string, uids []string) (map[string]bool, error) {
	return s.members(ctx, photosInProjectSQL, "photos", projectUID, uids)
}

// SubjectsInProject returns the subset of uids naming subjects of the project.
func (s *ScopeStore) SubjectsInProject(
	ctx context.Context, projectUID string, uids []string,
) (map[string]bool, error) {
	return s.members(ctx, subjectsInProjectSQL, "subjects", projectUID, uids)
}

// LabelsInProject returns the subset of uids naming labels of the project.
func (s *ScopeStore) LabelsInProject(ctx context.Context, projectUID string, uids []string) (map[string]bool, error) {
	return s.members(ctx, labelsInProjectSQL, "labels", projectUID, uids)
}

// members runs one membership read and collects the uids it returns. An empty
// projectUID is ErrNoProject; an empty uids list answers an empty set without a
// query.
func (s *ScopeStore) members(
	ctx context.Context, query, what, projectUID string, uids []string,
) (map[string]bool, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	out := make(map[string]bool, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, query, projectUID, uids)
	if err != nil {
		return nil, fmt.Errorf("review: reading the project's %s: %w", what, err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("review: scanning the project's %s: %w", what, err)
		}
		out[uid] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review: iterating the project's %s: %w", what, err)
	}
	return out, nil
}

// proveInProject returns ErrQuestionNotFound unless every photo, subject and
// label the question names belongs to the project. It runs before an answer
// writes anything, the skip memory included.
func (s *Service) proveInProject(ctx context.Context, projectUID string, ref questionRef) error {
	photoUIDs := []string{ref.PhotoUID}
	if ref.OtherUID != "" {
		photoUIDs = append(photoUIDs, ref.OtherUID)
	}
	checks := []struct {
		uids []string
		read func(context.Context, string, []string) (map[string]bool, error)
	}{
		{photoUIDs, s.scope.PhotosInProject},
		{nonEmpty(ref.SubjectUID), s.scope.SubjectsInProject},
		{nonEmpty(ref.LabelUID), s.scope.LabelsInProject},
	}
	for _, check := range checks {
		if len(check.uids) == 0 {
			continue
		}
		found, err := check.read(ctx, projectUID, check.uids)
		if err != nil {
			return fmt.Errorf("review: proving the question in its project: %w", err)
		}
		if slices.ContainsFunc(check.uids, func(uid string) bool { return !found[uid] }) {
			return ErrQuestionNotFound
		}
	}
	return nil
}

// nonEmpty returns uid as a one-element list, or nil when it is empty.
func nonEmpty(uid string) []string {
	if uid == "" {
		return nil
	}
	return []string{uid}
}
