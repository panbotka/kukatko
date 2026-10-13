-- 0108_down: put back the library-wide dedup indexes of 0096 / 0105 / 0106.
--
-- 0096's notification_purge index keyed on the type alone; 0105 and 0106 keyed
-- face_cluster and family_export on the payload's project_uid only. With one
-- project the old keys admit exactly what the new ones do, so the rebuild
-- cannot meet a duplicate.
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
