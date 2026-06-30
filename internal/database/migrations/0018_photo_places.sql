-- 0018_photo_places: cached reverse-geocoded place hierarchy per photo.
--
-- A side table keyed by photo_uid (rather than columns on the already-wide photos
-- table) because place data is sparse — only geotagged photos ever get a row —
-- and is a derived, regenerable cache filled asynchronously by the `places` job,
-- so it belongs next to the catalogue, not inside it. This mirrors the
-- face_detections and user_ratings side tables.
--
-- The job stamps the lat/lng the geocode was computed from so a later coordinate
-- edit re-geocodes (the handler skips only when a row exists AND its coordinates
-- still match the photo's). A row with NULL lat/lng marks a photo without GPS as
-- processed, so the job never retries it. The foreign key cascades on photo
-- deletion, so a purged photo drops its place row rather than leaving an orphan.
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE photo_places (
    photo_uid   VARCHAR(32) PRIMARY KEY REFERENCES photos (uid) ON DELETE CASCADE,
    -- Place hierarchy from least to most specific; place_name is the most
    -- specific label (the geocoded point's own name). Empty string when the
    -- geocoder had no value for that level (or the photo has no GPS).
    country     TEXT        NOT NULL DEFAULT '',
    region      TEXT        NOT NULL DEFAULT '',
    city        TEXT        NOT NULL DEFAULT '',
    place_name  TEXT        NOT NULL DEFAULT '',
    -- Coordinates the geocode was computed from, so a later coordinate change can
    -- be detected and re-geocoded. NULL for a photo without GPS, whose row exists
    -- only to record it as processed.
    lat         DOUBLE PRECISION,
    lng         DOUBLE PRECISION,
    geocoded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Grouping/filtering the library by location hits country and city; index both.
CREATE INDEX idx_photo_places_country ON photo_places (country);
CREATE INDEX idx_photo_places_city ON photo_places (city);
