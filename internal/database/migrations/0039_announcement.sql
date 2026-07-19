-- 0039_announcement: a single instance-wide announcement shown to all users.
--
-- A maintainer publishes one short message (e.g. "expected downtime tonight
-- 22:00–23:00") from the admin area and every signed-in user then sees it as a
-- banner at the top of the app. There is at most one announcement at a time, so
-- the table is constrained to a single row: id is a BOOLEAN pinned to true by a
-- DEFAULT and a CHECK, which makes it the only permissible primary key value.
-- Publishing is therefore an INSERT ... ON CONFLICT (id) DO UPDATE upsert and
-- clearing is a DELETE — no row identity to track.
--
-- level drives the banner variant (info | warning), constrained by a CHECK so a
-- bad value cannot reach the client. author_uid records who last published it and
-- cascades to NULL on user deletion (SET NULL, not CASCADE: losing the author must
-- not silently take the live announcement down). updated_at is bumped on every
-- publish so the frontend can key a per-user "dismissed" flag on it — dismissing
-- hides the current message, but a freshly published one (new updated_at) reappears.
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE announcements (
    id         BOOLEAN     PRIMARY KEY DEFAULT true CHECK (id),
    message    TEXT        NOT NULL,
    level      TEXT        NOT NULL DEFAULT 'info' CHECK (level IN ('info', 'warning')),
    author_uid VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
