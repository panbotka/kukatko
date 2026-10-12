-- 0104_immutable_unaccent_qualified: make the full-text wrapper restorable.
--
-- immutable_unaccent (0007) called unaccent('unaccent', $1) unqualified, which
-- resolves only through the session's search_path. pg_dump/pg_restore run with
-- an empty search_path, and PostgreSQL inlines the SQL function while it builds
-- photos.fts (a generated column), so restoring a backup failed at CREATE TABLE
-- photos with "function unaccent(unknown, text) does not exist" — and because
-- `kukatko restore db` restores in a single transaction, the whole restore was
-- rolled back. Found by the multi-tenancy migration rehearsal on a copy of
-- production (docs/MIGRATION_SAFETY.md).
--
-- The fix pins both the function and the dictionary to the schema the unaccent
-- extension lives in, so the body means the same thing under any search_path.
-- The body still computes exactly the same value, so the stored photos.fts
-- needs no rewrite; it stays a plain SQL body so the planner keeps inlining it
-- (a SET search_path clause would stop that). CREATE OR REPLACE keeps the
-- function's OID, so the generated column that depends on it is untouched.
-- Idempotent: re-running replaces the body with itself.

DO $migration$
DECLARE
    ext_schema TEXT;
BEGIN
    SELECT n.nspname INTO STRICT ext_schema
    FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
    WHERE e.extname = 'unaccent';

    EXECUTE format($fn$
        CREATE OR REPLACE FUNCTION immutable_unaccent(text)
            RETURNS text
            LANGUAGE sql
            IMMUTABLE
            PARALLEL SAFE
            STRICT
        AS $body$
            SELECT %1$I.unaccent(%2$L::pg_catalog.regdictionary, $1)
        $body$
    $fn$, ext_schema, ext_schema || '.unaccent');
END
$migration$;
