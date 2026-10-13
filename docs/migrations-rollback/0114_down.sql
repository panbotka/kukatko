-- 0114_down: put back the transitional DEFAULT 'v' on every project_id column,
-- for a release older than 0114 whose writers may still rely on it. Harmless
-- on a library that holds more projects: a default only fills an insert that
-- names none, which this schema's own release never issues.
ALTER TABLE photos ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE subjects ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE albums ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE labels ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE face_clusters ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE subject_families ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE photo_tasks ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE upload_links ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE import_runs ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE saved_searches ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE notifications ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE embeddings ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE faces ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE search_history ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE instance_settings ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE announcements ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
ALTER TABLE photoprism_aliases ALTER COLUMN project_id SET DEFAULT 'pj0000000000000000veselice';
DELETE FROM schema_migrations WHERE version = 114;
