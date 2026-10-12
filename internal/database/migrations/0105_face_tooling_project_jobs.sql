-- 0105_face_tooling_project_jobs: the face tooling's library-wide jobs are keyed
-- by project.
--
-- Multi-tenancy step 14. The face-grouping pass (`face_cluster`) and the
-- nameless-subject repair (`nameless_detach` / `nameless_restore`) work on one
-- project at a time, and their payloads now carry `project_uid` (the field name
-- every library-wide job uses). Their dedup key has to carry it too, or one
-- project's queued pass would swallow another project's.
--
-- face_cluster never had a dedup index: its scheduler checked "is a pass
-- pending?" before enqueuing. That check is now per project, and this index is
-- its backstop against two schedulers racing — at most one *queued* pass per
-- project. The predicate is state = 'queued' only, for the reason 0044 and 0089
-- give: a running pass enqueues its own successor (the summary backlog drains in
-- bounded steps), which must not collide with itself.
--
-- The nameless repair queues one job per subject; the key is (type, project,
-- subject), so replaying the same undo file twice while the first replay is
-- still queued collapses instead of stacking a second restore of every subject.
-- The restore payload carries the subject uid at the top level for this index.
--
-- Jobs queued before this migration carry no project_uid: the expression is NULL
-- for them, NULLs are distinct in a unique index, so building the indexes can
-- never fail on existing rows. Their handlers read a missing project as the
-- bootstrap project, the only one that existed when they were queued.
--
-- Also: the summary backlog the `face_cluster` job lists is per project now, so
-- its index leads with project_id (the ready listing got its twin in 0102).
--
-- This migration is wrapped in a transaction by the runner.

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_face_cluster_dedup ON jobs ((payload ->> 'project_uid'))
    WHERE type = 'face_cluster' AND state = 'queued';

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_nameless_dedup
    ON jobs (type, (payload ->> 'project_uid'), (payload ->> 'subject_uid'))
    WHERE type IN ('nameless_detach', 'nameless_restore') AND state = 'queued';

CREATE INDEX IF NOT EXISTS idx_face_clusters_project_pending
    ON face_clusters (project_id, created_at, uid) WHERE summary IS NULL;
