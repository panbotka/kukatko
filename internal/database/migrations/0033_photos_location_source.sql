-- 0033_photos_location_source: where a photo's coordinates came from, so an
-- inferred location can never pass itself off as a measured one.
--
-- Plenty of photos have no GPS — a camera without a receiver, a scan, a stripped
-- export — but were taken the same day, in the same place, as photos that do have
-- coordinates. Inferring the missing location from those neighbours fills the map
-- and makes the places hierarchy useful. It also invents data, and lat/lng alone
-- cannot say whether a coordinate was measured or guessed. This column can:
--
--   * ''         nothing is known about the provenance. Legacy rows land here, as
--                does every photo nobody has decided anything about. Combined with
--                a NULL lat/lng this is the ONLY state the estimator may fill.
--   * 'exif'     the file's own GPS tags.
--   * 'manual'   the user decided. Note this covers "the user cleared the
--                location" — a location the user deleted is a decision, not a gap,
--                so 'manual' with NULL coordinates is a deliberate tombstone that
--                keeps the nightly backfill from re-adding a guess the user threw
--                away. Without it, re-running the backfill would be maddening.
--   * 'estimate' inferred from same-day neighbours. The only value the UI marks,
--                the only value the estimator is allowed to overwrite, and the
--                only value "accept" (→ 'manual') and "clear" act on.
--
-- The vocabulary mirrors taken_at_source (0003_photos), down to being a plain
-- TEXT NOT NULL DEFAULT '' rather than an enum: the same reasons apply (a new
-- source must not need a migration) and the Go model stays a plain string.
--
-- Existing rows are deliberately NOT backfilled to 'exif'. A row with
-- coordinates today may have got them from the file, from an import, or from a
-- user who typed them in years ago, and this migration cannot tell which — so
-- writing 'exif' over all of them would put a confident lie in the exact column
-- whose entire job is to be honest about provenance. '' means "we don't know",
-- which is true. Nothing is lost: the estimator only ever considers rows with NO
-- location (lat IS NULL AND lng IS NULL), so a legacy row's coordinates are
-- already safe from it, and the UI only marks 'estimate' — an unmarked location
-- is exactly what a legacy row should render as.
--
-- The partial index serves the estimator's candidate scan, which is the one hot
-- query here: "photos with no location that nobody has decided about". It is
-- partial (WHERE lat IS NULL AND lng IS NULL) so it indexes only the shrinking
-- backlog rather than the whole table, and it disappears from the index as soon
-- as a photo is estimated or decided. The neighbour lookup rides the existing
-- taken_at index from 0015_perf_indexes instead.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE photos
    ADD COLUMN location_source TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_photos_location_estimate_candidates
    ON photos (taken_at)
    WHERE lat IS NULL AND lng IS NULL AND location_source = '';
