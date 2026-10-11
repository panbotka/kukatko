-- 0093_notification_prefs_channel: a notification preference is per delivery
-- channel, not only per kind.
--
-- A kind can now reach an account through more than one channel — a push to its
-- devices, an e-mail — and the account chooses for each separately. The first
-- e-mail channel is the administrators' "a registration is waiting for approval"
-- notice, which until now went to every approver unconditionally and is opt-in
-- from here on (its default lives in internal/notification, like every other).
--
-- Every row stored so far was a push choice: before this migration a preference
-- only ever gated the push. The column's default says so, which is also what
-- backfills the existing rows. Like kind, channel carries no CHECK on its values:
-- adding a channel costs a constant in internal/notification, never a migration.
-- The primary key widens to the pair, so one account may hold a different choice
-- per channel of the same kind.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE notification_prefs
    ADD COLUMN channel TEXT NOT NULL DEFAULT 'push' CHECK (channel <> '');

ALTER TABLE notification_prefs DROP CONSTRAINT notification_prefs_pkey;
ALTER TABLE notification_prefs ADD PRIMARY KEY (user_uid, kind, channel);

COMMENT ON TABLE notification_prefs IS
    'Whether an account wants a notification kind through a delivery channel. An absent row means the default for that kind and channel (defined in internal/notification).';
COMMENT ON COLUMN notification_prefs.channel IS
    'Delivery channel the choice is about: push or email. Rows from before channels existed are push choices.';
