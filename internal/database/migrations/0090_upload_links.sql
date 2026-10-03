-- 0090_upload_links: short links anybody can upload event photos through.
--
-- After an event everybody has a few photographs on their phone. A curator
-- creates one link, posts it to the group chat, and whoever opens it — signed
-- in or not — uploads straight into the preset albums and labels.
-- internal/uploadlink owns all four tables.
--
-- upload_links is one link. The short code in the URL is the whole credential,
-- so it is stored only as its SHA-256 (code_hash), exactly like an API token's
-- secret or a password-reset token: a leaked listing cannot be replayed, and
-- the plaintext exists only in the creator's create response. The creator is
-- SET NULL, not CASCADE — a link somebody posted outlives the account that
-- made it, and the photos it brought in certainly do. revoked_at is nullable
-- (a live link has no such moment) and final; expires_at is what "extend"
-- moves. upload_count and last_used_at are the counters the management list
-- shows, bumped in the transaction that records each upload.
--
-- upload_link_albums / upload_link_labels are where a link's photos go. Both
-- cascade from either side: a deleted album simply stops being a target.
--
-- upload_link_photos is the provenance of every photo that came in through a
-- link: which link, the name the uploader typed (optional, free text), the
-- account when the uploader was signed in, and session_hash — the SHA-256 of
-- the anonymous uploader's browser-session cookie, which is what lets a person
-- who registers afterwards claim the photos they just uploaded. outcome tells a
-- freshly created photo from a duplicate the link merely filed into its
-- targets; only a created one may be attributed to a new account. The pair is
-- the primary key, so a photo uploaded twice through one link is one row.
--
-- All four are catalogue tables for the guarded library wipe: they reference
-- albums, labels and photos, which the wipe truncates.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE upload_links (
    uid          VARCHAR(32) PRIMARY KEY,
    code_hash    TEXT        NOT NULL UNIQUE,
    title        TEXT        NOT NULL DEFAULT '',
    note         TEXT        NOT NULL DEFAULT '',
    created_by   VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    upload_count INTEGER     NOT NULL DEFAULT 0 CHECK (upload_count >= 0),
    last_used_at TIMESTAMPTZ
);

COMMENT ON TABLE upload_links IS
    'A short link anybody may upload photos through into preset albums/labels. The code is stored hashed only.';

-- A creator's own links, newest first (the management list).
CREATE INDEX idx_upload_links_creator ON upload_links (created_by, created_at DESC);

CREATE TABLE upload_link_albums (
    link_uid  VARCHAR(32) NOT NULL REFERENCES upload_links (uid) ON DELETE CASCADE,
    album_uid VARCHAR(32) NOT NULL REFERENCES albums (uid) ON DELETE CASCADE,
    PRIMARY KEY (link_uid, album_uid)
);

CREATE TABLE upload_link_labels (
    link_uid  VARCHAR(32) NOT NULL REFERENCES upload_links (uid) ON DELETE CASCADE,
    label_uid VARCHAR(32) NOT NULL REFERENCES labels (uid) ON DELETE CASCADE,
    PRIMARY KEY (link_uid, label_uid)
);

CREATE TABLE upload_link_photos (
    link_uid      VARCHAR(32) NOT NULL REFERENCES upload_links (uid) ON DELETE CASCADE,
    photo_uid     VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    outcome       TEXT        NOT NULL CHECK (outcome IN ('created', 'duplicate')),
    uploader_name TEXT        NOT NULL DEFAULT '',
    uploaded_by   VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    session_hash  TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (link_uid, photo_uid)
);

COMMENT ON TABLE upload_link_photos IS
    'Provenance of every photo uploaded through an upload link: which link, the typed uploader name, the account or the anonymous session.';

-- A photo's provenance (sidecar export) and the cascade from photos.
CREATE INDEX idx_upload_link_photos_photo ON upload_link_photos (photo_uid, created_at);
-- Claiming an anonymous session's photos at registration.
CREATE INDEX idx_upload_link_photos_session ON upload_link_photos (session_hash) WHERE session_hash <> '';
