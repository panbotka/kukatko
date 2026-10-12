-- 0103_down: users.role back to the five-rung ladder, sessions.role back.
--
-- 0103 collapsed viewer/curator/editor/admin to 'user' after copying each role
-- into the bootstrap project's membership, so that membership is where the role
-- comes back from (a maintainer stays a maintainer). An account with no
-- bootstrap membership (impossible today; every account has one) falls back to
-- the least privilege, viewer. sessions.role is re-filled from the account, as a
-- fresh login of the previous release would have cached it.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ALTER COLUMN role DROP DEFAULT;

UPDATE users u SET role = m.role
FROM project_members m
WHERE m.user_uid = u.uid AND m.project_uid = 'pj0000000000000000veselice' AND u.role = 'user';
UPDATE users SET role = 'viewer' WHERE role = 'user';

ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('viewer', 'curator', 'editor', 'admin', 'maintainer'));

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS role TEXT;
UPDATE sessions s SET role = u.role FROM users u WHERE u.uid = s.user_uid;
ALTER TABLE sessions ALTER COLUMN role SET NOT NULL;

DELETE FROM schema_migrations WHERE version = 103;
