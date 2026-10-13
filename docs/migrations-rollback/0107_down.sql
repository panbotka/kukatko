-- 0107_down: put back 0084's one-queued-digest-for-the-instance index.
--
-- A per-(project, person) digest queue may hold several queued jobs; the old
-- key admits one, so the queued ones are cancelled first (the daily scheduler
-- enqueues the next pass, nothing is lost but a single day's mail).
DELETE FROM jobs WHERE type = 'task_digest' AND state = 'queued';
DROP INDEX IF EXISTS idx_jobs_task_digest_dedup;
CREATE UNIQUE INDEX idx_jobs_task_digest_dedup ON jobs (type)
    WHERE type = 'task_digest' AND state = 'queued';

DELETE FROM schema_migrations WHERE version = 107;
