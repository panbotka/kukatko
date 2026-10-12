-- 0099_comments_album: a comment thread may hang off an album, too.
--
-- Migration 0080 made the comment's subject a pair of nullable foreign keys —
-- one per subject kind, exactly one filled — and said the cost of that shape is
-- "one column per subject kind, paid only when a third kind ever appears". This
-- is the third kind. An album is where a family talks about a whole event
-- ("the wedding — who else was there?") rather than one picture of it, and that
-- is the same conversation again with a different subject: one table, one rate
-- limit, one set of audit actions, one soft-delete rule, one piece of client
-- code. The pattern is followed rather than replaced: a real foreign key, so a
-- comment cannot outlive its album and the cascade stays the database's job.

ALTER TABLE comments
    ADD COLUMN album_uid VARCHAR(32) REFERENCES albums (uid) ON DELETE CASCADE;

COMMENT ON TABLE comments IS
    'One person''s plain-text note on one subject: a photograph, a task, or an album. Exactly one of photo_uid, task_uid and album_uid is set.';

-- Still exactly one subject, now out of three. Existing rows all have exactly
-- one of the first two set and the new column NULL, so the widened constraint
-- validates without a rewrite.
ALTER TABLE comments DROP CONSTRAINT comments_one_subject;
ALTER TABLE comments
    ADD CONSTRAINT comments_one_subject CHECK (num_nonnulls(photo_uid, task_uid, album_uid) = 1);

-- The same access pattern as the other two subjects: one album's live thread,
-- oldest first. Partial, because a soft-deleted comment is invisible to every read.
CREATE INDEX idx_comments_album ON comments (album_uid, created_at) WHERE deleted_at IS NULL;
