-- 0060_users_subject: an account may say which person of the library it is.
--
-- Kukátko knows who is logged in and it knows the people on the photographs, but
-- nothing connected the two. One nullable pointer from the account to a subject
-- is what lets the app answer "which photos am I on", resolve `person:me`, and
-- draw a face instead of a letter beside what somebody wrote.
--
-- It is deliberately NOT unique. A family shares accounts: the household login
-- and a personal one can both legitimately be the same person, and refusing the
-- second would make the operator choose which of two true statements to keep.
--
-- ON DELETE SET NULL, not CASCADE: deleting a person from the library must never
-- delete an account. A link that has lost its subject simply becomes no link, and
-- every read path already has to survive that (the column is nullable from the
-- start).
--
-- The column does not touch updated_at. Saying who you are is a change to the
-- account, but the user-administration screens sort and read updated_at as "the
-- profile was edited by an administrator", and a self-service link is not that.

-- The constraint is named explicitly rather than left to Postgres, because the
-- library wipe has to name it: TRUNCATE refuses to empty a table that an
-- unlisted table references, whatever the rows say, and users is deliberately
-- not in the wipe's list. `kukatko maintenance reset` therefore drops this one
-- constraint and puts it straight back, inside the same transaction as the
-- truncation (see internal/reset, subjectFKConstraint). Renaming it here without
-- renaming it there breaks the reset.
ALTER TABLE users
    ADD COLUMN subject_uid VARCHAR(32),
    ADD CONSTRAINT users_subject_uid_fkey
        FOREIGN KEY (subject_uid) REFERENCES subjects (uid) ON DELETE SET NULL;

-- Deleting a subject has to find the accounts pointing at it to null them out,
-- and a merge deletes subjects in bulk. Without an index that is a sequential
-- scan of users per deleted subject. Partial on the linked rows: almost every
-- account has no link, and those rows are of no interest to either the FK's
-- cleanup or the "who is this subject?" lookup.
CREATE INDEX idx_users_subject_uid ON users (subject_uid)
    WHERE subject_uid IS NOT NULL;
