-- 0003_photos: the core photo schema (photos, photo_files, photo_phashes,
-- photo_edits).
--
-- photos is the central catalogue row: one per distinct image/video, keyed by
-- an app-generated VARCHAR(32) uid and deduplicated on file_hash (the SHA256 of
-- the original, 64 hex chars). External-ID columns let Kukátko deduplicate
-- against PhotoPrism (photoprism_uid + the PhotoPrism SHA1 file hash for
-- /dl/:hash downloads) and migrate 1:1 from photo-sorter (photosorter_uid).
-- Mutable text metadata defaults to '' (not NULL) so the Go models stay plain
-- strings; genuinely optional values (timestamps, GPS, EXIF camera numbers, the
-- raw EXIF blob, the uploader FK and the external IDs) are nullable.
--
-- photo_files records the original plus its derivatives (one primary per photo).
-- photo_phashes holds perceptual hashes for near-duplicate detection.
-- photo_edits holds non-destructive adjustments in normalised 0..1 coordinates.
--
-- The full-text tsvector column and the embeddings/faces tables described in
-- docs/ARCHITECTURE.md §5–6 are intentionally deferred to their own migrations
-- (search and face tasks). This migration is wrapped in a transaction by the
-- runner.

CREATE TABLE photos (
    uid                  VARCHAR(32) PRIMARY KEY,
    file_hash            VARCHAR(64) NOT NULL UNIQUE,
    file_path            TEXT        NOT NULL,
    file_name            TEXT        NOT NULL DEFAULT '',
    file_size            BIGINT      NOT NULL DEFAULT 0,
    file_mime            TEXT        NOT NULL DEFAULT '',
    file_width           INTEGER     NOT NULL DEFAULT 0,
    file_height          INTEGER     NOT NULL DEFAULT 0,
    file_orientation     INTEGER     NOT NULL DEFAULT 0,
    taken_at             TIMESTAMPTZ,
    taken_at_source      TEXT        NOT NULL DEFAULT '',
    title                TEXT        NOT NULL DEFAULT '',
    description          TEXT        NOT NULL DEFAULT '',
    notes                TEXT        NOT NULL DEFAULT '',
    lat                  DOUBLE PRECISION,
    lng                  DOUBLE PRECISION,
    altitude             DOUBLE PRECISION,
    camera_make          TEXT        NOT NULL DEFAULT '',
    camera_model         TEXT        NOT NULL DEFAULT '',
    lens_model           TEXT        NOT NULL DEFAULT '',
    iso                  INTEGER,
    aperture             REAL,
    exposure             TEXT        NOT NULL DEFAULT '',
    focal_length         REAL,
    exif                 JSONB,
    private              BOOLEAN     NOT NULL DEFAULT false,
    archived_at          TIMESTAMPTZ,
    uploaded_by          VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    photoprism_uid       VARCHAR(32),
    photoprism_file_hash VARCHAR(40),
    photosorter_uid      VARCHAR(32),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Default timeline ordering is newest-taken first.
CREATE INDEX idx_photos_taken_at ON photos (taken_at DESC);
-- Partial index: archived photos are the minority, so only index those rows.
CREATE INDEX idx_photos_archived_at ON photos (archived_at) WHERE archived_at IS NOT NULL;
-- GIN over the raw EXIF document for key/containment queries.
CREATE INDEX idx_photos_exif ON photos USING gin (exif);
-- External-ID lookups used by the PhotoPrism / photo-sorter import dedup paths.
CREATE INDEX idx_photos_photoprism_uid ON photos (photoprism_uid) WHERE photoprism_uid IS NOT NULL;
CREATE INDEX idx_photos_photosorter_uid ON photos (photosorter_uid) WHERE photosorter_uid IS NOT NULL;

CREATE TABLE photo_files (
    id         BIGSERIAL   PRIMARY KEY,
    photo_uid  VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    file_path  TEXT        NOT NULL,
    file_hash  VARCHAR(64) NOT NULL DEFAULT '',
    file_size  BIGINT      NOT NULL DEFAULT 0,
    file_mime  TEXT        NOT NULL DEFAULT '',
    is_primary BOOLEAN     NOT NULL DEFAULT false,
    role       TEXT        NOT NULL CHECK (role IN ('original', 'sidecar', 'edited')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_photo_files_photo_uid_file_path UNIQUE (photo_uid, file_path)
);

-- Enforce at most one primary file per photo.
CREATE UNIQUE INDEX idx_photo_files_one_primary ON photo_files (photo_uid) WHERE is_primary;

CREATE TABLE photo_phashes (
    photo_uid  VARCHAR(32) PRIMARY KEY REFERENCES photos (uid) ON DELETE CASCADE,
    phash      BIGINT      NOT NULL,
    dhash      BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE photo_edits (
    photo_uid  VARCHAR(32) PRIMARY KEY REFERENCES photos (uid) ON DELETE CASCADE,
    crop_x     REAL,
    crop_y     REAL,
    crop_w     REAL,
    crop_h     REAL,
    rotation   INTEGER     NOT NULL DEFAULT 0 CHECK (rotation IN (0, 90, 180, 270)),
    brightness REAL        NOT NULL DEFAULT 0,
    contrast   REAL        NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Crop is all-or-nothing: either every coordinate is set or none are.
    CONSTRAINT ck_photo_edits_crop_all_or_nothing CHECK (
        (crop_x IS NULL AND crop_y IS NULL AND crop_w IS NULL AND crop_h IS NULL)
        OR (crop_x IS NOT NULL AND crop_y IS NOT NULL AND crop_w IS NOT NULL AND crop_h IS NOT NULL)
    )
);
