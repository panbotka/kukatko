-- 0044_jobs_dedup_sidecar_queued: let an edit during a running sidecar job
-- schedule a follow-up rewrite instead of being dropped.
--
-- 0005's idx_jobs_dedup keyed dedup on (type, photo_uid) WHERE state IN
-- ('queued','running'): at most one *active* job per photo per type. For every
-- other job type that is correct — a running import, embed or thumbnail already
-- covers the photo, so a duplicate enqueue while it runs is genuinely redundant.
--
-- For the `sidecar` type it silently drops updates. A sidecar job re-reads the
-- photo when it runs, so a job that is already 'running' read the photo at claim
-- time and wrote the file *before* it could see an edit that lands afterwards.
-- With 'running' in the dedup predicate, that later edit's EnqueueSidecar collides
-- with the in-flight job and is swallowed as a duplicate, so the edit never reaches
-- the on-disk sidecar until some unrelated later edit happens to enqueue a fresh
-- job — and if the database is lost first, the edit is unrecoverable, defeating the
-- "curation survives the database" guarantee.
--
-- Fix: scope the sidecar dedup to state='queued' only, so an edit arriving while a
-- sidecar job runs schedules a fresh follow-up. Other job types keep the
-- queued|running semantics unchanged. This is not a tight rewrite loop: the handler
-- enqueues nothing itself, each edit schedules at most one queued job, and the
-- queued-state debounce still collapses ordinary bursts (SidecarDebounce delays
-- each job and further edits during that window coalesce onto the one queued row).
--
-- The rewritten index keeps the name idx_jobs_dedup (the store maps that name to
-- ErrDuplicate) and indexes a strict subset of the old rows — it drops only the
-- running-sidecar rows — so recreating it cannot fail on existing data. This
-- migration is wrapped in a transaction by the runner.

DROP INDEX idx_jobs_dedup;

CREATE UNIQUE INDEX idx_jobs_dedup ON jobs (type, (payload ->> 'photo_uid'))
    WHERE state = 'queued'
       OR (state = 'running' AND type <> 'sidecar');
