-- 0010_face_clusters: groups of unassigned faces produced by auto-clustering.
--
-- Auto-clustering buckets currently-unassigned faces (faces with no subject) by
-- ArcFace embedding similarity so the user can name a whole group of the same
-- person in one action instead of one face at a time (photo-sorter named faces
-- one by one). A cluster owns one or more faces and caches the mean embedding
-- (centroid) used both to pick a representative face and to suggest a likely
-- existing subject (the nearest named centroid).
--
-- A face's cluster membership lives in faces.cluster_uid, added below. A face is
-- eligible for clustering only when it is unassigned (subject_uid IS NULL) and
-- not yet in a cluster (cluster_uid IS NULL), which makes re-clustering
-- incremental: assigned faces and already-clustered faces are left untouched.
--
-- The centroid uses the same halfvec(512)/cosine setup as faces.embedding so the
-- nearest-named-subject suggestion is a plain similarity query.
CREATE TABLE face_clusters (
    uid        VARCHAR(32)  PRIMARY KEY,
    -- centroid is the L2-normalised mean of the member faces' embeddings, kept so
    -- the suggestion query and the representative-face pick need no recomputation.
    centroid   halfvec(512) NOT NULL,
    -- size is the number of member faces, denormalised for cheap listing/display.
    size       INTEGER      NOT NULL DEFAULT 0,
    -- model is the sidecar's face model identifier the members were embedded with.
    model      TEXT         NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- cluster_uid ties a face to its auto-cluster. ON DELETE SET NULL so consuming a
-- cluster (assigning it to a subject, then deleting the cluster row) simply
-- detaches its faces, and emptying a cluster removes it without orphaning faces.
ALTER TABLE faces
    ADD COLUMN cluster_uid VARCHAR(32) REFERENCES face_clusters (uid) ON DELETE SET NULL;

-- Lookup index for listing a cluster's faces and for finding clusterable faces.
CREATE INDEX idx_faces_cluster_uid ON faces (cluster_uid);
