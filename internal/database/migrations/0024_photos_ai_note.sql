-- 0024_photos_ai_note: free-text "AI note" on photos, written by an external AI
-- classification pass and included in full-text search.
--
-- ai_note mirrors the existing notes column (0003_photos): a NOT NULL TEXT that
-- defaults to '' so the Go model stays a plain string and existing rows carry the
-- empty string. It is user-editable via PATCH /photos/{uid} and settable by an
-- automated agent through the same route.
--
-- The photos.fts generated column (0007_fts) is rebuilt to fold ai_note into the
-- search vector at weight C — the same weight as notes — so a term appearing only
-- in a photo's AI note still finds it, while a title/description hit still ranks
-- above it. PostgreSQL 17's ALTER COLUMN … SET EXPRESSION rewrites the table,
-- recomputes the stored vector for every existing row and rebuilds the GIN index
-- (idx_photos_fts) automatically, so no index or application query changes are
-- needed. immutable_unaccent keeps the new field diacritics-insensitive like the
-- others.
--
-- This migration is wrapped in a transaction by the runner; ALTER TABLE … ADD
-- COLUMN and ALTER COLUMN … SET EXPRESSION are both transaction-safe.

ALTER TABLE photos ADD COLUMN ai_note TEXT NOT NULL DEFAULT '';

ALTER TABLE photos
    ALTER COLUMN fts SET EXPRESSION AS (
        setweight(to_tsvector('simple', immutable_unaccent(title)), 'A') ||
        setweight(to_tsvector('simple', immutable_unaccent(description)), 'B') ||
        setweight(to_tsvector('simple', immutable_unaccent(notes)), 'C') ||
        setweight(to_tsvector('simple', immutable_unaccent(ai_note)), 'C') ||
        setweight(
            to_tsvector('simple', immutable_unaccent(
                regexp_replace(file_name, '[^[:alnum:]]+', ' ', 'g'))),
            'D')
    );
