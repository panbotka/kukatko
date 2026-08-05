-- Persisted "yes, this person really is marked twice here" decisions.
--
-- One person carrying more than one valid face marker on the same photo is
-- almost always a mistake: a group shot where the matcher put the same name on
-- two neighbouring boxes. The review page that surfaces those groups recomputes
-- them from `markers` on every request, so a curator's "this one is fine, leave
-- it" had nowhere to live and the same group came back on the next reload,
-- forever. That is the same gap migrations 0031/0032/0034 closed for faces,
-- labels and duplicate pairs; this closes it for repeated markers.
--
-- The false alarms are real and worth keeping: a double exposure, a mirror, a
-- photo of a photo, a person on a poster behind themselves. In every one of
-- those the second marker is correct, so the decision is a durable opinion —
-- nothing is merged, detached or deleted by recording it.
--
-- The row keys the (photo, subject) PAIR, which is exactly the group the page
-- shows: "this person, on this photo". It deliberately does not key the markers
-- themselves. Marker uids come and go as a photo is re-detected, and the opinion
-- is about the situation ("two of her here is correct"), not about two specific
-- boxes — keying the boxes would resurrect the group the moment one of them was
-- redrawn.
--
-- photo_uid and subject_uid both CASCADE: a purged photo or a deleted subject
-- cannot have a repeated-marker group, so the opinion about it is meaningless
-- and goes with it. dismissed_by is SET NULL rather than CASCADE — the decision
-- outlives the account that made it, the same way rejected_by does in 0031.

CREATE TABLE duplicate_marker_dismissals (
    id           BIGSERIAL   PRIMARY KEY,
    photo_uid    VARCHAR(32) NOT NULL REFERENCES photos (uid) ON DELETE CASCADE,
    subject_uid  VARCHAR(32) NOT NULL REFERENCES subjects (uid) ON DELETE CASCADE,
    dismissed_by VARCHAR(32) REFERENCES users (uid) ON DELETE SET NULL,
    dismissed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (photo_uid, subject_uid)
);

-- The listing reads every dismissal at once (one bulk lookup per request, never
-- an N+1), which the UNIQUE index above already serves. This second index covers
-- looking the other way round — everything dismissed for one person — without
-- scanning the table.
CREATE INDEX idx_duplicate_marker_dismissals_subject
    ON duplicate_marker_dismissals (subject_uid);
