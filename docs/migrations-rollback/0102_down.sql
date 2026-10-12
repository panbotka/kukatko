-- 0102_down: drop project_id from the root tables and restore the
-- instance-wide keys and the one-row settings/banner.
--
-- Refuses while a second project exists: dropping project_id would merge its
-- rows into the one library the previous release knows, and the instance-wide
-- UNIQUE keys below could not even be rebuilt.
DO $guard$
BEGIN
    IF EXISTS (SELECT 1 FROM projects WHERE uid <> 'pj0000000000000000veselice') THEN
        RAISE EXCEPTION '0102 down: a project other than the bootstrap project exists; refusing';
    END IF;
END
$guard$;

-- The per-project keys and indexes go first (dropping the column would take
-- most of them along, but saying so is clearer).
ALTER TABLE photos DROP CONSTRAINT IF EXISTS photos_project_file_hash_key;
DROP INDEX IF EXISTS idx_photos_file_hash;
ALTER TABLE subjects DROP CONSTRAINT IF EXISTS subjects_project_slug_key;
ALTER TABLE albums DROP CONSTRAINT IF EXISTS albums_project_slug_key;
ALTER TABLE labels DROP CONSTRAINT IF EXISTS labels_project_slug_key;
ALTER TABLE search_history DROP CONSTRAINT IF EXISTS search_history_pkey;
ALTER TABLE photoprism_aliases DROP CONSTRAINT IF EXISTS photoprism_aliases_pkey;
ALTER TABLE instance_settings DROP CONSTRAINT IF EXISTS instance_settings_pkey;
ALTER TABLE announcements DROP CONSTRAINT IF EXISTS announcements_pkey;
DROP INDEX IF EXISTS idx_import_runs_watermark;
DROP INDEX IF EXISTS idx_photos_project_live_taken_at;
DROP INDEX IF EXISTS idx_photos_project_live_created_at;
DROP INDEX IF EXISTS idx_subjects_project_created_at;
DROP INDEX IF EXISTS idx_albums_project_created_at;
DROP INDEX IF EXISTS idx_face_clusters_project_ready;
DROP INDEX IF EXISTS idx_subject_families_project;
DROP INDEX IF EXISTS idx_photo_tasks_project_state;
DROP INDEX IF EXISTS idx_upload_links_project_creator;
DROP INDEX IF EXISTS idx_saved_searches_project_owner_uid;
DROP INDEX IF EXISTS idx_notifications_project_inbox;
DROP INDEX IF EXISTS idx_embeddings_project;
DROP INDEX IF EXISTS idx_faces_project;
DROP INDEX IF EXISTS idx_search_history_project_recent;
DROP INDEX IF EXISTS idx_photoprism_aliases_project_photo_uid;
DROP INDEX IF EXISTS idx_audit_log_project_created_at;

ALTER TABLE photos DROP COLUMN IF EXISTS project_id;
ALTER TABLE subjects DROP COLUMN IF EXISTS project_id;
ALTER TABLE albums DROP COLUMN IF EXISTS project_id;
ALTER TABLE labels DROP COLUMN IF EXISTS project_id;
ALTER TABLE face_clusters DROP COLUMN IF EXISTS project_id;
ALTER TABLE subject_families DROP COLUMN IF EXISTS project_id;
ALTER TABLE photo_tasks DROP COLUMN IF EXISTS project_id;
ALTER TABLE upload_links DROP COLUMN IF EXISTS project_id;
ALTER TABLE import_runs DROP COLUMN IF EXISTS project_id;
ALTER TABLE saved_searches DROP COLUMN IF EXISTS project_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS project_id;
ALTER TABLE embeddings DROP COLUMN IF EXISTS project_id;
ALTER TABLE faces DROP COLUMN IF EXISTS project_id;
ALTER TABLE search_history DROP COLUMN IF EXISTS project_id;
ALTER TABLE instance_settings DROP COLUMN IF EXISTS project_id;
ALTER TABLE announcements DROP COLUMN IF EXISTS project_id;
ALTER TABLE photoprism_aliases DROP COLUMN IF EXISTS project_id;
ALTER TABLE audit_log DROP COLUMN IF EXISTS project_id;

-- The instance-wide keys of 0099.
ALTER TABLE photos ADD CONSTRAINT photos_file_hash_key UNIQUE (file_hash);
ALTER TABLE subjects ADD CONSTRAINT subjects_slug_key UNIQUE (slug);
ALTER TABLE albums ADD CONSTRAINT albums_slug_key UNIQUE (slug);
ALTER TABLE labels ADD CONSTRAINT labels_slug_key UNIQUE (slug);
ALTER TABLE search_history ADD CONSTRAINT search_history_pkey PRIMARY KEY (user_uid, query);
ALTER TABLE photoprism_aliases ADD CONSTRAINT photoprism_aliases_pkey PRIMARY KEY (photoprism_uid);
CREATE INDEX idx_import_runs_watermark ON import_runs (source, finished_at DESC)
    WHERE status = 'done' AND high_watermark IS NOT NULL;

-- One row pinned by id = true again (the column comes back last in the row; no
-- query of the previous release depends on column order).
ALTER TABLE instance_settings ADD COLUMN id BOOLEAN NOT NULL DEFAULT true
    CONSTRAINT instance_settings_id_check CHECK (id);
ALTER TABLE instance_settings ADD CONSTRAINT instance_settings_pkey PRIMARY KEY (id);
ALTER TABLE announcements ADD COLUMN id BOOLEAN NOT NULL DEFAULT true
    CONSTRAINT announcements_id_check CHECK (id);
ALTER TABLE announcements ADD CONSTRAINT announcements_pkey PRIMARY KEY (id);

DELETE FROM schema_migrations WHERE version = 102;
