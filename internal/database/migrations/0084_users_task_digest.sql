-- 0084_users_task_digest: the daily e-mail digest of tasks waiting on a person.
--
-- An agent opens questions and review batches in the task queue for a human,
-- but until now nothing reached that human outside the app. The `task_digest`
-- job (internal/taskdigestjob) runs once a day and mails every person one
-- message listing the open tasks whose move is theirs — the same "waiting on
-- me" the listing computes, evaluated for everybody at once.
--
-- task_digest_at is when that person's last digest was scheduled. It is what
-- keeps the mail from repeating itself: a digest is sent only when some waiting
-- task has activity newer than this stamp (or the stamp is NULL — nobody has
-- been written to yet), so a person whose queue has not moved since yesterday
-- receives nothing today. It is stamped after the mail is enqueued and does not
-- touch updated_at, for the reason last_seen_at (0053) does not: being mailed
-- is not an edit to the profile.
--
-- The dedup index keeps at most one *queued* task_digest job, exactly as
-- 0075 does for family_export: the job belongs to no photo, so the per-photo
-- index (payload ->> 'photo_uid' is NULL, and NULLs are distinct) cannot see
-- it, and without an index of its own a restart straddling the scheduled hour
-- could queue the same day's digest twice. Scoped to 'queued' so a job that is
-- already running never blocks the next day's.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE users
    ADD COLUMN task_digest_at TIMESTAMPTZ;

COMMENT ON COLUMN users.task_digest_at IS
    'When the last tasks-waiting digest was scheduled for this account; NULL before the first one.';

CREATE UNIQUE INDEX idx_jobs_task_digest_dedup ON jobs (type)
    WHERE type = 'task_digest' AND state = 'queued';
