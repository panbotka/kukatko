-- 0074_subjects_nickname: the thing people actually call somebody.
--
-- Every person in a village archive has a name on their documents and, very
-- often, a different one everybody uses. Until now the library could record only
-- the first, so searching for the handle somebody naming faces actually
-- remembers found nothing.
--
-- The column is declared exactly as name and notes already are on this table:
-- TEXT NOT NULL DEFAULT '', so "no nickname" is the empty string rather than
-- NULL and every read path can compare it without a NULL guard.
--
-- What it deliberately is NOT:
--
--   * not unique — two men in one village are both "Bohouš", and that is normal
--     rather than a data-entry error;
--   * not length-checked — name carries no CHECK either, and one consistent rule
--     beats a guessed limit;
--   * not a slug source. The slug is derived from name alone, is UNIQUE and
--     appears in URLs; re-deriving it from a nickname would break every bookmark
--     and every subject link already written into the audit trail;
--   * not a list. One nickname is what the archive needs; notes is free text
--     already, and a second table on speculation would cost every read path a
--     join for nothing.

ALTER TABLE subjects
    ADD COLUMN nickname TEXT NOT NULL DEFAULT '';

COMMENT ON COLUMN subjects.nickname IS
    'What people actually call the subject; empty when unknown. Searchable like name, never part of the slug.';
