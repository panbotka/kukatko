-- 0086_push_subscriptions: the browsers and devices an account receives Web
-- Push notifications on.
--
-- A push subscription is what a browser hands back when a person allows
-- notifications: an endpoint URL at the browser vendor's push service plus the
-- two client keys the payload is encrypted to (RFC 8291). internal/push stores
-- them here and sends through them; nothing else can reach that browser.
--
-- One account may hold several — a phone and a laptop are two subscriptions —
-- so the table is a list per user rather than a column on `users`. user_uid
-- cascades: a subscription of an account that no longer exists would deliver
-- somebody else's notifications to nobody.
--
-- endpoint is UNIQUE because it *is* the identity of a subscription: the same
-- browser subscribing again (after a permission reset, or because the page
-- asks on every visit) hands back the same endpoint, and that must update the
-- row it already has rather than add a second one that would deliver every
-- notification twice. It is TEXT because the push services do not promise a
-- length; internal/push caps what it accepts.
--
-- p256dh and auth are the client keys exactly as the browser reports them
-- (base64url). Neither is a secret of ours: p256dh is a public key, and auth is
-- a salt that only means something together with the endpoint. There is no
-- secret in this table at all — the private VAPID key lives in the environment.
--
-- user_agent is only there so a person looking at their devices can tell the
-- phone from the laptop; empty is allowed, because a browser that sends none
-- must still be able to subscribe.
--
-- last_used_at is nullable — a subscription nothing was ever sent through has
-- no such moment, and a zero timestamp would claim it did. failure_count counts
-- the failed sends since the last successful one and last_failure_at is when
-- the latest of them happened, so the caller can retire a subscription that
-- keeps failing without a push service ever saying it is gone.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE push_subscriptions (
    id              VARCHAR(32) PRIMARY KEY,
    user_uid        VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    endpoint        TEXT        NOT NULL UNIQUE,
    p256dh          TEXT        NOT NULL,
    auth            TEXT        NOT NULL,
    user_agent      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ,
    failure_count   INTEGER     NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    last_failure_at TIMESTAMPTZ
);

CREATE INDEX idx_push_subscriptions_user_uid ON push_subscriptions (user_uid);
