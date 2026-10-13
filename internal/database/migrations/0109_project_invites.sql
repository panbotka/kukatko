-- 0109_project_invites: invitation links into a project.
--
-- A project admin adds people without seeing the instance's other accounts.
-- Besides the exact-match add, the main way is an invitation link: a short code
-- carrying a role and an expiry, which a signed-in person opens to join the
-- project and an anonymous one opens to register straight into it.
-- internal/invite owns the table.
--
-- The code is handled exactly like an upload link's (0090, 0091): looked up
-- through its SHA-256 (code_hash, the unique, indexed column) and kept readable
-- (code) so the project's admins can copy the URL again from the list. role is
-- what accepting grants (registration through an invite caps it at editor, in
-- code). max_uses is NULL for "no cap"; uses counts the accepts that granted
-- something. revoked_at is final. The creator is SET NULL, not CASCADE — an
-- invite somebody sent outlives the account that made it.
--
-- It is a catalogue table for the guarded library wipe: invitations are part
-- of the project's library and are wiped with it. Deleting the project
-- cascades.

CREATE TABLE IF NOT EXISTS project_invites (
    uid         VARCHAR(32) PRIMARY KEY,
    project_uid TEXT        NOT NULL REFERENCES projects (uid) ON DELETE CASCADE,
    code_hash   TEXT        NOT NULL UNIQUE,
    code        TEXT        NOT NULL,
    role        TEXT        NOT NULL CHECK (role IN ('viewer', 'curator', 'editor', 'admin')),
    expires_at  TIMESTAMPTZ NOT NULL,
    max_uses    INTEGER     NULL CHECK (max_uses IS NULL OR max_uses > 0),
    uses        INTEGER     NOT NULL DEFAULT 0 CHECK (uses >= 0),
    revoked_at  TIMESTAMPTZ NULL,
    created_by  VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE project_invites IS
    'An invitation link into a project: a short code (hashed for lookup, readable for its admins) granting a role.';

-- A project's invites, newest first (the admin's list).
CREATE INDEX IF NOT EXISTS idx_project_invites_project ON project_invites (project_uid, created_at DESC);
