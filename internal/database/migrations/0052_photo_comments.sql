-- 0052_photo_comments: per-photo comment threads.
--
-- A photo in a family library is a conversation starter — "who is the boy on the
-- left?", "this was the summer before the barn burned down" — and until now there
-- was nowhere to put that. A comment is a short plain-text note by one user on one
-- photo; every authenticated user may write one, viewers included, because
-- commenting is social participation rather than curation of the library.
--
-- Bodies are plain text and stay that way: nothing here is HTML or markdown, and
-- the server never renders it. The 1–2000 character CHECK mirrors the Go-side
-- validation in internal/comments so a body that skipped the service layer (a
-- direct SQL write, a future job) still cannot store an empty or unbounded note.
-- char_length counts characters, not bytes, so the limit means the same thing to
-- a Czech comment as to an English one.
--
-- Deletion is soft: deleted_at is stamped and the row stays. A thread whose middle
-- comment vanished would lose the shape of the conversation for the audit trail
-- that references it, and a delete must be explainable afterwards ("who removed
-- what"), so every listing filters on deleted_at IS NULL instead. edited_at stays
-- NULL until the author first rewrites the body, which is what lets a client mark
-- a comment as edited without comparing timestamps.
--
-- photo_uid CASCADEs: purging a photo is permanent and irreversible, and its
-- conversation has nothing left to be about. author_uid is ON DELETE SET NULL, so
-- deleting a user account does not silently rewrite the history of a thread — the
-- comment survives authorless (rendered without a name, and no longer editable by
-- anyone, since the author-only check can never match a NULL author). This
-- migration is wrapped in a transaction by the runner.

CREATE TABLE photo_comments (
    uid        VARCHAR(32)  PRIMARY KEY,
    photo_uid  VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- Who wrote it; NULL once the account is deleted.
    author_uid VARCHAR(32)  REFERENCES users (uid) ON DELETE SET NULL,
    body       TEXT         NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- NULL until the author first edits the body.
    edited_at  TIMESTAMPTZ,
    -- NULL while the comment is live; stamped on a soft delete.
    deleted_at TIMESTAMPTZ
);

-- The only hot access path: one photo's live comments, oldest first, and the
-- count behind the detail view's badge. The partial predicate keeps deleted rows
-- out of the index entirely, so a thread that was heavily edited still reads as
-- cheaply as a fresh one.
CREATE INDEX idx_photo_comments_photo ON photo_comments (photo_uid, created_at)
    WHERE deleted_at IS NULL;
