-- 0070_photo_hls_renditions: what the HLS transcode produced for one video.
--
-- A video's segments live in the object store under hls/<file_hash>/<rendition>/
-- (see internal/hls), but the store cannot answer the questions a player's
-- playlists need answered: how big the encoded picture is, how much bandwidth it
-- demands, which codecs it uses, how many segments there are and how long they
-- run to. This table is where the transcode job records exactly that, one row per
-- (photo, rendition).
--
-- playlist holds the media playlist ffmpeg wrote, verbatim. It is not served as
-- it stands — the URIs in it name bare object names, and the .m3u8 a player
-- fetches is rendered per request from this text with those URIs rewritten (see
-- hls.RewriteMedia) — but it is the one thing that cannot be recomputed without
-- re-encoding: ffmpeg measured each segment's real duration while it wrote it,
-- and a playlist whose EXTINF values were synthesised afterwards would drift from
-- the bytes a player receives.
--
-- The primary key is (photo_uid, rendition), which is what makes a re-encode
-- replace a rendition instead of duplicating it: the job upserts on that key.
-- photo_uid CASCADEs, because purging a photo removes its segments from the store
-- and a row describing objects that no longer exist would make the player ask for
-- them.
--
-- rendition is VARCHAR(16) to match hls.ValidateRendition's bound, and the CHECK
-- mirrors its character rule, so a name that skipped the Go layer still cannot
-- become a path segment that climbs out of its video's prefix.
--
-- The numeric CHECKs are all "a player can use this": a zero-pixel picture, a
-- zero bandwidth or a rendition with no segments are not degraded rows to be
-- rendered cautiously, they are rows no master playlist may advertise.

CREATE TABLE photo_hls_renditions (
    photo_uid     VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- The rendition name, verbatim as it appears in the object keys ("1080p").
    rendition     VARCHAR(16) NOT NULL CHECK (rendition ~ '^[a-z0-9]+$'),
    -- The media playlist ffmpeg wrote, with its own relative URIs.
    playlist      TEXT        NOT NULL CHECK (playlist <> ''),
    -- The encoded picture's real dimensions, read off what ffmpeg produced.
    width         INTEGER     NOT NULL CHECK (width > 0),
    height        INTEGER     NOT NULL CHECK (height > 0),
    -- Peak bandwidth in bits per second, as EXT-X-STREAM-INF must advertise it.
    bandwidth     INTEGER     NOT NULL CHECK (bandwidth > 0),
    -- The RFC 6381 codecs string of this rendition's streams.
    codecs        TEXT        NOT NULL CHECK (codecs <> ''),
    -- How many media segments were uploaded, init.mp4 excluded.
    segment_count INTEGER     NOT NULL CHECK (segment_count > 0),
    -- The rendition's total length in milliseconds, summed from the playlist.
    duration_ms   INTEGER     NOT NULL CHECK (duration_ms > 0),
    -- When this rendition was last encoded; a re-encode moves it forward.
    encoded_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (photo_uid, rendition)
);

COMMENT ON TABLE photo_hls_renditions IS
    'One encoded HLS rendition of one video: the media playlist ffmpeg wrote plus what the master playlist advertises.';
