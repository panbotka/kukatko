-- 0117_down: drop the withheld-identity flag of exact-match member adds.
--
-- Safe at any time: a release older than 0117 neither reads nor writes the
-- column. Its rows stay approved memberships, so the accounts an admin added
-- by an exact match and that have not entered yet become ordinary members of
-- the project — that release shows their names again, as it always did.
ALTER TABLE project_members DROP COLUMN IF EXISTS identity_withheld;

DELETE FROM schema_migrations WHERE version = 117;
