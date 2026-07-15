-- 0032_face_confirmations: persisted positive feedback — a user's "yes, this
-- really is this person" for a face↔subject assignment.
--
-- Outlier review (internal/outliers) ranks a subject's faces by distance from
-- the subject's centroid and offers the most distant ones as likely mistakes.
-- Without a durable "no, this one is fine", the same false alarms are offered
-- again on every sweep and the review work never shrinks — the exact gap
-- migration 0031 closed for negative feedback. A confirmation is the mirror
-- OPINION: it never mutates the face or its marker, it only remembers the "yes"
-- so outlier results can exclude the face from future runs.
--
-- The key mirrors face_rejections: the face identity Kukátko already uses
-- (photo_uid + face_index, see migration 0006 and internal/facematch) plus the
-- subject. It deliberately does NOT foreign-key the faces table: faces are
-- DELETEd and re-INSERTed on every re-detection, so an FK with ON DELETE
-- CASCADE would erase the confirmation each time detection re-runs. The
-- (photo_uid, face_index) pair is stable across re-detection, and photo_uid →
-- photos CASCADE already cleans up when the photo itself is deleted.
--
-- confirmed_by → users, ON DELETE SET NULL so a confirmation survives the
-- deleting user. The UNIQUE natural key makes confirming twice a no-op
-- (ON CONFLICT DO NOTHING), not an error. subject_uid / photo_uid foreign keys
-- CASCADE so deleting a subject or photo cleans up its confirmations. This
-- migration is wrapped in a transaction by the runner.

CREATE TABLE face_confirmations (
    id           BIGSERIAL   PRIMARY KEY,
    photo_uid    VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- The per-photo face slot (faces.face_index). No FK to faces on purpose:
    -- faces are replaced on re-detection, and the confirmation must outlive that.
    face_index   INTEGER     NOT NULL,
    subject_uid  VARCHAR(32) NOT NULL REFERENCES subjects (uid) ON DELETE CASCADE,
    -- Who said "yes"; NULL for a system action or after the user is deleted.
    confirmed_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    confirmed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Confirming the same (face, subject) twice is a no-op, not an error.
    UNIQUE (photo_uid, face_index, subject_uid)
);

-- The hot bulk lookup: every face confirmed for one subject, used as an
-- exclusion filter by outlier review. subject_uid is last in the unique key,
-- so it needs its own index.
CREATE INDEX idx_face_confirmations_subject ON face_confirmations (subject_uid);
