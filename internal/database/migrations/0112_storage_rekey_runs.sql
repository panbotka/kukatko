-- 0112_storage_rekey_runs: the bookkeeping of a project's storage move.
--
-- A project created before projects existed keeps its objects at the bucket
-- root (storage_prefix = ''); the storage move re-keys every one of them under
-- projects/<uid>/, one photo at a time while the application runs. One row here
-- is one such move: its state, its totals and progress, the cursor it resumes
-- from, and the last errors it met. internal/rekey owns the table.
--
-- state: running (the worker steps it), paused (an operator stopped it; resume
-- puts it back), done (no photo of the project is left in the old layout) or
-- failed (too many errors, or photos the pass could not move; retry puts it
-- back). At most one run of a project is running or paused at a time.
--
-- last_key is the file_path cursor of the pass: photos are moved in file_path
-- order, so a resumed run continues after the last photo it finished.
-- pending_photo_uid/pending_old_key name the one photo whose row already points
-- at its new key but whose old objects are not deleted yet; they are written in
-- the transaction that moves the row and cleared once the old objects are gone,
-- so a crash between the two is finished by the next step instead of leaving
-- the old objects behind unnoticed.
--
-- errors is a ring of the last 50 {key, message, at}; error_count counts every
-- error the run ever met (past 100 the run fails).
--
-- It is preserved by the guarded library wipe: it describes the store's layout,
-- not the photographs. Deleting the project cascades.

CREATE TABLE IF NOT EXISTS storage_rekey_runs (
    uid               VARCHAR(32) PRIMARY KEY,
    project_uid       TEXT        NOT NULL REFERENCES projects (uid) ON DELETE CASCADE,
    state             TEXT        NOT NULL CHECK (state IN ('running', 'paused', 'done', 'failed')),
    target_prefix     TEXT        NOT NULL CHECK (target_prefix <> ''),
    total_photos      BIGINT      NOT NULL DEFAULT 0 CHECK (total_photos >= 0),
    done_photos       BIGINT      NOT NULL DEFAULT 0 CHECK (done_photos >= 0),
    total_bytes       BIGINT      NOT NULL DEFAULT 0 CHECK (total_bytes >= 0),
    done_bytes        BIGINT      NOT NULL DEFAULT 0 CHECK (done_bytes >= 0),
    last_key          TEXT        NOT NULL DEFAULT '',
    pending_photo_uid TEXT        NOT NULL DEFAULT '',
    pending_old_key   TEXT        NOT NULL DEFAULT '',
    errors            JSONB       NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(errors) = 'array'),
    error_count       INTEGER     NOT NULL DEFAULT 0 CHECK (error_count >= 0),
    started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at       TIMESTAMPTZ NULL
);

COMMENT ON TABLE storage_rekey_runs IS
    'One storage move of a project from the bucket root to projects/<uid>/: state, progress, cursor and last errors.';

-- One active (running or paused) run per project.
CREATE UNIQUE INDEX IF NOT EXISTS idx_storage_rekey_runs_active
    ON storage_rekey_runs (project_uid) WHERE state IN ('running', 'paused');

-- A project's runs, newest first (Status).
CREATE INDEX IF NOT EXISTS idx_storage_rekey_runs_project
    ON storage_rekey_runs (project_uid, started_at DESC);

-- The move walks one project's photos in file_path order from a cursor.
-- photos holds ~21k rows in production, so a plain build is momentary.
CREATE INDEX IF NOT EXISTS idx_photos_project_file_path ON photos (project_id, file_path);
