-- 0098_comment_mentions: who a comment mentions.
--
-- A comment may name people with `@[Display Name](user:<uid>)` tokens, which
-- the composer inserts when somebody is picked from the directory and which
-- the renderer shows as a highlighted "@Display Name". The token carries the
-- account's uid rather than only its name, so a rename never breaks an old
-- mention and the name in the token is merely what to show when the account
-- is gone.
--
-- The mentioned set is also stored here, one row per (comment, account),
-- parsed from the body when the comment is written or edited. The table feeds
-- the comment_mention notification — a mention is announced only when it is
-- new on the comment, so editing a typo does not notify everybody again — and
-- a later "mentions of me" listing, which could not be answered by scanning
-- every body. Position is the order of first mention in the body, so a read
-- lists the people in the order the writer named them.
--
-- Both foreign keys cascade: a purged comment takes its mentions with it, and
-- a deleted account stops being mentioned anywhere (the token in the body still
-- renders, with the stored name and no link — the row here was only ever the
-- lookup). A soft-deleted comment keeps its rows, as it keeps everything else.
CREATE TABLE comment_mentions (
    comment_uid VARCHAR(32) NOT NULL REFERENCES comments (uid) ON DELETE CASCADE,
    user_uid    VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    position    INTEGER     NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_uid, user_uid)
);

COMMENT ON TABLE comment_mentions IS
    'The accounts a comment mentions with an @[name](user:uid) token, in order of first mention.';

-- "Mentions of me": every comment naming one account, newest first.
CREATE INDEX idx_comment_mentions_user ON comment_mentions (user_uid, created_at DESC);
