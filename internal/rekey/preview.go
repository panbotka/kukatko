package rekey

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/project"
)

// PreviewPhoto is one photo a move would take: the move of its original and how
// many derived keys (thumbnail sizes, streaming segments, the sidecar) the plan
// lists after it. The plan lists keys whether or not an object exists there, so
// Derived is an upper bound; the move copies what exists and skips the rest.
type PreviewPhoto struct {
	// UID identifies the photo.
	UID string
	// Original is the move of the photo's original.
	Original Move
	// Derived counts the derived keys planned to follow the original.
	Derived int
}

// Preview is what starting the move of a project would do, computed without
// doing any of it: the prefix the objects would move under, how many photos
// and bytes of originals are still in the old layout, and the first photos of
// the pass in the order the move takes them.
type Preview struct {
	// ProjectUID is the project previewed.
	ProjectUID string
	// TargetPrefix is the prefix the objects would move under.
	TargetPrefix string
	// Photos and Bytes count the photos still in the old layout and the summed
	// size of their originals — the totals a run started now would record.
	Photos int64
	Bytes  int64
	// First are the first photos of the pass, in file_path order.
	First []PreviewPhoto
}

// previewTotalsSQL counts what a run started now would record as its totals.
const previewTotalsSQL = `SELECT count(*), COALESCE(sum(file_size), 0)::bigint
FROM photos WHERE project_id = $1 AND NOT starts_with(file_path, $2)`

// Preview reports what Start would do to the project projectUID without
// changing anything: the totals it would record and the first limit photos of
// the pass with their planned moves. It refuses exactly what Start refuses —
// ErrProjectNotFound, ErrRunActive, ErrAlreadyPrefixed — so a dry run also
// answers "would it start at all". The checks run in a transaction that is
// always rolled back.
func (s *Service) Preview(ctx context.Context, projectUID string, limit int) (Preview, error) {
	target := project.StoragePrefixFor(projectUID)
	if err := s.checkStartReadOnly(ctx, projectUID, target); err != nil {
		return Preview{}, err
	}
	preview := Preview{ProjectUID: projectUID, TargetPrefix: target, First: []PreviewPhoto{}}
	if err := s.pool.QueryRow(ctx, previewTotalsSQL, projectUID, target).
		Scan(&preview.Photos, &preview.Bytes); err != nil {
		return Preview{}, fmt.Errorf("rekey: counting the photos to move: %w", err)
	}
	if limit <= 0 {
		return preview, nil
	}
	batch, err := s.pendingBatch(ctx, Run{ProjectUID: projectUID, TargetPrefix: target}, limit)
	if err != nil {
		return Preview{}, err
	}
	for _, photo := range batch {
		moves := Plan(photo, target)
		if len(moves) == 0 {
			continue
		}
		preview.First = append(preview.First, PreviewPhoto{UID: photo.UID, Original: moves[0], Derived: len(moves) - 1})
	}
	return preview, nil
}

// checkStartReadOnly runs Start's refusals in a transaction it always rolls
// back, so the project row's lock is released and nothing is written. (Not a
// READ ONLY transaction: checkStart locks the row FOR UPDATE, which one
// refuses.)
func (s *Service) checkStartReadOnly(ctx context.Context, projectUID, target string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rekey: opening the preview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // nothing was written: there is nothing to keep
	if _, err := s.checkStart(ctx, tx, projectUID, target); err != nil {
		return fmt.Errorf("rekey: previewing the move of %s: %w", projectUID, err)
	}
	return nil
}
