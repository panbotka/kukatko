-- 0092_photos_thumbnails_at: when a photo's thumbnails were last built.
--
-- The per-photo processing report used to read the `thumbnail` step's state from
-- the photo_phashes row. The upload pipeline writes that row *before* it renders
-- the thumbnails, so an upload whose client hung up in between left a photo
-- reported as `thumbnail: done` with no thumbnail anywhere (CDN 404s, 2026-10-05).
-- thumbnails_at is written only after every size was generated — by the upload
-- pipeline and by the thumbnail job — so it is evidence of the thumbnails
-- themselves rather than of a step that happens to precede them.
--
-- Existing photos inherit their pHash stamp: that is exactly what the report
-- showed for them until now, so nothing flips state on deploy. A photo whose
-- thumbnails went missing before this migration keeps reading as done here; the
-- integrity scan's missing-thumbnail finding (and a re-upload of the file) is
-- what catches those.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE photos ADD COLUMN thumbnails_at TIMESTAMPTZ;

UPDATE photos p
SET thumbnails_at = ph.created_at
FROM photo_phashes ph
WHERE ph.photo_uid = p.uid;
