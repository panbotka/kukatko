-- 0102_project_id_root_tables: every catalogue root row learns its project.
--
-- Step three of multi-tenancy (docs/superpowers/specs/2026-10-11-multi-tenancy-design.md
-- §2.4). A root table is one whose rows belong to a library directly: photos,
-- people, albums, labels, clusters, families, tasks, upload links, import runs,
-- saved searches, notifications, the per-library settings and banner, the
-- PhotoPrism uid aliases and each user's search history. Child tables (markers,
-- album_photos, comments, photo_task_photos, the rejections, …) are scoped
-- through their foreign key to a root row and get no column. embeddings and
-- faces carry a denormalised copy of their photo's project (kept in sync by the
-- writers in internal/vectors and internal/people) so the filtered HNSW kNN
-- queries can filter without joining photos.
--
-- Transitional: every new column defaults to the bootstrap project, so every
-- INSERT the code issues today keeps working unchanged and lands in the one
-- library that exists. Reads are not scoped here; later steps add the WHERE
-- project_id package by package, and a final step drops the defaults.
--
-- Cost on production (~21k photos, ~21k faces/embeddings): ADD COLUMN with a
-- constant DEFAULT is metadata-only (PostgreSQL ≥ 11); the foreign key check,
-- the index builds and the constraint swaps scan tables of that size in seconds.
-- The HNSW indexes are not touched.
--
-- Idempotent: columns and indexes are IF NOT EXISTS, every constraint is
-- dropped IF EXISTS before it is (re)created, and the backfill only fills NULLs.

-- ---------------------------------------------------------------------------
-- The column on the root tables.
-- ---------------------------------------------------------------------------

ALTER TABLE photos ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE albums ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE labels ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE face_clusters ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE subject_families ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE photo_tasks ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE upload_links ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE import_runs ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE saved_searches ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE embeddings ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE faces ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE search_history ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE instance_settings ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;
ALTER TABLE photoprism_aliases ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL
    DEFAULT 'pj0000000000000000veselice' REFERENCES projects (uid) ON DELETE CASCADE;

-- The audit trail is instance-wide: an account, session or maintenance action
-- happens in no project, so the column is nullable and has no foreign key (an
-- entry must outlive the project it names).
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS project_id TEXT NULL;

-- Belt and braces: ADD COLUMN … NOT NULL DEFAULT already filled every existing
-- row, so these update nothing. They make the backfill explicit and survive a
-- hand-made column that was added without the default.
UPDATE photos SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE subjects SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE albums SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE labels SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE face_clusters SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE subject_families SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE photo_tasks SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE upload_links SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE import_runs SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE saved_searches SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE notifications SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE embeddings SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE faces SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE search_history SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE instance_settings SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE announcements SET project_id = DEFAULT WHERE project_id IS NULL;
UPDATE photoprism_aliases SET project_id = DEFAULT WHERE project_id IS NULL;

-- ---------------------------------------------------------------------------
-- Keys: what was unique in the instance becomes unique within a project.
-- ---------------------------------------------------------------------------

-- The same file may live in two libraries; within one it is still stored once.
-- The constraint name keeps "file_hash" in it: internal/photos maps a violation
-- to ErrFileHashTaken by that substring.
ALTER TABLE photos DROP CONSTRAINT IF EXISTS photos_file_hash_key;
ALTER TABLE photos DROP CONSTRAINT IF EXISTS photos_project_file_hash_key;
ALTER TABLE photos ADD CONSTRAINT photos_project_file_hash_key UNIQUE (project_id, file_hash);
-- Transitional: today's dedup and maintenance reads look a photo up by file_hash
-- alone, which the composite key above cannot serve. The step that scopes those
-- reads by project drops this index.
CREATE INDEX IF NOT EXISTS idx_photos_file_hash ON photos (file_hash);

-- Slugs are URL segments inside /p/<slug>/…, so two libraries may each have an
-- "anna". The names keep "slug" in them: internal/people and internal/organize
-- retry a colliding slug by that substring.
ALTER TABLE subjects DROP CONSTRAINT IF EXISTS subjects_slug_key;
ALTER TABLE subjects DROP CONSTRAINT IF EXISTS subjects_project_slug_key;
ALTER TABLE subjects ADD CONSTRAINT subjects_project_slug_key UNIQUE (project_id, slug);
ALTER TABLE albums DROP CONSTRAINT IF EXISTS albums_slug_key;
ALTER TABLE albums DROP CONSTRAINT IF EXISTS albums_project_slug_key;
ALTER TABLE albums ADD CONSTRAINT albums_project_slug_key UNIQUE (project_id, slug);
ALTER TABLE labels DROP CONSTRAINT IF EXISTS labels_slug_key;
ALTER TABLE labels DROP CONSTRAINT IF EXISTS labels_project_slug_key;
ALTER TABLE labels ADD CONSTRAINT labels_project_slug_key UNIQUE (project_id, slug);

-- One recent-search ring per user per library.
ALTER TABLE search_history DROP CONSTRAINT IF EXISTS search_history_pkey;
ALTER TABLE search_history ADD CONSTRAINT search_history_pkey PRIMARY KEY (user_uid, project_id, query);

-- A PhotoPrism uid is unique within the library it was imported into.
ALTER TABLE photoprism_aliases DROP CONSTRAINT IF EXISTS photoprism_aliases_pkey;
ALTER TABLE photoprism_aliases ADD CONSTRAINT photoprism_aliases_pkey PRIMARY KEY (project_id, photoprism_uid);

-- The settings and the banner were one row pinned by id = true; they become one
-- row per project. The existing row already carries the bootstrap project from
-- the column default above, so it simply becomes that project's row. Dropping
-- the id column drops its CHECK (id) with it.
ALTER TABLE instance_settings DROP CONSTRAINT IF EXISTS instance_settings_pkey;
ALTER TABLE instance_settings DROP COLUMN IF EXISTS id;
ALTER TABLE instance_settings ADD CONSTRAINT instance_settings_pkey PRIMARY KEY (project_id);
ALTER TABLE announcements DROP CONSTRAINT IF EXISTS announcements_pkey;
ALTER TABLE announcements DROP COLUMN IF EXISTS id;
ALTER TABLE announcements ADD CONSTRAINT announcements_pkey PRIMARY KEY (project_id);

-- "Latest finished run per source" (the incremental import watermark) is per
-- library.
DROP INDEX IF EXISTS idx_import_runs_watermark;
CREATE INDEX IF NOT EXISTS idx_import_runs_watermark
    ON import_runs (project_id, source, finished_at DESC)
    WHERE status = 'done' AND high_watermark IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Indexes with project_id leading. Each copies the table's existing listing
-- index (same ordering, same predicate) with the project in front, so a scoped
-- listing walks one library's slice; a table without a listing index gets a
-- plain one. Every one also serves the ON DELETE CASCADE from projects.
-- ---------------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_photos_project_live_taken_at
    ON photos (project_id, taken_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_photos_project_live_created_at
    ON photos (project_id, created_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_subjects_project_created_at
    ON subjects (project_id, created_at DESC) WHERE name <> '';
CREATE INDEX IF NOT EXISTS idx_albums_project_created_at
    ON albums (project_id, created_at DESC) WHERE type = 'album';
CREATE INDEX IF NOT EXISTS idx_face_clusters_project_ready
    ON face_clusters (project_id, created_at DESC, uid) WHERE summary IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_subject_families_project ON subject_families (project_id);
CREATE INDEX IF NOT EXISTS idx_photo_tasks_project_state
    ON photo_tasks (project_id, state, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_upload_links_project_creator
    ON upload_links (project_id, created_by, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_saved_searches_project_owner_uid
    ON saved_searches (project_id, owner_uid);
CREATE INDEX IF NOT EXISTS idx_notifications_project_inbox
    ON notifications (project_id, user_uid, created_at DESC, uid) WHERE in_app;
CREATE INDEX IF NOT EXISTS idx_embeddings_project ON embeddings (project_id);
CREATE INDEX IF NOT EXISTS idx_faces_project ON faces (project_id);
CREATE INDEX IF NOT EXISTS idx_search_history_project_recent
    ON search_history (user_uid, project_id, searched_at DESC);
CREATE INDEX IF NOT EXISTS idx_photoprism_aliases_project_photo_uid
    ON photoprism_aliases (project_id, photo_uid);
CREATE INDEX IF NOT EXISTS idx_audit_log_project_created_at
    ON audit_log (project_id, created_at DESC) WHERE project_id IS NOT NULL;
