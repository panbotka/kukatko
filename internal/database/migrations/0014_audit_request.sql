-- 0014_audit_request: extend the durable audit_log (added in 0012) with the
-- request metadata required by the M6 audit spec — the client IP and User-Agent
-- of the actor — plus the composite (entity) index used by the admin audit view
-- when filtering by a single entity.
--
-- Column naming note: 0012 shipped the table with actor_uid/target_type/
-- target_uid/details. The M6 spec names these user_uid/entity_type/entity_uid/
-- metadata. They are the same concepts; the existing committed names are kept
-- (renaming an already-applied migration's columns would be a destructive change
-- to live data and break schema_migrations idempotency), and the audit package
-- documents the mapping. Only the genuinely new fields are added here.

ALTER TABLE audit_log
    ADD COLUMN ip         TEXT,
    ADD COLUMN user_agent TEXT;

-- Speeds up the admin view's "all entries for one entity" filter
-- (entity_type + entity_uid), required by the spec's index list.
CREATE INDEX idx_audit_log_target_entity ON audit_log (target_type, target_uid);
