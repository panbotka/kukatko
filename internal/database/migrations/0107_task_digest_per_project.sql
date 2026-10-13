-- 0107_task_digest_per_project: the task digest is one job per (project, person).
--
-- Multi-tenancy step 18. A person who works in two projects gets one digest of
-- each, measured against the watermark of each membership
-- (project_members.task_digest_at; users.task_digest_at is a transition copy
-- nobody writes any more). The daily scheduler therefore enqueues one
-- `task_digest` job per pair with something due, its payload naming
-- `project_uid` — the field every library-wide job uses — and `user_uid`.
--
-- The dedup key has to carry both: keyed on the type alone (0084), the first
-- pair's queued job would swallow every other pair's, and only one person of
-- one project would ever hear anything. The predicate stays state = 'queued'
-- only, for the reason 0084 gives: a restart straddling the scheduled hour must
-- not queue the same pair twice, while a running job never blocks the next.
--
-- Jobs queued before this migration carry no payload: both expressions are
-- NULL for them, NULLs are distinct in a unique index, so rebuilding the index
-- can never fail on existing rows. Their handler reads a missing pair as
-- "nothing to send" (the next daily run covers everybody).
--
-- The index keeps its name, so the queue's list of "this work is already
-- scheduled" indexes (internal/jobs dedupIndexes) needs no change.
--
-- This migration is wrapped in a transaction by the runner.

DROP INDEX IF EXISTS idx_jobs_task_digest_dedup;

CREATE UNIQUE INDEX idx_jobs_task_digest_dedup ON jobs ((payload ->> 'project_uid'), (payload ->> 'user_uid'))
    WHERE type = 'task_digest' AND state = 'queued';
