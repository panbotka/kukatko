-- 0113_down: drop the storage_rekey dedup index.
--
-- Only the storage move enqueues storage_rekey jobs, and a release older than
-- 0112 has no such handler; a queued one left behind would fail there as an
-- unknown type, so it is cancelled with the index.
DELETE FROM jobs WHERE type = 'storage_rekey' AND state IN ('queued', 'running');
DROP INDEX IF EXISTS idx_jobs_storage_rekey_dedup;

DELETE FROM schema_migrations WHERE version = 113;
