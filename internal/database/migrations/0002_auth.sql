-- 0002_auth: authentication & authorization schema (users and sessions).
--
-- users holds local accounts with bcrypt password hashes and a role of
-- admin / editor / viewer (editor and admin have write access; viewer is
-- read-only). sessions holds opaque-token sessions with a sliding expiry: the
-- application extends expires_at on activity up to a maximum lifetime and an
-- hourly job removes expired rows. Each session also carries a separate
-- download_token used to authorise media-download URLs without exposing the
-- main session token in query strings.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE users (
    uid           VARCHAR(32) PRIMARY KEY,
    username      TEXT        NOT NULL UNIQUE,
    display_name  TEXT        NOT NULL DEFAULT '',
    email         TEXT        NOT NULL DEFAULT '',
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    disabled      BOOLEAN     NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);

CREATE TABLE sessions (
    id             VARCHAR(32) PRIMARY KEY,
    token          TEXT        NOT NULL UNIQUE,
    download_token TEXT        NOT NULL UNIQUE,
    user_uid       VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    role           TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at     TIMESTAMPTZ NOT NULL
);

-- Supports the hourly cleanup of expired sessions and per-user invalidation.
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);
CREATE INDEX idx_sessions_user_uid ON sessions (user_uid);
