-- 0040_jobs_claim_index: align the job-claim index with the claim ORDER BY.
--
-- The claim query (internal/jobs, claimSQL) selects the next runnable row with
--   WHERE state = 'queued' AND run_after <= now()
--   ORDER BY priority DESC, run_after ASC, id ASC
--   FOR UPDATE SKIP LOCKED LIMIT 1
-- but 0005's idx_jobs_claim (state, run_after, priority) orders its columns the
-- other way round, so the planner could not walk it in claim order: under a deep
-- backlog every claim re-sorted the whole queued set.
--
-- Replace it with two partial indexes, each matching one hot query exactly and
-- each far smaller than the full-table original (a finished queue is mostly
-- 'done'/'dead' rows, which neither predicate indexes):
--   * the claim path, in claim order;
--   * the once-a-minute stale-lock recovery scan over running locks.
-- This migration is wrapped in a transaction by the runner.

CREATE INDEX idx_jobs_claim_ordered ON jobs (priority DESC, run_after, id)
    WHERE state = 'queued';

CREATE INDEX idx_jobs_running_locks ON jobs (locked_at)
    WHERE state = 'running';

DROP INDEX idx_jobs_claim;
