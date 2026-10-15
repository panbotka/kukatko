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
// schedules a rewrite of the sidecar of every photo of the project — so each is
// written afresh under the new prefix with the new keys inside — then removes
// what the root layout still holds of the project's: the root families.yaml and
// the root sidecars, each only once a listing shows its replacement under the
// new prefix (the genealogy export is copied there first when no export has
// been written since Start). Only then is the rewrite of the project's
// families.yaml scheduled: a family export job that wrote the fresh file between
// the listing and that copy has it overwritten by the pre-move root one, and the
// export scheduled after the copy writes it afresh again. Last, the run is
// marked done.
//
// A run already done is returned as it is. It returns ErrNotRunning for a run
// that is paused or failed and ErrPendingCleanup while a moved photo's old
// objects await deletion. When photos are still in the old layout and the pass
// recorded no error at all, they are photos the pass never saw — an upload
// already in flight at Start stores at the root, and its key may sort below the
// cursor — so the cursor is rewound and ErrPendingCleanup returned: the next
// steps take them. When the pass did record errors, the run is failed and
// ErrPhotosRemain returned; Retry starts another pass over every photo left.
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
	if err := s.enqueueSidecars(ctx, run.ProjectUID); err != nil {
		return run, err
	}
	if err := s.cleanRoot(ctx, run); err != nil {
		return run, err
	}
	if err := s.jobs.EnqueueFamilyExport(ctx, run.ProjectUID); err != nil {
		return run, fmt.Errorf("rekey: scheduling the family export: %w", err)
	}
	return queryRun(ctx, s.pool, ErrNoRun, setStateSQL, run.UID, string(StateDone))
}

// rewindSQL restarts the pass of a running run that has recorded no error, so
// its steps reach photos the pass never saw. It returns nothing for a run that
// has errors or is no longer running.
const rewindSQL = `UPDATE storage_rekey_runs SET last_key = '', updated_at = now()
WHERE uid = $1 AND state = 'running' AND error_count = 0 RETURNING uid`

// checkNoneRemain returns the run as it is when no photo of its project is left
// in the old layout. Otherwise, when the pass recorded no error, it rewinds the
// pass and returns ErrPendingCleanup (the photos left are ones the pass never
// saw: uploads in flight at Start, stored at the root below the cursor); when
// it did, it fails the run and returns it failed with ErrPhotosRemain.
func (s *Service) checkNoneRemain(ctx context.Context, run Run) (Run, error) {
	var remaining int64
	if err := s.pool.QueryRow(ctx, remainingSQL, run.ProjectUID, run.TargetPrefix).Scan(&remaining); err != nil {
		return run, fmt.Errorf("rekey: counting photos left behind: %w", err)
	}
	if remaining == 0 {
		return run, nil
	}
	var rewound string
	err := s.pool.QueryRow(ctx, rewindSQL, run.UID).Scan(&rewound)
	switch {
	case err == nil:
		s.log.InfoContext(ctx, "rekey: photos the pass never saw; another pass takes them",
			"run", run.UID, "photos", remaining)
		return run, ErrPendingCleanup
	case !errors.Is(err, pgx.ErrNoRows):
		return run, fmt.Errorf("rekey: rewinding the pass: %w", err)
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

// enqueueSidecars schedules the sidecar rewrite of every photo of the project.
func (s *Service) enqueueSidecars(ctx context.Context, projectUID string) error {
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
	shared, err := s.rootShared(ctx)
	if err != nil {
		return err
	}
	if shared {
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
// yet, deleted only once a listing shows it. The copy may overwrite a fresh
// export a job wrote after the listing; Finish schedules the export only after
// this, so the file is rewritten afresh either way.
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
		if listed, err = s.copyFamilies(ctx, root, target); err != nil {
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

// copyFamilies copies the root genealogy export to target, which a listing
// just showed absent, and returns a fresh listing of target.
func (s *Service) copyFamilies(ctx context.Context, root, target string) (map[string]bool, error) {
	if err := s.at(stageFamiliesListed); err != nil {
		return nil, err
	}
	if err := s.store.Copy(ctx, root, target); err != nil {
		return nil, fmt.Errorf("rekey: copying %s: %w", root, err)
	}
	return s.listed(ctx, target)
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

// rootShared reports whether a project is still in the root layout — the root
// is then not the moved projects' alone, and nothing there may be taken as
// theirs.
func (s *Service) rootShared(ctx context.Context) (bool, error) {
	var rootProjects int
	if err := s.pool.QueryRow(ctx, rootLayoutProjectsSQL).Scan(&rootProjects); err != nil {
		return false, fmt.Errorf("rekey: counting root-layout projects: %w", err)
	}
	return rootProjects > 0, nil
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
