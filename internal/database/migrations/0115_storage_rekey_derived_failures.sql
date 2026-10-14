-- 0115_storage_rekey_derived_failures: the photos whose derived objects a
-- storage move could not move.
--
-- The storage move (internal/rekey) moves a photo's original first and its
-- derived objects — thumbnails and streaming segments — after the row already
-- points at the new key. A derived copy that fails there (a throttled or
-- erroring object store) cannot undo the photo: the original is safe under the
-- new prefix and the photo counts as moved. Until this table, the failure only
-- reached the run's capped error ring, the photo's derived objects stayed at the
-- root where nothing would look for them again, and the signed thumbnail and
-- segment URLs pointed at keys that did not exist.
--
-- One row is one such photo: which derived kinds are still at the root
-- ('thumbnail', 'hls'), the last error, and how often it was tried. Every pass
-- of the move re-plans the derived objects of these photos, copies what is
-- missing under the prefix, deletes the root copies only once the prefixed ones
-- exist, and deletes the row on success. A move ends done only once no row of
-- its project is left. The move's final sweep also adds a row for any moved
-- photo whose derived objects a store listing still finds at the root.
--
-- Rows go with their photo (and their project). The guarded library wipe
-- empties it with the catalogue.

CREATE TABLE IF NOT EXISTS storage_rekey_derived_failures (
    photo_uid   VARCHAR(32) PRIMARY KEY REFERENCES photos (uid) ON DELETE CASCADE,
    project_uid TEXT        NOT NULL REFERENCES projects (uid) ON DELETE CASCADE,
    kinds       TEXT[]      NOT NULL DEFAULT '{}',
    message     TEXT        NOT NULL DEFAULT '',
    attempts    INTEGER     NOT NULL DEFAULT 1 CHECK (attempts >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE storage_rekey_derived_failures IS
    'Moved photos whose thumbnails/streaming segments a storage move left at the root; retried by every pass.';

-- A project's rows in photo_uid order: the cursor of the retry pass and the
-- run's derived_failed count.
CREATE INDEX IF NOT EXISTS idx_storage_rekey_derived_failures_project
    ON storage_rekey_derived_failures (project_uid, photo_uid);

-- The cursor of the derived-retry pass, beside the original pass's last_key.
ALTER TABLE storage_rekey_runs ADD COLUMN IF NOT EXISTS derived_cursor TEXT NOT NULL DEFAULT '';
