-- 0011_albums_labels_favorites: organisation schema — albums, labels and the
-- per-user favorites that replace photo-sorter's global photos.favorite flag.
--
-- An album groups photos under a title; album_photos is its ordered membership
-- (sort_order positions the photos, added_at records when each was added). A
-- label is a tag; photo_labels attaches labels to photos with a provenance
-- (source) and an uncertainty score. user_favorites makes "favorite" per-user
-- rather than a single global flag on the photo.
--
-- Foreign keys keep the graph clean: album/label membership and favorites use
-- ON DELETE CASCADE so deleting a photo, label, album or user removes the join
-- rows rather than leaving orphans; albums.cover_photo_uid and albums.created_by
-- are ON DELETE SET NULL so an album survives the deletion of its cover photo or
-- its creator. This migration is wrapped in a transaction by the runner.

CREATE TABLE albums (
    uid             VARCHAR(32)  PRIMARY KEY,
    -- slug is a URL-safe, diacritics-stripped form of title, made unique by the
    -- application (a numeric suffix is appended on collision).
    slug            TEXT         NOT NULL UNIQUE,
    title           TEXT         NOT NULL DEFAULT '',
    description     TEXT         NOT NULL DEFAULT '',
    -- album: a hand-curated set; folder: an import/path grouping; moment, state
    -- and month are auto-generated time/place groupings (mirrors PhotoPrism).
    type            TEXT         NOT NULL DEFAULT 'album'
                        CHECK (type IN ('album', 'folder', 'moment', 'state', 'month')),
    -- Representative photo for the album cover; cleared if that photo is deleted.
    cover_photo_uid VARCHAR(32)  REFERENCES photos (uid) ON DELETE SET NULL,
    private         BOOLEAN      NOT NULL DEFAULT FALSE,
    -- How the album's photos are ordered in the UI (e.g. added, oldest, name);
    -- free-form so new orderings need no migration. Default mirrors manual order.
    order_by        TEXT         NOT NULL DEFAULT 'added',
    -- The user who created the album; the album survives that user's deletion.
    created_by      VARCHAR(32)  REFERENCES users (uid) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE album_photos (
    album_uid  VARCHAR(32)  NOT NULL REFERENCES albums (uid) ON DELETE CASCADE,
    photo_uid  VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- Position of the photo within the album for manual ordering.
    sort_order INTEGER      NOT NULL DEFAULT 0,
    added_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (album_uid, photo_uid)
);

-- Listing a photo's albums (the reverse of album membership) is a hot lookup;
-- the composite primary key already covers album-then-photo access.
CREATE INDEX idx_album_photos_photo_uid ON album_photos (photo_uid);

CREATE TABLE labels (
    uid        VARCHAR(32)  PRIMARY KEY,
    -- slug is a URL-safe, diacritics-stripped form of name, made unique by the
    -- application (a numeric suffix is appended on collision).
    slug       TEXT         NOT NULL UNIQUE,
    name       TEXT         NOT NULL DEFAULT '',
    -- Display/sorting priority; higher floats a label up in the UI.
    priority   INTEGER      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE photo_labels (
    photo_uid   VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    label_uid   VARCHAR(32)  NOT NULL REFERENCES labels (uid) ON DELETE CASCADE,
    -- Where the label came from: a user (manual), automatic classification (ai),
    -- or an import from PhotoPrism / photo-sorter (import).
    source      TEXT         NOT NULL DEFAULT 'manual'
                    CHECK (source IN ('manual', 'ai', 'import')),
    -- Classifier uncertainty as an integer percentage (0 = certain), mirroring
    -- PhotoPrism's label uncertainty; 0 for manual labels.
    uncertainty INTEGER      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (photo_uid, label_uid)
);

-- Listing every photo carrying a label is the reverse of the natural
-- photo-then-label access the primary key already serves.
CREATE INDEX idx_photo_labels_label_uid ON photo_labels (label_uid);

CREATE TABLE user_favorites (
    user_uid  VARCHAR(32)  NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    photo_uid VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    added_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uid, photo_uid)
);

-- Listing who favorited a photo (the reverse of a user's favorites) is a hot
-- lookup; the composite primary key already covers user-then-photo access.
CREATE INDEX idx_user_favorites_photo_uid ON user_favorites (photo_uid);
