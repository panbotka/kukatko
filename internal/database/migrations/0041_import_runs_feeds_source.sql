-- 0041_import_runs_feeds_source: admit the 'photosorter_feeds' import source.
--
-- The photo-sorter FEEDS importer (internal/psfeedsimport) enriches
-- PhotoPrism-imported photos with photo-sorter's pre-computed embeddings and
-- faces, copied 1:1 from its HTTP migration feeds. It records its runs under a
-- source distinct from the legacy direct-database 'photosorter' migration, so the
-- two paths keep separate run history and watermarks. Extend the source CHECK
-- constraint (created inline in 0013 as import_runs_source_check) to allow it.
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE import_runs DROP CONSTRAINT IF EXISTS import_runs_source_check;
ALTER TABLE import_runs ADD CONSTRAINT import_runs_source_check
    CHECK (source IN ('photoprism', 'photosorter', 'photosorter_feeds'));
