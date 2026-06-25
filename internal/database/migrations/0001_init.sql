-- 0001_init: enable the PostgreSQL extensions Kukátko depends on.
--
-- pgvector provides the `vector` / `halfvec` types used for image and face
-- embeddings (Kukátko stores `halfvec` columns with HNSW cosine indexes).
-- unaccent powers diacritics-insensitive full-text search over Czech metadata.
--
-- The schema_migrations bookkeeping table is created by the migration runner
-- itself (see internal/database/migrate.go), so it is intentionally not created
-- here. This migration is wrapped in a transaction by the runner; CREATE
-- EXTENSION IF NOT EXISTS is transaction-safe and idempotent.

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS unaccent;
