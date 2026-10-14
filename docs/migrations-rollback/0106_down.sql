-- 0106_down: put back 0075's one-queued-export-for-the-instance index.
--
-- The old key admits one queued family_export for the whole instance. Every
-- project has its own while 0106 is in force, so one is queued per project with
-- a pending rewrite — and a project deleted before the rollback (so 0102_down
-- would pass) may still have one, the payload's project_uid being no foreign
-- key. Rebuilding the old index over two of them would fail here and leave the
-- chain half-unwound, so every queued export that is not the bootstrap
-- project's is cancelled first, the way 0107_down cancels digests. Nothing is
-- lost the previous release could use: it serves only the bootstrap project's
-- library, and the bootstrap project's queued export (if any) is kept.
DELETE FROM jobs
WHERE type = 'family_export' AND state = 'queued'
  AND payload ->> 'project_uid' IS DISTINCT FROM 'pj0000000000000000veselice';
DROP INDEX IF EXISTS idx_jobs_family_export_dedup;
CREATE UNIQUE INDEX idx_jobs_family_export_dedup ON jobs (type)
    WHERE type = 'family_export' AND state = 'queued';

DELETE FROM schema_migrations WHERE version = 106;
