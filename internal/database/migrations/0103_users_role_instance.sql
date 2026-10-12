-- 0103_users_role_instance: users.role becomes the instance role.
--
-- What an account may do inside a library is now its role in that project
-- (project_members.role, viewer..admin). What is left on users is the
-- instance-wide role: 'maintainer' — the system administrator, who operates the
-- instance (backup, jobs, maintenance, imports) and is implicitly an admin of
-- every project — or 'user', everybody else.
--
-- Order matters:
--   1. Re-run the membership backfill of 0101 first. An account created by the
--      previous binary after 0101 ran has no project_members row yet, and the
--      collapse below would erase the only record of its role. ON CONFLICT DO
--      NOTHING leaves every existing membership exactly as it is.
--   2. Drop the old CHECK, collapse every non-maintainer role to 'user', add the
--      new CHECK. The UPDATE runs before the ADD so the surviving rows are legal.
--   3. sessions.role cached the old role at login; nothing reads it any more
--      (authorization reads the live account and its membership on every
--      request), so it goes. Existing sessions stay valid — only the column is
--      gone, not the rows.
--
-- The runner wraps the migration in one transaction and the DDL takes an
-- exclusive lock, so no concurrent write slips an old role in while the
-- constraint is off.

INSERT INTO project_members (project_uid, user_uid, role, approved_at, subject_uid,
                             last_seen_at, visit_reference_at, task_digest_at)
SELECT 'pj0000000000000000veselice', u.uid,
       CASE WHEN u.role IN ('viewer', 'curator', 'editor', 'admin') THEN u.role ELSE 'admin' END,
       u.approved_at, u.subject_uid, u.last_seen_at, u.visit_reference_at, u.task_digest_at
FROM users u
ON CONFLICT (project_uid, user_uid) DO NOTHING;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;

UPDATE users SET role = 'user' WHERE role NOT IN ('user', 'maintainer');

ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'maintainer'));

ALTER TABLE users ALTER COLUMN role SET DEFAULT 'user';

ALTER TABLE sessions DROP COLUMN IF EXISTS role;
