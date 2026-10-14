-- 0118_instance_settings_footer: the footer line is a project's setting.
--
-- Every page's footer used to say "Provozuje SDH Veselice" — a sentence baked
-- into the frontend's translations, so it named Veselice's operator on every
-- other tenant's pages too. The line is now plain text a project's admin edits
-- on the project's settings page (internal/settings, PUT /p/{project}/settings);
-- empty shows no line at all.
--
-- The bootstrap project `v` (internal/project BootstrapUID) is backfilled with
-- the sentence it showed until now, so its pages look unchanged; every other
-- project starts empty. Only an empty value is backfilled, so a re-run never
-- overwrites what an admin has written since.
ALTER TABLE instance_settings ADD COLUMN IF NOT EXISTS footer_text TEXT NOT NULL DEFAULT '';

UPDATE instance_settings
SET footer_text = 'Provozuje SDH Veselice'
WHERE project_id = 'pj0000000000000000veselice' AND footer_text = '';

COMMENT ON COLUMN instance_settings.footer_text IS
    'Plain-text line shown in the footer of every page of the project; empty shows none.';
