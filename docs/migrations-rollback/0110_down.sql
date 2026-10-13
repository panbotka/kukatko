-- 0110_down: put the transitional users columns back, filled from the
-- memberships they were dropped in favour of.
--
-- The previous release reads users.approved_at for sign-in and users.subject_uid
-- for /auth/me and the profile picture, so both are rebuilt the way that release
-- kept them in step: an account is approved once any membership is (a
-- maintainer, or an account in no project, always), and its link, visit clocks and
-- digest watermark are its bootstrap-project membership's. Run it before the
-- older scripts (newest first, as the README says).
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS last_seen_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS visit_reference_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS subject_uid        VARCHAR(32),
    ADD COLUMN IF NOT EXISTS approved_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS task_digest_at     TIMESTAMPTZ;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_subject_uid_fkey;
ALTER TABLE users ADD CONSTRAINT users_subject_uid_fkey
    FOREIGN KEY (subject_uid) REFERENCES subjects (uid) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_users_subject_uid ON users (subject_uid)
    WHERE subject_uid IS NOT NULL;
COMMENT ON COLUMN users.task_digest_at IS
    'When the last tasks-waiting digest was scheduled for this account; NULL before the first one.';

UPDATE users u SET
    subject_uid        = m.subject_uid,
    last_seen_at       = m.last_seen_at,
    visit_reference_at = m.visit_reference_at,
    task_digest_at     = m.task_digest_at
FROM project_members m
WHERE m.user_uid = u.uid AND m.project_uid = 'pj0000000000000000veselice';

UPDATE users u SET approved_at = COALESCE(
    (SELECT min(m.approved_at) FROM project_members m WHERE m.user_uid = u.uid),
    CASE WHEN u.role = 'maintainer'
          OR NOT EXISTS (SELECT 1 FROM project_members m WHERE m.user_uid = u.uid)
    THEN u.created_at END);

DELETE FROM schema_migrations WHERE version = 110;
