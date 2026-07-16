-- 0035_photos_sidecar_written: bookkeeping for the metadata sidecar export.
--
-- A sidecar is the YAML file written next to the originals (under the sidecars/
-- prefix of the storage root) holding everything a human created or a machine
-- derived about a photo. It exists so the catalogue can be rebuilt from the
-- storage alone: originals + sidecars, no database. See docs/RESTORE.md.
--
-- sidecar_written_at records when the photo's sidecar was last written. Like
-- metadata_extracted_at (0028) it is not metadata about the photo — it is the
-- resume marker of the backfill:
--
--   * NULL  — no sidecar has ever been written for this photo. Every row that
--     predates the exporter is NULL, as is every row created while the exporter
--     was switched off.
--   * set   — a sidecar exists and was current as of this instant.
--
-- The backfill schedules the NULL rows plus the stale ones (written before the
-- photo's own updated_at), so it converges: running it twice over a drained
-- library enqueues nothing the second time, and a run interrupted halfway picks
-- up where it stopped.
--
-- The staleness half of that predicate is a safety net, not the primary path: a
-- sidecar job is enqueued by every mutation as it happens, and this catches only
-- what that missed — an enqueue lost to a crash between the commit and the
-- enqueue, or edits made while the feature was off. It is deliberately
-- incomplete: curation that lives in another table (album membership, labels,
-- markers, per-user ratings) does not touch photos.updated_at, so a lost enqueue
-- for one of those is only recovered by a forced full run (?all=true). That is
-- the trade — a cheap convergent predicate over a correct-but-unindexable one.
--
-- The write does NOT touch updated_at. Stamping this column is bookkeeping about
-- the export, not an edit of the photo; bumping updated_at here would make every
-- sidecar write mark its own photo stale again and the backfill would never
-- drain.
--
-- No DEFAULT: a default would silently mark every future row as exported,
-- including the rows the backfill exists for. The value is written by the
-- application, which knows whether the file actually landed in storage.
--
-- The partial index covers the backfill's first and largest query (never
-- exported, not archived) and stays tiny — it holds nothing once the backfill has
-- drained. The stale half falls back to a scan, which is fine for an admin-only
-- backfill that is expected to find nothing.

ALTER TABLE photos
    ADD COLUMN sidecar_written_at TIMESTAMPTZ;

CREATE INDEX idx_photos_sidecar_pending ON photos (created_at DESC, uid DESC)
    WHERE sidecar_written_at IS NULL AND archived_at IS NULL;
