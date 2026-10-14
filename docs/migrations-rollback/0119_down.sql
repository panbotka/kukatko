-- 0119_down: drop the render facts of notifications.
--
-- Safe at any time: a release older than 0119 neither reads nor writes the
-- column. Every notification keeps its stored title and body, which that
-- release shows as it always did — in the instance's language.
ALTER TABLE notifications DROP COLUMN IF EXISTS params;

DELETE FROM schema_migrations WHERE version = 119;
