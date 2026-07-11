-- 0023_role_ai: allow the new 'ai' user role.
--
-- Adds 'ai' to the users.role CHECK constraint introduced by 0002_auth. The ai
-- role is for an automated agent that authenticates via an API token: it holds
-- an editor's write powers plus permission to trigger imports, but no other
-- administrative capability. Extending the constraint makes the role assignable
-- through the admin user API; the application enforces its permission boundary.
--
-- sessions.role carries no CHECK constraint, so no change is needed there; the
-- value is copied from users.role at login and is already validated on the user.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE users DROP CONSTRAINT users_role_check;

ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'editor', 'viewer', 'ai'));
