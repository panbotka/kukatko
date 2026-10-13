-- 0106_family_export_per_project: the genealogy export is keyed by project.
--
-- Multi-tenancy step 16. Every project has its own families.yaml (under the
-- project's storage prefix), so a `family_export` job now names the project it
-- rewrites in its payload as `project_uid` — the field name every library-wide
-- job uses. The dedup key has to carry it too: keyed on the type alone (0075),
-- one project's queued rewrite would swallow another project's, and that
-- project's file would silently stop following its tree.
--
-- The predicate stays state = 'queued' only, for the reason 0075 gives: an edit
-- landing while a rewrite is running must schedule a follow-up, because the
-- running job read the genealogy before that edit.
--
-- Jobs queued before this migration carry no project_uid: the expression is
-- NULL for them, NULLs are distinct in a unique index, so rebuilding the index
-- can never fail on existing rows. Their handler reads a missing project as the
-- bootstrap project, the only one that existed when they were queued.
--
-- The index keeps its name, so the queue's list of "this work is already
-- scheduled" indexes (internal/jobs dedupIndexes) needs no change.
--
-- This migration is wrapped in a transaction by the runner.

DROP INDEX IF EXISTS idx_jobs_family_export_dedup;

CREATE UNIQUE INDEX idx_jobs_family_export_dedup ON jobs ((payload ->> 'project_uid'))
    WHERE type = 'family_export' AND state = 'queued';
