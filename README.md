# Kukátko

A standalone photo-management application — a replacement for PhotoPrism that combines the best
of PhotoPrism and of [photo-sorter](https://github.com/kozaktomas/photo-sorter), but is
**more robust and more usable**.

- **A single executable binary** (Go) including the embedded frontend (React + Bootstrap/Superhero).
- **PostgreSQL + pgvector** as the single source of truth for both metadata and vectors.
- **Semantic and full-text search**, similar photos, **face/people recognition**.
- **Global search as a command palette**: from any page via `/` or Cmd/Ctrl-K —
  keyboard-navigable grouped results (photos, people, albums, labels) in an overlay console.
- **A search query language**: free text + `key:value` filters in a single query —
  `dovolená camera:"Canon EOS R6" iso:100-400 faces:2`, `label:cat|dog`, `label:!blurry`,
  `near:<uid> dist:2`, `taken:2024-05`, `type:video`, `face:new`… OR operators (`|`), NOT (`!`,
  `-`), ranges (`800-`, `-200`), wildcard `*`, quotes; an unknown filter is searched as text
  and the UI flags it gently; a pure-filter query never touches the embedding sidecar. In-UI help
  (`?` next to the search field) and key autocomplete. Grammar: `docs/API.md`.
- **A library for an AI agent (MCP server):** Kukátko can expose itself as an **MCP server**
  (`POST /api/v1/mcp`), so an AI agent works with the library directly — searching, reading, organizing. It's not
  a toy: *"find all of grandma's photos from the sixties and put them in an album"* is the way this
  library is actually maintained. **Off by default** (a single key), the agent authenticates with an **existing API
  token** (`kkt_…`) and the **existing RBAC** applies to it — a viewer token reads and gets slapped
  down on every write; the `ai` role can write. **Nothing destructive is exposed**: no deletion, trash,
  archiving, restore, backup, or user management — the agent cannot delete photos. Every change it makes is
  written to the **audit** in the same transaction, so it is traceable just like a change made by a human. Details:
  [`docs/MCP.md`](docs/MCP.md).
- **Pi-first:** runs on a Raspberry Pi, delegating embedding computation to a powerful machine (a box with a GPU).
- **Import from PhotoPrism** via the API (+ downloading originals) and **data migration from photo-sorter**.
- **Uploading a folder from disk:** `kukatko import dir <path>` — you point it at a directory (scans, a card
  from a camera, an old backup) and it walks it recursively through the same pipeline as a browser upload.
  It only **copies** originals, skips junk with a reason, recognizes duplicates by SHA256 —
  so you **needn't fear running it again** (and `--dry-run` tells you up front what it would do).
- **Importing a Google Photos export without losing data or captions:** Takeout carries metadata **beside** the photo —
  the exported JPEG has its EXIF trimmed, and the real capture date, caption, and GPS sit in a `.json` file
  next to it. Kukátko reads them and attaches them to the right photo (it survives the whole mess of Google's names:
  `…supplemental-metadata.json` and truncated variants, a moved copy index), and likewise Apple `.xmp`.
  The date from the sidecar wins over the one Takeout falsely wrote into the EXIF during re-encoding; **albums
  are not created from the export** and whatever didn't pair up is **listed** — a silent mismatch is a way to lose
  decades of data. A folder imported earlier is fixed by a plain re-run (it only fills the gaps).
- **Approximate dates for historical photos:** with a scanned photo inherited from grandma you often know only "around
  1950" or "during the war". You tick **„Datum je odhad"** and write in your own words what you're basing it on —
  the photo is then shown everywhere with a **`cca`** marker and your note, so the estimate can't be confused
  with a definite date. Sorting, the timeline, and date filters keep running on `taken_at` unchanged, and a photo
  with no date at all can be an estimate too (the note carries the meaning).
- **Description and authorship of scanned photos:** for an inherited or scanned photo where the author, year, and caption
  live only in someone's memory (not in the file), you fill in on the photo detail the **subject, artist, copyright,
  license, and keywords** (as clickable chips) and mark that it is a **scan** of a physical photo.
  Everything is saved with a single button together with the title and description.
- **File groups of a single shot (stacks):** a camera in RAW+JPEG mode (and exported edits or
  copies) turns one shot into two or three tiles in the grid and double-counts it in every album. Kukátko
  **groups** them under one visible photo — the other variants stay available in a strip on its detail —,
  either by automatic detection (an admin action over the whole library) or manually from a selection. **Nothing
  is merged or deleted:** each file keeps its own row, so (un)grouping is reversible at any time.
- **Location estimation for photos without GPS:** many photos have no coordinates (a camera without a receiver, a scan, a trimmed
  export), but were taken the same day in the same place as photos that do have them — Kukátko
  **estimates** them from those, filling in the map and the place hierarchy. **Honestly:** an estimate is produced only when the neighboring
  photos really agree on a single place (within a few kilometers). When a day runs from Prague to Vienna, an honest
  answer doesn't exist and Kukátko **would rather write nothing** — a wrong location is worse than none, because it
  looks just as certain as a measured one and quietly poisons both the map and search. An estimate is **always visibly an estimate**
  (a label on the detail, otherwise a drawn pin on the map) and with one click you **confirm** it, or
  **discard** it — and a discarded one never comes back to you. The whole thing can be turned off with a single config key.
- **Find a person among untagged photos** (`/faces`, editors): you pick a person and Kukátko goes through
  photos where he isn't yet tagged and finds faces that resemble him. You set the threshold in **percent**
  (a tradeoff of "more results" ↔ "better matches"), a candidate is shown as a **colored rectangle over the whole
  photo** (you see the context, not a cropped chip). You confirm **in place with one click** or **with the keyboard**
  (`y`/`n`, arrows) — and **rejection is permanent**, so a wrong suggestion won't come back next time. „Potvrdit vše"
  runs through the whole batch. Unlike photo-sorter, where a rejection vanished and the face kept coming back.
- **Recognition sweep** (`/recognition`, editors): the same, but **for all people at once** and at
  **high confidence** — Kukátko runs through every named person and finds certain matches among untagged
  faces, grouped by person. The results stream in and appear **person by person** (a live bar
  with progress, cancelable), the work list **visibly shrinks** as you clear it — when you finish
  a person's last card, the whole person disappears. „Potvrdit vše" handles one person at a time, the keyboard works
  the same as in "Find a person". **It never assigns anything by itself** — confidence only narrows the list, a human confirms.
- **Collection expansion** (`/expand`, editors): for a whole album or label it finds photos that look
  like those already in it but aren't in it yet — the fastest way to finish off a half-tagged library
  ("show photos like those on the *Ostatky* label so I can add the ones I missed"). It searches **per-photo** and votes
  (not the average of the whole collection, which isn't one visual concept), skipping photos already in the collection and rejected ones.
  The page: you pick an album/label (sorted by photo count), you set the threshold in **percent** ("more
  results" ↔ "better matches", default 70%), each tile carries a **% similarity** and the **number of source
  photos** it matches — and the vote rule is spelled out, not a black box. You add the selected candidates with the ordinary
  **bulk edit prefilled with the collection being expanded** (added photos disappear from the results immediately),
  for a label, ✗ **permanently rejects** the photo, so it won't be offered again and repeated passes converge.
- **Possible mistakes** (`/outliers`, editors): the opposite task — not "who did I miss", but **"who do I have here
  by mistake"**. You pick a person and Kukátko sorts their faces starting from the one that resembles them least.
  The centroid is computed **robustly** (the most distant faces are discarded before the computation) — otherwise
  three badly assigned faces would pull the average toward themselves and mask exactly what you're looking for. Each
  card shows a **crop with its surroundings** (the face + a bit of the photo around it; from a tight crop you can't recognize a person)
  and asks "is this a mistake?": **✓ removes**, **✗ confirms it really is them** — and a confirmed face
  won't be offered again, so repeated passes converge instead of the same false alarms over and over.
  Threshold, bulk removal, and the keyboard (`y`/`n`, arrows, Ctrl+A). Kukátko **never removes anything
  by itself** — it only sorts and asks.
- **Sorting — a one-question game** (`/review`, editors): Kukátko shows you one photo and asks
  one thing — *"Is **Tomáš Kozák** in the photo?"* or *"Does the **Ostatky** label fit the photo?"*. You answer,
  and the next one appears. The questions aim **at the uncertainty band** (where the machine doesn't know and a human does), so
  every answer pays off. The photo is **full-screen** and the face has a frame **with a margin** —
  from a tight crop you can't recognize a person. **The keyboard is the main control**: `→` yes, `←` no,
  **spacebar** don't know, `z` undoes the last answer (a typo at speed is inevitable). The next
  card is **always preloaded**, so there's no waiting between questions — it runs like flipping through cards, not
  like filling out a form. The session shows **how many you've done and how many remain**, and nothing more: no
  score, streaks, or confetti. The reward is a tidy library.
- **Video playback** (HTTP range streaming + HTML5 player, live photos), maps
  ([mapy.com](https://mapy.com)), browsing by place (country/city), slideshow, albums, labels, bulk metadata editing,
  **multi-file uploads** (drag-and-drop / gallery / camera, with optional assignment of the whole
  batch to albums and labels), per-user favorites, a bilingual UI (Czech default + English), S3 backups.
- **User management** in the UI (`/users`, admin only): creating an account, changing role/name/note, resetting
  passwords, and disabling an account. Accounts are not deleted — they are retired by disabling so their history stays intact.
- **API tokens** (`Authorization: Bearer kkt_…`) for the CLI, scripts, and agents — a long-lived credential
  with its own expiry and revocation, inheriting its user's role. You create one via `POST /api/v1/auth/tokens`.
- **`kukatko ctl`** — a remote client that controls a **running** instance via its HTTP API
  (`kubectl`-style contexts, `-o json` for machine processing). Through the `kukatkoctl` symlink
  the `ctl` level is implied. Details: [`docs/OPERATIONS.md`](docs/OPERATIONS.md).

> **Status:** active development — features across milestones M0–M7 (§18 of the architecture) are implemented,
> fine-tuning is ongoing. Architecture:
> [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), developer guide:
> [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md), performance notes:
> [`docs/PERF.md`](docs/PERF.md), UI/UX audit + backlog:
> [`docs/UX_AUDIT.md`](docs/UX_AUDIT.md).
>
> PhotoPrism remains the **primary** system until the hard cutover to Kukátko; until then
> Kukátko runs in parallel and imports from PhotoPrism read-only.

## Documentation

| Document | Contents |
| --- | --- |
| [`CLAUDE.md`](CLAUDE.md) | Project conventions and hard rules |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Architecture, data model, milestones |
| [`docs/PACKAGES.md`](docs/PACKAGES.md) | Reference overview of Go packages |
| [`docs/API.md`](docs/API.md) | Reference overview of HTTP endpoints |
| [`docs/MCP.md`](docs/MCP.md) | MCP server for an AI agent — tools, auth, what is deliberately missing |
| [`docs/FRONTEND.md`](docs/FRONTEND.md) | Reference overview of the frontend |
| [`docs/OPERATIONS.md`](docs/OPERATIONS.md) | CLI, configuration keys, `make`, CI |
| [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) | Local development and build |
| [`docs/PERF.md`](docs/PERF.md) | Performance and measurement |
| [`docs/RESTORE.md`](docs/RESTORE.md) | Restore from backup |

## Quick start

You need **Go 1.26+**, **golangci-lint v2**, and **Node.js 22+** (npm) for the frontend.

```bash
make check            # quality gate: fmt-check + lint + typecheck + unit tests (Go and frontend)
make build            # frontend build (Vite) + static binary to bin/kukatko (CGO_ENABLED=0)

# serve and migrate need at least database.url (typically via env):
export KUKATKO_DATABASE_URL="postgres://kukatko:…@localhost:5432/kukatko"
./bin/kukatko migrate                     # runs pending DB migrations and exits
./bin/kukatko migrate photosorter         # read-only incremental data migration from photo-sorter
./bin/kukatko import photoprism           # read-only incremental import from PhotoPrism
./bin/kukatko import photoprism --album at8lq8ktxpl1thv4   # just a slice: album photos (+ --label/--person/--year),
                                          # each photo also comes with all the other albums and labels it carries
./bin/kukatko import dir /mnt/skeny --dry-run               # what would be imported from the directory (writes nothing)
./bin/kukatko import dir /mnt/skeny --album "Skeny 1985" --labels sken,rodina   # uploads a folder from disk
./bin/kukatko backup                      # one-off backup (pg_dump + sync of originals) to S3
./bin/kukatko restore list                # lists dumps available in the bucket (newest first)
./bin/kukatko restore db --yes            # restores the DB from the newest dump (DESTRUCTIVE) + migrations
./bin/kukatko restore originals           # downloads missing originals from S3 (skips existing)
./bin/kukatko restore verify              # integrity report: photos in the DB vs originals on disk
./bin/kukatko maintenance scan            # library integrity check (disk↔DB drift, derived data)
./bin/kukatko maintenance repair --thumbnails --phashes  # opt-in repairs (thumbnails/hashes/embeddings/faces/orphans)
./bin/kukatko serve                       # runs migrations, then the HTTP server (default 0.0.0.0:8080)
./bin/kukatko serve --config config.yaml  # explicit path to the config
./bin/kukatko version                     # prints the version and commit

# ctl talks to a RUNNING instance via the HTTP API — it needs neither database.url nor originals:
printf '%s' "$KUKATKO_TOKEN" | ./bin/kukatko ctl config set-context prod \
    --server https://kukatko.example.com --token-stdin
./bin/kukatko ctl photos list --year 2024 --limit 5
./bin/kukatko ctl photos search "západ slunce" --mode semantic -o json
```

`migrate photosorter` needs a read-only DSN of the photo-sorter DB in `import.photosorter.dsn`
(`KUKATKO_IMPORT_PHOTOSORTER_DSN`); without it, both the command and its admin trigger
`POST /api/v1/import/photosorter` fail / are not registered.

## Database and migrations

Kukátko uses **PostgreSQL + pgvector** (types `vector`/`halfvec`) and **unaccent**. Migrations
are SQL files embedded in the binary (`internal/database/migrations/NNNN_name.sql`); they run
automatically at `serve` startup (or manually via `kukatko migrate`) in ascending
version order, each in its own transaction, and are recorded in the `schema_migrations` table (idempotent —
never applied twice).

Integration tests run against a real test DB:

```bash
export KUKATKO_TEST_DATABASE_URL="postgres://kukatko:…@localhost:5432/kukatko_test"
make test-integration   # without KUKATKO_TEST_DATABASE_URL the DB tests are skipped (t.Skip)
```

### Photo schema (`internal/photos`)

The core of the catalog is in migration `0003_photos.sql` and the `internal/photos` package:

- **`photos`** — one row per photo/video; PK `uid` (app-generated, prefix `ph`),
  dedup on **SHA256** `file_hash` (UNIQUE), EXIF/camera/GPS metadata, `exif` JSONB (GIN),
  `archived_at`, `uploaded_by` (FK `users` `ON DELETE SET NULL`). External IDs for
  import/migration: `photoprism_uid`, `photoprism_file_hash` (SHA1), `photosorter_uid`.
  The `private` column is **legacy**: it remains only so that import from PhotoPrism/photo-sorter
  can keep mirroring their flag. The application nowhere filters, edits, or displays it —
  it was never a security boundary (a private photo was served like any other).
  **Approximate date** (migration `0029_photos_taken_at_estimate.sql`): `taken_at_estimated`
  (the date is an estimate, not a fact) + `taken_at_note` (free text about the dating, max 500 chars). `taken_at`
  remains the sole anchor for sorting/timeline/filters; the note is kept only for an estimate (drop the flag →
  the server deletes the note).
  **Video** (migration `0004_video.sql`): `media_type` (`image`/`video`/`live`, default `image`,
  CHECK + partial index) + `duration_ms`, `video_codec`, `audio_codec`, `has_audio`, `fps`
  (populated only for videos). A live photo = a still as the primary image + a motion clip as another
  `photo_files` row.
  **Full-text** (migration `0007_fts.sql`): a generated `fts tsvector` column (GIN index) =
  `setweight` over `to_tsvector('simple', immutable_unaccent(...))` from title (A) > description
  (B) > notes (C) > normalized file_name (D); diacritics-insensitive via the IMMUTABLE
  `immutable_unaccent` wrapper. `GENERATED ALWAYS … STORED` keeps `fts` up to date on every
  metadata insert/update without a trigger.
- **`photo_files`** — the original + derivatives, `role` original/sidecar/edited, at most one
  `is_primary` per photo. **`photo_phashes`** — `phash`/`dhash` (near-dup). **`photo_edits`**
  — non-destructive edits (crop 0..1 all-or-nothing, rotation 0/90/180/270, brightness/contrast).
  The satellite tables have an FK `ON DELETE CASCADE`.
- **Performance indexes** (migration `0015_perf_indexes.sql`): partial composite indexes precisely
  matching the grid's most common sort order — `idx_photos_live_taken_at (taken_at DESC NULLS
  LAST, uid DESC) WHERE archived_at IS NULL` and `idx_photos_live_created_at (...)` for `sort=added`.
  Thanks to them the timeline page is an **index scan without a Sort** (the original `idx_photos_taken_at` was
  NULLS FIRST without a `uid` tiebreak, so it forced a sort of the whole set). Details + EXPLAIN test:
  [`docs/PERF.md`](docs/PERF.md).

`photos.Store` (over the pgx pool) offers `Create`, `GetByUID`/`GetByFileHash`/
`GetByPhotoprismUID`/`GetByPhotosorterUID`, `UpdateMetadata`, `Archive`/`Unarchive`,
`Delete`, `List`/`Count` (archived/uploader filter, sorting, pagination),
`Search` (full-text over the `fts` column, sorting by `ts_rank`, honors the list filters +
pagination; empty query → `ErrEmptySearch`), `FilterUIDs` (from a set of uids returns those
that pass the structural list filters — for semantic search: it filters the vector
candidates) and methods for files/phash/edits.

### Originals storage (`internal/storage`)

The on-disk layer for original media. The `Storage` interface + the filesystem implementation `FS`
(`NewFS(root)`):

- **`Store(ctx, src, takenAt, originalName)`** streams the input to disk (never holding the whole
  file in RAM), computes **SHA256** during the write, and returns `StoredFile{Hash, RelPath, Size, MIME}`.
  The layout is `YYYY/MM/<filename>` (date from `taken_at`, falling back to import time). The write is
  crash-safe and race-free: data goes to a temp file in `<root>/.tmp` and is published by an **atomic
  hard link** to the target path.
- **Name collisions:** the same name + **identical content** → returns the existing `StoredFile` with the sentinel
  `ErrAlreadyExists` (a dedup signal for the caller); the same name + **different content** → stores it under
  a numeric suffix (`name_1.ext`), never overwriting. The authoritative catalog dedup is the DB's job
  (`photos.file_hash` UNIQUE).
- **`Open`/`Stat`/`Delete`/`Materialize`** work with a relative path; all paths are
  confined to the root (no escape via `..`), invalid paths return `ErrInvalidPath`.
- The `Storage` interface **does not assume a filesystem**. `URL(relPath)` returns an address that the
  client hits directly (FS returns `""` — originals on disk are not accessible over HTTP; they are served by
  the application). `Materialize(ctx,relPath)` returns a real local file for tools that only understand a
  file name (exiftool, ffprobe, ffmpeg, heif-convert, vipsthumbnail), plus a `cleanup` that the
  caller **always** calls — even on the error path. FS does not copy: it returns the path of the original itself
  and a no-op `cleanup`.
- **MIME** is detected from content (sniffing the first 512 B) with the extension as a fallback; the
  `mediaTypeByExt` table covers formats stdlib doesn't know (HEIC/HEIF/AVIF, RAW, container video).

### Thumbnails / thumbnailer (`internal/thumb` + `internal/imgconvert`)

Generating and caching derived JPEG thumbnails directly on the Pi, **without CGO** (pure-Go decoding +
shelling out to external tools for HEIC/RAW).

- **Size registry** (`internal/thumb`): named sizes in two modes — `fit_*`
  (longest side ≤ limit, preserves aspect ratio, never upscales) and `tile_*` (center-crop to a square).
  Default set: `fit_720/1280/1920/2560/3840` and `tile_100/224/500` (JPEG quality ~85–90).
  The set is easy to extend — add an entry to `sizes` + `sizeOrder` and it propagates through the whole
  pipeline. Introspection: `SizeNames()`, `IsValidSize(name)`.
- **Cache layout** under `storage.cache_path`: `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`
  (sharded by the first three byte-pairs of the original's hex SHA256). Fully regenerable
  from the originals and **idempotent** — an existing size is neither regenerated nor overwritten. The write
  is atomic (temp + rename), so a parallel write of the same content converges race-free.
- **API** (`thumb.New(store, cachePath)`): `Generate(ctx, photo, sizes...)`,
  `GenerateAll(ctx, photo)` (returns a map `size → absolute path`), `Path(hash, size)`,
  `Open(hash, size)` (returns `ErrNotCached` until the thumbnail exists). The source is decoded
  **once per photo**, the individual sizes are encoded in parallel with bounded concurrency
  (`WithConcurrency(n)`, default `GOMAXPROCS`, bound via `thumb.concurrency`). **EXIF
  orientation** (`photo.FileOrientation`, 1–8) is applied automatically.
- **Optional vips engine** (`thumb.engine: vips`, `WithVips(bin)`): pure-Go decoding of large
  JPEGs is slow and memory-heavy on the Pi (~1 s / ~90 MB for a single `fit_720` from a 12 MP photo,
  ~4 s / ~1.18 GB for all sizes — see [`docs/PERF.md`](docs/PERF.md)). `vips` switches
  JPEG/PNG/WebP thumbnails to a **shell-out to `vipsthumbnail`** (`internal/thumb/vips.go`) — significantly
  faster and more memory-efficient, **still without CGO** (a standalone binary, not libvips bindings).
  Pure-Go stays the default; vips **falls back per-photo** to pure-Go for other formats
  (HEIC/RAW/video via `imgconvert`) and on any vips failure, so it never changes the output —
  only the speed. Same semantics (fit `WxH>` without upscaling, crop `--smartcrop centre`, EXIF
  auto-rotation). `serve` logs the active engine and warns when `vips` is requested but
  `vipsthumbnail` is not on the PATH (apt `libvips-tools`).
- **HEIC/RAW/video** (`internal/imgconvert`): `EnsureDecodable(ctx, path)` returns the path to a file
  that `image.Decode` can handle. **JPEG/PNG/WebP/BMP/GIF/TIFF** pass through unchanged (pure-Go decoders,
  an animated GIF is thumbnailed from its first frame); **HEIC** is converted via `heif-convert` to a temporary
  JPEG; **RAW** (cr2/cr3/nef/nrw/arw/srf/dng/raf/orf/rw2/pef/srw/3fr/iiq/x3f/kdc/mrw/mef) extracts
  the **embedded JPEG preview** via `exiftool -b -PreviewImage` (fallback `-JpgFromRaw`/
  `-ThumbnailImage`) instead of a full demosaic; **video** (mp4/mov/m4v/avi/mkv/webm/…) delegates to
  `internal/video.ExtractPoster` (poster frame via `ffmpeg`). This lets both the thumbnailer and pHash
  process the video poster exactly like a photo. Detection goes by **magic bytes** (content wins over
  a wrong extension), with one exception: TIFF magic can't carry RAW — most RAW containers are
  TIFF-based, so the RAW extension takes precedence and the file goes through embedded-preview. A missing external
  tool returns a clear `ErrConverterMissing` (or `video.ErrFFmpegMissing`). Runtime apt dependencies:
  `libheif-examples`/`libheif-bin`, `libimage-exiftool-perl`, `ffmpeg`.

### Video (`internal/video`)

CGO-free shell-out to the **FFmpeg suite** (`ffprobe`/`ffmpeg`):

- **`Probe(ctx, path)`** — metadata via `ffprobe -print_format json -show_format -show_streams`:
  `duration_ms`, video/audio codecs, `has_audio`, `fps` (parsing the rational `30000/1001`),
  dimensions, `creation_time` (→ `taken_at`), GPS (ISO 6709 `+lat+lng+alt/`). **Fallback to
  `exiftool`** (via `internal/exif`) when `ffprobe` is missing; the whole probe document is stored in
  `photos.exif`.
- **`ExtractPoster(ctx, path)`** — a representative frame via `ffmpeg` (~1 s into the clip, falling back
  to the first frame for shorter ones) to a temporary JPEG + once-only cleanup.
- **`IsVideoPath` / `FFmpegAvailable` / `FFprobeAvailable`** + sentinels `ErrFFmpegMissing`/
  `ErrFFprobeMissing`/`ErrNoMetadataTool`/`ErrPosterFailed`.
- **Playback / streaming** — videos are served via `GET /photos/{uid}/video` with **HTTP Range**
  (`http.ServeContent`: 206 partial, `Accept-Ranges`, seek without downloading the whole file, memory-
  bounded from an `*os.File`); a live photo streams its motion clip. **Optional on-the-fly transcoding**
  of non-web-friendly codecs (HEVC/H.265 …) to H.264/MP4 via `ffmpeg` — `IsWebFriendlyCodec` +
  `TranscodeArgs` (fragmented MP4 to `pipe:1`) + `Transcode`. Default **off**
  (`video.transcode`): transcoding is CPU-heavy, runs on every playback (not cached) and can't be
  seeked precisely; off = the video is streamed as-is and the frontend offers a download when the browser
  can't decode it. Frontend: HTML5 player on the photo detail + play badge/duration on the tiles.

### EXIF / GPS metadata (`internal/exif`)

Metadata extraction at import time, **without CGO**. `exif.Extract(ctx, path)` returns `exif.Metadata`
(maps 1:1 onto the `internal/photos.Photo` columns): `TakenAt` + `TakenAtSource`
(`exif`/`filename`/`unknown`), `Lat`/`Lng`/`Altitude`, `CameraMake`/`CameraModel`/`LensModel`,
`ISO`/`Aperture`/`Exposure`/`FocalLength`, `Width`/`Height`/`Orientation`, `Mime` and the full EXIF
as a JSON-able map (`Exif`).

- **Primary path**: shell-out to `exiftool -json -n` (numeric values → deterministic
  parsing of dimensions, orientation and coordinates). **Fallback** to a pure-Go parser
  (`rwcarlsen/goexif`) when `exiftool` is not on the PATH or fails — the fallback also reads dimensions/MIME
  via `image.DecodeConfig` + `http.DetectContentType`.
- **GPS**: EXIF rational coordinates are converted to decimal degrees, hemisphere per the
  `N/S/E/W` refs (south/west → negative); `GPSAltitudeRef = 1` → negative altitude.
- **`taken_at`**: prefers EXIF `DateTimeOriginal` (zone-less times treated as UTC); when missing,
  it tries a date from the **filename** (`IMG_20230115_143052`, `2023-01-15 14.30.52`, …); otherwise
  `source = unknown`.
- **Tolerance**: a file without EXIF (e.g. a PNG screenshot) **is not an error** — it returns zero values,
  not an error. An error only for an empty path / unreadable file.
- Runtime apt dependency (optional, otherwise fallback): `libimage-exiftool-perl`.

### Perceptual hashes (`internal/phash`)

Pure-Go (without CGO) computation of two 64-bit perceptual hashes for detecting **similar**
(not just byte-identical) photos: **pHash** via a 2-D DCT (32×32 → low-freq 8×8 block, threshold
the median without DC) and **dHash** (gradient, 9×8). `phash.Compute(img)` returns `Hashes{Phash, Dhash int64}`,
`phash.Distance(a, b)` is the Hamming distance. Stored in `photo_phashes`; a smaller distance
= more visually similar. The near-duplicate query `photos.Store.NearestPhash(ctx, phash)` computes
the distance in Postgres (`bit_count(phash::bit(64) # …)`).

### Upload / ingest pipeline (`internal/ingest`)

The **`POST /api/v1/upload`** endpoint (editor/admin access) accepts `multipart/form-data` with one
or more files and **streams** them (never holding a whole file in RAM). Per-file pipeline:

1. Stream to a temporary file + a running **SHA256**.
2. **Exact-dup** by SHA256 (`photos.GetByFileHash`) → a friendly per-file "duplicate".
3. Media-type detection by extension (`video.IsVideoPath`). **Photo** → EXIF/GPS (`internal/exif`);
   **video** → `media_type=video` + probe (`internal/video.Probe`: duration/codecs/fps/GPS/time),
   `taken_at` falls back to the original filename. Then the original is published to storage (`YYYY/MM`).
4. Insert `photos` (incl. video columns) + the primary `photo_files` + computing **pHash/dHash** →
   `photo_phashes` (from the poster frame for a video).
5. Generating **thumbnails** (thumbnailer) — from the poster frame for a video, so the grid shows the poster.
6. **Enqueue** the `image_embed` + `face_detect` jobs via `ingest.JobEnqueuer` — `serve` injects
   the persistent `jobs.Enqueuer` (see [job queue](#persistent-job-queue-internaljobs)), so
   a new photo immediately gets its embedding/face jobs; in tests without a queue, `NopEnqueuer` is used.
   For a video they run on the poster frame, so it participates in semantic/face search.

Video requires **`ffmpeg`** (the poster has no fallback) — a missing `ffmpeg` on a video upload returns
a clear per-file error `video.ErrFFmpegMissing`. `ffprobe` has a fallback to `exiftool`.

Returns a **per-file** list of results `{filename, status, outcome (created/duplicate/error),
photo_uid?, error?, warnings?}` with 409 duplicate semantics in `status` (the overall response is 200,
so partial batches report cleanly). **Race**: concurrent uploads of identical content converge
onto a single photo (storage hard-link + unique constraint `file_hash`), the loser gets a clean
duplicate, not a 500. **Near-duplicate** (config-gated `duplicate.*`): if a very similar
pHash exists, the result carries a `warning` (non-blocking). File-size limit: `upload.max_file_size_mb`
(0 = no limit).

### Persistent job queue (`internal/jobs`)

A durable, Postgres-backed queue (migration `0005_jobs.sql`, table `jobs`) — the main
robustness improvement over photo-sorter, whose in-memory jobs were lost on restart. Jobs
survive a restart, retry with exponential backoff, dedup by photo and wait in `queued`
when the embeddings box is offline (upload and browsing work without it).

- **The `jobs` table**: `id BIGSERIAL`, `type`, `state` (`queued`/`running`/`done`/`failed`/`dead`),
  `priority`, `payload` JSONB, `attempts`/`max_attempts` (default 5), `last_error`, `run_after`
  (backoff/deferral), `locked_by`/`locked_at`, `created_at`/`updated_at`. Index on
  `(state, run_after, priority)`; **dedup** = a partial unique index on
  `(type, payload->>'photo_uid') WHERE state IN ('queued','running')` (a NULL photo_uid is not deduped,
  so jobs without a photo — e.g. `backup` — don't collide).
- **`Store`** (`jobs.NewStore(pool)`):
  - `Enqueue(ctx, type, payload, opts)` — idempotent with respect to the dedup key; an active duplicate returns
    `ErrDuplicate`. `EnqueueOptions{Priority, MaxAttempts, RunAfter}`.
  - `Claim(ctx, workerID, types...)` — atomically takes the next runnable job via
    `SELECT … FOR UPDATE SKIP LOCKED` (`run_after <= now()`, ordered `priority DESC, run_after ASC,
    id ASC`), marks it `running` + `locked_by`/`locked_at`. Empty queue → `ErrNoJobs`. Concurrent
    workers never get the same job.
  - `Complete(id)` / `Fail(id, err)` — `Fail` increments `attempts`; while `attempts < max_attempts`
    it requeues with exponential backoff via `run_after` (base 30 s, cap 1 h), otherwise `state=dead` +
    `last_error` (dead-letter).
  - `Defer(id, delay)` — requeues a running job to `now()+delay` **without** counting an attempt (the
    `attempts` attribute is unchanged, the lock is released). For transient, error-free states — mainly when the
    embeddings box is offline — so the job waits in the queue for the box to return without exhausting its retry budget.
  - `Heartbeat(id, workerID)` + `RecoverStaleLocks(staleAfter)` — running jobs with a stale lock
    (a dead worker) are requeued (counted as an attempt); a heartbeat refreshes the lock and protects against recovery.
  - Helpers: `CountsByState` / `CountsByType`, `ListDead`, `RequeueDead`, `Requeue` (dead **and**
    failed → queued), `List(ListOptions{State,Limit,Offset})` (a recent listing, ordered
    `updated_at DESC`, limit cap 500), `Get`.
- **`jobs.Enqueuer`** (`NewEnqueuer(store)`) implements `ingest.JobEnqueuer`
  (`EnqueueImageEmbed`/`EnqueueFaceDetect` with payload `{"photo_uid": …}`, `ErrDuplicate` =
  no-op) — wiring the queue into the upload.

### Background worker (`internal/worker`)

The execution loop that drains the queue runs **inside the `kukatko serve` process**:

- **`Registry`** (`NewRegistry()`) maps `type` → `HandlerFunc` (`func(ctx, jobs.Job) error`)
  via `Register(type, fn)`; panics on an empty type, a nil handler or a duplicate registration
  (a programmer error at startup). A built-in **noop** handler (`TypeNoop`, `RegisterBuiltins`)
  only for sanity/tests; the real `image_embed` handler registers `embedjob.Service.Handle`
  (see below), `face_detect` and others are added by later milestones.
- **`RetryAfter(delay, cause)` / `RetryAfterError`** — a handler uses it to signal "a transient error,
  retry later without counting an attempt". The worker recognizes such a result (`errors.As`) and instead of `Fail`
  calls the queue's `Defer(delay)`. Used by `image_embed` when the box is offline.
- **`Worker`** (`New(Config{Queue, Registry, Concurrency, PollInterval, StaleAfter,
  StaleScanInterval, IDPrefix})`) — `Run(ctx)` starts `Concurrency` goroutines that poll
  `Claim` (filtered to the registered `Types`), dispatch a job to a handler by `job.Type` and, per
  the result, call `Complete`/`Fail`. The bookkeeping (`Complete`/`Fail`) runs on a
  **shutdown-immune** context (`context.WithoutCancel`), so a result computed just before
  shutdown is still persisted. Alongside the workers runs a **stale-lock recovery** ticker.
- **Graceful shutdown**: cancelling `ctx` (SIGINT/SIGTERM) stops claiming; a job running at
  shutdown is **abandoned** (its lock is later requeued by the queue via `RecoverStaleLocks`), `Run`
  returns cleanly. A handler panic → `ErrHandlerPanic` (the job is failed, the worker doesn't crash),
  an unknown job type → `ErrNoHandler`.
- **`Queue`** is an interface = a subset of `jobs.Store` (`Claim`/`Complete`/`Fail`/`Defer`/
  `RecoverStaleLocks`), so the runtime can be unit-tested with a fake.
- Tuning via `worker.*` config (`count`, `poll_interval`, `stale_after`,
  `stale_scan_interval`).

### Wake-on-LAN auto-wake of the box (`internal/wake`)

Optionally **wakes the GPU box via Wake-on-LAN** when embedding jobs are waiting in the queue and the sidecar
is offline, so the queue catches up without manually powering on the box. **Off by default** and fully inert.

- **Trigger:** `Run(ctx, interval)` runs in its own goroutine in `serve` (every minute) and sends
  a magic packet **only** when `embedding.wake.enabled`, the number of waiting (`queued`/`running`)
  `image_embed`/`face_detect` jobs reaches `min_queue`, **the cooldown has elapsed** and the health check
  reports the sidecar **offline**. After `GracePeriod` (30 s) it re-checks health and logs whether the box
  came up; otherwise it backs off until the next cooldown. The loop **never blocks job processing** and
  only logs errors.
- **Network:** Wake-on-LAN **does not work over Tailscale** (an L3 overlay without L2 broadcast) — the host must be
  on the same physical LAN as the box. The default is a UDP broadcast to `broadcast_addr` (the
  `mdlayher/wol` library), optionally a raw Ethernet frame on `interface` (requires CAP_NET_RAW). Putting the
  box to sleep is out of scope.
- **Testability:** `QueueDepth`/`HealthChecker`/`Sender` are interfaces → unit tests run with
  a fake sender, **no real network traffic**; `Packet(mac)` builds the magic packet (102 B) and is
  tested separately.
- Config `embedding.wake.{enabled,mac,broadcast_addr,interface,min_queue,cooldown}`; enabled
  requires a valid `mac` (otherwise `ErrInvalidWake` at startup).

### Admin Jobs API (`internal/jobsapi`)

An admin-only HTTP API over the queue (guard `RequireAdmin`), the frontend polls it (no SSE):

- `GET /api/v1/jobs/stats` → `{by_state, by_type, total}` (aggregated counts for the dashboard).
- `GET /api/v1/jobs` → `{jobs, limit, offset}` (recent / dead-letter listing; query `state`,
  `limit` ≤ 500, `offset`; an invalid parameter → 400).
- `POST /api/v1/jobs/{id}/requeue` → the refreshed job (dead/failed → `queued`; 404 missing,
  409 non-requeueable).

### Embeddings sidecar client (`internal/embedding`)

An HTTP client for the inference service (a sidecar on the **box** — a GPU machine that is often offline). The same
contract as photo-sorter; everything behind the `Client` interface (fakeable in tests, no real network):

- `ImageEmbedding(ctx, img io.Reader)` → `POST {url}/embed/image` (multipart field `file`,
  streamed via `io.Pipe`) → a 768-dim CLIP vector + `model`/`pretrained`.
- `TextEmbedding(ctx, text)` → `POST {url}/embed/text` (JSON `{text}`) → a 768-dim vector in a
  space shared with the images.
- `FaceEmbeddings(ctx, img io.Reader)` → `POST {url}/embed/face` (multipart `file`) → `[]Face`
  (a 512-dim embedding, `bbox` in pixels `[x1,y1,x2,y2]`, `det_score`) + `model`.
- `Healthy(ctx)` → a cheap probe to `{url}/health`: any HTTP response = the box is reachable, only
  transport-error/timeout = offline.

**Box offline-aware:** typed errors distinguish **`ErrUnavailable`** (transport failed or
status 502/503/504 — retryable, helper `IsUnavailable`) from **`ErrBadResponse`** (a malformed
response) and **`ErrDimMismatch`** (wrong vector dimension, validation 768/512). A cancelled context is
not reported as unavailability. Base URL from `embedding.url`, dimensions from `embedding.image_dim`/`face_dim`;
timeouts have sensible defaults (request 60 s, health 5 s). The `image_embed`/`face_detect` jobs
wait in the queue through this until the box comes up.

### Embeddings & faces schema (`internal/vectors`)

Embeddings are stored **directly in PostgreSQL** as `halfvec` (float16) columns with **HNSW
cosine** indexes (migration `0006_embeddings.sql`) — no external vector store, a similarity
search is a plain SQL query via the `<=>` operator. `halfvec` instead of `vector` (float32) **halves
the HNSW index's memory** with negligible recall loss on normalized CLIP/ArcFace vectors,
which matters on the Pi.

- **`embeddings`**: one CLIP image embedding per photo (`photo_uid` PK FK `ON DELETE
  CASCADE`, `embedding halfvec(768)`, `model`/`pretrained`/`dim`/`created_at`), HNSW index
  `hnsw (embedding halfvec_cosine_ops) WITH (m=16, ef_construction=200)`.
- **`faces`**: zero or more detected faces per photo (`id` BIGSERIAL, `photo_uid` FK
  `ON DELETE CASCADE`, `face_index`, `embedding halfvec(512)`, `bbox DOUBLE PRECISION[4]`
  normalized `[x,y,w,h]` 0..1, `det_score`, `model`/`dim`/`created_at` + cache columns
  `marker_uid`/`subject_uid`/`subject_name`/`photo_width`/`photo_height`/`orientation`),
  `UNIQUE(photo_uid, face_index)` + HNSW index on `embedding`. The FK `ON DELETE CASCADE` fixes
  a gap in photo-sorter, where embeddings/faces had no FK and orphans arose.
- **`face_detections`** (migration `0009_face_detections.sql`): one row per photo that has gone through
  face detection (`photo_uid` PK FK `ON DELETE CASCADE`, `face_count`, `model`, `detected_at`).
  Because `faces` may have zero rows, this table is the only way to tell a photo **without
  faces** from a photo **not yet processed** — it holds the idempotence of the `face_detect` job and the backfill.

`vectors.Store` (`NewStore(pool)`) over the shared pgx pool:
`SaveEmbedding`/`GetEmbedding` (`ErrEmbeddingNotFound`), `FindSimilar(vec, limit, maxDistance)`
over `embedding <=> $vec` (nearest first), `SaveFaces`(idempotent replace in a transaction)/
`ListFaces`/`DeleteFaces`, `FindSimilarFaces`,
`FindSimilarFaceCandidates(vec, limit, maxDistance)` (like `FindSimilarFaces`, but also returns cached
`subject_uid`/`subject_name`/`marker_uid` and `bbox` — the basis for identity suggestions),
`UpdateFaceMarker(photoUID, faceIndex, markerUID, subjectUID, subjectName)` (writes the cache columns
for a single face; an empty `markerUID`/`subjectUID` → `NULL` — this is how an IoU match is cached and a
face is linked to a marker), `RecordFaceDetection(uid, faces, model)` (atomically replaces the photo's faces **and** writes
a `face_detections` row — even for zero faces), `FacesDetected(uid)` (does a `face_detections`
row exist?), `ListPhotosMissingFaces(limit)`. Queries run in a **read-only transaction** with
`SET LOCAL hnsw.ef_search = 100` for better recall; `limit` is clamped to `[1,500]`,
a non-positive `maxDistance` disables the filter. Helpers `ToHalfVec`/`FromHalfVec` (`[]float32` ↔
`pgvector.HalfVector`), dimension validation via `ErrDimMismatch`, a duplicate `face_index` →
`ErrFaceIndexTaken`. `ListPhotosMissingEmbedding(limit)` returns the uids of non-archived photos without an
embedding (LEFT JOIN on `embeddings`, newest first; `limit <= 0` = all) and
`ListPhotosMissingFaces(limit)` analogously the uids of photos without a `face_detections` row — inputs for the
backfill.

### Subjects & markers (`internal/people`)

Named **subjects** (people / animals / other) and **markers** (face/label regions on photos)
in migration `0008_subjects_markers.sql`:

- **`subjects`**: `uid` PK (prefix `su`), `slug` UNIQUE (generated from `name`, diacritics-free and
  unique thanks to a numeric suffix), `name`, `type IN (person|pet|other)`, `favorite`, `private`,
  `notes`, `cover_photo_uid` (FK photos `ON DELETE SET NULL` — the subject survives deleting the cover
  photo), `created_at`/`updated_at`.
- **`markers`**: `uid` PK (prefix `mk`), `photo_uid` (FK photos `ON DELETE CASCADE`),
  `subject_uid` (FK subjects `ON DELETE SET NULL`), `type IN (face|label)`, normalized bbox
  `x,y,w,h DOUBLE PRECISION` (0..1 display space, same convention as `faces.bbox`), `score`,
  `invalid`, `reviewed`, timestamps; indexes on `photo_uid` and `subject_uid`.

`people.Store` (`NewStore(pool)`) over the shared pgx pool:

- **Subjects:** `CreateSubject` (generates a uid + a **unique slug from the name** via `Slugify`,
  collision → `name-2`/`name-3`/…), `GetSubjectByUID`/`GetSubjectBySlug`, `UpdateSubject`
  (re-slugging on a name change + refreshing the `faces.subject_name` cache), `ListSubjects`
  (subjects with a count of **non-invalid** markers, ordered by name), `DeleteSubject` (the FK detaches
  markers to `NULL`, clears the faces cache).
- **Markers:** `CreateMarker` (validates the type and `0..1` bounds → `ErrInvalidType`/
  `ErrInvalidBounds`; optionally with a subject right away), `GetMarkerByUID`, `ListMarkersByPhoto`,
  `AssignSubject`/`UnassignSubject`, `SetMarkerInvalid`/`SetMarkerReviewed`, `DeleteMarker`.

**Consistency of the denormalized faces cache:** `faces` (migration 0006) holds the cache columns
`marker_uid`/`subject_uid`/`subject_name` for fast rendering. `people` keeps them in
sync — every change to a marker/subject (assign/unassign, renaming a subject, deleting a marker
or a subject) updates the corresponding `faces` rows **in the same transaction** (`WHERE marker_uid =
$1`, or `WHERE subject_uid = $1` respectively). Sentinels: `ErrSubjectNotFound`/`ErrMarkerNotFound`/
`ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidBounds`.

### Face↔marker matching, assignment & suggestions (`internal/facematch`)

Links detected faces to markers/subjects and suggests likely identities. Everything behind
interfaces (`PhotoStore`/`FaceStore`/`PeopleStore`), so it unit-tests with fakes without a DB.

- **IoU geometry** (`IoU(a, b [4]float64)`, pure function): Intersection-over-Union of two
  normalized boxes `[x,y,w,h]` (0..1). `findBestMarker` picks the most-overlapping **face**
  marker (ignores `invalid`), a match holds when `IoU ≥ faces.iou_threshold` (default 0.1, mirroring
  photo-sorter).
- **`PhotoFaces(ctx, photoUID)`** (backing `GET /photos/{uid}/faces`): for every stored face
  computes the best marker by IoU, determines the action (`create_marker` / `assign_person` / `already_done`),
  **caches the match on the face row** (`UpdateFaceMarker`) and adds suggestions to **every** face with an
  embedding — naming candidates for an unnamed one, **reassignment alternatives** for an assigned one
  (never suggesting the person it already carries). Markers without a matching face are attached (negative
  `face_index`) for the detail UI.
- **Suggestions** (`aggregateSuggestions`, pure function): from the nearest face embeddings
  (`FindSimilarFaceCandidates`, HNSW cosine) aggregates candidates by subject, excludes faces on
  the same photo, subjects **already assigned on the photo** (other people) and faces **below the minimum
  size** (`faces.min_face_size`), sorts by average distance, `confidence = 1 −
  distance`, limit `faces.suggestion_limit` (~5). Primary threshold `faces.suggestion_max_distance`,
  with a **fallback** to unbounded distance when suggestions are few.
- **Assignment (state machine)** (`Apply(ctx, AssignRequest)`, backing
  `POST /photos/{uid}/faces/assign`, editor/admin): `create_marker` (creates a face marker + assigns
  the subject + links the face), `assign_person` (assigns a subject to an existing marker),
  `unassign_person` (detaches the subject). Keeps the `faces` cache and `marker.reviewed` consistent
  (assign → `reviewed=true`, unassign → `reviewed=false`). **Auto-creates a subject by name**
  (find-or-create via `Slugify` + `GetSubjectBySlug`). Sentinels `ErrInvalidAction`/
  `ErrMissingBBox`/`ErrMissingMarker`/`ErrMissingSubject`; a missing photo/marker/subject maps
  to 404.

### Auto-clustering of faces (`internal/cluster` + `internal/clusterapi`)

Groups **still-unassigned faces** (without a subject) into clusters of the same person, so a whole cluster
can be named in one go — a key UX improvement over photo-sorter, where faces were named
one by one. Table `face_clusters` (migration `0010_face_clusters.sql`: `uid` PK prefix `fc`,
`centroid halfvec(512)`, `size`, `model`, timestamps) + cache column `faces.cluster_uid` (FK
`ON DELETE SET NULL`).

- **Algorithm** (`internal/cluster`, pure functions in `algo.go`/`suggest.go` are unit-tested):
  greedy **connected components** (union-find) over the HNSW nearest neighbors of each clusterable
  face within a **cosine-distance threshold** (`cluster.threshold`, default 0.4). An edge = two faces
  closer than the threshold; every component of size `≥ cluster.min_size` (default 2) becomes a cluster,
  smaller ones stay unclustered. For each cluster an L2-normalized **centroid** is computed
  (the mean of the embeddings) — used to pick a representative face and to suggest an existing subject.
- **Incremental and re-runnable** (`Recluster(ctx)`): only a face **without a
  subject** (`subject_uid IS NULL`) **and without a cluster** (`cluster_uid IS NULL`) is clusterable, so re-clustering
  never touches assigned or already-clustered faces — it groups only the fresh unassigned ones.
  Deterministic for a given set of faces.
- **`ListClusters(ctx)`** (backing `GET /faces/clusters`): for each cluster returns its size,
  a representative face (nearest to the centroid), a few examples and a **suggested existing subject** —
  the nearest **already-named** centroid (`FindSimilarFaceCandidates` over the centroid, aggregated by
  subject, `confidence = 1 − distance`). The suggestion is `null` when no named neighbor is close
  enough (`cluster.suggestion_max_distance`, default 0.5).
- **`AssignCluster(ctx, req)`** (backing `POST /faces/clusters/{id}/assign`, editor/admin): assigns
  **all** faces of the cluster to one subject (by `subject_uid`, otherwise find-or-create by
  `subject_name`) — for each face it creates a face marker via the **shared facematch state machine**
  (no duplication of marker-creation logic), then deletes the consumed cluster (the FK clears `cluster_uid`).
- **`RemoveFace(ctx, clusterUID, ref)`** (backing `POST /faces/clusters/{id}/remove-face`,
  editor/admin): detaches a stray face from the cluster **before** naming (so it doesn't taint the name),
  recomputes the centroid/size; if the cluster is orphaned, deletes it. Returns the refreshed view (or `deleted`).
- **HTTP layer** (`internal/clusterapi`): a `Service` interface (satisfied by `cluster.Service`),
  `NewAPI(Config{Service, RequireWrite})` + `RegisterRoutes` mounts `/faces/clusters`; 503 when
  the backend isn't wired, 400/404/409 per the sentinels. The admin trigger for re-clustering is
  `POST /api/v1/process/clusters` (see Process API). Tunables live in the `cluster.*` config.

### Face outlier detection (`internal/outliers` + `internal/outlierapi`)

For a given person it surfaces likely **misassigned faces** by sorting them by distance
from the centroid of that person's embeddings (mirroring photo-sorter). Everything behind interfaces (`FaceStore` =
a subset of `vectors.Store`, `PeopleStore` = a subset of `people.Store`), so it unit-tests
with fakes without a DB.

- **`Outliers(ctx, subjectUID)`** (backing `GET /subjects/{uid}/outliers`): verifies the subject
  exists (`people.ErrSubjectNotFound` → 404), loads all faces with `subject_uid =
  subjectUID` (`vectors.ListFacesBySubject`), computes the **centroid** (element-wise mean,
  L2-normalized) via the shared `vectors.Centroid`, scores each face by its **cosine
  distance** from the centroid (`vectors.CosineDistance`) and returns them sorted **descending**
  (most suspicious first; tie-break by `photo_uid`/`face_index` for determinism).
- Response = `{subject_uid, count, meaningful, faces:[{photo_uid, face_index, bbox, det_score,
  distance, marker_uid?, width, height, orientation}]}`. From `faces` the UI renders a thumbnail crop
  and **detaches a wrong face via the existing assign API** (`POST /photos/{uid}/faces/assign`
  with `unassign_person`) — this layer adds no mutation of its own.
- **Small sets:** 1–2 faces → `meaningful: false` (with so few, no face stands out),
  the faces are still returned sorted.
- **HTTP layer** (`internal/outlierapi`): a `Service` interface (satisfied by `outliers.Service`),
  `NewAPI(Config{Service, RequireWrite})` + `RegisterRoutes` mounts `GET /subjects/{uid}/
  outliers` behind `RequireWrite` (editor/admin); 503 without a backend, 404 for a missing subject.
- The shared vector math `vectors.Centroid`/`vectors.Normalize`/`vectors.CosineDistance`
  (in `internal/vectors/math.go`) is the single implementation — `internal/cluster` reuses it.

### Subjects / People API (`internal/peopleapi`)

A read/curation HTTP API over subjects (people/animals/other) — the backend for the **People UI**. Built on
interfaces (`SubjectStore` = a subset of `people.Store`, `PhotoStore` = `photos.Store.ListByUIDs`),
so it unit-tests with fakes without a DB.

- `GET /subjects` (RequireAuth) → `{subjects:[{...subject, marker_count}]}` (sorted by name).
- `POST /subjects` (RequireWrite) → 201 creates a subject from `{name, type, favorite, private, notes,
  cover_photo_uid?}`; body via `DisallowUnknownFields` + 1 MiB limit, empty name/unknown type → 400.
- `GET /subjects/{uid}` (RequireAuth) → the subject (404 if missing).
- `PATCH /subjects/{uid}` (RequireWrite) → edits `name/type/favorite/private/notes/cover_photo_uid`.
- `DELETE /subjects/{uid}` (RequireWrite) → 204 (markers detach via the FK).
- `GET /subjects/{uid}/photos` (RequireAuth) → a paginated gallery of the subject's photos
  `{photos, total, limit, offset, next_offset}` (newest-first, non-archived only, `limit` ≤ 500).
  Built on `people.Store.ListPhotoUIDsBySubject` (distinct photo uids from non-invalid markers) →
  page slice → `photos.Store.ListByUIDs` → reorder by uid order.
- **The routes are flat** (not `chi.Route`/Mount) so they coexist with `outlierapi`'s
  `GET /subjects/{uid}/outliers` on the same router. Mounted as the eighth `server.WithAPI`
  (`buildPeopleAPI` in `cmd/kukatko/people.go`).

### Albums, labels, favorites & ratings (`internal/organize`)

An organization schema over the catalog (migrations `0011_albums_labels_favorites.sql`,
`0016_user_ratings.sql` and `0025_user_ratings_eye.sql`, package `internal/organize` with a `Store`
over the shared pgx pool). In Kukátko both favorites and ratings (stars + a **personal mark**) are
**per-user** (favorites replace photo-sorter's global `photos.favorite`). The personal mark is
a neutral three-state icon — 👁 eye, 👍 thumbs up (stored as `pick`), 👎 thumbs down (stored as
`reject`) — instead of the earlier pick/reject culling.

- **`albums`** — PK `uid` (prefix `al`), `slug` UNIQUE (from `title`, Slugify + numeric suffix),
  `title`/`description`, `type` CHECK (`album`/`folder`/`moment`/`state`/`month`),
  `cover_photo_uid` (FK `photos` `ON DELETE SET NULL`), `private`, `created_by` (FK `users`
  `ON DELETE SET NULL`), timestamps — an album is **always displayed chronologically** (oldest first,
  an undated photo falls back to its upload time), so neither a sort choice nor a manual order exists
  (the `order_by` and `sort_order` columns were removed by migration `0022_chronological_albums.sql`).
  **`album_photos`** — membership: PK `(album_uid, photo_uid)`, both FKs `ON DELETE CASCADE`,
  `added_at`.
- **`labels`** — PK `uid` (prefix `lb`), `slug` UNIQUE (from `name`), `name`, `priority` (ordering
  in the UI), timestamps. **`photo_labels`** — attachment: PK `(photo_uid, label_uid)`, both FKs
  `ON DELETE CASCADE`, `source` CHECK (`manual`/`ai`/`import`), `uncertainty` (int %), `created_at`.
- **`user_favorites`** — per-user favorites: PK `(user_uid, photo_uid)`, both FKs
  `ON DELETE CASCADE`, `added_at`.
- **`user_ratings`** — per-user ratings: PK `(user_uid, photo_uid)`, both FKs `ON DELETE CASCADE`,
  `rating SMALLINT` CHECK 0–5 (default 0), `flag TEXT` CHECK (`none`/`pick`/`reject`/`eye`, default
  `none`), `updated_at`. A row exists **only for a non-default value** — when the rating drops to 0
  and the flag to `none`, the store deletes the row, so a photo without a row = rating 0 / flag `none` and the table
  stays sparse.

`organize.Store` API:

- **Albums** — `CreateAlbum`/`GetAlbumByUID`/`GetAlbumBySlug`/`UpdateAlbum` (re-slug from title)/
  `ListAlbums` (with photo counts, sorted by title)/`DeleteAlbum`; membership `AddPhoto`
  (idempotent position upsert)/`RemovePhoto` (idempotent)/`ReorderPhotos` (atomic rewrite of
  `sort_order` by order)/`SetCover` (set/clear cover)/`ListPhotoUIDs` (ordered by `sort_order`).
- **Labels** — `CreateLabel`/`GetLabelByUID`/`GetLabelBySlug`/`UpdateLabel` (re-slug)/
  `ListLabels` (with counts, sorted by priority DESC)/`DeleteLabel`; attachment `AttachLabel`
  (idempotent upsert of source/uncertainty)/`DetachLabel` (idempotent)/`ListPhotoUIDsByLabel`.
- **Favorites** — `AddFavorite`/`RemoveFavorite` (both idempotent), `IsFavorite`,
  `ListFavorites` (per-user, newest-first), `FavoritedAmong` (from a given set of photo uids returns
  the per-user subset of favorites as a set — annotating a whole page's `is_favorite` in one query).
- **Ratings** — `SetRating(user,photo,rating)` (validates 0–5 → `ErrInvalidRating`) and
  `SetFlag(user,photo,flag)` (validates `none`/`pick`/`reject`/`eye` → `ErrInvalidFlag`): an idempotent
  upsert of a single column in a transaction (the other is preserved), and when the row drops to 0/`none`, deletes it.
  `ClearRating(user,photo)` deletes both the rating and the flag in one idempotent DELETE (mirroring
  `RemoveFavorite`, a no-op on an unrated/missing photo — backing `DELETE /photos/{uid}/rating`).
  `GetRating(user,photo)` → `PhotoRating{Rating,Flag}` (a missing row = 0/`none`).
  `RatingsAmong(user,photoUIDs)` → a map `photo_uid → PhotoRating` for rated photos only (annotating
  a whole page in one query, mirroring `FavoritedAmong`; the caller defaults missing photos to 0/`none`).
- **Sentinels** — `ErrAlbumNotFound`/`ErrLabelNotFound`/`ErrPhotoNotFound`/`ErrUserNotFound`/
  `ErrSlugExhausted`/`ErrInvalidType`/`ErrInvalidSource`/`ErrInvalidRating`/`ErrInvalidFlag`. FK
  violations when writing to the join tables map to not-found sentinels according to the violated column
  (`photo_uid` → photo, otherwise user/album/label).

### Albums & labels API (`internal/organizeapi`)

An HTTP API over the catalog of albums and labels (`NewAPI(Config{Albums,Labels,RequireAuth,RequireWrite})` +
`RegisterRoutes`). `Albums`/`Labels` are interfaces (subsets of `organize.Store`), so the
handlers unit-test with fakes without a DB. Reads are for any authenticated user (`RequireAuth`), mutations
for editor/admin (`RequireWrite`). Browsing an album's/label's photos **has no endpoint of its own** —
it goes through the shared `GET /photos` scoped by `?album={uid}` / `?label={uid}` (see below), so the
frontend reuses the same virtualized grid.

- **Albums** — `GET /albums` (list with counts + cover), `POST /albums` (201, `title` required, type
  validation), `GET /albums/{uid}`, `PATCH /albums/{uid}` (title/description/cover/private;
  **the structural `type` is preserved**, not editable), `DELETE /albums/{uid}` (204);
  membership `POST /albums/{uid}/photos` `{photo_uids:[…]}` (adds),
  `DELETE /albums/{uid}/photos` `{photo_uids:[…]}` (removes) — both return the current
  **chronological** order `{photo_uids:[…]}`; manual reordering (`PATCH /albums/{uid}/order`)
  does not exist, an album always starts from the oldest photo.
- **Labels** — `GET /labels` (list with counts), `POST /labels` (201, `name` required),
  `GET /labels/{uid}`, `PATCH /labels/{uid}` (name/priority), `DELETE /labels/{uid}` (204);
  attachment `POST /labels/{uid}/photos` `{photo_uid,source?,uncertainty?}` (204),
  `DELETE /labels/{uid}/photos` `{photo_uid}` (204).
- **Scoped listing** — `GET /photos?album={uid}` and `GET /photos?label={uid}` (and likewise
  `GET /search`) add correlated `EXISTS` filters (`AlbumUID`/`LabelUID`) to `photos.ListParams`,
  so the scope honors all other list filters and pagination and the response has an identical shape
  to the regular library listing; an album scope additionally **forces chronological ordering** (oldest
  first, `sort`/`order` from the query are ignored), a label honors the chosen ordering.
- **Status codes** — 400 (validation/unknown field/invalid type/source), 404 (missing album/label/
  photo), 403 (viewer on a mutation), 401 (unauthenticated). Mounted as the ninth `server.WithAPI`
  (`buildOrganizeAPI` in `cmd/kukatko/organize.go`).

### Saved searches / smart albums (`internal/savedsearch` + `internal/savedsearchapi`)

Per-user **saved searches** ("smart albums"): a named, **owner-private** definition
of a filter/search (filters, sorting, query, mode as opaque JSONB `params`) that the user can
reopen later. It mirrors the per-user ownership model of `user_favorites` — only the owner sees and
may change their saved search. Table `saved_searches` in migration `0017_saved_searches.sql`
(`uid PK` prefix `ss`, `owner_uid` FK users `ON DELETE CASCADE`, `name`, `params JSONB NOT NULL`,
`created_at`/`updated_at`, index on `owner_uid`).

- **`internal/savedsearch`** — a `Store` over the shared pgx pool: `Create(ctx,ownerUID,name,params)`,
  `List(ctx,ownerUID)` (newest-first), `Get(ctx,uid)`, `Update(ctx,uid,name,params)`, `Delete(ctx,uid)`;
  `params` is held as `json.RawMessage` (empty → `{}`), sentinel `ErrNotFound`.
- **`internal/savedsearchapi`** — `NewAPI(Config{Store,RequireAuth})` + `RegisterRoutes` under `/api/v1`,
  all behind `RequireAuth` and **scoped to the authenticated user** from the auth context: `GET /saved-searches`
  (`{saved_searches:[{uid,name,params,created_at,updated_at}]}`), `POST /saved-searches`
  `{name,params}` → 201 (empty name → 400), `GET /saved-searches/{uid}` → 200,
  `PATCH /saved-searches/{uid}` `{name?,params?}` → 200, `DELETE /saved-searches/{uid}` → 204.
  **Ownership isolation** — another owner's saved search is always reported as **404** (never
  disclosed), the body decoded with `DisallowUnknownFields` + 1 MiB limit. Mounted via `server.WithAPI`
  (`buildSavedSearchAPI` in `cmd/kukatko/savedsearch.go`).

**Frontend (saved searches):** the client `web/src/services/savedSearches.ts` (`fetchSavedSearches`/
`createSavedSearch`/`updateSavedSearch`/`deleteSavedSearch`, types `SavedSearch`/`SavedSearchParams`/
`SavedSearchUpdate`). `params` is a **verbatim URL view-state object** (`Record<string,string>`) that the
app already serializes into the URL via `useUrlState` — it is saved and restored unchanged. The pure helper
`web/src/lib/savedSearchView.ts` (`isSearchParams` — the presence of `mode` distinguishes a search from a library
view; `savedSearchHref` — assembles `pathname?query` for `/` (library) or `/search` and minimally encodes the
params against defaults, so opening restores the view exactly). The **„Uložit pohled"** action on `LibraryPage`
and `SearchPage` (`SaveSearchModal` in `web/src/components/savedsearch/` — a modal for naming on
creation and for renaming), a dedicated page **`/saved`** (`SavedSearchesPage` — a list with open/
rename/optimistic delete + empty state) and a **dropdown in the `SearchPage` header**
(`SavedSearchesDropdown` — lazy fetch on open, items open the saved view, „Spravovat" points
to `/saved`). Saved searches are controlled **only from the `/search` page**, not from the navbar; route `/saved`
(any authenticated user) in `App.tsx`.

### Announcement for all users (`internal/announcement` + `internal/announcementapi`)

A single **instance-wide announcement**: a maintainer publishes a short message (e.g. "expect an outage
at 22:00–23:00 today") from the admin section and **every authenticated user** then sees it as a **banner
at the top of the app**. The single-row table `announcements` in migration `0039_announcement.sql`
(`id BOOLEAN PK DEFAULT true CHECK (id)` → publish is an **upsert**, `message TEXT NOT NULL`,
`level TEXT NOT NULL DEFAULT 'info' CHECK (info|warning)`, `author_uid VARCHAR(32)` FK users
`ON DELETE SET NULL`, `updated_at TIMESTAMPTZ`).

- **`internal/announcement`** — a `Store` over the shared pgx pool: `Get(ctx)` (sentinel `ErrNotFound` when
  nothing), `Set(ctx,message,level,authorUID,entry)` (upsert; empty message → `ErrEmptyMessage`, unknown level
  → `ErrInvalidLevel`, empty → `info`), `Clear(ctx,entry)`; **both publish and clear are audited**
  (`announcement.set`/`announcement.clear`) in the **same transaction** as the change (mirroring `internal/organize`).
- **`internal/announcementapi`** — `NewAPI(Config{Store,RequireAuth,RequireMaintainer})` + `RegisterRoutes`
  under `/api/v1`: `GET /announcement` behind `RequireAuth` (any authenticated user reads; when nothing → **200
  `{"message":""}`** instead of 404), `PUT /announcement` `{message,level}` and `DELETE /announcement` behind
  `RequireMaintainer` (publish/clear, `author_uid` = the actor). Body `DisallowUnknownFields` + 16 KiB limit,
  validation → 400. Mounted via `server.WithAPI` (`buildAnnouncementAPI` in `cmd/kukatko/announcement.go`).

**Frontend (announcement):** the client `web/src/services/announcement.ts` (`fetchAnnouncement`/`setAnnouncement`/
`clearAnnouncement`, types `Announcement`/`AnnouncementLevel`). The `AnnouncementBanner` banner in `Layout` right
before `<Outlet/>` — via `useAnnouncement` (fetch + polling ~60 s) and a dismissible `<Alert>` with a variant per
`level`. A **per-user dismiss keyed on `updated_at`** in localStorage (`lib/announcementDismissal.ts`):
dismissing hides the current message, but a newly published one (new `updated_at`) is **shown again**. Routes outside
`Layout` (`/photos/:uid`, `/slideshow`, `/review`, `/duplicates/compare`) have no banner (immersive views).
The compose control (maintainer) is the **Oznámení** card on `SystemStatusPage` (a textarea + level +
Publish/Clear). Strings `announcement.*` (cs default, en).

### Global search (`internal/globalsearchapi`)

A single **grouped cross-entity** endpoint **`GET /api/v1/search/global?q=`** (authenticated via
`RequireAuth`) for the cross-entity section of the search page: it finds matches across
**albums, labels, people and photos** in one query. Albums/labels/people are matched by name/description
**accent- and case-insensitive** (`immutable_unaccent` + ILIKE), photos via the **existing full-text** over
the `fts` tsvector — the existing `GET /search` (per-user photo full-text/semantic/hybrid) stays unchanged.

- **New store methods** — `organize.Store.SearchAlbums(ctx,q,limit)` (title/description) and
  `SearchLabels(ctx,q,limit)` (name), `people.Store.SearchSubjects(ctx,q,limit)` (name); each caps
  the result at `limit`, sorts stably and returns counts (albums/labels). LIKE metacharacters in `q` are escaped,
  so they match literally.
- **`internal/globalsearchapi`** — small interfaces `Organizer`/`PeopleSearcher`/`PhotoSearcher` (satisfied
  by `organize.Store`/`people.Store`/`photos.Store`) → unit-testable with fakes.
  `NewAPI(Config{Organizer,People,Photos,Limit,RequireAuth})` + `RegisterRoutes` mounts
  `GET /search/global`; it handles each group separately, caps at `Limit` (default 8). Response
  `{query, albums:[{uid,title,cover,photo_count}], labels:[{uid,name,photo_count}],
  people:[{uid,name,cover}], photos:[…usual photo shape…]}` (groups are always non-nil arrays). An empty/
  whitespace `q` → **400**, a store error → **500**. Mounted via `server.WithAPI`
  (`buildGlobalSearchAPI` in `cmd/kukatko/globalsearch.go`, sharing the organize/people/photos store).

**Frontend (global search):** the client `web/src/services/search.ts` (`globalSearch(q,signal)` →
`GlobalSearchResult` + helpers `hasEntityMatches`/`isEmptyResult`) and the hook
[`useGlobalSearch(query)`](web/src/hooks/useGlobalSearch.ts) (250 ms debounce, idle/loading/ready/error,
cancels in-flight). On the search page
[`GlobalSearchSections`](web/src/components/search/GlobalSearchSections.tsx) renders, above the photo
grid, compact chips of matching albums/people/labels (only when they exist), so a text query surfaces
non-photo entities too.

### Bulk metadata editing (`internal/bulk` + `internal/bulkapi`)

A single endpoint **`POST /api/v1/photos/bulk`** (editor/admin via `RequireWrite`) applies a set of
operations to **many photos at once in a single transaction** together with a durable audit-log record, so
the whole batch commits or rolls back atomically. Body: `{"photo_uids":[…],"operations":{…}}`.
Supported operations (each optional, an omitted field = no change):

- `add_to_albums`/`remove_from_albums` `[al…]`, `add_labels`/`remove_labels` `[lb…]` (idempotent);
- `set_caption`/`clear_caption` (→ `title`), `set_description`/`clear_description`;
- `set_location {lat,lng}` (range validation) / `clear_location`;
- `archive` / `unarchive` (mutually exclusive);
- `set_favorite` (bool) — **per-user** favorite for the caller;
- `set_rating` (0–5) / `set_flag` (`none`/`pick`/`reject`/`eye`) — **per-user** rating for the caller
  (an invalid value → **400**; the `user_ratings` row is cleared when it drops to rating 0 + flag `none`).

Set/clear pairs are separate keys (not presence/null), so the payload is unambiguous and a conflict
(`set_*` + `clear_*`, `archive` + `unarchive`) is **400**. An unknown operation key → **400**
(`DisallowUnknownFields`). A batch over the `bulk.max_batch_size` limit (default 1000) → **413**.

- **Result semantics** — response `{results:[{photo_uid,status,error?}],counts:{total,updated,
  skipped,errored}}` (HTTP 200 even on partial errors). Per-photo statuses: `updated` (applied),
  `skipped` (duplicate uid in the batch), `error` (the photo doesn't exist). **A missing photo does not abort
  the valid ones** — it is recorded as an error, the rest are applied and committed; only a genuine DB error
  rolls back the whole batch (500). Albums/labels in add operations are verified up front (missing → 400).
- **Audit log** (`internal/audit`) — the bulk write inserts an audit row into the **same transaction** as the mutation
  via `audit.Write(ctx, tx, Entry)`. For a full description of the durable audit trail and the admin API see the section
  [Durable audit log](#durable-audit-log-internalaudit--internalauditapi) below.
- **Layers** — `bulk.Service` (`NewService(pool, maxBatch)`, `Apply(ctx, actorUID, photoUIDs, ops)`)
  holds the transactional logic and the SQL itself (its own tx for atomicity), `bulkapi` does the HTTP +
  payload validation. Mounted as another `server.WithAPI` (`buildBulkAPI` in `cmd/kukatko/bulk.go`).

### Durable audit log (`internal/audit` + `internal/auditapi`)

An append-only audit trail written **in the same transaction** as the mutation it records — it fixes
a photo-sorter gap where the audit was written only after the commit (on a crash between committing the mutation and writing
the audit, the record was lost). Table `audit_log` (migration `0012_audit_log.sql`, extended in
`0014_audit_request.sql`): `id BIGSERIAL`, `actor_uid` (FK users `ON DELETE SET NULL` — the trail
survives account deletion), `action`, `target_type`, `target_uid`, `details JSONB`, `ip`, `user_agent`,
`created_at`; indexes on `(created_at)`, `(target_type, target_uid)`, `(action)`, `(actor_uid)`.
(The `actor`/`target`/`details` columns correspond to the spec terms `user`/`entity`/`metadata` — the originally
shipped names are kept, a rename applied by migration would be destructive.)

- **Mechanism** — `Write(ctx, exec, Entry)` accepts an `Execer` (a pool **or** a `pgx.Tx`), so the audit
  insert runs on the same transaction as the mutation and commits/rolls back with it (no orphaned or missing
  record). `Store.Record` writes on its own connection; `Store.List(ctx, Filter)` + `Count(ctx, Filter)`
  read with filters (actor/entity/action/date, pagination, newest-first, limit cap 500/default 100).
- **Handler convention** — `audit.FromRequest(r, actorUID)` gathers the actor (from the auth context), the IP
  (`X-Forwarded-For` → `X-Real-IP` → `RemoteAddr`) and the User-Agent into `Meta`; `meta.Entry(action,
  entityType, entityUID, details)` builds an `Entry` from it. Action constants: `ActionPhotosBulk`,
  `ActionPhoto{Update,Archive,Unarchive}`, `ActionAlbum/Label{Create,Update,Delete}`,
  `ActionFaceAssign`, `ActionUser{Create,Update,Disable,Password}`.
- **Wired-up in-tx mutations** — bulk editing (`internal/bulk`) and photo PATCH/archive/unarchive via
  the audited variants `photos.Store.{UpdateMetadata,Archive,Unarchive}Audited` (a shared `rowQuerier`
  runs the mutation on the tx, `mutateAudited` adds the audit and commits). The other mutating domains (albums/labels,
  people, user management) adopt the same pattern in follow-up iterations.
- **Admin API** — `GET /api/v1/audit` (admin-only via `RequireAdmin`, `internal/auditapi`) returns
  `{entries,total,limit,offset,next_offset}` newest-first with filters `?user=`/`?entity_type=`/
  `?entity_uid=`/`?action=`/`?since=`/`?until=` (RFC3339) and `?limit=`(≤500)/`?offset=`; an invalid
  time/number → 400. Read-only — writes go exclusively through the mutation transactions. Mounted via
  `buildAuditAPI` in `cmd/kukatko/audit.go`.

### Maps: tiles, reverse geocode & GeoJSON (`internal/mapy` + `internal/mapsapi`)

A backend proxy to [mapy.com](https://mapy.com), so the **API key never leaves the server** (it is sent
only in the `X-Mapy-Api-Key` header), plus a GeoJSON feed of geotagged photos for the map view.
All endpoints require authentication (`RequireAuth`), mounted via `server.WithAPI` (`buildMapsAPI`
in `cmd/kukatko/maps.go`).

- **`GET /api/v1/map/tiles/{mapset}/{z}/{x}/{y}`** — a tile proxy: the backend fills in the key and **streams**
  the bytes back with a long `Cache-Control` (immutable). `mapset` is restricted to the allowlist
  `basic|outdoor|aerial|winter` (anything else → 400, before even calling mapy.com); retina `@2x` (a suffix on `{y}`
  or `?retina=true`) applies only to `basic`/`outdoor`. Invalid `z`/`x`/`y` → 400.
  Successful tiles are **cached server-side** (bounded LRU + TTL, `maps.tile_cache_bytes`/
  `maps.tile_cache_ttl`, default 64 MiB / 24 h) — the free tier charges **1 credit per tile**, so
  panning across an already-seen area again costs nothing; hit/miss is reported by `X-Tile-Cache`. **An error is never
  cached** (otherwise an outage would freeze in the map for the whole TTL).
- **`GET /api/v1/map/rgeocode?lat=&lng=`** — reverse geocode → a simplified
  `{name, location, regional_structure}`. **It is cached** (key = rounded coordinate) and uncached
  lookups are **rate-limited** because of credits (a geocode = 4 credits); over the limit → 429, no match → 404.
- **`GET /api/v1/map/photos`** — a **GeoJSON FeatureCollection** of geotagged photos (only those with lat/lng;
  coordinates RFC 7946 `[lng, lat]`). It honors the standard list filters (`taken_after`/`taken_before`, `album`,
  `label`, `archived`); each feature carries `uid`, `title`, `taken_at`, `media_type` and a relative
  `thumb` path for markers/clustering.
- **mapy.com errors** (401/403/404/429/5xx) map to sensible statuses and the **key never leaks**
  into responses or errors. **A rejected key (401/403) gets its own status `424`** — a raw 403 is not
  sent out, because the caller's request is fine, it is *our* key that is faulty; an upstream error → 502,
  unavailable → 503, 404/429 propagated. Every non-200 is **logged as WARN** with the status and the mapset.
- **When the map goes gray** (typically an expired / over-quota key) the app **says so**, instead of showing
  an unexplained gray grid: the map view displays a closeable warning („Mapové podklady se
  nepodařilo načíst — mapový klíč byl odmítnut") and **the photos keep being drawn** over the empty base layer;
  the admin **Systém** (`/system`) reports the map backend as *degraded* (`maps.state = key_rejected`),
  so it is visible even without opening the map. The fix is manual — a new key in the mapy.com console.
- **Layers** — `mapy.Client` (an HTTP client to mapy.com behind an interface, fakeable;
  sentinels `ErrUnauthorized`/`ErrNotFound`/`ErrRateLimited`/`ErrUpstream`/`ErrUnavailable`/
  `ErrInvalidMapset`) + `mapy.Health` (the last observed upstream state for the system
  status), `mapsapi` does the HTTP handlers + cache (both tiles and geocode) + rate limit
  + filter parsing. The base URL is configurable (`maps.base_url`, default
  `https://api.mapy.com`) mainly for the test double (an httptest fake
  mapy.com).

### Places: a photo's reverse-geocoded location (`internal/places` + `internal/placesjob`)

Geotagged photos are reverse-geocoded in the background into a hierarchy of **country / region (kraj) / city
(obec) / place_name** and cached on the photo, so the library can be browsed and filtered by location without
repeatedly calling the rate-limited geocoder (the browse API + UI are separate tasks). The geocoding
runs as a **queued `places` job** (not inline) via the existing `mapy.ReverseGeocode`.

- **Schema** — a **side table `photo_places`** (migration `0018_photo_places.sql`) keyed by
  `photo_uid` (FK `ON DELETE CASCADE`), not columns on the wide `photos`: place data is sparse (only geotagged
  photos have a row) and it is a derived, regenerable cache filled asynchronously. Columns
  `country`/`region`/`city`/`place_name` (`TEXT NOT NULL DEFAULT ''`), `lat`/`lng`
  (`DOUBLE PRECISION` — the coordinates the geocode was computed from; NULL for a photo without GPS) and
  `geocoded_at`; indexes on `country` and `city` for grouping/filtering. `places.Store` =
  `GetPlace`/`SavePlace` (upsert)/`ListPhotosMissingPlaces` (geotagged non-archived photos without a place).
- **Job handler** (`placesjob.Service.Handle`, the `places` job) — from `{photo_uid}` it loads the photo;
  **idempotent** (a photo with a place for the **current** coordinates is skipped, a coordinate change →
  re-geocode), a photo **without GPS** gets an empty "processed" marker (never retried). Otherwise
  it calls the geocoder, `parsePlace` parses `regional_structure` (types `regional.country`/`region`/
  `municipality`) into country/region/city + place_name = the most specific label, and stores it.
- **Resilience & credits** — mapy.com unavailable / rate-limited → `worker.RetryAfter` (deferral without
  burning an attempt, mirroring the embed/face job); `ErrNotFound` → a processed marker (does not retry forever).
  A dedicated **token-bucket limiter** (`maps.geocode_rate_per_sec` default 5 / `geocode_burst` 10)
  protects the monthly mapy.com credit budget — empty → a short deferral (processing slowly is OK).
- **Backfill & wiring** — `placesjob.BackfillPlaces` enqueues a `places` job for every geotagged photo
  without a place; admin `POST /api/v1/process/places` → `{enqueued}`. The handler registers on the worker and the
  endpoint is available **only when `maps.mapy_api_key` is set** (otherwise 503), the client is built
  server-side (`buildPlacesServiceOrNil` in `cmd/kukatko/places.go`). Everything behind interfaces → fake in tests.

### Places Browse API (`internal/placesapi`)

Browsing the cached place hierarchy + scoping the photo listing to a location (backend, always
mounted).

- **`GET /api/v1/places`** (authenticated) — a hierarchy with counts aggregated over **non-archived**
  photos with place data: `{places:[{country, count, cities:[{city, count}]}]}`. A country's `count` includes
  photos with no known city too (it can exceed the sum of the cities), `cities` is always an array (empty when no
  city). Optional `?country=` drills only into the cities of one country. Sorted by **count desc, then name**
  (for both countries and cities). Photos without place data (no `photo_places` row or an empty `country` — e.g.
  a no-GPS "processed" marker) are excluded. The aggregation is computed by `photos.Store.AggregatePlaces(country)`
  (a single `GROUP BY country, city` JOIN on `photo_places`, the hierarchy assembled in Go). Mounted via
  `server.WithAPI` (`buildPlacesAPI` in `cmd/kukatko/places.go`, aggregation via the photos store over the
  `photo_places` cache).
- **Scoped listing** — `photos.ListParams` has `Country`/`City` (exact match) added to `buildWhere`
  (a single correlated `EXISTS` over `photo_places`, so `Count` matches), `photoapi.parseListParams`
  reads `?country=`/`?city=`. The shared **`GET /photos?country=&city=`** returns the photos of a given location and
  honors all the other filters/ordering/pagination — exactly the `?album=`/`?label=` scoping pattern (archived
  photos outside the default listing).
- **Frontend** — `PlacesPage` (`/places`, navbar **Místa**, for any authenticated user) over
  `places.ts` `fetchPlaces(country?)`: with one fetch it pulls the country→city hierarchy (`PlaceCountry[]`)
  and clicks through the levels country → city → a **photo grid** scoped to `{country,city}` via `useScopedPhotos`
  (`enabled` only after a city is picked) + the shared `FilterBar`/`PhotoGrid`. Both the drill and the filters live in the URL
  (`/places?country=&city=` via `useUrlState`), so Back walks the levels; loading/empty/error states.

### People UI (frontend)

A complete human experience over the APIs above (react-bootstrap Superhero, i18n cs/en,
responsive/touch). Routes in the `Layout` navbar under the **Lidé** link (`/people`):

- **`/people`** (`PeoplePage`) — a grid of people (`SubjectTile`: cover/name/photo count);
  editors get a link to cluster review.
- **`/people/:uid`** (`SubjectPage`) — a person's page: header (name/type, edit via
  `SubjectEditModal`), a paginated gallery (`useSubjectPhotos` + `SubjectPhotoTile` with a "set as
  cover" action), and an **outliers** section (`Outliers` — a ranking of suspicious faces, one-tap unassign;
  editor/admin only).
- **`/people/clusters`** (`ClustersPage`, editor/admin) — **the primary fast path**: a queue
  of unnamed face clusters, each a `ClusterCard` (representative + samples + removal of a stray
  face + **one-shot naming of the whole cluster** onto a new/existing subject); optimistic
  removal after naming.
- **`/photos/:uid`** (`PhotoDetailPage`) — **a rich photo detail**: a large preview reflecting
  the saved non-destructive edit, **prev/next** navigation respecting the source listing's order
  (`usePhotoNeighbors`), deep-link + **Back** to the source view (`lib/detailView`), download of
  the original and the edited version. **Clicking the preview opens a fullscreen lightbox** (`components/photo/`
  `Lightbox`): the photo full-screen (contain) on a dark background with the saved edit, **large
  left/right arrows** paging through the same order/scope as the detail (`usePhotoNeighbors`, stopping at
  the ends), ←/→ and Esc keys, swipe on mobile, close with the X or by clicking the background, prefetch of
  neighbors; the lightbox pages internally and **restores the URL on close** to the current photo (Back always
  works). A video/live photo has its own native fullscreen (`VideoPlayer`/`LivePhoto`) and
  does not open the image lightbox. The right panel has tabs (`components/photo/`): **Informace**
  (`MetadataPanel` view/edit title/description/notes/taken_at + EXIF + an **interactive location
  picker**: a single lenient coordinate field (`lib/coordinates` parser: decimal degrees / DMS /
  degrees-decimal-minutes, N/S/E/W or signs, comma/space separator) over a Leaflet map
  (`LeafletMap` **picker mode** over the mapy.com tile proxy) with **dragging/clicking** the marker —
  two-way sync (parsing the text moves the marker, moving the marker rewrites the text to canonical decimal
  degrees), invalid text shows an inline error and **blocks saving**, a button to clear the location; without
  coordinates the map starts over the Czech Republic; `OrganizePanel`
  inline add/remove of albums and labels — adding via the `AddAutocomplete` type-to-filter combobox,
  a client-side case/accent-insensitive filter, keyboard + a „nic neodpovídá" state, without creating
  new albums/labels), **Poloha** (`PhotoLocation` Leaflet mini-map over the mapy.com
  proxy + on-demand reverse-geocode + clear), **Úpravy** (editor/admin: `EditPanel`
  rotation/brightness/contrast/crop with a live CSS preview → `PUT /photos/{uid}/edit`). An interactive
  **`FaceOverlay`** (face boxes from a normalized bbox, click → identity suggestions + a free-text name),
  in the header **stars + pick/reject** (`RatingStars`/`FlagControl` over `useRating`) and
  `FavoriteButton` (per-user) plus **rating hotkeys** `0`–`5`/`p`/`r` (except when typing into an input;
  disabled while the lightbox is open), a `SimilarPhotos` strip. A viewer sees it read-only. Vitest covers
  metadata edit, prev/next + Back, add/remove of albums/labels, favorite toggle, writing an edit + preview,
  read-only viewer, **lightbox** (open by click, close via X/background/Esc, prev/next incl.
  the ends, video does not open) (mock API/Leaflet).
- Shared: `FaceThumb` (a face crop from the thumbnail via `faceCropStyle`), the `services/people.ts` client,
  the `lib/faceGeometry.ts` geometry. Vitest covers cluster naming, positioning/assignment in the overlay
  and outlier unassign (mock API).

### Image embedding & similar photos (`internal/embedjob`)

`embedjob.Service` plugs CLIP embedding into the job queue and builds embedding queries on top of it.
Everything is behind interfaces (`PhotoStore`/`VectorStore`/`Previewer`/`Enqueuer` + `embedding.Client`), so
it can be unit-tested with fakes, without network/DB/disk.

- **The `image_embed` handler** (`Handle` = `worker.HandlerFunc`, registered in `serve`): from the
  `{"photo_uid": …}` payload it loads the photo, (idempotently) renders the `fit_720` preview, sends it to the sidecar
  (`Client.ImageEmbedding`) and stores the 768-dim `halfvec` via `vectors.SaveEmbedding` (+ `model`/
  `pretrained`). **Idempotent** — a photo that already has an embedding is skipped without calling the sidecar.
  **The box is offline** (`embedding.IsUnavailable`) → returns `worker.RetryAfter(5 min, …)`, so the job
  is merely deferred (`Defer`) without burning an attempt; any other error follows the normal retry/dead-letter path.
- **`BackfillEmbeddings(ctx)`** — for every photo without an embedding (`ListPhotosMissingEmbedding`)
  it enqueues `image_embed` (dedup = no-op), returns the count. Safe to run repeatedly.
- **`Duplicates(ctx, photoUID)`** — embedding-based near-duplicate detection: finds photos within
  the configured cosine distance (`duplicate.embedding_max_dist`) of the photo's embedding, excluding the photo
  itself; `<= 0` disables it, a photo without an embedding → nil. Complements the pHash check that upload performs
  already at ingest time (when the embedding does not yet exist).

### Face detection (`internal/facejob`)

`facejob.Service` plugs face detection into the job queue. Everything is behind interfaces
(`PhotoStore`/`VectorStore`/`ImageSource`/`Enqueuer` + `embedding.Client`), so it can be unit-tested
with fakes, without network/DB/disk.

- **The `face_detect` handler** (`Handle` = `worker.HandlerFunc`, registered in `serve`): from the
  `{"photo_uid": …}` payload it loads the photo, opens a **decodable full-resolution original** (via
  `StorageSource` = `storage` + `imgconvert.EnsureDecodable`, so HEIC/RAW/video are converted) and
  sends it to the sidecar (`Client.FaceEmbeddings` → 512-dim ArcFace embeddings + pixel bboxes +
  det_score). The original (not the preview) because the sidecar (InsightFace) rotates by EXIF itself and returns
  the bbox in display pixels — normalization by the stored dimensions only holds at the same scale. Each
  face is stored via `vectors.RecordFaceDetection` (512-dim `halfvec`, a **normalized bbox**,
  det_score, `face_index`, model, cached `photo_width`/`photo_height`/`orientation`).
- **BBox conversion** (`normalizeBBox`): pixel `[x1,y1,x2,y2]` → normalized `[x,y,w,h]` (0..1)
  by the photo's dimensions and **EXIF orientation** — for orientations 5–8 (a 90°/270° rotation) the width
  and height of the display space are swapped. Mirrors photo-sorter logic, tested across all 8 orientations.
- **det_score filter** (`faces.min_det_score`, default `0.5`): faces with lower confidence are
  dropped; the survivors are re-indexed contiguously (no gaps in `face_index`). `<= 0` disables the filter.
- **Idempotence**: a photo that already has a `face_detections` row is skipped without calling the sidecar;
  a detection with **zero faces** is still recorded, so it is not reprocessed. **The box is offline**
  (`embedding.IsUnavailable`) → `worker.RetryAfter(5 min, …)` (deferral without burning an attempt).
- **`BackfillFaces(ctx)`** — enqueues `face_detect` for every unprocessed photo
  (`ListPhotosMissingFaces`, dedup = no-op), returns the count. Upload enqueues `face_detect` right
  at ingest; backfill is the recovery path for photos uploaded while the box was offline.

### Import tracking (`internal/importer`)

A record of import/migration runs and their high-watermarks for **incremental, idempotent** import
(migration `0013_import_runs.sql`, table `import_runs`; see ARCHITECTURE.md §5.2/§9/§10). Every
import run from PhotoPrism (`photoprism`) or migration from photo-sorter (`photosorter`) stores the time
window it processed: `high_watermark` (`TIMESTAMPTZ`) = the largest source timestamp (e.g. the max
PhotoPrism `UpdatedAt`). The next run of the same source picks up from the watermark of the **last successful** run,
so a crashed/failed run does not advance the cursor and the work simply repeats.

- **Schema** — `import_runs`: `id BIGSERIAL PK`, `source TEXT CHECK(photoprism|photosorter)`,
  `started_at`/`finished_at TIMESTAMPTZ`, `status TEXT CHECK(running|done|failed)`,
  `high_watermark TIMESTAMPTZ` (NULL until the run finishes / processed nothing), `counts JSONB`
  (`{imported,updated,skipped,failed}`), `last_error TEXT`. A partial index
  `(source, finished_at DESC) WHERE status='done' AND high_watermark IS NOT NULL` makes the resume
  query cheaper, hitting exactly the rows it reads.
- **`importer.Store`** (`NewStore(pool)`) — the run lifecycle: `Start(ctx, source)` opens a row in the
  `running` state (unknown source → `ErrInvalidSource`), `UpdateCounts(ctx, id, counts)` continuously
  overwrites the tally, `Complete(ctx, id, watermark, counts)` closes the run as `done` with a stamped
  `finished_at` + watermark, `Fail(ctx, id, lastErr, counts)` as `failed` **without** a watermark.
  `Complete`/`Fail` match only a running run (no double close → `ErrRunNotFound`). `Get(ctx, id)`
  reads a single run.
- **`LatestWatermark(ctx, source)`** → `(time.Time, found bool, err)` — the watermark of the **last
  successful** run of the source (ordered by `finished_at`), which the next incremental run should pick up from;
  `found=false` on the first (full) run. **Ignores** running and failed runs, and done runs without
  a watermark. Each source has its own independent cursor.

### PhotoPrism API client (`internal/photoprism`)

A read-only HTTP client to a running PhotoPrism instance — the basis of the incremental import (see
ARCHITECTURE.md §9). Everything is behind the `Client` interface (fakeable in tests), so neither the importer nor
the tests need a real PhotoPrism, network, or token.

- **Authentication** — a long-lived **app password / access token** (PP:
  `photoprism auth add -n Kukatko -s "photos albums"`) is sent in the `Authorization: Bearer` header
  on **every** request; it is never logged per-request (login is the most heavily rate-limited).
  Configured via `import.photoprism.{base_url,token}` (the token via
  `KUKATKO_IMPORT_PHOTOPRISM_TOKEN`, do not commit).
- **`ListPhotos(ctx, PhotoListParams)`** → `GET /api/v1/photos?count=1000&offset=N&merged=true&
  order=updated&q=updated:"<RFC3339>"`. `UpdatedSince` (non-zero) adds the `updated:` filter for the
  **incremental** pull; `count` is clamped to `MaxCount` (1000), `offset` drives pagination
  (the caller pages until a page returns a full `count`). It parses the UID, TakenAt, Lat/Lng/Altitude,
  Title/Description, Type, Width/Height, OriginalName, Camera/Lens/EXIF, and `Files[]`
  (UID, **Hash = SHA1**, Primary, Mime, `Markers[]`) fields. `Photo.PrimaryFile()` returns the primary file.
  `PhotoListParams` can additionally **scope** the listing for membership mapping: `AlbumUID` adds the
  `s=<albumUID>` filter (an album's photos), `Query` sets `q=` outright (overrides the watermark — used for
  `label:"<slug>"`).
- **`ListAlbums`/`ListLabels`/`ListSubjects(ctx, ListParams)`** → `GET /api/v1/{albums,labels,subjects}`
  (count/offset); markers go through `Files[].Markers[]`.
- **`DownloadOriginal(ctx, fileHash)`** → `GET /api/v1/dl/{hash}?t=<download_token>` **streams** the
  original (never fully in RAM; the caller owns the body and closes it). The download token is obtained from
  create-session (`POST /api/v1/session`, reads `config.downloadToken`), **can rotate** — the client
  keeps picking it up from the `X-Download-Token` header and, on 401/403, refreshes the session once and retries.
- **Robustness** — **429** is retried with exponential backoff (honors `Retry-After`); JSON endpoints
  require `Content-Type: application/json`; sensible timeouts (JSON calls use `Timeout`, download only
  the caller's ctx); typed errors `ErrInvalidURL`/`ErrUnauthorized`/`ErrNotFound`/`ErrRateLimited`/
  `ErrUpstream`/`ErrUnavailable`/`ErrBadResponse` — they never contain the token or the response body.

### PhotoPrism import (`internal/ppimport` + `internal/importapi`)

A read-only, **incremental and idempotent** import from PhotoPrism (ARCHITECTURE.md §9). It downloads
new/changed photos, deduplicates, maps external IDs, and after import lets the regular jobs compute
embeddings/faces. It keeps all collaborators behind interfaces → unit-testable with fakes, without
PhotoPrism, network, DB, or disk.

- **Invocation** — either the CLI `kukatko import photoprism` (synchronously, for ops/cron without a running
  server), or the admin endpoint `POST /api/v1/import/photoprism`, which enqueues a **`pp_import` job**
  (runs in the background worker). The `pp_import` payload carries a fixed sentinel `photo_uid`, so queue dedup
  lets **only one import** run at a time (a second trigger → 409). Both the handler and the CLI call the same
  `Service.Import`.
- **The run** (`Service.Import`) — opens an `import_runs` run, resumes from the last successful watermark, and:
  1. **Photos** — pages `ListPhotos(UpdatedSince=watermark)`; per photo, dedup by `photoprism_uid`
     (already imported → update changed metadata, otherwise skip), otherwise it **selects media by the PP `Type`**
     (`selectMedia`): **video/animated** → downloads the **video file** itself (`Photo.VideoFile()`,
     `media_type=video`; a video without a detectable stream gracefully degrades to an image), **live** →
     the **still** as the primary original + the **motion clip** as a `sidecar` photo_file
     (`Photo.StillFile()`+`VideoFile()`, `media_type=live`), otherwise **image** (the primary file);
     it downloads the selected original, computes **SHA256**, dedups by `file_hash` (identical content already in the catalog
     → backfill `photoprism_uid`/`photoprism_file_hash` via `photos.SetPhotoprismRef`, no new
     photo), stores the original, **for video/live it probes video metadata** (`Prober.Probe` →
     `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps`; for video from the original, for live from the motion
     clip; best-effort), creates a `photos` row with **PP metadata** (title/desc/taken_at/GPS/camera/EXIF)
     + `media_type` + video metadata + external IDs + the **EXIF orientation from the file** (PP does not expose it),
     **for live** it also stores the motion clip as `RoleSidecar`, renders thumbnails (**for video a poster frame**
     via ffmpeg), and **enqueues `image_embed`** (on the poster) **+ `face_detect`** jobs. Counts are
     **checkpointed after every page**.
  2. **People** — from `Files[].Markers[]`, but **only from the photo DETAIL**: the photo listing always returns markers
     as an empty array, so whoever reads them from the listing brings in nobody (this is exactly the bug the import
     used to have). A scoped run takes them from the detail it downloads anyway — for **every** photo in
     scope, so a **re-run also fills in people for previously imported photos**. A full run requests the detail
     with one extra request **per newly imported** photo. Each **named valid face
     marker** → find-or-create subject (by `Slugify`) + a Kukátko marker assigned to the subject, which
     **keeps the PhotoPrism UID** (→ idempotence, no duplicate people on re-run). Faces are
     paired to markers via IoU (`facematch`) — so the assignment "survives" even the fact that embeddings
     and faces are only computed by Kukátko itself.
  3. **Albums & labels** — find-or-create by name (a map from `ListAlbums`/`ListLabels`), membership via
     a scoped `ListPhotos` (`AlbumUID` / `label:"<slug>"`) → `AddPhoto` / `AttachLabel` (both
     idempotent), only for already imported photos.
  4. **Closing** — writes counts + the new watermark and marks the run `done`.
- **Scoped run** (`Service.ImportScoped`) — the library can be migrated **in slices**:
  `kukatko import photoprism --album <uid> --label <slug> --person "<name>" --year <YYYY>`. The flags
  **combine** and narrow the run (the album goes into `s=`, the rest as AND-ed `q=` terms). The slice is pulled in
  **in full, regardless of photo age** (the watermark is ignored) and only the structure of **those**
  photos is mapped — the named album and the named label; people are seeded by the face markers of the imported photos.
  A scoped run is **partial and never advances the watermark**, so a later full import still sees
  all photos (otherwise a silent cursor advance would lose everything older). No flags = a full run.
- **Robustness** — a per-photo error is recorded in `counts.failed` and **does not abort the run** (only
  an infrastructure error — cannot list / DB — makes the run `fail`); 429 backoff is handled by the client; **the watermark
  never advances past the oldest failure** (a failed photo is picked up again next time); the whole import is
  safe to repeat. Configured via `import.photoprism.{base_url,token,page_size}`; without
  `base_url`, neither the import job nor the endpoint is registered (the CLI returns an error).

### photo-sorter migration (`internal/photosorter` + `internal/psimport`)

A read-only, **incremental and idempotent** direct migration from the **photo-sorter** PostgreSQL DB
(ARCHITECTURE.md §10). Because both photo-sorter and Kukátko use the **same models and dimensions**
(CLIP 768 + InsightFace 512) and the **same SHA256** file hashes, **embeddings and faces are transferred
1:1** without recomputation and photos are deduplicated directly. It keeps all collaborators behind interfaces →
unit-testable with fakes, without photo-sorter, network, DB, or disk; the **integration tests** run against
a **seeded fake photo-sorter schema** (`ps_fixture`) alongside the Kukátko tables in a single test DB.

- **`internal/photosorter`** — a read-only client with its own pgx pool (separate from Kukátko),
  pgvector types registered on every connection, an optional `Schema` scopes each query via
  `search_path` (that's how the integration test reads the fake schema). It reads **only** the tables the migration needs
  (`photos`, `embeddings`, `faces`, `faces_processed`, `subjects`, `markers`, `albums`/
  `album_photos`, `labels`/`photo_labels`, `photo_phashes`, `photo_edits`) — it **never reads
  the photo book or share links**.
- **Invocation** — either the CLI `kukatko migrate photosorter` (synchronously, for ops/cron), or the admin
  endpoint `POST /api/v1/import/photosorter`, which enqueues a **`ps_migrate` job** (runs in the background
  worker). The `ps_migrate` payload carries a fixed sentinel, so queue dedup lets **only one migration**
  run at a time (a second trigger → 409). Both the handler and the CLI call the same `Service.Migrate`.
- **The run** (`Service.Migrate`) — opens an `import_runs` run (`source=photosorter`), resumes from the last
  successful watermark, and:
  1. **Catalogs** — find-or-create a Kukátko **subject** (by the slug from the name), **album** (by title),
     and **label** (by name) for each photo-sorter one; a ps-uid → kk-uid map is built for the satellites.
  2. **Photos** — pages `ListPhotos(UpdatedSince=watermark)` (ordered by `updated_at`); per photo
     match by `photosorter_uid` (already migrated → skip), otherwise by **`file_hash`** (already in the catalog,
     e.g. from the PhotoPrism import → backfill `photosorter_uid` via `photos.SetPhotosorterRef`, no
     copying), otherwise it **copies the original** from `file_path` into Kukátko storage (SHA256, thumbnails),
     creates a `photos` row with photo-sorter metadata + `photosorter_uid`.
  3. **Satellites** — the **embedding** (768) and **faces** (512 + bbox + det_score + cache) are inserted
     **1:1** (preserving `model`/`pretrained`, remapping the subject, preserving `marker_uid`); a photo that
     photo-sorter **did not embed/detect** gets a Kukátko `image_embed`/`face_detect` job.
     **markers** are migrated under their original UID (idempotence), **album/label membership**, **phash**,
     and **edits** are transferred (best-effort, idempotently).
  4. **Closing** — writes counts + the new watermark and marks the run `done`.
- **Robustness** — a per-photo error is recorded in `counts.failed` and **does not abort the run** (only
  an infrastructure error makes the run `fail`); **the watermark never advances past the oldest failure**; the whole
  migration is safe to repeat. Configured via `import.photosorter.{dsn,page_size}` (the DSN
  via `KUKATKO_IMPORT_PHOTOSORTER_DSN`, do not commit); without `dsn`, neither the migration job nor the endpoint
  is registered (the CLI returns an error).

### Import admin UI (`internal/importapi` + `web` `ImportPage`)

An admin-only console at `/import` ([`web/src/pages/ImportPage.tsx`](web/src/pages/ImportPage.tsx)) for
triggering and tracking imports — visible in the navbar only to administrators (the `isAdmin` gate), the route under
`RequireRole role="admin"`.

- **Backend** (`internal/importapi`, everything admin-only via `RequireAdmin`) adds, on top of the triggers, a
  **history**: `GET /api/v1/import/runs` (always registered) → `{runs,limit,offset,sources}` —
  a page of `import_runs` newest-started-first (query `limit`≤200/`offset`, invalid → 400) plus
  `sources:{photoprism,photosorter}` flags of which sources are configured. The page is read by
  `importer.Store.List`. The whole API is mounted **always** (even without a configured source), so history
  works; the triggers `POST /import/{photoprism,photosorter}` are registered only for configured
  sources (otherwise 404).
- **Frontend** ([`web/src/services/import.ts`](web/src/services/import.ts)) polls
  `GET /import/runs` + `GET /jobs/stats` every 3 s. Two sections (PhotoPrism, photo-sorter) with a
  **Spustit import** button (gated on the `sources` flags + a running run), the **live progress** of a running run (spinner +
  counts imported/updated/skipped/failed), a summary of the **background queue** (queued/running/failed/dead),
  and a **run history** table (source / start / end / status / counts / last error). It clearly communicates
  that PhotoPrism stays primary and the import is incremental/repeatable; before the **first** (potentially
  large) run of a source it asks for confirmation. 409 → „import už běží". i18n cs/en.

### S3 backup (`internal/backup` + `internal/backupapi`)

An in-process, scheduled backup of the **database and originals** to any **S3-compatible** endpoint
(AWS / MinIO / Backblaze / Wasabi) via **`minio-go/v7`** with **path-style** addressing and
a **streamed** upload (`objectSize=-1`, never holds the whole file in RAM). Everything is behind interfaces
(`ObjectStore`/`Dumper`/`OriginalSource`) → unit-testable with fakes, without S3, DB, or FS. Secret keys
never leak into the log or an error.

A single run does three things in order:

1. **Database dump** — shell-out to **`pg_dump`** (custom/compressed format, `--no-owner
   --no-privileges`) streamed straight to S3 as `db/kukatko-<timestamp>.dump`. The DSN is passed
   via the `PGDATABASE` env variable (not an argument), so the password is not visible in `ps`. The timestamp is supplied by
   the scheduler/command.
2. **Sync of originals** — walks the originals storage and **incrementally** uploads only those not yet in the bucket
   (skip by key + size), streamed; the key = the original's relative path, the dump
   is stored under the `db/` prefix. The temporary upload folder `.tmp` is skipped.
3. **Retention** — after a successful dump it prunes old dumps down to the last `backup.retention` (≤ 0 =
   keep everything). It **never touches originals** (only the `db/` prefix) and **never prunes after
   a failed dump**, so a backup failure cannot delete the last good dumps.

- **Scheduler**: `backup.schedule` (standard 5-field cron or `@daily`/`@hourly`/`@every 6h`
  descriptors via `robfig/cron`) runs inside `kukatko serve`; an empty/invalid schedule disables scheduled
  backups (manual ones still work). Concurrent runs are serialized (`ErrAlreadyRunning`).
- **CLI**: `kukatko backup` runs a single backup synchronously and prints the counts (ops/cron without a running
  server). Requires `backup.s3.endpoint` + `backup.s3.bucket`.
- **Admin API** (`internal/backupapi`, admin-only): `GET /api/v1/backup` (status + last run,
  `configured:false` when not configured) and `POST /api/v1/backup` (runs a backup in the background →
  202, 409 when already running, 503 without configuration).
- Runtime apt dependency: **`postgresql-client`** (pg_dump **and** pg_restore). Config keys
  `backup.s3.{endpoint,region,bucket,access_key,secret_key,path_style}`, `backup.schedule`,
  `backup.retention`; secrets (`access_key`/`secret_key`) via env.

### Restore / disaster recovery (`internal/backup` + `internal/restoreapi`)

The counterpart to the backup, to make it actually **usable**. It shares the `backup.s3.*` configuration (the bucket =
the restore source), `database.url` (the target), and `storage.originals_path` (where originals are written). Everything behind
the same interfaces (`ObjectStore` extended with `Open`, new `Restorer`/`LocalOriginals`/
`PhotoCatalog`) → unit-testable with fakes. Secrets never into the log or argv.

The **`kukatko restore`** CLI tree (ops/cron without a running server; requires `backup.s3.{endpoint,bucket}`):

- **`restore list`** — lists the dumps in the bucket (`db/kukatko-*.dump`), newest first.
- **`restore db [--dump KEY] [--yes] [--verify]`** — **DESTRUCTIVE**: downloads the dump from S3 and streams
  it straight into **`pg_restore`** (`--clean --if-exists --single-transaction --no-owner
  --no-privileges`, reads the archive from stdin → never fully in RAM). The password is passed via the `PGPASSWORD` env
  (parsed from the DSN), **never in argv**. After the restore it idempotently re-applies the migrations. Without `--dump`
  it restores the newest dump; **requires `--yes`** (overwrites all data). `--verify` immediately runs
  the integrity report.
- **`restore originals`** — downloads from the bucket only the originals not yet on disk (skip by
  **key + size**), with an atomic write via `.tmp` + rename → **resumable** (an interrupted run
  is safely repeated). Dumps under `db/` are skipped.
- **`restore verify`** — an integrity report: **photos in the DB vs originals on disk** + mismatches
  (`photo_files.file_path` missing on disk / files on disk with no catalog record), with
  a limited sample in the listing.

**Admin API** (`internal/restoreapi`, admin-only, read-only operations only): `GET /api/v1/restore/dumps`
(list of dumps, 503 without configuration) and `POST /api/v1/restore/verify` (the integrity report). Destructive
DB restore is **deliberately not exposed** over HTTP (it would pull the tables out from under a running server) — it belongs in the CLI
with the server stopped. Thumbnails are regenerated lazily on-demand after a restore; embeddings/faces are in the dump.

The full procedure (fresh machine → install → restore → verify) with exact commands: [`docs/RESTORE.md`](docs/RESTORE.md).

### Library maintenance — integrity check & repair (`internal/maintenance` + `internal/maintenanceapi`)

Keeps a large, long-lived library consistent: it detects drift between the catalog and the files on disk
and fills in/regenerates derived data. Mirrors photo-sorter's `cache build-thumbs`, but is broader and
safer — it **never deletes originals** (that's the job of the trash/purge), is idempotent, and repairs run
through the persistent job queue (bounded concurrency, resumable). Everything is behind interfaces
(`PhotoCatalog`/`VectorCatalog`/`OriginalStore`/`DiskScanner`/`ThumbChecker`/`Enqueuer`/
`EmbedBackfiller`/`FaceBackfiller`/`OrphanImporter`) → unit-testable with fakes, without DB/disk/queue.

- **Integrity check** (`Scan`, read-only): returns a `Report` with counts + limited samples for each
  class of problem — photos with a **missing original** on disk, **orphaned files** on disk without
  a catalog record, photos with **missing thumbnails**, **embeddings**, **face detections**, and
  **perceptual hashes**, plus totals (photos / files in DB / originals on disk).
- **Repairs** (`Repair`, each opt-in, idempotent): regenerates missing thumbnails and recomputes
  missing pHash/dHash (via the `thumbnail` job → the `internal/thumbjob` handler, which rebuilds both thumbnails and pHash
  from the original), enqueues `image_embed`/`face_detect` for photos without them, and optionally
  **imports orphaned originals** into the catalog via the upload pipeline (content dedup). Fixed order;
  a per-orphan failure is counted without aborting, the result is a `RepairResult` with scheduling counts.
- **CLI**: `kukatko maintenance scan` (prints the report) and `kukatko maintenance repair`
  with flags `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans` (ops/cron without a
  running server; repairs enqueue jobs that the running server's worker drains).
- **Admin API** (`internal/maintenanceapi`, admin-only): `GET /api/v1/maintenance/scan` (the integrity
  report) and `POST /api/v1/maintenance/repair` `{thumbnails,embeddings,faces,phashes,import_orphans}`
  (runs the selected repairs → `RepairResult`; 400 without a selected repair, 503 when orphan import is not
  configured). The **Údržba** admin UI (`/maintenance`, `MaintenancePage`) runs the check, shows
  the findings, and runs the repairs with progress via job-queue polling.

### System status — admin dashboard (`internal/system` + `internal/systemapi`)

A single place with the operational health of the running instance, aggregated from existing subsystems (no new
data, just a merge). The domain service (`internal/system`) collects everything behind small interfaces
(`DBPinger`/`EmbeddingHealth`/`JobCounter`/`ImportLister`/`BackupReporter`) → unit-testable
with fakes, without a DB; the HTTP layer (`internal/systemapi`) is thin.

- **Aggregation** (`Service.Collect`): **embeddings sidecar availability** (online/offline via
  `embedding.Client.Healthy`), **job queue depth** (counts by state/type, total, dead-letter,
  "pending embeddings" = queued/running `image_embed`+`face_detect`), **backup status** (the last
  run + result, nil-safe when not configured), **the last import per source**
  (`importer.Store.LatestRun` — the newest run regardless of status), **storage usage**
  (size of originals + cache, free/total space via `statfs`; the measurement is memoized for
  `defaultStorageTTL` = 30 s, so polling does not walk the large originals tree), **DB availability**
  (`db.Ping`, a sanitized error — the connection string does not leak) and the build's **version/commit**. Errors
  reading the queue/imports (which require a DB) return 500; an unavailable DB and unreadable storage are reported inline
  (best-effort), not as an error.
- **Admin API** (`internal/systemapi`, admin-only via `RequireAdmin`): `GET /api/v1/system/status`
  → a single snapshot. Mounted always (`buildSystemAPI` in `cmd/kukatko/system.go`, builds its own
  stateless embeddings client just for the Healthy probe and shares the pool for the job/import stores; the backup
  service is passed nil-safe).
- **Admin UI** **Systém** (`/system`, `SystemStatusPage`, admin-only) — auto-refresh (polling 5 s)
  card grid (DB, embeddings, queue, backup, imports, storage, version) with **quick actions**:
  *requeue dead jobs* (lists the dead-letter and requeues them via `POST /jobs/{id}/requeue`),
  *run a backup* (`POST /backup`), and links into the *import* flow (`/import`) and *maintenance check*
  (`/maintenance`). When the **box is offline** and embedding jobs are waiting, the card highlights it with the message
  „box offline → embeddingy ve frontě, doženou se po návratu".

### Observability — metrics & structured logs (`internal/metrics` + `internal/obs`)

Lightweight observability modeled on photo-sorter, wired into `kukatko serve`.

**Prometheus metrics** (`internal/metrics`): a single isolated registry (not the global
`DefaultRegisterer`, so tests have their own surface) in the `kukatko_` namespace. `serve`
mounts it at **`GET /metrics`** (outside `/api/v1`, **without authentication** — protect it at the network
layer) and installs the request-metrics middleware. Series:

- **HTTP** — `kukatko_http_requests_total{method,route,status}`,
  `kukatko_http_request_duration_seconds{method,route}`, `kukatko_http_inflight_requests`.
  `route` is the **chi route pattern** (`/photos/{uid}`), no match → `unmatched` (bounded cardinality,
  never a raw URL). The `/metrics` scrape does not count itself.
- **Jobs** — `kukatko_jobs_started_total{type}`, `kukatko_jobs_finished_total{type,outcome}`,
  `kukatko_jobs_execution_duration_seconds{type,outcome}` (outcome `success`/`error`/`deferred`)
  via the `worker.Observer` hook; queue depth `kukatko_jobs_queue_depth{state}` +
  `kukatko_jobs_queue_depth_by_type{type}` via a collector that reads `jobs.Store` on scrape.
- **Embeddings sidecar** — `kukatko_embedding_request_duration_seconds{operation,outcome}` +
  `kukatko_embedding_service_up` via the `embedding.Instrument` decorator (transparent, returns
  the inner error unchanged, so `errors.Is(ErrUnavailable)` keeps working).
- **Import** — `kukatko_import_run_photos{source,outcome}` (the run's last checkpointed tally)
  via `importer.ProgressObserver` in ppimport/psimport.
- **Thumbnails** — `kukatko_thumbnail_generation_duration_seconds` via `thumb.WithObserver`.
- **DB pool** — `kukatko_db_pool_*` (total/acquired/idle/max + wait/empty-acquire) via a collector
  over `pgxpool.Stat`.
- The standard `go_*` and `process_*` families.

**Structured logs** (`internal/obs`): slog **JSON** to stderr, level from `log.level`
(`KUKATKO_LOG_LEVEL`; debug/info/warn/error, invalid → error at startup). The **access-log
middleware** writes one line per request with consistent fields `request_id` (from chi
`RequestID`, ties the logs + the `X-Request-Id` header together), `method`, `path`, `route`, `status`, `bytes`,
`duration_ms`, `remote_ip`, and `user` (the UID, stamped by the auth middleware into a request-scoped bag);
`/metrics` is not logged. **Secret redaction**: the slog `ReplaceAttr` hook replaces the value of any
attribute whose key contains `password`/`token`/`secret`/`api_key`/`access_key`/`secret_key`/
`authorization`/`cookie`/`credential`/`dsn` with `[REDACTED]` — the mapy key, S3 keys, session token,
and password never leak into the log. To disable metrics: `metrics.enabled=false`
(`KUKATKO_METRICS_ENABLED=false`) — `/metrics` is not mounted, the access log keeps running.

## Configuration

Kukátko is configured via a **YAML file with env overrides** (Viper; env always wins).
Copy [`config.example.yaml`](config.example.yaml) to `config.yaml` (or the gitignored
`config.local.yaml`) and edit it. The file path is taken from the `--config` flag, otherwise from the
`KUKATKO_CONFIG` env, otherwise the default `config.yaml`. **The file is optional** — with `KUKATKO_DATABASE_URL`
in the environment the app runs on its defaults.

Env variables carry the `KUKATKO_` prefix and dots in the key become underscores
(`database.url` → `KUKATKO_DATABASE_URL`, `web.port` → `KUKATKO_WEB_PORT`,
`backup.s3.bucket` → `KUKATKO_BACKUP_S3_BUCKET`). Exception: the mapy.com key is read from the
unprefixed `MAPY_API_KEY`. Keep secrets (DSN, session secret, admin password, S3 keys,
mapy key) in the environment, not in a committed file. All keys and defaults are documented in
`config.example.yaml`.

## Authentication and authorization

Kukátko has its own accounts (bcrypt cost 12), roles **admin / editor / viewer** (editor and admin have
write, viewer is read-only) and sessions via an **HttpOnly + SameSite=Strict** cookie holding an opaque
token. Improvements over photo-sorter: **sliding expiry** (an active session is extended),
a **login rate limit** and **changing the password revokes the user's other sessions**.

- **Admin bootstrap:** on an empty `users` table the first admin is created from `auth.bootstrap_admin_username` +
  `auth.bootstrap_admin_password` (otherwise `serve` merely logs a warning).
  Pass the password via the `KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD` env, don't commit it.
- **Sliding expiry:** every authenticated request pushes `expires_at` to `now+session_ttl`, but never
  past `created_at+session_max_lifetime`. Expired sessions are cleaned up by an hourly job.
- **Rate limit:** more than `auth.login_rate_limit` failed attempts per (username+IP) within
  `auth.login_rate_window` returns HTTP 429.

### Rate limits for heavy endpoints (`internal/ratelimit`)

Beyond login, resource-heavy endpoints also have **per-client-IP token-bucket** limits, so that one
noisy client can't exhaust shared resources. `internal/ratelimit` is a reusable package
(`New(ratePerSec, burst)` → `Allow(key)` / `Middleware`), keyed by the client's IP address (from
`X-Forwarded-For`/`X-Real-IP` behind a trusted proxy via chi `RealIP`); an empty bucket → **HTTP
429** with a `Retry-After` header. The limiter runs **before** the auth check (a flood is dropped before the
DB lookup), it is **memory-bounded** (opportunistic cleanup of fully refilled buckets, capped at
`maxBuckets`), and `rate_per_sec ≤ 0` **disables** the whole rule (the middleware then becomes a no-op).

Covered endpoints and defaults (config section `ratelimit.*`):

| Rule | Endpoint | `rate_per_sec` | `burst` |
|----------|----------|----------------|---------|
| `upload` | `POST /upload` | 5 | 30 |
| `bulk` | `POST /photos/bulk` | 2 | 10 |
| `import` | `POST /import/{photoprism,photosorter}` | 1 | 3 |
| `tiles` | `GET /map/tiles/...` | 50 | 200 |

The reverse-geocode proxy (`GET /map/rgeocode`) keeps its own credit-saving limiter under `maps.*`.
Override the keys via env, e.g. `KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC=10`.

Endpoints under `/api/v1` (JSON):

| Method | Path | Access | Description |
|--------|-------|---------|-------|
| POST | `/auth/login` | public | `{username,password}` → sets the session cookie, returns the user + `download_token` |
| POST | `/auth/logout` | public | revokes the session and cookie (idempotent) |
| GET  | `/auth/me` | authenticated | current user + `download_token` |
| POST | `/auth/password` | authenticated | `{current_password,new_password}` → changes the password, revokes other sessions |
| GET  | `/admin/users` | admin | list of users |
| POST | `/admin/users` | admin | `{username,password,display_name,email,role}` → creates a user |
| PATCH | `/admin/users/{uid}` | admin | `{display_name,email,role,disabled}` → edits the profile |
| POST | `/admin/users/{uid}/disable` | admin | disables the account (revokes its sessions) |
| POST | `/admin/users/{uid}/password` | admin | `{new_password}` → password reset (revokes all its sessions) |
| POST | `/upload` | editor/admin | `multipart/form-data` with one+ files → per-file `{outcome, photo_uid, warnings}` (see Upload / ingest) |
| GET | `/photos` | authenticated | list with filters/sorting/pagination → `{photos,total,limit,offset,next_offset}` (see Photo API); per-user filters `min_rating` (≥ n), `flag` (`pick`/`reject`) and sorting `sort=rating` scoped to the current user; `country`/`city` scope to a location (see Places) |
| GET | `/search?q=&mode=` | authenticated | semantic + hybrid search; `mode` = `fulltext`/`semantic`/`hybrid` (default `hybrid`): fulltext (tsvector + unaccent, `ts_rank`), semantic (CLIP text→embedding → cosine HNSW), hybrid (fusion of both via **Reciprocal Rank Fusion**, k=60, dedup); all modes honour list filters + pagination; `q` required → same shape as `/photos` + `mode`+`degraded`; box offline → fallback to fulltext with `degraded:true` |
| GET | `/photos/{uid}` | authenticated | full photo detail (metadata, EXIF, GPS) + `files` + `albums` + `labels` + `is_favorite` + `rating`/`flag` (for the current user) |
| GET | `/photos/{uid}/edit` | authenticated | the stored non-destructive edit (`crop`/`rotation`/`brightness`/`contrast`); an unedited photo → neutral edit |
| PUT | `/photos/{uid}/edit` | editor/admin | writes the non-destructive edit into `photo_edits` (bounds validation); the original is unchanged, download honours it |
| GET | `/photos?favorite=true` | authenticated | list scoped to the **current user's** favorites (per-user); every photo in the list/search/detail carries `is_favorite` + `rating`/`flag` (per-user, unrated = 0 / `none`) |
| PUT | `/photos/{uid}/favorite` | authenticated | marks the photo as a favorite of the current user (idempotent) → 204; 404 missing photo |
| DELETE | `/photos/{uid}/favorite` | authenticated | removes the current user's favorite (idempotent) → 204 |
| PUT | `/photos/{uid}/rating` | authenticated | sets the current user's stars and/or flag `{rating?:0..5, flag?:none\|pick\|reject\|eye}` (at least one) → 204; 400 invalid value, 404 missing photo, 503 no backend |
| DELETE | `/photos/{uid}/rating` | authenticated | clears the current user's rating and flag (idempotent) → 204 |
| GET | `/favorites` | authenticated | the current user's favorites in the shape of `/photos` (shares filters/sorting/pagination) |
| GET | `/photos/{uid}/similar` | authenticated | visually similar photos by cosine distance of the embedding (`?limit`, default 24, max 100) → `{similar:[{…photo, distance}]}` |
| GET | `/photos/{uid}/faces` | authenticated | the photo's faces with bbox, assignment (marker/subject), action (`create_marker`/`assign_person`/`already_done`) and identity **suggestions** for each face — for an unnamed one candidates, for an assigned one alternatives for reassignment; face↔marker IoU matching (see `internal/facematch`) |
| POST | `/photos/{uid}/faces/assign` | editor/admin | assignment action `{action, face_index?, marker_uid?, subject_uid?, subject_name?, bbox?}`: `create_marker`/`assign_person`/`unassign_person`; auto-creates a subject by name; keeps the `faces` cache + `marker.reviewed` consistent |
| GET | `/faces/clusters` | editor/admin | clusters of unassigned faces (auto-clustering) → `{clusters:[{uid,size,representative,examples,suggestion?}]}`; `suggestion` = the nearest named subject (see `internal/cluster`) |
| POST | `/faces/clusters/{id}/assign` | editor/admin | assigns an **entire cluster** to one subject `{subject_uid?,subject_name?}` (find-or-create by name) → markers for all faces; the cluster is consumed |
| POST | `/faces/clusters/{id}/remove-face` | editor/admin | detaches a stray face `{photo_uid,face_index}` from the cluster before naming it → the refreshed cluster (or `null` when it is orphaned) |
| GET | `/subjects/{uid}/outliers` | editor/admin | a person's faces ordered by distance from the centroid (most suspicious first) → `{subject_uid,count,meaningful,faces:[{photo_uid,face_index,bbox,distance,…}]}`; 1–2 faces → `meaningful:false` (see `internal/outliers`); a wrong face is detached via the assign API |
| GET | `/subjects` | authenticated | list of subjects with photo counts → `{subjects:[{…subject, marker_count}]}` (see Subjects / People API) |
| POST | `/subjects` | editor/admin | `{name,type,favorite,private,notes,cover_photo_uid?}` → 201 creates a subject (empty name/unknown type → 400) |
| GET | `/subjects/{uid}` | authenticated | subject detail (404 missing) |
| PATCH | `/subjects/{uid}` | editor/admin | edits `name/type/favorite/private/notes/cover_photo_uid` |
| DELETE | `/subjects/{uid}` | editor/admin | deletes the subject (markers are detached) → 204 |
| GET | `/subjects/{uid}/photos` | authenticated | paginated gallery of a subject's photos → `{photos,total,limit,offset,next_offset}` (newest-first, non-archived) |
| GET | `/albums` | authenticated | list of albums with photo counts + cover → `{albums:[{…album, photo_count}]}` (see Albums & labels API) |
| POST | `/albums` | editor/admin | `{title,description?,type?,cover_photo_uid?,private?}` → 201 (empty title/invalid type → 400) |
| GET | `/albums/{uid}` | authenticated | album detail (404 missing) |
| PATCH | `/albums/{uid}` | editor/admin | edits `title/description/cover_photo_uid/private` (the structural `type` is preserved) |
| DELETE | `/albums/{uid}` | editor/admin | deletes the album (memberships are detached) → 204 |
| POST | `/albums/{uid}/photos` | editor/admin | `{photo_uids:[…]}` appends photos after the existing ones → `{photo_uids:[…]}` (current order) |
| DELETE | `/albums/{uid}/photos` | editor/admin | `{photo_uids:[…]}` removes photos → `{photo_uids:[…]}` (an album is always chronological, there is no manual reordering) |
| GET | `/labels` | authenticated | list of labels with photo counts → `{labels:[{…label, photo_count}]}` (ordered by priority DESC) |
| POST | `/labels` | editor/admin | `{name,priority?}` → 201 (empty name → 400) |
| GET | `/labels/{uid}` | authenticated | label detail (404 missing) |
| PATCH | `/labels/{uid}` | editor/admin | edits `name/priority` |
| DELETE | `/labels/{uid}` | editor/admin | deletes the label (attachments are detached) → 204 |
| POST | `/labels/{uid}/photos` | editor/admin | `{photo_uid,source?,uncertainty?}` attaches the label to a photo → 204 |
| DELETE | `/labels/{uid}/photos` | editor/admin | `{photo_uid}` detaches the label from a photo → 204 |
| GET | `/photos?album={uid}` / `?label={uid}` | authenticated | scoped listing of an album's/label's photos via the shared `/photos` (honours filters/sorting/pagination, same shape) |
| GET | `/places` | authenticated | place hierarchy with counts `{places:[{country,count,cities:[{city,count}]}]}` over non-archived photos; `?country=` drills into the cities of one country; ordered by count desc/name |
| GET | `/photos?country=&city=` | authenticated | scoped listing of a given location's photos (exact match on the `photo_places` cache) via the shared `/photos` (honours the other filters/sorting/pagination) |
| PATCH | `/photos/{uid}` | editor/admin | partial edit of `title/description/notes/taken_at/lat/lng` (null clears a nullable field) |
| POST | `/photos/{uid}/archive` | editor/admin | soft-delete (sets `archived_at`) → returns the photo |
| POST | `/photos/{uid}/unarchive` | editor/admin | restores an archived photo |
| POST | `/photos/{uid}/purge` | editor/admin | **permanently** deletes an archived photo (row+cascade, original, thumbnails, and S3 if any); requires `?confirm=true` → 204, 400 without confirmation, 404 missing, 409 photo is not archived |
| GET | `/trash/info` | authenticated | retention window `{retention_days}` for the countdown to auto-purge |
| POST | `/trash/empty` | editor/admin | **permanently** deletes all archived photos (requires `?confirm=true`) → `{purged,failed}` |
| GET | `/duplicates` | editor/admin | groups of likely duplicates (pHash + embedding) → `{groups,total,limit,offset,next_offset}`; query `limit`(≤100)/`offset`; 503 when `duplicate.enabled=false` (see Duplicates) |
| POST | `/duplicates/merge` | editor/admin | resolves a group — merges the copies into the keeper (union of albums/labels/people, fills missing fields) and archives them, in one transaction → `{keeper_uid,albums_added,labels_added,people_added,metadata_filled,archived,dry_run}`; `dry_run:true` = preview |
| GET | `/photos/{uid}/thumb/{size}` | session/token | thumbnail (cached, generated on-miss) — streams JPEG, `ETag`/304 |
| GET | `/photos/{uid}/video` | session/token | inline video stream with **HTTP Range** (206 partial, `Accept-Ranges`, seek; a live photo = motion clip, still → 404); optional on-the-fly transcode via `video.transcode` |
| GET | `/photos/{uid}/download` | session/token | photo as an attachment — the original is streamed (never wholly in RAM), `Content-Length`/`ETag`; if a non-destructive edit is stored, it returns the **edited** version rendered on the fly (unless `?original=true`) |
| GET | `/jobs/stats`, `GET /jobs`, `POST /jobs/{id}/requeue` | admin | job queue (see Admin Jobs API) |
| POST | `/process/embeddings` | admin | backfill — enqueues `image_embed` for photos without an embedding → `{enqueued}` (see Process API) |
| POST | `/process/faces` | admin | backfill — enqueues `face_detect` for photos without face detection → `{enqueued}` (see Process API) |
| POST | `/process/clusters` | admin | re-clustering — groups unassigned faces into clusters → `{created}` (see Process API) |
| POST | `/process/places` | admin | backfill — enqueues `places` reverse-geocode for geotagged photos without a place → `{enqueued}`; 503 without a mapy.com key (see Process API) |
| GET | `/import/runs` | admin | history of import/migration runs + `sources` flags → `{runs,limit,offset,sources}` (see Import admin UI) |
| POST | `/import/photoprism` | admin | enqueues a `pp_import` job (only if the source is configured) → 202 `{job_id,status}`, 409 already running |
| POST | `/import/photosorter` | admin | enqueues a `ps_migrate` job (only if the source is configured) → 202 `{job_id,status}`, 409 already running |
| GET | `/backup` | admin | S3 backup status + last run (`configured:false` without configuration) |
| POST | `/backup` | admin | starts a backup in the background → 202 `{status}`, 409 already running, 503 without configuration |
| GET | `/restore/dumps` | admin | list of dumps in the bucket (newest first) → `{dumps}`, 503 without configuration, 502 on an S3 error |
| POST | `/restore/verify` | admin | integrity report (photos in the DB vs. originals on disk) → `VerifyReport`, 503 without configuration |

RBAC is enforced by middleware (`RequireAuth` / `RequireWrite` / `RequireAdmin` /
`RequireAuthOrDownloadToken`). The configuration
keys (`auth.session_ttl`, `auth.session_max_lifetime`, `auth.login_rate_limit`,
`auth.login_rate_window`, `web.secure_cookies`) are documented in [`config.example.yaml`](config.example.yaml).

### Photo API (`internal/photoapi`)

Browsing and curating the catalog live in the [`internal/photoapi`](internal/photoapi/) package (an HTTP
layer over `internal/photos` + `internal/storage` + `internal/thumb`; the guards are injected
from the auth subsystem, so the package doesn't know its wiring). The endpoints are mounted by `buildPhotoAPI`
(`cmd/kukatko/photos.go`) as the third `server.WithAPI` in `serve`.

- **List** `GET /photos` — query parameters mirrorable into the URL (FE "Back always works"):
  - **Filters:** `taken_after`/`taken_before` (RFC3339 or `YYYY-MM-DD`), `has_gps` (`true`/`false`),
    `camera`, `lens`, `q` (fulltext title/description/notes),
    `uploader` (UID), `archived` (`false` default = only live, `true` = including the archive, `only` =
    archive only), `year` (four-digit calendar year of capture, 1000–9999; photos without `taken_at`
    never match), `favorite=true` (only photos the **logged-in user** has marked as a favorite —
    per-user scope via a correlated `EXISTS` over `user_favorites`).
  - **Sorting:** `sort` = `newest` (default) / `oldest` / `taken_at` / `added` / `title` / `size`,
    with an optional `order=asc|desc`.
  - **Pagination:** `limit` (default 100, max 500) + `offset`. The response carries `total` and
    `next_offset` (null on the last page) for infinite scroll.
  - **`is_favorite`:** every photo in the response (list, search and detail) carries an `is_favorite`
    flag for the **current user** (the whole page is annotated with a single `FavoritedAmong` query).
  - **Invalid parameter → HTTP 400.**
- **Detail** `GET /photos/{uid}` — the photo + `files` (list of `photo_files`) + `is_favorite`,
  `404` when missing.
- **Timeline** `GET /photos/timeline` (authenticated) — a monthly date histogram of the library for a fast
  year/month scrubber. It accepts the **same filters** as `GET /photos` (archived/has_gps/date
  range/camera/lens/uploader/album/label/country/city/favorite/`q`) via the shared `parseListParams`,
  so the buckets match exactly what the list would return in the same order. The response is
  `{buckets:[{year,month,count,cumulative}],total}` — buckets ordered **newest first** (by
  `taken_at`, like the default grid), `cumulative` = the number of photos **before** the bucket in this order
  (maps a bucket to a scroll index). `total` is the overall count (via `Count`) and also includes photos without
  a capture date, which fall into no bucket (they sort to the end). `sort`/`order` are ignored
  (always grouped by date; the scrubber assumes the default date sort). It is backed by
  `photos.Store.TimelineBuckets` (shares `buildWhere` with `List`/`Count`). **Invalid parameter → 400.**
- **Years** `GET /photos/years` (authenticated) — a year histogram of the library, the basis for the filters'
  **year facet**. It accepts the **same filters** as `GET /photos` via the shared `parseListParams` and honours
  the caller's visibility (archived) as well as per-user filters (favorite, rating/flag), so a year's count
  is exactly what the grid shows once it is selected. The response is `{years:[{year,count}],total}` —
  **newest year first**. The `year` filter is the **only one ignored**: a facet must not narrow its own
  offering, otherwise after selecting year 2019 only 2019 would remain on offer and there'd be no way to switch.
  `sort`/`order` and pagination are ignored (always grouped by year). `total` (via `Count`)
  also includes photos **without a capture date**, which fall into no year, so it may exceed the
  sum of the counts. It is backed by `photos.Store.YearBuckets` (shares `buildWhere` with `List`/`Count`).
  **Invalid parameter → 400.**
- **Favorites** `PUT /photos/{uid}/favorite` + `DELETE /photos/{uid}/favorite` (each authenticated,
  favorites are personal) — an idempotent favorite toggle for the current user → `204`; `404`
  on a missing photo, `503` without a favorites backend. **Listing** `GET /favorites` (authenticated) —
  the current user's favorites in the same shape as `GET /photos` (shares filters/sorting/pagination,
  it is the equivalent of `GET /photos?favorite=true`). The favorites backend (`FavoriteStore`, satisfied by
  `organize.Store`) is injected via `Config.Favorites`.
- **Similar** `GET /photos/{uid}/similar` — photos ordered by increasing cosine distance of the source
  photo's embedding, **excluding** itself; each carries the full `Photo` + `distance`. `?limit`
  (default 24, max 100). **Empty-friendly:** a photo without an embedding (not yet processed) → an empty
  list with `200`, a non-existent photo → `404`. Vector search is provided by the injected
  `SimilarSearcher` (= `vectors.Store`); the neighbours are fetched with a batch query `photos.ListByUIDs`.
- **Edit** `PATCH /photos/{uid}` (editor/admin) — partial: a field omitted from the body is unchanged,
  an explicit `null` clears a nullable field (`taken_at`/`lat`/`lng`); the coordinate range is validated
  (`lat ∈ ⟨-90,90⟩`, `lng ∈ ⟨-180,180⟩`).
- **Archiving** `POST /photos/{uid}/archive` + `/unarchive` (editor/admin) — soft-delete via
  `archived_at`; archived photos are excluded from the default list.
- **Trash / permanent deletion** (`internal/trash`) — archived photos, once their retention
  (`trash.retention_days`, default 30) elapses, are **hard-deleted** by a scheduled sweep in `kukatko serve`
  (every 6 h, `RunPurge`; retention ≤ 0 disables it). Purge deletes the DB row (cascading embeddings/
  faces/markers/album_photos/photo_labels/phashes/edits/favorites via `ON DELETE CASCADE`),
  the original + cached thumbnails from disk and (if configured) the corresponding S3 object; it is
  **idempotent** and deletes the artefacts **before** the row, so an interrupted run leaves a re-purgeable
  row instead of orphaned files (no dangling files). Manual control:
  `POST /photos/{uid}/purge` (one photo) and `POST /trash/empty` (everything) — both require
  `?confirm=true`; `GET /trash/info` returns the retention window for the countdown in the UI. The trash listing runs via
  the shared `GET /photos?archived=only`. The HTTP layer (`internal/photoapi/trash.go`) calls the purge
  service through the `Purger` interface (nil → 503); the service is built by `buildTrashService`
  (`cmd/kukatko/trash.go`). The **Trash UI** is the `/trash` page (editor/admin) with restore and permanent
  deletion (individually and in bulk) and a countdown to auto-purge.
- **Duplicates — review and resolution** (`internal/duplicates` + `internal/dupmerge` + `internal/duplicatesapi`) — alongside
  the upload-time warning there is a **review surface**: `GET /duplicates` (editor/admin) returns
  **groups** of likely duplicates. It links photos by two signals — perceptual hash (pHash)
  Hamming distance up to `duplicate.phash_max_diff` bits and embedding cosine distance up to
  `duplicate.embedding_max_dist` — and merges the edges into connected components via union-find. **No
  O(n²) scan:** pHash uses banded-LSH buckets (pigeonholing into `maxDiff+1` bands guarantees a shared
  bucket for pairs within the threshold), embeddings go through the HNSW index (`vectors.FindDuplicatePairs`,
  a correlated `CROSS JOIN LATERAL` with `LIMIT` neighbours per photo). Each group carries its members
  with details for comparison (thumbnail, dimensions, size, `taken_at`, distance to the keeper)
  and a **suggested keeper** (highest resolution → largest file → oldest → smallest uid); groups are
  ordered largest-first, paginated by `limit`(≤100)/`offset`. Detection **never deletes anything** — it only reads.
  Embeddings are read from Postgres, so it works even when the box is offline. Wired by `buildDuplicatesAPI`
  (`cmd/kukatko/duplicates.go`); with `duplicate.enabled=false` the route is mounted with a nil service
  and responds 503. **Resolution** (`POST /duplicates/merge`, `internal/dupmerge`) doesn't discard the group — in one
  transaction it **merges the remaining copies into the chosen keeper**: the keeper inherits the union of albums,
  labels and people (a person = a box-less marker, because a bounding box is tied to specific pixels) and fills its missing scalar
  fields (title/description + per-user rating/favorite/flag; it **never overwrites** an existing value),
  and only then are the copies archived (originals remain until purge) and the merge is written to the audit trail. Idempotent
  (a re-run on a resolved group = no-op). The **Duplicates UI** is the `/duplicates` page (editor/admin):
  groups side by side, the user picks the photo to keep, **„Ponechat nejlepší a sloučit"** shows a preview
  (what will move + how many copies will be archived) to confirm and then calls merge, or **rejects** the group
  as „není duplikát" (it disappears from view). No auto-deletion, always confirmed by the user.
- **Comparing two duplicates side by side** (`/duplicates/compare`, editor/admin) — the duplicates list can
  tell you that two photos are nearly identical; **it can't help you decide which one to keep**. What decides
  it — one is 12 Mpx and the other 2, one has the correct date, one came from a camera and the other
  from WhatsApp, and above all: **the "worse" one is sometimes the one all your albums and people hang on** — it isn't
  visible until you open both side by side. The compare view is fullscreen, both photos side by side (on a narrow
  display stacked) under **one synchronized zoom** (the wheel zooms toward the cursor, dragging
  pans, double-click toggles) — only that reveals that one is a soft JPEG re-encode of the other.
  A **difference table** compares dimensions+megapixels, size, format, capture date, camera,
  lens, filename, place and albums/labels/people, and **flags only the rows that differ** (identical ones
  are noise); a toggle can hide the identical ones entirely. Three actions: **Nechat levou / Nechat pravou** (keep left / keep right) →
  a dry-run preview (what the merge will carry over) → confirmation → `POST /duplicates/merge` **for that pair only**
  (the group's third member wasn't on screen, so the decision doesn't touch it); **Nechat obě** (keep both) →
  `POST /feedback/duplicate-dismissals`, i.e. a **permanent** "these two really are different" — detection is
  recomputed on every call, so without saving it the same pair would be offered forever. Groups
  of more than two members are compared **pairwise against the suggested keeper** and the UI says so
  („Dvojice 1 z 2 v této skupině" — pair 1 of 2 in this group); no member is hidden. After a decision you go **to the next pair**,
  not back to the list — it's a queue. Keys `←` / `→` / `b` / `Esc` (in the `?` overlay). **Safety:**
  the copy that isn't kept is **archived to the trash, never deleted** — permanent removal is a separate, manual
  action from `/trash`; the confirmation dialog says so.
- **Media** `GET /photos/{uid}/thumb/{size}` and `/download` — they are **streamed** (`io.Copy`, never
  the whole file in RAM), with `Cache-Control`/`ETag` (and `304` on `If-None-Match`). Access via the session
  cookie **or** a `download_token` in the `?t=…` query parameter (`RequireAuthOrDownloadToken`), so
  `<img>`/`<video>` work without a cookie too. A thumbnail is generated on-demand on a cache miss; download
  sends the original as an `attachment` with `Content-Length` from the DB.
- **Video** `GET /photos/{uid}/video` (`internal/photoapi/video.go`) — an inline stream for the HTML5
  player **with HTTP Range** via `http.ServeContent` (206 partial, `Content-Range`, `Accept-Ranges`,
  seek without downloading the whole clip, `If-Range`/`If-None-Match`/`If-Modified-Since`, memory-bounded from an
  `*os.File` via `storage.Materialize`). A live photo streams its **motion clip** sidecar
  (`pickMotionClip` by video MIME/extension), a still image → 404. **On-the-fly transcode** is gated by
  `video.transcode` (default off) + `video.IsWebFriendlyCodec` (h264/vp8/vp9/av1/… play natively) +
  `video.FFmpegAvailable`: a non-web-friendly codec is transcoded to progressive H.264/MP4
  (`video.Transcode`, no range, `no-store`), with a fallback to the original when ffmpeg fails. The frontend
  (`VideoPlayer`/`LivePhoto`) shows a download fallback when the browser can't play the codec.

### Process API (`internal/processapi`)

An admin-only HTTP API for bulk processing of the catalog (guarded by `RequireAdmin`), mounted by
`buildJobs` (`cmd/kukatko/jobs.go`) via `server.WithAPI`:

- `POST /api/v1/process/embeddings` → `{enqueued}` — runs `embedjob.BackfillEmbeddings`:
  enqueues an `image_embed` job for every photo without an embedding (dedup = no-op), returns the count. A recovery
  path for photos uploaded while the box was offline, or imported before embeddings were introduced.
- `POST /api/v1/process/faces` → `{enqueued}` — runs `facejob.BackfillFaces`: enqueues a
  `face_detect` job for every photo without face detection (dedup = no-op), returns the count. A recovery
  path, same as for embeddings.
- `POST /api/v1/process/clusters` → `{created}` — runs `cluster.Recluster`: groups the as-yet
  unassigned, unclustered faces into clusters of the same person (connected components over HNSW neighbours within
  the cosine-distance threshold), returns the count of newly formed clusters. Incremental and re-runnable —
  it doesn't touch assigned or already-clustered faces (see `internal/cluster`).

## Frontend

The SPA is **React 19 + TypeScript + Vite** in the [`web/`](web/) directory, styled with the
**Bootswatch Superhero** (dark) theme via **react-bootstrap**, with `react-router-dom` routing
and i18n via **i18next** (**Czech default** + English; the language switch lives in the **Jazyk** section
on the **Můj účet** page, not in the navigation bar, and the choice is persisted to `localStorage`. Without a saved
choice the language is always Czech — the app doesn't ask the browser).
All UI texts go through `t()` — **no hard-coded strings**; both languages
have a complete, parallel set of keys. Counts are **pluralized** via i18next CLDR plural suffixes
(Czech `_one/_few/_many/_other`, English `_one/_other` — the caller passes `{ count }`), dates are
formatted according to the active language (`lib/format` `formatDate`/`formatDateTime`). The set is guarded by
**drift-guard tests** ([`web/src/i18n/i18n.test.ts`](web/src/i18n/i18n.test.ts) +
[`screens.test.tsx`](web/src/i18n/screens.test.tsx)): cs/en must have identical logical keys,
no empty values, complete plural categories and matching interpolation variables, and representative
screens render without missing-translation warnings. The build (`npm run build`) is written to
`internal/web/static/dist`, from where
Go embeds it (`//go:embed`) and serves it with an **SPA fallback** (unknown non-asset paths →
`index.html`; fingerprinted files under `/assets/` have an immutable cache). `kukatko serve`
thus serves both `GET /healthz` and the whole SPA.

**Mobile/tablet:** the whole application is responsive for phones and tablets. `index.html` has
`viewport-fit=cover` and a thin global layer [`web/src/styles/app.css`](web/src/styles/app.css)
(imported in `main.tsx`) handles cross-cutting touch things that Bootstrap utilities can't:
**safe-area insets** (notch/home-indicator) on the navbar and the main container, a guard against
horizontal scroll and overscroll bounce, a shared **sticky-toolbar offset**
(`.kukatko-sticky-toolbar` — in-page sticky bars such as the selection toolbar settle below the navbar,
not underneath it) and a minimum **tap target of 44px** (`.kukatko-tap-target`) for icon-only controls (the favorites
heart). On top of that an **app-wide touch-target floor**: `@media (pointer: coarse)` enforces a minimum of 44px on
all buttons, form controls, nav/dropdown items and checkboxes on touch
devices (phone/tablet), without intruding on the denser desktop layout — a systematic fix
for small `size="sm"` controls across the app. Grids are an `auto-fill` CSS grid (adapts the column count to the width), the photo detail
**wraps to full width** below `lg` (the preview above the metadata panel), edit modals (albums/labels/people/
bulk edit) go **fullscreen** on phones (`fullscreen="sm-down"`), the slideshow and the map
are fully touch (swipe, pinch/zoom/pan). Multiupload takes photos from the **mobile gallery and camera**
([`components/upload/DropZone`](web/src/components/upload/DropZone.tsx) — `accept="image/*,video/*"`,
`multiple`, plus a camera button via `capture="environment"`).

**Authentication in the frontend:** `AuthProvider` ([`web/src/auth/`](web/src/auth/)) loads
`GET /auth/me` at startup and, via the `useAuth()` hook, exposes `user`/`role`/`login`/`logout`/`refresh` +
the derived `canWrite`/`isAdmin`. The login page (`/login`) is public; everything else is guarded by
`RequireAuth` (unauthenticated → redirect to `/login` saving the original path, and after login
returning back), roles guarded by `RequireRole`. The navbar shows the logged-in user with logout and
a link to **Můj účet** (`/account` — changing one's own password via `POST /auth/password`, plus
a muted line with the API status (`GET /healthz`) and the build version); write
actions are hidden from viewers (`viewer`). Auth calls to the backend live in
[`web/src/services/auth.ts`](web/src/services/auth.ts) (types `User`/`Role`/`AuthSession`,
`ApiError` with a status, helpers `canWrite`/`roleAtLeast`).

**URL = state (Back always works):** the shared hook `useUrlState`
([`web/src/lib/urlState.ts`](web/src/lib/urlState.ts)) reads/writes the view state (filters, sorting,
search, page) to query parameters via the History API, so Back/Forward restore the previous state.
Default values are omitted from the URL (clean URLs), an update pushes history by default
(`{ replace: true }` for live typing).

**Library (`/`, home page):** the library **is** the home page — after logging in you're greeted by
photos, not a hub. The old `/library` route redirects to `/` (preserving the query string),
so old bookmarks and links keep working. An empty catalog offers to
**upload the first photos**, an empty filter result offers to clear the filters. The main view
([`web/src/pages/LibraryPage.tsx`](web/src/pages/LibraryPage.tsx)) is a **virtualized, infinitely
scrolling thumbnail grid** with a filter panel. The grid is rendered by
[`react-virtuoso`](https://virtuoso.dev/) `VirtuosoGrid` (window scroll, responsive columns via
CSS `auto-fill`, mounts only the visible rows); reaching the end (`endReached`) fetches the next
page. The tiles ([`components/library/PhotoTile`](web/src/components/library/PhotoTile.tsx)) are
square, **lazy-loaded** (`loading="lazy"`, a fixed `aspect-ratio` → no layout shift) and lead to
the `/photos/{uid}` detail. The **filter bar** ([`components/library/FilterBar`](web/src/components/library/FilterBar.tsx))
is **designed for a calm default state**: the header holds only a prominent **search field**
(a visual anchor), **sorting** (newest/oldest/added/title/size/**rating**) and a
**Filters** button with a badge of the active-filter count. Advanced filters (capture date range, location (GPS),
private, camera, archive and **per-user rating filters** — minimum stars ≥1…≥5 and the picked/
rejected flag) live in an **expandable panel** (an inline collapse on desktop, an offcanvas on mobile via
`matchMedia`), so the default view isn't cluttered. Each active filter is shown as a **removable
chip** (the cross clears just that one filter) plus a single **Clear filters**. The controls have a
touch-friendly size (~44 px). **The whole view state (filters + sorting) lives in the URL**
via `useUrlState`, so Back/Forward restore the exact view and sharing the URL
reproduces it (the mapping is in [`lib/libraryView.ts`](web/src/lib/libraryView.ts), fields `min_rating`/`flag`).
The `showSearch`/`showSort` props (the search page) hide the query/sorting, while the chips, panel and Clear
filters keep working. Pagination is handled by the hook
[`usePhotoLibrary`](web/src/hooks/usePhotoLibrary.ts) — a thin wrapper over the shared
[`usePaginatedPhotos`](web/src/hooks/usePaginatedPhotos.ts) (accumulates pages, `loadMore`/`retry`,
reset + refetch when the query changes, cancels in-flight requests and ignores stale responses); it reads data
via [`services/photos.ts`](web/src/services/photos.ts) (`fetchPhotos` over `GET /api/v1/photos`,
`thumbUrl`). The view has an i18n loading skeleton, an empty state and an error state with „Zkusit znovu".

Next to the grid there is a **timeline** ([`components/library/TimelineScrubber`](web/src/components/library/TimelineScrubber.tsx))
— a thin fixed vertical data bar for quick jumps to a month. The hook
[`useTimeline`](web/src/hooks/useTimeline.ts) pulls the monthly date histogram via
`fetchTimeline` (`GET /api/v1/photos/timeline`, the same filters as the list, refetch on their change);
each month is a clickable tick positioned proportionally by `cumulative / total`, the month labels via
`lib/format` `formatMonth`. A click or drag jumps the grid to that month via `scrollToIndex`
with index `bucket.cumulative` — if the month lies **beyond** the loaded portion, the hook
[`useGridJump`](web/src/hooks/useGridJump.ts) first loads more pages before jumping. As the grid
scrolls (`rangeChanged`), the month containing the start of the visible range is highlighted. The bar is a
`position: fixed` overlay, so a loading/empty timeline renders nothing and doesn't shift the layout, and on
small widths it hides (`styles/app.css` `.kukatko-timeline*`); it shows only for the default newest
sort and outside selection mode.

**Search (`/search`):** the page
([`web/src/pages/SearchPage.tsx`](web/src/pages/SearchPage.tsx)) with a **prominent search
field** and a **mode toggle** (hybrid – default / fulltext / semantic). **Both the query and the mode live in the URL**
(`?q=…&mode=hybrid`) via `useUrlState`, so Back works and the URL is shareable; typing is
**debounced** (350 ms) before it is written to the URL and the query is fired. The results are rendered in the **same
virtualized grid** as the library and share the `FilterBar` (with the `showSearch`/`showSort` props hidden,
because the query and relevance-driven sorting belong to search). Data is read by the hook
[`usePhotoSearch`](web/src/hooks/usePhotoSearch.ts) (over `usePaginatedPhotos`) via `searchPhotos`
over `GET /api/v1/search`; an empty query is the `idle` state (no request, a prompt to the user). When the
**inference service is offline**, the backend returns `degraded: true` and the view shows a **non-blocking
notice** that semantic search is temporarily unavailable (the results fall back to fulltext). Above the photo
grid, [`GlobalSearchSections`](web/src/components/search/GlobalSearchSections.tsx) additionally renders
**compact cross-entity sections** — chips of matching albums, people and labels from the grouped `GET /search/global` —
so a text query surfaces non-photo entities too (see *Global search* above). The view has i18n
idle/loading/empty/error states. The URL ↔ state mapping is in
[`lib/searchView.ts`](web/src/lib/searchView.ts) (`SearchView`, `SEARCH_DEFAULTS`, `toMode`).

**Similar photos:** the reusable component
[`components/library/SimilarPhotos`](web/src/components/library/SimilarPhotos.tsx) (for the later
photo detail) loads `GET /api/v1/photos/{uid}/similar` via `fetchSimilar` and shows a **horizontally
scrollable strip** of similar-photo thumbnails, each linking to its own detail. It is empty-friendly
(a photo without an embedding → empty response → renders nothing), has a loading/error state and refetches on
a change of `uid`.

**Albums (`/albums`, `/albums/{uid}`):** [`AlbumsPage`](web/src/pages/AlbumsPage.tsx) is a responsive
grid of album cards ([`components/organize/AlbumTile`](web/src/components/organize/AlbumTile.tsx) —
cover, title, photo count), each leading to a detail. Editors/admins have a **New album** button
([`AlbumEditModal`](web/src/components/organize/AlbumEditModal.tsx) = create/rename, description,
private). [`AlbumDetailPage`](web/src/pages/AlbumDetailPage.tsx) shows a header (title, private badge)
with editor actions (edit/delete/**select**) above a photo grid
**scoped to the album** via the shared `GET /photos?album={uid}` (hook
[`useScopedPhotos`](web/src/hooks/useScopedPhotos.ts), the same `FilterBar` + URL state as the library).
An album is **always displayed chronologically** (the backend keeps the order, no manual reordering); selection
([`useSelection`](web/src/hooks/useSelection.ts) + [`SelectionBar`](web/src/components/organize/SelectionBar.tsx))
can remove photos from the album or set the cover.

**Labels (`/labels`, `/labels/{uid}`):** [`LabelsPage`](web/src/pages/LabelsPage.tsx) is a list of
labels with photo counts; editors create/rename/delete them
([`LabelEditModal`](web/src/components/organize/LabelEditModal.tsx) = name + priority). Clicking a
label opens [`LabelDetailPage`](web/src/pages/LabelDetailPage.tsx) — a photo grid scoped to the
label via `GET /photos?label={uid}` (again `useScopedPhotos` + `FilterBar` + URL state).

**Bulk edit from a selection:** the library has a **selection mode** for editors (`useSelection`):
the tiles switch to checkable (`PhotoTile` `selectable`), a sticky `SelectionBar` shows the count
and offers **Select all** (select-all-in-view via `useSelection.selectMany`) and **Bulk edit**
via [`BulkEditModal`](web/src/components/organize/BulkEditModal.tsx). The modal loads albums/labels and
in a single `POST /photos/bulk` ([`services/bulk.ts`](web/src/services/bulk.ts)) applies any
subset of operations — **add/remove album**, **add/remove label**, **set/clear description**,
**set/clear location**, **private**, **archive**, **favorite** (per-user); the set/clear pairs are
separate modes, the coordinates are validated client-side and at least one change is required. After applying,
a **per-photo result summary** is shown instead of the form (how many edited/skipped/failed + a list
of errors) from the response. The albums/labels API is called by [`services/organize.ts`](web/src/services/organize.ts)
(CRUD + memberships/attachments), `photos.ts` adds an `album`/`label` scope to `PhotoListParams`. The **Albums**
and **Labels** links are in the navbar under the **Browse** dropdown.

**Navbar — grouped menu:** to keep the top bar tidy, related destinations in
[`Layout`](web/src/components/Layout.tsx) are consolidated into `react-bootstrap` `NavDropdown` groups instead of
a flat list of links: **Home** is reachable via the brand link, the **Browse** dropdown groups
Library/Favorites/Albums/Labels/People/Places/Map (for all roles), **Search** and **Upload** stay
prominent top-level, the editor **Tools** dropdown (gated by `canWrite`) groups Duplicates + Trash and
the admin **Manage** dropdown (gated by `isAdmin`) groups Import + Maintenance + System. A group hides
entirely when all its items are hidden for the given role, and the parent menu lights up into an
active state when the current route is one of its children (including detail sub-paths). In the mobile
burger menu the groups expand inline with finger-friendly tap targets.

**Keyboard shortcuts:** both the grid and the photo detail are keyboard-operable via the shared hook
[`useKeyboardShortcuts`](web/src/hooks/useKeyboardShortcuts.ts) and a small registry
[`lib/shortcuts.ts`](web/src/lib/shortcuts.ts) (the source of truth for both the help and the behaviour). In the **grid**
(`LibraryPage`/`PhotoGrid` via [`useGridKeyboardNavigation`](web/src/hooks/useGridKeyboardNavigation.ts))
the arrows and `j`/`k`/`h`/`l` move a visible **focus highlight** between tiles — focus follows
virtualization and scrolls the tile into view (the row width is read from the live `grid-template`, so it
matches the responsive `auto-fill`); `Enter` opens the focused photo, `x` toggles its selection (turns on
selection mode, integrates with `useSelection`), `f` toggles the favorite (optimistically via `favoritePhoto`)
and `Escape` clears the selection first, then the focus. In the **detail** (`PhotoDetailPage`) `←`/`→` page to
the previous/next photo (preserving the order/scope of the source listing), `f` toggles the favorite (shares one
`useFavorite` with the header heart via a controlled `FavoriteToggle`) and `Escape` returns to
the source listing. The numeric rating keys (`0`–`5`, `p`/`r`) are handled separately by the ratings UI. The shortcuts
**never fire** while the user is typing into an input/textarea/`contenteditable` or a
**form modal** is open (`isFormModalOpen`), so neither typing nor dialogs are overridden. The help is opened by
`?` (Shift+/) anywhere or the **keyboard icon** in the navbar — the modal
[`KeyboardShortcutsHelp`](web/src/components/KeyboardShortcutsHelp.tsx) lists all shortcuts
grouped by context (Grid / Detail) and closes with Escape or the cross. i18n cs+en; the focus ring is
`.kukatko-tile-focused` in [`styles/app.css`](web/src/styles/app.css).

**Favorites (`/favorites` + the heart everywhere):** every tile in the library and the photo detail header
carry a **heart toggle** ([`FavoriteButton`](web/src/components/library/FavoriteButton.tsx) over the hook
[`useFavorite`](web/src/hooks/useFavorite.ts)) — an **optimistic** per-user toggle over
`PUT`/`DELETE /photos/{uid}/favorite` (`favoritePhoto` in `photos.ts`) with a **rollback** on error.
Favoriting is a personal action **available to viewers too** (no role gate), unlike bulk edit
(editor/admin only). The [`FavoritesPage`](web/src/pages/FavoritesPage.tsx) page (`/favorites`, a link
in the navbar) is the same grid/filters as the library, scoped `favorite=true`, so a photo can be removed from
favorites right on the spot. Every photo in the list/search/detail carries `is_favorite` for
the current user.

**Rating (stars + pick/reject everywhere):** next to the heart, the photo detail header and every
library/favorites tile carry a **compact rating control** — 0–5 stars
([`RatingStars`](web/src/components/library/RatingStars.tsx)) and a pick/reject flag
([`FlagControl`](web/src/components/library/FlagControl.tsx)) over the hook
[`useRating`](web/src/hooks/useRating.ts): an **optimistic** per-user write via
`PUT /photos/{uid}/rating` (`ratePhoto` in `photos.ts`, only the changed field is sent) with a **rollback**
on error, mirroring `useFavorite`. Clicking the current star/flag clears it (0 / `none`). Rating is
a personal action **available to viewers too** (no role gate). **Keyboard shortcuts** — on the photo detail
(a document listener) and on the **focused tile** in the grid, `0`–`5` set the star count, `p` = pick,
`r` = reject (a pure mapper [`lib/ratingHotkeys.ts`](web/src/lib/ratingHotkeys.ts)
`ratingHotkey`/`isTypingElement`); the shortcuts **don't work while typing** into an input/textarea/contenteditable.
A **rejected** tile is muted and carries a reject badge; the stars/flag overlay hides in selection mode
(like the heart). Filtering and sorting by rating goes through the `FilterBar` (see above). Vitest covers
the optimistic update + rollback (`useRating`/`RatingStars`), the hotkeys and the filter/sort in the `FilterBar`.

**Multiupload (`/upload`, editor/admin):** the page
([`web/src/pages/UploadPage.tsx`](web/src/pages/UploadPage.tsx)) for bulk uploading of photos/videos
including on **mobile**. [`components/upload/DropZone`](web/src/components/upload/DropZone.tsx) offers a
**drag-and-drop** zone and a file input (`multiple`, `accept="image/*,video/*"` → on mobile opens the
gallery) and a separate **Take photo** button (`capture="environment"` → the camera). The queue is driven by the hook
[`useUploadQueue`](web/src/hooks/useUploadQueue.ts): adding/removing files (dedup by
name+size+mtime), upload with a **concurrency cap** (`MAX_CONCURRENT_UPLOADS`, default 3),
**per-file progress** and a final state (uploaded / duplicate / failed) with a summary of counts, **retry** of
failed ones (individually and in bulk) and aborting running ones. Each file goes as its **own** `POST
/api/v1/upload` request via [`services/upload.ts`](web/src/services/upload.ts) (`uploadFile` over
**`XMLHttpRequest`** for the upload-progress events; the FormData is streamed, never wholly in RAM). The per-file
[`UploadItem`](web/src/components/upload/UploadItem.tsx) shows a progress bar, a status badge and a
**near-duplicate warning** from the API (non-blocking). Once finished it offers a link to the newly uploaded photos
in the library (`/?sort=added`). Everything is i18n (cs/en), touch-friendly. The **Upload** link in the navbar is
visible only to editors/admins.

**Map (`/map`):** the page ([`web/src/pages/MapPage.tsx`](web/src/pages/MapPage.tsx)) displays
geotagged photos as **clustered markers** over [mapy.com](https://mapy.com) tiles via
**[Leaflet](https://leafletjs.com/) + [Leaflet.markercluster](https://github.com/Leaflet/Leaflet.markercluster)**.
The tile layer points at a **backend proxy** (`/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}`), so the
**API key never leaves the server**; `{r}` becomes `@2x` on retina displays. The imperative
Leaflet logic is isolated in [`components/map/LeafletMap`](web/src/components/map/LeafletMap.tsx)
(a bridge of React props → Leaflet via effects: one-off setup, swapping the tile URL on a mapset change,
rebuilding the markers when the photos change). The **mandatory mapy.com controls** are always present:
attribution with the link „© Seznam.cz a.s. a další" (→ `mapy.com/copyright`) and a **clickable logo**
at the bottom left linking to `mapy.com`. Clicking a **cluster** zooms in (default markercluster), clicking a
**marker** opens a popup with a thumbnail ([`lib/mapPopup.ts`](web/src/lib/mapPopup.ts)) linking to
the photo detail (`/photos/{uid}`, SPA navigation). The **basemap switcher** (basic/tourist/aerial)
and the **filters** (date range, archive, private) are in
[`components/map/MapFilterBar`](web/src/components/map/MapFilterBar.tsx). **The state lives in the URL** via
`useUrlState` ([`lib/mapView.ts`](web/src/lib/mapView.ts) — mapset, viewport `lat`/`lng`/`z`,
filters), so Back/Forward and sharing the URL reproduce the map; **pan/zoom** writes the viewport without
a refetch, a **filter change** refetches the GeoJSON. Data is read by the hook
[`useMapPhotos`](web/src/hooks/useMapPhotos.ts) via `fetchMapPhotos` over `GET /api/v1/map/photos`
([`services/map.ts`](web/src/services/map.ts), a GeoJSON FeatureCollection). The view has i18n
loading/empty/error states and is responsive/touch. The **Map** link is in the navbar.

**Slideshow (`/slideshow`):** a fullscreen slideshow of photos
([`web/src/pages/SlideshowPage.tsx`](web/src/pages/SlideshowPage.tsx)) startable with the
**Slideshow** button from an **album** detail, a **label** detail and the (filtered) **library**. The target view carries
the **same sorting/filters** as the grid it is launched from — the launch link is built by
[`lib/slideshowView.ts`](web/src/lib/slideshowView.ts) (`slideshowHref`) from the current URL state,
so the scope (`?album=`/`?label=`) and the filters round-trip through the URL and **Back** returns to
the previous view. The page pages the catalog via the shared
[`usePaginatedPhotos`](web/src/hooks/usePaginatedPhotos.ts) (`fetchPhotos`), so **large sets aren't
loaded all at once**. The route lives **outside the layout shell** (no navbar), to take the whole viewport.

Playback is driven by the hook [`useSlideshow`](web/src/hooks/useSlideshow.ts): its own index + play/pause,
**auto-advance on a configurable interval** (setTimeout, a manual next/previous resets the countdown),
wrap-around at the end, and a **prefetch of further pages** (`PRELOAD_AHEAD` frames ahead via
`onLoadMore`) — at the very end with a next page it waits instead of looping. An empty set is a no-op.
The presentation layer [`components/slideshow/Slideshow`](web/src/components/slideshow/Slideshow.tsx)
shows the current photo in **preview size** (`fit_1920`), **preloads the neighbouring frames**
(`new Image()`), and carries the controls **previous / play-pause / next / fullscreen / settings /
close** plus a caption and the position `n / total`. The **keys** (←/→ navigation, spacebar play/pause, Esc
exits or leaves fullscreen, F fullscreen) and **touch** (horizontal swipe) work on mobile/tablet;
the Fullscreen API is feature-detected. The **transition effect** (crossfade / slide / no effect, CSS in
[`slideshow.css`](web/src/components/slideshow/slideshow.css)) and the **speed** are chosen in the settings
panel and **persisted to `localStorage`** via [`useSlideshowSettings`](web/src/hooks/useSlideshowSettings.ts)
+ [`lib/slideshowSettings.ts`](web/src/lib/slideshowSettings.ts) (sanitized on both read and write),
so the choice survives a reload and the next slideshow. Everything is i18n (cs/en).

Frontend development (a dev server proxying to the Go backend) and the standalone targets:

```bash
cd web && npm install     # one-off install of dependencies
npm run dev               # Vite dev server (proxies /healthz and /api → localhost:8080)

# or via the Makefile (from the repo root):
make web-build            # build the SPA into internal/web/static/dist
make web-lint             # ESLint (strict)
make web-fmt-check        # Prettier --check
make web-typecheck        # tsc -b --noEmit
make web-test             # Vitest (React Testing Library)
make web-fmt              # Prettier --write
```

The frontend targets are wired into the main gate: `make check` runs ESLint, Prettier `--check`,
`tsc` and Vitest. The SPA build runs in `make build` before `go build`.

## CI and release (packaging)

**CI** ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs on push/PR to `main`:

- **`check`** — Go 1.26 + Node 22 (+ golangci-lint v2.11.4), runs the quality gate `make check`
  (fmt-check + golangci-lint + Go unit tests + frontend ESLint/Prettier/tsc/Vitest) and on top of it
  also `make test-race` — the race detector is outside the gate, but in CI it runs on every push.
- **`integration`** — `make test-integration` against a **service container
  `pgvector/pgvector:pg17`**; a setup step creates the `vector` and `unaccent` extensions,
  `KUKATKO_TEST_DATABASE_URL` points at an ephemeral CI database (no secrets in the log).

Cache: Go module/build cache (`actions/setup-go`) and npm (`actions/setup-node`,
`web/package-lock.json`).

**Release** ([`.github/workflows/release.yml`](.github/workflows/release.yml)) runs on a
`v*.*.*` tag and launches **goreleaser** ([`.goreleaser.yaml`](.goreleaser.yaml)): a `CGO_ENABLED=0` build
for **arm64** (Raspberry Pi, production) and **amd64** (dev), version/commit via ldflags into
`internal/version`, the frontend is built in a before-hook, so the embedded SPA is up to date. Local
verification of the whole pipeline: `goreleaser release --snapshot --clean`.

**The .deb package** (nfpm in goreleaser) installs:

- the binary into `/usr/bin/kukatko`,
- a **systemd unit** [`deb/kukatko.service`](deb/kukatko.service) into `/lib/systemd/system/`
  (`kukatko serve`, `Restart=always`, `EnvironmentFile=/etc/kukatko/kukatko.env`, a dedicated
  `kukatko` user),
- an env-file template [`deb/kukatko.env`](deb/kukatko.env) into `/etc/kukatko/kukatko.env` as a dpkg
  **conffile** (`config|noreplace` — operator edits survive an upgrade),
- postinstall ([`deb/postinstall.sh`](deb/postinstall.sh)) creates the system user and the data
  directories `/var/lib/kukatko/{originals,cache}`.

Apt dependencies: `libimage-exiftool-perl` (exiftool), `libheif-examples | libheif-bin`
(`heif-convert`), `dcraw` (RAW preview), `postgresql-client`, `ca-certificates`. **No texlive**
(the photo book is out of scope).

More in [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) (layout, make targets, the quality gate).
