-- 0100_projects: the project — one independent library — and the bootstrap row.
--
-- Kukátko is becoming multi-tenant: one instance will host several projects,
-- each a whole library of its own (photos, people, albums, labels, tasks,
-- settings) belonging to tenants that must never learn about each other. This
-- is the first step: the table and the one project everything that exists
-- today already belongs to. Nothing points at it yet; the project_id columns,
-- the membership and the routing come in later migrations.
--
-- The slug is the project's URL segment (/p/<slug>/…), so it is immutable once
-- created and restricted to what reads well in a path. storage_prefix is the key
-- prefix of everything the project stores in the object store: '' (the root
-- layout) only for the bootstrap project until its storage is moved, and
-- 'projects/<uid>/' for every project created afterwards. The two limits are
-- NULL for "no limit".

CREATE TABLE IF NOT EXISTS projects (
    uid             TEXT PRIMARY KEY,
    slug            TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,39}$'),
    name            TEXT NOT NULL CHECK (name <> ''),
    description     TEXT NOT NULL DEFAULT '',
    active          BOOLEAN NOT NULL DEFAULT true,
    storage_prefix  TEXT NOT NULL DEFAULT ''
        CHECK (storage_prefix = '' OR (storage_prefix ~ '/$' AND storage_prefix !~ '^/')),
    max_file_bytes  BIGINT NULL CHECK (max_file_bytes IS NULL OR max_file_bytes > 0),
    max_total_bytes BIGINT NULL CHECK (max_total_bytes IS NULL OR max_total_bytes > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE projects IS
    'One independent library (tenant) of the instance. The bootstrap row pj0000000000000000veselice holds everything that existed before multi-tenancy.';

-- The bootstrap project: the library as it is today. Its uid is fixed (and
-- mirrored by project.BootstrapUID) so every later project_id column can carry it
-- as a transitional DEFAULT. ON CONFLICT keeps a re-run harmless.
INSERT INTO projects (uid, slug, name, description, active, storage_prefix)
VALUES ('pj0000000000000000veselice', 'v', 'Veselice', '', true, '')
ON CONFLICT (uid) DO NOTHING;
