package rekey

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/storage"
)

// Step moves up to batchSize photos of run (DefaultBatchSize when not
// positive) and reports how many it took and whether the pass is over. It is
// the unit of work the worker job calls until done is true, then calls Finish.
//
// It re-reads the run first, refusing one that is not running with
// ErrNotRunning, and finishes the photo a crashed step left pending before
// taking new ones. Photos are taken in file_path order after the run's cursor.
// A photo that fails is recorded in the run's error ring and skipped; when that
// pushes the run past MaxErrors the run is failed and Step returns
// ErrRunFailed. A pause that lands mid-batch is honoured after the photo in
// hand. A failure that is not about one photo — a cancelled context, a store
// that refuses every request, a database error — is returned as it is, with the
// run left exactly resumable.
func (s *Service) Step(ctx context.Context, run Run, batchSize int) (processed int, done bool, err error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	run, err = s.reload(ctx, run.UID)
	if err != nil {
		return 0, false, err
	}
	if run.State != StateRunning {
		return 0, false, fmt.Errorf("%w: it is %s", ErrNotRunning, run.State)
	}
	if run.PendingPhotoUID != "" {
		if err := s.finishPending(ctx, run); err != nil {
			return 0, false, err
		}
		if run, err = s.reload(ctx, run.UID); err != nil {
			return 0, false, err
		}
		if run.State != StateRunning {
			return 0, false, stoppedErr(run.State)
		}
	}
	batch, err := s.pendingBatch(ctx, run, batchSize)
	if err != nil {
		return 0, false, err
	}
	if len(batch) == 0 {
		return 0, true, nil
	}
	return s.stepBatch(ctx, run, batch)
}

// stepBatch moves the photos of one batch in order, stopping early when the run
// stops running.
func (s *Service) stepBatch(ctx context.Context, run Run, batch []PhotoFiles) (int, bool, error) {
	processed := 0
	for _, photo := range batch {
		state, err := s.stepPhoto(ctx, run, photo)
		if err != nil {
			return processed, false, err
		}
		processed++
		switch state {
		case StateRunning:
			continue
		case StateFailed, StatePaused, StateDone:
			return processed, false, stoppedErr(state)
		}
	}
	return processed, false, nil
}

// stoppedErr is what a step that saw the run stop mid-way returns: ErrRunFailed
// for a run its own errors failed, nil for a pause (the next step refuses).
func stoppedErr(state State) error {
	if state == StateFailed {
		return ErrRunFailed
	}
	return nil
}

// stepPhoto moves one photo and returns the run's state afterwards. A per-photo
// failure is recorded rather than returned; only a failure that would make the
// next photo fail the same way is returned.
func (s *Service) stepPhoto(ctx context.Context, run Run, photo PhotoFiles) (State, error) {
	state, err := s.movePhoto(ctx, run, photo)
	if err == nil {
		return state, nil
	}
	if fatal(ctx, err) {
		return "", err
	}
	return s.recordError(ctx, run.UID, photo.FilePath, photo.FilePath, err)
}

// fatal reports whether err stops the whole step rather than one photo: the
// context ended, the store itself is unusable, or the test hook crashed it.
func fatal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || storage.IsSystemic(err) || errors.Is(err, errCrash)
}

// errCrash is what a test hook returns to simulate the process dying.
var errCrash = errors.New("rekey: simulated crash")

// movePhoto runs the whole safety sequence for one photo: copy the original,
// verify the copy, move the row, then hand over to completePhoto for the derived
// objects and the deletions.
func (s *Service) movePhoto(ctx context.Context, run Run, photo PhotoFiles) (State, error) {
	moves := Plan(photo, run.TargetPrefix)
	if len(moves) == 0 {
		return "", fmt.Errorf("rekey: nothing to plan for %q", photo.FilePath)
	}
	original := moves[0]
	if err := s.copyVerified(ctx, photo, original); err != nil {
		return "", err
	}
	if err := s.at(stageCopied); err != nil {
		return "", err
	}
	if err := s.commitMove(ctx, run, photo, original); err != nil {
		return "", err
	}
	if err := s.at(stageCommitted); err != nil {
		return "", err
	}
	return s.completePhoto(ctx, run, photo, moves)
}

// copyVerified makes sure the original's new key holds a verified copy: a copy
// already there is verified first, and copied afresh only when it does not
// verify, so a resumed run re-uses what a crashed one wrote.
func (s *Service) copyVerified(ctx context.Context, photo PhotoFiles, original Move) error {
	_, err := s.store.Stat(ctx, original.To)
	switch {
	case err == nil:
		if s.verify(ctx, photo, original) == nil {
			return nil
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("rekey: checking %s: %w", original.To, err)
	}
	if err := s.store.Copy(ctx, original.From, original.To); err != nil {
		return fmt.Errorf("rekey: copying %s: %w", original.From, err)
	}
	return s.verify(ctx, photo, original)
}

// commitMove points the photo row and its file row at the new key and names the
// photo as pending on the run, all in one transaction.
func (s *Service) commitMove(ctx context.Context, run Run, photo PhotoFiles, original Move) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, movePhotoSQL, photo.UID, original.From, original.To, run.ProjectUID)
		if err != nil {
			return fmt.Errorf("moving the photo row: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return ErrPhotoChanged
		}
		if _, err := tx.Exec(ctx, moveFileSQL, photo.UID, original.From, original.To); err != nil {
			return fmt.Errorf("moving the file row: %w", err)
		}
		if _, err := tx.Exec(ctx, markPendingSQL, run.UID, photo.UID, original.From); err != nil {
			return fmt.Errorf("marking the photo pending: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("rekey: committing %s: %w", photo.UID, err)
	}
	return nil
}

// completePhoto finishes a photo whose row already points at its new key: it
// copies the derived objects that exist, deletes the old original and the old
// derived objects it copied, then counts the photo and clears it as pending. A
// derived object that cannot be copied keeps its old key, and that — or a
// deletion that fails — is recorded as an error of the photo; the photo still
// counts as moved, since its row points at a verified copy.
func (s *Service) completePhoto(ctx context.Context, run Run, photo PhotoFiles, moves []Move) (State, error) {
	var problems []error
	for _, move := range moves[1:] {
		if err := s.moveDerived(ctx, move); err != nil {
			if fatal(ctx, err) {
				return "", err
			}
			problems = append(problems, err)
		}
	}
	if err := s.deleteOldOriginal(ctx, moves[0]); err != nil {
		if fatal(ctx, err) {
			return "", err
		}
		problems = append(problems, err)
	}
	var state string
	err := s.pool.QueryRow(ctx, completePhotoSQL, run.UID, photo.FileSize, moves[0].From).Scan(&state)
	if err != nil {
		return "", fmt.Errorf("rekey: counting %s: %w", photo.UID, err)
	}
	if len(problems) > 0 {
		return s.recordError(ctx, run.UID, moves[0].From, "", errors.Join(problems...))
	}
	return State(state), nil
}

// moveDerived copies one derived object to its new key and deletes the old one.
// An object that does not exist is no work at all.
func (s *Service) moveDerived(ctx context.Context, move Move) error {
	err := s.store.Copy(ctx, move.From, move.To)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rekey: copying %s %s: %w", move.Kind, move.From, err)
	}
	if err := s.store.Delete(ctx, move.From); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rekey: deleting old %s %s: %w", move.Kind, move.From, err)
	}
	return nil
}

// deleteOldOriginal deletes the original's old key — unless a photo row still
// names it, which a row of this photo no longer does, so only another photo's
// could. A key already gone is no work.
func (s *Service) deleteOldOriginal(ctx context.Context, original Move) error {
	var referenced bool
	if err := s.pool.QueryRow(ctx, keyReferencedSQL, original.From).Scan(&referenced); err != nil {
		return fmt.Errorf("rekey: checking references to %s: %w", original.From, err)
	}
	if referenced {
		return fmt.Errorf("rekey: old original %s kept: another photo row names it", original.From)
	}
	if err := s.store.Delete(ctx, original.From); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rekey: deleting old original %s: %w", original.From, err)
	}
	return nil
}

// finishPending completes the photo a crashed step moved but did not clean up.
// The new copy is verified again before anything old is deleted — and copied
// afresh from the old key, which is still there, when it does not verify. A
// photo that is gone, or whose row no longer points at the move's new key, is
// only forgotten: its objects are left for the maintenance scan to report. A
// copy that can be neither verified nor redone is recorded as an error and
// forgotten too, with nothing deleted.
func (s *Service) finishPending(ctx context.Context, run Run) error {
	photo, found, err := s.loadPhoto(ctx, run.ProjectUID, run.PendingPhotoUID)
	if err != nil {
		return err
	}
	old := PhotoFiles{
		UID: photo.UID, FilePath: run.PendingOldKey, FileHash: photo.FileHash,
		FileSize: photo.FileSize, Renditions: photo.Renditions,
	}
	moves := Plan(old, run.TargetPrefix)
	if !found || len(moves) == 0 || moves[0].To != photo.FilePath {
		return s.clearPending(ctx, run.UID)
	}
	if err := s.copyVerified(ctx, old, moves[0]); err != nil {
		if fatal(ctx, err) {
			return err
		}
		if _, err := s.recordError(ctx, run.UID, moves[0].To, "", err); err != nil {
			return err
		}
		return s.clearPending(ctx, run.UID)
	}
	_, err = s.completePhoto(ctx, run, old, moves)
	return err
}

// clearPending forgets the run's pending photo.
func (s *Service) clearPending(ctx context.Context, runUID string) error {
	if _, err := s.pool.Exec(ctx, clearPendingSQL, runUID); err != nil {
		return fmt.Errorf("rekey: clearing the pending photo: %w", err)
	}
	return nil
}

// recordError appends cause to the run's error ring under key, advances the
// cursor to cursor (when not empty) so the pass moves past the photo, and
// returns the run's state afterwards; a run that the error tipped over
// MaxErrors is failed.
func (s *Service) recordError(ctx context.Context, runUID, key, cursor string, cause error) (State, error) {
	s.log.WarnContext(ctx, "rekey: photo skipped", "run", runUID, "key", key, "error", cause)
	entry, err := errorEntry(key, cause, s.clock())
	if err != nil {
		return "", err
	}
	var state string
	err = s.pool.QueryRow(ctx, recordErrorSQL, runUID, entry, cursor, MaxErrorRing, MaxErrors).Scan(&state)
	if err != nil {
		return "", fmt.Errorf("rekey: recording an error: %w", err)
	}
	return State(state), nil
}
