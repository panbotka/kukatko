-- 0015_perf_indexes: covering indexes for the hot photo-listing orderings.
--
-- The shared GET /photos browse/grid path (internal/photos buildListQuery) runs
--
--   WHERE archived_at IS NULL
--   ORDER BY taken_at DESC NULLS LAST, uid DESC
--   LIMIT n OFFSET m
--
-- by far the most frequent query in the application (every library/album/label
-- /favorites grid page). The original idx_photos_taken_at (taken_at DESC) cannot
-- serve that ordering: it is NULLS FIRST (PostgreSQL's default for DESC), it has
-- no uid tiebreaker, and it is not partial on archived_at, so the planner has to
-- read every live row and Sort it on each page. On a large library that Sort is
-- the dominant cost of a timeline page.
--
-- These two partial composite indexes match the live-timeline orderings exactly
-- (column directions, NULLS LAST placement, the uid tiebreaker, and the
-- archived_at IS NULL predicate), so a page becomes a bounded index scan that
-- stops after LIMIT+OFFSET rows with no Sort node. They are partial on
-- archived_at IS NULL because archived photos are a small minority and never
-- appear in the default grid; that keeps the indexes small and write-cheap.
--
-- idx_photos_live_taken_at backs the default capture-time ordering; the
-- companion idx_photos_live_created_at backs the "recently added" ordering
-- (sort=added -> created_at) used right after an upload. The other sort fields
-- (title, file_size) are rare and intentionally left to a sort.
--
-- The original idx_photos_taken_at is kept: it still serves the uncommon
-- include-archived timeline (which these partial indexes do not cover).
--
-- This migration is wrapped in a transaction by the runner; CREATE INDEX is
-- transaction-safe. The indexes are created non-concurrently because the runner
-- owns the transaction and the table is small relative to the shared instance.

CREATE INDEX idx_photos_live_taken_at
    ON photos (taken_at DESC NULLS LAST, uid DESC)
    WHERE archived_at IS NULL;

CREATE INDEX idx_photos_live_created_at
    ON photos (created_at DESC NULLS LAST, uid DESC)
    WHERE archived_at IS NULL;
