-- 0072_video_faces_removed: a video has no detected faces any more, only the
-- people somebody attached to it by hand.
--
-- Face detection on a clip only ever looked at ONE frame — the poster — which is
-- an arbitrary sample of the footage. What it produced was worth less than it
-- cost: a face that happens to be in that one frame, a box drawn over a picture
-- the clip stops showing the moment it plays, and nothing at all about whoever
-- appears in the other few thousand frames. Who is in a video is now recorded the
-- one way that is honest about it — by hand, as a `person` marker with no
-- geometry (migration 0071).
--
-- Nobody loses a name. The four statements below, in order:
--
--   * the faces detected on every video go. They are boxes nothing draws and
--     vectors that would keep turning up as candidates, cluster members and
--     sweep hits for a picture nobody looks at.
--   * so does the row recording that the detector ever ran on the clip. Kept, it
--     would report the step as done for work that no longer happens; gone, the
--     processing report says `skipped`, which is the truth.
--   * a face marker on a video that NAMES somebody becomes a `person` marker —
--     the hand-attached kind, which is exactly what "this person is in this clip"
--     now means. One per (video, person), because that is all uidx_markers_person
--     allows and all the statement is: the same person marked twice on one clip
--     was always one fact. An invalid marker is excluded — it records a rejected
--     name, and resurrecting it as an attachment would put back a name somebody
--     took off.
--   * every face marker left on a video is deleted: the surplus copies the
--     conversion did not take, the invalid ones, and the ones that name nobody.
--     Each is a region of a detection that no longer exists, so there is nothing
--     left for it to describe.
--
-- Live photos are untouched throughout. A live photo is a photograph that carries
-- a motion clip beside it; detection runs on the photograph and always did.

DELETE FROM faces
WHERE photo_uid IN (SELECT uid FROM photos WHERE media_type = 'video');

DELETE FROM face_detections
WHERE photo_uid IN (SELECT uid FROM photos WHERE media_type = 'video');

UPDATE markers m
SET type = 'person', x = 0, y = 0, w = 0, h = 0, score = 0, updated_at = now()
WHERE m.type = 'face'
  AND m.invalid = FALSE
  AND m.subject_uid IS NOT NULL
  AND m.photo_uid IN (SELECT uid FROM photos WHERE media_type = 'video')
  -- Not already attached by hand, or the conversion would collide with the row
  -- that says the same thing.
  AND NOT EXISTS (
        SELECT 1 FROM markers p
        WHERE p.photo_uid = m.photo_uid
          AND p.subject_uid = m.subject_uid
          AND p.type = 'person')
  -- Exactly one face marker per (video, person) survives as the attachment.
  AND m.uid = (
        SELECT MIN(f.uid) FROM markers f
        WHERE f.photo_uid = m.photo_uid
          AND f.subject_uid = m.subject_uid
          AND f.type = 'face'
          AND f.invalid = FALSE);

DELETE FROM markers
WHERE type = 'face'
  AND photo_uid IN (SELECT uid FROM photos WHERE media_type = 'video');
