-- 0028_photos_metadata_extracted: bookkeeping for the metadata extraction that
-- fills the IPTC/XMP and file-technical columns added by 0027.
--
-- metadata_extracted_at records when a photo's *own file* was last read out into
-- those columns. It is not metadata about the photo — it is the resume marker of
-- the backfill:
--
--   * NULL  — the original has never been read. Every row that predates the
--     extractor is NULL, as is every row an importer creates by mapping metadata
--     from its source (PhotoPrism, photo-sorter) rather than from the file.
--   * set   — the file has been read. The ingest pipeline stamps it as it creates
--     the row, and the `metadata` job stamps it when it re-reads an original.
--
-- The backfill schedules exactly the NULL rows, so it converges: running it twice
-- enqueues nothing the second time, and a run interrupted halfway picks up where
-- it stopped. A photo whose file simply carries no IPTC tags is stamped like any
-- other — "we looked and there was nothing" is a finished photo, not a pending
-- one.
--
-- No DEFAULT: a default would silently mark every future row as extracted,
-- including the importer rows that are precisely what the backfill exists for.
-- The value is written by the application, which knows whether it actually read
-- the file.
--
-- The partial index covers the backfill's only query (the pending, non-archived
-- photos) and stays tiny — it holds nothing once the backfill has drained.

ALTER TABLE photos
    ADD COLUMN metadata_extracted_at TIMESTAMPTZ;

CREATE INDEX idx_photos_metadata_pending ON photos (created_at DESC, uid DESC)
    WHERE metadata_extracted_at IS NULL AND archived_at IS NULL;
