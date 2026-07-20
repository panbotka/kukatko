-- 0042_import_failures: persist per-photo and per-file import failures, and admit
-- a truthful 'partial' run status.
--
-- Before this migration a per-photo or per-satellite import failure went only to
-- slog.Warn and was lost: a run reported status='done' even with hundreds of
-- failed photos, files, markers or album memberships (the counts JSONB carries
-- only four aggregate integers). This table records every individual failure of a
-- run so the failures can be listed and retried, and the new 'partial' status lets
-- a run that finished its scan but recorded at least one unresolved failure say so
-- instead of masquerading as a clean 'done'. See ARCHITECTURE.md §5.2, §9, §10.
--
-- Watermark semantics are deliberately unchanged: LatestWatermark still resumes
-- only from a 'done' run (idx_import_runs_watermark is predicated on status =
-- 'done'), so a 'partial' run never advances the cursor and re-running the import
-- retries the same window — the imports are idempotent, so a re-run is safe.
--
-- This migration is wrapped in a transaction by the runner.

-- Admit the 'partial' status. The CHECK was created inline in 0013_import_runs.sql
-- and auto-named import_runs_status_check by Postgres.
ALTER TABLE import_runs DROP CONSTRAINT IF EXISTS import_runs_status_check;
ALTER TABLE import_runs ADD CONSTRAINT import_runs_status_check
    CHECK (status IN ('running', 'done', 'partial', 'failed'));

-- Restore the 'folder' source. 0026_import_runs_folder.sql widened the source
-- CHECK to include 'folder' (the `kukatko import dir` runs), but 0041 rebuilt the
-- constraint from an older list and dropped it again, so a folder import currently
-- fails to insert its run row. importer.SourceFolder is still a valid source, so
-- re-admit every value the Source type accepts.
ALTER TABLE import_runs DROP CONSTRAINT IF EXISTS import_runs_source_check;
ALTER TABLE import_runs ADD CONSTRAINT import_runs_source_check
    CHECK (source IN ('photoprism', 'photosorter', 'photosorter_feeds', 'folder'));

CREATE TABLE import_failures (
    id          BIGSERIAL    PRIMARY KEY,
    -- The run that recorded this failure. ON DELETE CASCADE keeps failures tied to
    -- the lifetime of their run history row.
    run_id      BIGINT       NOT NULL REFERENCES import_runs (id) ON DELETE CASCADE,
    -- source mirrors the run's import source ('photoprism' / 'photosorter' /
    -- 'photosorter_feeds' / 'folder') so failures can be filtered without a join.
    source      TEXT         NOT NULL,
    -- stage names the step that failed: 'photo' for a whole photo, otherwise the
    -- satellite that was dropped ('file', 'marker', 'album_member', 'label',
    -- 'thumbnail', 'embedding', 'faces', 'phash', 'edit', ...).
    stage       TEXT         NOT NULL,
    -- photo_uid is the Kukátko photo uid when the failure is scoped to a photo that
    -- already exists in the catalogue; empty when the photo itself failed to import.
    photo_uid   TEXT         NOT NULL DEFAULT '',
    -- source_ref is the external identifier the failure is about: a PhotoPrism uid,
    -- a photo-sorter uid, a file hash, a marker uid, an album/label name — whatever
    -- lets an operator locate it at the source. Empty when not applicable.
    source_ref  TEXT         NOT NULL DEFAULT '',
    -- detail is a short human hint (a filename, a marker/album/label name).
    detail      TEXT         NOT NULL DEFAULT '',
    -- error is the failure message (the same text that used to go to slog.Warn).
    error       TEXT         NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- resolved_at is set when the failure is no longer outstanding (e.g. a later
    -- run imported the item, or an operator dismissed it). NULL = unresolved, which
    -- is what makes a run 'partial' and what the failures listing shows by default.
    resolved_at TIMESTAMPTZ
);

-- Listing a run's failures, newest first.
CREATE INDEX idx_import_failures_run ON import_failures (run_id, id DESC);

-- Unresolved-failure lookups drive both the 'partial' decision at run completion
-- (count per run) and the operator's outstanding-failures view (per source). The
-- partial predicate keeps the index to exactly the outstanding rows.
CREATE INDEX idx_import_failures_unresolved ON import_failures (source, created_at DESC)
    WHERE resolved_at IS NULL;
