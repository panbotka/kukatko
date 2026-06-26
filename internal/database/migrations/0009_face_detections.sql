-- 0009_face_detections: tracks which photos have completed face detection.
--
-- The faces table (migration 0006) holds zero-or-more rows per photo, so a photo
-- with no detected faces is indistinguishable from one that was never processed
-- if we only look at faces. This table records the detection event itself —
-- regardless of how many faces were found — so the face_detect job can skip a
-- photo it has already processed (idempotency) and the backfill can enqueue only
-- photos that have never run, including those that legitimately contain no faces.
--
-- This mirrors photo-sorter's MarkFacesProcessed bookkeeping but stores it in a
-- dedicated table with a foreign key, so deleting a photo removes its detection
-- record (no orphans, a photo-sorter gap).
CREATE TABLE face_detections (
    photo_uid   VARCHAR(32) PRIMARY KEY REFERENCES photos (uid) ON DELETE CASCADE,
    -- face_count is how many faces were stored for the photo at detection time,
    -- kept for diagnostics and admin display; zero is a valid, processed result.
    face_count  INTEGER     NOT NULL DEFAULT 0,
    -- model is the sidecar's face model identifier, recorded so a later model
    -- change can be detected and re-detection triggered.
    model       TEXT        NOT NULL DEFAULT '',
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
