-- 0108_down: put back the library-wide dedup indexes of 0096 / 0105 / 0106.
--
-- 0096's notification_purge index keyed on the type alone; 0105 and 0106 keyed
-- face_cluster and family_export on the payload's project_uid only. For those
-- two the old key admits exactly what the new one does (the new one only adds
-- the type its WHERE already fixes), so their rebuild cannot meet a duplicate.
--
-- notification_purge is different: 0108 queues one per project, the old key one
-- for the instance, and a project deleted before the rollback (so 0102_down
-- would pass) may still have one queued — the payload's project_uid is no
-- foreign key. Rebuilding over two of them would fail here and leave the chain
-- half-unwound, so every queued purge that is not the bootstrap project's is
-- cancelled first, the way 0107_down cancels digests. Nothing is lost: the purge
-- is a retention sweep its startup and daily scheduler queue again.
DELETE FROM jobs
WHERE type = 'notification_purge' AND state = 'queued'
  AND payload ->> 'project_uid' IS DISTINCT FROM 'pj0000000000000000veselice';
DROP INDEX IF EXISTS idx_jobs_notification_purge_dedup;
CREATE UNIQUE INDEX idx_jobs_notification_purge_dedup ON jobs (type)
    WHERE type = 'notification_purge' AND state = 'queued';

DROP INDEX IF EXISTS idx_jobs_face_cluster_dedup;
CREATE UNIQUE INDEX idx_jobs_face_cluster_dedup ON jobs ((payload ->> 'project_uid'))
    WHERE type = 'face_cluster' AND state = 'queued';

DROP INDEX IF EXISTS idx_jobs_family_export_dedup;
CREATE UNIQUE INDEX idx_jobs_family_export_dedup ON jobs ((payload ->> 'project_uid'))
    WHERE type = 'family_export' AND state = 'queued';

DELETE FROM schema_migrations WHERE version = 108;
