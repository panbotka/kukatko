-- 0020_api_tokens: long-lived API tokens for non-interactive clients.
--
-- Until now the only credential a client could present was the kukatko_session
-- cookie (plus the per-session download_token on media URLs), so a CLI, a script
-- or an agent had to store a username and password and replay a login. An API
-- token is a bearer credential of the form `kkt_<id>_<secret>`: `id` is this
-- table's primary key, so verification is a single indexed lookup rather than a
-- scan over every row's hash.
--
-- Only a SHA-256 hash of the secret is stored; the plaintext is returned exactly
-- once, at creation. SHA-256 rather than bcrypt is deliberate — see the comment
-- on hashAPITokenSecret in internal/auth/apitoken.go.
--
-- A token inherits the role of the user it belongs to, so there is no role
-- column here and no second permission system: the existing RBAC middleware
-- reads the role off the user row as it always has. Deleting the user deletes
-- their tokens.
--
-- expires_at NULL means the token never expires; revoked_at NULL means it is
-- live. Both are checked on every request, so a revoked or lapsed token stops
-- authenticating immediately without needing a cleanup job. last_used_at is a
-- best-effort activity stamp, written at most once a minute per token.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE api_tokens (
    id           VARCHAR(32) PRIMARY KEY,
    user_uid     VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    secret_hash  TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

-- Supports listing a caller's own tokens and the cascade on user deletion.
CREATE INDEX idx_api_tokens_user_uid ON api_tokens (user_uid);
