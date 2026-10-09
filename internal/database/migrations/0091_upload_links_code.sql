-- 0091_upload_links_code: an upload link's short code, readable again.
--
-- 0090 stored the code only as its SHA-256, so the URL existed in the creator's
-- create response and nowhere else. In practice a curator posts the link once
-- and then needs it again a week later for a second group chat, and the only
-- way out was a new link. The code is therefore kept readable next to the hash,
-- the same decision the registration secret in instance_settings made: it is
-- shown only to whoever may manage the link (its creator or an admin), and a
-- leaked listing is answered by "new code", which replaces both columns.
--
-- code is nullable: links created before this migration have only a hash, and
-- nothing here may change their URL. The column is only added — code_hash,
-- expiry and state of the existing rows stay exactly as they are. Such a link
-- gets its code back through "restore code", which stores the code a manager
-- types only once its hash matches code_hash. Lookup by code still goes
-- through code_hash, the one column with an index.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE upload_links ADD COLUMN code TEXT;

COMMENT ON COLUMN upload_links.code IS
    'The readable short code; NULL for a link created before 0091 until its manager restores it. Always hashes to code_hash.';
