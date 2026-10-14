-- 0118_down: drop the project's footer line.
--
-- Safe at any time: a release older than 0118 neither reads nor writes the
-- column — it draws the footer sentence it hard-codes ("Provozuje SDH
-- Veselice") on every project's pages again. The lines admins wrote are lost;
-- read them first (SELECT project_id, footer_text FROM instance_settings) if
-- they are to be typed back in after a later upgrade.
ALTER TABLE instance_settings DROP COLUMN IF EXISTS footer_text;

DELETE FROM schema_migrations WHERE version = 118;
