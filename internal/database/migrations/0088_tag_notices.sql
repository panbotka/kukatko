-- 0088_tag_notices: the photos an account is waiting to hear it was tagged on.
--
-- When somebody spends an afternoon naming the faces of a village event, the
-- people they name must not receive forty separate notifications. Tagging is
-- therefore collected per account: the first tag opens a window, every photo
-- tagged inside it is counted, and when the window closes one notification says
-- how many (internal/tagnotifyjob). This table is what the window collects — one
-- row per (account, photo) still waiting to be announced, with when it was
-- recorded.
--
-- The primary key is the pair, so tagging the same person twice on one photo —
-- two markers, or an assignment undone and redone — is still one photo. The row
-- is written by the face-assignment write path in the assignment's own
-- transaction (an assignment that rolls back announces nothing), deleted again
-- when the person is untagged before the window closes (a notification must
-- never name a photo the person is no longer on), and consumed — read and
-- deleted in one statement — by the `tag_notify` job that closes the window.
--
-- Both sides cascade. An account deleted before its window closes takes its
-- notices with it and the job finds nothing; a photo deleted outright simply
-- drops out. A photo archived, hidden or made private while it waits is left in
-- place and filtered by the job instead, which re-reads the photo's state when it
-- runs.
--
-- There is no index on recorded_at: nothing reads by age — the job reads one
-- account's rows, which the primary key's leading column already serves.
--
-- This migration is wrapped in a transaction by the runner.

CREATE TABLE tag_notices (
    user_uid    VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    photo_uid   VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uid, photo_uid)
);

COMMENT ON TABLE tag_notices IS
    'Photos an account was tagged on that are waiting for the aggregated "you were tagged in N photos" notification (internal/tagnotifyjob).';

-- The reverse direction, which the photo cascade walks.
CREATE INDEX idx_tag_notices_photo ON tag_notices (photo_uid);
