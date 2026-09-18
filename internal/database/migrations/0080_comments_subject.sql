-- 0080_comments_subject: a comment thread may now hang off a task, not only a photo.
--
-- Comments were built for one picture at a time: "who is the boy on the left".
-- Tasks (migration 0079) need the same thing for a group — a question asked of
-- whoever knows the answer, and the answer written underneath it. That is the
-- same conversation with a different subject, so it gets the same table rather
-- than a parallel one: one thread implementation, one rate limit, one set of
-- audit actions, one soft-delete rule, one piece of client code. A second table
-- would have duplicated every one of those, and the two copies would have drifted.
--
-- The table is renamed because its name would otherwise be a lie. Nothing about
-- the rows changes: the same uids, the same bodies, the same soft deletes.
--
-- # Two columns rather than a type/uid pair
--
-- The subject could have been stored as ('photo', uid) — a kind and a bare
-- identifier. It is stored as two nullable foreign keys instead, exactly one of
-- which is filled, because that is the shape the database can enforce. A bare
-- uid column cannot reference anything, so nothing would stop a comment on a
-- photograph that no longer exists, and nothing would clean up after a deleted
-- task. With real foreign keys both are impossible and both cascades are the
-- database's job. The cost is one column per subject kind, paid only when a
-- third kind ever appears.
--
-- The CHECK is what makes the pair behave like a single field: exactly one
-- subject, never both, never neither.

ALTER TABLE photo_comments RENAME TO comments;

COMMENT ON TABLE comments IS
    'One person''s plain-text note on one subject: a photograph, or a task. Exactly one of photo_uid and task_uid is set.';

-- A photo comment still points at its photo; a task comment leaves it null.
ALTER TABLE comments ALTER COLUMN photo_uid DROP NOT NULL;

ALTER TABLE comments
    ADD COLUMN task_uid VARCHAR(32) REFERENCES photo_tasks (uid) ON DELETE CASCADE;

-- Exactly one subject. Existing rows all have a photo_uid and no task_uid, so
-- the constraint validates without a rewrite.
ALTER TABLE comments
    ADD CONSTRAINT comments_one_subject CHECK (num_nonnulls(photo_uid, task_uid) = 1);

-- The reading index follows the table's new name; it is the same index.
ALTER INDEX idx_photo_comments_photo RENAME TO idx_comments_photo;

-- The same access pattern for the other subject: one task's live thread, oldest
-- first. Partial, because a soft-deleted comment is invisible to every read.
CREATE INDEX idx_comments_task ON comments (task_uid, created_at) WHERE deleted_at IS NULL;

-- Likewise the library-wide "since my last visit" index from 0053.
ALTER INDEX idx_photo_comments_created_at RENAME TO idx_comments_created_at;
