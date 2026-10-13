-- 0114_project_id_drop_defaults: an insert without a project fails loudly.
--
-- Migration 0102 gave every project_id column a transitional DEFAULT of the
-- bootstrap project `v`, so the writers that had not yet been taught the
-- project kept working while the stores moved one package at a time. Every
-- writer now names its project explicitly, so the safety net goes: an insert
-- that forgets project_id is a NOT NULL violation instead of a row silently
-- landing in `v`.
--
-- Only the DEFAULT goes. NOT NULL and the foreign key to projects (ON DELETE
-- CASCADE) stay exactly as 0102 made them. audit_log.project_id never had a
-- default (an instance-level entry has no project) and is not touched.
--
-- Metadata-only on every table; DROP DEFAULT on a column without one is a
-- no-op, so a re-run is harmless. The hand-run
-- docs/migrations-rollback/0114_down.sql puts the defaults back for a release
-- that still relies on them.
ALTER TABLE photos ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE subjects ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE albums ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE labels ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE face_clusters ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE subject_families ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE photo_tasks ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE upload_links ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE import_runs ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE saved_searches ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE notifications ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE embeddings ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE faces ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE search_history ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE instance_settings ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE announcements ALTER COLUMN project_id DROP DEFAULT;
ALTER TABLE photoprism_aliases ALTER COLUMN project_id DROP DEFAULT;
