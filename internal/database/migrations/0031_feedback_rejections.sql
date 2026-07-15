-- 0031_feedback_rejections: persisted negative feedback — a user's "no, this is
-- wrong" for a face↔subject guess or a photo↔label guess.
--
-- photo-sorter never persisted a rejection: say "that is not Tomáš" and the same
-- face is offered again on the next search, forever, so the review work never
-- shrinks. Kukátko records the opinion durably so every review/search feature can
-- exclude what a user has already turned down. A rejection is an OPINION, not a
-- mutation: it never deletes a face, unassigns a marker or removes a label — it
-- only remembers the "no".
--
-- face_rejections keys a "this face is NOT this person" by the face identity
-- Kukátko already uses (photo_uid + face_index, see migration 0006 and
-- internal/facematch) plus the subject. It deliberately does NOT foreign-key the
-- faces table: faces are DELETEd and re-INSERTed on every re-detection, so an FK
-- with ON DELETE CASCADE would erase the rejection each time detection re-runs —
-- exactly the durability we are adding. The (photo_uid, face_index) pair is stable
-- across re-detection, and photo_uid → photos CASCADE already cleans up when the
-- photo itself is deleted.
--
-- label_rejections keys a "this photo should NOT have this label" by
-- photo_uid + label_uid.
--
-- Both carry who rejected it (rejected_by → users, ON DELETE SET NULL so a
-- rejection survives the deleting user) and when. The UNIQUE natural key makes
-- rejecting twice a no-op (ON CONFLICT DO NOTHING), not an error. subject_uid /
-- label_uid / photo_uid foreign keys CASCADE so deleting a subject, label or photo
-- cleans up its rejections. This migration is wrapped in a transaction by the
-- runner.

CREATE TABLE face_rejections (
    id          BIGSERIAL   PRIMARY KEY,
    photo_uid   VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    -- The per-photo face slot (faces.face_index). No FK to faces on purpose: faces
    -- are replaced on re-detection, and the rejection must outlive that.
    face_index  INTEGER     NOT NULL,
    subject_uid VARCHAR(32) NOT NULL REFERENCES subjects (uid) ON DELETE CASCADE,
    -- Who said "no"; NULL for a system action or after the user is deleted.
    rejected_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    rejected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Rejecting the same (face, subject) twice is a no-op, not an error.
    UNIQUE (photo_uid, face_index, subject_uid)
);

-- The hot bulk lookup: every face rejected for one subject, used as an exclusion
-- filter by the unassigned-face search. subject_uid is last in the unique key, so
-- it needs its own index.
CREATE INDEX idx_face_rejections_subject ON face_rejections (subject_uid);

CREATE TABLE label_rejections (
    id          BIGSERIAL   PRIMARY KEY,
    photo_uid   VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    label_uid   VARCHAR(32) NOT NULL REFERENCES labels (uid) ON DELETE CASCADE,
    -- Who said "no"; NULL for a system action or after the user is deleted.
    rejected_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    rejected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Rejecting the same (photo, label) twice is a no-op, not an error.
    UNIQUE (photo_uid, label_uid)
);

-- The hot bulk lookup: every photo rejected for one label, used as an exclusion
-- filter by label-expansion. label_uid is second in the unique key, so it needs its
-- own index.
CREATE INDEX idx_label_rejections_label ON label_rejections (label_uid);
