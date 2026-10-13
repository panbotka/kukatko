-- 0111_down: drop the per-project usage index (an index only, no data).
DROP INDEX IF EXISTS idx_photos_project_file_size;

DELETE FROM schema_migrations WHERE version = 111;
