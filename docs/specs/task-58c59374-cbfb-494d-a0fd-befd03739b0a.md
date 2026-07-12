# Bulk-regenerate missing thumbnails (admin backfill)

Photos can end up with no thumbnail: their format was not decodable at ingest time, the box was
offline, or thumbnailing hit a transient error. There is a per-photo repair path, but nothing
fixes the whole library at once — so after decoding support is broadened, every already-uploaded
BMP/TIFF/RAW/HEIC that never got a thumbnail stays broken. Add an admin backfill that regenerates
thumbnails across the catalog, mirroring the existing backfills.

## Current state (for context)

- `internal/processapi` exposes admin-only backfills that enqueue jobs for rows missing derived
  data: `POST /process/embeddings`, `/process/faces`, `/process/clusters`, `/process/places`
  (each `RequireAdmin`, request-scoped, enqueues into the persistent queue). **There is no
  thumbnail backfill.**
- Thumbnails and perceptual hashes are produced by the `thumbnail` worker handler
  (`internal/thumbjob`); the queue (`internal/jobs`) already deduplicates identical pending jobs.

## Requirements

- Add `POST /process/thumbnails` alongside the existing backfills, following the exact same
  pattern (admin-only via `requireAdmin`, same request/response shape, same enqueue-into-queue
  approach). It enqueues a `thumbnail` job for every photo that currently lacks a generated
  thumbnail, reusing the existing thumbnailer and job handler — do not duplicate thumbnailing
  logic.
- Define "missing thumbnail" with whatever the schema/thumb cache already exposes (e.g. photos
  with no perceptual hash, which the thumbnail job computes alongside the thumbnail, or an explicit
  thumb-status column if one exists). Prefer the narrow predicate so a backfill does not needlessly
  re-thumbnail the entire library; optionally support forcing a full re-run via a query flag
  (e.g. `?all=true`).
- Idempotent and safe to run repeatedly: rely on the queue's dedup so concurrent/duplicate runs do
  not pile up redundant jobs. Return a clear summary (how many jobs were enqueued).
- Do not touch originals — this only rebuilds derived thumbnails/hashes. Respect that the box may
  be offline; enqueued thumbnail jobs run locally (thumbnailing is local shell-out / pure-Go, not
  the box sidecar), so they should proceed regardless of box availability.
- If the admin backfill UI (the page that triggers the other `/process/*` backfills) exists in the
  frontend, add a matching "Regenerate missing thumbnails" trigger there consistent with the others.

## Verification

`make check` must pass. Add a `processapi` test mirroring the existing backfill tests: it enqueues a
thumbnail job for a photo missing its thumbnail, enforces `RequireAdmin` (non-admin forbidden), and
returns the enqueued count; assert dedup/idempotency on a repeat call. Document the new endpoint in
`docs/API.md` and `docs/PACKAGES.md` (processapi), and any UI trigger in `docs/FRONTEND.md`.