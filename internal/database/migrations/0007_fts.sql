-- 0007_fts: Czech-aware full-text search over photo text fields.
--
-- Kukátko mirrors photo-sorter's search: a single tsvector column on photos,
-- built with the `simple` dictionary (no stemming — Czech has no good Postgres
-- stemmer) wrapped in unaccent so matching is diacritics-insensitive ("deti"
-- finds "Děti"). The text fields are weighted title (A) > description (B) >
-- notes (C) > file_name (D) so a title hit outranks a notes hit.
--
-- The column is GENERATED ALWAYS … STORED so it stays correct automatically on
-- every insert and metadata update — there is no trigger and no application
-- code to keep in sync. PostgreSQL only allows IMMUTABLE expressions in a
-- generated column, but the unaccent extension's unaccent(text) is merely
-- STABLE (its default dictionary could change). The standard workaround is the
-- immutable_unaccent wrapper below: it pins the dictionary explicitly to the
-- `unaccent` text-search dictionary and is declared IMMUTABLE, which is safe in
-- practice because that dictionary is fixed for the lifetime of the database.
--
-- file_name is normalised before tokenisation by replacing every run of
-- non-alphanumeric characters with a space, so "IMG_1234.heic" yields the
-- tokens "img", "1234" and "heic" instead of one opaque blob.
--
-- This migration is wrapped in a transaction by the runner; CREATE FUNCTION,
-- ALTER TABLE … ADD COLUMN and CREATE INDEX are all transaction-safe.

-- immutable_unaccent is an IMMUTABLE wrapper over unaccent() with the dictionary
-- pinned, so it may be used in the generated column and in query-time tsquery
-- parsing. STRICT keeps NULL in -> NULL out; the photos text columns are NOT
-- NULL DEFAULT '' so this only matters defensively.
CREATE FUNCTION immutable_unaccent(text)
    RETURNS text
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    STRICT
AS $$
    SELECT unaccent('unaccent', $1)
$$;

ALTER TABLE photos
    ADD COLUMN fts tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', immutable_unaccent(title)), 'A') ||
        setweight(to_tsvector('simple', immutable_unaccent(description)), 'B') ||
        setweight(to_tsvector('simple', immutable_unaccent(notes)), 'C') ||
        setweight(
            to_tsvector('simple', immutable_unaccent(
                regexp_replace(file_name, '[^[:alnum:]]+', ' ', 'g'))),
            'D')
    ) STORED;

-- GIN index over the search vector for fast `fts @@ tsquery` lookups.
CREATE INDEX idx_photos_fts ON photos USING gin (fts);
