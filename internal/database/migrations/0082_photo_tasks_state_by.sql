-- 0082_photo_tasks_state_by: who moved the state last.
--
-- A task is a conversation between two sides — typically an agent working
-- through `kukatko ctl` and the person it asked — and each side needs to know
-- whose move it is. The state says that only nominally: `question` is meant to
-- wait on a person, but the agent asks its questions in `working` too, and the
-- human answers in whatever state the task happens to be in. What actually
-- settles "is it my turn?" is who acted last: the last state change, the task's
-- creation, or the newest comment, whichever is newest.
--
-- Two of those three already name their actor (created_by, and a comment's
-- author_uid). The state change did not: state_at records *when* the state
-- moved, closed_by records who closed it, but nobody recorded who moved it into
-- `working` or `review`. state_by is that missing name. It is stamped in the
-- same transaction as state_at on every state change and stays NULL on the rows
-- that predate it, where a reader treats it as the creator — the best guess
-- available, and the truth for a task nobody has advanced.
--
-- SET NULL like created_by: losing the account does not un-move the state.

ALTER TABLE photo_tasks
    ADD COLUMN state_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL;

COMMENT ON COLUMN photo_tasks.state_by IS
    'Who last moved the state (stamped with state_at). NULL on rows from before the column existed: read as the creator.';
