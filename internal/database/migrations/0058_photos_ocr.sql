-- 0058_photos_ocr: the text printed *in* a photo becomes part of the catalogue
-- and part of search.
--
-- The embeddings sidecar gained POST /ocr/image (PP-OCRv5 via RapidOCR, latin
-- model, Czech diacritics included). The `ocr` job runs a photo's fit_1920
-- preview through it and stores the answer here, so a street sign, a shop front,
-- a scanned newspaper headline or a handwritten-caption-in-print becomes
-- findable by typing what it says.
--
-- Three columns, and they are deliberately three rather than one:
--
--   * ocr_text — the recognised text, blocks joined by newlines in reading
--     order. NOT NULL DEFAULT '' like every other text column of 0003_photos, so
--     existing rows simply carry the empty string.
--
--   * ocr_at — when the recogniser last ran. NULL means "never ran", which is
--     the whole point of a separate column: an empty ocr_text alone cannot tell
--     "OCR looked and this photo has no text in it" from "OCR has not seen this
--     photo yet", and without that distinction every backfill would re-enqueue
--     the (large) majority of a family archive forever. It is the same
--     bookkeeping marker metadata_extracted_at is for file metadata (0028).
--
--   * ocr_model — which recogniser produced the text ("PP-OCRv5_mobile"), so a
--     future model swap can be told apart from a photo that genuinely says
--     nothing, exactly as the embeddings' model/pretrained tags do.
--
-- The bounding boxes the service returns are NOT stored. Nothing consumes them
-- yet (there is no text overlay on the photo detail), and a per-block table for
-- a feature nobody asked for is schema nobody has to migrate later.
--
-- The photos.fts generated column (0007_fts, last rebuilt by
-- 0027_photos_iptc_metadata) is rebuilt so ocr_text folds into the search vector
-- at weight D — the lowest — next to file_name. That weight is the ranking
-- statement: a photo whose *title* is "Veselice" must still outrank a photo that
-- merely has a sign reading "Veselice" somewhere in the frame, and D against
-- title's A is what enforces it (ts_rank weights 0.1 vs 1.0). Every existing
-- contribution (title A, description B, subject B, notes C, ai_note C,
-- keywords C, file_name D) is kept exactly as it was, and immutable_unaccent
-- keeps the OCR text diacritics-insensitive like the rest.
--
-- WARNING — this rewrites the whole photos table. PostgreSQL's ALTER COLUMN …
-- SET EXPRESSION recomputes the stored tsvector for every existing row and
-- rebuilds the GIN index (idx_photos_fts) automatically; on the ~20 000-row
-- production library that is seconds, but it is a full rewrite and takes an
-- ACCESS EXCLUSIVE lock for its duration. Adding the three columns is free by
-- comparison (a NOT NULL DEFAULT of a constant is metadata-only since PG 11),
-- and both statements are transaction-safe, so the runner's transaction holds.
--
-- idx_photos_ocr_pending is the partial index the OCR backfill reads, mirroring
-- idx_photos_metadata_pending (0028): it covers exactly the "never OCR'd, not
-- archived, not a video" predicate in the order the backfill lists, so
-- scheduling the remaining work stays an index scan rather than a full pass over
-- the catalogue. Videos are excluded from the index because they are excluded
-- from OCR — no poster-frame recognition. A live photo is not excluded: it is a
-- still image that happens to carry a motion clip, and its still frame is
-- exactly what OCR reads.

ALTER TABLE photos
    ADD COLUMN ocr_text  TEXT NOT NULL DEFAULT '',
    ADD COLUMN ocr_at    TIMESTAMPTZ,
    ADD COLUMN ocr_model TEXT NOT NULL DEFAULT '';

ALTER TABLE photos
    ALTER COLUMN fts SET EXPRESSION AS (
        setweight(to_tsvector('simple', immutable_unaccent(title)), 'A') ||
        setweight(to_tsvector('simple', immutable_unaccent(description)), 'B') ||
        setweight(to_tsvector('simple', immutable_unaccent(subject)), 'B') ||
        setweight(to_tsvector('simple', immutable_unaccent(notes)), 'C') ||
        setweight(to_tsvector('simple', immutable_unaccent(ai_note)), 'C') ||
        setweight(to_tsvector('simple', immutable_unaccent(keywords)), 'C') ||
        setweight(
            to_tsvector('simple', immutable_unaccent(
                regexp_replace(file_name, '[^[:alnum:]]+', ' ', 'g'))),
            'D') ||
        setweight(to_tsvector('simple', immutable_unaccent(ocr_text)), 'D')
    );

CREATE INDEX idx_photos_ocr_pending ON photos (created_at DESC, uid DESC)
    WHERE ocr_at IS NULL AND archived_at IS NULL AND media_type <> 'video';
