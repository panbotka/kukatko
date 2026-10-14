-- 0115_down: drop the storage move's record of derived objects left at the root.
--
-- The schema change is safe at any time, but the rows are forgotten with it:
-- each names a moved photo whose thumbnails or streaming segments are still at
-- the root, and a release older than 0115 has no record, no retry and no
-- `root derived` scan of them — after the rollback those root objects belong
-- to no row, and its `kukatko maintenance scan` sees them at most as legacy
-- layout or orphans. Rows exist only once a move has committed photos, and
-- then 0112_down refuses to go further, so this matters only when going back
-- to a tenancy release between 0112 and 0114: list the rows first
-- (SELECT photo_uid, kinds FROM storage_rekey_derived_failures) and clean
-- their root keys up by hand. The run's derived_cursor goes with them.
DROP TABLE IF EXISTS storage_rekey_derived_failures;
ALTER TABLE storage_rekey_runs DROP COLUMN IF EXISTS derived_cursor;

DELETE FROM schema_migrations WHERE version = 115;
