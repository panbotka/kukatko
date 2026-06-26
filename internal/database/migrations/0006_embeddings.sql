-- 0006_embeddings: pgvector storage for image and face embeddings.
--
-- Kukátko keeps embeddings directly in PostgreSQL (no external vector store) so
-- similarity search is a plain SQL query against an HNSW index. Both columns use
-- `halfvec` (float16) rather than `vector` (float32): on normalised CLIP/ArcFace
-- embeddings the recall loss is negligible while the HNSW index uses roughly half
-- the memory — material on the Pi. The distance metric is cosine throughout, so
-- the indexes use halfvec_cosine_ops and queries use the `<=>` operator.
--
-- embeddings holds one CLIP image embedding per photo (768-dim, keyed by
-- photo_uid). faces holds zero-or-more ArcFace face embeddings per photo (512-dim),
-- one row per detected face, ordered by face_index. Both reference photos with
-- ON DELETE CASCADE so deleting a photo removes its vectors — closing a
-- photo-sorter gap where embeddings/faces had no foreign key and leaked orphans.
--
-- This migration is wrapped in a transaction by the runner; building an HNSW
-- index inside that transaction is supported by pgvector.

CREATE TABLE embeddings (
    photo_uid  VARCHAR(32)  PRIMARY KEY REFERENCES photos (uid) ON DELETE CASCADE,
    embedding  halfvec(768) NOT NULL,
    model      TEXT         NOT NULL DEFAULT '',
    pretrained TEXT         NOT NULL DEFAULT '',
    dim        INTEGER      NOT NULL DEFAULT 768,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- HNSW cosine index over the image embeddings. m and ef_construction match the
-- photo-sorter tuning; ef_search is set per read transaction by the Go layer.
CREATE INDEX idx_embeddings_hnsw ON embeddings
    USING hnsw (embedding halfvec_cosine_ops) WITH (m = 16, ef_construction = 200);

CREATE TABLE faces (
    id           BIGSERIAL    PRIMARY KEY,
    photo_uid    VARCHAR(32)  NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    face_index   INTEGER      NOT NULL,
    embedding    halfvec(512) NOT NULL,
    -- Normalised bounding box [x, y, w, h] in 0..1, independent of the displayed
    -- image size. A fixed 4-element double precision array.
    bbox         DOUBLE PRECISION[4] NOT NULL,
    det_score    DOUBLE PRECISION    NOT NULL DEFAULT 0,
    model        TEXT         NOT NULL DEFAULT '',
    dim          INTEGER      NOT NULL DEFAULT 512,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- Cached/derived columns populated by people clustering and PhotoPrism
    -- import. marker_uid/subject_uid are external identifiers (nullable until a
    -- face is assigned to a subject); the rest are denormalised render hints.
    marker_uid   VARCHAR(32),
    subject_uid  VARCHAR(32),
    subject_name TEXT         NOT NULL DEFAULT '',
    photo_width  INTEGER      NOT NULL DEFAULT 0,
    photo_height INTEGER      NOT NULL DEFAULT 0,
    orientation  INTEGER      NOT NULL DEFAULT 1,
    -- At most one row per (photo, face slot); lets face detection be re-run
    -- idempotently by replacing a photo's faces.
    UNIQUE (photo_uid, face_index)
);

-- HNSW cosine index over the face embeddings for face similarity / clustering.
CREATE INDEX idx_faces_hnsw ON faces
    USING hnsw (embedding halfvec_cosine_ops) WITH (m = 16, ef_construction = 200);

-- Lookup index for listing/deleting all faces of a photo.
CREATE INDEX idx_faces_photo_uid ON faces (photo_uid);
