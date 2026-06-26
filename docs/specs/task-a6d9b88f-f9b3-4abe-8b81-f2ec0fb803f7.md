# M3 — Face detection job

Detect and embed faces via the sidecar and store them, converting pixel bounding boxes to
normalized coordinates with correct EXIF orientation handling.

## Context
Read `docs/ARCHITECTURE.md` §6.1, §7. Depends on: job queue + worker, embeddings sidecar client
(`FaceEmbeddings`, returns 512-dim + bbox `[x1,y1,x2,y2]` in pixels + det_score), faces table.
The sidecar returns pixel bbox; the DB stores normalized `[x,y,w,h]` (0..1, display space).

## Requirements
- **`face_detect` job handler**: load the photo image, call sidecar `FaceEmbeddings`, and for each
  detected face store a `faces` row (512-dim halfvec embedding, normalized bbox, det_score,
  face_index, model, cached photo_width/height/orientation). Idempotent per photo (replace or skip
  existing). Offline sidecar → requeue with backoff (do not burn attempts).
- **bbox conversion helper**: convert pixel `[x1,y1,x2,y2]` → normalized `[x,y,w,h]` using the
  photo's width/height and **EXIF orientation** (swap width/height for orientations 5–8), mirroring
  photo-sorter's logic. Unit-test this thoroughly.
- Upload enqueues `face_detect` for new photos; add an admin backfill
  (`POST /api/v1/process/faces`) enqueuing for photos not yet processed.
- Filter out very low det_score faces (configurable threshold).

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Unit tests: pixel→normalized bbox conversion across all 8 EXIF orientations; det_score filter.
- Integration tests (test DB) with a **fake sidecar**: job stores faces correctly, idempotent,
  requeues when offline; backfill enqueues only unprocessed photos.