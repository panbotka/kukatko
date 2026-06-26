-- 0005_jobs: the persistent, Postgres-backed job queue.
--
-- This is Kukátko's core robustness improvement over photo-sorter, whose jobs
-- lived in memory and SSE and were lost on every restart. Background work
-- (image_embed, face_detect, thumbnail, pp_import, ps_migrate, backup) is durable:
-- it survives restarts, retries with exponential backoff, deduplicates per photo,
-- and simply waits in 'queued' while the embeddings box is offline.
--
-- Workers claim jobs with SELECT … FOR UPDATE SKIP LOCKED so multiple workers or
-- instances never process the same row. A failed attempt is requeued with a
-- run_after backoff until attempts reaches max_attempts, after which the job is
-- dead-lettered (state='dead' + last_error) for an admin to inspect or requeue.
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE jobs (
    id           BIGSERIAL   PRIMARY KEY,
    type         TEXT        NOT NULL,
    state        TEXT        NOT NULL DEFAULT 'queued'
        CHECK (state IN ('queued', 'running', 'done', 'failed', 'dead')),
    priority     INTEGER     NOT NULL DEFAULT 0,
    payload      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    max_attempts INTEGER     NOT NULL DEFAULT 5,
    last_error   TEXT        NOT NULL DEFAULT '',
    run_after    TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by    TEXT,
    locked_at    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Claim/scan index: the worker filters by state, then earliest run_after and
-- priority. Matches the WHERE/ORDER BY of the SKIP LOCKED claim query.
CREATE INDEX idx_jobs_claim ON jobs (state, run_after, priority);

-- Dedup: at most one *active* (queued|running) job per (type, photo_uid). The
-- partial predicate lets a finished/dead job for the same photo be re-enqueued,
-- and because NULLs are distinct in a unique index, jobs without a photo_uid
-- (e.g. backup) are never deduplicated against one another.
CREATE UNIQUE INDEX idx_jobs_dedup ON jobs (type, (payload ->> 'photo_uid'))
    WHERE state IN ('queued', 'running');
