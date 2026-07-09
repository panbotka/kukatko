# Object storage for originals and thumbnails — design

**Date:** 2026-07-09
**Status:** approved (revised the same day: DigitalOcean Spaces → Cloudflare R2)

## Problem

Kukátko will run on a VPS with a small disk. The library is ~120 GB today and grows by
roughly 30 GB a year, so the local filesystem cannot be the primary store. Originals and
thumbnails move to S3-compatible object storage, served to browsers through a CDN.

## Why Cloudflare R2 rather than DigitalOcean Spaces

Cost is **not** the deciding factor. At ~140 GB (originals plus thumbnails) R2 costs about
$1.95/month against Spaces' flat $5; in five years, at ~310 GB, roughly $4.50 against $5.77.
A three-dollar monthly difference decides nothing.

Two properties do decide it, both verified against provider documentation:

- **Egress.** R2 charges nothing for egress at any volume. Spaces includes 1 TiB and then
  bills $0.01/GiB — and its allowance covers CDN *and* origin traffic, so an edge cache miss
  is billed on both legs.
- **Private objects can still be edge-cached.** On Spaces they cannot: presigned URLs bypass
  the CDN because the SigV4 signature covers the `Host` header and edge servers never
  validate signatures. R2 has the same limitation on its own — presigned URLs work only
  against `<accountid>.r2.cloudflarestorage.com` and are not served through the cache, while
  caching requires a Custom Domain, which means a public bucket.

  R2 escapes the trap through a **Worker**. Cloudflare's Cache API lets a Worker store a
  response it generated with `caches.default.put()` and retrieve it with `.match()`, and the
  Worker **executes on every request, including a cache hit**. Authorization is therefore
  enforced per request while the cache still spares the R2 read.

This reverses the trade-off accepted in the first draft of this design. Objects stay
**private**, and Kukátko's `private` flag and archive remain real security boundaries rather
than UI filters.

## Costs and caveats

- R2: $0.015/GB-month storage (10 GB free), Class A operations $4.50/million (1M free),
  Class B $0.36/million (10M free), egress free. No minimum fee. Migrating 120 GB is on the
  order of 100 000 writes — inside the free Class A allowance.
- Workers: 100 000 requests/day free; $5/month for 10 million. Every thumbnail is one Worker
  request. A page of fifty thumbnails means the free tier covers about two thousand page
  views a day.
- **Video is not edge-cached.** The Cache API rejects `206 Partial Content`, and Range
  requests are exactly how a browser streams video. Video streams through the Worker from R2
  on every request. Egress is free, so this costs latency, not money.
- **R2 has no native object versioning** that could be confirmed in its documentation. The
  durability plan therefore does not depend on it (see Backup below).
- The Worker is a second deployable, written in JavaScript. It does not violate the
  single-static-binary rule — it runs on Cloudflare, not in the binary — but it is a separate
  artifact to version and deploy.

## Design

### Object keys

Keys derive from values already stored in Postgres. `photos.file_path` and
`photo_files.file_path` are stored verbatim rather than recomputed, so an object key can
simply *be* that value: no addressing rework, no key migration, **no new column**.

The earlier draft introduced a random `photos.public_key` so that a public object could not be
guessed. With private objects and per-request authorization, key secrecy buys nothing —
knowing a key without a valid signature gets you a 403. The column is dropped from the design,
and with it a migration.

Thumbnails keep their existing hash-derived cache layout (`thumb/aa/bb/cc/<hash>_<size>.jpg`),
which becomes the object key directly.

### Signed URLs

Kukátko mints a short-lived URL of the form:

```
https://<media-domain>/<object-key>?exp=<unix-seconds>&sig=<hex HMAC-SHA256>
```

The signature covers the object key and the expiry. Kukátko and the Worker share the secret;
the Worker holds it as a Cloudflare secret, Kukátko as `storage.r2.url_signing_secret`.
Support **two secrets at once** (current and previous) so the secret can be rotated without a
window of broken URLs.

A short TTL — one hour by default — bounds the damage from a leaked URL. Photo payloads carry
freshly signed URLs on every API response, so a reloaded page simply gets new ones.

### The Worker

For each request the Worker:

1. Verifies the HMAC and rejects an expired or unsigned request with `403`. Comparison is
   constant-time.
2. On a `Range` request, streams the range straight from the R2 binding. It does **not**
   attempt to cache: the Cache API rejects `206`.
3. Otherwise looks the object up in `caches.default` under a **canonical cache key that omits
   the query string**. This is the whole point — keying on the full URL would give every
   signature its own cache entry and the hit rate would collapse.
4. On a miss, reads the object from the R2 binding, stores it in the cache with a long
   `Cache-Control`, and returns it to the client with a short `private` `Cache-Control` — the
   edge may hold the object for a year, the browser only for the life of the signature.
5. Never caches an error response.

### Serving

The API returns `thumb_url` and `download_url` on photo payloads; the frontend puts them
straight into `<img src>`. The VPS transfers no image bytes. The existing
`/photos/{uid}/thumb/{size}` and `/download` routes remain as redirects so old links keep
working. Authorization gates *discovery* of a photo in Kukátko; the object itself is gated by
the signature the Worker checks.

### The storage interface

`storage.Storage` is already an interface with one concrete implementation (`FS`). The one
blocker is `AbsPath(relPath) string`, which hands out a real filesystem path and is consumed
by everything that shells out — `exiftool`, `ffprobe`, `ffmpeg`, `heif-convert`,
`vipsthumbnail` — because those tools take a filename and cannot read an `io.Reader`. Replace
it with:

- `URL(relPath) string` — the signed, client-fetchable address of the object.
- `Materialize(ctx, relPath) (path string, cleanup func(), err error)` — a real local file for
  the shell-out tools. `FS` returns the existing path and a no-op cleanup, keeping local
  development and the test suite zero-copy. The R2 backend downloads to a temp file.

Hard links, used by `FS.Store` for atomic publish, have no object-storage equivalent and need
none: `PutObject` is atomic and catalogue-wide deduplication is enforced by the unique
constraint on `photos.file_hash`.

### Client library

`internal/backup` already depends on **minio-go v7** with a configurable, non-AWS endpoint,
built for "AWS / MinIO / Backblaze / Wasabi". R2 is S3-compatible: endpoint
`<accountid>.r2.cloudflarestorage.com`, region `auto`. No new dependency.

### Ingest

Unchanged in shape. An upload already lands in a local staged temp file, and EXIF and
`ffprobe` already run against it. Thumbnails and the perceptual hash are computed there too;
then the original and its thumbnails are uploaded and the temp file is removed. Only one file
occupies the disk at a time.

### Backup

Object storage is not a backup, and this design does not rely on R2 versioning — which could
not be confirmed to exist. `internal/backup` gains an originals source that copies bucket to
bucket **server-side**, without streaming the library through the VPS. The second bucket is
configured independently — its own endpoint, region, bucket and credentials — so it can live
with a different provider. Retention applies to database dumps only. **Originals are never
expired: a deleted original is a lost photo.**

### Configuration

- `storage.backend`: `fs` (default) or `r2`
- `storage.r2.endpoint`, `.region`, `.bucket`, `.access_key`, `.secret_key`
- `storage.r2.media_base_url` — the Worker's domain
- `storage.r2.url_signing_secret` and `.url_signing_secret_previous`
- `storage.r2.url_ttl` — default one hour
- `storage.temp_path` — where `Materialize` writes

### Migration

A resumable, idempotent CLI command moves the existing library: walk the catalogue, upload the
original and its thumbnails, verify, and only then optionally drop the local file. It follows
the high-watermark pattern the importers already use, so an interrupted run resumes rather
than restarts.

## Task breakdown

| Priority | Task |
| --- | --- |
| 200 | Replace `AbsPath` with `URL` + `Materialize`; `FS` behaviour unchanged |
| 190 | R2 backend over minio-go, config keys, `storage.backend` |
| 185 | The Cloudflare Worker: signature check, Range passthrough, canonical-key caching |
| 180 | Serve media through the Worker: signed URLs in API payloads, redirects |
| 170 | `kukatko storage migrate-to-r2` — idempotent, resumable |
| 160 | Backup: bucket-to-bucket copy to an independent second bucket |

The first task is a pure refactor with no behaviour change, so it can be verified on its own,
and everything else depends on it.

## Sources

- [R2 pricing](https://developers.cloudflare.com/r2/pricing/) and
  [R2 product page](https://www.cloudflare.com/products/r2/)
- [Caching R2 objects](https://developers.cloudflare.com/cache/interaction-cloudflare-products/r2/)
- [Workers Cache API](https://developers.cloudflare.com/workers/runtime-apis/cache/)
- [Workers pricing](https://developers.cloudflare.com/workers/platform/pricing/)
- [Spaces pricing](https://docs.digitalocean.com/products/spaces/details/pricing/) and
  [presigned URLs vs the Spaces CDN](https://www.digitalocean.com/community/questions/presigned-urls-vs-spaces-cdn-can-i-get-both-private-access-and-edge-caching)
