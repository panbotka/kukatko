# Restore / disaster recovery runbook

This is the counterpart to the [backup](ARCHITECTURE.md#14-konfigurace-build-a-provoz-s15)
subsystem: how to rebuild a Kukátko instance from an S3-compatible backup. A backup writes a
`pg_dump` (custom/compressed format) to `db/kukatko-<timestamp>.dump` in the backup bucket and
incrementally copies the originals in beside it. Restore reverses both, then verifies integrity.

## There is no versioning. The backup bucket is all you have.

**Object storage is not a backup.** Read this before relying on anything below.

- **Kukátko does not lean on object versioning, because there is none to lean on.** Cloudflare R2
  has no native object versioning that could be confirmed in its documentation, so an original
  deleted or overwritten in the primary bucket is simply gone. There is no previous version, no
  undelete, no grace window.
- **The second bucket carries the whole of the protection.** `backup.s3.*` names a bucket that
  shares nothing with `storage.r2.*` — its own endpoint, region, bucket and credentials, and it may
  sit at an entirely different provider. That copy is the one thing standing between an accidental
  `DELETE` (or a leaked primary API token) and a lost photo.
- **The copy is additive, always.** Deleting an original from the primary bucket never propagates to
  the backup bucket, and `backup.retention` prunes **database dumps only** — it never expires an
  original. A backup run only ever adds objects to the backup bucket.
- **So keep the backup bucket's credentials separate.** If one token can write both buckets, you
  have a single bucket with extra steps. Give the backup bucket its own credentials, ideally in a
  different account or with a different provider.

> **The database restore is destructive.** `kukatko restore db` overwrites the entire target
> database. Run it only against an empty or throw-away database during recovery, and only with the
> explicit `--yes` flag. Do **not** run it against a live, in-use database.

All restore commands read the same `backup.s3.*` configuration as backup (so a machine that can
back up can also restore), plus `database.url` (the **target** to restore into) and — on the `fs`
backend — `storage.originals_path` (where originals are written). Secrets travel via environment
variables and are never logged or placed on the process argument list.

Which path you take for the originals depends on `storage.backend`:

| `storage.backend` | Originals live in | Restore them with |
| --- | --- | --- |
| `fs` | `storage.originals_path` on local disk | `kukatko restore originals` (step 5 below) |
| `r2` | a private bucket (`storage.r2.bucket`) | a bucket-to-bucket copy into a fresh bucket ([below](#restoring-the-originals-into-a-fresh-bucket)) |

The database dump is restored the same way either way.

## Prerequisites

- **`pg_restore`** and **`pg_dump`** on `PATH` — Debian/Ubuntu package `postgresql-client`.
- An empty target PostgreSQL 17 database with the `vector` (pgvector) and `unaccent` extensions
  available. On the shared instance these are pre-provisioned; on a fresh server install them once:
  `CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS unaccent;` (migration `0001`
  also attempts this, so a superuser DSN can skip it).
- Read access to the backup bucket (`backup.s3.{endpoint,bucket,access_key,secret_key,region,
  path_style}`).

## Fresh-machine recovery (end to end)

### 1. Install the binary

From the `.deb` (recommended) — this also creates the `kukatko` service user and the data
directories `/var/lib/kukatko/{originals,cache}`:

```bash
sudo apt install ./kukatko_<version>_arm64.deb
```

Or drop the single static binary on `PATH` and create `storage.originals_path` /
`storage.cache_path` yourself.

### 2. Point at the bucket and the target database

Edit `/etc/kukatko/kukatko.env` (deb install) or export the variables before running the CLI. The
minimum for a restore:

```bash
# Target database to restore INTO (must be reachable and, ideally, empty):
export KUKATKO_DATABASE_URL='postgres://kukatko:PASSWORD@localhost:5432/kukatko?sslmode=disable'

# Where restored originals are written:
export KUKATKO_STORAGE_ORIGINALS_PATH=/var/lib/kukatko/originals

# Backup bucket (the restore source) — same keys the backup task uses:
export KUKATKO_BACKUP_S3_ENDPOINT=https://s3.eu-central-1.amazonaws.com
export KUKATKO_BACKUP_S3_REGION=eu-central-1
export KUKATKO_BACKUP_S3_BUCKET=kukatko-backups
export KUKATKO_BACKUP_S3_ACCESS_KEY=...      # secret — keep out of shell history
export KUKATKO_BACKUP_S3_SECRET_KEY=...      # secret
# export KUKATKO_BACKUP_S3_PATH_STYLE=true   # MinIO and most self-hosted S3
```

If you use a config file instead, pass `--config /path/to/config.yaml` to every command.

### 3. Confirm the backup is reachable and pick a dump

```bash
kukatko restore list
# 3 dump(s) available (newest first):
#   db/kukatko-20260629T020000Z.dump  (184320991 bytes)
#   db/kukatko-20260628T020000Z.dump  (184211004 bytes)
#   db/kukatko-20260627T020000Z.dump  (183998120 bytes)
```

### 4. Restore the database (destructive)

Restore the most recent dump (the default), re-applying migrations afterwards:

```bash
kukatko restore db --yes
```

Or restore a specific dump:

```bash
kukatko restore db --dump db/kukatko-20260628T020000Z.dump --yes
```

What happens: the dump is streamed straight from S3 into `pg_restore`
(`--clean --if-exists --single-transaction --no-owner --no-privileges`), so the restore runs in a
single transaction and rolls back cleanly if interrupted. Migrations are then re-applied
(idempotent — normally a no-op, but it guarantees the schema matches the binary).

> The connection password is passed to `pg_restore` via `PGPASSWORD`, never on the command line.

### 5. Restore the originals (`storage.backend: fs`)

On the `r2` backend skip this step and follow
[Restoring the originals into a fresh bucket](#restoring-the-originals-into-a-fresh-bucket)
instead — `kukatko restore originals` writes to local disk, which is exactly what the bucket
backend exists to avoid.

```bash
kukatko restore originals
# originals restore complete: downloaded=48213 skipped=0
```

This downloads every original in the bucket that is not already on disk at the same key and size,
writing each atomically (temp file under `.tmp/` then rename). **It is resumable**: if it is
interrupted (network drop, Ctrl-C), just run it again — completed files are skipped and only the
remainder is fetched. Database dumps under `db/` are never downloaded as originals.

### 6. Verify integrity (`storage.backend: fs`)

```bash
kukatko restore verify
# photos in DB:       48201
# files in DB:        48213
# originals on disk:  48213
# integrity: OK (catalogue and originals are consistent)
```

If there are mismatches the report lists a bounded sample of each:

- **missing on disk** — a catalogued `photo_files.file_path` with no file on disk. Re-run
  `kukatko restore originals` (the object may have failed to download), or check the original was
  actually backed up.
- **extra on disk** — a file on disk with no catalogue row. Usually harmless (e.g. an original whose
  photo row predates the chosen DB dump); confirm before deleting anything.

`kukatko restore verify` reconciles the catalogue against originals **on local disk**, so it only
means anything on the `fs` backend. On `r2`, compare object counts as shown in the next section.

### 7. Start the service

```bash
sudo systemctl start kukatko        # deb install
# or: kukatko serve
```

Thumbnails and other derived cache are **regenerated lazily** on first access, so the cache
directory does not need to be restored. Embeddings and detected faces are part of the database dump
and are restored with it.

## Restoring the originals into a fresh bucket

This is the recovery path when `storage.backend: r2` — the primary bucket was deleted, corrupted, or
its contents were wiped by a stray API token. The backup bucket holds every original that ever
existed under its original key (`YYYY/MM/<name>`, identical to the primary layout) plus the database
dumps under `db/`. Recovery is: create an empty bucket, copy the originals across, point Kukátko at
it, restore the database.

The copy is a plain bucket-to-bucket transfer, and the two buckets may be at different providers, so
do it with [`rclone`](https://rclone.org/) rather than from inside Kukátko. The steps below are
exact; substitute your own names.

### 1. Create the fresh primary bucket

Create it **private** (no public access) at the provider of your choice. On Cloudflare R2 the bucket
and the media Worker's binding are defined by Terraform in the infra repo (root module
`cloudflare-r2/`) — apply it there rather than clicking in the dashboard, or the Worker will 404 on
every object.

### 2. Configure rclone with both buckets

Write `~/.config/rclone/rclone.conf` (mode `0600`; these are credentials):

```ini
[backup]
type = s3
provider = Other
endpoint = https://s3.eu-central-1.amazonaws.com
region = eu-central-1
access_key_id = <backup.s3.access_key>
secret_access_key = <backup.s3.secret_key>

[primary]
type = s3
provider = Cloudflare
endpoint = https://<accountid>.r2.cloudflarestorage.com
region = auto
access_key_id = <storage.r2.access_key>
secret_access_key = <storage.r2.secret_key>
```

### 3. Copy the originals across, excluding the dumps

`db/` holds database dumps, not originals — it must not land in the primary bucket:

```bash
rclone copy backup:kukatko-backups primary:kukatko-originals-new \
  --exclude '/db/**' \
  --transfers 16 --checkers 32 --progress
```

`rclone copy` is additive and resumable: it never deletes at the destination, and re-running it
transfers only what is missing. **Never use `rclone sync`** here — `sync` deletes destination objects
that are absent from the source, which is the exact failure this whole design exists to prevent.

Verify the two sides agree before you rely on the result:

```bash
rclone check backup:kukatko-backups primary:kukatko-originals-new --exclude '/db/**' --size-only
rclone size primary:kukatko-originals-new
```

### 4. Point Kukátko at the fresh bucket

```bash
export KUKATKO_STORAGE_BACKEND=r2
export KUKATKO_STORAGE_R2_BUCKET=kukatko-originals-new
# endpoint / region / access_key / secret_key / media_base_url / url_signing_secret unchanged
```

The object key is `photo_files.file_path` verbatim, so nothing in the database refers to the bucket
name — repointing is enough, and no migration of keys is needed.

### 5. Restore the database and start up

```bash
kukatko restore list                 # confirm the backup bucket is reachable
kukatko restore db --yes             # destructive; see step 4 of the runbook above
sudo systemctl start kukatko
```

Then confirm the catalogue and the bucket agree: `rclone size primary:kukatko-originals-new` should
report as many objects as `SELECT count(*) FROM photo_files;` returns.

### 6. Re-check the backup wiring

`backup.s3.bucket` must still name the **backup** bucket, never the fresh primary one — Kukátko
refuses to start a backup whose destination is the primary bucket (`errBackupSameBucket`), but a
typo pointing it at a *third* bucket would silently start a new, empty backup history. Run one
backup by hand and confirm it copies zero originals (they are all present already) and writes one
new dump:

```bash
kukatko backup
# backup complete: dump=db/kukatko-... originals uploaded=0 skipped=48213 dumps pruned=0
```

## Metadata sidecars — the format

Everything a user builds in Kukátko — titles, descriptions, who is in the photo, which album it
belongs to, the rating — otherwise exists in exactly one place: Postgres. The backup above is good,
but it is a **single mechanism**, and a backup that has been quietly failing for three months is
discovered on the day you need it.

A **sidecar** is a second mechanism of a different kind. It is a YAML file per photo, written next
to the originals in storage, holding that photo's metadata and curation in plain text any tool can
read. The curation lives *next to the photo it describes*, on the same storage that holds the
original — so the index can be thrown away and rebuilt from the originals plus the sidecars.

This section is the format's reference. It is here, rather than in an architecture document,
because this is where you will look when the database is gone.

### Where they live

Sidecars form a **parallel tree** under the `sidecars/` prefix of the storage root, mirroring the
originals' layout:

| Original | Sidecar |
| --- | --- |
| `2024/05/IMG_1234.jpg` | `sidecars/2024/05/IMG_1234.jpg.yml` |
| `2019/12/VID_0001.mp4` | `sidecars/2019/12/VID_0001.mp4.yml` |

The mapping is total and reversible: strip the `sidecars/` prefix and the trailing `.yml` and you
have the original's storage key. Three consequences worth knowing:

- **The extension is appended, not replaced.** `IMG_1.jpg` and `IMG_1.png` are two different photos
  and get two different sidecars. Replacing the extension would collide them onto one file, and each
  would silently overwrite the other's curation.
- **A parallel tree, not a file beside the original.** The originals tree stays purely media, so the
  importers and integrity scanners that walk it never have to learn to ignore a second kind of file.
  The whole export is one prefix, so it can be listed, `rsync`ed or discarded as a unit.
- **Same storage as the originals.** It works identically on the `fs` and `r2` backends, and the
  backup's originals sync copies the sidecars into the backup bucket along with the photos — the
  curation travels with the photos, which is the point. The `storage migrate-to-r2` move (see
  `docs/OPERATIONS.md`) carries them the same way: each sidecar is uploaded and verified into the
  destination alongside its original, and — with `--delete-local` — its local copy is removed with
  the original. Reclaiming the local disk after a migration therefore never strands a sidecar.

### The file

Every sidecar opens with a header comment explaining what it is and what is deliberately missing,
then the YAML document. Comments are ignored by any parser, so the file round-trips.

```yaml
# Kukátko metadata sidecar.
#
# This file holds one photo's metadata and curation: what it is, when and where it
# was taken, who is in it, which albums and labels it carries, how it was rated.
# ...
# NOT in this file, deliberately, and please do not "fix" it: the image embedding
# and the face vectors. ...
version: 1
generated_at: 2026-07-17T12:00:00Z
identity:
    uid: pht000000000001
    sha256: abababab...
    file_name: IMG_1234.jpg
    file_path: 2024/05/IMG_1234.jpg
    original_name: DSC_0001.JPG
    media_type: image
    uploaded_by: pan.botka
    external:
        photoprism_uid: ppuid123
descriptive:
    title: Svatba
    description: Obřad na zahradě
    keywords: svatba,zahrada,rodina
    artist: Jan Novák
temporal:
    taken_at: 2024-05-17T14:30:00Z
    taken_at_source: exif
spatial:
    lat: 50.0755
    lng: 14.4378
    source: exif
    place:
        country: Česko
        city: Praha
        geocoded_at: 2024-05-18T09:00:00Z
technical:
    camera_make: NIKON CORPORATION
    camera_model: NIKON D750
    iso: 400
    aperture: 1.8
    exposure: 1/250
    width: 6016
    height: 4016
curation:
    albums:
        - uid: alb001
          slug: svatba
          title: Svatba
          type: album
    labels:
        - uid: lbl001
          name: Portrét
          source: ai
          uncertainty: 12
    people:
        - marker_uid: mrk001
          subject_uid: sub001
          name: Jana Nováková
          subject_type: person
          type: face
          box: {x: 0.25, y: 0.1, w: 0.2, h: 0.3}
          score: 88
    favorites:
        - user: pan.botka
          user_uid: usr001
    ratings:
        - user: pan.botka
          stars: 4
          flag: pick
edit:
    crop: {x: 0.1, y: 0.2, w: 0.6, h: 0.5}
    rotation: 90
```

Every group and key is omitted when empty, so a photo nobody has touched yields a short file.

### The groups

| Group | Holds | Notes |
| --- | --- | --- |
| `version` | Schema version (currently `1`) | First key, so a reader can dispatch before parsing. A reader that meets a version it does not know should **refuse the file**, not guess. |
| `generated_at` | When the file was written | Provenance: it tells you how current the file is, the first thing you want to know when rebuilding. |
| `identity` | `uid`, `sha256`, `file_name`, `file_path`, `original_name`, `media_type`, `uploaded_by`, `external` | `sha256` is the durable link to the original: paths move, content does not. `external` carries `photoprism_uid` / `photoprism_file_hash` (PhotoPrism's SHA1, not Kukátko's SHA256) / `photosorter_uid` so a re-import recognises what it already has. |
| `descriptive` | `title`, `description`, `notes`, `ai_note`, `subject`, `keywords`, `artist`, `copyright`, `license` | `keywords` are the IPTC keywords verbatim, comma-separated. They are **not** labels — labels are Kukátko's own taxonomy and live under `curation`. |
| `temporal` | `taken_at`, `taken_at_source`, `estimated`, `note` | `estimated: true` marks the date as a guess and `note` records what it rests on ("kolem roku 1950"). A photo with no `taken_at` may still be estimated, the note then carrying the whole meaning. |
| `spatial` | `lat`, `lng`, `altitude`, `source`, `place` | `source` is `exif` / `manual` / `estimate` / empty. **It matters:** an inferred location must never be rebuilt as a measured one. `source: manual` with no coordinates is not a contradiction but a **tombstone** — the user deleted the location on purpose, and a rebuild must not hand it back. `place` is the cached reverse-geocode (geocoding costs credits; recording it means a rebuild does not pay twice). |
| `technical` | Camera, lens, exposure, dimensions, file, `video` | Mostly recomputable from the original, recorded anyway: it is small, and a sidecar readable on its own is worth more than the saved bytes. `video` is present only for videos and live photos. |
| `curation` | `albums`, `labels`, `people`, `favorites`, `ratings`, `private`, `archived_at`, `stack` | **The group that exists nowhere else.** Everything above can in the last resort be re-derived from the original; none of this can. |
| `edit` | `crop`, `rotation`, `brightness`, `contrast` | The non-destructive edit. Originals are never modified, so a lost edit is a visible change silently reverted. Omitted when the edit is a no-op. Crop is normalised `0..1`; brightness/contrast are CSS-filter-style, meaningful in `[-1, 1]`, `0` neutral. |

Details inside `curation` that a rebuild must not drop:

- **`people[].box`** is the face rectangle in `0..1` **display space** (EXIF-aware — relative to the
  upright image the user sees, not the raw stored pixels). *A marker without its box cannot be
  rebuilt*, so the box is written even for a face nobody has named.
- **`people[]` includes the unnamed and the rejected.** An unnamed face is work in progress;
  `invalid: true` is a decision the user made. Writing only the named ones loses both — and a
  rebuild would resurrect every face the user already said no to.
- **`labels[].uncertainty`** is the classifier's uncertainty as an integer percentage where **`0`
  means certain** — not a confidence. It is recorded as stored rather than inverted, because
  inverting it here would invent precision. A manual attachment is always `0`. `source` is
  `manual` / `ai` / `import`, and it matters: a hand-attached label is a fact, an AI one is a
  suggestion.
- **`favorites` and `ratings` are per-user**, so every user's are recorded — these are personal in
  Kukátko, not a single value on the photo. `user` (the username) is the half that survives a
  rebuild; `user_uid` is this database's identifier and will not mean anything in a new one.
- **`archived_at`** marks a photo in the trash awaiting purge. **A rebuild should honour it** — a
  photo the user deleted, silently resurrected, is the worst kind of restore bug.
- **`uid`/`slug` on albums and labels, `subject_uid` on people** are this database's identifiers. A
  rebuild should match on the **name/title/slug**; UIDs are regenerated.

### What is deliberately NOT written

**The image embedding and the face vectors.** This is not an oversight — please do not "fix" it:

- They are large and binary. They would dwarf everything else and make the file unreadable, which
  defeats the purpose of a format a human can open cold.
- They are **cheap to recompute from the original**. That is exactly what the `image_embed` and
  `face_detect` backfill jobs are for (`POST /api/v1/process/embeddings`, `/process/faces`).

What cannot be recomputed is what a **person decided**, and that is all here. The same reasoning
excludes the raw EXIF blob: it is in the original, and `kukatko` re-reads it via the `metadata` job.

The file header says all of this, so the next person to open one is told before they get ideas.

### When they are written

- **On every change.** A `sidecar` job is enqueued whenever a photo's metadata or curation changes
  (an edit, a rating, a favorite, an album, a label, a face assignment, a stack, a bulk edit) and
  when a photo is first catalogued. The job re-reads the photo and rewrites the file.
- **Debounced.** The queue's per-photo dedup index keeps at most one queued `sidecar` job per photo
  and the job waits ~5s before running, so a burst of edits collapses into one file write. A
  500-photo bulk edit enqueues 500 small rows and returns; the worker writes the files.
- **Atomically.** The bytes are streamed to a temp file and renamed (`fs`), or verified and removed
  on mismatch (`r2`). A reader never sees a half-written sidecar — a truncated YAML document is not
  a slightly worse sidecar, it is an unparseable one.
- **Idempotently.** The handler writes the current truth, not a delta. Running it twice writes the
  same bytes; running it late writes the current state. That is why a coalesced or lost job costs
  nothing but staleness.
- **Never at the user's expense.** A failed write is logged and retried by the queue; it never fails
  the edit. The edit is safely in Postgres either way.
- **Removed on purge.** Permanently deleting a photo deletes its sidecar, because one left behind is
  precisely the file a rebuild reads to resurrect a photo the user deleted.

### Backfilling the whole library

Run this before anything risky — a migration, an upgrade, a restore rehearsal:

```bash
kukatko sidecar backfill          # every photo whose sidecar is missing or stale
kukatko sidecar backfill --all    # forced full re-run over every non-archived photo
```

Or, admin-only, over HTTP: `POST /api/v1/process/sidecars` (`?all=true` for the full re-run).

Both **enqueue** jobs; the running server's worker writes the files, so the command returns as soon
as the work is scheduled and prints the count. It is idempotent — a run over a library whose
sidecars are current schedules nothing — and resumable, since an interrupted run just leaves the
rest pending.

Use `--all` when curation changed without the photo row changing (an album membership, a label) and
an enqueue was lost: the default predicate keys on `photos.updated_at`, which those do not touch.

### Switching it off

`sidecar.enabled: false` (or `KUKATKO_SIDECAR_ENABLED=false`) stops all of it: nothing is written or
deleted, no job is enqueued, and `/process/sidecars` answers 503. Sidecars already in storage are
**left exactly as they are** — turning the export off is not a request to destroy what it already
wrote, and a stale sidecar is worth more than none. See `config.example.yaml`.

### Known limitations

- **Rebuilding the catalogue from sidecars is not implemented yet.** This is the export half. The
  format is designed to be sufficient for a future `kukatko restore --from-sidecars`, and the
  round-trip test in `internal/sidecarexport` is what pins that sufficiency — it serialises a
  fully-populated photo, parses it back and asserts every field survives. That test is what the
  importer will be built against.
- **The backup's incremental sync compares key and size only.** Originals are immutable, so that was
  always safe; sidecars are not. A sidecar edited to the *same byte length* (`rating: 3` →
  `rating: 4`) may be skipped and not re-uploaded to the backup bucket, leaving the backup's copy
  stale. The sidecars on the **primary** storage are always current — they are the authoritative
  copy — and a `--all` backfill after a size-neutral edit forces a rewrite. Fixing this properly
  means comparing mtime or ETag in `internal/backup`.
- **Sidecars are excluded from the integrity scan.** `kukatko maintenance scan` defines an orphan as
  "on disk but not in the catalogue", which every sidecar is by construction. They are filtered out,
  so the scan neither counts them as orphans nor offers them to `repair --import-orphans`.

## Admin API (read-only)

A running server exposes two **admin-only**, read-only restore endpoints under `/api/v1` (useful to
check backups and integrity without shell access):

- `GET /restore/dumps` → `{ "dumps": [ { "key": ..., "size": ... }, ... ] }`
- `POST /restore/verify` → the same integrity report as `kukatko restore verify`.

The destructive database restore is **not** exposed over HTTP — restoring the database underneath a
running server would drop the tables it is using. Use the `kukatko restore db` command with the
server stopped.

## Notes

- Restore is safe to interrupt and re-run; only `restore db` mutates the database, and it does so in
  one transaction.
- The bucket layout is identical to backup: dumps under `db/`, originals under their
  `YYYY/MM/<name>` keys.
- To restore into a different database (e.g. staging), just point `KUKATKO_DATABASE_URL` at it.
