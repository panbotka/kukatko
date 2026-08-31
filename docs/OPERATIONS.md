# Operations: CLI, configuration, build, and CI

A descriptive reference overview of commands, configuration keys, `make` targets, and packaging.
**These are not rules** — the rules live in [`CLAUDE.md`](../CLAUDE.md). Write a new
configuration key both here **and** into `config.example.yaml`.

## CLI

<!-- BODY CLI -->
- **CLI:** `kukatko serve` (loads the config, **runs migrations**, **bootstraps the admin**, starts
  the hourly cleanup of expired sessions, the **background worker** (`internal/worker`) that
  processes the job queue, and the **scheduled trash cleanup** (`internal/trash` `RunPurge`, every 6 h —
  permanently deletes photos archived longer than `trash.retention_days`, default **365 days (1 year)**;
  retention ≤ 0 disables it),
  the **scheduled S3 backup** (`internal/backup` `RunSchedule` on `backup.schedule`; only if
  `backup.s3.{endpoint,bucket}` is configured), and the **optional Wake-on-LAN auto-wake of the box**
  (`internal/wake` `Run`, every minute; only if `embedding.wake.enabled`, otherwise fully inert),
  then listens on `web.host:web.port`, default
  `0.0.0.0:8080`; `GET /healthz` → 200 JSON `{"status":"ok","version":{…}}`, **`GET /metrics`**
  Prometheus (outside `/api/v1`, unauthenticated; only when `metrics.enabled`), the auth/admin API
  under `/api/v1` — see below, and all other paths are served by the **embedded SPA** with a fallback to
  `index.html`; `serve` additionally sets up **structured logging** (`obs.Setup`, JSON slog to
  stderr, level `log.level`) and — when `metrics.enabled` — builds the `metrics.Registry`, registers
  the DB-pool + job-queue-depth + geocode-credit-budget collectors, and inserts the request-metrics + access-log middleware via
  `server.WithMiddleware`/`WithMetricsHandler`; **note on the first start after an upgrade to
  migration 0047**: it builds a second HNSW graph over the unassigned faces and that is
  minutes of work on a large library — a couple per 50 000 faces on Pi-class hardware,
  once, before the server accepts anything. Run `kukatko migrate` ahead of the deploy if that
  downtime matters; see `docs/PERF.md` §3),
  `kukatko migrate` (runs pending migrations on their own and exits),
  **`kukatko import dir <path>`** (uploads a **directory from disk** — `internal/dirimport`; see below),
  `kukatko backup` (synchronous one-off **S3 backup** — `internal/backup`; pg_dump + sync of
  originals + retention; needs `backup.s3.{endpoint,bucket}`, otherwise `errBackupNotConfigured`;
  under `storage.backend: r2` the originals are **copied bucket→bucket server-side** and a backup into
  the **same** bucket the library lives in fails with `errBackupSameBucket`;
  for ops/cron without a running server),
  **`kukatko restore`** (the restore/disaster-recovery tree — `internal/backup`; shares `backup.s3.*`,
  otherwise `errRestoreNotConfigured`; for ops/cron without a running server): `restore list` (dumps in
  the bucket), `restore db [--dump KEY] [--yes] [--verify]` (**destructive** DB restore via
  `pg_restore` streamed from S3 + idempotent re-migration; without `--yes` → `errRestoreNotConfirmed`),
  `restore originals` (downloads missing originals, skips by key+size, resumable),
  `restore verify` (integrity report of photos in the DB vs originals on disk); runbook
  [`docs/RESTORE.md`](RESTORE.md),
  **`kukatko maintenance`** (library integrity check & repair — `internal/maintenance`; for
  ops/cron without a running server, applies migrations and builds a service shared with the admin API):
  `maintenance scan` (read-only integrity report — disk↔DB drift + missing derived data),
  **`maintenance reset`** (the **guarded library wipe** — `internal/reset`; dry run by default, `--execute` +
  a typed database name — and, on a bucket-backed store, a typed bucket name — to delete, `--force` for a
  non-interactive run, `--orphan-sweep` for the leftovers the catalogue never referenced;
  accounts/announcement/instance settings/audit trail/migrations are never touched; see below) and
  **`maintenance nameless-subjects`** (reports — and with `--apply --undo-file` detaches — subjects whose name
  identifies nobody, the importer-minted catch-all; dry run by default, reversible via `--undo`; see below) and
  `maintenance repair` with the flags
  `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans`/`--dimensions`/`--face-markers`/
  `--sideways-faces`
  (each opt-in; thumbnails/phashes enqueue `thumbnail` jobs drained by a running server's worker,
  embeddings/faces backfill, orphan import synchronously via the upload pipeline; `--dimensions` writes the
  catalogue directly — it rewrites the pixel dimensions of quarter-turned photos whose columns hold the
  **displayed** frame instead of the stored one, taking that correction from the file's own EXIF document rather
  than from its provenance, and then corrects the face boxes normalized against the same transposed frame.
  Those boxes are **not** all in one coordinate space (whether the embeddings sidecar auto-rotated before
  detecting has varied), so this half decides **per row from the photo's own face markers**: a quarter turn for a
  box that is really in the raw frame, a per-axis rescale for one the sidecar had already rotated, or only the
  cached frame when the box itself is right. A row the markers cannot place is left completely untouched
  (`left alone=N` in the output) and stays findable, so a later run picks it up once the photo carries a marker
  to reconcile it against. Its **dry run is `maintenance scan`**, whose `transposed dims` and `transposed faces`
  lines and samples are exactly what it would rewrite, and every write is guarded on the state it replaces, so a
  re-run is a no-op, the photo pair's swap is undone by swapping back and no face box is ever moved twice.
  It does **not** rewrite the metadata sidecars, which carry a copy of the dimensions — follow it with
  `kukatko sidecar backfill` if the corrected pair should reach them too.
  `--face-markers` likewise writes the catalogue directly: a marker describes one region, so at most one
  detected face may claim it, and this clears the surplus links non-exclusive matching left behind (which
  render one person twice on a photo). Its **dry run is `maintenance scan`** too — the `dup face markers`
  line counts the markers more than one face row still caches, sampled by marker uid — and it only ever nulls
  `marker_uid`/`subject_uid`/`subject_name` on the losing faces: no face row and no marker is deleted, and a
  marker with a single face link is left alone, so genuinely duplicated markers from an import are not swept
  up by it. A re-run is a no-op.
  `--sideways-faces` is the third face repair and the only one that re-runs detection instead of moving
  numbers: the embeddings sidecar reads no EXIF, so a quarter-turned photo sent as it lies on disk was detected
  **on its side** — the boxes it returned are in a frame nobody displays and the faces it missed on a turned
  picture are simply not there, which no coordinate math recovers. It clears the `face_detections` record of
  every quarter-turned photo whose recorded detection frame is not the displayed one and enqueues `face_detect`
  for it, printing `sideways faces re-detected=N`; the jobs **wait in the queue while the sidecar's box sleeps**,
  so N is photos scheduled, not photos re-detected. Its **dry run is `maintenance scan`** (the `sideways faces`
  line and samples), the photos' existing face rows are kept until the new detection replaces them wholesale,
  and a photo re-detected upright drops out of the finding for good, so a re-run is a no-op;
  a no-op without any flag;
  the **retention purge of old audit logs** is separate, only via HTTP/UI, not the CLI — the maintainer calls
  `POST /api/v1/maintenance/audit/purge` `{older_than_days}` (`internal/maintenanceapi`), which deletes audit
  entries older than `now − older_than_days` and **audits itself** (`audit.purge`, so that deleting the trail
  stays traceable); the admin UI has a „Vymazat audit log" card on the Údržba page with presets
  (3/6 months, 1/2 years) or a custom number of days plus a confirmation),
  **`kukatko sidecar`** (metadata sidecars — `internal/sidecarjob`; the terminal entry point into the export
  that makes curation data independent of the database): `sidecar backfill` enqueues a `sidecar` job for
  every photo with a **missing or stale** sidecar, `--all` forces a full re-run over every
  unarchived photo (this catches up changes outside the photo's own row — album membership, a label).
  It only **enqueues**; the files are written by a running server's worker (the same queue, same handler, same
  dedup as for live edits, so the backfill cannot race the user), which is why it prints the number of
  scheduled jobs. Idempotent — over a library with up-to-date sidecars it schedules zero — so it
  can be run from cron and, above all, **before any risky operation** (a migration, upgrade, restore drill),
  which is exactly the moment a person is at the terminal. When `sidecar.enabled: false`, the command
  **fails** instead of a silent “0 scheduled”. The full format is in [`docs/RESTORE.md`](RESTORE.md),
  the HTTP counterpart is `POST /api/v1/process/sidecars`,
  **`kukatko storage`** (operations over the storage of originals — `internal/storagemigrate`):
  `storage migrate-to-r2` (a one-off **resumable** move of the library to R2, see below),
  **`kukatko ctl`** (a remote client over the HTTP API of a running instance — `internal/ctl`; the only subcommand
  that **touches neither the DB nor disk**, see below),
  `kukatko version` (version + commit). The persistent `--config <path>` flag selects the YAML config.
  `server.New(addr, server.WithAPI(register))` mounts the route groups under `/api/v1`.

### `kukatko import dir <path>`

Walks a directory on disk (recursively) and uploads every media file into the library **through the same
pipeline as a browser upload** (`internal/ingest`): stream + SHA256, metadata, the original into
`YYYY/MM`, thumbnails, `image_embed`/`face_detect` jobs onto the queue. The source directory is **read only** —
originals are copied, never moved or modified. For ops/cron without a running server (it applies
migrations and opens the DB itself); the run is recorded in `import_runs` as source `folder`, so it is visible
in `/import` and in `GET /import/runs` alongside the finished migration's runs. It is the **only** source
still written.

**It is always safe to run again.** Identity is the SHA256 of the content: anything already in the library is reported
as a duplicate (even under a different name — the listing shows both paths) and nothing is written. The run is also
resumable — each file is committed separately, so a crash or Ctrl-C leaves the already-imported photos
in the library and the next run finishes the rest (an interrupted run is closed as `failed`). An error on a single file
is logged and **processing continues**; the command exits with a **nonzero exit code** when at least one file
failed, so a script can tell.

#### Sidecars: Google Takeout (`.json`) and Apple (`.xmp`)

A Google Photos (Takeout) export carries metadata **next to** the photo, not inside it: the exported JPEG
usually has its EXIF stripped on re-encode, so the real capture date, caption, and GPS live only in the `.json`
file beside it. Importing such a folder naively = losing everything; that is why the import **reads** sidecars
(disable with `--no-sidecars`).

- **What migrates.** Takeout: `photoTakenTime` → `taken_at`, `description` → description,
  `geoData`/`geoDataExif` → `lat`/`lng`/`altitude` (**an exact 0/0 = unknown**, not a point in the Gulf
  of Guinea), `favorited` → favorite for the **importing user** (favorites in Kukátko are per-user),
  `people[].name` → metadata only (Google has no face boxes, **no subject or marker is created from them**).
  Apple `.xmp` (via `exiftool`): date, GPS, caption/description, keywords, rating (per-user),
  author. `.AAE` describes an **edit**, not metadata → it is ignored.
- **Precedence.** The file's own EXIF is primary and the sidecar **fills gaps** — except for the one case
  this whole thing exists for: when the EXIF date is **more than 24 h behind** the sidecar's date, it is the
  *export* date (Takeout writes it into `DateTimeOriginal` on re-encode) and the **sidecar wins**. The sidecar
  also wins over a date guessed from the file name. The source is recorded in `taken_at_source` as `sidecar`.
  **Whatever the user has already edited in Kukátko is never overwritten** — the import fills holes.
- **Albums are not created from the export.** The folder structure and album `metadata.json` files are full of
  automatically generated junk from the phone; album membership is handled via `--album`.
- **A re-run fixes an old import.** A folder that was imported before sidecars were read just needs to be
  imported again: the files are reported as duplicates, nothing is created, but the **missing**
  date, description, and GPS are filled in. A third run writes nothing more.
- **What did not pair is named** — at the end of the run:
  `sidecars: matched=… applied=… unreadable=… unmatched=… media-without-sidecar=…` and below it a listing
  of the specific paths (max 10, then `… and N more`): a sidecar that found no photo; a photo with no sidecar
  (reported **only in directories that contain some sidecars** — in a folder straight off a camera it would be noise);
  and a sidecar that could not be read (the photo is imported **anyway**, it only loses its metadata).
  A silent mismatch is a way to lose a decade of data — so it is reported, not guessed.
  Name pairing survives every Takeout variant (`IMG.jpg.json`,
  `IMG.jpg.supplemental-metadata.json` and its truncated forms, `IMG_1234.jp.json`,
  `IMG_1234.jpg(1).json` ↔ `IMG_1234(1).jpg`); an **ambiguous** truncated match prefers to pair nothing.

Skipped (and counted by reason, never causing a failure): dotfiles and dot-directories, `@eaDir`,
`__MACOSX`, `Thumbs.db`, `.DS_Store`, `desktop.ini`, sidecars (`.xmp`, `.json`, `.aae`, `.thm` — they are
not imported as **media**; their metadata is read from `.xmp`/`.json`, see above),
empty files, and formats that are neither a supported image nor video (HEIC/RAW/video **are
supported**). **Symlinks are skipped, not followed** (so the walk cannot loop); only the
`<path>` itself is expanded, so pointing the command at a symlinked directory works. A file with no EXIF and no date
in its name is imported with `taken_at` = NULL — the date is **never inferred from mtime** (a wrong date is
worse than none).

The embedding sidecar (box) being offline is fine and expected: the jobs stay queued in Postgres
and are picked up once the box is reachable again — the summary says as much.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--album <uid\|name>` | – | adds every photo to the album; a uid is used as-is, a name is looked up and **created if not found** (applies to duplicates too → this fixes a forgotten `--album`) |
| `--labels <a,b,c>` | – | attaches labels (by name; anything that does not exist is created) to every photo in the run |
| `--recursive`, `-r` | `true` | descends into subdirectories too |
| `--no-recursive` | `false` | flat directory only (**mutually exclusive** with `--recursive`) |
| `--dry-run` | `false` | only reports what it would do (new / duplicate / skipped + reason, including the **full sidecar report**) — **writes nothing**, not even `import_runs` |
| `--no-sidecars` | `false` | ignores metadata next to the media (a Takeout export then arrives **without dates or captions**) |
| `--concurrency N` | `3` | how many files are uploaded in parallel; **cap 8** (thumbnailing large photos is memory-hungry and the box has 16 GB shared with everything else) |
| `--uploader <user>` | bootstrap admin | the username of the owner of the imported photos; without it `auth.bootstrap_admin_username`, otherwise the first admin |

Output: one line per file (`[12/2000] imported 2014/IMG_0001.JPG (sidecar: IMG_0001.JPG.json)`) and at
the end a summary `imported=… duplicates=… skipped=… failed=…` + a breakdown of the skipped ones by reason,
the **sidecar report** (see above), and the run duration.

### `kukatko storage migrate-to-r2`

A one-off move of ~120 GB of originals (with their metadata sidecars and already-cached thumbnails)
from the local disk to the R2 bucket. It runs for hours and may be killed and restarted at any time.
Object keys = `file_path` from Postgres (and the parallel `sidecars/…` key for each sidecar), so
nothing is re-keyed — the bucket gets the same layout as the disk.

It needs `storage.r2.{endpoint,bucket,access_key,secret_key}` and `storage.temp_path`, otherwise
it ends on `errStorageR2NotConfigured` (the message names the keys, never their values).
It needs neither `storage.r2.media_base_url` nor the signing secret — the command only writes objects,
it does not mint URLs. Run it **before** switching `storage.backend` to `r2`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dry-run` | `false` | only counts how many photos/objects/bytes would be moved; touches neither the bucket, DB, nor disk |
| `--delete-local` | `false` | deletes the local original **and its metadata sidecar** — only **after** the row is committed, never for a photo that failed verification |
| `--concurrency` | `2` | how many photos are uploaded in parallel (deliberately low: small VPS, FDs and memory) |
| `--batch-size` | `200` | how many pending photos are loaded from the catalog at once |

**The per-photo step order is binding:** upload the objects — the original, its metadata sidecar,
and any cached thumbnails — → read them back (size + SHA256) → commit the row
(`photos.storage_migrated_at`) → only then delete the local original **and its sidecar**. The sidecar
is the disaster-recovery artifact (a rebuild reads the catalogue back out of it), so it travels into
the bucket with the original and the original is **never** deleted until its sidecar is durable there;
both sit under the originals root this migration exists to empty, so `--delete-local` removes both.
Thumbnails are never deleted (regenerable from the original, and living in a separate cache). A photo
with no sidecar yet simply has none to move. A photo that failed stays without a stamp, keeps its
original on disk, and the next run retries it.

**Resume:** the cursor is `photos.storage_migrated_at` (migration `0019`) — the same high-watermark
rule as `internal/importer`, only per row, because under parallelism photo N+1 commonly finishes
before N. A done photo is skipped; an object already in the bucket with the correct
size and digest is not re-uploaded.

**Errors:** a per-photo failure is collected and printed only at the end (the run continues), a systemic failure
(bad keys, a missing bucket → `storage.IsSystemic`) stops the run **immediately**. Exit ≠ 0 when
the run crashed or some photo failed.

**Progress** is printed every 15 s: done photos, uploaded objects and bytes, skipped, failed,
and an estimate of the remainder — an hours-long job that stays silent is a broken job.

**Billing:** R2 charges a Class A operation per write and a million a month is free → a full migration of
~100,000 objects costs nothing. **A repeated full upload does not** — so the command first asks the
bucket what it already has (`HEAD` = Class B, 10 M/month free), and writes only the missing ones.

```bash
kukatko storage migrate-to-r2 --dry-run                      # how much is left
kukatko storage migrate-to-r2 --concurrency 4                # upload, leave originals on disk
kukatko storage migrate-to-r2 --delete-local                 # upload and clean up after itself
```

### Retired: the one-off importers

This library was originally filled by importers written for the two systems that held it before.
They ran once, reconciled COMPLETE on 2026-08-05, and were deleted — some 20 000 lines kept alive
only to prove a finished job. **`kukatko import dir` is the only import there is**, and the
`POST /api/v1/import/*` triggers and `GET /api/v1/import/verify` are gone with them.

What stays is live data, not leftovers:

- the `import_runs`/`import_failures` history (`GET /api/v1/import/runs`, `GET /api/v1/import/failures`,
  the `/import` page), including those first runs as provenance;
- the `photos.photoprism_uid`/`photoprism_file_hash`/`photosorter_uid` columns and the
  `photoprism_aliases` table. **Do not drop them** — `uid:pt…` search resolves through them and every
  metadata sidecar carries them. They are named after where the identifier came from, which is the
  only reason those names appear in the schema at all.

An old `config.yaml` or unit file may still set `import.photoprism.*`, `import.photosorter.*` or
`ratelimit.import.*`. Nothing maps onto those keys any more and Viper ignores them silently, so a
deployment that was never cleaned up still starts (pinned by `TestLoad_retiredImportSectionsAreIgnored`
and `TestLoad_retiredImportEnvIsIgnored` in `internal/config`).

### `kukatko maintenance nameless-subjects` — the nameless catch-all subject

Reports, and on request removes, subjects whose **name identifies nobody** — empty, whitespace, or punctuation
alone. Such a subject cannot be created deliberately: `POST /api/v1/subjects` rejects a name with no letter or
digit. One in the catalogue was therefore minted by an importer, and it behaves as a **catch-all**: the
find-or-create-by-name paths used to key on `people.Slugify`, which is total and answers `subject` for a
nameless face, so the first unnamed face created one empty-named subject and every unnamed face after it was
*found* by that same key and assigned to it. On production (2026-08-02) one such subject collected **16 532
markers** against 4 635 on all real people, sat first in `/people`, and made face assignment unusable.

The importers now key on `people.NameSlug`, which returns `""` for exactly those names, so a nameless face
stays unassigned. This command is the repair for the data the old behaviour already wrote.

```bash
kukatko maintenance nameless-subjects                                   # dry run (default): report only
kukatko maintenance nameless-subjects --apply --undo-file /tmp/undo.json   # detach, writing the undo
kukatko maintenance nameless-subjects --undo /tmp/undo.json                # put everything back
```

- **Dry run by default.** With no flag it lists every nameless subject with its uid, slug, creation time and
  how many markers and cached faces point at it, then totals them. It changes nothing.
- **`--apply` requires `--undo-file`** (`errUndoFileRequired`). Detaching sets the marker→subject links NULL,
  and nothing else in the database records what they were, so the undo file is the only way back; the command
  refuses to run destructively with nowhere to put it. The file is written (and so proven writable) *before*
  the first deletion and rewritten after each one, so an interrupted run leaves an undo covering exactly what
  it changed. If a rewrite ever fails, the snapshot is printed to stdout to be saved by hand.
- **What `--apply` does** per subject, in one transaction: the markers are detached (`markers.subject_uid`
  → NULL via the FK), the cached `subject_uid`/`subject_name` on the `faces` rows are cleared, the subject row
  is deleted, and a `subject.delete` audit entry is written in that same transaction. No photo, marker or face
  is deleted — only the assignment.
- **`--undo <file>` restores** every subject in the file under its original uid, name, notes and timestamps
  (only the slug may differ, if another subject took the base slug meanwhile) and re-points its markers and
  faces, auditing each as `subject.create`. A marker or face deleted since the snapshot is simply skipped, so a
  partially outdated undo restores what it can.

It is **not a migration**: it deletes catalogue rows the user might conceivably have wanted, so it stays an
operator decision taken with the report in hand.

The same repair is on the admin **Údržba** page (`/maintenance`, maintainer-only, `GET`/`POST
/api/v1/maintenance/nameless-subjects[/detach|/restore]`, see `docs/API.md`), because SSH into the production
container is a poor place to keep the fix for the one row the user is actually staring at. The HTTP form keeps
the same contract with the same code: the report is read-only, the apply hands the undo file to the browser as
a **download** and schedules the detach only once it has gone out (the browser's copy *is* `--undo-file`), and
the undo takes that file back. The file format is identical (`namelessjob.Undo`), so a file downloaded in the
browser replays with `--undo` and one written by `--undo-file` uploads to the page. The destructive halves run
as the `nameless_detach` / `nameless_restore` **jobs** rather than inside the request: detaching production's
catch-all moves ~111 000 faces into the partial "unassigned faces" HNSW index (migration 0047), which is
minutes of index maintenance. Watch the job queue on the same page for progress.

### `kukatko maintenance reset` — the guarded library wipe

Empties this instance's library — every catalogue table and every object the configured store owns
(`internal/reset`). It is the only command in the binary that destroys the library on purpose. This
deployment has **no S3 backup**, and the only way back is re-walking the folders the library was
imported from: a misfire is **unrecoverable**. The guards below are the feature, not decoration.

**What it deletes.** The 28 catalogue tables — `photos`, `photo_files`, `albums`, `album_photos`, `labels`,
`photo_labels`, `subjects`, `markers`, `faces`, `face_clusters`, `face_detections`, `face_confirmations`,
`embeddings`, `photo_phashes`, `photo_places`, `photo_edits`, `photo_comments`, `photoprism_aliases`,
`import_runs`, `import_failures`, `jobs`, the
per-user curation (`user_favorites`, `user_ratings`, `saved_searches`) and the rejection/dismissal tables
(`face_rejections`, `label_rejections`, `duplicate_dismissals`, `duplicate_marker_dismissals`) — in **one**
`TRUNCATE … RESTART IDENTITY`
(no `CASCADE`: every FK between them is inside the list, so a future table that references one of them and was
not classified makes Postgres refuse the statement instead of silently widening the blast radius). In the store
it deletes the `YYYY/MM` originals, the `thumb/` thumbnails and the `sidecars/` metadata, plus the local
thumbnail cache under `storage.cache_path`.

**What it never touches.** `users`, `sessions`, `api_tokens`, `announcements`, `instance_settings`,
`audit_log` and `schema_migrations` — a wipe must not lock you out of the instance you just wiped, nor close
self-service registration and throw away its shared secret, nor erase the record of the wipe.

**The guards** (all on by default):

| Guard | Behaviour |
| --- | --- |
| Dry run is the default | Without `--execute` it prints a row count per table and an object count per prefix, and deletes nothing. |
| Typed confirmation | You must type the target database's name (not `y/N`); a mismatch → `ErrConfirmationMismatch` and nothing is deleted. `--confirm-database <name>` supplies it without a prompt. |
| Typed bucket confirmation | On a bucket-backed store you must type the configured bucket's name too; a mismatch → `ErrBucketConfirmationMismatch`. `--confirm-bucket <name>` supplies it without a prompt. The database and the bucket come from independent config keys and can name independent deployments (a dev database pointed at the production bucket is exactly the accident this refuses), so confirming one says nothing about the other. A name typed against a store that has **no** bucket — the `fs` backend — is refused for the same reason: the operator was aiming at something this run cannot reach. |
| Target check | `current_database()` is read **from the server** and compared with the database in the loaded config; a mismatch → `ErrTargetMismatch`. Host + database are printed before you are asked. |
| Non-interactive refusal | Stdin that is not a terminal (a script, cron, an agent — `/dev/null` included, which is why the check is a terminal ioctl and not "is a character device") is refused unless `--force` is passed too. Checked **before** the config is loaded, so a stray invocation opens nothing. |
| Storage scope | Only the three prefixes the store owns are ever deleted; anything else in the bucket is counted as `foreign` and left alone. A key the catalogue does not reference is deleted only with `--orphan-sweep`. There is no delete-everything path. |
| Audit | One `library.reset` entry (`internal/audit`), written **in the same transaction as the truncation**, recording the operator (`$USER@$HOSTNAME`), the target database and bucket, the per-table row counts removed and the object counts. |
| Before/after summary | Both snapshots are printed, and a catalogue table that somehow survived is flagged with `WARNING`. |
| Schema drift | A table in `public` that the command classifies as neither wiped nor preserved (or a classified table that is missing) aborts the run with `ErrSchemaDrift`, naming it. Adding a table to a migration therefore cannot silently leave part of the library behind — the fix is one line in `internal/reset/tables.go`. |

```bash
kukatko maintenance reset                       # dry run: what would be deleted (default; changes nothing)
kukatko maintenance reset --execute             # asks you to type the database name, then wipes
kukatko maintenance reset --execute --orphan-sweep   # also deletes leftovers the catalogue never referenced
kukatko maintenance reset --execute --force \
    --confirm-database kukatko \
    --confirm-bucket kukatko-dev --orphan-sweep # non-interactive (a script/agent must say all of it;
                                                # --confirm-bucket only on a bucket-backed store)
```

**Stop the server first.** The wipe empties the job queue and the catalogue the running instance is serving; a
worker mid-job would keep writing rows into a library that no longer exists.

**Use `--orphan-sweep` for the cutover wipe.** Without it the catalogue is the list of what may be deleted,
which leaves behind whatever an earlier interrupted import put in the bucket. On an object store the sweep is
also the *faster* path: it deletes the objects that are actually there (one `LIST` + one `DELETE` each) instead
of probing for the eight thumbnail keys every photo might have.

It **does not apply migrations** — unlike `maintenance scan`/`repair`, a command whose job is to delete has no
business changing the schema on the way in. A database behind (or ahead of) its migrations therefore aborts on
the schema-drift guard, naming the tables involved; run `kukatko migrate` first.

The run is **idempotent and resumable**: the store is emptied before the catalogue (the catalogue is where the
object keys come from), and if any object fails to delete the truncation is skipped and the command exits
nonzero with `ErrStorageIncomplete` — the catalogue still describes the store, so the fix is to make the store
reachable and run it again. It is deliberately **not exposed over HTTP**, for the same reason `restore db` is
not: it pulls the tables out from under a running server.

### `kukatko ctl` — remote API client

The other subcommands touch the database and filesystem directly. `ctl` is the opposite: it talks to a **running**
instance via its `/api/v1`, authenticates with an **API token** (`Authorization: Bearer kkt_…`,
see [`docs/API.md`](API.md)) and needs neither `database.url` nor access to the originals.
It is for driving production from the terminal — and, through that terminal, by an agent too. It is cheaper
in tokens than the MCP server: no tool schema is loaded into the model's context, only a short
command and a narrow result. **That is why the output is compact** — that is the whole point.

**One binary, two names.** Through a symlink named `kukatkoctl` the `ctl` level is implied
(detected from `os.Args[0]`), so `kukatkoctl photos list` == `kukatko ctl photos list`:

```bash
ln -s /usr/local/bin/kukatko /usr/local/bin/kukatkoctl
```

#### Client configuration

`kubectl`-style contexts live in **`~/.config/kukatko/ctl.yaml`** (honors `XDG_CONFIG_HOME`).
They have **nothing to do** with the server configuration (`internal/config`, `config.yaml`) — that
describes the server and knows nothing about the remote endpoint.

```yaml
current-context: prod
contexts:
  - name: prod
    server: https://kukatko.example.com   # the web root, WITHOUT /api/v1 (the client appends it)
    token: kkt_ab12_…                     # the token in plaintext; the file is always 0600
```

The file is written **atomically and always with mode `0600`** (the directory `0700`); an existing
world-readable file is tightened before writing. **The token is never printed anywhere** — not to the
log, not to an error message, not to `ctl config list`.

| Command | Meaning |
| --- | --- |
| `ctl config set-context <name> --server <url> [--token <t> \| --token-stdin] [--current]` | creates/updates a context; the first one created becomes current. An omitted field is preserved (changing the URL does not clear the token). |
| `ctl config list` (alias `get-contexts`) | lists the contexts; for the token only `stored`/`not set` |
| `ctl config use-context <name>` (alias `use`) | switches the current context |

`--token` is visible in `ps` to the whole machine — **prefer `--token-stdin`**:
`printf '%s' "$TOKEN" | kukatkoctl config set-context prod --server https://… --token-stdin`.

**Env overrides the active context, field by field:** `KUKATKO_SERVER` and `KUKATKO_TOKEN`.
So `KUKATKO_TOKEN` alone re-credentials the stored context without touching the file.
With no file and no context, the two variables are enough. The `--context <name>` flag selects a context
other than the current one, `--ctl-config <path>` a different file.

#### Output and exit codes

`-o table` (default) is a compact table + one summary line (`3 of 42 photos · offset 0 ·
next offset 3`, plus `mode`/`degraded` for a search). An empty result prints a single line
`no photos found` / `no albums found` / … **with no header**. `-o json` prints the **server's JSON
unchanged** (no re-marshal) for machine processing; `-o yaml` does not exist.

**`-o llm` — the same answer, minus what costs tokens.** A third format, accepted by `ParseFormat` and
therefore available to **every** `ctl` command, not just `photos get`. It re-encodes the server's body as
compact JSON with everything a reader learns nothing from removed:

| Dropped | Why |
| --- | --- |
| every empty and zero-valued field | a zero rating and an empty note say exactly as much as their absence |
| `exif` | a raw camera document, hundreds of tags deep, all of it already in named columns |
| `thumb_url`, `download_url` | long signed URLs that expire — fetch the image with `photos image` |
| `file_hash`, `file_path`, `file_orientation`, `files` | where the bytes live is the storage layer's business |
| `software`, `color_profile`, `image_codec`, `camera_serial`, `original_name`, `projection`, `video_codec`, `audio_codec`, `title_edited` | machine-derived provenance the API itself refuses to edit |
| `processing` | the queue's bookkeeping about the photo, not the photo |
| `photoprism_uid`, `photoprism_file_hash`, `photosorter_uid` | which library a row came from |
| `created_at`, `updated_at`, `metadata_extracted_at` | row bookkeeping — the date that means something is `taken_at`, which is kept |

Kept: identity, the texts, the date trio with its precision, the location with its origin, `people`,
albums, labels, media type and dimensions. It is a rule about **keys**, not a projection per resource,
which is exactly why one implementation covers every command and everything added later.

**`--fields uid,title`** narrows it further to an allowlist. A named key keeps its whole value; a key that
was **not** named survives only as the road to one that was, so the allowlist reaches through a list
envelope without your having to name `photos`. An allowlist nothing matches prints `{}` rather than
silently falling back to the whole body.

```bash
kukatkoctl photos get pht01h2j3 -o llm
kukatkoctl photos list --year 2024 -o llm --fields uid,title,taken_at
```

**Exception for `204 No Content`.** Where the API returns no body (attach/detach a label, favorite,
rating), there is nothing to pass through unchanged — `-o table` prints one sentence and `-o json` a single
payload the CLI produces itself: `{"status":"ok","message":"photo pht01 favorited"}`. A pipeline
can thus tell success from failure.

Exit `0` on success, nonzero on both HTTP and transport errors. **`401`** gives a short, actionable
message (the token is missing / expired / was revoked + how to make a new one). **`403`** (a viewer touched
a mutation) says **outright that the role is insufficient** — mutations want `editor`/`admin`/`ai`, a viewer only reads.
The **`ai`** role is meant for an API-token automaton: it writes like an editor **plus** import (`POST /import/*`),
but other admin actions (users, backups, jobs, maintenance, processes, audit, system) return `403`.
Neither one prints a stack trace, the response body, or the token.

#### `ctl photos`

| Command | Meaning |
| --- | --- |
| `ctl photos list` | a page of `GET /photos` |
| `ctl photos get <uid>` | detail `GET /photos/{uid}` (+ albums, labels, `ocr_text` and, by default, who is on it) |
| `ctl photos search <query>` | `GET /search?q=…&mode=…` |
| `ctl photos image <uid>` | saves a rendition to a file and prints the path |
| `ctl photos edit <uid>` | `PATCH /photos/{uid}` — the whole editable metadata surface (`editor`/`admin`) |
| `ctl photos faces <uid>` | `GET /photos/{uid}/faces` — the detections, their markers, and who they might be |
| `ctl photos similar <uid>` | `GET /photos/{uid}/similar` — the visual neighbours with their cosine distance; `--limit` 1…100 |
| `ctl photos upload <path>…` | `POST /upload` — streams files in through the ordinary ingest path (`editor`/`admin`) |
| `ctl photos archive` / `unarchive <uid>` | `POST /photos/{uid}/archive`+`/unarchive` — to the trash and back (reversible) |
| `ctl photos hide` / `unhide <uid>` | `POST /photos/{uid}/hide`+`/unhide` — out of the library grid, nothing deleted |
| `ctl photos rebuild <step> <uid>` | recompute one derived thing over the top of what is stored (`maintainer`); see below |
| `ctl photos purge <uid>` | `POST /photos/{uid}/purge` — **permanent**, `admin`, needs `--yes`; see [the gate](#the-irreversible-commands-and-their-gate) |

Together `get`, `image` and `edit` are the loop an agent needs per photo: **read it whole, look at it,
write the evaluation back** — one command per step instead of three raw HTTP calls. `faces` opens the
second loop, over people, which [`ctl faces`](#ctl-faces) continues.

`list` and `search` share the filters, except those marked "`list` only" / "`search` only".
`search` orders by relevance, so it offers no `--sort`/`--order`; it offers no `--favorite` because
`GET /search` does not read that parameter at all — offering it would silently return an unfiltered result.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--limit` | `0` (= server default 100) | photos per page, the server caps at 500 |
| `--offset` | `0` | how many to skip; the next offset is given by the summary line |
| `--sort` (`list` only) | server default | `newest`/`oldest`/`taken_at`/`added`/`title`/`size`/`rating` |
| `--order` (`list` only) | per `--sort` | `asc`/`desc` |
| `--year` | `0` (no filter) | calendar year. **The API has no year** — the client translates it into `taken_after`/`taken_before` |
| `--album` / `--label` | — | scope to an album/label uid |
| `--favorite` (`list` only) | `false` | only your own favorites |
| `--archived` | server default (`false`) | `true` = including the archive, `only` = trash only |
| `--mode` (`search` only) | `hybrid` | `fulltext`/`semantic`/`hybrid` |

If the box (embeddings sidecar) is offline, `semantic`/`hybrid` falls back to fulltext and the summary
line says so (`degraded`). The CLI has no capability probe, so unlike the web UI it does send the
request and finds out from the answer — bounded by `embedding.dial_timeout`/`text_timeout`, a few
seconds, not the half minute a stock dialer would spend.

```bash
kukatkoctl photos list --year 2024 --limit 5
kukatkoctl photos list --album alb1a2b3 --sort title -o json | jq '.photos[].uid'
kukatkoctl photos get pht01h2j3
kukatkoctl photos search "západ slunce nad jezerem" --mode semantic
KUKATKO_SERVER=http://localhost:8080 KUKATKO_TOKEN=kkt_… kukatkoctl photos list
```

#### `ctl photos rebuild` — recompute, don't merely re-schedule

**The trap this exists for:** every per-photo job skips work it has already done. Asking the server to run
a step again — `POST /photos/{uid}/process/{step}`, or the same button in the UI — enqueues the ordinary
job, which looks at a photo that already has an embedding, a recorded face detection or a geocoded
coordinate and leaves it alone. The request answers **200** and the step reads `done`, so it looks like it
worked while nothing happened. That is the right behaviour for a photo the pipeline **missed**, and useless
for a photo whose stored answer is **wrong** — computed from a source that has since been corrected, or by a
model that has since changed. (This is how seven Nikon NEFs kept their 640×424 thumbnails after the RAW
preview fix in `273a724`: `process/thumbnail` skipped every cached size.)

`rebuild` discards the stored answer and computes a new one. It needs the **maintainer** role, never touches
an original, and is not gated — everything it throws away it can produce again.

| Command | Endpoint | What it redoes |
| --- | --- | --- |
| `ctl photos rebuild thumbnail <uid>` | `POST /photos/{uid}/regenerate-thumbnail` | every cached thumbnail size + the perceptual hashes |
| `ctl photos rebuild embedding <uid>` | `POST /photos/{uid}/reembed` | the CLIP image embedding behind semantic search and similar photos |
| `ctl photos rebuild faces <uid>` | `POST /photos/{uid}/redetect-faces` | face detection; the stored faces are **replaced**, and it reports how many the photo has afterwards |
| `ctl photos rebuild place <uid>` | `POST /photos/{uid}/regeocode` | the reverse geocode — **costs a mapy.com credit every time** |

A re-detection replaces rather than appends, and hands each assignment to the face that comes back in the
same place, so redoing the detection never leaves duplicate faces behind and never un-names anybody. A face
found somewhere new arrives unassigned.

When the embeddings box (or mapy.com) is offline the work is **queued** instead of failing: a forced job goes
into the queue and runs when the service is back. The command says so and exits 0 — a sleeping box is not an
error. Two requests for the same photo collapse into one job (queue dedup is keyed on type + photo uid).

```bash
kukatkoctl photos rebuild thumbnail pht01h2j3
kukatkoctl photos rebuild faces pht01h2j3       # → "face_detect: rebuilt, 3 faces on the photo"
kukatkoctl photos rebuild embedding pht01h2j3   # → "image_embed: … a forced job is queued …" when the box sleeps
```

#### `ctl photos get` — the whole photo in one request

Beyond the metadata, `get` reports **`ocr_text`** (the text the recogniser read *in* the photo; the table
folds it onto one line, the full reading is in `-o json`/`-o llm`) and **who is on the photo**: the named
subjects followed by a count of the detections nobody has assigned yet. Reading the photo whole is the
point of the command, so the roll-call is asked for **by default**; on the server it stays opt-in
(`?people=true`) because assembling it costs a face↔marker match a plain read should not pay for, and
**`--people=false`** skips it. When the response carries no roll-call at all — you turned it off, or the
instance has no face backend — the row reads `- (not reported)`, which is **not** the same as "nobody is
on this photo". The date and the location are printed beside their provenance (`estimated, year, manual` /
`50.08750, 14.42111 (estimate)`) so an inferred value never reads like a measured one.

#### `ctl photos image` — actually look at the photo

```
ctl photos image <uid> [--size fit_720] [--output-file <path>]
```

Saves one rendition and prints the path, which is what you feed to the next step (`-o json`/`-o llm` get
`{"path":…,"bytes":…,"media_type":…}` instead, so a pipeline need not stat the file). `--size` takes a
thumbnail size (`fit_3840`/`fit_2560`/`fit_1920`/`fit_1280`/`fit_720`/`tile_500`/`tile_224`/`tile_100`)
or **`original`** — the stored file itself, full size, in its own format, a video included. An unknown size
is refused locally rather than becoming a puzzling 404.

The bytes are **streamed** from the socket into the file and never held in memory, on a client with **no
timeout** (a hundred-megabyte original is slow on purpose; only Ctrl-C ends it). The download lands on a
temporary file beside its destination and is renamed into place only when it is complete, so an interrupted
transfer never leaves a half-written file that looks like a photo. With no `--output-file` the name comes
from the response — the `Content-Disposition` of `/download` for an original, `<uid>_<size>.jpg` otherwise —
always reduced to a bare file name, so a server cannot steer the write out of the working directory.

The flag is `--output-file` (`-f`), not `--output`: `-o`/`--output` is already the global output format, and
a local flag of the same name would shadow it and break `-o llm` on this one command.

#### `ctl photos edit` — write the evaluation back

```
ctl photos edit <uid> [fields…] [--people] [--dry-run]
```

The whole `PATCH /photos/{uid}` surface, one flag per API field, named after the field:

| Flag | Meaning |
| --- | --- |
| `--title` / `--description` / `--notes` / `--ai-note` | the free texts |
| `--subject` / `--keywords` / `--artist` / `--copyright` / `--license` / `--scan` | the IPTC/XMP credits |
| `--taken-at` / `--clear-taken-at` | the capture time: a date, a date and time, or an RFC 3339 timestamp |
| `--taken-at-estimated` / `--taken-at-note` | the date is a guess, and what the guess rests on |
| `--lat` + `--lng` / `--clear-location` | the position; both halves or neither |
| `--accept-location` | accept an estimated position as your own (`location_source: manual`) |

**Only the flags you actually write are sent.** That is not an optimisation: re-sending the `taken_at`
that is already on the photo would make the server stamp it `manual`, and the library would permanently
lose the fact that the date came out of the file — on a photo you only meant to retitle.

**Clearing is therefore its own act, never an accident of omission.** A text column is emptied by passing
it the empty string (`--title ""`); the three nullable ones get their own flag (`--clear-taken-at`,
`--clear-location`), which sends an explicit JSON `null`. Nothing else can express the difference, which is
why the request body is built as a key/value map rather than a struct of pointers.

**The rules stay on the server.** The length caps, the dating note that only lives while the date is
flagged as an estimate, and which `location_source` a client may claim are all enforced by
`internal/photoapi`; `ctl` re-implements none of them and reports the `400` it gets back. The fields the API
serves but refuses to edit (`software`, `color_profile`, `image_codec`, `camera_serial`, `original_name`,
`projection`) get **no flags at all** — offering one the server would reject is worse than offering none.
Contradictions that need no round trip (`--taken-at` with `--clear-taken-at`, a lone `--lat`, an unreadable
date) are caught locally.

**`--dry-run` prints the request body and writes nothing** — not even a context is needed. This runs
against a live family archive; show your intent before you change it.

```bash
kukatkoctl photos image pht01h2j3 --size fit_1920 -f /tmp/look.jpg
kukatkoctl photos edit pht01h2j3 --ai-note "babička na dvoře" --dry-run
kukatkoctl photos edit pht01h2j3 --taken-at 1978-06-03 --taken-at-estimated \
  --taken-at-note "podle babičky rok po svatbě" --people -o llm   # --people: read the roll-call back
kukatkoctl photos edit pht01h2j3 --title ""      # empties it; omitting --title leaves it alone
```

#### `ctl photos upload` — the way in

```
ctl photos upload <path>…
```

The files go through **the same ingest path as the web uploader** (`POST /upload`,
`internal/ingest`): the server hashes each one and refuses to store the same bytes twice, reads its
metadata, renders its thumbnails and queues the follow-up work (embeddings, faces, OCR, the metadata
sidecar). None of that happens in the CLI — this command only carries the bytes there, so an upload
from the terminal and one from the browser produce the same photo.

**It streams.** The multipart body is written into the request as the files are read from disk, so a
hundred-megabyte original never sits in memory, and the transfer has no client timeout (like
`photos image`, only the context you interrupt it with ends it). Every path is checked to be an
existing regular file **before** a byte is sent, so a typo fails the command instead of half the batch.

A file whose bytes are already in the library is reported as **`duplicate`, not as an error** — that
is the deduplication doing its job — and the row carries the uid of the photo that already holds
them. Only a file that could not be catalogued at all makes the command **exit nonzero**; the report
is printed either way.

```
FILE      OUTCOME    UID     NOTE
a.jpg     created    pht01   near_duplicate
b.jpg     duplicate  pht02   -
c.txt     error      -       unsupported media type

3 files · 1 created · 1 already in the library · 1 failed
```

To ingest a whole directory tree, use **`kukatko import dir`** beside the library instead: this
command takes files, and a directory is refused rather than walked.

#### The lifecycle: hide, archive, purge

A photo moves through four states, and `ctl` draws a hard line across them:

| Step | Command | Reversible? |
| --- | --- | --- |
| out of the library grid | `photos hide` / `photos unhide` | yes, by its undo |
| into the trash | `photos archive` / `photos unarchive` | yes, by its undo |
| gone | `photos purge`, `trash empty`, `trash purge-older` | **no** |

**`hide` is not archiving.** Nothing is deleted or scheduled for deletion: the photo leaves the
library grid and its counts, the timeline, the map, the slideshow, the review game and the default
search, and stays fully visible in its albums and labels, in favourites and at its own uid. It is
for the photo worth keeping but not worth meeting again by accident; `q=hidden:yes` lists them.

**`archive` is a soft delete** — nothing about the photo changes, it simply leaves the default
listings — but it starts a clock: the trash is purged by retention. All four commands answer with
the refreshed photo and print **both** flags, not only the one they changed, because archiving a
hidden photo leaves it in two states at once.

`ctl bulk --archive` / `--unarchive` does the same to a whole set in one transaction; these
per-photo commands exist because hiding has no bulk operation and because a single photo needs no batch.

#### `ctl trash`

The trash, and the two ways to empty it for good.

| Command | Meaning |
| --- | --- |
| `ctl trash info` | what is in the trash and when retention takes each photo (read-only, any role) |
| `ctl trash empty` | `POST /trash/empty` — **permanently deletes every archived photo** (`admin`) |
| `ctl trash purge-older --days N` | `POST /trash/purge-older` — the same for photos archived longer ago than `N` days (`admin`); `--days 0` is the whole trash |

`info` reads `GET /trash/info` for the retention window and pages `GET /photos?archived=only` for
the photos themselves, then sorts them **oldest-archived first** — the order retention takes them
in — and stamps each with the date it will be destroyed on. The sort happens client-side because the
listing has no `archived_at` sort key, and a "what goes next" answer computed over one arbitrary page
would be wrong rather than merely partial. With retention off (`trash.retention_days` <= 0) there is
no date to print and the summary says so: nothing in the trash then goes away on its own.

```
UID     FILE       TITLE   SIZE     ARCHIVED          PURGE AT
pht01   old.jpg    Lake    2.0 MiB  2026-01-02 10:00  2026-02-01 10:00

1 of 1 photos · 2.0 MiB · retention 30 days
```

#### The irreversible commands and their gate

`photos purge`, `trash empty`, `trash purge-older` and `duplicates merge` are the commands that
cannot be taken back. They all carry the same two flags:

| Flag | Meaning |
| --- | --- |
| `--yes` / `-y` | confirm; **without it the command refuses and says so**, having changed nothing |
| `--dry-run` | list exactly what would be destroyed, change nothing, and **exit 0 without `--yes`** |

The asymmetry is deliberate: a rehearsal is how the decision gets made, so it cannot sit behind the
same flag as the decision. There is no size threshold and no prompt (unlike
[large batches](#large-batches-confirmation-above-50-photos)) — there is no number of photos at which
permanent deletion is harmless, and a piped agent has no terminal to answer a question from.

**Nothing here deletes an original by itself.** Every purge goes through `internal/trash`, the
catalogue's own path (row, original, thumbnails, storyboard, backup object, in that order, audited);
the CLI adds no deletion logic and has no way around it. `photos purge` refuses a photo that is not
archived (`409`), so nothing live is one command away from gone, and it needs the **admin** role,
not merely write access. `duplicates merge` archives the copies it did not keep — it deletes
nothing — which is why it is gated here and yet its result says out loud that they are in the trash.

`ctl trash info` exists so the gate is informed rather than blind, and each dry run prints the same
listing under a heading that says what it is:

```bash
kukatkoctl trash info                                  # what is in there, and what goes next
kukatkoctl trash empty --dry-run                       # exactly what would be lost
kukatkoctl trash empty --yes                           # 2 photos permanently deleted
kukatkoctl trash purge-older --days 30 --dry-run       # only what is past the window
kukatkoctl photos purge pht01h2j3 --dry-run            # one photo, named and sized
kukatkoctl duplicates merge pht01h2j3 pht09z8y7 --dry-run
```

#### `ctl albums`

Albums and their membership (`internal/organizeapi`). Anyone logged in may list; **create and membership
require `editor`/`admin`**.

| Command | Meaning |
| --- | --- |
| `ctl albums list` | `GET /albums` — a **bare `{"albums":[…]}`, no pagination**, each album with a photo count |
| `ctl albums get <uid>` | `GET /albums/{uid}`; the detail **does not send** `photo_count`, so the column is absent |
| `ctl albums create <title>` | `POST /albums`; `--description`, `--type`, `--order-by`, `--cover`, `--private` |
| `ctl albums update <uid>` | `PATCH /albums/{uid}`; `--title`, `--description`, `--cover`, `--private` |
| `ctl albums delete <uid>` | `DELETE /albums/{uid}` — needs `--yes`; the photos stay |
| `ctl albums add-photos <album-uid> [<photo-uid>…]` | `POST /albums/{uid}/photos` — appends **after** the existing ones |
| `ctl albums remove-photos <album-uid> [<photo-uid>…]` | `DELETE /albums/{uid}/photos`; a non-member = no-op |

`--type` is `album` (default), `folder`, `moment`, `state`, or `month`; only `album` makes sense
manually, the server generates the rest. `add-photos`/`remove-photos` read uids from arguments, or **from
stdin** when there are none (see *Large batches* below), and send them in **one** request.
In a table they print one line (`album alb1a2b3 now holds 12 photos`), `-o json` the whole new order.

**`update` reads the album before it writes**, exactly like `ctl subjects rename` and for the same reason:
`PATCH /albums/{uid}` rewrites the whole record, so a body carrying only a new title would empty the
description and clear the cover. Only the flags you actually write change; `--cover ""` is how you say "no
cover", and there is no `--type` because the structural type is not user-editable. An `update` naming no flag
is refused locally rather than rewriting the row and writing an audit entry for nothing.

**`delete` refuses without `--yes`** and offers `--dry-run`, like the subject commands: the photos survive —
an album is a grouping — but *which* photos somebody chose cannot be rebuilt from the library. It resolves
the album first, so the confirmation reads `Trip (alb1a2b3)` rather than a bare uid.

#### `ctl labels`

Labels and attaching them to photos (`internal/organizeapi`). Anyone may list; the rest `editor`/`admin`.

| Command | Meaning |
| --- | --- |
| `ctl labels list` | `GET /labels` — a **bare `{"labels":[…]}`**, ordered by priority |
| `ctl labels get <uid>` | `GET /labels/{uid}` |
| `ctl labels create <name>` | `POST /labels`; `--priority` |
| `ctl labels update <uid>` | `PATCH /labels/{uid}`; `--name`, `--priority`, `--review[=false]` |
| `ctl labels delete <uid>` | `DELETE /labels/{uid}` — needs `--yes`; the photos stay |
| `ctl labels attach <label-uid> <photo-uid>` | `POST /labels/{uid}/photos`; `--source`, `--uncertainty` |
| `ctl labels detach <label-uid> <photo-uid>` | `DELETE /labels/{uid}/photos`; a non-attached one = no-op |

`--source` is `manual` (default), `ai`, or `import`. If omitted it is **not sent** in the body, so the
server fills in its own default.

`update` reads the label first for the same reason `albums update` does — a body carrying only a new name
would reset the priority to zero — and `--review=false` takes the label out of the review game
([`internal/review`](PACKAGES.md)) without touching anything else. `delete` needs `--yes` and offers
`--dry-run`; the photos survive and simply stop carrying the label, but which photos somebody decided it
applied to is gone, and re-creating a label of the same name re-attaches none of them.

#### `ctl subjects`

People, animals, and other subjects of the face pipeline (`internal/peopleapi`). Reading needs any role,
writing `editor`/`admin`.

| Command | Meaning |
| --- | --- |
| `ctl subjects list` | `GET /subjects` — a **bare `{"subjects":[…]}`**; `PHOTOS` = distinct photos, `MARKERS` = faces |
| `ctl subjects get <uid>` | `GET /subjects/{uid}` |
| `ctl subjects photos <uid>` | `GET /subjects/{uid}/photos`; `--limit`/`--offset` |
| `ctl subjects create <name>` | `POST /subjects`; `--type`, `--notes`, `--cover`, `--favorite`, `--private`, `--birth-year`, `--death-year` |
| `ctl subjects rename <uid> <name>` | `PATCH /subjects/{uid}` with the stored record read back first |
| `ctl subjects merge <source-uid> <keeper-uid>` | `POST /subjects/{uid}/merge` — **irreversible**, needs `--yes` |
| `ctl subjects delete <uid>` | `DELETE /subjects/{uid}` — **irreversible**, needs `--yes` |

A subject's gallery is the only paginated subject endpoint and returns the **`/photos` envelope**, so it
prints as a photo list. It does not read the catalog filters, so `ctl` does not offer them either.

**There is no `ctl subjects edit`.** `PATCH /subjects/{uid}` rewrites the whole editable record rather than
patching it, so a flag-per-field edit would silently erase everything you did not mention. `rename` is that
edit done safely: it reads the record, changes the name, and sends the rest back untouched — otherwise
renaming a pet would reclassify it as a person and drop its notes, cover and life years on the way.

**Merging and deleting cannot be undone**, so both refuse without an explicit `--yes` (there is no size at
which losing a person's name is harmless, and no threshold to be under) and both offer `--dry-run`, which
names who would go and writes nothing. Both **resolve the people by name first**: a mistyped uid becomes a
`404` before anything is destroyed, and the confirmation reads `Anna N. (sub01)` rather than a bare uid.
`merge` moves everything the source carried onto the keeper — markers, the faces cache, confirmations,
rejections, dismissals — fills the keeper's *empty* fields from it, and deletes the source in the same
transaction. Its report is the one result `ctl` **synthesizes** instead of echoing the server (the other is
the `204` `Ack`): the response carries only uids, and the source's name exists nowhere else once the merge
has run. `delete` leaves the photos and their markers alone; the markers simply stop naming anybody, and
re-creating the person will not re-attach a single face — if the two records are the same person, `merge`
is what you want.

#### `ctl faces`

Naming the people on a photo — the most frequent curation there is (`internal/facematch`,
`internal/feedbackapi`). All of it `editor`/`admin`; start from `ctl photos faces <uid>`, whose `FACE`
column is the index every command here takes.

| Command | Meaning |
| --- | --- |
| `ctl faces assign <photo-uid> <face> [<subject-uid>]` | attach one detection to a person; `--name` instead of a uid |
| `ctl faces detach <photo-uid> <face>` | clear whoever that face names; the marker survives, unnamed |
| `ctl faces reject <photo-uid> <face> <subject-uid>` | `POST /feedback/face-rejections` — "this is NOT them" |
| `ctl faces unreject …` | `DELETE /feedback/face-rejections` (undo) |
| `ctl faces confirm <photo-uid> <face> <subject-uid>` | `POST /feedback/face-confirmations` — "this really IS them" |
| `ctl faces unconfirm …` | `DELETE /feedback/face-confirmations` (undo) |

`assign` **reads the photo's faces first**, because whether the detection already carries a marker is what
decides the action (`assign_person` on the existing marker, or `create_marker` over the detection's own
box), and only the server knows. That read is also what lets a face index the photo does not have fail
against the listing — naming the indexes it *does* have — instead of becoming a puzzling `404`. The
assignment state machine itself stays on the server, and so does `--name`: an unknown name creates the
subject there, by slug, which is how an agent names somebody the library has never heard of in one command.

**A rejection and a confirmation are opinions, not edits.** Neither detaches a marker nor draws one. A
rejection keeps a wrong suggestion from coming back on every sweep; a confirmation keeps a correct
assignment out of that person's outlier review. **They are opposites** — reaching for one meaning the other
records the exact opposite of what you decided. All four are idempotent, answer `204`, and are undoable.
They resolve the subject's name before writing, so the confirmation says *who* you just refused.

#### `ctl clusters`

The groups of unassigned faces the auto-clustering found (`internal/clusterapi`), `editor`/`admin`
throughout. Naming a whole cluster is the cheapest curation in the library — one command names a person on
every photo the clustering put in the group — which is exactly why looking first matters.

| Command | Meaning |
| --- | --- |
| `ctl clusters list` | `GET /faces/clusters`; `SUGGESTION` = the nearest named subject + its cosine distance |
| `ctl clusters assign <cluster-uid> [<subject-uid>]` | names **every** face of the group; `--name` instead of a uid |
| `ctl clusters remove-face <cluster-uid> <photo-uid> <face>` | drops one face that does not belong, before naming |

`REPRESENTATIVE` prints as `<photo-uid> #<face-index>` — the two arguments `ctl photos image` and
`remove-face` take, so looking at a group and repairing it need no translation step. `assign` **consumes**
the cluster: its faces become that person's markers and the group is gone. Removing the last face of a
cluster removes the cluster too, and the confirmation says so rather than reporting a group of zero.

```bash
kukatkoctl photos faces pht01h2j3                       # who is on it, and who they might be
kukatkoctl faces assign pht01h2j3 1 --name "Anna Nováková"
kukatkoctl faces reject pht01h2j3 2 sub1a2b3            # no, that is not her
kukatkoctl clusters list -o llm
kukatkoctl clusters assign clu1a2b3 sub1a2b3            # twelve photos named at once
kukatkoctl subjects merge sub9z8y7 sub1a2b3 --dry-run   # the same person recorded twice
```

#### `ctl stacks`

Grouping the several files one shot was stored as — a RAW beside its JPEG, an edit beside its original —
into a single tile (`internal/stacks`). All of it `editor`/`admin`.

**A stack groups, it never merges.** Every member keeps its own uid, its own file and its own metadata; the
group only decides which of them a listing, a search and the map show. That is why ungrouping loses nothing
and why none of these commands deletes anything.

| Command | Meaning |
| --- | --- |
| `ctl stacks group <photo-uid> <photo-uid> […]` | `POST /photos/stack` — the manual path, for the pairs the rules miss |
| `ctl stacks set-primary <photo-uid>` | `POST /photos/{uid}/stack/primary` — which variant the stack is shown as |
| `ctl stacks ungroup <photo-uid>` | `POST /photos/{uid}/unstack` — one member leaves, the rest stay grouped |
| `ctl stacks ungroup-all <photo-uid>` | `POST /photos/{uid}/unstack-all` — the whole group dissolves |

All four answer with the affected photo's **detail**, so the output is the resulting variants strip: one row
per member with the primary marked, closed by `stack stk1a2b3 groups 2 photos; each keeps its own file and
metadata`. After `ungroup` the photo is standalone, and the table would be empty — so it prints
`photo pht01h2j3 is not stacked` instead, which is the whole result. A group of fewer than two photos is
refused locally (a uid given twice is one photo, not two), and an instance with stacking switched off answers
`503`.

#### `ctl edits` — the non-destructive image edit

**Not `ctl photos edit`.** That writes the photo's *metadata* (title, date, place); this writes how the
library *renders* it — crop, rotation, brightness, contrast (`internal/photoedit`). The original file is
never rewritten either way. Reading needs any role, writing `editor`/`admin`.

| Command | Meaning |
| --- | --- |
| `ctl edits get <uid>` | `GET /photos/{uid}/edit` — the stored edit, or the neutral one |
| `ctl edits set <uid>` | `PUT /photos/{uid}/edit`; `--crop`, `--clear-crop`, `--rotate`, `--brightness`, `--contrast` |
| `ctl edits reset <uid>` | `PUT` the neutral edit — a reset, and a real write |

`--crop` takes `x,y,w,h` as fractions of the whole image (`0.1,0.1,0.8,0.8`), so the same rectangle survives
a thumbnail of any size; `--clear-crop` removes it and leaves the rest alone. `--rotate` is `0`/`90`/`180`/
`270` clockwise and the two adjustments run `-1`…`1`, neutral at `0` — all three are checked locally, so a
typo costs neither a round trip nor an audit entry.

**`set` reads the stored edit first** and sends the merged record back, because `PUT` replaces the whole edit
and a body carrying only a rotation would silently drop an existing crop. An argument-less `set` is refused
and names `reset`, which is the deliberate way to clear everything.

**A reset is a write, not the absence of one.** The thumbnails the grid, the search results and the map show
were rendered through the previous edit and are cached against the original's hash, so only saving the
neutral edit actually puts the photo back the way the file has it. `get` says `NEUTRAL yes` outright, because
a table of zeroes cannot otherwise be told from "nobody has edited this photo".

#### `ctl saved-searches` — smart albums

Named library views, stored per user (`internal/savedsearchapi`). Alias: `smart-albums`. Any signed-in role
may keep their own — a saved search curates nobody else's view — so there is no `editor` check.

| Command | Meaning |
| --- | --- |
| `ctl saved-searches list` | `GET /saved-searches` — a **bare `{"saved_searches":[…]}`**, newest first |
| `ctl saved-searches get <uid>` | `GET /saved-searches/{uid}`; the stored view spelled out one key per row |
| `ctl saved-searches create <name>` | `POST /saved-searches`; `--param k=v` (repeatable) or `--params '<json>'` |
| `ctl saved-searches update <uid>` | `PATCH /saved-searches/{uid}`; `--name`, `--param`/`--params` |
| `ctl saved-searches delete <uid>` | `DELETE /saved-searches/{uid}` — no photo is touched, so no confirmation |

The stored view is the flat key/value object the app puts in its URL — the filters, the sort, the query `q`
and the search `mode` — so a search written here **opens in the web UI exactly as you wrote it**. Every value
must be a string; `--params '{"year":2024}'` is refused rather than stored as a view the app cannot open.
`--param` and `--params` are mutually exclusive, and the view is always replaced whole, never merged into.

**This is the one `ctl` update that needs no read first:** `PATCH /saved-searches/{uid}` genuinely merges, so
an omitted field is left alone server-side.

**They are strictly yours.** A saved search belonging to somebody else answers `404` — never `403`, which
would confirm it exists — so a bare status cannot tell "gone" from "not yours". `ctl` says both:

```
Error: fetching saved search sav9z8y7: saved search sav9z8y7 is not yours: it either does not exist or
belongs to another user, and the server never says which.
`ctl saved-searches list` shows the ones you own.
```

#### `ctl duplicates`

The groups of photos the library thinks are the same shot (`internal/duplicates`), and the two opinions a
human can record about a pair (`internal/feedbackapi`). All of it `editor`/`admin`.

| Command | Meaning |
| --- | --- |
| `ctl duplicates list` | `GET /duplicates`; `--limit`/`--offset`, confirmed groups first |
| `ctl duplicates confirm <a> <b>` | `POST /feedback/duplicate-confirmations` — "yes, the same shot" |
| `ctl duplicates unconfirm <a> <b>` | `DELETE` the same (undo) |
| `ctl duplicates dismiss <a> <b>` | `POST /feedback/duplicate-dismissals` — "no, these are different photos" |
| `ctl duplicates undismiss <a> <b>` | `DELETE` the same (undo) |
| `ctl duplicates merge <keeper> <other>…` | `POST /duplicates/merge` — resolve the group into the keeper, **archiving the copies**; needs `--yes` |

Confirming and dismissing are **opinions**: one ranks a group up the duplicates page, the other stops
the pair being offered again. They change no photo, they are opposites, both idempotent, both
undoable, and the pair is unordered.

**`merge` is the one command here that changes the library.** Everything the other members carried —
albums, labels, the people on them — moves onto the keeper, its empty metadata fields are filled from
theirs, and the copies are **archived**. That is why it is gated like a deletion (see
[the gate](#the-irreversible-commands-and-their-gate)) even though it deletes nothing: an opinion can
be taken back, an archived photo is on the retention clock. The keeper is named first and is folded
into its own group, so the API's "the keeper must be in the group" rejection is unreachable; and
`--dry-run` is **the server's own preview** of that exact merge (`dry_run` in the request body), not a
second implementation's guess at it.

`list` prints **one row per member**, because the member uids are what `confirm` and `dismiss` take.
`DISTANCE` names the detector that measured it (`phash 2`, `cos 0.013`): a Hamming distance between
perceptual hashes and a cosine distance between embeddings are not the same number. An instance with
detection switched off answers `503`.

#### `ctl comments`

A photo's conversation (`internal/comments`). Every signed-in role may read a thread and write into one — a
comment is participation, not curation.

| Command | Meaning |
| --- | --- |
| `ctl comments list <photo-uid>` | `GET /photos/{uid}/comments` — the whole thread, oldest first |
| `ctl comments add <photo-uid> <text>` | `POST /photos/{uid}/comments` — plain text, at most 2000 characters |

**Reading matters most.** A thread is often the only record of who is on a photo, where it was taken and
when — which is exactly what dating an undated photo needs. So it renders as **prose, not a table**: one
header line per comment (`2024-05-01 10:22  Anna Nováková (usr01)  cmt1a2b3`) and the body indented whole
below it, nothing elided. A photo with no thread — and a photo that does not exist — both print
`no comments on this photo`.

**Write only under your own account.** The author is whoever the API token belongs to: the server takes it
from the authenticated principal and the audit trail records it there too. Commenting through a person's
token puts words in that person's mouth, in the one place in the library whose whole value is that it says
who remembered what. That is why the **MCP server exposes no comment tool at all**; `ctl` may have one
because an agent's account is a distinct one. The route is rate limited per user (`429`).

```bash
kukatkoctl photos similar pht01h2j3 --limit 10          # what else looks like this?
kukatkoctl duplicates list -o llm
kukatkoctl duplicates dismiss pht01h2j3 pht09z8y7       # no, two different photos of the same wall
kukatkoctl stacks group pht01h2j3 pht09z8y7             # the JPEG and its RAW, one tile
kukatkoctl edits set pht01h2j3 --rotate 90              # it was scanned sideways
kukatkoctl edits reset pht01h2j3                        # and back, thumbnails rebuilt
kukatkoctl comments list pht01h2j3                      # who remembers what about this photo
kukatkoctl saved-searches create "Nedatované" --param q="taken:unknown" --param sort=oldest
```

#### `ctl favorites` and `ctl rating`

Favorites and ratings are both **per-user**, not global: the token scopes them, not a parameter. So **even a viewer**
may change them — their own.

| Command | Meaning |
| --- | --- |
| `ctl favorites list` | `GET /favorites`; the `/photos` envelope + filters like `photos list` (without `--favorite`) |
| `ctl favorites add <uid>` | `PUT /photos/{uid}/favorite` (idempotent, `204`) |
| `ctl favorites remove <uid>` | `DELETE /photos/{uid}/favorite` (idempotent, `204`) |
| `ctl rating set <uid> [<0-5>]` | `PUT /photos/{uid}/rating`; `--flag none\|pick\|reject` |
| `ctl rating clear <uid>` | `DELETE /photos/{uid}/rating` (idempotent) |

Stars and the flag are **independent**: whatever you omit on `rating set` the server leaves alone — but you must give
at least one. `ctl favorites list` does not send a `favorite` parameter; the endpoint scopes itself.

#### `ctl bulk`

One metadata edit across many photos (`POST /photos/bulk`, `editor`/`admin`).

```
ctl bulk [<photo-uid>…] [operations…] [--yes]
```

**The whole batch goes in one request**, because the server applies it in **one transaction** — a loop
over photos would trade atomicity for N transactions and N audit rows. The server caps the batch at 1000 photos
(`413`). Uids are taken from arguments, or **from stdin** when there are none; four shapes are read from
stdin: the envelope `{"photos":[…]}` (exactly what `ctl photos list -o json` prints), a bare JSON array of uids,
a bare array of objects with `uid`, or a plain whitespace-separated list. Uids are trimmed and **deduplicated**.

| Flag | Meaning |
| --- | --- |
| `--add-album` / `--remove-album` | album uid; repeatable |
| `--add-label` / `--remove-label` | label uid; repeatable |
| `--set-caption` / `--clear-caption` | photo caption |
| `--set-description` / `--clear-description` | description |
| `--location "lat,lng"` / `--clear-location` | GPS position |
| `--favorite[=false]` | favorite (per-user) |
| `--archive` / `--unarchive` | move to trash / back |
| `--rating 0..5` | stars (per-user) |
| `--flag none\|pick\|reject` | cull flag (per-user) |

Flags whose "unset" is also a valid value (`--favorite`, `--rating`, `--flag`)
are sent **only when you actually write them** — otherwise `ctl bulk --add-label x` would silently unfavorite everything
it touches and drop the rating to zero. Mutually exclusive pairs (`--set-caption`
+ `--clear-caption`, `--archive` + `--unarchive`, …), the star range, the flag, and the coordinates are validated
**locally**, so a typo costs neither a round trip nor a rolled-back transaction.

The output is a summary (`120 photos · 118 updated · 0 skipped · 2 errored`); **only the photos that
failed are printed**. The full per-photo breakdown is in `-o json`.

#### Large batches: confirmation above 50 photos

A command that would touch **more than 50 photos** (`ctl bulk`, `ctl albums add-photos`/`remove-photos`)
asks first:

```
About to apply this edit to 120 photos, more than the 50-photo threshold. Continue? [y/N]
```

`--yes` / `-y` skips the prompt. When the uids came **from stdin**, the prompt cannot be asked — that stream already
swallowed the list of uids and there is no terminal in the pipeline to answer from. So the command **ends with an error
that asks for `--yes`**, instead of silently continuing past an unanswerable question.

**The irreversible commands are gated differently.** `ctl subjects merge` and `ctl subjects delete` never ask:
there is no size at which losing a person's name is harmless, so there is no threshold to be under and no
question a piped agent would fail to answer. They simply **refuse without `--yes`**, and offer `--dry-run`.

```bash
kukatkoctl albums create "Léto 2024" --description "prázdniny"
kukatkoctl labels attach lbl1a2b3 pht01h2j3
kukatkoctl subjects photos sub1a2b3 --limit 20
kukatkoctl favorites add pht01h2j3
kukatkoctl rating set pht01h2j3 5 --flag pick

# the whole batch in one transaction, uids straight from the listing:
kukatkoctl photos search "jezero" --limit 200 -o json | kukatkoctl bulk --add-label lbl1a2b3 --yes
kukatkoctl photos list --year 2019 -o json | kukatkoctl bulk --archive --yes
```

#### What `ctl` deliberately cannot do

Backups, restore, migrations, maintenance, the library wipe, and the job queue are **not offered over the network**. They are destructive or
long-running and belong on the machine where the instance runs — so they remain only as local subcommands
(`kukatko backup`, `restore`, `migrate`, `maintenance`, …).

**Permanent deletion is the exception, and it is a deliberate one.** `photos purge`, `trash empty`,
`trash purge-older` and `duplicates merge` are here, behind
[the `--yes`/`--dry-run` gate](#the-irreversible-commands-and-their-gate), because the person who has
to decide whether a photo goes is holding a token and a terminal. **The MCP server exposes none of
it** and must stay that way — an integration test walks `tools/list` for destructive names and fails
if one appears. That is the whole distinction: the CLI is the door for a human holding a token, MCP is
the door for an agent running unattended.

Ingesting a **directory tree** stays local too (`kukatko import dir`): `ctl photos upload` takes
files, one request, streamed — a walk over a disk the server cannot see is not something an API can do.

## Configuration keys

<!-- BODY CONFIG -->
- **Observability keys:** `log.level` (debug/info/warn/error, default info, invalid → an error at
  startup; `KUKATKO_LOG_LEVEL`), `metrics.enabled` (bool, default true; disabled → `/metrics` is
  not mounted, the request-metrics middleware is not installed, the access log keeps running; `KUKATKO_METRICS_ENABLED`)
  and `metrics.library_ttl` (duration, default `1m`; how long the **library-content gauges** are
  memoised — they are aggregates over the largest tables in the database and Prometheus scrapes on a
  fixed interval forever, so raising it on a large library only makes the counts staler, never wrong;
  a non-positive value falls back to the built-in default; `KUKATKO_METRICS_LIBRARY_TTL`).
- **Storage keys (`storage.*`, `internal/storage`):** `backend` (`fs` **default** = local disk /
  `r2` = a private Cloudflare R2 bucket; an unknown value → `ErrInvalidStorageBackend` at startup),
  `originals_path` (the originals root, `fs` only), `cache_path` (derived artifacts — thumbnails,
  video posters, and the `storyboard/` scrub-preview sprites, which live **only** here and are never
  uploaded to the bucket), `temp_path` (default `/var/lib/kukatko/tmp`; `r2` stages uploads through it
  and materializes objects for tools that only accept a file name — the **single largest
  file** must fit there, not the library). `KUKATKO_STORAGE_BACKEND`/`_ORIGINALS_PATH`/`_CACHE_PATH`/
  `_TEMP_PATH`.
- **Cloudflare R2 keys (`storage.r2.*`, read only when `storage.backend: r2`):** `endpoint`
  (`https://<accountid>.r2.cloudflarestorage.com`), `region` (R2 accepts only `auto`, default),
  `bucket` (**keep it private** — objects are served by an edge Worker that verifies the URL signature),
  `access_key`/`secret_key` (an R2 API token), `media_base_url` (the Worker's domain under which
  signed URLs are minted — `https://kukatko-media.panbotka.cz`), `url_signing_secret`
  (+ `url_signing_secret_previous`) and `url_ttl` (default `1h`, must be positive). Env:
  `KUKATKO_STORAGE_R2_ENDPOINT`/`_REGION`/`_BUCKET`/`_ACCESS_KEY`/
  `_SECRET_KEY`/`_MEDIA_BASE_URL`/`_URL_SIGNING_SECRET`/`_URL_SIGNING_SECRET_PREVIOUS`/`_URL_TTL`.
  `ErrIncompleteR2Config` validation **at startup**: the `r2` backend requires `endpoint`, `bucket`,
  `access_key`, `secret_key` and `storage.temp_path` (the missing ones are listed in the error — names
  only, never values). `media_base_url` + `url_signing_secret` are a **pair and optional as a pair**:
  set both and objects are served as signed URLs by the edge Worker (then `url_ttl` must be positive);
  set neither and nothing is signed — `R2.URL()` returns `""`, `internal/mediaurl` falls back to the
  application's own `/api/v1/photos/…` routes and the app streams the bytes itself, which is what a
  bucket with no Worker in front of it (development against a local MinIO) needs. Exactly one of the
  pair fails startup, since it means either URLs the Worker will reject or a secret that signs nothing. Neither the secrets nor the access key are ever logged or appear in an error.
  **The Worker itself is not in this repo** — the bucket, its source, bindings, and hostname are defined and deployed by
  Terraform in the infra repo (root module `cloudflare-r2/`). Rotating the signing secret therefore reaches into
  **two repositories** — procedure below.
- **⚠️ Nobody uploads thumbnails to the bucket yet.** With `storage.backend: r2` the API mints `thumb_url`
  (and the route `/photos/{uid}/thumb/{size}` redirects) to the object key
  `thumb/aa/bb/cc/<hash>_<size>.jpg`, but `thumb.Thumbnailer` writes every size **locally**
  into `storage.cache_path` — the same for both backends. `storage.Storage.Store` cannot write an object to a
  **chosen** key (it derives one from `taken_at` + the file name), so publishing the cache must come from
  a new interface method. Until it exists, an R2 deployment must mirror the thumbnails into the bucket **outside the app**
  (e.g. `rclone sync` from `cache_path`), otherwise the Worker returns a 404 for every tile. Originals and video
  are unaffected — those are written to the bucket by `Store` on import.
- **Backup keys (`backup.*`, `internal/backup`):** `backup.s3.*` describes a **second, independent
  bucket** — `endpoint`, `region`, `bucket`, `access_key`/`secret_key` and `path_style` (bool,
  default false; MinIO and most self-hosted S3 want it). It shares **nothing** with `storage.r2.*`, so
  the backup can live in a different account and even a different provider; **do not assume both are R2.** Further,
  `backup.schedule` (5-field cron / `@daily`/`@every`; empty disables the scheduler) and `backup.retention`
  (how many recent **dumps** to keep, ≤ 0 = keep all). Env: `KUKATKO_BACKUP_S3_ENDPOINT`/
  `_REGION`/`_BUCKET`/`_ACCESS_KEY`/`_SECRET_KEY`/`_PATH_STYLE`, `KUKATKO_BACKUP_SCHEDULE`,
  `KUKATKO_BACKUP_RETENTION`.
  **Where the originals come from is decided by `storage.backend`:** `fs` → `backup.DiskOriginals` walks
  `storage.originals_path` and streams the files up; `r2` → `backup.BucketOriginals` lists the
  primary bucket and has the **backup endpoint copy the object server-side** (`CopyObject` via
  `ComposeObject`, so even an object > 5 GiB goes through a multipart copy) — the payload **never flows through
  the app**, which is the whole point on a VPS whose disk cannot hold the library.
  **Consequence for permissions:** the server-side copy is sent to `backup.s3.endpoint` with the primary
  bucket as the source, so `backup.s3.access_key` must be able to **read `storage.r2.bucket`**
  (typically the same S3 service / account, or a cross-account grant). `retention` prunes **only
  the `db/` prefix** — originals **never expire** and a deletion in the primary bucket does **not
  propagate** to the backup; the copy is purely additive. **Better to fail loudly than to quietly back up
  nothing:** missing `backup.s3.{endpoint,bucket}` → `errBackupNotConfigured`, aiming the backup at the
  primary bucket → `errBackupSameBucket` (both in the wiring, `cmd/kukatko/backup.go`). A missing
  `storage.r2.bucket` is caught already by `config.Load` (`ErrIncompleteR2Config`) at startup; the sentinels
  `backup.ErrNoSourceStore`/`ErrNoSourceBucket` therefore only guard against a wiring bug inside the package.
  Object versioning **does not exist**, the second bucket is the only protection — see [`RESTORE.md`](RESTORE.md).
- **Thumbnail keys (`thumb.*`, `internal/config`):** `engine` (`go` **default** / `vips`;
  an unknown value → `ErrInvalidThumbEngine` at startup) — `vips` switches JPEG/PNG/WebP thumbnails to a
  shell-out to `vipsthumbnail` (faster/leaner on large images, **still no CGO**),
  pure-Go stays the default and the per-photo fallback; `vips_binary` (the executable on PATH, default
  `vipsthumbnail`, `vips` only); `concurrency` (max sizes encoded in parallel per photo,
  `0`=GOMAXPROCS — lower it on a memory-constrained host); `max_pixels` (`int64`, **default
  `200000000`** = 200 MP) — the decode-pipeline cap: a source whose `width×height` exceeds it is
  rejected (`imgconvert.ErrImageTooLarge`) before its RGBA bitmap is allocated, so a decompression
  bomb or an enormous panorama fails its thumbnail/pHash job instead of OOMing a worker on the shared
  box (a 30000×30000 image is ~3.6 GB; 200 MP is ~800 MB peak at ~4 bytes/pixel). The same cap guards
  thumbnail generation, the ingest-time perceptual-hash decode **and** the face detector's upright rotation
  (`facejob` rasterizes an original that still carries an EXIF orientation in order to turn it before sending it
  to the sidecar, which reads no EXIF); `0`/negative disables it.
  `KUKATKO_THUMB_ENGINE`/`_VIPS_BINARY`/`_CONCURRENCY`/`_MAX_PIXELS`. `serve` logs the active engine +
  warns when `vips` is missing on PATH. See `docs/PERF.md`.
- **Video keys (`video.*`, `internal/config`):** `transcode` (bool, **default false**) — enables
  on-the-fly transcode of non-web-friendly codecs (HEVC/H.265 …) to H.264/MP4 via ffmpeg for playback
  in the browser. Off = video is streamed as-is (with HTTP Range) and the client offers a download when the
  browser cannot decode it. Transcode is CPU-heavy, runs on every playback (not cached), and a
  transcoded stream cannot be seeked precisely — hence opt-in. `KUKATKO_VIDEO_TRANSCODE`.
- **Worker keys (`worker.*`, `internal/config` + `internal/worker`):** the in-process background
  worker that drains the job queue inside `kukatko serve`. `count` (**default 2**) sizes the **shared
  pool** — the goroutines that drain every job type *without* a `type_count` entry (`thumbnail`,
  `metadata`, `places`, `sidecar`, `storyboard`, `pp_import`, …). All of that is local CPU/IO work, so size it by the
  host's cores. `type_count` is a **map of job type → slots**: a type named there gets its **own
  dedicated pool** of that many goroutines and stops competing for the shared pool's slots. That split
  is the whole point — **`image_embed`/`face_detect`/`ocr` call the embeddings sidecar on the GPU box,
  which serves one request at a time**, and before per-type pools existed the single global slot that
  protected it also serialised every thumbnail on the box. They stay at **one slot each even when
  `type_count` does not mention them** (a YAML map *replaces* the default, it does not merge into it),
  so running several against the box is only ever an explicit entry. **`mail_send` gets its own one-slot pool
  for the same reason** — one conversation at a time with a remote mail server, and a message that never waits
  behind a backlog of thumbnails; values ≤ 0 are ignored and a type
  with no registered handler gets no pool at all (with `mail.enabled` false there is no `mail_send` handler,
  and nothing enqueues one either). `serve` logs the resulting layout at startup
  (`worker: pool "shared" draining [...] with N slot(s)`) — that line is how you confirm an override
  took effect. Also `poll_interval` (**default 2s**, the idle delay between empty claims — every pool
  polls, so more pools means proportionally more idle queries), `stale_after` (**default 5m**, the lock
  age past which a running job is presumed abandoned and requeued) and `stale_scan_interval`
  (**default 1m**). Env: `KUKATKO_WORKER_COUNT`, `_POLL_INTERVAL`, `_STALE_AFTER`,
  `_STALE_SCAN_INTERVAL`; `type_count` is a map, so it is set in YAML only (the safe defaults mean an
  env-only deployment never needs it).
- **Embedding-sidecar keys (`embedding.*`, `internal/embedding`):** `url` (default
  `http://localhost:8000`), `image_dim`/`face_dim` (**1152**/512) plus three timeouts, all built into
  every client through `embeddingClientConfig` in `cmd/kukatko`. `image_dim` is not a tuning knob:
  it must equal the width of the stored `embeddings.embedding` column, so moving it means a
  migration plus a full re-embed (that is what migration `0057` did when the sidecar swapped CLIP
  ViT-L-14 for SigLIP 2 so400m/14 and the width went 768 → 1152). `serve` therefore reads the
  sidecar's `GET /health` once at startup, in the background, and logs
  `embedding: sidecar image dimension differs from configured image_dim` naming both values when
  `clip.dim` disagrees — otherwise a mismatch only shows up as every `image_embed` job failing with
  a non-transient dimension error and semantic search quietly degrading to full text. An offline
  box (the normal state) logs nothing above debug. `dial_timeout` (**default 3s**)
  bounds *opening* the connection: the box is usually powered off, that shows up as a dial nobody
  answers, and Go's stock transport would sit on it for 30 s — this is the ceiling on what any call
  pays to find out the sidecar is not there. `request_timeout` (**default 60s**) bounds one
  image/face embedding; those are queue work on a possibly cold GPU, so it stays generous and never
  delays a request a person is waiting on. `text_timeout` (**default 5s**) bounds embedding a search
  query — the one interactive call — because search degrades to full-text when it expires and text
  results now beat semantic results later. `text_url` (**default empty**) sends *only* `/embed/text`
  to a second instance of the same service and is what makes semantic search work while the box
  sleeps: production points it at the CPU text-only container next to the app
  (`KUKATKO_EMBEDDING_TEXT_URL: "http://embeddings-text:8000"`, measured p50 0.43 s — 50× the box's
  0.0086 s and still far inside `text_timeout`, cosine agreement with the box 0.999999), while
  `/embed/image`, `/embed/face` and `/ocr/image` keep going to `url` because a text-only instance
  answers them with 503. Empty means "one host answers everything", which is what an unsplit
  deployment gets. The **health probe stays on `url`** with them: it is what `internal/wake` reads to
  decide whether to send a magic packet, and a probe pointed at an always-on instance would report a
  green light forever, leaving the box asleep and the `image_embed`/`face_detect` queue stuck behind
  it. The one probe that does follow `text_url` is the `GET /capabilities` reachability checker
  (`buildReachabilityChecker`), because that flag is about semantic search rather than about the box —
  pointed at a sleeping box it would grey the semantic mode out in the UI while the search itself
  works. Env: `KUKATKO_EMBEDDING_URL`, `_TEXT_URL`, `_DIAL_TIMEOUT`, `_REQUEST_TIMEOUT`,
  `_TEXT_TIMEOUT`.
- **Text-recognition keys (`embedding.ocr.*`, `internal/ocrjob`):** reading the text printed *in* a
  photo — a street sign, a shop front, a scanned page — so search finds the photo by what it says. It
  is served by the **same** sidecar on the same box as the embeddings above, which is why it has no URL
  of its own and inherits `request_timeout`. `enabled` (bool, **default true**): when false the feature
  is fully inert — no `ocr` handler is registered, uploads enqueue no job, and `POST /process/ocr`
  answers 503; text recognised earlier stays stored and stays searchable. `min_confidence` (**default
  0.5**, the service's own) is the per-block confidence floor: blocks the recogniser is less sure about
  are dropped before the text is assembled; a non-positive value sends nothing and lets the service
  decide. `preview_size` (**default `fit_1920`**) is the thumbnail sent to the recogniser — bigger than
  the `fit_720` used for embedding on purpose, because the image tower downsamples to a small square
  anyway while OCR needs the pixels; `fit_720` loses the small print on signs and in newspapers, which
  is the whole point. The recognised text lands in `photos.ocr_text` (searchable at the **lowest**
  full-text weight `D`, below `file_name`'s siblings and far below a real title) with `ocr_model` and
  `ocr_at`; `ocr_at IS NULL` is what the backfill reads as "never looked at", so an empty reading is
  recorded rather than skipped. Throughput on the box is ~4.4 photos/s on CUDA, so a library-wide
  backfill is a long queue drain — and the box being offline is the normal case, in which the jobs
  simply wait. Env: `KUKATKO_EMBEDDING_OCR_ENABLED`, `_OCR_MIN_CONFIDENCE`, `_OCR_PREVIEW_SIZE`.
- **Wake-on-LAN keys (`embedding.wake.*`, `internal/wake`):** `enabled` (bool, **default false** —
  the feature is fully inert), `mac` (the box's MAC, **required and parsed during validation** when enabled),
  `broadcast_addr` (the UDP broadcast target, default `255.255.255.255:9`), `interface` (the NIC for the raw
  Ethernet frame; requires CAP_NET_RAW), `min_queue` (the threshold of pending `image_embed`/`face_detect`
  jobs, default 1), `cooldown` (min. spacing between packets, default 5m). `ErrInvalidWake` validation:
  enabled requires a valid MAC + at least one target (`broadcast_addr`/`interface`).
- **Rate-limit keys (`ratelimit.*`, `internal/ratelimit`):** per-client-IP token-bucket limits on
  heavy endpoints. Sections `upload`/`bulk`/`comment`/`tiles`, each `{rate_per_sec, burst}`;
  defaults 5/30, 2/10, 0.5/10, 50/200; `rate_per_sec ≤ 0` disables the rule (middleware no-op). Env e.g.
  `KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC`. **`comment` (POST `/photos/{uid}/comments`) is keyed by the
  authenticated user**, not by IP (`Limiter.KeyedMiddleware`, mounted *inside* the auth guard so the
  principal is on the context): a household shares one address, and throttling everyone's conversation
  because one person is chatty would be wrong. Login has its own limiter (`auth.login_rate_*`), the geocode
  proxy too (`maps.*`).
- **Login keys (`auth.login_rate_limit`, `auth.login_rate_window`):** default **10 failed attempts per
  (username, client IP) within 15m**, then 429; a successful login clears the count. Every attempt is
  charged to a **second, IP-independent per-username budget** as well — `login_rate_limit × 3` over the same
  window (30/15m by default), not separately configurable. That one is what an attacker cannot walk away
  from by changing address, and it is deliberately the looser of the two: somebody mistyping their own
  password from one machine hits the per-IP limit first and never meets it, while somebody aiming at
  another person's account to lock them out has to spend three times as much to do it. Both are in-memory
  and per process, so a restart clears them. `KUKATKO_AUTH_LOGIN_RATE_LIMIT`/`_LOGIN_RATE_WINDOW`.
- **Trusted proxies (`web.trusted_proxies`, `internal/clientip`):** which peers may rename the client with a
  forwarding header. Default **`["loopback", "private"]`** = `127.0.0.0/8`, `::1`, `10/8`, `172.16/12`,
  `192.168/16`, `fc00::/7`. Entries are CIDR blocks, single addresses, or those two keywords; an unparseable
  entry fails startup with `ErrInvalidTrustedProxy`. Env: `KUKATKO_WEB_TRUSTED_PROXIES="loopback,10.8.0.0/24"`
  (comma-separated). **A list set here replaces the default, it does not extend it** — and because viper reads
  an *empty* environment variable as unset, emptying the list has to be written in YAML (`trusted_proxies: []`).
  Only a request that arrives **from** one of these networks has its `X-Forwarded-For`/`X-Real-Ip` believed;
  from anyone else the socket address wins. `True-Client-IP` and the vendor variants are **never** read, from
  anybody. The resolved address is what the rate limiters key on, what the audit trail records and what the
  access log's `remote_ip` reports — one value, so a log line and an audit row about the same request name the
  same machine.
  **Deployment:** the VPS runs the container behind Traefik on a shared Docker network, so the peer address
  the app sees is the proxy's `172.x` bridge address — already inside the `private` default, and **no config
  change is needed there**. Check it after any topology change: if `remote_ip` in the access log is the same
  address for every request, the real proxy is outside the trusted set and everyone shares one rate-limit
  bucket; if it is an address a caller could have chosen, the set is too wide. A proxy on another host, or one
  reached over the tailnet (`100.64/10`, deliberately **not** trusted by default — a tailnet carries clients,
  not proxies), has to be added explicitly. Front ends should also strip inbound `X-Forwarded-For` and set it
  themselves; with this list in place a forged one from an untrusted peer is ignored either way.
- **Maps/geocode keys (`maps.*`, `internal/config`):** `mapy_api_key` (server-side, env
  `MAPY_API_KEY`; empty → the tile/rgeocode proxy 503s, the `places` job is not registered, and `/process/places`
  returns 503), `user_agent` (see below), `base_url` (default `https://api.mapy.com`), and a reverse-geocode
  throttle for the background **`places` job** (which caches a photo's locality): `geocode_rate_per_sec`
  (default 5, ≤ 0 disables) + `geocode_burst` (default 10) — protects the monthly mapy.com credit budget,
  processing slowly is OK. `KUKATKO_MAPS_GEOCODE_RATE_PER_SEC`/`_GEOCODE_BURST`.
- **Geocode credit budget (`maps.geocode_budget`, `maps.geocode_budget_window`):** default **1000 per 24h**;
  `geocode_budget ≤ 0` removes the cap. The throttle above bounds how **fast** credits are spent, this bounds
  **how many** — a full-library import (~6k geotagged photos) would otherwise drain the whole quota in one
  pass at 5/s. When the window's credits are gone the queued `places` jobs are **deferred until the budget
  refills** (a `worker.RetryAfter` of the time left in the window, floor 1 min) — nothing fails, the queue
  simply drains over the following days, and the jobs do not churn once a minute in the meantime. Bumping
  the budget makes an import finish sooner; the count lives **in memory**, so restarting the server starts a
  fresh window (the budget guards against a runaway import, not against an operator). Env
  `KUKATKO_MAPS_GEOCODE_BUDGET`/`_GEOCODE_BUDGET_WINDOW`. Watch the spend live on `GET /system/status` →
  `geocode` (also on the **System** page, in the Maps card) or with `kukatko_geocode_credits_spent_total` /
  `kukatko_geocode_credits_remaining` / `kukatko_geocode_credits_limit`.
- **Server-side tile cache (`maps.tile_cache_bytes`, `maps.tile_cache_ttl`):** default **64 MiB**
  (`67108864`) and **24h**; ≤ 0 for either of them disables the cache. The mapy.com free tier charges **1 credit
  per tile** (250k/month), so without a cache every re-pan over an already-seen area costs again.
  **Only successful** tiles are cached (an error never — otherwise a rejected key would freeze in the map for the whole
  TTL); the `X-Tile-Cache` header reports hit/miss. `KUKATKO_MAPS_TILE_CACHE_BYTES`/`_TILE_CACHE_TTL`.
- **Map is gray?** Look at `GET /system/status` → `maps.state`: `key_rejected` means that
  mapy.com is rejecting **our** API key (expired / revoked / out of credits) — the proxy logs a WARN
  (`mapy: tile request failed`, with the status) and returns **424**; the frontend then shows a warning instead of a
  gray grid. The fix is manual: a new key in the mapy.com console → `MAPY_API_KEY`.
- **Stacks keys (`stacks.*`, `internal/config` + `internal/stacks`):** grouping several files
  of one shot (RAW+JPEG, an exported edit, a copy) under one visible photo. `enabled` (bool,
  **default true**) is the **master switch for the whole feature** — automatic detection **and** manual stacking;
  when `false` both the detection endpoint and the manual stack endpoints return **503**. `rules.*` enables the individual
  detection rules independently (they have a very different rate of false matches): `base_name` (**default true** —
  same name, different extension; the safest), `sequential_copy` (**default true** — copy/
  sequence/edit names `IMG_1234 (2).jpg` / `copy` / `-edited` folded onto the original), `unique_id`
  (**default true** — same EXIF `ImageUniqueID` / XMP `InstanceID`; very reliable where it exists)
  and `time_gps` (**default false** — same capture second AND same GPS; the loosest, wrongly merges
  burst shots). Env: `KUKATKO_STACKS_ENABLED`, `KUKATKO_STACKS_RULES_BASE_NAME`,
  `_RULES_SEQUENTIAL_COPY`, `_RULES_UNIQUE_ID`, `_RULES_TIME_GPS`. The **admin backfill** `POST
  /process/stacks` (like the other `/process/*`) runs detection over the whole library via
  `stacks.Service.DetectStacks` and returns `{created}`; the candidates are only so-far-unstacked, unarchived
  photos, so a re-run is idempotent. With `stacks.enabled: false` it responds 503.
- **Duplicate keys (`duplicate.*`, `internal/config` + `internal/duplicates`/`internal/embedjob`):**
  near-duplicate detection, the `GET /duplicates` review page and the non-blocking "this looks like an
  existing photo" warning on upload. `enabled` (bool, **default true**) is the master switch — with it off
  `GET /duplicates` answers **503** and the review game skips duplicate questions. `phash_max_diff`
  (**default 8**) — the maximum perceptual-hash Hamming distance, in bits, for two photos to be linked;
  a negative value disables pHash linking. `embedding_max_dist` (**default 0.028**) — the maximum
  embedding cosine distance for the same; `<= 0` disables embedding linking. **The distance is
  model-specific**: it was 0.05 while the image tower was CLIP ViT-L-14 and 0.028 is the SigLIP 2 value
  that flags the same number of pairs on the same photos — the measurement, and the method to repeat it
  after the next model change, are in [`THRESHOLDS.md`](THRESHOLDS.md). Env:
  `KUKATKO_DUPLICATE_ENABLED`, `_PHASH_MAX_DIFF`, `_EMBEDDING_MAX_DIST`.
- **Sidecar keys (`sidecar.*`, `internal/config` + `internal/sidecarexport`/`internal/sidecarjob`):**
  **Metadata sidecars** — a YAML file per photo next to the originals in storage (`sidecars/<original
  key>.yml`) with its metadata and curation data (caption, description, who is in the photo along with the
  face box, albums, labels, per-user favorite and rating, non-destructive edit). It exists
  so the library **survives losing the database**: curation data otherwise lives in a single place, in Postgres,
  and the S3 backup is the only mechanism — a backup that has been quietly failing for three months, you discover on the
  day you need it. `enabled` (bool, **default true**) is the master switch and is **deliberately on**:
  a recovery mechanism nobody turned on is no mechanism at all. When `false` nothing is written or
  deleted, no `sidecar` job is enqueued (the handler is not registered, so a job would hang forever in the
  queue), and `POST /process/sidecars` responds 503; **the sidecars already in storage stay exactly as they
  are** — turning the export off is not a request to destroy what it already wrote, and a stale sidecar is worth
  more than none. Turn it off when the I/O is not worth it to you: it is one small write per photo per edit,
  against a store that may charge per request. Env: `KUKATKO_SIDECAR_ENABLED`. Unrelated to
  `internal/sidecar`, which reads *foreign* sidecars (Google Takeout, Apple XMP) on import. **The full
  format is in [`docs/RESTORE.md`](RESTORE.md)**; backfill `kukatko sidecar backfill [--all]` or
  admin-only `POST /process/sidecars`.
- **MCP keys (`mcp.*`, `internal/config` + `internal/mcpapi`):** the **MCP server** — the library exposed
  to an AI agent (Model Context Protocol) at `POST /api/v1/mcp`, so it can search, read, and organize within it
  ("find all photos of grandma from the sixties and put them in an album"). `enabled` (bool, **default false**) is
  the **master switch and is deliberately off**: the endpoint is a new attack surface, so it is **opt-in** — and when
  `false` the route is **not mounted at all** (`RegisterRoutes` registers nothing), so the path **does not exist**,
  rather than returning 403; a 403 would still reveal that the endpoint is there. `page_size` (**default 25**) —
  how many rows a list tool returns when the agent gives no limit; `max_page_size` (**default 100**) — a hard cap
  on the `limit` argument (a larger request is **truncated**, not refused). Both are deliberately small: the scarce resource
  is the **agent's context window**, not the database. A non-positive value for either falls back to the default. Env:
  `KUKATKO_MCP_ENABLED`, `KUKATKO_MCP_PAGE_SIZE`, `KUKATKO_MCP_MAX_PAGE_SIZE`.
  **Auth:** no new mechanism — it sits behind the same `RequireAuth` and the same RBAC as the rest of `/api/v1`;
  the agent authenticates with an **API token** (`Authorization: Bearer kkt_…`) and the **owner's role** decides
  (the token has no role of its own): `viewer` = read only, `editor`/`admin`/**`ai`** = write too.
  **The token for the agent** is minted by the user **for themselves** — an admin creates a user with the `ai` role
  (`POST /api/v1/admin/users`), that user logs in (`POST /api/v1/auth/login`) and mints a token
  (`POST /api/v1/auth/tokens`); the plaintext `kkt_…` is shown **once**. **Nothing destructive is
  exposed** (no deletion, purge, trash, archiving, restore, backup, user management) and **every mutation
  writes an audit row in its own transaction**, with `"via": "mcp"` in details. The full tool list, the auth model,
  and what is deliberately missing: [`docs/MCP.md`](MCP.md).
- **Location estimate keys (`location_estimate.*`, `internal/config` + `internal/geoestimate`):**
  estimating the location of GPS-less photos from photos taken close in time. `enabled` (bool, **default true** — a full
  map and a usable place hierarchy is what most libraries want; disabling it is one key away,
  because inferring data is exactly the kind of helpfulness someone may not want): when `false` **nothing**
  is ever estimated and `POST /process/locations` returns **503**; already-estimated locations remain, marked so
  the user can accept or delete them. `window` (duration, **default 6h**) is the **half-width** of the
  neighbor window — a photo is estimated from photos taken ±window from it; the same calendar day is the obvious choice, a few
  hours is better (a day that starts in Brno and ends in Vienna is exactly the case where a same-day estimate is
  wrong). `radius_meters` (float, **default 5000**) is the **coherence radius**: the neighbors are trusted only
  when **each** of them lies within this distance of their centroid — otherwise the photo stays without a location.
  Both levers **err toward rejection** and rightly so: a wrong location quietly poisons the map,
  the place hierarchy, and every `near:` search over them, and widening the radius beyond the size of a single trip is
  a bad trade (there is no value at which a day between Prague and Vienna becomes honest). An enabled
  estimator with a non-positive `window`/`radius_meters` **does not pass startup** (`ErrInvalidLocationEstimate`) —
  better to refuse to boot than to look enabled and never produce anything; for a disabled one the values are
  not checked. Env: `KUKATKO_LOCATION_ESTIMATE_ENABLED`, `_WINDOW`, `_RADIUS_METERS`. The **admin
  backfill** `POST /process/locations` → `{estimated}` is the only way an estimate is created (there is no estimation on upload
  — a fresh photo has no neighbors from the same day yet). Every new estimate gets a `places`
  job, so it propagates into the place hierarchy; **the geocode is metered**, it runs through the existing
  `maps.geocode_rate_per_sec` limiter *and* the `maps.geocode_budget` cap, so a large backfill feeds the
  geocoder in drips instead of swamping it and cannot outspend the daily budget — count on **1 mapy.com
  credit per estimated photo**. A re-run is idempotent and
  **an estimate deleted by the user never comes back**.
- **Candidates keys (`candidates.*`, `internal/config` + `internal/candidates`):** tunes the search for
  "a person among untagged photos" (`POST /subjects/{uid}/candidates`). `max_distance` (**default
  0.5**) — the default max cosine distance of a candidate from an exemplar when the request does not send it, **and**
  the baseline the vote rule scales against; `search_limit` (**default 1000**) — how many nearest
  unassigned faces the kNN of each exemplar returns before voting (bounds the fan-out per exemplar);
  `min_face_px` (**default 32**) — the minimum face width in **display pixels** for it to be
  reviewable (a tiny face in a crowd cannot be judged; complements the relative floor taken from
  `faces.min_face_size`); `concurrency` (**default 8**) — how many exemplar kNNs run at once, so searching for
  a person with hundreds of photos does not fan out unbounded; `max_exemplars` (**default 500**) — how many of a
  subject's faces seed the kNN, read as an even-strided sample **in SQL** above that, so a person tagged on
  thousands of photos costs the same as one tagged on hundreds (the vote rule clamps at five agreeing exemplars,
  so the recall cost is small); `max_candidates` (**default 500**) — how many voted candidates are hydrated into
  full photo records (EXIF blob included) and returned, nearest first, with the cut reported as `capped` on the
  response instead of applied silently. The last two are **memory bounds**: without them one request grows with
  the library, and a subject holding 16 532 exemplars once grew the process to 10.9 GB and the host OOM killer
  took the whole VPS down with it — see [`docs/PERF.md`](PERF.md) §3. A non-positive value for any key falls back
  to the default (for `min_face_px` it disables the absolute floor). Env: `KUKATKO_CANDIDATES_MAX_DISTANCE`,
  `_SEARCH_LIMIT`, `_MIN_FACE_PX`, `_CONCURRENCY`, `_MAX_EXEMPLARS`, `_MAX_CANDIDATES`.
- **Sweep keys (`sweep.*`, `internal/config` + `internal/sweep`):** tunes the **recognition sweep**
  (`GET /faces/sweep`), which composes the candidates search across all people at once. `concurrency`
  (**default 4**) — how many subjects are scanned **at once**; it **stacks** on `candidates.concurrency`
  (exemplar kNNs per subject), so on a RAM-constrained box keep it small. `max_subjects` (**default
  500**) — a cap on how many subjects one sweep scans; on overflow it scans the first `max_subjects`
  (by name) and marks the result `capped` instead of a silent truncation. A non-positive value → default. The sweep
  **never auto-confirms** — the confidence only narrows the list. Env: `KUKATKO_SWEEP_CONCURRENCY`,
  `_MAX_SUBJECTS`.
- **Expand keys (`expand.*`, `internal/config` + `internal/expand`):** tunes **collection expansion**
  "find photos similar to an album / label" (`GET /albums/{uid}/similar`, `GET /labels/{uid}/similar`).
  `max_distance` (**default 0.20**, the UI shows it as 80 % similarity; it was 0.30 under CLIP ViT-L-14 and
  was re-derived for SigLIP 2 — the measurement is in [`THRESHOLDS.md`](THRESHOLDS.md)) — the default max cosine distance
  of a candidate from the source photo when the request does not send it, **and** the baseline for the vote rule; `limit` (**default
  50**) — the default number of returned candidates; `max_limit` (**default 200**) — a cap on the `?limit` request;
  `search_limit` (**default 200**) — how many nearest photos the kNN of each source photo returns before
  voting (an over-fetch, so later filters do not starve); `source_cap` (**default 500**) — a cap on how many
  members are used as query vectors, a huge album is **sampled** (deterministically, evenly across
  the members) and the cap is **reported** (`source_capped`) instead of a silent truncation; `concurrency` (**default 8**) — how many
  kNNs per source run at once. A non-positive value for any key falls back to the default. Expansion is
  **read-only** — adding the found photos goes through `POST /photos/bulk`. Env:
  `KUKATKO_EXPAND_MAX_DISTANCE`, `_LIMIT`, `_MAX_LIMIT`, `_SEARCH_LIMIT`, `_SOURCE_CAP`, `_CONCURRENCY`.
- **Review keys (`review.*`, `internal/config` + `internal/review`):** tunes the **review game**
  (`GET /review/queue`, `POST /review/answer`) — one question at a time over candidates the
  system is unsure about.
  `kind_shares.{face,label,place,duplicate,outlier}` (**default 1 / 0 / 0 / 0 / 0**) — **what the
  game is about**. One weight per question kind; only the ratios matter, so `19 / 1` and
  `0.95 / 0.05` say the same thing, and every kind is defaulted so setting one share alone means
  what it looks like. A kind at zero is switched off *entirely*: it is never scanned, so it costs a
  rebuild nothing, and it is not a source the empty-queue reason can name — a game configured down
  to faces that has run out of faces says `no_people`, not `no_people_no_labels`. The default
  leaves the queue to faces because that is what the game is for; restoring the mix it started as
  is `face: 0.95, label: 0.05`, and the `place` / `duplicate` kinds still need their own
  subsystem switched on as well (`location_estimate.enabled`, `duplicate.enabled`). A set where
  every weight is non-positive falls back to faces alone rather than to a game that can ask
  nothing. **This is also what pays for the wider `face_budget` below:** measured on a test library
  of 105 named subjects and 20 labels of 20 members, a cold rebuild went from **375 ms** (8 face
  kNN + 6 label similarity fan-outs) to **141 ms** (10 face kNN, no label scan), while the widened
  face window costs at most **+26 ms** — 47 ms → 73 ms — in the worst case where a whole window of
  subjects turns out to have nothing left to ask about.
  `band_min` / `band_max` (**default 0.45 / 0.75**) — the **uncertainty band**: a
  candidate with confidence (= 1 − cosine distance) in `[band_min, band_max)` is a hard question, where a human
  answer teaches the system the most; below `band_min` the guess is noise and nothing is ever asked.
  An invalid band (outside (0,1), min ≥ max) falls back to the default **pair**.
  `sure_min` / `sure_share` (**default 0.80 / 0.70**) — the **confident tier and its share of a batch**.
  A candidate at or above `sure_min` is one the answer to is almost certainly yes, and `sure_share` of every
  batch is drawn from there, the rest from the band; the ratio holds in any *prefix* of the queue, so a batch
  never opens with a run of hard questions. `sure_min` is clamped up to `band_max` so the tiers cannot overlap
  (setting the two equal leaves no confidence between them that nothing asks about); a `sure_share` outside
  (0, 1) falls back to the default.
  **Do not raise `sure_share` much above 0.85.** The point of the game is confirmed assignments per minute of
  human attention, but a game that is 95 % "yes" turns the player into a rubber stamp who stops looking, and
  wrong assignments then enter the library through the very feature meant to clean it up — the minority of hard
  questions is load-bearing. Running out of one tier fills from the other, and a rebuild whose window came back
  empty rotates to the next one (up to three, inside the one `build_timeout`), so an empty queue means a
  genuinely empty library rather than an exhausted tier. `queue_size` (**default 20**) —
  how many questions one rebuild gathers into the **pool** a round is mixed from. It is
  material, not a response length: several rounds come out of one pool, so the expensive
  vector searches run once per pool rather than once per round.
  `round_size` (**default 10**) — how many questions **one round** holds, and therefore how
  long a queue response is: **one request is one round**. Ten is short enough that finishing
  a round is a decision a player makes several times an evening; raise it for longer
  sittings, lower it for a game played in the gaps. A request may still send its own
  `?limit` (cap 100), which sizes that round.
  `round_max_per_entity` (**default 3**) — how many questions about one person or one label
  a **single round** may hold. It is the round's own variety cap, deliberately tighter than
  `max_per_entity`: that one bounds the pool a rebuild gathers, this one bounds what a
  player actually sits through. Both are **preferences, not walls** — a library with one
  named person still gets a full round, because a round that came back empty rather than
  repetitive would be worse.
  `cache_ttl` (**default 60s**) — how long a built queue is served from the per-user cache before the
  expensive vector searches run again (answers edit the queue in-place, the session counter is cheap).
  `max_labels` (**default 200**) — a cap on how many labels one rebuild considers. `label_concurrency`
  (**default 2**) — how many label-similarity searches run at once (each already fans out internally; on a
  RAM-constrained box keep it low). `face_budget` / `label_budget` (**default 24 / 6**) — how many named
  subjects and how many labels **one rebuild may scan**. These are the bound that keeps the endpoint off the
  library's growth curve: building the queue used to run the whole recognition sweep inside the request, which
  on 105 named subjects took 250 s and meant `/review` never loaded (`docs/PERF.md` §3). A rebuild stops
  earlier still once the batch is full, and the cursor rotates, so successive rebuilds walk the rest of the
  library; raise a budget for a broader mix of people or labels per batch at the cost of a slower first load.
  The face budget was **8 until the kind shares arrived**, and the reason it is now 24 is variety rather than
  coverage: most subjects have nothing unassigned left in the band, so a window of eight regularly produced
  candidates from one or two people — and a pool drawn from two people cannot be mixed into a round that does
  not keep coming back to them, whatever the variety rules say.
  `build_timeout` (**default 15s**) — the hard cap on one rebuild, the backstop behind both budgets: a rebuild
  that runs out of time serves what it has (logged as `review: queue rebuild hit its deadline`) rather than
  holding the request open. `max_per_entity` (**default 4**) — **the variety knob**: how many questions about
  one person or one label may enter a batch. Ordering by informativeness alone let one label matching half the
  library supply the whole batch (measured: 19 of 20 questions about the same label, 11 of them in a row), so a
  batch takes at most this many per entity and never asks about the same one more than **twice in a row** while
  another entity still has a question waiting. With the default batch of 20 that forces a rebuild to draw on at
  least five different people or labels — lower it for more variety at the cost of a costlier rebuild (a batch
  must be filled from more sources), raise it for the opposite. The share is counted **across the kinds**: a
  person's unnamed faces and their outliers are two searches and one person, and capping each search on its own
  let the game ask about them twice as often as this promises. The "twice in a row" limit is likewise a
  **refusal, not a preference** — it used to be priced below `round_max_per_entity`, so once every person in a
  round had had their three questions the cheapest next question was whichever also continued a run, which is
  how one person got asked about five times running. It stands down only for a pool that has run down to a
  single person, because a library with one person left in question still has to be playable.
  `outlier_budget` / `outlier_threshold` (**defaults 4 / 0.5**) belong to the **outlier check** — the question
  "is this really X?" over a face already assigned to X but sitting far from X's centroid. Ranking one person
  loads every face assigned to them and scores it against a trimmed centroid, which is the most expensive
  per-person read the game does, so the window is smaller than `face_budget`; the cursor rotates, so successive
  rebuilds cover everybody. The threshold is how far a face must sit (cosine distance) before it is worth
  asking about: two people's embeddings sit around 1.0, so 0.5 is comfortably past "a bad photo of the right
  person". Lower it to hear about borderline assignments too — at the cost of a game that mostly asks about
  correct ones, which is precisely how a player learns to answer yes without looking. The other two new
  question kinds have no budget keys of their own: on top of their `kind_shares` weight the **place check**
  follows `location_estimate.enabled` (no estimator, no estimates to rule on) and the **duplicate check**
  follows `duplicate.enabled` and its thresholds, the same switch that makes `GET /duplicates` answer 503 — so
  the game and the page can never disagree about whether duplicates exist.
  `skip_mute_threshold` / `skip_mute_cooldown` (**defaults 3 / 168h**) are **"I don't know", remembered**. A
  skip on a face question is written to `review_skips` per (user, subject, photo); the threshold is how many of
  them about one person quiet that person for the player who skipped them, and the cooldown how long the first
  quiet lasts. Two skips are forgiven, because the first couple are far more often an unclear photo than a face
  somebody genuinely cannot place. The mute is a **pause, not a verdict**: past the cooldown the game tries
  once more, but only on a face that player has never been shown (the photos they gave up on stay silent for
  good), a photo the library gained *after* the mute is never suppressed, and every further skip doubles the
  wait — capped at a year — so a person somebody will never place is asked about ever less often rather than
  every week for ever. It is strictly **per user**, and it reaches nothing in the catalogue: a skip never
  becomes a face rejection, never narrows the candidate search or the sweep, and is deliberately absent from
  the audit trail. Every rebuild logs the result at debug level
  (`review: queue rebuilt` with `questions`/`entities`/`longest_run`/`sure`/`band`/`took`). Apart from the budgets, the
  review does not take the face side with its own keys — it runs through sweep/candidates and their
  `sweep.*`/`candidates.*` limits (**and the memory bound lives there**: the queue asks for the whole window
  from the confident tier down to `band_min`, so `candidates.max_exemplars`/`max_candidates` and the
  cut-before-hydration order are what keep one rebuild off the library's growth curve — see `docs/PERF.md` §3).
  There is no per-label config key: taking a single label out of the game is a **per-label switch on the labels
  page** (`labels.review_enabled`, default on), not an operator setting. A non-positive value for any key falls
  back to the default. Env:
  `KUKATKO_REVIEW_BAND_MIN`, `_BAND_MAX`, `_SURE_MIN`, `_SURE_SHARE`, `_QUEUE_SIZE`, `_CACHE_TTL`,
  `_MAX_LABELS`, `_LABEL_CONCURRENCY`, `_FACE_BUDGET`, `_LABEL_BUDGET`, `_BUILD_TIMEOUT`, `_MAX_PER_ENTITY`,
  `_KIND_SHARES_FACE` (and `_LABEL`/`_PLACE`/`_DUPLICATE`/`_OUTLIER`), `_SKIP_MUTE_THRESHOLD`,
  `_SKIP_MUTE_COOLDOWN`.
- **Mail keys (`mail.*`, `internal/mailer` + `internal/mailjob`):** outgoing transactional mail — the messages
  sent around accounts (a registration was received, an account was approved, an administrator has somebody to
  approve, a password reset). Delivery always goes **through the job queue** (`mail_send`, one worker slot), so
  a request never waits on the SMTP server and a message enqueued while it is unreachable is delivered once it
  is back; a permanently rejected recipient is parked in `failed` instead of being retried. `enabled` (bool,
  **default false**) is the master switch: with mail off no `mail_send` job is enqueued at all and no handler is
  registered, which is a working instance rather than a broken one (and the **no-op sender** covers any direct
  `mailer.Sender` caller). `host` + `port` (**default 587**, the submission port) name the SMTP server;
  `username`/`password` are **optional** — an empty username skips the `AUTH` command entirely, which is
  what an unauthenticated relay on localhost wants. `encryption` is `starttls` (**default**, port 587),
  `tls` (implicit TLS, port 465) or `none` (a local relay only: the standard library refuses password
  authentication over an unencrypted connection to anything but the local host); an unknown value →
  `ErrInvalidMailEncryption` at startup. `from_address` + `from_name` are the visible sender, `base_url`
  is **this instance's public URL** and the base of every link inside a mail, and `timeout`
  (**default 15s**) bounds one delivery attempt — connect, negotiate and send. **Validation at startup:**
  an *enabled* mailer with an empty `mail.host`, `mail.from_address` or `mail.base_url` fails with
  `ErrIncompleteMailConfig` listing **every** missing key at once (names only — `mail.password` never
  reaches the error or a log); a *disabled* one is never checked, so an instance that sends no mail need
  not mention a single key. Messages are `text/plain; charset=utf-8` with RFC 2047 encoded subjects, and a
  recipient in the reserved `.invalid` domain (the placeholder addresses in the user table) is **refused,
  never dialled**. Env: `KUKATKO_MAIL_ENABLED`/`_HOST`/`_PORT`/`_USERNAME`/`_PASSWORD`/`_ENCRYPTION`/
  `_FROM_ADDRESS`/`_FROM_NAME`/`_BASE_URL`/`_TIMEOUT`.

### `maps.user_agent` — restricting the mapy.com key to a User-Agent

`maps.user_agent` (env **`KUKATKO_MAPS_USER_AGENT`**, default **empty**) is the exact `User-Agent`
the `internal/mapy` client sends on **every** upstream request — both tiles and (r)geocode. An empty
value = no explicit header is sent (the Go default `Go-http-client/2.0` applies), so an
instance that does not set the key works unchanged.

The mapy.com console can restrict a key **either** to source IPs, **or** to a User-Agent — always only one type
of restriction at a time. IP restriction is fragile here (both the public IPv4 and the ISP-delegated IPv6 prefix change and
the key then returns `403` → gray tiles), and because the key is purely server-side, we use a **User-Agent
restriction**. mapy.com requires an **exact match** (no wildcards).

**The value is a second secret, not cosmetics:** it contains a random token, so a leaked API key alone
is useless without the correct User-Agent. That is why we **never** log it, commit it, or put it into
`config.example.yaml` (only a placeholder is there) — the same regime as `mapy_api_key`. The real value
lives in the gitignored `.secrets/db.env`.

Switch-over procedure (the order matters — the restriction is switched atomically in the console):

1. Deploy a build that sends the header, and set `KUKATKO_MAPS_USER_AGENT` in the instance's environment.
2. Restart the instance (the value is read at startup).
3. Only then, in the mapy.com console, switch the key from an IP restriction to a User-Agent restriction with the same
   value.

We do not add a `Referer` — for it mapy.com verifies only the host+port of the header we send ourselves; without
a browser it is a self-declaration with no value.

### Rotating `url_signing_secret` (a procedure across two repositories)

Kukátko **signs** media URLs, the edge Worker **verifies** them. The Worker lives in the **infra repo**
(root module `cloudflare-r2/`, deployed by Terraform), so the **same value** must be
configured on both sides: here as `storage.r2.url_signing_secret`, there as a `secret_text`-type
binding on the Worker. **Both** secrets are verified at once (`url_signing_secret` +
`url_signing_secret_previous`), signing is always with the current one — so the rotation has no window
of broken URLs, **if the order is kept**:

1. Move the existing `url_signing_secret` value into `url_signing_secret_previous` — on both
   sides (Kukátko and the Worker in the infra repo).
2. Put the new value into `url_signing_secret` — again on both sides.
3. Deploy **both** sides (`terraform apply` in `cloudflare-r2/`, restart Kukátko). Their order
   does not matter: as long as both know the old and the new secret, a URL signed by either verifies.
4. Wait until the **last already-issued URL** expires — that is, at least `url_ttl` (default **1 h**) from the moment
   Kukátko stopped signing with the old secret.
5. Only then discard the old value: empty `url_signing_secret_previous` on both sides
   and deploy again.

The shortcut through steps 1–2 (overwriting `url_signing_secret` without saving the old value into `_previous`)
**403s every photo** for which a browser or an API response already holds a signed URL. The signing
contract itself (the message `"<key>\n<expiry>"`, HMAC-SHA256, hex) is frozen in the golden vectors
`internal/storage/testdata/url_signature_vectors.json`; both sides are tested against them, so the
algorithm cannot be changed in just one of them.

## Prometheus metrics

`GET /metrics` (namespace `kukatko`, mounted outside `/api/v1` when `metrics.enabled`) exposes four
groups. It is **unauthenticated** — restrict it at the network layer — so it deliberately carries only
instance-wide aggregates: nothing per-user, and no name of a photo, album, label or person ever
becomes a label value.

- **Request and worker instrumentation** (event-driven, recorded as things happen):
  `kukatko_http_requests_total{method,route,status}` + `_request_duration_seconds` + `_inflight_requests`
  (the route label is the chi route *pattern*, never a raw URL), `kukatko_jobs_started_total{type}` /
  `_finished_total{type,outcome}` / `_execution_duration_seconds{type,outcome}`,
  `kukatko_embedding_request_duration_seconds{operation,outcome}` + `_service_up`,
  `kukatko_thumbnail_generation_duration_seconds` and `kukatko_geocode_credits_spent_total`.
  `kukatko_import_run_photos{source,outcome}` — the tally of a run in progress — was removed with the
  migration importers that checkpointed it; the last-run gauges below are unaffected.
- **Infrastructure, sampled at scrape time:** `kukatko_db_pool_*` (live pgx pool stats),
  `kukatko_jobs_queue_depth{state}` / `_by_type{type}` / `_by_type_state{type,state}` (all three folded
  from **one** `GROUP BY type, state` — the two one-dimensional families are sums over the third, so
  they can never disagree) and `kukatko_geocode_credits_remaining` / `_limit`.
- **Library content**, memoised for `metrics.library_ttl` (default 1 m) over the same aggregation the
  admin dashboard reads (`internal/system`, one SQL statement), so the gauges and `GET /system/stats`
  cannot disagree and a scrape does not re-count the catalogue:
  `kukatko_library_photos{media_type="image|video|live"}` (the total is `sum()` over the label),
  `_photos_archived` (the trash), `_photos_processed{stage="embedding|faces|places"}` and
  `_photos_pending{stage=…}` (the processing coverage and its gap — note the `faces` gap also counts
  photos that genuinely contain no face, which the counts cannot tell apart), `_embeddings`, `_faces`,
  `_markers{state="assigned|unassigned"}`, `_subjects{type}`, `_albums{type}`, `_labels`, plus
  `kukatko_library_collect_errors_total` (a failed aggregation exports **no** library gauges for that
  scrape — a gap, not a stale number — and bumps this counter, so alert on it).
  RAW originals are deliberately **not** a media type here: RAW-ness is a per-file property judged by
  extension (`internal/stacks`), not a column, so counting it would mean an unindexed scan on every
  refresh; use `media_type`, which is what the catalogue actually models.
- **Import history** (same memoisation): `kukatko_import_last_run_status{source,status}` — 1 for the
  status the last run of that source is in and **0 for every other known status**, so a transition is
  visible instead of a series silently vanishing — plus
  `kukatko_import_last_run_start_timestamp_seconds{source}` and `_finish_timestamp_seconds{source}`
  (absent while a run is still going). Both are Unix seconds: express the age as `time() - <gauge>`
  rather than exporting a pre-computed one.

The gauges carry no `_total` suffix on purpose (it is reserved for counters, so `rate()` over a name
stays meaningful). A queue-query or library-aggregation failure drops only its own families and never
fails the whole scrape.

## Make targets and CI/CD

<!-- BODY MAKE -->
- **Make targets:** `fmt` (golangci-lint fmt + Prettier `--write` — the **only target that changes
  files**), `fmt-check` (`golangci-lint fmt --diff` + Prettier `--check`, read-only),
  `vet` (standalone; `check` does not run it, because `.golangci.yml` has `default: standard`,
  so `golangci-lint run` already includes `govet`), `lint` (golangci-lint + ESLint),
  `lint-fix`, `typecheck` (`tsc -b --noEmit`), `test` (Go unit `CGO_ENABLED=0` without `-race`
  + Vitest — shares the build cache with `build`), `test-race` (`CGO_ENABLED=1 go test -race ./...`,
  requires cgo/gcc; runs in CI, not in the gate), `test-integration` (tag `integration` +
  `KUKATKO_TEST_DATABASE_URL`, `-p 1` — the integration packages share one test DB, so they run
  serially; the `integration` tag also selects the cheap bcrypt work factor
  (`KUKATKO_TEST_BCRYPT_COST`, default `bcrypt.MinCost` — see `docs/DEVELOPMENT.md`), without which
  the seeded accounts of ~15 packages dominated the suite; `-timeout 30m` because Go's 10m
  per-package default is too tight for a run at the production cost — `internal/auth` alone took
  ~11 minutes on the ARM dev box — and the default aborts a healthy run mid-test as if it failed;
  the R2-backend tests additionally want `KUKATKO_TEST_S3_ENDPOINT` — without it they are skipped,
  see `docs/DEVELOPMENT.md`), `check` (the gate = `docs-budget` + `fmt-check` + `lint` +
  `web-typecheck` + `test`; **rewrites nothing**, after a successful run `git status --short` is
  empty), `check-box` (that same gate, executed on the build box — see below),
  `build` (frontend build + `CGO_ENABLED=0` → `bin/kukatko`), `dev` (smart rebuild + run on
  `:6480` via `scripts/dev.sh`, `DEV_ARGS=--force` for a full rebuild), `dev-storage`
  (`scripts/dev-storage.sh` — the local MinIO the dev runtime *and* the S3 integration tests share:
  container `kukatko-minio`, named volume `kukatko-minio-data`, `--restart unless-stopped`, 1 GB cap,
  API `127.0.0.1:18100` + console `:18101`, buckets `kukatko-dev` and `kukatko-test*`; idempotent,
  and `--env` prints the block `.secrets/db.env` needs — see `docs/DEVELOPMENT.md`), `clean`, `help`.
  Outside `make`: **`scripts/icons.sh`** re-renders the app identity (PWA icons, favicons,
  `apple-touch-icon.png`, `favicon.ico`) from the two committed source SVGs in `web/public/icons/`
  using headless Chromium plus ImageMagick, and is run **by hand** after editing either SVG — the
  outputs are committed, so no build step ever generates or downloads an asset.
  Frontend-only targets: `web-deps` (`npm ci`, guarded by the stamp file
  `web/node_modules/.kukatko-npm-ci-stamp` that depends on `web/package-lock.json`, so it is
  reinstalled only when the lockfile changes), `web-build`, `web-fmt`, `web-fmt-check`, `web-lint`,
  `web-typecheck`, `web-test`.
  You inject the version with `make build VERSION=x.y.z`. The frontend needs **Node.js 22+**.
- **CI/CD and packaging:** `.github/workflows/ci.yml` (push/PR → job `check` = `make check`
  + `make test-race` on Go 1.26 + Node 22 + golangci-lint v2.11.4; job `integration` = `make test-integration`
  against the service container `pgvector/pgvector:pg17`, extensions `vector`/`unaccent` in a setup
  step + apt `ffmpeg`/`libimage-exiftool-perl` (video probe/poster), `KUKATKO_TEST_DATABASE_URL`
  pointing at an ephemeral CI DB). `.github/workflows/release.yml`
  (tag `v*.*.*`) runs **goreleaser** (`.goreleaser.yaml`): `CGO_ENABLED=0` for arm64+amd64,
  version/commit via ldflags into `internal/version`, frontend build in the before-hook, **.deb**
  via nfpm. **Provenance:** goreleaser validates the git state *before* its before-hooks, so
  whatever they leave behind used to ship unnoticed — the Vite build deleted the tracked
  `internal/web/static/dist/.gitkeep` and Go's VCS stamping wrote `v0.8.0+dirty` into every
  published binary. The Vite build now restores that placeholder itself (`web/build/gitkeep.ts`),
  and the last before-hook, **`./scripts/assert-clean-tree.sh`** (runnable by hand), fails the
  release if the tree is dirty for any other reason. Nothing disables VCS stamping: a binary built
  from a genuinely modified tree is still stamped `+dirty`, which is again a signal that says
  something. `deb/`: `kukatko.service` (systemd, user `kukatko`, `EnvironmentFile`,
  `Restart=always`), `kukatko.env` (dpkg conffile `config|noreplace`),
  `postinstall.sh`/`preremove.sh`/`postremove.sh` (user + `/var/lib/kukatko/{originals,cache}`).
  Apt deps: `libimage-exiftool-perl`, `libheif-examples|libheif-bin`, `dcraw`, `ffmpeg`,
  `postgresql-client`, `ca-certificates`; **no texlive**.

### `make check-box` — the gate on the build box

`make check` on the Pi is dominated by work four cores cannot parallelise away — ESLint and the
2734-test Vitest suite. Measured on 2026-08-12 with warm caches: **434 s on the Pi, 66 s on the
build box** (`ssh box`, 24 threads, 62 GB), 114 s for the very first run including the toolchain
bootstrap. `make check-box` (`scripts/check-on-box.sh`) syncs the working tree there and runs
the same target remotely, streaming the output back.

- **It is an accelerator, not a second gate.** The script runs `make check` — the whole target,
  never a subset — and **exits with the remote exit code**, so a red gate on the box is a red
  gate locally. `make check` keeps its meaning as the binding gate.
- **What is synced:** the working tree, *including uncommitted work* (`rsync -a --delete`, so a
  file deleted here disappears there). **`.secrets/` is never synced** — the gate is unit tests
  only, so it needs no credentials, and the box has no business holding this instance's DSNs or
  API keys. Also excluded: `.git/`, `bin/`, `.devdata/`, `.shots/`, `web/node_modules/`, the Vite
  and embed build outputs (`internal/web/static/dist/.gitkeep` is re-included — without a file
  there `//go:embed all:dist/*` does not compile), `*.local.yaml` and `.env*`. About 20 MB
  initially, a delta afterwards.
- **Concurrency.** Several Claude sessions share this Pi and may run the target at once, often
  from the same checkout, so the remote directory must not be a single shared path. Each run
  claims one of `KUKATKO_BOX_SLOTS` workspaces (`~/.cache/kukatko-check/<origin-host>-wsN/src`)
  through an `flock` held for its whole life; with all of them busy it waits rather than
  clobbering a neighbour. The lock lives on the Pi because that is where every run starts, and
  the workspace name carries the origin host so another machine gets its own set. Two runs
  launched together took `pi-ws1`/`pi-ws2` and finished green in 75 s and 99 s.
- **One-time bootstrap, done by the script.** The box has Node 22 but no Go and no
  golangci-lint. The script installs the Go minor version from `go.mod` (resolved to the current
  patch release via `go.dev/dl/?mode=json`) and the golangci-lint version pinned in
  `.github/workflows/ci.yml` into `~/.cache/kukatko-check/toolchain`, under a lock so two
  workspaces cannot race into a half-extracted `GOROOT`, and runs `npm ci` in the workspace.
  Re-runs reuse all of it: the Go module/build caches and the npm cache are shared across
  workspaces, `node_modules` lives in the workspace and survives the sync. `rm -rf
  ~/.cache/kukatko-check` on the box is a complete uninstall.
- **The box is not always on.** The script sends a wake-on-LAN packet (`boxon`) and polls SSH
  for `KUKATKO_BOX_WAKE_TIMEOUT` seconds (default 300). It never hangs: on timeout it says the
  gate did *not* run and that `make check` is the local fallback, and exits non-zero.
- **Integration tests stay local.** Their database lives on the Pi, so `make test-integration`
  has no remote equivalent and the script says so at the end of a green run.
- **Environment:** `KUKATKO_BOX_HOST` (default `box` — `~/.ssh/config` sets the user; the
  `ssh box@box` form from the global CLAUDE.md does not authenticate), `KUKATKO_BOX_ROOT`
  (remote cache dir relative to the remote `$HOME`, default `.cache/kukatko-check`),
  `KUKATKO_BOX_SLOTS` (default 4), `KUKATKO_BOX_WAKE_TIMEOUT`, `KUKATKO_BOX_LOCK_WAIT`
  (default 1800 s of waiting for a busy workspace).

## Docker image — container build and publishing to GHCR

<!-- BODY DOCKER -->
Alongside the `.deb` (goreleaser), Kukátko is also packaged as a **container image** for running on an amd64 VPS.
Sources: `Dockerfile` + `.dockerignore` in the root, the workflow `.github/workflows/docker-publish.yml`
and an example `.env.example`.

- **`Dockerfile` (root, multi-stage → a small static image):**
  1. **frontend** (`node:22-alpine`): `npm ci` + `npm run build` in `web/` → writes into
     `internal/web/static/dist` (set by `vite.config.ts`).
  2. **backend** (`golang:1.26-alpine`, `CGO_ENABLED=0`): `go mod download`, then **before**
     `go build` the finished `dist/` is copied from the frontend stage (otherwise `//go:embed all:dist/*`
     in `internal/web/static` does not compile). The build is a single static binary `./cmd/kukatko`;
     `-ldflags "-s -w -X …/internal/version.Version=$VERSION -X …/internal/version.Commit=$COMMIT_SHA"`
     stamps the version from the build-args `VERSION`/`COMMIT_SHA`.
  3. **runtime** (`alpine:3`): only the tools the pipeline **actually** shells out to —
     `ffmpeg` (ffprobe + ffmpeg for video metadata/poster/transcode), `exiftool` (EXIF/XMP
     **and** RAW = extracting the embedded JPEG preview via `exiftool -b`, no demosaic → no `dcraw`/
     `libraw` needed) and `libheif-tools` (heif-convert for HEIC/HEIF), plus `ca-certificates`
     and `tzdata`. **No `libvips`** — `thumb.engine` is pure-Go by default. Runs as **nonroot**
     (`nobody`), `EXPOSE 8080` (= `web.port` default), `STOPSIGNAL SIGTERM` (graceful drain),
     `ENTRYPOINT` the binary + `CMD ["serve"]`. Mount the library/cache/tmp as volumes
     (`/var/lib/kukatko/{originals,cache,tmp}`). Those three directories are **created in the image
     and chowned to `nobody`** on purpose: a fresh named volume inherits the ownership of the path it
     is mounted onto, and a mountpoint the image does not have is created **root-owned**, which the
     unprivileged process cannot write (the first upload fails with a permission error). With them
     pre-created, `-v kukatko-originals:/var/lib/kukatko/originals` just works. A **bind** mount keeps
     the host directory's ownership instead, so `chown -R 65534 ./originals` on the host first.
- **Publishing (`docker-publish.yml`) to `ghcr.io/panbotka/kukatko`** (image = `${{ github.repository }}`),
  authentication via the built-in `GITHUB_TOKEN` (permission `packages: write`), **no other secrets**.
  Triggers: push to `main`, a push of a `v*.*.*` tag, and a `pull_request` to `main` (**a PR only builds, never
  pushes** — `push` is true only when `github.event_name != 'pull_request'`).
  - **Test gate:** the `test` job runs **`make test` + `make test-integration`** (mirroring the setup
    of the `integration` job from `ci.yml`: Go 1.26, Node 22, service container `pgvector/pgvector:pg17`,
    extensions `vector`/`unaccent`, apt `ffmpeg`/`exiftool`, `KUKATKO_TEST_DATABASE_URL`). The
    `build` job has **`needs: test`** → if the tests fail, **no image is pushed**.
  - **Tags** (via `docker/metadata-action@v5`, `flavor: latest=false` + explicit control):
    push to `main` → **`latest`** (only on the default branch, `enable={{is_default_branch}}`; on tags
    **not** `latest`); tag `vMAJOR.MINOR.PATCH` → **`{{version}}`** and **`{{major}}.{{minor}}`**; plus
    always an immutable **`sha`** tag. Build via `docker/build-push-action@v6` with build-args
    `VERSION` (the tag without a leading `v`, otherwise `dev`) and `COMMIT_SHA` (short SHA).
- **`.env.example` (root):** a documented, secret-free example of the env variables for running the container
  (`docker run --env-file .env …`). Derived from `config.example.yaml`: the `KUKATKO_` convention (dot →
  underscore) + the `MAPY_API_KEY` exception. Covers `KUKATKO_DATABASE_URL` (required), the embedding URL,
  storage/R2 keys, backup S3 keys, and `MAPY_API_KEY`. The real **`.env` is gitignored**
  (`.env`/`.env.*`), `.env.example` is the exception and is committed.

