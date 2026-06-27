-- 0013_import_runs: history of import/migration runs and their high-watermarks.
--
-- Backs incremental, idempotent imports (see ARCHITECTURE.md §5.2, §9, §10).
-- Each PhotoPrism (read-only, repeatable) or photo-sorter run records the time
-- window it covered: high_watermark holds the largest source timestamp processed
-- (e.g. max PhotoPrism UpdatedAt). The next run for the same source resumes from
-- the latest *successful* run's watermark, so a crashed or failed run never
-- advances the cursor and work is simply retried. counts is a JSONB summary
-- (imported/updated/skipped/failed) and last_error carries the failure reason.
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE import_runs (
    id             BIGSERIAL    PRIMARY KEY,
    source         TEXT         NOT NULL
        CHECK (source IN ('photoprism', 'photosorter')),
    started_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ,
    status         TEXT         NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'done', 'failed')),
    high_watermark TIMESTAMPTZ,
    counts         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    last_error     TEXT         NOT NULL DEFAULT ''
);

-- Resume index: the next incremental run looks up the most recent successful run
-- per source that produced a watermark. The partial predicate keeps the index to
-- exactly the rows that query reads, so running/failed runs are ignored cheaply.
CREATE INDEX idx_import_runs_watermark ON import_runs (source, finished_at DESC)
    WHERE status = 'done' AND high_watermark IS NOT NULL;
