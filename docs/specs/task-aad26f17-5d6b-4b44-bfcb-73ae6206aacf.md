# Rebuild endpoints: force every per-photo computation, not just the thumbnail

Every per-photo job skips work it has already done, and only the thumbnail has a
forced counterpart. A photo whose derived data is wrong — computed from a bad
source, or before a fix — cannot be recomputed through the API at all.

## Background

`EnqueueThumbnailRebuild` already carries a `force` flag in the job payload, and
`POST /photos/{uid}/regenerate-thumbnail` exposes it: `ForceRegenerate` overwrites
cached sizes where the plain path skips them. Nothing else has that pair. The
plain `POST /photos/{uid}/process/{step}` enqueues the *repair* job, so for a
photo that already has the data it is a silent no-op — it answers 200 with the
step "done" and changes nothing.

This was found in production on 2026-08-31: seven Nikon NEF photos re-rendered
after the RAW preview fix (`273a724`) kept their 640x424 derivatives, because
`process/thumbnail` skipped every cached size. `regenerate-thumbnail` fixed the
thumbnails; the embedding and the face detection could not be redone at all.

## Requirements

- Add a rebuild endpoint per step that owns an idempotent skip, named and shaped
  after the existing `regenerate-thumbnail`: `POST /photos/{uid}/reembed`,
  `POST /photos/{uid}/redetect-faces`, `POST /photos/{uid}/regeocode`.
- Audit every job handler for an idempotent skip before deciding the final set.
  Confirmed present in `embedjob` (skips a photo that already has an embedding),
  `facejob` (skips a photo whose detection is recorded, via `FacesDetected`) and
  `placesjob` (skips coordinates already geocoded). `thumbjob` already has its
  pair. Steps with no skip need no endpoint — say so in the PR rather than
  inventing one.
- Give each an enqueuer counterpart following `EnqueueThumbnailRebuild`: the
  forced flag rides in the job payload, so dedup stays keyed on type + photo_uid
  and at most one active job per photo per type survives.
- A forced job must recompute and *replace* the stored evidence, not append to
  it. Re-running face detection must not leave the previous detections behind as
  duplicates; existing face-to-subject assignments for the photo must survive
  where the same face is detected again, and the response must report how many
  faces the photo has afterwards.
- Guard the routes as their plain counterparts are: `regenerate-thumbnail`
  requires write, `process/{step}` requires maintainer. Match the stricter of the
  two for anything that discards stored work.
- A forced step whose backing service is offline (the embeddings sidecar) must
  queue and answer as the plain path does, not fail.
- Record each forced rebuild in the audit trail, as `regenerate-thumbnail`
  already does.
- Cover with tests: a forced job recomputes where the plain one skips, dedup
  still collapses two forced requests into one, and assignments survive a forced
  face re-detection.

## Implementation notes

- The pattern to copy is `internal/jobs/enqueuer.go` (`EnqueueThumbnailRebuild`,
  `forcedPhotoPayload`), `internal/thumbjob/thumbjob.go` (the `Force` payload
  field and `ForceRegenerate`) and `internal/photoapi/thumbnail.go`
  (`handleRegenerateThumbnail`).
- Extend `kukatkoctl` in the same change: the rebuild endpoints belong next to
  the existing photo lifecycle commands, so the operator does not need curl.
- Document the difference between repair (`process/{step}`) and rebuild in
  `docs/` — the trap is that repair answers 200 and looks like it worked.