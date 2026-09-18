-- 0079_photo_tasks: a question about a group of photographs, and who is on the move.
--
-- Curating an inherited library is not one long stream of individual edits. It
-- arrives in batches that share a doubt: sixty-seven scans whose printed caption
-- disagrees with their stored date, forty photographs dated before their camera
-- was on sale, six copies of one picture carrying three different years. Each
-- batch ends at a question only a person can answer, and until now the library
-- had nowhere to keep one. The question lived in a file in somebody's git
-- checkout and the group lived as a label on the photographs, which is why both
-- were awkward: the file nobody outside the repository ever opened, and the
-- label could not be removed when the work was done without also erasing the
-- record of what the work had touched.
--
-- A task is that question made addressable. It has a URL to send to whoever
-- knows the answer, a frozen list of the photographs it is about, the state of
-- play, and — through the comments table, see migration 0080 — the conversation
-- that settles it.
--
-- # Why the list of photographs is frozen
--
-- Membership is an explicit list of uids, not a stored query. The query that
-- produced the group is kept too (source_query), but only as evidence: it is
-- never re-run. This is deliberate and it is the whole point. A task exists
-- because the data is wrong, and it is answered by fixing that data — so a group
-- defined by "photographs dated before their camera existed" would empty itself
-- the moment the work was done, taking with it the record of which photographs
-- were changed and why. The frozen list is what lets a closed task stay
-- meaningful: it is the receipt.
--
-- # Why a closed task must say how it ended
--
-- "Rejected" is a real outcome here, not a failure — deciding that a doubt is
-- unfounded, or that a date cannot be established, is as much a result as
-- changing it. What must never happen is a task that is closed and silent, since
-- the next person to look at those photographs would have no way to tell a
-- considered decision from an abandoned one. The constraints below therefore
-- make the two halves inseparable: a task in a closed state carries both its
-- closing timestamp and a non-empty resolution, and an open one carries neither.
--
-- # Why state_at exists next to updated_at
--
-- The state says whose move it is, but only while somebody remembers to change
-- it. state_at records when it last moved, which — compared against the time of
-- the newest comment — answers the question the state alone cannot: has anybody
-- replied since we last looked? That comparison is what an agent polls, and it
-- stays true even when nobody advanced the state by hand.

CREATE TABLE photo_tasks (
    uid VARCHAR(32) PRIMARY KEY,
    -- The question itself, in one line, as a person would ask it. It is the page
    -- heading and the text of the link that gets sent around, so it is required
    -- and short: "In which year was house no. 2 rebuilt?", not "date batch 11".
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    -- The context: what was noticed, what is already known, what would settle it.
    -- Markdown, rendered by the client through the sanitising renderer.
    body TEXT NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
    -- Whose move it is. 'question' waits for a person, 'working' for whoever is
    -- doing the edits, 'review' for the owner to approve a finished batch;
    -- 'done' and 'rejected' are the two closed states.
    state VARCHAR(16) NOT NULL DEFAULT 'question'
        CHECK (state IN ('question', 'working', 'review', 'done', 'rejected')),
    -- How it ended, in the words of whoever closed it. Empty while open.
    resolution TEXT NOT NULL DEFAULT '' CHECK (char_length(resolution) <= 4000),
    -- The search that produced the group, kept verbatim as evidence of the rule
    -- behind it. Never executed by the server; see the header.
    source_query TEXT NOT NULL DEFAULT '' CHECK (char_length(source_query) <= 2000),
    created_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- When the state last moved. Compared against the newest comment to tell
    -- "somebody has replied since" from "nothing has happened".
    state_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ,
    closed_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    -- A closed task is always explained, and always stamped.
    CONSTRAINT photo_tasks_closed_is_explained CHECK (
        state NOT IN ('done', 'rejected')
        OR (closed_at IS NOT NULL AND char_length(resolution) > 0)
    ),
    -- An open one carries no closing marks, so reopening cannot leave a stale
    -- "closed by" hanging off a task that is demonstrably open.
    CONSTRAINT photo_tasks_open_is_not_closed CHECK (
        state IN ('done', 'rejected')
        OR (closed_at IS NULL AND closed_by IS NULL)
    )
);

COMMENT ON TABLE photo_tasks IS
    'A question about a frozen group of photographs: what is being asked, who is on the move, and how it ended.';

-- The listing is always "the open ones, most recently touched first", so the
-- index carries the order as well as the filter.
CREATE INDEX idx_photo_tasks_state ON photo_tasks (state, updated_at DESC);

CREATE TABLE photo_task_photos (
    task_uid VARCHAR(32) NOT NULL REFERENCES photo_tasks (uid) ON DELETE CASCADE,
    photo_uid VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_uid, photo_uid)
);

COMMENT ON TABLE photo_task_photos IS
    'The frozen membership of a task: which photographs the question is about. Explicitly listed, never derived from a query.';

-- The reverse direction: which tasks is this photograph part of. Asked by the
-- photo detail, so a person looking at a picture finds the open question on it.
CREATE INDEX idx_photo_task_photos_photo ON photo_task_photos (photo_uid);
