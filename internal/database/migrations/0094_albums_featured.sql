-- 0094_albums_featured: a curator may promote an album to the homepage for a
-- limited time ("Doporučujeme").
--
-- featured_until is the whole state: an album is featured while
-- featured_until > now(). Nothing ever "unfeatures" an album when the date
-- passes — every read compares against the clock, so an expired value may stay
-- in the row and simply stops matching. NULL means the album was never featured
-- or the feature was switched off by hand.
--
-- featured_at orders the homepage strip (newest feature first) and featured_by
-- records who promoted it. Extending a running feature keeps featured_at, so a
-- prolonged album does not jump ahead of one featured later; featuring an album
-- whose feature had lapsed stamps a fresh one.
--
-- Only a hand-made album (type 'album') can be featured: the machine-made
-- groupings (folders, months, moments, places) are not something one points
-- the whole family at. The CHECK holds the line in the database too, so no
-- other write path can feature one by accident.
--
-- Reset classification: these are columns on albums, which internal/reset
-- already wipes, so the table lists need no change. featured_by points from the
-- wiped albums into the preserved users (exactly as created_by does), which a
-- TRUNCATE of albums is free to do.
--
-- Adding nullable columns with no default is metadata-only in PostgreSQL. The
-- partial index covers the handful of albums that have ever been featured, so
-- the homepage read never scans the whole albums table.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE albums
    ADD COLUMN featured_until TIMESTAMPTZ,
    ADD COLUMN featured_at    TIMESTAMPTZ,
    ADD COLUMN featured_by    VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL;

ALTER TABLE albums
    ADD CONSTRAINT albums_featured_only_hand_made
        CHECK (featured_until IS NULL OR type = 'album');

CREATE INDEX idx_albums_featured ON albums (featured_at DESC)
    WHERE featured_until IS NOT NULL;
