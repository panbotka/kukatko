package rekey

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/storekeys"
)

// Finish closes run once no photo of its project is left in the old layout. It
// schedules a rewrite of the project's families.yaml and of the sidecar of
// every photo of the project — so both are written afresh under the new prefix
// with the new keys inside — then removes what the root layout still holds of
// the project's: the root families.yaml and the root sidecars, each only once a
// listing shows its replacement under the new prefix (the genealogy export is
// copied there first when no export has been written since Start). Last, the run
// is marked done.
//
// A run already done is returned as it is. It returns ErrNotRunning for a run
// that is paused or failed and ErrPendingCleanup while a moved photo's old
// objects await deletion. When photos are still in the old layout — the ones the
// pass recorded errors for — the run is failed and ErrPhotosRemain returned;
// Retry starts another pass over them.
//
// Before anything is rewritten it sweeps the root for thumbnails and streaming
// segments of the project's moved photos (a bucket listing): a photo found there
// and not yet recorded is recorded for the retry and ErrPendingCleanup returned,
// so the next steps move it. When recorded photos remain after that — their
// derived objects could not be moved this pass — the run is failed and
// ErrDerivedRemain returned; Retry tries them again.
//
// A root object that cannot be removed is recorded in the run's errors and left
// behind; the maintenance scan reports it. Nothing at the root is removed while
// another project is still in the root layout.
func (s *Service) Finish(ctx context.Context, run Run) (Run, error) {
	run, err := s.reload(ctx, run.UID)
	if err != nil {
		return Run{}, err
	}
	switch {
	case run.State == StateDone:
		return run, nil
	case run.State != StateRunning:
		return run, fmt.Errorf("%w: it is %s", ErrNotRunning, run.State)
	case run.PendingPhotoUID != "":
		return run, ErrPendingCleanup
	}
	if run, err = s.checkNoneRemain(ctx, run); err != nil {
		return run, err
	}
	if run, err = s.settleDerived(ctx, run); err != nil {
		return run, err
	}
	if err := s.enqueueRewrites(ctx, run.ProjectUID); err != nil {
		return run, err
	}
	if err := s.cleanRoot(ctx, run); err != nil {
		return run, err
	}
	return queryRun(ctx, s.pool, ErrNoRun, setStateSQL, run.UID, string(StateDone))
}

// checkNoneRemain fails the run, and returns it failed with ErrPhotosRemain,
// when photos of its project are still in the old layout; otherwise it returns
// the run as it is.
func (s *Service) checkNoneRemain(ctx context.Context, run Run) (Run, error) {
	var remaining int64
	if err := s.pool.QueryRow(ctx, remainingSQL, run.ProjectUID, run.TargetPrefix).Scan(&remaining); err != nil {
		return run, fmt.Errorf("rekey: counting photos left behind: %w", err)
	}
	if remaining == 0 {
		return run, nil
	}
	cause := fmt.Errorf("%w: %d photo(s) could not be moved", ErrPhotosRemain, remaining)
	if _, err := s.recordError(ctx, run.UID, "", "", cause); err != nil {
		return run, err
	}
	failed, err := queryRun(ctx, s.pool, ErrNoRun, setStateSQL, run.UID, string(StateFailed))
	if err != nil {
		return run, err
	}
	return failed, cause
}

// settleDerived sweeps the root for derived objects of the run's moved photos
// nobody recorded — returning ErrPendingCleanup when it recorded any, so the
// next steps move them — and then fails the run with ErrDerivedRemain while
// recorded photos remain. Otherwise it returns the run as it is.
func (s *Service) settleDerived(ctx context.Context, run Run) (Run, error) {
	swept, err := s.sweepRootDerived(ctx, run)
	if err != nil {
		return run, err
	}
	if swept > 0 {
		s.log.InfoContext(ctx, "rekey: derived objects found at the root", "run", run.UID, "photos", swept)
		return run, ErrPendingCleanup
	}
	return s.checkNoDerivedRemain(ctx, run)
}

// checkNoDerivedRemain fails the run, and returns it failed with
// ErrDerivedRemain, while moved photos of its project still have derived
// objects recorded at the root; otherwise it returns the run as it is.
func (s *Service) checkNoDerivedRemain(ctx context.Context, run Run) (Run, error) {
	run, err := s.reload(ctx, run.UID)
	if err != nil || run.DerivedFailed == 0 {
		return run, err
	}
	cause := fmt.Errorf("%w: %d photo(s) keep thumbnails or streaming segments at the root",
		ErrDerivedRemain, run.DerivedFailed)
	if _, err := s.recordError(ctx, run.UID, "", "", cause); err != nil {
		return run, err
	}
	failed, err := queryRun(ctx, s.pool, ErrNoRun, setStateSQL, run.UID, string(StateFailed))
	if err != nil {
		return run, err
	}
	return failed, cause
}

// enqueueRewrites schedules the family export of the project and the sidecar of
// every one of its photos.
func (s *Service) enqueueRewrites(ctx context.Context, projectUID string) error {
	if err := s.jobs.EnqueueFamilyExport(ctx, projectUID); err != nil {
		return fmt.Errorf("rekey: scheduling the family export: %w", err)
	}
	rows, err := s.pool.Query(ctx, photoUIDsSQL, projectUID)
	if err != nil {
		return fmt.Errorf("rekey: listing photos for their sidecars: %w", err)
	}
	uids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("rekey: reading photos for their sidecars: %w", err)
	}
	for _, uid := range uids {
		if err := s.jobs.EnqueueSidecar(ctx, uid); err != nil {
			return fmt.Errorf("rekey: scheduling the sidecar of %s: %w", uid, err)
		}
	}
	return nil
}

// cleanRoot removes the root genealogy export and the root sidecars that have
// their replacement under the run's prefix — unless another project is still in
// the root layout, in which case the root is not the run's project's alone and
// nothing there is touched.
func (s *Service) cleanRoot(ctx context.Context, run Run) error {
	var rootProjects int
	if err := s.pool.QueryRow(ctx, rootLayoutProjectsSQL).Scan(&rootProjects); err != nil {
		return fmt.Errorf("rekey: counting root-layout projects: %w", err)
	}
	if rootProjects > 0 {
		s.log.InfoContext(ctx, "rekey: root layout still in use; leaving its objects", "run", run.UID)
		return nil
	}
	problems := append(s.removeRootSidecars(ctx, run.TargetPrefix), s.moveFamilies(ctx, run.TargetPrefix))
	for _, problem := range problems {
		if problem == nil {
			continue
		}
		if fatal(ctx, problem) {
			return problem
		}
		if _, err := s.recordError(ctx, run.UID, "", "", problem); err != nil {
			return err
		}
	}
	return nil
}

// moveFamilies makes sure the project's genealogy export is under prefix and
// then removes the root one: copied there first when no export has landed there
// yet, deleted only once a listing shows it.
func (s *Service) moveFamilies(ctx context.Context, prefix string) error {
	root := storekeys.FamiliesKey
	target := storekeys.Join(prefix, root)
	if _, err := s.store.Stat(ctx, root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("rekey: checking %s: %w", root, err)
	}
	listed, err := s.listed(ctx, target)
	if err != nil {
		return err
	}
	if !listed[target] {
		if err := s.store.Copy(ctx, root, target); err != nil {
			return fmt.Errorf("rekey: copying %s: %w", root, err)
		}
		if listed, err = s.listed(ctx, target); err != nil {
			return err
		}
	}
	if !listed[target] {
		return fmt.Errorf("rekey: %s kept: %s is not listed after the copy", root, target)
	}
	if err := s.store.Delete(ctx, root); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rekey: deleting %s: %w", root, err)
	}
	return nil
}

// removeRootSidecars deletes every root sidecar whose replacement — the same
// key under prefix — a listing shows. A root sidecar without one is left for the
// maintenance scan.
func (s *Service) removeRootSidecars(ctx context.Context, prefix string) []error {
	rootDir := storekeys.SidecarDir + "/"
	replacements, err := s.listed(ctx, storekeys.Join(prefix, rootDir))
	if err != nil {
		return []error{err}
	}
	var stale []string
	err = s.store.KeysWithPrefix(ctx, rootDir, func(key string) error {
		if replacements[storekeys.Join(prefix, key)] {
			stale = append(stale, key)
		}
		return nil
	})
	if err != nil {
		return []error{fmt.Errorf("rekey: listing root sidecars: %w", err)}
	}
	var problems []error
	for _, key := range stale {
		if err := s.store.Delete(ctx, key); err != nil && !errors.Is(err, os.ErrNotExist) {
			problems = append(problems, fmt.Errorf("rekey: deleting root sidecar %s: %w", key, err))
		}
	}
	return problems
}

// listed returns the set of keys the store lists under prefix.
func (s *Service) listed(ctx context.Context, prefix string) (map[string]bool, error) {
	keys := map[string]bool{}
	err := s.store.KeysWithPrefix(ctx, prefix, func(key string) error {
		if strings.HasPrefix(key, prefix) {
			keys[key] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rekey: listing %s: %w", prefix, err)
	}
	return keys, nil
}
