-- 0012_audit_log: durable audit trail written in the same transaction as the
-- mutation it records (see ARCHITECTURE.md §5.1, §12). Backs the bulk metadata
-- editing API and any future auditable change. actor_uid is SET NULL on user
-- deletion so the trail survives account removal.

CREATE TABLE audit_log (
    id          BIGSERIAL    PRIMARY KEY,
    actor_uid   VARCHAR(32)  REFERENCES users (uid) ON DELETE SET NULL,
    action      TEXT         NOT NULL,
    target_type TEXT         NOT NULL DEFAULT '',
    target_uid  VARCHAR(32),
    details     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_actor_uid ON audit_log (actor_uid);
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at DESC);
CREATE INDEX idx_audit_log_action ON audit_log (action);
