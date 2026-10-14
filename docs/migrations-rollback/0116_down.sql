-- 0116_down: drop the root keys the storage move's final sweep recorded.
--
-- Safe at any time: a release older than 0116 neither reads nor writes the
-- column; its retry re-plans only the keys it knows, and its final sweep finds
-- the rest again from a bucket listing.
ALTER TABLE storage_rekey_derived_failures DROP COLUMN IF EXISTS root_keys;

DELETE FROM schema_migrations WHERE version = 116;
