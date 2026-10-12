-- 0101_down: drop the membership table and the last-project pointer. Run after
-- 0103_down, which reads the roles back out of project_members.
DROP TABLE IF EXISTS project_members;
ALTER TABLE users DROP COLUMN IF EXISTS last_project_uid;

DELETE FROM schema_migrations WHERE version = 101;
