-- 0105_down: drop the face tooling's per-project dedup and pending indexes.
--
-- face_cluster never had a dedup index before 0105 (its scheduler checked for a
-- pending pass), and the nameless repair's jobs were not deduplicated in the
-- index either; both are indexes only, no data.
DROP INDEX IF EXISTS idx_jobs_face_cluster_dedup;
DROP INDEX IF EXISTS idx_jobs_nameless_dedup;
DROP INDEX IF EXISTS idx_face_clusters_project_pending;

DELETE FROM schema_migrations WHERE version = 105;
