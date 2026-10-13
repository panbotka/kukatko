package phototask

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
)

// The project scope of the work queue.
//
// photo_tasks is a root table and carries project_id; photo_task_photos,
// photo_task_participants and the thread (comments.task_uid) carry none and
// belong to the project of the task they hang off. Every Store method takes the
// project uid as its first argument after the context and never defaults it: an
// empty uid is ErrNoProject, a programming error rather than "the whole
// instance". A task of another project is answered exactly like a missing one
// (ErrNotFound, an empty listing, zero in the summary), so the HTTP layer gives
// the same 404.
//
// A task may only freeze photographs of its own project. The batch is proved in
// one statement before anything is written (requirePhotosInProject): one foreign
// uid refuses the whole batch with ErrPhotoNotFound — the same answer as a
// photograph that does not exist — and nothing is created or added.
//
// One reader stays instance-wide for now: Digests, the daily task digest, which
// is scoped in a later step together with its mail.

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every scoped Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// withProject defaults entry's ProjectUID to projectUID, so every audited task
// write records the project it happened in.
func withProject(entry audit.Entry, projectUID string) audit.Entry {
	if entry.ProjectUID == "" {
		entry.ProjectUID = projectUID
	}
	return entry
}

// photosInProjectSQL reports whether every uid of the batch $1 names a photo of
// the project $2: one statement, whatever the batch size, so a membership write
// is all or nothing.
const photosInProjectSQL = `
SELECT NOT EXISTS (
    SELECT 1 FROM unnest($1::text[]) AS b(uid)
    WHERE NOT EXISTS (SELECT 1 FROM photos p WHERE p.uid = b.uid AND p.project_id = $2)
)`

// requirePhotosInProject returns ErrPhotoNotFound unless every uid of photoUIDs
// names a photo of the project, read on tx — the transaction of the write it
// guards. An empty batch is trivially inside.
func requirePhotosInProject(ctx context.Context, tx pgx.Tx, projectUID string, photoUIDs []string) error {
	if len(photoUIDs) == 0 {
		return nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, photosInProjectSQL, photoUIDs, projectUID).Scan(&ok); err != nil {
		return fmt.Errorf("checking the project of %d photos: %w", len(photoUIDs), err)
	}
	if !ok {
		return ErrPhotoNotFound
	}
	return nil
}

// tasksInProjectSQL reports whether every uid of $1 names a task of the
// project $2.
const tasksInProjectSQL = `
SELECT NOT EXISTS (
    SELECT 1 FROM unnest($1::text[]) AS b(uid)
    WHERE NOT EXISTS (SELECT 1 FROM photo_tasks t WHERE t.uid = b.uid AND t.project_id = $2)
)`

// InProject reports whether every uid of taskUIDs names a task of the project.
// It is how a reader scoped by tasks (GET /photos?task=) tells a foreign or
// missing task — a 404 — from a task whose frozen group is merely empty. An
// empty input is trivially inside; an empty projectUID is ErrNoProject.
func (s *Store) InProject(ctx context.Context, projectUID string, taskUIDs []string) (bool, error) {
	if err := requireProject(projectUID); err != nil {
		return false, err
	}
	if len(taskUIDs) == 0 {
		return true, nil
	}
	var ok bool
	if err := s.pool.QueryRow(ctx, tasksInProjectSQL, taskUIDs, projectUID).Scan(&ok); err != nil {
		return false, fmt.Errorf("phototask: checking the project of %d tasks: %w", len(taskUIDs), err)
	}
	return ok, nil
}
