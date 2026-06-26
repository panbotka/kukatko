# M2 — Image embedding job + similar photos

Wire image embedding into the job system and expose a "similar photos" API backed by vector
search.

## Context
Read `docs/ARCHITECTURE.md` §6, §7-no, §8. Depends on: job queue + worker runtime, embeddings
sidecar client (offline-aware), embeddings table + repository, photos/storage. The upload task
left a hook to enqueue `image_embed`.

## Requirements
- **`image_embed` job handler**: load the photo's original (or a suitable preview), call the
  sidecar `ImageEmbedding`, store the 768-dim `halfvec` in `embeddings` with model/pretrained.
  Idempotent (skip if already embedded). If the sidecar is **offline/unavailable**, do not burn
  the attempt — requeue with backoff so it completes once the box is up.
- Ensure upload enqueues `image_embed` for new photos; add a backfill command/endpoint
  (`POST /api/v1/process/embeddings` admin) that enqueues embedding jobs for photos missing one.
- **Similar photos API**: `GET /api/v1/photos/{uid}/similar?limit=` → photos ordered by cosine
  distance to the given photo's embedding (exclude itself), returning uid + distance + thumb info.
  404/empty-friendly when the source has no embedding yet.
- **Duplicate detection** helper reused by upload near-dup warning: find embeddings within a small
  cosine distance (config `duplicate.embedding_max_dist`).

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Integration tests (test DB) with a **fake sidecar** (httptest): job computes + stores
  embedding, is idempotent, requeues on simulated offline; similar API returns correct ordering
  and excludes the source; backfill enqueues only missing ones.