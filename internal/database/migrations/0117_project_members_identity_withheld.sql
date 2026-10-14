-- 0117_project_members_identity_withheld: a membership made by an exact-match
-- add does not show the project who the account is until it enters.
--
-- A project admin adds an existing account by its exact username or e-mail
-- address (internal/project AddByIdentifier). The answer is the same 202 for a
-- hit and a miss, but the members list used to show a hit at once with the
-- account's name, display name, last sign-in anywhere on the instance and its
-- picture — so the add was an account-existence and activity oracle one guess
-- at a time, and it pulled a stranger's identity into a project it never
-- chose. The membership is still made (the design's §4.1: "if that account
-- exists, it has been added"), but with identity_withheld set: the members list
-- shows only the row and its role, and the account cannot be named in the
-- project (directory, mentions, task participants) until it enters the project
-- once, which clears the flag (internal/auth enterProject).
--
-- Every existing membership was made by registration, an invitation, the
-- system administrator or the backfill, never blind, so the default false is
-- right for all of them.
ALTER TABLE project_members ADD COLUMN IF NOT EXISTS identity_withheld BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN project_members.identity_withheld IS
    'True for an exact-match add the account has not entered yet: its name, last sign-in and picture are withheld from the project.';
