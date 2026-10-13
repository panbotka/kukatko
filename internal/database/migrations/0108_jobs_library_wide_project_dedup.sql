-- 0108_jobs_library_wide_project_dedup: every library-wide job dedupes on
-- (type, project).
--
-- Multi-tenancy step 21 defines the mechanism once: the library-wide job types
-- (face_cluster, family_export, nameless_detach/nameless_restore, task_digest,
-- notification_purge, and the future storage_rekey) carry their project in the
-- payload as project_uid, internal/jobs refuses to enqueue one without it, and
-- their dedup key is (type, payload ->> 'project_uid') plus whatever further
-- identifier the job carries.
--
-- Three indexes are rebuilt to that one shape:
--
--   * notification_purge was still keyed on the type alone (0096), so the first
--     project's queued purge swallowed every other project's — it is now one
--     queued purge per project;
--   * face_cluster (0105) and family_export (0106) were already keyed on the
--     project, but on the payload's project_uid alone (the type living only in
--     the predicate); they gain the type as their leading key so all three
--     read the same.
--
-- The task digest (0107: project + person) and the nameless repair (0105:
-- type + project + subject) already carry the project and stay as they are.
-- tag_notify stays keyed per account (0089) — it is instance-wide, grouping by
-- project inside its run.
--
-- Every predicate stays state = 'queued' only, for the reasons the original
-- migrations give: a running pass enqueues its own successor (face_cluster), an
-- edit landing during a running export must schedule a follow-up
-- (family_export), and a running purge never blocks a fresh one. The index names
-- are kept, so the queue's list of "this work is already scheduled" indexes
-- (internal/jobs dedupIndexes) needs no change.
--
-- Jobs queued before the payload carried a project have NULL there; NULLs are
-- distinct in a unique index, so rebuilding can never fail on existing rows.
-- DROP … IF EXISTS / CREATE make the migration idempotent.
--
-- This migration is wrapped in a transaction by the runner.

DROP INDEX IF EXISTS idx_jobs_notification_purge_dedup;
CREATE UNIQUE INDEX idx_jobs_notification_purge_dedup ON jobs (type, (payload ->> 'project_uid'))
    WHERE type = 'notification_purge' AND state = 'queued';

DROP INDEX IF EXISTS idx_jobs_face_cluster_dedup;
CREATE UNIQUE INDEX idx_jobs_face_cluster_dedup ON jobs (type, (payload ->> 'project_uid'))
    WHERE type = 'face_cluster' AND state = 'queued';

DROP INDEX IF EXISTS idx_jobs_family_export_dedup;
CREATE UNIQUE INDEX idx_jobs_family_export_dedup ON jobs (type, (payload ->> 'project_uid'))
    WHERE type = 'family_export' AND state = 'queued';
