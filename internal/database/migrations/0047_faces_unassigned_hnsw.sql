-- 0047_faces_unassigned_hnsw: a second, PARTIAL HNSW index over the faces nobody
-- has named yet — the one search every review feature runs, and the one the full
-- index served pathologically badly.
--
-- The candidate search (internal/candidates) asks, per exemplar of a subject,
-- "which UNASSIGNED faces are nearest to this one?". Against idx_faces_hnsw that
-- is an ORDER BY ... LIMIT over the whole faces table with `subject_uid IS NULL`
-- applied afterwards as a filter — and in the neighbourhood of a well-tagged
-- person almost every near neighbour IS assigned (they are that person's own
-- tagged faces), so the filter throws nearly all of them away. pgvector's
-- iterative scan compensates by walking further and further into the graph, and it
-- keeps walking until hnsw.max_scan_tuples (20 000). Measured on a 50 410-face
-- library on the Pi: 90 ms for ONE exemplar, 20 260 tuples visited, ~43 000 buffer
-- accesses. A subject with 428 tagged photos runs 428 of those searches per
-- request, which is where the "hledání tváří trvá extrémně dlouho" report came
-- from — 13.7 s end to end, against a warm cache and nothing else running.
--
-- A partial index whose predicate is exactly the search's predicate turns that
-- back into what it should be: a plain top-k HNSW scan over only the rows that can
-- match, no filtering, no iterative walk. Same measurement, same query: 10 ms.
--
-- The predicate is written `subject_uid IS NULL` verbatim so the planner's
-- predicate-implication check matches the WHERE clause of
-- internal/vectors.findSimilarUnassignedFaceCandidatesSQL without having to prove
-- anything. Keep the two in step: change one and the search silently falls back to
-- the full index and its 9x cost.
--
-- Cost of carrying it: one more HNSW graph over most of the faces table (65 MB per
-- 50 000 faces, measured), and assignment writes maintain both indexes. Building it
-- takes a couple of minutes per 50 000 faces on Pi-class hardware, once, while the
-- migration runs at startup — before the server serves anything.
--
-- This migration is wrapped in a transaction by the runner.

CREATE INDEX idx_faces_unassigned_hnsw ON faces
    USING hnsw (embedding halfvec_cosine_ops) WITH (m = 16, ef_construction = 200)
    WHERE subject_uid IS NULL;
