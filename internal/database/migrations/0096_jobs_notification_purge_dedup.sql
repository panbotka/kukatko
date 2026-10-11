-- 0096_jobs_notification_purge_dedup: at most one queued notification_purge job.
--
-- The notification retention purge (internal/notifypurgejob) is enqueued once a
-- day by a scheduler that also fires at startup. Its payload names nothing, so
-- like the task digest (migration 0084) the dedup key is the type alone: a
-- restart, or two restarts in a row, cannot queue a second purge while the
-- first is still waiting. A running job does not block a fresh one — the purge
-- is idempotent, so running twice only finds nothing the second time.
--
-- This migration is wrapped in a transaction by the runner.

CREATE UNIQUE INDEX idx_jobs_notification_purge_dedup ON jobs (type)
    WHERE type = 'notification_purge' AND state = 'queued';
