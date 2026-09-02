-- 0068_photos_blurhash: every photo gets a tiny blurred stand-in, so a grid can
-- paint something the moment its rows arrive instead of a page of empty tiles.
--
-- blurhash holds a BlurHash string (woltapp/blurhash): a few dozen ASCII
-- characters describing the picture as a handful of DCT components — 28 bytes
-- for the 4x3 grid an ordinary photograph gets, 36 for the 4x4 a square one
-- gets. That is small enough to ride along in every photo of every list payload,
-- which is the whole reason for storing it on the row rather than in a side
-- table: a placeholder that costs a second request is a placeholder that arrives
-- after the image it was meant to stand in for.
--
-- The column is nullable with no default, and the NULL is load-bearing: it means
-- "not computed yet" — a row catalogued before this migration, or one whose
-- original could not be decoded at upload — and it is exactly the predicate the
-- backfill lists. An empty string would say the same thing far less clearly, and
-- the CHECK below makes sure nobody writes one: a photo either has a placeholder
-- or has none.
--
-- Adding a nullable column with no default is metadata-only in PostgreSQL, so
-- this migration does not rewrite the photos table and is safe to apply while
-- the app is serving.
--
-- idx_photos_blurhash_pending is the partial index the backfill reads, mirroring
-- idx_photos_ocr_pending (0058) and idx_photos_metadata_pending (0028): it
-- covers exactly the "no placeholder, not archived" predicate in the order the
-- backfill lists, so scheduling the remaining work stays an index scan rather
-- than a full pass over the catalogue. Videos are NOT excluded — their
-- thumbnails are rendered from the poster frame the pipeline already extracts,
-- so a clip has a first frame to blur just like a still has a picture.

ALTER TABLE photos
    ADD COLUMN blurhash TEXT
        CONSTRAINT photos_blurhash_not_empty CHECK (blurhash IS NULL OR blurhash <> '');

COMMENT ON COLUMN photos.blurhash IS
    'BlurHash placeholder of the photo''s rendering; NULL = not computed yet.';

CREATE INDEX idx_photos_blurhash_pending ON photos (created_at DESC, uid DESC)
    WHERE blurhash IS NULL AND archived_at IS NULL;
