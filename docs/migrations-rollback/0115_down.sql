-- 0115_down: drop the storage move's record of derived objects left at the root.
--
-- Safe at any time: the rows only name photos whose thumbnails or streaming
-- segments are still at the root, and a release older than 0115 neither reads
-- nor retries them (`kukatko maintenance scan` of this release finds them
-- again from a bucket listing). The run's derived_cursor goes with them.
DROP TABLE IF EXISTS storage_rekey_derived_failures;
ALTER TABLE storage_rekey_runs DROP COLUMN IF EXISTS derived_cursor;

DELETE FROM schema_migrations WHERE version = 115;
