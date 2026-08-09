-- 0056_search_history: what each user searched for, most recent first.
--
-- Searching a family library is repetitive by nature — the same person, the same
-- album, the same "svatba 1974" typed again next week, on a different device.
-- Until now every query was thrown away the moment the page was left, so the
-- query language's whole point (a filter expression worth composing) had to be
-- re-composed from memory each time.
--
-- A row is one query one user ran, kept verbatim as a plain string. It is not
-- parsed here and never will be: the history's job is to hand the exact text
-- back to the search box, and `internal/query` remains the only thing that reads
-- meaning into it. The 1–500 character CHECK mirrors
-- searchhistory.MaxQueryLength so a query that skipped the service layer still
-- cannot store an empty or unbounded string; char_length counts characters, so a
-- Czech query means the same as an English one.
--
-- The primary key is (user_uid, query), which is what makes recording an
-- idempotent upsert: running the same search again moves its searched_at forward
-- instead of appending a duplicate. Deduplication is therefore on the exact
-- trimmed text — "Praha" and "praha" are two entries, because they are two
-- different things to hand back to the box, and folding them would mean choosing
-- which spelling the user gets to see.
--
-- The history is strictly per-user: nothing reads across user_uid, and the
-- CASCADE means deleting an account takes its searches with it. There is no
-- retention job — searchhistory.MaxEntries caps each user's history at a fixed
-- number of rows on every write, so the table's size is bounded by the number of
-- accounts rather than by how much anyone searches. This migration is wrapped in
-- a transaction by the runner.

CREATE TABLE search_history (
    user_uid    VARCHAR(32) NOT NULL REFERENCES users (uid) ON DELETE CASCADE,
    query       TEXT        NOT NULL CHECK (char_length(query) BETWEEN 1 AND 500),
    searched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_uid, query)
);

-- The only read path, and the one the per-write prune uses to decide which rows
-- fall off the end: one user's queries, newest first.
CREATE INDEX idx_search_history_recent ON search_history (user_uid, searched_at DESC);
