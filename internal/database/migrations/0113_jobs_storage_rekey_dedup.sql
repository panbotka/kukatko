-- 0113_jobs_storage_rekey_dedup: one storage_rekey job per project.
--
-- The storage move of a project (internal/rekey) is driven by one job,
-- storage_rekey, that does about a second of work and then requeues itself
-- (the worker's Defer turns the same row back to queued) until the run is done
-- or failed. Start, Resume and Retry all enqueue it, so the dedup key is the
-- project — library-wide, keyed on (type, payload ->> 'project_uid') like the
-- others (0108).
--
-- Unlike the other library-wide indexes, the predicate covers running as well
-- as queued. A second job for the same project must never exist next to a
-- running one: when the running job defers itself back to queued, a queued
-- twin would make that UPDATE violate the index, and two jobs stepping one run
-- would only race each other. A start or a resume that lands while the job is
-- running is therefore a no-op — the running job re-reads the run's state on
-- its next pass anyway.
--
-- Jobs of this type never existed before this migration (no handler was
-- registered), so building the index cannot fail on existing rows.
-- IF NOT EXISTS makes the migration idempotent.
--
-- This migration is wrapped in a transaction by the runner.

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_storage_rekey_dedup ON jobs (type, (payload ->> 'project_uid'))
    WHERE type = 'storage_rekey' AND state IN ('queued', 'running');
