-- 0062_instance_settings: the instance-wide settings an administrator edits at runtime.
--
-- Three values that describe the instance rather than the library, and that an
-- administrator must be able to change without a redeploy:
--
--   registration_enabled — whether the sign-in screen offers self-service
--     registration at all. Default false: a fresh instance is closed until its
--     administrator deliberately opens it.
--   registration_secret — the shared secret registration asks a newcomer for.
--     Stored in readable form on purpose: the administrator has to read it back
--     to tell people what it is, which a hash cannot do. It is therefore never
--     returned to anyone below the admin role (see internal/settingsapi).
--   welcome_markdown — the Markdown greeting shown to a person the first time
--     they sign in.
--
-- Like announcements (0039) there is at most one row, enforced the same way: id
-- is a BOOLEAN pinned to true by a DEFAULT and a CHECK, which makes it the only
-- permissible primary key value. Writing the settings is therefore an
-- INSERT ... ON CONFLICT (id) DO UPDATE upsert.
--
-- The row is seeded here so every read finds it — the sign-in screen asks
-- whether registration is open before anybody has ever opened the admin area,
-- and "no row yet" must not be a case the client has to think about.
--
-- updated_by_uid records who last changed the settings and cascades to NULL on
-- user deletion (SET NULL, not CASCADE: losing that account must not take the
-- instance's own configuration with it). This migration is wrapped in a
-- transaction by the runner.

CREATE TABLE instance_settings (
    id                   BOOLEAN     PRIMARY KEY DEFAULT true CHECK (id),
    registration_enabled BOOLEAN     NOT NULL DEFAULT false,
    registration_secret  TEXT        NOT NULL DEFAULT '',
    welcome_markdown     TEXT        NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by_uid       VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL
);

INSERT INTO instance_settings (id) VALUES (true);
