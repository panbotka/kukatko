-- 0110_users_drop_transitional_columns: the per-library state lives only on
-- the membership now.
--
-- Migration 0101 copied the account's approval, its "this is me" link, the two
-- what's-new visit clocks and the task digest watermark onto project_members,
-- one row per (project, account), and left the users columns in place so a
-- rollback stayed possible while the readers moved one step at a time. Every
-- reader and writer now uses the membership: sign-in asks whether the account
-- holds an approved membership, person:me and the profile picture read the
-- membership of the request's project, the visit clocks and the digest have
-- been per project since 0107. So the transitional copies go.
--
-- Nothing is copied first, on purpose: the membership has been the source of
-- truth for every one of these values since its readers moved, and a "final
-- sync" from the stale users copy could only resurrect a link or an approval a
-- project admin has since taken back. The values themselves are not lost: the
-- hand-run docs/migrations-rollback/0110_down.sql rebuilds the columns from
-- the memberships for a release that still reads them.
--
-- Dropping subject_uid takes its foreign key (users_subject_uid_fkey) and its
-- index (idx_users_subject_uid) with it. IF EXISTS keeps a re-run harmless.
ALTER TABLE users
    DROP COLUMN IF EXISTS approved_at,
    DROP COLUMN IF EXISTS subject_uid,
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS visit_reference_at,
    DROP COLUMN IF EXISTS task_digest_at;
