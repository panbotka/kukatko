# S3 object storage for originals and thumbnails — design

**Date:** 2026-07-09
**Status:** approved

## Problem

Kukátko will run on a VPS with a small disk. The library is ~120 GB today and grows by
roughly 30 GB a year, so the local filesystem cannot be the primary store. Originals and
thumbnails move to S3-compatible object storage (DigitalOcean Spaces), served to browsers
through the Spaces CDN.

## Why Spaces, and what it costs

The base subscription is $5/month for 250 GiB of storage and 1 TiB of outbound transfer,
with the CDN included at no extra cost. Overages are $0.02/GiB/month for storage and
$0.01/GiB for transfer. At 120 GB plus 30 GB a year, the base allowance lasts roughly five
years. Cost is not the deciding factor.

Two properties of Spaces shape the design and were verified against DigitalOcean's docs:

- **Presigned URLs are not cached by the CDN.** The SigV4 signature covers the `Host`
  header and edge servers do not validate signatures, so every presigned request falls
  through to the origin. Private objects and edge caching are mutually exclusive.
- **Versioning exists but is off by default** and can only be enabled through the API.
  Without it, a deleted object is unrecoverable; with it, a deleted *bucket* still takes the
  version history with it.

## Decisions

1. **Objects are public, addressed by an unguessable key.** This is the only way to use the
   CDN at all, per the presigned-URL finding above.
2. **Accepted risk, recorded deliberately:** with a public bucket, the `private` flag and the
   archive stop being a security boundary and become a UI filter. Anyone holding a photo's
   URL can fetch it, indefinitely, until the key is rotated. Face crops and GPS coordinates
   are part of that exposure.
3. **Both originals and thumbnails live in the bucket.** The thumbnail cache for a 120 GB
   library runs to 15–20 GB, which the VPS disk cannot hold, and thumbnails are exactly the
   objects a browser fetches by the hundred — where the CDN earns its keep.
4. **Durability is versioning plus a second bucket.** Spaces is not a backup.

## Design

### Key scheme

Add `photos.public_key`: 128 bits of randomness, unique, indexed. Keys derive from it:

```
originals/<public_key>/<file_name>
thumbs/<public_key>/<size>.jpg
```

One secret per photo covers the original and every thumbnail size. A leaked URL is revoked
by rotating `public_key` and re-keying two objects.

The thumbnail cache path is currently computed from `file_hash` (`thumb/aa/bb/cc/<hash>_<size>.jpg`).
That must not become the public key: anyone who learns a photo's SHA256 could then derive its
public URL. `file_hash` keeps its role in deduplication and stays out of object keys.

Bucket listing must be disabled. An unguessable key protects nothing if the bucket can be
enumerated. Objects get `public-read` individually; the bucket does not.

Objects are immutable — the key embeds randomness and the content never changes in place —
so they are written with a one-year `Cache-Control`, making the CDN's default one-hour edge
TTL irrelevant.

### The interface change

`storage.Storage` is already an interface with one concrete implementation (`FS`), and
`file_path` is stored verbatim in Postgres rather than recomputed. An S3 key can simply *be*
`file_path`, so no addressing logic changes and no key migration is needed.

The one blocker is `AbsPath(relPath) string`, which hands out a real filesystem path. It is
consumed by everything that shells out — `exiftool`, `ffprobe`, `ffmpeg`, `heif-convert`,
`vipsthumbnail` — because those tools take a filename argument and cannot read an
`io.Reader`. It is replaced by two methods:

- `URL(relPath) string` — the public CDN address of the object.
- `Materialize(ctx, relPath) (path string, cleanup func(), err error)` — provides a real
  local file. `FS` returns the existing path and a no-op cleanup, so local development and
  tests keep their zero-copy behaviour. The S3 backend downloads to a temp file and removes
  it on cleanup.

Hard links, used by `FS.Store` for atomic publish, have no S3 equivalent and need none:
`PutObject` is atomic, and catalogue-wide deduplication is enforced by the unique constraint
on `photos.file_hash`, not by the link.

### Serving

The API returns `thumb_url` and `download_url` on photo payloads and the frontend puts them
straight into `<img src>`. The VPS then transfers no image bytes at all, which is the point.
The existing `/photos/{uid}/thumb/{size}` and `/download` routes remain as redirects so old
links keep working. Authorization still gates *discovery* of a photo; the object itself is
protected by its key.

Video gets simpler. Today `http.ServeContent` needs an `io.ReadSeeker`, meaning a real file.
Streaming straight from Spaces moves Range handling to Spaces. `ffmpeg` and `ffprobe` accept
an HTTP URL as input, which would remove the temp download for transcoding and probing —
**this must be verified during implementation, not assumed.**

### Ingest

Unchanged in shape. An upload already lands in a local staged temp file, and EXIF and
`ffprobe` already run against it. Thumbnails and the perceptual hash are computed there too;
then the original and its thumbnails are uploaded and the temp file is removed. Only one file
occupies the disk at a time.

### Backup

`internal/backup` walks originals on disk today. It gains a source that copies bucket to
bucket server-side, so the VPS never downloads the library to back it up. `internal/backup`
already uses minio-go v7 with a configurable endpoint, built for "AWS / MinIO / Backblaze /
Wasabi", so Spaces needs no new dependency. Versioning is enabled on the primary bucket
through the API as an operational step. `docs/RESTORE.md` must state plainly that versioning
does not survive deletion of the bucket, which is why the second bucket exists.

### Configuration

New keys mirroring the existing `backup.s3.*` block:

- `storage.backend`: `fs` (default) or `s3`
- `storage.s3.endpoint`, `.region`, `.bucket`, `.access_key`, `.secret_key`, `.path_style`
- `storage.s3.public_base_url` — the CDN endpoint or custom domain
- `storage.temp_path` — where `Materialize` writes

### Migration

A resumable, idempotent CLI command moves the existing library: walk the catalogue, assign a
`public_key` where missing, upload the original and its thumbnails, verify, update the row,
and only then optionally drop the local file. It follows the high-watermark pattern the
importers already use, so an interrupted run resumes rather than restarts.

## Costs and caveats

- The 1 TiB transfer allowance covers CDN *and* origin traffic; an edge cache miss is billed
  on both legs.
- Versioned objects count toward the 250 GiB allowance, so the base tier fills sooner than
  the five-year estimate above.
- Deleting a photo leaves edge copies until the TTL expires or the cache is purged.

## Task breakdown

| Priority | Task |
| --- | --- |
| 200 | Replace `AbsPath` with `URL` + `Materialize`; `FS` behaviour unchanged |
| 190 | S3 backend over minio-go, `public_key`, config keys, `storage.backend` |
| 180 | Serve media from the CDN: URLs in API payloads, redirects, video via Range |
| 170 | `kukatko storage migrate-to-s3` — idempotent, resumable |
| 160 | Backup: bucket-to-bucket copy, versioning, `docs/RESTORE.md` |

The first task is a pure refactor with no behaviour change, so it can be verified on its own,
and everything else depends on it.

## Sources

- [Spaces pricing](https://docs.digitalocean.com/products/spaces/details/pricing/)
- [Presigned URLs vs Spaces CDN](https://www.digitalocean.com/community/questions/presigned-urls-vs-spaces-cdn-can-i-get-both-private-access-and-edge-caching)
- [Enabling Spaces versioning](https://docs.digitalocean.com/products/spaces/how-to/enable-versioning/)
