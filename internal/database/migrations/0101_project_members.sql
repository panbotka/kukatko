-- 0101_project_members: who belongs to which project, and in what role.
--
-- An account stays global (one person, one login), but everything that says
-- what that person is *inside a library* becomes per project: the role, whether
-- an admin has approved them, the person in the library they are ("this is
-- me"), and the three per-library clocks (last visit, the "what's new"
-- reference, the last task digest). One row per (project, account).
--
-- The role ladder here stops at admin: maintainer is an instance-wide operations
-- role, not a project one, so an existing maintainer becomes the bootstrap
-- project's admin.
--
-- Transitional: the matching users columns (role, approved_at, subject_uid,
-- last_seen_at, visit_reference_at, task_digest_at) stay and keep being read by
-- today's code; later steps move the readers over and a final step drops them.
-- The backfill below copies them once, so the two agree at the moment of the
-- switch.
--
-- Every statement is idempotent: the table and index are IF NOT EXISTS, the
-- backfill is ON CONFLICT DO NOTHING and the last-project update only fills
-- NULLs, so re-running the body changes nothing.

CREATE TABLE IF NOT EXISTS project_members (
    project_uid        TEXT NOT NULL REFERENCES projects (uid) ON DELETE CASCADE,
    user_uid           TEXT NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    role               TEXT NOT NULL CHECK (role IN ('viewer', 'curator', 'editor', 'admin')),
    approved_at        TIMESTAMPTZ NULL,
    -- Named explicitly: internal/reset drops and re-adds it around the wipe of
    -- subjects, exactly like users_subject_uid_fkey.
    subject_uid        TEXT NULL
        CONSTRAINT project_members_subject_uid_fkey REFERENCES subjects (uid) ON DELETE SET NULL,
    last_seen_at       TIMESTAMPTZ NULL,
    visit_reference_at TIMESTAMPTZ NULL,
    task_digest_at     TIMESTAMPTZ NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_uid, user_uid)
);

CREATE INDEX IF NOT EXISTS idx_project_members_user_uid ON project_members (user_uid);

COMMENT ON TABLE project_members IS
    'Membership of an account in a project: its per-project role (viewer..admin), approval, linked subject and per-project visit/digest clocks.';

-- Every existing account becomes a member of the bootstrap project with what it
-- has today; a maintainer is the project's admin.
INSERT INTO project_members (project_uid, user_uid, role, approved_at, subject_uid,
                             last_seen_at, visit_reference_at, task_digest_at)
SELECT 'pj0000000000000000veselice', u.uid,
       CASE u.role WHEN 'maintainer' THEN 'admin' ELSE u.role END,
       u.approved_at, u.subject_uid, u.last_seen_at, u.visit_reference_at, u.task_digest_at
FROM users u
ON CONFLICT (project_uid, user_uid) DO NOTHING;

-- The project an account last worked in, so a sign-in can land it there.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS last_project_uid TEXT NULL REFERENCES projects (uid) ON DELETE SET NULL;

UPDATE users SET last_project_uid = 'pj0000000000000000veselice' WHERE last_project_uid IS NULL;
