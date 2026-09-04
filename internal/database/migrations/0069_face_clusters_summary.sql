-- 0069_face_clusters_summary: the face-groups page stops rebuilding itself.
--
-- Listing the groups of unnamed faces used to assemble every group from scratch
-- on every request: one query for the group's member faces, plus one HNSW vector
-- search for the "is this Alice?" suggestion — per group, for every group in the
-- library. On the production library that is thousands of vector searches behind
-- one page load, and the page never finished.
--
-- summary caches exactly what the listing needs: the representative face, the
-- handful of example faces and the suggested subject, as the JSON the API
-- already returns. The listing then reads one indexed page of rows and returns
-- it — no per-group query, no vector search on the request path.
--
-- The NULL is load-bearing: it means "not prepared yet" — a group created by a
-- clustering pass, or one whose membership changed — and it is exactly the
-- predicate the `face_cluster` job lists. Preparing a summary is background
-- work; the page says how many groups are ready while the rest are being built.
-- A group is listed only once it has one, so a half-prepared library shows fewer
-- groups rather than broken ones.
--
-- summary_at records when the cached summary was built, so an operator (and the
-- audit of a slow suggestion) can tell a summary written minutes ago from one
-- written before a subject was named.
--
-- Adding nullable columns with no default is metadata-only in PostgreSQL, so
-- this migration does not rewrite face_clusters and is safe to apply while the
-- app is serving. Every existing group starts out NULL, which is correct: none
-- of them has a cached summary yet, and the job builds them.

ALTER TABLE face_clusters
    ADD COLUMN summary    JSONB,
    ADD COLUMN summary_at TIMESTAMPTZ;

COMMENT ON COLUMN face_clusters.summary IS
    'Cached listing view of the cluster (representative, examples, suggestion); NULL = not prepared yet.';
COMMENT ON COLUMN face_clusters.summary_at IS
    'When the cached summary was built; NULL together with summary.';

-- The listing index: the page the API serves, in its exact order, over the ready
-- rows only.
CREATE INDEX idx_face_clusters_ready ON face_clusters (created_at DESC, uid)
    WHERE summary IS NOT NULL;

-- The backlog index: the rows the `face_cluster` job picks up, oldest first, so
-- preparing the remaining groups stays an index scan.
CREATE INDEX idx_face_clusters_summary_pending ON face_clusters (created_at, uid)
    WHERE summary IS NULL;
