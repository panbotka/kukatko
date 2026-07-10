-- 0021_user_note: free-text administrative note on user accounts.
--
-- note lets an administrator record why an account exists or who the person is.
-- It is nullable, so existing rows simply carry NULL; the auth store COALESCEs
-- it to the empty string on read, which keeps the Go model a plain string and
-- makes "no note" and "empty note" indistinguishable to callers.
--
-- The note is admin-only: it is deliberately withheld from the session and
-- current-user payloads that any authenticated user can read.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE users ADD COLUMN note TEXT;
