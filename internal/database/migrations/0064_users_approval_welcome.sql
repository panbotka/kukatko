-- 0064_users_approval_welcome: an account records when it was approved and when
-- its owner last saw the welcome.
--
-- Two facts about an account had nowhere to live. The first is approval: today
-- every account is made by an administrator, but self-service registration will
-- create accounts that wait for one, and "waiting" has to be a state the roster
-- can show. The second is the first-run welcome: the instance settings hold the
-- text (migration 0062), and until now nothing remembered who had already read
-- it, so it could only ever be shown always or never.
--
-- Both are nullable timestamps rather than booleans. A flag would answer "yes"
-- and lose "when", and both questions are asked in practice — an administrator
-- reviewing the roster wants to know how long somebody has been waiting, and the
-- welcome is re-shown when its text changes, which needs a comparison of times.
-- NULL is the honest absence: "registered, waiting for an administrator" and
-- "never seen".
--
-- approved_at is deliberately NOT the inverse of the existing disabled flag, and
-- no read may collapse the two. "Never approved" is an account that has not yet
-- been let in; "approved and later blocked" is one that was let in and then shut
-- out again. They look the same to a boolean and mean opposite things to the
-- person holding the account, so the user listing carries both columns.
--
-- Every row that exists when this migration runs is backfilled as approved: the
-- only way to get an account before this point was for an administrator (or the
-- bootstrap) to create one, which is exactly what approval records. The stamp is
-- created_at rather than now(), because that is when the approval actually
-- happened. welcome_seen_at stays NULL for everybody — nobody has been shown the
-- welcome yet, and pretending otherwise would silently swallow the first
-- showing.
--
-- Neither column has a database default. An INSERT that says nothing about
-- approval means "not approved", and that must stay the safe answer once
-- registration writes rows of its own; the administrator paths in internal/auth
-- pass the stamp explicitly.
--
-- No index: users is a table of tens of rows read in full by the admin roster,
-- and neither column is ever a search key.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE users
    ADD COLUMN approved_at      TIMESTAMPTZ,
    ADD COLUMN welcome_seen_at  TIMESTAMPTZ;

UPDATE users SET approved_at = created_at;
