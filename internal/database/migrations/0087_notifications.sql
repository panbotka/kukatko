-- 0087_notifications: notification records, their frozen photo sets and the
-- per-account choice of which kinds an account wants.
--
-- A push notification has to survive being tapped. "You were tagged in 12
-- photos" must open exactly those 12 — even after somebody tags a thirteenth,
-- even the next morning — so a notification is a record, not a transient
-- message, and its photographs are a frozen list rather than a query re-run on
-- open. The shape follows photo_tasks / photo_task_photos (migration 0079).
-- internal/notification owns all three tables.
--
-- notifications holds one notification as it was sent: the account it belongs
-- to, its kind, the title and body text and the deeplink path it opens. The
-- account cascades — a notification for nobody means nothing. kind is plain
-- text with no CHECK on purpose: the set of kinds lives in Go, and adding one
-- must never cost a migration. read_at is nullable: an unread notification has
-- no such moment. created_at carries the retention purge (read ones go after
-- one threshold, unread ones after a longer one), hence its own index.
--
-- notification_photos is the frozen set. position keeps the order the sender
-- chose — unique per notification, so two photographs can never claim the same
-- slot — and the pair is the primary key, so one photograph appears once. The
-- photo cascades: a deleted photograph simply drops out of every set, and a
-- notification whose whole set is gone stays a legitimate record the page says
-- "the photos are gone" about. An archived, hidden or private photograph does
-- NOT drop out — whether it may be shown depends on who asks, so the reader
-- filters. A notification with no rows here is legitimate too (the pending
-- registration kind has no photographs at all).
--
-- notification_prefs is one row per (account, kind) saying whether that
-- account wants that kind. An absent row means the kind's default, which is
-- what lets a new kind ship without a backfill. It carries no foreign key to
-- anything but users: a preference is part of the account, not of the library.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE notifications (
    uid        VARCHAR(32) PRIMARY KEY,
    user_uid   VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind <> ''),
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL DEFAULT '',
    link       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at    TIMESTAMPTZ
);

COMMENT ON TABLE notifications IS
    'One notification as it was sent to one account: kind, text, deeplink, when it was read. Its photographs are frozen in notification_photos.';

-- An account's notifications, newest first.
CREATE INDEX idx_notifications_user ON notifications (user_uid, created_at DESC);
-- The retention purge walks by age alone.
CREATE INDEX idx_notifications_created ON notifications (created_at);

CREATE TABLE notification_photos (
    notification_uid VARCHAR(32) NOT NULL REFERENCES notifications (uid) ON DELETE CASCADE,
    photo_uid        VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    position         INTEGER     NOT NULL CHECK (position >= 0),
    PRIMARY KEY (notification_uid, photo_uid),
    CONSTRAINT notification_photos_position_unique UNIQUE (notification_uid, position)
);

COMMENT ON TABLE notification_photos IS
    'The frozen, ordered photo set of a notification. Explicitly listed, never derived from a query; the reader applies visibility.';

-- The reverse direction, which the photo cascade walks.
CREATE INDEX idx_notification_photos_photo ON notification_photos (photo_uid);

CREATE TABLE notification_prefs (
    user_uid   VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    kind       TEXT        NOT NULL CHECK (kind <> ''),
    enabled    BOOLEAN     NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uid, kind)
);

COMMENT ON TABLE notification_prefs IS
    'Whether an account wants a notification kind. An absent row means the kind''s default (defined in internal/notification).';
