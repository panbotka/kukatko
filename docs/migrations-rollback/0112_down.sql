-- 0112_down: drop the storage-move bookkeeping.
--
-- Refuses once a move has committed a single photo: a moved photo's file_path
-- already names projects/<uid>/…, which a release older than 0112 cannot place
-- under its project, and dropping the run would forget where the move stopped.
-- Finish the move or put the keys back first; an empty run (started, nothing
-- copied) is safe to drop.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM storage_rekey_runs WHERE done_photos > 0) THEN
        RAISE EXCEPTION 'a storage move has committed photos; refusing to drop storage_rekey_runs';
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_photos_project_file_path;
DROP TABLE IF EXISTS storage_rekey_runs;

DELETE FROM schema_migrations WHERE version = 112;
