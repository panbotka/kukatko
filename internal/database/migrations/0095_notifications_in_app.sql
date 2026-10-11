-- 0095_notifications_in_app: the in-app inbox — which notification records the
-- bell in the navbar lists.
--
-- Until now a notifications row existed only behind a push: the record a tapped
-- push opens. A third channel, in-app, makes the record itself the delivery,
-- listed in the owner's inbox. One row still serves every channel of one
-- notification (the push opens it, the e-mail links to it, the inbox lists it),
-- so whether it is *listed* is a property of the row, not of its existence: a
-- row written only to back a push or an e-mail the owner wanted, while they
-- turned the in-app channel off, must not turn up in the inbox. Hence a flag
-- rather than a separate table — the uid, the text and the frozen photo set are
-- the same either way.
--
-- Existing rows were all written for a push, before the in-app channel existed,
-- so nobody asked for them in an inbox: they default to false.
--
-- The two partial indexes serve the inbox's two reads: the newest-first page
-- (the uid breaks a tie in created_at, so a page boundary is stable) and the
-- unread count the bell polls. Both cover only listed rows.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE notifications
    ADD COLUMN in_app BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN notifications.in_app IS
    'Listed in the owner''s in-app inbox. False for a record that only backs a push or an e-mail.';

CREATE INDEX idx_notifications_inbox ON notifications (user_uid, created_at DESC, uid)
    WHERE in_app;

CREATE INDEX idx_notifications_inbox_unread ON notifications (user_uid)
    WHERE in_app AND read_at IS NULL;
