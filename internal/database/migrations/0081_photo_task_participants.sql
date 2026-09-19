-- 0081_photo_task_participants: who is on a task.
--
-- A task is a question, and a question has people around it: whoever asked it,
-- whoever was asked, and whoever turned up in the thread with something to say.
-- Until now the only name a task carried was its creator's, which made one
-- reasonable question unanswerable — "what am I involved in?". The queue could
-- only be read as one shared list, so a person with two questions waiting on
-- them had to find them among everybody else's.
--
-- # Participation is mostly not a decision
--
-- The common case is not that somebody is assigned a task; it is that they act
-- on one. Answering a question, moving its state, adding photographs to it —
-- each of those is a stronger statement of involvement than any checkbox, and
-- having to also tick the checkbox would mean the list is wrong whenever
-- somebody forgets. So the writes join their actor here, idempotently, and the
-- row appears as a side effect of the work.
--
-- Being put on a task by somebody else is the other, rarer case: asking a
-- particular person, before they have done anything, is how a question reaches
-- the one relative who would know. `added_by` records that difference and is the
-- whole distinction between the two: NULL means "joined by acting", a uid means
-- "was asked, by this person". The column is therefore not bookkeeping — the UI
-- reads it to tell "Anna is on this" from "you put Anna on this".
--
-- # Why not derive it
--
-- Participation could be computed: the creator, plus the authors in the thread,
-- plus the actors in the audit trail. That query is expensive, impossible to
-- filter a paged listing by, and silently wrong the moment a comment is soft
-- deleted or an audit row ages out. And it could never express the rarer case
-- above, which has no act to derive from. A small explicit table is both cheaper
-- and more truthful.
--
-- # Deletes
--
-- Both sides CASCADE. A deleted task takes its participation with it (the task
-- is the only thing the row is about), and a deleted account takes its own rows
-- rather than leaving a membership nobody can resolve to a name — unlike
-- `photo_tasks.created_by`, which is SET NULL because "opened by somebody who
-- has since left" is still a fact worth keeping about the question itself.
-- `added_by` is SET NULL: losing the account that asked somebody does not undo
-- the asking.

CREATE TABLE photo_task_participants (
    task_uid  VARCHAR(32) NOT NULL REFERENCES photo_tasks (uid) ON DELETE CASCADE,
    user_uid  VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- NULL = joined by acting on the task; a uid = was put here by that person.
    added_by  VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,

    PRIMARY KEY (task_uid, user_uid)
);

-- "What am I on?" — the listing this table exists for, most recent first.
CREATE INDEX idx_photo_task_participants_user
    ON photo_task_participants (user_uid, joined_at DESC);

-- Every task that already exists was opened by somebody, and opening one is
-- participating in it. Backfilling means the feature does not start out looking
-- broken for the tasks that predate it.
INSERT INTO photo_task_participants (task_uid, user_uid, joined_at)
SELECT uid, created_by, created_at
FROM photo_tasks
WHERE created_by IS NOT NULL
ON CONFLICT DO NOTHING;

-- Likewise everybody who has already written in a task's thread.
INSERT INTO photo_task_participants (task_uid, user_uid, joined_at)
SELECT c.task_uid, c.author_uid, min(c.created_at)
FROM comments c
WHERE c.task_uid IS NOT NULL AND c.author_uid IS NOT NULL
GROUP BY c.task_uid, c.author_uid
ON CONFLICT DO NOTHING;
