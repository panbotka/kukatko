-- 0029_photos_taken_at_estimate: an approximate ("circa") capture date for the
-- scanned and inherited photos whose real date nobody knows.
--
-- Until now a photo had exactly one capture time (taken_at) and its provenance
-- (taken_at_source: exif/filename/manual/unknown), which left two bad options for
-- a photo that is "somewhere in the forties": type a precise date that lies, or
-- leave it empty and drop the photo out of the timeline. Two columns fix that:
--
--   * taken_at_estimated — the date is a guess, not a fact. Presentation and
--     truthfulness only: the UI marks such a date "cca" so it cannot be mistaken
--     for a known one.
--   * taken_at_note — free text in the user's own words about what the estimate
--     rests on ("kolem roku 1950", "za války", "podle babičky před svatbou").
--
-- taken_at keeps its meaning and stays the single anchor for sorting, the
-- timeline, grouping and the date filters — the flag adds no second date axis and
-- changes no ordering, which is why no index is needed here. A NULL taken_at with
-- the flag set is allowed and behaves exactly like any undated photo; the note
-- then carries the whole meaning.
--
-- Both columns mirror the existing text/bool columns of 0003_photos: NOT NULL with
-- a zero default, so the Go model stays a plain bool/string and existing rows
-- simply carry the zero value. The photos.fts generated column is deliberately not
-- rebuilt: the note is a dating remark, not a caption, and folding it in would
-- rewrite the whole table for no search benefit.

ALTER TABLE photos
    ADD COLUMN taken_at_estimated BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN taken_at_note      TEXT    NOT NULL DEFAULT '';
