# Migrate image embeddings to SigLIP 2 (768 → 1152)

The embeddings sidecar now serves SigLIP 2 so400m/14 @378 instead of CLIP ViT-L-14. The vector width changes from 768 to 1152, which is a breaking contract change: the stored vectors, the column width and both dimension checks have to move together.

## The sidecar change is already merged and deployed

- PR: **kozaktomas/image-embeddings#3** — "Swap CLIP for SigLIP 2 so400m (768 -> 1152 dim, fp16)", merged as `7debc4d`. Read it: it carries the reasoning, the measurements, and two estimates that turned out wrong and were corrected. The repo's `README.md` section "CLIP model" is the durable version of the same material.
- It is **live in production** on the box as of 2026-08-11. Verified `/health`:

```json
{"clip": {"model": "ViT-SO400M-14-SigLIP2-378", "pretrained": "webli",
          "dim": 1152, "precision": "fp16"}}
```

- `/embed/image` and `/embed/text` return 1152-dim unit vectors. `/embed/face` is unchanged at 512 (InsightFace, a different model). `/ocr/image` unchanged.

## Current state: kukátko is degraded until this task lands

Both directions are broken against the deployed sidecar, and `embedding.ErrDimMismatch` is deliberately non-transient, so nothing retries its way out of it:

- **Image embedding**: every `image_embed` job fails immediately. New photos get no vector.
- **Semantic search**: `TextEmbedding` validates the query vector against the configured `image_dim` (768) and now receives 1152, so it errors and search falls back to full-text on every query. It is *not* quietly serving results from the old stored vectors — it never gets far enough to compare them.

Both are expected and both are fixed by this task. Full-text search keeps working throughout.

## Why the change was made

Text→image retrieval is what this service is used for. On COCO text→image R@1 the model goes 46.1 → 55.8 (+21 % relative); zero-shot ImageNet 75.3 % → 84.1 %. Source: SigLIP 2 paper, Table 1 (arXiv 2502.14786). SigLIP 2 is Apache 2.0 and `open_clip` 3.3 ships the config.

Measured end-to-end on the RTX 3070, `POST /embed/image` with JPEG decode included, 7 real photos × 3 runs:

| | ViT-L-14 fp32 (old) | SigLIP 2 fp16 (new) |
|---|---|---|
| median | 0.084 s/photo | 0.091 s/photo |
| throughput | 11.88 photos/s | 11.00 photos/s |
| interactive text query | 6.3 ms | 9.1 ms |

Only 8 % slower per photo — JPEG decode dominates a real request, so the heavier tower barely moves the total. **A full re-embed of ~20 664 photos is roughly half an hour of sidecar time, not hours.** Service VRAM is ~3090 MiB against ~3120 MiB before, so nothing on the GPU side changed for the worse.

## Context

- Faces are a different model (InsightFace, 512-dim) and must not change.
- Everything here lands as one change; a partial rollout just extends the outage above.
- The box is often powered off and the job queue already tolerates that; a long backfill is fine.

## Requirements

- New migration `0057_...`: delete all rows from `embeddings`, widen `embeddings.embedding` to `halfvec(1152)`, and recreate `idx_embeddings_hnsw` with the existing `halfvec_cosine_ops`, `m = 16`, `ef_construction = 200`. Old vectors cannot be converted — they are recomputed, not migrated.
- `vectors.ImageDim` becomes 1152 (it guards both `SaveEmbedding` and the search path).
- Config default `embedding.image_dim` becomes 1152.
- `faces`, `face_clusters` and `FaceDim` stay untouched.
- On startup, log a warning when `/health` reports a `clip.dim` that differs from the configured `image_dim`, naming both values. The sidecar now publishes `clip.model`, `clip.pretrained`, `clip.dim` and `clip.precision` precisely so this check is possible — a mismatch should be visible in the log, not discovered as a wave of failed jobs and a silently full-text search.
- Update the 768 references in `README.md`, `docs/OPERATIONS.md`, `config.example.yaml` and `deb/kukatko.env`.
- Existing tests keep passing. Add coverage that the store rejects a wrong-width vector and that the search path rejects a wrong-width query.

## Acceptance

- The migration applies to a database holding existing embeddings and leaves the table empty, at the new width, with a usable HNSW index.
- After deploy, `BackfillEmbeddings` enqueues every non-archived photo (~20 664) and the queue drains with no dimension errors.
- Semantic search returns results again once the backfill completes.

## Implementation notes

- `BackfillEmbeddings` already enqueues only photos that have no embedding row, so clearing the table is exactly what makes it pick up the whole library. No new backfill code is needed.
- `embeddings.model` / `embeddings.pretrained` already exist and are documented in `internal/vectors/models.go` as being stored "so a later model change can be detected and re-embedded" — this is that moment. After the backfill they should all read the SigLIP 2 tags, which is a cheap way to confirm nothing was missed.
- Leave the duplicate-detection thresholds alone here — separate task (`ffd2b819-c594-4833-97cb-e0c79c4d95b3`), which depends on this one finishing first.
- If the sidecar ever needs rolling back, it is `CLIP_MODEL`/`CLIP_PRETRAINED` in its unit file plus a restart — but the stored vectors would then be the wrong width again, so a rollback means redoing this migration in reverse. Prefer fixing forward.