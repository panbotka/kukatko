-- 0083_photo_tasks_options: the answers a question offers.
--
-- Most questions the agent opens are a choice — "1936, or 1938?", "1950, or
-- 1955?" — and a batch waiting in review needs a yes or a no. Until now the
-- person answering had to scroll past the photographs to the thread and type a
-- sentence, on a phone, and the agent then had to interpret free text. A task
-- may now carry up to five short answer options. The client draws them as
-- buttons under the question; tapping one posts an ordinary comment whose body
-- is the option text, verbatim. That keeps the thread the single record of the
-- conversation and lets the agent match the answer against its options exactly.
--
-- Only the count is checked here. The per-option rules (trimmed, non-empty, at
-- most 60 characters, distinct) live in internal/phototask, where the error can
-- say which option broke which rule; the array bound is the one thing worth
-- pinning at the storage layer, so a malformed write can never grow the row.
-- An empty array means "no options": the question is answered in free text.

ALTER TABLE photo_tasks
    ADD COLUMN options TEXT[] NOT NULL DEFAULT '{}'
        CONSTRAINT photo_tasks_options_count CHECK (cardinality(options) <= 5);

COMMENT ON COLUMN photo_tasks.options IS
    'Up to five answer options a client draws as buttons; choosing one posts a comment with exactly that text. Empty = free-text answer.';
