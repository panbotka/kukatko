-- 0067_passkey_credentials: the public keys a passkey (WebAuthn) sign-in is
-- verified against.
--
-- Registration on this instance is open behind a shared secret, so the password
-- is no longer the only thing between a stranger and an account they guessed the
-- name of. A passkey removes the guessable part entirely: the private half never
-- leaves the authenticator, nothing reusable is typed into a form, and a
-- credential minted for this origin cannot be replayed against another one.
--
-- One account may hold several — a phone, a laptop, a hardware key are three
-- different things a person signs in from — so the table is a list per user
-- rather than a column on `users`. user_uid cascades: a credential of an account
-- that no longer exists is not a credential.
--
-- credential_id is the authenticator's own identifier for the key pair, raw
-- bytes rather than base64 so the equality the login does is over the value the
-- protocol actually carries. It is UNIQUE because it *is* the lookup key of a
-- discoverable ("usernameless") login: the browser hands back a credential id
-- and nothing else, and the row it names decides whose session is created. The
-- WebAuthn specification allows up to 1023 bytes, which BYTEA holds without a
-- declared width.
--
-- public_key is the COSE-encoded public half. There is deliberately no secret in
-- this table at all: unlike api_tokens, which stores a hash because the
-- plaintext must never be recoverable, a public key is public by construction
-- and reading the table gives an attacker nothing to sign with.
--
-- sign_count is the authenticator's monotonic counter, kept so a clone that
-- replays an older assertion can be spotted. It is BIGINT because the protocol's
-- counter is an unsigned 32-bit integer, which does not fit INTEGER.
--
-- The four flag columns are not decoration: go-webauthn refuses a login whose
-- backup-eligibility flag disagrees with the one recorded at registration, so
-- backup_eligible has to survive here or every synced platform passkey — which
-- is most of them — would fail on its second use. backup_state, user_present and
-- user_verified are stored beside it because they are latched the same way and
-- describe the same credential record.
--
-- aaguid, attestation_type and attestation_format record what kind of
-- authenticator this was, which is what lets a later release show "your YubiKey"
-- instead of "a passkey" without asking anybody to register again.
--
-- name is what the owner calls it ("phone", "work laptop"); empty is allowed,
-- because a person adding their first passkey should not be stopped by a form
-- field. last_used_at is nullable — a credential that has never signed anybody
-- in has no such moment, and a zero timestamp would claim it did.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE passkey_credentials (
    id                 VARCHAR(32) PRIMARY KEY,
    user_uid           VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    credential_id      BYTEA       NOT NULL UNIQUE,
    public_key         BYTEA       NOT NULL,
    sign_count         BIGINT      NOT NULL DEFAULT 0,
    transports         TEXT[]      NOT NULL DEFAULT '{}',
    aaguid             BYTEA       NOT NULL DEFAULT '\x'::bytea,
    attestation_type   TEXT        NOT NULL DEFAULT '',
    attestation_format TEXT        NOT NULL DEFAULT '',
    backup_eligible    BOOLEAN     NOT NULL DEFAULT false,
    backup_state       BOOLEAN     NOT NULL DEFAULT false,
    user_present       BOOLEAN     NOT NULL DEFAULT false,
    user_verified      BOOLEAN     NOT NULL DEFAULT false,
    name               TEXT        NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at       TIMESTAMPTZ
);

CREATE INDEX idx_passkey_credentials_user_uid ON passkey_credentials (user_uid);
