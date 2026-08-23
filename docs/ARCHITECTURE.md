# Kukátko — architecture design

**Version:** 0.1 (draft) · **Date:** 2026-06-25 · **Status:** implemented, in active development (M0–M7)

This document is the binding design of the Kukátko system. It draws on the design doc (feature
list), on the author's earlier photo-management work, and on a verified survey of the real
interfaces (mapy.com REST API, pgvector on ARM, the inference sidecar). Cited sources are in
section [§17 Reference](#17-reference).

---

## 1. Purpose and scope

Kukátko is a standalone application for managing a personal/family photo library: the everyday
catalogue (albums, labels, people, places, videos) and the "smart" layer on top of it (embeddings,
face recognition, semantic search, similar photos) in one binary, with **usability and robustness**
as the point — earlier attempts at this had the features and were still hard to use.

**What is in scope (from the design doc):**

- Simple storage: originals + thumbnails on disk, pgvector as the relational DB.
- Full metadata: GPS, labels, albums, people.
- Import of an existing library + **incremental** re-import. *(Done 08/2026, then removed — [§9](#9-the-one-off-importers-s11s12--retired).)*
- Image and face embeddings.
- Design per [Bootswatch Superhero](https://bootswatch.com/superhero/), with a focus on usability.
- Slideshow on labels/albums — configurable transition effect and speed.
- Reliable "back" (including on a filter).
- Users viewer/editor/admin/maintainer (a strict ladder), bcrypt passwords.
- Maps via [mapy.com](https://mapy.com).
- Bulk metadata editing (albums, labels, captions, location).
- Per-user favorite photos.
- Everything as a single executable binary, frontend included.
- Backup to S3 (originals + DB dump) as part of the running process.
- Configuration via YAML + env variables.
- Text search, both semantic and full-text.
- People recognition + similar photos.
- Working multi-upload, including uploads from the mobile gallery.
- Bilingual: Czech (default) + English.
- Full phone/tablet support.
- Filters and sorting everywhere (library, albums, labels).
- Photo detail = metadata/editing, faces and similar photos on one page.
- **Videos** (mp4/mov/live photos) — storage, poster + thumbnails via `ffmpeg`,
  playback/streaming (range requests). Embedding on the poster frame
  (which also makes videos searchable).
- **Duplicate management** — review of similar/duplicate photos (pHash + embedding) and bulk cleanup.

**What is out of scope:**

- **Photo book creation** — deliberately not built (a LaTeX stack, and a project of its own).
- Public sharing / share links are not a priority (they can be added later).

---

## 2. Guiding principles

1. **Inspiration, not a copy.** The proven contracts and data schema of the author's earlier
   photo-management work are taken over; its pain points are fixed (see [§15](#15-what-we-do-differently)).
2. **The previous system stayed primary** until the cutover; the import was read-only and repeatable,
   and ran in parallel without disturbing it. Done in 08/2026 — see [§9](#9-the-one-off-importers-s11s12--retired).
3. **Pi-first, the box as an accelerator.** The app runs on a Raspberry Pi (ARM64, limited RAM).
   Compute-heavy inference (image embeddings, faces) runs on a powerful machine (the box with an NVIDIA GPU,
   on Tailscale), which **is not always powered on**. Everything must work even when the box is offline.
4. **Visible early.** The milestones are ordered so that a usable UI appears as soon as possible,
   which is then iterated on.
5. **Robustness > extra features.** Persistent state, idempotency, graceful degradation,
   no data loss on restart / box outage.
6. **YAGNI.** No speculative features. Simple, testable, well-bounded modules.
7. **Testability and quality from the start.** Every change has unit and (where it makes sense)
   integration tests. A strict `golangci-lint` and the tests are a **hard gate** (`make check`).
   Nothing is merged with red lint or tests. Goal: an extensible application that the next
   iteration won't break. Details in [§19 Quality, testing and linting](#19-quality-testing-and-linting).

---

## 3. Architecture — overview

### 3.1 Deployment topology

```
┌──────────────────────────── Raspberry Pi (ARM64) ────────────────────────────┐
│                                                                                │
│   kukatko (a single Go binary)                                                  │
│   ├─ HTTP server (chi) + embedded SPA (React/Bootstrap)                         │
│   ├─ REST API /api/v1/...                                                       │
│   ├─ Worker (job runner) ── reads the persistent queue from Postgres           │
│   ├─ Thumbnailer (pure-Go + shell-out heif-convert/exiftool)                   │
│   ├─ Scheduler (S3 backup, trash cleanup, session expiry)                      │
│   └─ Mapy.com proxy (hides the API key)                                        │
│            │                          │                         │              │
│   ┌────────▼─────────┐      ┌─────────▼─────────┐     ┌─────────▼──────────┐  │
│   │ Disk: originals   │      │ shared-postgres   │     │ (local cache        │  │
│   │ + thumbnail cache │      │ + pgvector (HNSW) │     │  thumbnails on disk)│  │
│   └───────────────────┘      └───────────────────┘     └────────────────────┘  │
└───────────────────────────────────┬────────────────────────────────────────────┘
                                     │ Tailscale (HTTP), only when the box is powered on
                          ┌──────────▼───────────┐
                          │ box (x86, RTX GPU)    │
                          │ embeddings sidecar    │  /embed/image  (SigLIP 2 1152)
                          │ (FastAPI + ONNX)      │  /embed/text   (SigLIP 2 1152)
                          │                       │  /embed/face   (InsightFace 512)
                          └───────────────────────┘

       Import (the only one left):
       a directory on disk  ──walk + ingest──▶  kukatko import dir
```

### 3.2 Components (subsystems)

Each subsystem has one purpose, a clear interface, and can be tested independently.

| # | Subsystem | Responsibility |
|---|-----------|-------------|
| S1 | **Storage** | Layout of originals + derivatives on disk; hashing; integrity. |
| S2 | **Ingest/upload** | Multi-upload (incl. mobile), dedup (SHA256 + pHash), EXIF/GPS, photo write, job enqueue. |
| S3 | **Thumbnailer** | Thumbnail generation on the Pi (pure-Go + shell-out for HEIC/RAW). |
| S4 | **Job queue** | Persistent queue in Postgres; retry; survives restart; graceful when the box is offline. |
| S5 | **Embeddings client** | HTTP client for the sidecar (image/text/face); availability detection; backoff. |
| S6 | **Search** | Full-text (tsvector+unaccent) + semantic (SigLIP 2) hybrid; filters/sorting; similar photos. |
| S7 | **People** | Face detection/embedding, IoU marker matching, subjects, suggestions, auto-clustering, outliers. |
| S8 | **Organization** | Albums, labels, bulk metadata editing, per-user favorites. |
| S9 | **Maps** | mapy.com proxy (tile + reverse geocode), GeoJSON for the map, client-side clustering. |
| S10 | **Auth** | Users viewer/editor/admin/maintainer (ladder), bcrypt, sliding sessions, rate-limit, audit. |
| S11–S12 | ~~**The one-off importers**~~ | Done and removed in 08/2026 — see [§9](#9-the-one-off-importers-s11s12--retired). |
| S13 | **Backup** | S3-compatible backup of originals + `pg_dump`, scheduled, in-process. |
| S14 | **Frontend (SPA)** | React/Bootstrap Superhero, i18n, mobile/tablet, back/history, slideshow, detail. |
| S15 | **Config & ops** | YAML+env configuration, Prometheus metrics, audit log, CLI (Cobra). |

---

## 4. Tech stack

The choices draw on what was already proven in the author's earlier photo-management work, and on
research ([§17](#17-reference)).

### Backend
- **Go**, a single static binary, **`CGO_ENABLED=0`** (keeps deployment simple: shell-out to CLI
  tools for HEIC/RAW instead of CGO libraries).
- HTTP router **chi/v5**; CLI **Cobra**; configuration **Viper** (YAML + env, so a deployment is
  not env-only).
- DB access: `pgx` (pool) + `pgvector-go`.

### Database
- **PostgreSQL + pgvector.** Use the shared **`shared-postgres`** (own DB + user per the
  convention in CLAUDE.md, don't launch a separate container). **Verify the `vector` extension is available**
  in shared-postgres — if it's missing, that's the first M0 task (`CREATE EXTENSION vector`).
- **Vectors: `halfvec` (float16) + HNSW + `vector_cosine_ops`.** Half-precision halves the index
  memory at <1 % recall loss on normalized embeddings — crucial on the Pi.
- Migrations: SQL files in `embed.FS`, auto-applied at startup in lexicographic order.

### Frontend
- **React 19 + TypeScript + Vite**, embedded into the binary via `//go:embed all:dist/*`,
  SPA fallback to `index.html`.
- **react-bootstrap + Bootswatch Superhero** theme (dark). Rich interactions (slideshow, crop,
  infinite scroll) → React is necessary over vanilla Bootstrap.
- **i18next** (cz default, en). **Leaflet** + `Leaflet.markercluster` for the map.
  `react-virtuoso` for the virtualized grid.

### Image processing (on the Pi, without CGO)
- JPEG/PNG/WebP/**BMP/GIF/TIFF**: pure-Go (`disintegration/imaging` + `golang.org/x/image/{bmp,tiff}`
  + stdlib `image/gif`), in parallel via goroutines. EXIF orientation `imaging.AutoOrientation`;
  an animated GIF is thumbnailed from its first frame.
- **HEIC:** shell-out `heif-convert` (apt `libheif-examples`) → JPEG → resize in Go.
- **RAW** (cr2/cr3/nef/nrw/arw/srf/dng/raf/orf/rw2/pef/srw/3fr/iiq/x3f/kdc/mrw/mef): extract
  the embedded JPEG preview (`exiftool -b -PreviewImage` / `dcraw -e`), not a full demosaic. TIFF magic
  can't carry RAW — the RAW extension takes precedence, because RAW containers are mostly TIFF-based.
- EXIF metadata: `exiftool` (subprocess) + pure-Go fallback.
- (Optionally later: shell-out to `vipsthumbnail` for large files for memory reasons — ~200 MB
  vs GB thanks to shrink-on-load. The default is pure-Go.)

### Inference sidecar (on the box)
- **Kukátko does not build a sidecar — it is a client of one.** The reference implementation is
  [`kozaktomas/image-embeddings`](https://github.com/kozaktomas/image-embeddings) (FastAPI over
  OpenCLIP + InsightFace), normally running on the GPU box. The address is in the configuration
  (`embedding.url`); when the box is offline, jobs wait in the queue
  (see [§8](#8-asynchronni-joby--box-offline)). For the contract see [§6.1](#61-kontrakt-sidecaru).
- **One call may go elsewhere:** embedding the *search query* is the only sidecar call a person waits
  on, and waiting for a sleeping desktop is not an option, so `embedding.text_url` can point
  `/embed/text` at a second, always-on instance of the same service in text-only mode (production runs
  one on the VPS next to the app, ~0.43 s on CPU, cosine agreement with the box 0.999999). Everything
  else — images, faces, OCR and the **health probe auto-wake reads** — stays on `embedding.url`.
- Models: **SigLIP 2 so400m/14 @378** (`ViT-SO400M-14-SigLIP2-378` / `webli`, image+text,
  **1152-dim**, Apache 2.0), **InsightFace `buffalo_l`** (ArcFace, 512-dim — a different model,
  unaffected by an image-tower swap). The image tower was CLIP ViT-L/14 (768-dim) until 08/2026;
  see migration `0057`. Note: InsightFace's pretrained packs are *non-commercial/research* — OK
  for personal use.

### Storage of originals (`storage.backend`)
- **Two backends behind one `storage.Storage` interface**, switched by a single configuration
  key `storage.backend`; above the interface no package notices the difference.
  - **`fs`** (default) — originals on the local disk, published via an atomic hard-link.
    Existing deployments and the whole test suite stay untouched by it.
  - **`r2`** — a **private** Cloudflare R2 bucket (S3-compatible, the client is the **same
    `minio-go/v7`** as for backups; no new dependency). For a VPS whose disk can't hold the
    library (~120 GB).
- **Object key = `photos.file_path` / `photo_files.file_path` verbatim.** That value is already in Postgres
  and is not derived; the key therefore *is* it → **no new column and no key migration**. Thumbnails
  keep their hash-derived cache layout, which becomes the key directly. The key is **not a secret**:
  a request without a valid signature is rejected by the Worker before it ever reaches the object.
- **Signed URL:** `https://<media_base_url>/<key>?exp=<unix>&sig=<hex HMAC-SHA256>`, the signature covers
  both the key and the expiry. **Two secrets are verified at once** (current + previous), so rotation has no window
  of broken URLs. Default TTL 1 h; every API response carries a freshly signed URL.
- **The Worker is not part of this repo.** The bucket, the Worker source, its bindings and hostname
  (`kukatko-media.panbotka.cz`) live in the **infra repo** (`/home/pi/projects/infra`, root module
  `cloudflare-r2/`) and Terraform deploys them there. Kukátko **stamps** the URL (`internal/storage/sign.go`),
  the Worker **verifies** it — two implementations in two repos and two languages, whose divergence
  no build would catch (one extra byte in the signed message = 403 on every photo).
- **The contract is held by golden vectors:** `internal/storage/testdata/url_signature_vectors.json` (secret,
  key, expiry → expected signature; including the previous secret and a deliberately wrong signature).
  **Both** are tested against them — the Go signer and the Worker in the infra repo. Changing the algorithm = regenerating
  the file, which makes the change visible in review and forces a simultaneous update of the Worker.
- **A hard-link has no equivalent in object storage and isn't needed:** `PutObject` is atomic and
  catalog dedup is held by the unique constraint on `photos.file_hash`. Uploads and downloads stream —
  a file is never held entirely in RAM; `r2` stages them via `storage.temp_path`, because the key depends on
  the content (without SHA256 a re-upload can't be told apart from a different same-named file) and because
  `Materialize` must hand external tools a **real local file**. The temp file is always deleted,
  even on the error path.
- Details (`x-amz-meta-sha256` metadata, configuration keys, secret rotation) in
  [`docs/PACKAGES.md`](PACKAGES.md) and [`docs/OPERATIONS.md`](OPERATIONS.md); the decision and a price
  comparison against DO Spaces in `docs/superpowers/specs/2026-07-09-s3-storage-design.md`.

### Backup
- **`minio-go/v7`** (generic S3 endpoint, path-style, stream `objectSize=-1`).

---

## 5. Data model

The schema follows on from the library's previous shape, so the import could be a 1:1 carry-over,
with changes for Kukátko.
UID = `VARCHAR(32)`, generated by the application (prefix + random suffix). `file_hash` = SHA256 hex.
Originals in the `YYYY/MM/<filename>` layout — on disk a path under the root, in R2 the object key directly.

### 5.1 Key tables

- **`photos`** — `uid PK`, `file_hash UNIQUE` (SHA256), `file_path`, `file_name/size/mime`,
  `file_width/height/orientation`, `taken_at` + `taken_at_source`, `title/description/notes`,
  `ai_note` (free text from external AI classification, `NOT NULL DEFAULT ''`, editable, in full-text),
  `lat/lng/altitude`, `camera_make/model`, `lens_model`, `iso/aperture/exposure/focal_length`,
  `exif JSONB`, `private` (**legacy** — written by the one-off importers and by nothing since;
  the application neither filters nor edits it), `archived_at`,
  `uploaded_by`, timestamps.
  - **IPTC/XMP + technical file metadata** (migration `0027_photos_iptc_metadata.sql`, all
    `NOT NULL DEFAULT ''`, or `false` respectively): **editable** `subject` (IPTC headline — what the photo
    is about; full-text weight B), `keywords` (IPTC keywords **verbatim**, comma-separated;
    full-text weight C — **these are not labels**, `internal/organize` stays unchanged),
    `artist`, `copyright`, `license`, `scan` (`BOOLEAN` — a scan of a paper photo, not a camera shot);
    **machine-derived** (stored and served, but not edited) `software` (firmware/Lightroom/scanner),
    `color_profile` (ICC), `image_codec` (**still** compression: jpeg/heic/avif — `video_codec`/
    `audio_codec` are separate), `camera_serial`, `original_name` (the file name before import;
    `file_name` is the name in the storage layout), `projection` (`equirectangular` for panoramas).
    Populating from EXIF and mapping them at import were separate tasks — existing rows have
    defaults.
  - **Hidden from the library** (migration `0049_photos_hidden_from_library.sql`):
    `hidden_from_library BOOLEAN NOT NULL DEFAULT false` keeps a photo out of the firehose — the grid
    and its counts, the timeline and year buckets, the map and places, the slideshow, the review game
    and the default search — while it stays fully visible in album and label galleries, in favourites
    and at its own URL. It is for what belongs in the catalogue but not among the photographs: scanned
    documents, screenshots. The filter is `hiddenClauses` in `internal/photos` (`stackClauses`' sibling,
    so it reaches every listing at once) and it **lifts itself** for a listing scoped to an album, a
    label or the caller's favourites — filing a photo there was a deliberate act. Distinguish it
    carefully from **`archived_at`** (a photo on its way OUT: trash, purged after retention) and from
    **`private`** (a sharing concept, out of scope); the long column name exists so the three cannot be
    confused. Toggled by `POST /photos/{uid}/hide`+`/unhide` and by the bulk API, listed again with
    `hidden:yes`, and carried in the metadata sidecar (format version 2).
  - **Approximate ("circa") date** (migration `0029_photos_taken_at_estimate.sql`): `taken_at_estimated`
    (`BOOLEAN NOT NULL DEFAULT false` — the date is an **estimate**, not a fact) + `taken_at_note`
    (`TEXT NOT NULL DEFAULT ''` — free text in one's own words: "around 1950", "during the war").
    For scanned and inherited photos where nobody knows the exact date. `taken_at` **remains the only
    anchor** for sorting, the timeline, grouping and date filters — the flag is only presentation and
    truthfulness (the UI marks the date `cca`/`c.`), **it is not a second date axis** and changes no ordering
    (hence no new index). `taken_at` NULL + the flag `true` is an allowed state: the photo behaves everywhere
    like any other one without a date and the meaning is carried by the note. The note lives only with the flag — if the
    flag is cleared, `internal/photoapi` deletes the note (it never stays with a date presented as a fact).
    It does **not** fall into the `photos.fts` full-text (it's a dating note, not a title).
  - **Date precision** (migration `0055_photos_taken_at_precision.sql`): `taken_at_precision`
    (`TEXT NOT NULL DEFAULT 'day'`, CHECK `day|month|year|decade`) — how fine the date was **stated**,
    which is a different question from whether it is a guess (`taken_at_estimated`, above). It exists
    because the bulk editor can date a whole shelf of scans at once ("somewhere in 1974"), and `taken_at`
    is the only anchor the timeline, the period filter, the year facets and `year:` read, so a period must
    still be stored as a concrete instant.
    **The convention: the anchor is the first instant of the stated period, in UTC** — 1 January for a
    year and for a decade, the 1st for a month. UTC is not incidental: the year facets derive
    `date_part('year', taken_at)` in UTC and the period bounds are read as UTC days, so anchoring there is
    what makes a photo dated "1974" land in the 1974 bucket rather than in December 1973.
    Whatever writes a `taken_at` writes the matching precision: `internal/bulkapi` from the stated grain
    (and raises `taken_at_estimated` for anything coarser than a day — a period picked for fifty scans is
    a guess by nature), `internal/photoapi` back to `day` whenever its date-and-time field sets one.
    Presentation must consult it (`web/src/lib/takenDate.ts`): a year-precision photo reads "1974", never
    the 1 January it is anchored at — the anchor is a storage decision, and printing it back would invent
    a day nobody claimed. Carried in the metadata sidecar as `temporal.precision` (format version 3).
    No index: it is never a filter or a sort key.
  - **Location source** (migration `0033_photos_location_source.sql`): `location_source`
    (`TEXT NOT NULL DEFAULT ''`, the vocabulary mirrors `taken_at_source` — `exif` / `manual` / `estimate` /
    empty), plus a partial index `idx_photos_location_estimate_candidates ON photos(taken_at) WHERE
    lat IS NULL AND lng IS NULL AND location_source = ''` for scanning estimator candidates (partial,
    so it indexes only the shrinking backlog, not the whole table).
    It exists because a photo without GPS can be **estimated** from photos taken close in time
    (`internal/geoestimate`), and `lat/lng` on their own can't tell whether a coordinate was measured
    or guessed. **The honesty rule: a wrong location is worse than none** — not only does it look bad
    on the detail view, it quietly poisons the map, the place hierarchy and every `near:` search over them, and looks
    just as trustworthy as a measured coordinate. Hence: an estimate is written **only** onto a photo that
    has no location at all (it can overwrite neither EXIF nor the user's location **by definition** of the candidate set),
    always marked `estimate`, and the UI **flags** it (a badge + a sentence on the detail view, a different pin **shape** on
    the map) — an unflagged estimate is a lie the app tells the user.
    `manual` **without coordinates is not a contradiction but a headstone**: it records the decision "this photo has no
    location" (the user discarded the estimate, or deleted the EXIF location), and it is the only thing that stops the nightly backfill
    from returning the same tip over and over. That's why deleting a location — unlike deleting `taken_at`, which resets
    `taken_at_source` to `unknown` — does **not** reset the source to empty. Empty = "we don't know" and is reserved
    for rows nobody has decided anything about; legacy rows were deliberately **not backfilled** to `exif`
    (a migration can't tell a coordinate from the file apart from one someone once typed in, and writing `exif`
    across all of them would be a confident lie in exactly the column whose only job is to be honest).
  - **Stacks (groups of files of one shot)** (migration `0030_photo_stacks.sql`): two columns —
    `stack_uid VARCHAR(32)` (shared by every member of a single stack, **NULL = the photo is not in a stack**)
    and `stack_primary BOOLEAN NOT NULL DEFAULT false` (exactly one `true` per stack — the visible
    member). Constraints: CHECK `ck_photos_stack_primary_has_uid` (a primary must have a `stack_uid`),
    partial UNIQUE `idx_photos_stack_primary ON photos(stack_uid) WHERE stack_primary` (one
    primary per stack) and partial `idx_photos_stack_uid … WHERE stack_uid IS NOT NULL` (member lookup).
    **A stack groups, it does not merge.** Every member (a RAW + its JPEG, an exported edit, a copy/sequential
    name) keeps its **own row** — its own dimensions, EXIF, embedding, faces and thumbnails —, so
    (un)stacking is purely **reversible** bookkeeping (set / clear the column) and never loses
    anything. That reversibility is the whole reason the user can let automatic detection run
    over the entire library. **Do not simplify to a merge:** `internal/dupmerge` deliberately merges one row
    into another for **real duplicates** (the loser is redundant) — stack members are **not**
    redundant, they are kept on purpose. `photo_files` (role original/sidecar/edited) is a **different concept** —
    the files of a **single** row (still + the motion clip of a live photo); a stack groups whole rows,
    don't conflate them. A live photo is already a single row today (still + MOV as `photo_files`), which is why it stayed
    unchanged — the stack model just generalizes to the row level what `media_type='live'` does at the file
    level. **Visibility:** non-secondary members are hidden everywhere by the predicate
    `(stack_uid IS NULL OR stack_primary)`, added to the shared `whereClauses`
    (`internal/photos/store_list.go`, covers `List`/`Count`/`Search`/`FilterUIDs`/`YearBuckets`/
    `TimelineBuckets` → grid, favorites, ratings, map GeoJSON, global and saved search, every
    paginated total) and to manual count queries (album/label counts, a subject's marker count + its
    gallery, the place facet) and to `ListActivePhashes` (the duplicate universe — a RAW and its JPEG are thus never
    offered as a near-duplicate) and the similar strip. `ListParams.IncludeStackMembers` lifts the predicate
    for callers that want all members (listing the variants of a single stack). These changes also
    **straightened out the counts**: album/label/subject counts now count `p.uid` via a join to `photos` with
    the predicate "visible + not archived", so the count always matches the grid it describes (previously
    it also included archived ones). Detection is driven by `internal/stacks` (synchronous, idempotent, incremental
    global grouping, triggered by the admin `POST /process/stacks`).
  **New columns for Kukátko:**
  - **Provenance ids** — `photoprism_uid VARCHAR(32)`, `photoprism_file_hash VARCHAR(40)` (a source
    file's SHA1) and `photosorter_uid VARCHAR(32)`. They are named after the systems the library was
    imported from, and they are **live data**: `uid:pt…` search resolves through them and every
    metadata sidecar carries them. Never drop them.
  - **Video** (migration `0004_video.sql`): `media_type IN (image|video|live)` (default `image`,
    partial index for "videos only"), `duration_ms`, `video_codec`, `audio_codec`, `has_audio`,
    `fps`. Populated for videos via `internal/video.Probe` (ffprobe → exiftool fallback);
    the poster frame (`internal/video.ExtractPoster`, ffmpeg) feeds the thumbnailer/pHash and the embed/face
    jobs. A live photo = still as the primary image + a motion clip as another `photo_files` row.
  - a generated `fts tsvector` column (GIN index) — see [§6.2](#62-hledani).
  - `favorite` is **moved** into a per-user table (see below).
- **`photo_files`** — originals + derivatives, `role IN (original|sidecar|edited)`, `is_primary`.
- **`photoprism_aliases`** — `photoprism_uid PK` → `photo_uid` (many-to-one, `ON DELETE CASCADE`),
  `photoprism_file_hash`: the source photos that collapsed onto a row already holding their exact
  content (see §5.3). Provenance like the columns above, and equally live.
- **`photo_phashes`** — `phash/dhash BIGINT` (near-duplicate detection).
- **`photo_edits`** — non-destructive edits (crop/rotation/brightness/contrast), 0..1 coordinates.
  The original file is never rewritten: the edit is rendered at run time — into the download
  (`internal/photoedit`) **and into the thumbnails**, which a saved edit rebuilds through a forced
  `thumbnail` job, so the whole library shows the photo the way its owner turned it.
- **`embeddings`** — `photo_uid PK`, `embedding halfvec(1152)`, `model`, `pretrained`, `dim`;
  HNSW `halfvec_cosine_ops` (m=16, ef_construction=200).
- **`faces`** — `id BIGSERIAL`, `photo_uid`, `face_index`, `embedding halfvec(512)`,
  `bbox float8[4]` (normalized [x,y,w,h] 0..1), `det_score`, cache `marker_uid/subject_uid/
  subject_name/photo_width/height/orientation`; HNSW `halfvec_cosine_ops`.
- **`subjects`** — people/animals (`type IN (person|pet|other)`), `name`, `slug`, `cover_photo_uid`.
- **`markers`** — `type IN (face|label)`, normalized bbox (x,y,w,h 0..1), `subject_uid`,
  `score`, `invalid`, `reviewed`.
- **`albums`** + **`album_photos`** — `type IN (album|folder|moment|state|month)`; an album is always
  chronological (migration 0022 removed both the manual `sort_order` and the `order_by` sort choice).
- **`labels`** + **`photo_labels`** — `source IN (manual|ai|import)`, `uncertainty`.
- **`users`** — `role IN (viewer|editor|admin|maintainer)`, `password_hash` (bcrypt cost 12), `disabled`,
  plus the nullable `approved_at` (NULL = registered, waiting for an administrator — distinct from
  `disabled`, which is an account that *was* let in and then blocked) and `welcome_seen_at`
  (NULL = the first-run welcome has never been seen), migration `0064_users_approval_welcome.sql`.
- **`sessions`** — see [§11](#11-auth-a-bezpečnost) (sliding expiry added).
- **`password_reset_tokens`** — one-time links with which somebody sets their own password
  (migration `0065_password_reset_tokens.sql`): `token_hash` (hex SHA-256, UNIQUE — **only the hash**
  is stored, so the table hands nobody a working link), `expires_at` (7 days), nullable `used_at`,
  `user_uid` CASCADE and `issued_by_uid` SET NULL. Preserved by `kukatko maintenance reset` for the
  same reason `sessions` is; pruned by the cleanup that removes expired sessions.
- **`audit_log`** — durable, written **in the same transaction** as the mutation (migrations
  `0012_audit_log.sql` + `0014_audit_request.sql` add `ip`/`user_agent` and an index
  `(target_type, target_uid)`, package `internal/audit`: `Write(ctx, exec, Entry)` over the pool **and**
  a `pgx.Tx`; handler convention `FromRequest`→`Meta`→`Entry`). Consumers: bulk metadata editing
  (`POST /api/v1/photos/bulk`) and photo PATCH/archive/unarchive (audited variants of `photos.Store`).
  Admin read: `GET /api/v1/audit` (`internal/auditapi`, user/entity/action/date filters +
  pagination, admin-only); every signed-in user reads **their own** actions through the sibling route
  `GET /api/v1/audit/mine`, whose handler forces the actor filter to the session's user (so the narrowing
  is the route's shape, not a condition), refuses a `?user=` naming somebody else with 403 and therefore
  never yields foreign or system rows. The trail is otherwise **append-only**; the only exception is the **maintainer-only
  retention purge** `POST /api/v1/maintenance/audit/purge` (`audit.Store.PurgeOlderThan`, a single
  `DELETE ... WHERE created_at < cutoff` over `idx_audit_log_created_at`, `older_than_days`), which
  deletes old records and **audits itself** (`audit.purge` with the cutoff and the count — the fresh purge record
  survives, so deleting the trail stays traceable). The same reasoning makes `audit_log` one of the six tables the
  **library wipe** (`kukatko maintenance reset`, `internal/reset`) must never truncate: it writes its own
  `library.reset` entry **in the truncation's transaction**, so the record of the deletion is what survives it.
  Other mutation domains adopt the in-tx audit convention gradually.
  **The edit payload** (`ChangeSet` in `internal/audit/changes.go`): the `details` of an edit action carries under
  the key `changes` a map `{"<field>":{"old":…,"new":…}}` **with only the changed fields** (old → new),
  so the log shows e.g. a caption change. It is used by photo PATCH (`photo.update` over HTTP and MCP),
  album/label/subject update. **Bulk editing (`internal/bulk`) is exempt from the convention** — a single
  `UPDATE` over many photos without loading the old rows, a per-photo SELECT-before-UPDATE would double
  the statements per batch; it keeps its existing summary details.

### 5.2 New tables in Kukátko

- **`user_favorites`** — per-user favorites: `(user_uid, photo_uid) PK`, `added_at`.
  Replaces the global `photos.favorite`.
- **`jobs`** — persistent queue (see [§8](#8-asynchronni-joby--box-offline)):
  ```
  jobs(
    id BIGSERIAL PK,
    type        TEXT,          -- image_embed | face_detect | thumbnail | pp_import | ...
    state       TEXT,          -- queued | running | done | failed | dead
    priority    INT DEFAULT 0,
    payload     JSONB,         -- e.g. {"photo_uid": "..."}
    attempts    INT DEFAULT 0,
    max_attempts INT DEFAULT 5,
    last_error  TEXT,
    run_after   TIMESTAMPTZ,   -- backoff / deferral
    locked_by   TEXT,          -- worker id (for SELECT … FOR UPDATE SKIP LOCKED)
    locked_at   TIMESTAMPTZ,
    created_at, updated_at TIMESTAMPTZ
  )
  -- index on (state, run_after, priority); dedup unique on (type, payload->>'photo_uid') WHERE state IN (queued,running)
  ```
- **`import_runs`** — import history: source (**`folder`** = `kukatko import dir`, migration `0026`,
  plus three legacy values written by the retired one-off importers), high-watermark, counts, time.
  Only `folder` is still written — a folder has no source time, so it records no watermark and
  idempotency is done by the content SHA256. The rest is kept as the provenance record.
- **`face_rejections` / `label_rejections`** — persisted **negative feedback** (migration
  `0031_feedback_rejections.sql`, package `internal/feedback`). A permanent user "no": *this
  face is NOT this person* (`face_rejections`: `photo_uid`+`face_index`+`subject_uid`) and *this photo
  should NOT have this label* (`label_rejections`: `photo_uid`+`label_uid`). Both carry `rejected_by`
  (FK users `ON DELETE SET NULL`) and `rejected_at`; **UNIQUE natural key** (rejecting twice is a no-op,
  not an error); FK to photos/subjects/labels `ON DELETE CASCADE` cleans up after deletion. **A design
  decision the next iteration must not undo:** the previous system **never kept** rejections,
  so the same wrong face was offered forever and review work never shrank — Kukátko stores it
  durably, so that every review/search feature can exclude it. **A rejection is an OPINION, not
  a mutation** — it never deletes a face, detaches a marker, or removes a label. `face_rejections`
  **deliberately has no FK to `faces`**: faces are deleted and re-inserted on re-detection, a cascade would
  delete the rejection; `(photo_uid, face_index)` is stable across re-detection and deleting the photo cleans it up.
  Bulk lookups (`FaceRejectionsForSubject`, `LabelRejectionsForLabel`) serve the search paths as an
  exclusion filter **without N+1**.

### 5.3 Identity mapping (provenance)

These columns were the import's dedup keys; they are now **the record of where each photo came from**
and they stay. `uid:pt…` search resolves through the first two, and `internal/sidecarexport` writes all
of them into the sidecar next to every original, so dropping a column would silently break both.

| Id in the source system | Storage in Kukátko | Purpose |
|----------------|-------------------|------|
| photo uid, 16 chars (`pt…`) | `photos.photoprism_uid` | provenance; the target of `uid:pt…` search |
| photo uid of a DUPLICATE | `photoprism_aliases.photoprism_uid` | a source photo whose content is already catalogued |
| a source file's SHA1 | `photos.photoprism_file_hash` | provenance of a single source file (a RAW sibling) |
| photo uid of the second system | `photos.photosorter_uid` | provenance of a photo that came through it |

> **Note:** the first source system hashed files with **SHA1**, Kukátko uses **SHA256**. The import
> computed its own SHA256 after downloading (that is what dedup ran on) and kept the SHA1 only for
> lookup. The second source shared SHA256, so dedup there was direct.

> **`photos.photoprism_uid` is 1:1 and cannot express a duplicated source photo.** The source system
> indexed the same bytes as two photos; `photos.file_hash` is UNIQUE, so the second one gets no row, and the row that
> holds its content already wears the first one's uid. **`photoprism_aliases`** (migration `0046`:
> `photoprism_uid` PK → `photo_uid` many-to-one, `ON DELETE CASCADE`) records that collapse, so the second
> uid still resolves — to the surviving row — and the duplicate's albums, labels and markers are attached
> there instead of being dropped with it. Every reader of a source uid resolves through it after the 1:1
> column — today that is `internal/globalsearchapi` resolving a `uid:pt…` search, and back then it was
> the importers and the completeness check, which counted an aliased photo as *deduplicated* rather than
> *missing*. Before it existed, 450 of the 20 660 production source photos were lost in silence under a
> run reporting `failed=0`.

---

## 6. Embeddings and vector search

### 6.1 Sidecar contract

One HTTP service, offline-aware by default; the reference implementation is
[`kozaktomas/image-embeddings`](https://github.com/kozaktomas/image-embeddings):

- **`POST /embed/image`** — multipart, field `file`. Response:
  `{ "dim": 1152, "embedding": [float32×1152], "model": "...", "pretrained": "webli" }`
- **`POST /embed/text`** — JSON `{ "text": "..." }`. Response as above (1152-dim, shared space).
- **`POST /embed/face`** — multipart, field `file`. Response:
  ```
  { "faces_count": N, "model": "...",
    "faces": [ { "face_index": 0, "dim": 512, "embedding": [float32×512],
                 "bbox": [x1,y1,x2,y2] /*px*/, "det_score": 0.0..1.0 } ] }
  ```
  Pixel `[x1,y1,x2,y2]` are converted on write to normalized `[x,y,w,h]` (0..1) according to
  the dimensions and EXIF orientation.

### 6.2 Search

- **Implementation:** a single endpoint `GET /api/v1/search?q=…&mode=…` (`internal/photoapi`),
  the `mode` parameter = `fulltext` | `semantic` | `hybrid` (**default `hybrid`**). All modes
  honor the standard list filters (date/GPS/…) and pagination; the response has the same shape as
  the list + a `mode` field (the effective mode) and `degraded` (see below).
- **Full-text:** PostgreSQL `tsvector` (dictionary `simple`, `unaccent` for Czech) over
  title(A) > description(B) = subject(B) > notes(C) = ai_note(C) = keywords(C) > file_name(D).
  Diacritic-insensitive ("deti" = "Děti"). Sorted by `ts_rank` (`photos.Store.Search`).
  The generated column is rewritten by `ALTER COLUMN fts SET EXPRESSION` (most recently
  `0027_photos_iptc_metadata.sql`) — Postgres recomputes the vector for all rows and rebuilds the GIN index itself.
- **Semantic (text→photo):** text → sidecar `/embed/text` (1152-dim SigLIP 2) → HNSW cosine over
  `embeddings` (`vectors.Store.FindSimilar`). The candidates are then filtered by the list filters via
  `photos.Store.FilterUIDs` (structural filters, ignores full-text) and sorted by distance.
- **Hybrid:** the full-text and semantic rankings are merged by **Reciprocal Rank Fusion (RRF)** —
  an item's score = Σ 1/(k + rank) over both lists, constant **k = 60** (the original RRF paper,
  Cormack et al. 2009). The result is deduplicated (the union of both lists), sorted by the
  fusion score (tie-break by uid descending).
- **Box offline → graceful degradation:** when the sidecar doesn't get the query embedding
  (`embedding.IsUnavailable`, or the embedder/vector store not wired in), `semantic`/`hybrid`
  fall back to plain full-text and the response sets `degraded: true` (the UI informs the user of this).
  Upload and browsing keep working without the box.
- **Similar photos:** HNSW over `embeddings` (`embedding <=> $vec`), a distance threshold for
  "duplicates" (0.028) and "similar" (a higher one, 0.20). Both belong to the model that produced the
  vectors and are re-derived when it changes — the measurements are in [`THRESHOLDS.md`](THRESHOLDS.md).
- **HNSW parameters:** `m=16`, `ef_construction=200`, query `SET LOCAL hnsw.ef_search=100`
  (never ≥400 — the planner falls back to a seq scan). Cosine metric (embeddings are L2-normalized).
- **Search among unassigned faces only** (`vectors.FindSimilarUnassignedFaceCandidates`,
  the basis for finding a person among untagged photos, the recognition sweep, album/label expansion and the review game):
  like a candidate search, but `WHERE subject_uid IS NULL` and with an **exclusion set** (faces already rejected
  for the given subject) filtered out **in SQL** (an anti-join via `unnest`). Filtering happens **before**
  the `LIMIT` and the query runs under `SET LOCAL hnsw.iterative_scan = strict_order` (pgvector ≥ 0.8), so
  the caller gets the number of candidates it asked for even when rejections take away the nearest neighbors —
  **filtering only after the HNSW limit would quietly shrink the result** (a list of 50 from which rejections remove
  30 must still return 50 good candidates). This is a design decision, not an implementation detail.
- **The unassigned-face search has its own partial index** (`idx_faces_unassigned_hnsw`, migration 0047)
  whose predicate is that `WHERE subject_uid IS NULL` verbatim, and the **distance threshold is applied in Go**
  to the ordered rows rather than as a SQL predicate. Both follow from the same property of the iterative scan:
  a filter it cannot see forces it to walk the graph to `hnsw.max_scan_tuples` before it gives up. Around a
  well-tagged person nearly every near neighbor is already assigned, so on the full index that happened on
  every one of the hundreds of per-exemplar searches a single request runs — 13.7 s for one request, measured.
  This too is a design decision: the index predicate and the query's `WHERE` clause must stay written the same way.
- **The negative-exemplar rule** (`vectors.IsNegativeExemplar`, shared for faces and labels):
  so that a rejection **teaches**, not just hides one row. For a candidate *C* and a subject *S*: compute
  the distance from *C* to the nearest **accepted** exemplar of *S* (faces already assigned / photos carrying
  the label) and to the nearest **rejected** one (rejections for *S*). When *C* is closer to the rejected than
  to the accepted, it is **negative** → dropped. It's a nearest-neighbour margin test: no training, no
  learned weights, cheap (the vectors are already in hand) and trivially explainable in the UI ("looks more like something
  you've already rejected"). Without rejections it's a **no-op that costs nothing**; a distance tie survives
  (deterministically, "strictly closer to the rejected one" is dropped). **Don't build anything heavier** — no model
  fitting, no learned weights.

---

## 7. People and faces

Workflow:

1. After import/upload the `face_detect` job → sidecar `/embed/face` → save into `faces`.
2. **Auto-clustering:** similar faces are grouped by vector (HNSW + threshold / connected components),
   whole clusters are offered to the user for one-shot naming — fewer clicks than per-face.
3. **IoU matching** (threshold 0.1) links a detected face to an existing marker.
4. **Suggestions:** for an unnamed face, similar already-named ones are searched (HNSW), with a filter
   (min. face size, exclude other people), limit ~5.
5. **Assignment:** states `create_marker` / `assign_person` / `unassign_person` / `already_done`.
6. **Outlier detection:** for each person compute the centroid and sort faces by distance
   → reveals the mis-assigned ones. Implementation: `internal/outliers` (`Outliers(subjectUID)` over
   `vectors.ListFacesBySubject` + shared `vectors.Centroid`/`CosineDistance`) behind the endpoint
   `GET /api/v1/subjects/{uid}/outliers` (editor/admin, `internal/outlierapi`); small sets
   (< 3 faces) are returned with `meaningful:false`. A wrong face is detached via the existing assign
   API — the outlier layer does not mutate.
7. **People pages:** overview, cover, counts, occurrences.

Coordinates: `faces.bbox` normalized [x,y,w,h] (0..1, display space); `markers` likewise 0..1.
**The sidecar (InsightFace) does not apply EXIF**, so `face_detect` sends an image that needs no
applying: `facejob.StorageSource.OpenUpright` turns the **full-resolution original** (not a thumbnail)
into display orientation by the tag *that file* carries and reports the frame it measured on the bytes
it sent, and `facejob.NormalizeBBox` divides the returned pixel box by exactly that frame — so no
orientation enters the conversion at all. Sending the original untouched instead is what put every
quarter-turned photo's boxes beside its faces, and made the detector miss faces it could not recognise
sideways; both are why the frame is now recorded (`face_detections.detect_width/detect_height`,
migration 0061) and why the repair for the affected rows is a **re-detection**
(`maintenance repair --sideways-faces`) rather than arithmetic.
The `face_detect` job (`internal/facejob`) is **idempotent** via the `face_detections` table
(migration 0009): one row per processed photo distinguishes a photo with no faces from one not yet
processed (`faces` may have zero rows). Weak detections are filtered by the `faces.min_det_score` threshold.
Admin backfill: `POST /api/v1/process/faces`.

**The review game and the uncertainty band (`internal/review`):** alongside the bulk pages
(`/recognition`, expand) there is a "one question at a time" mode — and its design decision is
**the uncertainty band**. Candidates are split by confidence (= 1 − cosine distance) into three zones:
above `review.band_max` the system is essentially decided and confirmation belongs in the bulk UI (asking
one by one would waste time), below `review.band_min` the guess is noise and the question demoralizes; **only
the middle band** (default 0.45–0.75, configurable) becomes questions, because a human answer
at the decision boundary teaches the system the most — a "no" is stored as a permanent rejection in
`internal/feedback` and, via the negative-exemplar rule, also kills similar candidates, so the game
converges. The queue **composes existing services** (sweep/candidates for faces, expand for labels),
sorts by distance from the middle of the band, interleaves question kinds deterministically (no `rand` — tests
and reproduction require the same queue for the same library state) and is cached per user
(`review.cache_ttl`). Answers go **exclusively through the existing write paths** (the assign state machine,
`AttachLabelAudited`, feedback) — review has no write path of its own.

The game knows **five kinds** of question, not two. Face and label check guesses about things nobody had
decided; **place**, **duplicate** and **outlier** check guesses the machine already *acted on* — a coordinate
the geo-estimator wrote onto a photo, a pair the duplicate detector linked, a face somebody filed under a
person. Those are the errors that otherwise sit in a library unseen, because no page lists them as questions,
and each of the three composes an existing read-only service (`geoestimate.Reviewer`, `duplicates.Service`,
`outliers.Service`) exactly as the first two compose sweep and expand. The tiers do not apply to them — their
confidences are not points on one comparable scale — so each carries its own ordering.

**Which of the five the game asks, and in what proportion, is configuration** (`review.kind_shares`), not
something that falls out of how much material each search happened to return. The default is one line long —
faces, and nothing else — because that is what the game is for; restoring the mix it began as is
`face: 0.95, label: 0.05`. A kind at zero is never scanned, which is what buys the wider face window the
variety rules need, and it is not a source an empty queue can name either. What the shares do *not* do is
starve a collector: every enabled kind still gathers a full pool's worth, so a kind that is the only one with
material fills the pool alone — an exhausted library must not withhold the work it has.

**"I don't know" is remembered.** Yes and no leave a durable trace of their own, so the game never repeats
them; "don't know" left none, and a restart put the same unrecognisable face back on the screen. A skip on a
question about a person is now written per (user, subject, photo) in `review_skips` (migration `0059`), and
three of them about one person quiet that person for that player. The whole mute is derived from those rows —
when it began is the newest skip, how long it lasts is the cooling-off period doubled once per skip past the
threshold — so there is no second state table to keep in step. It is a **pause, not a verdict**: the photos
themselves stay silent for good, but past the cooling-off period the game tries once more on a face that
player has never been shown, and a photo the library gained after the mute is never suppressed. It is
strictly **per user**, and it is emphatically **not a rejection** — nothing about it reaches the catalogue,
narrows a candidate search or changes an identity, and it stays out of the audit trail, because the audit
records what happened to the library and a player's uncertainty is a fact about the player.
The rule that answers open no new write path holds for all of them, and one consequence is deliberate and
absolute: **the game never merges duplicates, never deletes a photo and never invalidates a marker.** A
duplicate "yes" records an opinion (`duplicate_confirmations`, migration `0054`) that ranks the group first on
the duplicates page, where merging remains an explicit act with a preview in front of it; an outlier "no" goes
through `unassign_person`, so the marker and the face survive and the answer stays reversible. A place verdict
is the one answer with **no inverse** — nothing can mark a location an estimate again — so the UI withholds
undo there rather than writing the old coordinates back as a decision the user never made.

Every **decisive** answer writes a durable audit row marked
`details.via = "review"` in the same transaction as the mutation; a review face confirmation (`face.assign`)
and an outlier detach (`face.unassign`) get this marker through facematch `Service.Apply` too (via
`AssignRequest.Via`), so every action is consistent and the same action performed on an ordinary curation page
stays unmarked. Which actions count as a yes and which as a no lives in **one shared list**
(`audit.ReviewYesActions()`/`ReviewNoActions()`), read by both the leaderboard and the admin decision view, so
a new question type becomes countable the moment its write path is wired rather than the next time somebody
remembers. On top of these
rows sits a **leaderboard** (`internal/review` `LeaderboardStore`, `GET /review/leaderboard`): per
`actor_uid` it counts the yes and no decisions,
skip is not counted (it writes nothing), a NULL actor (a deleted user) is omitted, with all-time /
7-day / today windows. The partial index `idx_audit_log_review_actor` (migration `0037`) on `details->>'via'`
keeps the aggregation cheap.

---

## 8. Asynchronous jobs and "box offline"

This is the main robustness improvement over the previous system (in-memory jobs + SSE,
lost on restart).

- **Persistent queue in Postgres** (`jobs`). The worker takes work via
  `SELECT … FOR UPDATE SKIP LOCKED`, so that multiple workers/instances don't collide.
- **Worker runtime** (`internal/worker`) runs **in the `kukatko serve` process**: a configurable
  number of goroutines polling `Claim` with bounded concurrency, dispatch to a handler from a **registry**
  (`Register(type, HandlerFunc)`) by `job.Type`, `Complete`/`Fail` per the result, plus
  stale-lock recovery. A **heartbeat** refreshes a running job's lock while its handler works, so a job
  that legitimately outlives `worker.stale_after` (a full import pass) is not recovered and run twice;
  every lifecycle write is fenced by `locked_by`, so a reclaimed job cannot be finished by its previous
  owner. Recovery requeues with the same backoff `Fail` uses. **Graceful shutdown** (SIGINT/SIGTERM)
  stops claiming and leaves abandoned in-flight jobs to the queue for recovery — except a deferral
  (`RetryAfterError`), which is still written so it never burns a retry attempt. The queue state is read via the **admin Jobs API**
  (`internal/jobsapi`: `GET /jobs/stats`, `GET /jobs`, `POST /jobs/{id}/requeue`); the UI polls it.
- **Job types:** `thumbnail`, `places`, `metadata`, `sidecar`, `storyboard`, `mail_send` (run locally on the
  Pi, immediately), `image_embed`, `face_detect`, `ocr` (require the box), `pp_import`, `ps_migrate`, `backup`.
  `ocr` reads the text printed in a photo (`POST /ocr/image` over its `fit_1920` preview) into
  `photos.ocr_text`, so a sign in the frame becomes searchable; it runs for **stills only** and records an
  empty reading as a finished one. See `internal/ocrjob`.
  `storyboard` is the odd one out in *when* it is scheduled: it renders a video's scrub-preview sprite
  (one ffmpeg pass over the clip) and is enqueued **lazily, on the first playback of that video** — never
  for the library at large, because most clips are never watched. See `internal/storyboardjob`.
  `mail_send` is the **only** path by which Kukátko sends an e-mail: the payload names a template and carries
  its data, so the message is rendered when it is delivered rather than when it is scheduled, and a mail
  enqueued while the SMTP server is away still arrives once it is back. It is enqueued **inside the
  transaction of the mutation that caused it** (the way audit rows are written), so a rolled-back registration
  sends nothing. See `internal/mailjob`.
- **Box offline:** the embeddings client checks the sidecar's availability before processing (health check).
  When the box is offline, `image_embed`/`face_detect`/`ocr` jobs stay `queued` with `run_after`
  pushed out (backoff), upload and browsing work without restriction. Once the box comes up the queue
  catches up on its own.
- **Idempotency:** dedup on `(type, photo_uid)` in active states; `filterUnprocessedPhotos`
  skips already-done ones.
- **Retry & dead-letter:** `attempts < max_attempts`, exponential backoff via `run_after`,
  then `state=dead` + `last_error` (visible in the admin). A handler that recognises a failure **no retry can
  fix** (a permanently undeliverable address, an unreadable payload) returns `worker.Terminal(cause)` instead:
  the job is parked in `state=failed` on the first attempt rather than knocking on the same door five times,
  and an operator can still requeue it once the cause is fixed.
- **Progress:** the UI gets the state from the DB (polling / SSE only as a thin layer over the DB state).
- **Box auto-wake (optional, `internal/wake`):** the `wake` package periodically checks the queue
  (every minute, `wakeCheckInterval`) and when configured **on** and the number of pending
  `image_embed`/`face_detect` jobs reaches `embedding.wake.min_queue` **and at the same time** the sidecar
  health check reports offline, it sends a Wake-on-LAN magic packet onto the local LAN (the `mdlayher/wol` library).
  A **cooldown** (`embedding.wake.cooldown`) prevents spamming a sleeping box; after a **grace period** it
  re-checks health and logs whether the box came up, otherwise it backs off until the next cooldown.
  The loop runs in its own goroutine in `serve` — it **never blocks job processing**. WoL
  **does not work directly over Tailscale** (an L3 overlay without L2 broadcast) — the host must be on the same
  physical network as the box; the default path is a UDP broadcast to `embedding.wake.broadcast_addr`,
  optionally a raw Ethernet frame on `embedding.wake.interface` (requires CAP_NET_RAW). **Off by
  default** (`embedding.wake.enabled=false`), fully inert; waking the box by hand is enough. Putting
  the box to sleep is out of scope.
- **Embeddings-sidecar reachability for the UI (`internal/reachability`):** a small background loop
  (same structure as auto-wake, `capabilitiesCheckInterval` = 1 min) probes `embedding.Client.Healthy`
  and **caches** the result in an `atomic.Bool`, so the HTTP handler reads it without a live probe — the box is
  often offline, so a probe on every request would be slow. The flag is exposed by the all-authenticated
  `GET /api/v1/capabilities` (`internal/capabilitiesapi`, `{semantic_search}`), which the frontend polls
  to show/hide the semantic-search option depending on whether the box is online. It is **purely
  presentational**: search degrades to full-text on its own (`degraded=true`), so the safe default
  "unavailable" only hides the option, it never breaks the flow. It probes the host that answers
  `/embed/text` — `embedding.text_url` when configured, otherwise `embedding.url` — because the flag is
  about semantic search, not about the box; auto-wake's probe stays on the box. When neither is set,
  the loop is inert and the flag is always `false`.

### 8.1 Metadata sidecars — curation data independent of the database

**Decision:** for every photo, write a **YAML sidecar** next to the originals in storage
(`sidecars/<original key>.yml`) with its metadata and curation data, so the catalog can be restored
**from storage alone** — originals + sidecars, without the database. Packages `internal/sidecarexport` (format +
atomic write) and `internal/sidecarjob` (job handler + backfill). **The whole format is in
[`RESTORE.md`](RESTORE.md)** — that's where someone will look for it once the database is gone.

**Why:** everything the user creates — titles, who is in the photo, albums, ratings — otherwise exists
in **one single place**: in Postgres. The S3 backup is good, but it is **one mechanism**, and a backup
that quietly fails for three months you discover on the day you need it. The sidecar is a second mechanism of a **different
kind**: curation data sits *next to the photo it describes*, in a text file that any tool can read,
on the same storage as the original.

**Key decisions:**

- **A parallel tree, not a file next to the original.** The tree of originals stays purely media — importers and
  the integrity scan that walk it don't have to learn to ignore a second kind of file — and the whole export
  is one prefix, so it can be listed, rsynced or discarded as a whole. It's still the **same
  storage** as the originals (both FS and R2), so sidecars travel with the photos into the backup too.
- **The extension is added, not replaced** (`IMG_1.jpg.yml`): `IMG_1.jpg` and `IMG_1.png` are two photos and
  must not collide on one sidecar.
- **Embeddings and face vectors are deliberately not written.** They are large, binary, would swamp the file's
  readability — and they are **cheaply recomputed from the original**, that's what the backfill jobs are for. What can't
  be recomputed is what a **human** decided, and all of that is there. The file header says so out loud, so that nobody
  "fixes" it.
- **The write is asynchronous and debounced.** Every mutation enqueues a job; the queue's dedup index keeps at most
  one pending `sidecar` job per photo and the job has a ~5s `run_after`, so a bulk edit of 500 photos
  enqueues 500 cheap rows and returns — the files are written by the worker, one per photo. The handler is
  **idempotent and stateless** (it reads the photo and writes the current truth), which is why a coalesced or lost
  job is only staleness, not a lost update.
- **Never at the user's expense.** A failed write is logged and the queue retries it; an edit never fails —
  the edit is safely in Postgres anyway, and the safety net must not be the thing that trips you.
- **The sidecar is deleted on purge.** A sidecar that outlives the photo is exactly the file from which
  a restore would resurrect a permanently deleted photo.
- **Sidecars are excluded from the integrity scan** (`maintenance scan`): its definition of an orphan is "on
  disk, not in the catalog", which every sidecar is by nature. The filter sits in the adapter in `cmd/kukatko`, not
  in `backup.DiskOriginals` — because the same pass also feeds the S3 sync, which **is** supposed to copy sidecars.
- **Reading back (`restore --from-sidecars`) does not exist yet.** This is the export half; the format is
  designed to be sufficient for it, and the **round-trip test** in `internal/sidecarexport` is what holds that
  sufficiency (and what a future importer will get as its spec).

---

## 9. The one-off importers (S11–S12) — retired

**Done and gone.** This library was first filled by importers written for the two systems that held
it before. They closed on 2026-08-05 with a COMPLETE reconciliation (`photos`, faces, albums, labels
and people all reconciled without a gap), and the ~20 000 lines that carried them — four importer
packages, three source clients, the `import`/`migrate`/`verify` subcommands they added, the
`POST /import/*` triggers and their `import.*` configuration — were removed in August 2026. Kukátko
is the primary system and imports from nothing but the disk.

**What outlived them** (and must not be mistaken for leftovers):

- `photos.photoprism_uid`, `photos.photoprism_file_hash`, `photos.photosorter_uid` and the
  `photoprism_aliases` table — the provenance of 20 647 photos, the target of `uid:pt…` search
  (`internal/globalsearchapi`), and part of every metadata sidecar written next to an original
  (`internal/sidecarexport`), i.e. of 25 791 files in object storage. See [§5.3](#53-identity-mapping-provenance).
- The `import_runs`/`import_failures` rows they wrote, and the three legacy values of
  `importer.Source` needed to read them.
- The one import that remains: **`kukatko import dir`** (`internal/dirimport`), which walks a
  directory on the server's disk — a Google Takeout export included — and ingests it through the same
  pipeline as a browser upload.

---

## 11. Auth and security

- **Users:** roles viewer/editor/admin/maintainer (a strict ladder, each inherits the lower one); write from
  `editor` up, `maintainer` is the top (operations: imports/maintenance/backup/…). Bcrypt cost 12.
  Bootstrap the admin via env (`BOOTSTRAP_ADMIN_*`) on a clean install.
- **Sessions:** an opaque token in an HttpOnly + SameSite=Strict cookie; a separate `download_token`.
  **Improvements over the previous system:**
  - **Sliding expiry** — extension on activity (an active user doesn't get dropped after 30 days).
  - **A password change revokes the user's other sessions.**
  - **A password reset by link revokes them all** — an administrator issues a one-time link
    (`password_reset_tokens`, 7 days, only the hash stored, the newest link supersedes the previous),
    the person behind it chooses their own password, and every session of that account is deleted.
    Both public halves of it are rate-limited per client address.
  - **Rate-limit on `/auth/login`** (brute-force protection).
  - **Rate-limit on demanding endpoints** (`internal/ratelimit`) — a per-client-IP token-bucket
    (`ratelimit.*` config) on `POST /upload`, `POST /photos/bulk`, `POST /import/*` and
    `GET /map/tiles/...`, so that a single client can't swamp the server; an empty bucket → 429. The limiter runs
    before the auth check and can be turned off (`rate_per_sec ≤ 0`).
- **Durable audit log** — written to `audit_log` in the **same transaction** as the mutation (the previous system
  writes only after commit → loss on a crash).
- **The Mapy.com key** is never sent to the browser — tile/geocode requests go through the
  **backend proxy** (see §12 Maps).

---

## 12. Maps (S9)

- **Tiles:** Kukátko proxy endpoint → `https://api.mapy.com/v1/maptiles/{mapset}/256/{z}/{x}/{y}`
  (the backend adds the key via `X-Mapy-Api-Key`, it never appears in the client). Map sets
  `basic|outdoor|aerial|winter`, retina `256@2x` for basic/outdoor.
- **Mandatory (DO NOT BREAK):** the attribution `© Seznam.cz a.s. a další` (link to `/copyright`)
  **and** the clickable mapy.com logo above the map (a Leaflet control with `logo.svg` → `mapy.com`).
- **Markers/clustering:** `Leaflet.markercluster` on the client; photo data with GPS from the Kukátko API.
- **Reverse geocode (a photo's locality):** proxy to `GET /v1/rgeocode?lon=&lat=&lang=cs`
  → a `location` string (e.g. "Praha 1 - Staré Město, Česko"). Called on demand / in batches,
  not for every photo (geocode = 4 credits vs 1 tile).
- **Limits:** free 250k credits/month; rate 500 tiles/s, 200 rgeocode/s. Keep an eye on it.

---

## 13. Frontend (S14)

- **Stack:** React 19 + TS + Vite + react-bootstrap + Bootswatch Superhero (dark), embedded into the binary.
- **i18n:** i18next, **Czech default** + English; a language switcher, persistence of the choice.
- **Mobile/tablet:** fully responsive; multi-upload **from the mobile gallery** (`<input capture>` /
  file picker); touch-friendly slideshow and detail.
- **"Back always works (even on a filter)":** all view state (filters, sorting, search, page)
  is in **URL query params** + History API. Browser back restores the previous filter; the server is
  stateless with respect to view state. Sharing a URL = sharing a view.
- **Library:** a virtualized grid (`react-virtuoso`), infinite scroll, filters+sorting.
- **Photo detail:** preview + metadata (view/edit),
  EXIF, GPS/mini-map, **faces** (boxes, assigning people), **similar photos**, labels, albums,
  favorites, non-destructive edits (crop/rotate/brightness/contrast).
- **Bulk editing:** select multiple photos → albums, labels, captions, location, favorites.
- **Slideshow:** on albums/labels; a configurable **transition effect** (fade/slide/…) and **speed**;
  fullscreen, touch/keys.

---

## 14. Configuration, build and operations (S15)

### Configuration
- **YAML + env override** (Viper). Keys: `database.url`,
  `storage.originals_path`, `storage.cache_path`, `embedding.url`/`dim`, `web.port`/`host`/
  `session_secret`, `auth.bootstrap_admin_*`, `maps.mapy_api_key`, `backup.s3.{endpoint,
  region,bucket,access_key,secret_key,path_style}`, `backup.schedule`, `duplicate.*`,
  `trash.retention_days`. Secrets primarily via env.

### Build & deploy
- **goreleaser**, `CGO_ENABLED=0`, **arch arm64** (+ amd64 for development), a `.deb` package with a
  systemd unit and env-file (a conffile, preserved across upgrades).
- Runtime dependencies (apt): `exiftool`, `libheif-examples` (heif-convert), `dcraw`/LibRaw,
  `postgresql-client` (pg_dump **and** pg_restore). (No `texlive` — the photo book is omitted.)
- DB migrations auto-apply at startup. Frontend `npm ci && npm run build` → `embed.FS`.

### Backup (S13)
- In-process, scheduled (cron/scheduler): `pg_dump` + sync of originals to an **S3-compatible**
  endpoint (`minio-go`, path-style, stream `objectSize=-1`). A configurable endpoint
  (AWS/MinIO/Backblaze/Wasabi). Retention/versions configurable.

### Restore / disaster recovery (S13)
- The counterpart to the backup, so that it is **usable**. The CLI tree `kukatko restore` (shares the `backup.s3.*`
  configuration, `internal/backup`):
  - `restore list` — lists the dumps in the bucket (`db/kukatko-*.dump`), newest first.
  - `restore db [--dump KEY] [--yes] [--verify]` — **destructive**: downloads the dump from S3 and streams
    it into `pg_restore` (`--clean --if-exists --single-transaction`, password via the `PGPASSWORD` env,
    never in argv), then idempotently re-applies migrations. Requires `--yes` (overwrites data).
    Without `--dump` it restores the newest dump.
  - `restore originals` — downloads missing originals from the bucket into `storage.originals_path`,
    skips ones that already exist by **key + size**; an atomic write via `.tmp` + rename →
    **resumable** (an interrupted run repeats safely).
  - `restore verify` — an integrity report: the count of photos in the DB vs. originals on disk + mismatches
    (`photo_files.file_path` missing on disk / files on disk with no record).
- **Admin API** (`internal/restoreapi`, admin-only, **read-only** operations only): `GET /restore/dumps`
  and `POST /restore/verify`. Destructive DB restore is **deliberately not exposed** over HTTP (a restore under
  a running server would pull the tables out from under it) — it belongs in the CLI with the server stopped.
- Thumbnails (the cache) are regenerated **lazily** on-demand after a restore; embeddings/faces are part of the dump.
- Runbook (fresh machine → install → restore → verify): [`docs/RESTORE.md`](RESTORE.md).

### Observability
- **Prometheus** metrics, `audit_log`, structured logs.

---

## 15. What we do differently

The pain points below are the ones the author's earlier photo-management app had; fixing them is
most of why Kukátko exists.

| Pain point | Solution in Kukátko |
|----------------------|------------------|
| In-memory jobs, lost on restart | Persistent queue in Postgres (`jobs`, SKIP LOCKED, retry, dead-letter) |
| No rate-limit on login | Rate-limit on `/auth/login` |
| 30-day absolute session expiry | Sliding expiry (extension on activity) |
| A password change doesn't revoke other sessions | Revokes them |
| An edited download holds the whole image in RAM | Streaming the output |
| Missing FK on embeddings/faces | FK with `ON DELETE CASCADE` |
| Audit log outside the transaction (risk of loss) | Audit in the same transaction as the mutation |
| Manual per-face naming (laborious) | Auto-clustering of faces + bulk naming of a cluster |
| Global "favorite" | Per-user favorites (`user_favorites`) |
| Env-only configuration | YAML + env |
| Photo book (LaTeX, complex) | Omitted |

---

## 16. Open questions and risks to verify

1. **pgvector in `shared-postgres`** — is `CREATE EXTENSION vector` available? (blocking for M0)
2. **`halfvec`** requires pgvector ≥ 0.7 — verify the version; otherwise fall back to `vector` (float32).
3. **Mapy.com key** — a binding to a domain/referrer is not documented; keep the key server-side.
6. **Pi HW** — the real speed of pure-Go thumbnails and HEIC on the target Pi; possibly enable
   the `vipsthumbnail` shell-out. Measure the HNSW index build (maintenance_work_mem) on the Pi vs a build
   on the box/shared server.
5. **Inference models** — the image checkpoint and its `pretrained` field must stay pinned: change the
   checkpoint and every stored embedding is in a different space. That happened once, deliberately,
   in 08/2026 (CLIP ViT-L-14 → SigLIP 2 so400m/14): migration `0057` empties `embeddings`, widens the
   column and the library is re-embedded from scratch. `serve` warns at startup when the sidecar's
   `/health` reports a `clip.dim` other than the configured `embedding.image_dim`.

---

## 17. Reference

**Embeddings sidecar:** [`kozaktomas/image-embeddings`](https://github.com/kozaktomas/image-embeddings)
— FastAPI over SigLIP 2 so400m/14 (1152-dim image + text) and InsightFace `buffalo_l` (512-dim faces);
`server.py` is the contract in [§6.1](#61-kontrakt-sidecaru).

**mapy.com:** [REST API](https://developer.mapy.com/rest-api-mapy-cz/) ·
[Map tiles](https://github.com/mapycom/developer/blob/master/docs/rest-api/map-tiles.md) ·
[Reverse geocoding](https://github.com/mapycom/developer/blob/master/docs/rest-api/reverse-geocoding.md) ·
[Pricing](https://developer.mapy.com/pricing/).

**pgvector / ARM / inference:** [pgvector](https://github.com/pgvector/pgvector) ·
[HNSW vs IVFFlat](https://bigdataboutique.com/blog/hnsw-vs-ivfflat-how-to-choose-the-right-vector-index) ·
[disintegration/imaging](https://github.com/disintegration/imaging) ·
[libvips speed/memory](https://github.com/libvips/libvips/wiki/Speed-and-memory-use) ·
[Immich machine-learning](https://github.com/immich-app/immich/blob/main/machine-learning/README.md) ·
[open_clip pretrained](https://github.com/mlfoundations/open_clip/blob/main/docs/PRETRAINED.md) ·
[mdlayher/wol](https://github.com/mdlayher/wol) · [minio-go](https://pkg.go.dev/github.com/minio/minio-go/v7).

---

## 18. Breakdown into milestones (epics)

Detailed tasks are created in the **botka** system. The milestones are ordered for an early-visible UI.

- **M0 — Foundations:** repo scaffolding, Go module, config (YAML+env), DB+migrations, pgvector/halfvec
  (verify in shared-postgres), CI/build (goreleaser arm64 .deb), skeleton of the embedded frontend
  (react-bootstrap+Superhero+i18n), auth/users+sliding sessions, layout + back/history.
- **M1 — Storage & ingest:** storage layout, upload (multi-upload+mobile), dedup (SHA256+pHash),
  EXIF/GPS, thumbnailer on the Pi, photo CRUD, library with filters/sorting/pagination (a visible UI).
- **M2 — Jobs & embeddings:** persistent queue, sidecar client + health/offline, image
  embeddings, similar photos, semantic + full-text (hybrid) search.
- **M3 — People:** face jobs, markers/subjects, IoU matching, auto-clustering, suggestions, assignment UX, outliers, people pages.
- **M4 — Organization:** albums, labels, bulk metadata editing, per-user favorites, the map (mapy.com proxy), slideshow.
- **M5 — Import/migration:** the one-off import of the existing library (originals, increment, 1:1 embeddings). **Done 08/2026, then removed** — see [§9](#9-the-one-off-importers-s11s12--retired).
- **M6 — Backup & ops:** S3 backup (originals + dump), durable audit, rate-limiting, metrics, optional WoL auto-wake (`internal/wake`), hardening.
- **M7 — Polish:** photo detail (PP+PS combo), mobile/tablet, i18n completeness, slideshow effects, performance, non-destructive edits.

---

## 19. Quality, testing and linting

Robustness and extensibility are a first-class goal. Every task (including autonomous ones in botka) must
follow these rules; **a task is not done with red lint or tests.**

### 19.1 Linting (Go)
- **golangci-lint v2**, configuration **`.golangci.yml`**
  (a strict set of ~40+ linters: `revive`, `gosec`, `errcheck`, `errorlint`, `wrapcheck`,
  `cyclop`, `gocognit`, `funlen`, `dupl`, `goconst`, `gocritic`, `prealloc`, `sqlclosecheck`,
  `bodyclose`, `noctx`, `testifylint`, `thelper`, `usetesting`, `nilerr`, `lll` (120),
  `misspell`, `godot`, `nakedret`, `unparam`, `wastedassign`, …).
- Settings incl.: `funlen` 60/40, `gocognit` 15, `goconst` 3, `lll` 120. Exported symbols
  documented (`revive: exported`). `//nolint` only with justification (`nolintlint`).
- `gofmt`/`gofumpt` clean code.

### 19.2 Tests (Go)
- **Unit tests** for all business logic (table-driven, `testify`). Prefer pure functions without I/O
  → easily testable.
- **Integration tests** for DB repositories and HTTP handlers against a **real pgvector Postgres**
  (test DB `kukatko_test`, DSN in `KUKATKO_TEST_DATABASE_URL`). Harness: applies migrations,
  provides a clean state per test (truncate/transaction + rollback). When the env is missing, the integration
  tests `t.Skip` (so that the fast gate `make check` doesn't require a DB).
- **The `integration` build tag also lowers the bcrypt work factor.** ~15 packages seed accounts through the
  real auth path to exercise RBAC, and at the production cost 12 (multiplied by `-race`) those hashes were
  88 % of the suite's runtime (measured 39m46s → 6m00s). `internal/auth/password_cost_integration.go` —
  compiled **only** under the tag — mints at `bcrypt.MinCost` (override: `KUKATKO_TEST_BCRYPT_COST`).
  Verification is unaffected: a hash
  carries the cost it was minted at. The knob is a build-tag-selected identifier, not a settable `var`, so a
  running server cannot reach it, and `TestHashPassword_productionCost` in `make test` fails if a tagless
  build stops minting at 12. Details and the measured before/after in `docs/DEVELOPMENT.md`.
- **The R2 backend** has integration tests against a **real S3-compatible endpoint**
  (`KUKATKO_TEST_S3_ENDPOINT`, MinIO is enough; optionally `_BUCKET`/`_REGION`/`_ACCESS_KEY`/`_SECRET_KEY`):
  store/open/stat/delete, materialize + cleanup of the temp file (even on the error path) and a key with a UTF-8 name.
  Without the variable they are skipped, just like the DB tests. URL signing is a pure function → unit tests.
- External dependencies (the embeddings sidecar, mapy.com, S3) behind an **interface**
  (interface) → mocked/fake in tests; verify the sidecar contract with a contract test against
  a fake server too.
- Meaningful coverage of the logic (not vanity %). New behavior = new/updated tests.

### 19.3 Frontend tests
- **ESLint** (strict) + **Prettier**. **Vitest + React Testing Library** for components and hooks
  (especially filter state in the URL, i18n, auth flow). Cover the critical flows (login, upload, search).
- **Mocks are restored centrally, never per file.** `restoreMocks: true` in `vite.config.ts` restores
  every mock *before* each test, which is the only safe ordering; a test file that also calls
  `vi.restoreAllMocks()` in an `afterEach` empties the module mocks while the tree is still mounted, RTL's
  `cleanup()` then unmounts it, React flushes the pending passive effects and a service mock with no
  implementation returns `undefined` — the calling hook's `.then` throws. That was a real, order-dependent
  flake that failed a release build, so an ESLint rule (`no-restricted-syntax` on `**/*.test.{ts,tsx}`)
  bans the call; re-stub in `beforeEach` instead.
- (Optionally M7) **Playwright** E2E for a few key scenarios.

### 19.4 Make targets and the gate
```
make fmt              # gofmt/gofumpt + prettier (the only target that rewrites files)
make fmt-check        # format check without writing (golangci-lint fmt --diff + prettier --check)
make vet              # go vet (standalone; in the gate it's covered by govet inside golangci-lint)
make lint             # golangci-lint run + eslint
make lint-fix         # golangci-lint run --fix
make typecheck        # tsc -b --noEmit (frontend)
make test             # unit tests (no DB, CGO_ENABLED=0, no -race)
make test-race        # unit tests with the race detector (CGO_ENABLED=1) — in CI, not in the gate
make test-integration # integration tests (requires KUKATKO_TEST_DATABASE_URL)
make check            # docs-budget + fmt-check + lint + typecheck + test   ← gate (changes nothing)
make build            # frontend build + go build (embed)
```

### 19.5 CI and the gate in botka
- **GitHub Actions:** on push/PR run `make check` + `make test-race` + `make test-integration`
  with the service container `pgvector/pgvector:pg17` (env `KUKATKO_TEST_DATABASE_URL`) + frontend
  lint/test. The race detector is deliberately outside `make check`, so the pre-commit gate stays fast.
- **The project's Botka verification command = `make check`** → if a task leaves red lint
  or tests, it gets the `needs_review` state instead of `done`.
- Autonomous agents for Go code use the **golang-developer** skill (strict lint, documentation,
  tests).
