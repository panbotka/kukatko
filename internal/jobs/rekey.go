package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// hurryStorageRekeySQL makes the project's queued storage_rekey job runnable
// now. A job that waits out a pause re-checks the run only once a minute; a
// resume that found it queued pulls that re-check forward instead of leaving
// the operator to wait for it.
const hurryStorageRekeySQL = `UPDATE jobs SET run_after = now(), updated_at = now()
WHERE type = '` + TypeStorageRekey + `' AND state = 'queued' AND payload ->> 'project_uid' = $1
  AND run_after > now()
RETURNING id`

// EnqueueStorageRekey schedules the storage_rekey job of the project
// projectUID using exec — a pool, or the transaction that starts, resumes or
// retries the run, so the run and the job that drives it commit together.
//
// One job drives one run, so the call is idempotent per project: a job of the
// project already queued or running (idx_jobs_storage_rekey_dedup, migration
// 0113) makes it a no-op, except that a queued job waiting to run later is
// made runnable now. It steps aside on the duplicate rather than failing on it
// (EnqueueOrSkip), so the caller's transaction survives. An empty projectUID is
// refused with ErrProjectRequired.
func EnqueueStorageRekey(ctx context.Context, exec Execer, projectUID string) error {
	if projectUID == "" {
		return fmt.Errorf("jobs: enqueuing %s: %w", TypeStorageRekey, ErrProjectRequired)
	}
	payload, err := projectJSON(projectUID)
	if err != nil {
		return err
	}
	_, err = EnqueueOrSkip(ctx, exec, TypeStorageRekey, payload, EnqueueOptions{})
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, ErrDuplicate):
		return fmt.Errorf("jobs: enqueuing %s: %w", TypeStorageRekey, err)
	}
	var id int64
	err = exec.QueryRow(ctx, hurryStorageRekeySQL, projectUID).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("jobs: hurrying the queued %s job: %w", TypeStorageRekey, err)
	}
	return nil
}
