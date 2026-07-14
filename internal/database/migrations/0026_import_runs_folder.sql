-- 0026_import_runs_folder: allow the "folder" import source.
--
-- `kukatko import dir <path>` ingests a directory of originals from disk through
-- the same pipeline as an upload, and records the run in import_runs so it shows
-- up in /import and GET /import/runs next to the PhotoPrism and photo-sorter
-- runs. A folder run has no source timestamp to resume from, so it never sets a
-- high_watermark — idempotency comes from the SHA256 content hash instead.
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE import_runs DROP CONSTRAINT IF EXISTS import_runs_source_check;
ALTER TABLE import_runs ADD CONSTRAINT import_runs_source_check
    CHECK (source IN ('photoprism', 'photosorter', 'folder'));
