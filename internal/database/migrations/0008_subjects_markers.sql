-- 0008_subjects_markers: named subjects (people/pets/other) and the markers that
-- tie photo regions to them.
--
-- A subject is a named entity — a person, a pet, or something else worth
-- grouping photos by. A marker is a rectangular region on one photo (a detected
-- face, or a manually drawn label box) that may be assigned to a subject. The
-- region uses normalised [x, y, w, h] coordinates in 0..1 display space, the same
-- convention as faces.bbox, so it is independent of the rendered image size.
--
-- The faces table (migration 0006) caches marker_uid/subject_uid/subject_name for
-- fast rendering; the Go internal/people layer keeps those denormalised columns in
-- step whenever a marker's subject changes. subject_uid on markers is ON DELETE
-- SET NULL so deleting a subject orphans its markers rather than dropping the
-- regions; markers.photo_uid is ON DELETE CASCADE so deleting a photo removes its
-- markers. subjects.cover_photo_uid is ON DELETE SET NULL so a subject survives the
-- deletion of its cover photo.

CREATE TABLE subjects (
    uid             VARCHAR(32)  PRIMARY KEY,
    -- slug is a URL-safe, diacritics-stripped form of name, made unique by the
    -- application (a numeric suffix is appended on collision).
    slug            TEXT         NOT NULL UNIQUE,
    name            TEXT         NOT NULL DEFAULT '',
    type            TEXT         NOT NULL DEFAULT 'person'
                        CHECK (type IN ('person', 'pet', 'other')),
    favorite        BOOLEAN      NOT NULL DEFAULT FALSE,
    private         BOOLEAN      NOT NULL DEFAULT FALSE,
    notes           TEXT         NOT NULL DEFAULT '',
    -- Representative photo for the subject's page; cleared if that photo is deleted.
    cover_photo_uid VARCHAR(32)  REFERENCES photos (uid) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE markers (
    uid         VARCHAR(32)  PRIMARY KEY,
    photo_uid   VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- The assigned subject, NULL until the marker is named; a deleted subject
    -- leaves the marker in place with subject_uid reset to NULL.
    subject_uid VARCHAR(32)  REFERENCES subjects (uid) ON DELETE SET NULL,
    type        TEXT         NOT NULL DEFAULT 'face'
                    CHECK (type IN ('face', 'label')),
    -- Normalised bounding box in 0..1 display space (EXIF-aware), matching
    -- faces.bbox. Stored as four columns rather than an array for easy querying.
    x           DOUBLE PRECISION NOT NULL DEFAULT 0,
    y           DOUBLE PRECISION NOT NULL DEFAULT 0,
    w           DOUBLE PRECISION NOT NULL DEFAULT 0,
    h           DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- Detector / matcher confidence as an integer percentage (0..100).
    score       INTEGER      NOT NULL DEFAULT 0,
    -- invalid marks a region a user rejected (e.g. a false-positive face);
    -- reviewed marks one a user has confirmed.
    invalid     BOOLEAN      NOT NULL DEFAULT FALSE,
    reviewed    BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Listing every marker of a photo, and every marker of a subject, are the two
-- hot lookups.
CREATE INDEX idx_markers_photo_uid ON markers (photo_uid);
CREATE INDEX idx_markers_subject_uid ON markers (subject_uid);
