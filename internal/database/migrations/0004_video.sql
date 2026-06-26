-- 0004_video: extend the photos catalogue with media-type and video metadata.
--
-- Kukátko ingests videos (PhotoPrism stores many — plain mp4/mov plus the
-- motion clips of live photos) alongside images, so the central photos row
-- gains a media_type discriminator and a handful of video-only columns probed
-- from the container at ingest time (via ffprobe, falling back to exiftool).
--
-- media_type is one of 'image' (the default, every pre-existing row), 'video'
-- (a standalone clip) or 'live' (a still image whose motion clip is linked as a
-- separate photo_files row). The video columns are NULL/zero for images.
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE photos
    ADD COLUMN media_type  TEXT             NOT NULL DEFAULT 'image'
        CHECK (media_type IN ('image', 'video', 'live')),
    ADD COLUMN duration_ms INTEGER,
    ADD COLUMN video_codec TEXT             NOT NULL DEFAULT '',
    ADD COLUMN audio_codec TEXT             NOT NULL DEFAULT '',
    ADD COLUMN has_audio   BOOLEAN          NOT NULL DEFAULT false,
    ADD COLUMN fps         DOUBLE PRECISION;

-- Partial index: videos are a minority of the catalogue, so index only the rows
-- that are not plain images for the "videos only" library filters and counts.
CREATE INDEX idx_photos_media_type ON photos (media_type) WHERE media_type <> 'image';
