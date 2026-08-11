-- 0057_embeddings_siglip2: the image-embedding space moves from CLIP ViT-L-14
-- (768-dim) to SigLIP 2 so400m/14 @378 (1152-dim).
--
-- The embeddings sidecar (kozaktomas/image-embeddings) swapped its image tower,
-- and that is a breaking contract change rather than a tuning knob: the stored
-- vectors, the column width and the dimension checks in internal/vectors and
-- internal/embedding only make sense together. The sidecar's /health now
-- publishes clip.model / clip.pretrained / clip.dim / clip.precision, and the
-- server logs a warning at startup when clip.dim disagrees with the configured
-- embedding.image_dim — so a future divergence shows up in the log instead of as
-- a wave of failed jobs and a silently full-text search.
--
-- The old vectors cannot be converted. Two models trained separately do not share
-- a space, and there is no projection from one to the other; a 768-dim CLIP
-- vector says nothing about where the photo sits in SigLIP 2's space. So they are
-- recomputed, not migrated: this migration empties the table and
-- vectors.BackfillEmbeddings — which enqueues exactly the non-archived photos
-- that have no embedding row — then re-embeds the whole library (~20 664 photos,
-- roughly half an hour of sidecar time at the measured 11 photos/s). Until that
-- backfill drains, semantic search finds nothing and degrades to full text, which
-- is the same graceful path an offline box already takes.
--
-- TRUNCATE rather than DELETE: nothing references embeddings, and it hands the
-- ~20 000 rows of dead tuples back immediately instead of leaving them for a
-- later vacuum. Dropping the HNSW index first is what makes the widening cheap —
-- ALTER TABLE would otherwise rebuild the graph, and rebuilding it before the
-- table refills is pure waste. The recreated index keeps the same name, opclass
-- and m/ef_construction as migration 0006, so nothing downstream (the ef_search
-- the Go layer sets per read transaction, the partial faces index) has to know
-- this happened.
--
-- Faces are a different model entirely (InsightFace, 512-dim) and are deliberately
-- untouched here. This migration is wrapped in a transaction by the runner;
-- building an HNSW index inside that transaction is supported by pgvector.

TRUNCATE TABLE embeddings;

DROP INDEX idx_embeddings_hnsw;

ALTER TABLE embeddings
    ALTER COLUMN embedding TYPE halfvec(1152),
    ALTER COLUMN dim SET DEFAULT 1152;

CREATE INDEX idx_embeddings_hnsw ON embeddings
    USING hnsw (embedding halfvec_cosine_ops) WITH (m = 16, ef_construction = 200);
