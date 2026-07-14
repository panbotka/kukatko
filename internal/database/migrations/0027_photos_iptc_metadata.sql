-- 0027_photos_iptc_metadata: the IPTC/XMP credit fields and the file-technical
-- metadata PhotoPrism keeps but Kukátko had nowhere to put.
--
-- Two groups of columns:
--
--   * user-editable credit fields — subject (the IPTC headline: what the photo is
--     about), artist, copyright, license and keywords. Keywords hold the original
--     IPTC value verbatim, comma-separated, exactly as PhotoPrism shapes it. They
--     are deliberately NOT labels: Kukátko's labels are its own curated taxonomy
--     (internal/organize) and stay untouched, while this column preserves what the
--     source file actually said.
--
--   * machine-derived fields — software (camera firmware, Lightroom, a scanner's
--     driver), scan (the image is a digitised print, not a camera capture),
--     color_profile, image_codec (the *still* image's compression: jpeg, heic,
--     avif — the existing video_codec/audio_codec from 0004_video are separate and
--     stay untouched), camera_serial, original_name (the file name the photo had
--     before it was ingested; file_name is the name it carries in the storage
--     layout) and projection ("equirectangular" for a panorama, empty otherwise).
--
-- Every column mirrors the existing text columns of 0003_photos: NOT NULL with a
-- '' default (scan: false), so the Go model stays a plain string/bool and existing
-- rows simply carry the zero value. Nothing backfills them here — EXIF extraction
-- and the PhotoPrism import mapping are separate changes.
--
-- The photos.fts generated column (0007_fts, last rebuilt by 0024_photos_ai_note)
-- is rebuilt so subject folds into the search vector at weight B — it is a real
-- caption-like field, ranking with description — and keywords at weight C, with
-- the notes. Every existing contribution (title A, description B, notes C,
-- ai_note C, file_name D) is kept exactly as it was. PostgreSQL's ALTER COLUMN …
-- SET EXPRESSION rewrites the table, recomputes the stored vector for every
-- existing row and rebuilds the GIN index (idx_photos_fts) automatically, so no
-- index or application query changes are needed. immutable_unaccent keeps the new
-- fields diacritics-insensitive like the others.
--
-- This migration is wrapped in a transaction by the runner; ALTER TABLE … ADD
-- COLUMN and ALTER COLUMN … SET EXPRESSION are both transaction-safe.

ALTER TABLE photos
    ADD COLUMN subject       TEXT    NOT NULL DEFAULT '',
    ADD COLUMN artist        TEXT    NOT NULL DEFAULT '',
    ADD COLUMN copyright     TEXT    NOT NULL DEFAULT '',
    ADD COLUMN license       TEXT    NOT NULL DEFAULT '',
    ADD COLUMN keywords      TEXT    NOT NULL DEFAULT '',
    ADD COLUMN software      TEXT    NOT NULL DEFAULT '',
    ADD COLUMN scan          BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN color_profile TEXT    NOT NULL DEFAULT '',
    ADD COLUMN image_codec   TEXT    NOT NULL DEFAULT '',
    ADD COLUMN camera_serial TEXT    NOT NULL DEFAULT '',
    ADD COLUMN original_name TEXT    NOT NULL DEFAULT '',
    ADD COLUMN projection    TEXT    NOT NULL DEFAULT '';

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
            'D')
    );
