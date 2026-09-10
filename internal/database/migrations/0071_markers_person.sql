-- 0071_markers_person: a person can be attached to a photo or a video by hand,
-- with no bounding box and no detected face.
--
-- Until now the only way to say "this person is in this picture" was a marker
-- created from a face the detector found. That covers less than it looks: face
-- detection on a video only ever sees the poster frame, so anybody appearing
-- later in the clip is invisible to it, and on a still the detector misses
-- profiles, backs of heads and faces in a crowd. In all those cases the library
-- had no way of recording who is there.
--
-- The link is a third kind of marker rather than a table of its own, because the
-- person filter, the subject gallery, the subject counts, subject merge and the
-- metadata sidecar all read markers already — reusing the table is what makes
-- every one of those behaviours fall out without a second code path.
--
-- The two constraints are what keep the new kind out of everything that assumes
-- a face image exists:
--
--   * markers_person_no_box — a 'person' marker carries no geometry and no
--     score. Every crop (the subject avatar, the people index tile, the face
--     endpoint) already filters on type = 'face', and this makes a leak
--     harmless even if one ever forgot to: there is no box to cut.
--   * uidx_markers_person — one link per (photo, person). Attaching the same
--     person twice is idempotent rather than a second row, which is what lets
--     the API answer success instead of a conflict.
--
-- The unique index is partial, so it does not constrain face or label markers:
-- a photo may legitimately carry several face markers of one person (that is
-- what `GET /duplicate-markers` reports on), and nothing here changes that.

ALTER TABLE markers DROP CONSTRAINT markers_type_check;

ALTER TABLE markers ADD CONSTRAINT markers_type_check
    CHECK (type IN ('face', 'label', 'person'));

ALTER TABLE markers ADD CONSTRAINT markers_person_no_box
    CHECK (type <> 'person' OR (x = 0 AND y = 0 AND w = 0 AND h = 0 AND score = 0));

CREATE UNIQUE INDEX uidx_markers_person ON markers (photo_uid, subject_uid)
    WHERE type = 'person';

COMMENT ON COLUMN markers.type IS
    'face = a face region, label = a hand-drawn region, person = a hand-attached person with no geometry.';
