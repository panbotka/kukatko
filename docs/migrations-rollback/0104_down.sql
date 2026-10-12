-- 0104_down: put back the unqualified immutable_unaccent body of 0007.
--
-- Optional: the previous release works with either body, and the old body is the
-- one that makes a pg_dump unrestorable (see 0104). Run it only to return the
-- schema byte-for-byte to 0099.
CREATE OR REPLACE FUNCTION immutable_unaccent(text)
    RETURNS text
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    STRICT
AS $$
    SELECT unaccent('unaccent', $1)
$$;

DELETE FROM schema_migrations WHERE version = 104;
