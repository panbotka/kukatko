-- 0100_down: drop the projects table. Run last: every other tenancy object
-- references it.
DROP TABLE IF EXISTS projects;

DELETE FROM schema_migrations WHERE version = 100;
