# Photo Placeholder Hashes

Store a tiny visual placeholder for every photo so the UI can paint a blurred preview instantly while real thumbnails load. Backend only; the UI change ships separately.

## Requirements

- For every photo compute a compact placeholder representation — BlurHash or ThumbHash, your choice; the encoder must be pure Go (CGO_ENABLED=0). The encoded value must be tens of bytes, cheap enough to embed in every list payload. Stills always; for videos use the poster frame if the pipeline already has it decoded, otherwise skip videos.
- Persist it on the photo record (migration; a nullable column — absence means "not computed yet").
- Compute it during ingest for new uploads, at the same stage thumbnails/perceptual hashes are produced.
- Backfill the existing library via the job system like other derived data (see the existing internal/*job packages and processing/backfill patterns): idempotent, resumable, safe while the app serves traffic.
- Expose the value in photo list and detail API payloads so the frontend can use it later.
- Tests: unit for encoder integration, integration for storage + backfill + API exposure.

## Implementation notes

- The thumbnail pipeline already decodes images (internal/thumb; internal/thumbjob computes pHashes) — compute the placeholder from an existing decoded small rendition rather than re-decoding originals.
- The GPU box is NOT involved — this must work purely locally.
- Pick the hash flavor also by frontend decodability: a tiny, maintained TS decoder must exist (blurhash and thumbhash both qualify).
- Update docs/API.md (new payload field), docs/PACKAGES.md, and the package-map line in CLAUDE.md if you add a package.