-- 0106_down: put back 0075's one-queued-export-for-the-instance index.
--
-- With one project there is at most one queued family_export, so the old key
-- cannot meet a duplicate.
DROP INDEX IF EXISTS idx_jobs_family_export_dedup;
CREATE UNIQUE INDEX idx_jobs_family_export_dedup ON jobs (type)
    WHERE type = 'family_export' AND state = 'queued';

DELETE FROM schema_migrations WHERE version = 106;
