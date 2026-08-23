-- 0065_password_reset_tokens: one-time links somebody uses to choose their own
-- password.
--
-- Until now the only way to help a person who lost their password was for an
-- administrator to set a new one and tell them what it is — which means the
-- administrator learns a password that person may well use elsewhere. A reset
-- link moves the choice back to its owner: the administrator issues the link,
-- the person picks the password, and nobody else ever sees it.
--
-- Only a hash of the token is stored, never the token itself. The row is
-- therefore useless to anybody who reads the table: the link that was mailed
-- cannot be reconstructed from it, exactly as api_tokens keeps only the hash of
-- its secret. It is a plain SHA-256 (hex) rather than bcrypt for the same reason
-- api_tokens uses one — the token is 256 bits from a cryptographically secure
-- source, so there is no dictionary for a slow hash to defend against. The
-- column is UNIQUE because the hash *is* the lookup key: a link is resolved by
-- one indexed equality on it, never by a scan.
--
-- issued_by_uid records which account started the reset. It is ON DELETE SET
-- NULL rather than CASCADE: the token belongs to the person who receives it, not
-- to the administrator who sent it, and removing an administrator must not
-- silently invalidate links other people are holding. user_uid is the opposite —
-- a token of an account that no longer exists is not a token — so it cascades.
--
-- used_at is a nullable timestamp, not a boolean: "when was this link used" is
-- the question an incident is investigated with, and a flag would answer only
-- the half of it that matters least. A consumed row is kept until the periodic
-- cleanup prunes it, so the moment stays readable for as long as the trail is
-- fresh; the audit log keeps the permanent record.
--
-- No partial index on the unused rows: the table holds one row per outstanding
-- reset (issuing a new one deletes the account's earlier unused links), so it is
-- a handful of rows on this instance and the hash index answers every read.
-- idx_password_reset_tokens_user_uid supports exactly that invalidation, and
-- idx_password_reset_tokens_expires_at the cleanup that runs beside the expired
-- sessions one.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE password_reset_tokens (
    id            VARCHAR(32) PRIMARY KEY,
    user_uid      VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    token_hash    TEXT        NOT NULL UNIQUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    issued_by_uid VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL
);

CREATE INDEX idx_password_reset_tokens_user_uid ON password_reset_tokens (user_uid);
CREATE INDEX idx_password_reset_tokens_expires_at ON password_reset_tokens (expires_at);
