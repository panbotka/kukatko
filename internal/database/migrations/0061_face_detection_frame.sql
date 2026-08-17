-- 0061_face_detection_frame: record the frame the face detector actually saw.
--
-- A face's bbox is normalised against the image that was handed to the detector.
-- Which image that was used to be implicit, and getting it wrong is invisible in
-- the data: a box divided by the wrong frame is still four numbers in 0..1. It is
-- how every quarter-turned photo the live detector touched ended up with its faces
-- beside the faces (the sidecar does not apply EXIF, so it was detecting on a
-- sideways picture) and with faces missing altogether, which no coordinate math
-- can recover.
--
-- These two columns make that frame evidence instead of an assumption: they are the
-- pixel dimensions of the bytes the detector was given, in display orientation
-- because that is what the face_detect job now sends. For a quarter-turned photo
-- they are therefore photos.file_width/file_height exchanged, and a row where they
-- are NOT is a detection that ran sideways.
--
-- NULL means "recorded before this was tracked". Every such detection on a
-- quarter-turned photo ran on a sideways image, which is exactly the set
-- `maintenance repair --sideways-faces` re-detects; on any other photo the two
-- frames coincide and there is nothing to tell apart.
ALTER TABLE face_detections
    ADD COLUMN detect_width  INTEGER,
    ADD COLUMN detect_height INTEGER;

COMMENT ON COLUMN face_detections.detect_width IS
    'Pixel width of the (upright) image the detector saw; NULL = not recorded (pre-0061).';
COMMENT ON COLUMN face_detections.detect_height IS
    'Pixel height of the (upright) image the detector saw; NULL = not recorded (pre-0061).';
