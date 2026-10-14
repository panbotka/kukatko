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
  downtime matters — **only for a release whose migrations the running binary survives** (0047 is
  one: it only adds an index). **Never for the 0.31.0 → multi-tenant release**: 0.31.0 served against
  the migrated schema fails every sign-in with 500, see "Upgrade runbook — multi-tenancy"; see
  `docs/PERF.md` §3),
  `kukatko migrate [--force]` (runs pending migrations on their own and exits). **Only `migrate` and
  `serve` ever change the schema.** Both hold one fixed `pg_advisory_lock` for the whole run, so two
  started at once take turns and the second finds the schema up to date. Every other subcommand that opens
  the database — `maintenance` (scan/repair/reset/…), `import dir`, `maintenance nameless-subjects`,
  `sidecar backfill`, `sidecar families`, `storage migrate-to-r2`, `storage rekey`, `project purge`,
  `restore db`'s repair step, `restore repair` and `restore verify` — **refuses** a database that lacks a
  migration the binary embeds and exits 1: `the database schema is older than this binary: database is at
  schema N, this binary expects M (K pending migration(s): first … last); run `kukatko migrate` after
  stopping the old server`. `maintenance inventory` is the one exception: it neither migrates nor refuses
  (it is the release-day "before" snapshot). Before migrating, both migrators look for **other sessions on
  the database** in `pg_stat_activity`: every session whose `application_name` starts with `kukatko/` (every
  Kukátko after 0.31.0 announces `kukatko/<version> <instance>` on each connection, overriding a DSN's
  `application_name`), and every other session of the same database user (0.31.0 and older announce none).
  Only when migrations are pending: `migrate` lists them on stderr and **refuses** (exit 1, `other clients
  are connected to the database`) unless `--force` (for a client that is not Kukátko — an admin's `psql`,
  say); `serve` logs them at `ERROR` (`migrating a database other clients are connected to…`, then one
  `other database client` line each) and **starts anyway**, because a compose deploy stops the old container
  before starting the new one and a refusing server would only restart into the same refusal. A session of
  another user that announces no Kukátko name (a monitoring exporter) is not counted),
  **`kukatko import dir --project <slug> <path>`** (uploads a **directory from disk** into one project —
  `internal/dirimport`; see below),
  `kukatko backup` (synchronous one-off **S3 backup** — `internal/backup`; pg_dump + sync of
  originals + retention; needs `backup.s3.{endpoint,bucket}`, otherwise `errBackupNotConfigured`;
  under `storage.backend: r2` the originals are **copied bucket→bucket server-side** and a backup into
  the **same** bucket the library lives in fails with `errBackupSameBucket`;
  for ops/cron without a running server),
  **`kukatko restore`** (the restore/disaster-recovery tree — `internal/backup`; shares `backup.s3.*`,
  otherwise `errRestoreNotConfigured`; for ops/cron without a running server): `restore list` (dumps in
  the bucket), `restore db [--dump KEY] [--yes] [--verify]` (**destructive** DB restore via
  `pg_restore` streamed from S3 + **clearing the streaming renditions**, whose
  segments the backup never carried, so the ordinary `POST /process/hls` backfill re-encodes them;
  without `--yes` → `errRestoreNotConfirmed`; it **never migrates**: a dump older than the binary is
  restored and then the command exits 1 — run `kukatko migrate`, then `restore repair`),
  `restore repair` (the clearing step on its own, for exactly that case; refuses a schema that is behind),
  `restore originals` (downloads missing originals, skips by key+size, resumable),
  `restore verify` (integrity report of photos in the DB vs originals on disk); runbook
  [`docs/RESTORE.md`](RESTORE.md),
  **`kukatko maintenance`** (library integrity check & repair — `internal/maintenance`; for
  ops/cron without a running server, refuses a schema that is behind and builds a service shared with the
  admin API):
  `maintenance scan` (read-only integrity report — store↔DB drift + missing derived data, the latter counted over
  every active project, since the backfill listings it reads are per project (`cmd/kukatko/allprojects.go`); the store it
  inventories is the configured backend, the originals root under `fs` and the bucket under `r2`, and the
  summary line names the one it read; a listing it could not run prints `originals …: unknown (reason)`
  instead of a zero, because an unreadable store must not pass for an empty one; a `per project:` block
  follows with each project's share and the `orphan project` / `legacy layout` findings — see
  "`maintenance scan` — the per-project report" below),
  **`maintenance reset`** (the **guarded library wipe** — `internal/reset`; dry run by default, `--execute` +
  a typed database name — and, on a bucket-backed store, a typed bucket name — to delete, `--force` for a
  non-interactive run, `--orphan-sweep` for the leftovers the catalogue never referenced;
  accounts/announcement/instance settings/audit trail/migrations are never touched; see below) and
  **`maintenance inventory`** / **`maintenance inventory-diff`** (the read-only before/after snapshot of an
  upgrade and its judged diff — `internal/inventory`; **never migrates**; see "Upgrade runbook — multi-tenancy"
  below) and
  **`maintenance nameless-subjects --project <slug>`** (reports — and with `--apply --undo-file` detaches —
  the project's subjects whose name identifies nobody, the importer-minted catch-all; dry run by default,
  reversible via `--undo`; see below) and
  `maintenance repair` with the flags
  `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans`/`--places`/`--dimensions`/
  `--face-markers`/`--sideways-faces`/`--impossible-dates`/`--missing-renditions`/`--delete-orphan-segments`/
  `--unscheduled-steps`/`--root-derived`
  (each opt-in; thumbnails/phashes enqueue `thumbnail` jobs drained by a running server's worker,
  embeddings/faces backfill, orphan import synchronously via the upload pipeline. `--faces` prints the
  videos it passed over on its own line (`videos skipped by face detection=N`): detection does not run on
  footage, so a library of mostly clips schedules far fewer jobs than it has media, and that number is what
  says why rather than leaving it looking like nothing happened; `--dimensions` writes the
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
  and a photo re-detected upright drops out of the finding for good, so a re-run is a no-op.
  `--places` is the reverse-geocode backfill: it enqueues a `places` job for every **live photo that carries
  coordinates and has no cached place**, printing `places=N` in the scheduled line. Its **dry run is
  `maintenance scan`** (the `missing places` line). It only fills the queue — every job still reserves a
  credit from the mapy.com window budget (`maps.geocode_budget`/`maps.geocode_budget_window`) before it
  reaches the geocoder, and a job that finds the window empty is **deferred until it refills, not failed**:
  it burns neither a retry attempt nor a credit, so scheduling a whole library spreads the spend over
  windows instead of overrunning the quota. On an instance with **no `maps.mapy_api_key` the flag refuses**
  (and the scan reports no backlog), because no `places` handler is registered there and the jobs would wait
  for ever. Since photos now earn a `places` job **at upload** (`internal/ingest`), this is a catch-up for the
  library that predates that, not routine work.
  `--unscheduled-steps` enqueues every processing step a live photo is owed and nothing has scheduled — the
  steps its processing report (the photo detail's `processing` block) calls `pending`, one job per (photo,
  step), never a step switched off on this instance, and **never `places`**: a pending reverse geocode is the
  deliberately deferred, paid backlog `--places` owns (the scan's `missing places` line), not a gap, so it
  is neither counted nor queued here. The scan's `unscheduled steps` line is its dry run,
  sampled as `<photo_uid>/<step>`. It is the catch-up for an upload cut short after its original was stored
  (2026-10-05: no thumbnails, no jobs); since the pipeline now finishes regardless of the client and a
  re-upload of the same file completes its photo, a non-zero count is worth a look rather than routine.
  `--root-derived` moves the thumbnails and streaming segments a storage move left at the **store root** for
  photos whose row already points under their project's prefix (the scan's `root derived` line, sampled by
  root key, is its dry run; see "`maintenance scan` — the per-project report"). It drives the storage move's
  own rules (`internal/rekey`): an object already under the prefix is kept, a missing one is copied there, and
  a photo's root copies are deleted **only once every one of them exists under the prefix**; it prints
  `root derived objects moved: photos=N objects=M failed=K` — a failed photo kept everything at the root.
  Like `--delete-orphan-segments` it is refused while a storage move is `running` or `paused`.
  `--impossible-dates` is the fourth flag that writes the catalogue directly, and the only one that **removes**
  something: it withdraws the capture date of every photo dated to a year no photograph can have been taken in
  — before 1826 or further ahead than next year, the same range `internal/exif` enforces on an incoming
  file-name guess (`internal/exif/captureyear.go`, asked for through `exif.CaptureYearBounds()` so the two
  cannot drift apart). Those rows are what a Facebook or WhatsApp download leaves behind
  (`90090310_638783213372240_…` reads as 9009-03-10) and no other backfill touches them: `internal/metajob` is
  a gap-filler that holds the capture time outside its reach. The date is **cleared, never replaced** — the
  photo ends up with no date at all and is found with `dated:no` — because a guessed substitute would be a
  second wrong answer on top of the first. Nothing is lost: the discarded date is preserved in
  `taken_at_before_unknown` (still shown on the photo page and written to the sidecar), the original file is
  never touched, and each cleared date is one `photo.date_clear` **audit entry written in the same transaction**
  as the clear, carrying the discarded date, its provenance and the file name; a CLI run has no acting user, so
  it is recorded as a system action. Its **dry run is `maintenance scan`** (the `impossible dates` line and
  samples), each write is guarded on that same predicate — a photo re-dated by hand in between is skipped —
  and each cleared photo gets a `sidecar` job — when `sidecar.enabled` — so the metadata on disk drops the
  date too. It prints
  `impossible dates cleared=N`, and a re-run is a no-op.
  `--missing-renditions` and `--delete-orphan-segments` are the two **streaming** repairs, and the scan grew the
  two lines that are their dry run: `missing renditions` (rows of `photo_hls_renditions` whose objects are not in
  the store, sampled as `<photo_uid>/<rendition>`) and `orphan segments` (objects under `hls/` that no recorded
  rendition claims, counted with the storage they occupy and sampled by the `hls/<file_hash>/<rendition>/` prefix
  they sit under — one abandoned encode is thousands of segments under a single prefix). A missing rendition is
  what a storage migration that moved the originals but not the segments leaves behind: the photo still
  advertises itself as streamable and every segment request answers 404. Presence is judged by the rendition's
  **initialisation segment alone** — a player fetches it before any media segment, so a rendition without it
  cannot be played whatever else survives — and the playlist is no evidence at all, being a database column
  rather than an object. `--missing-renditions` **drops the row** (`renditions dropped=N`): the clip stops
  claiming it streams and lands in exactly the state `POST /api/v1/p/<slug>/process/hls` looks for, so recovery is one
  background job. It removes no media — not the original, and not the stray segments of a partly-surviving
  rendition, which simply become orphans the next scan reports.
  `--delete-orphan-segments` is the **only repair that deletes anything from the store**, which is why the scan
  never sweeps on its own and the flag has to be asked for by name. It skips every object belonging to a video
  with an unfinished `hls_transcode` job (`orphan segments deleted=N kept=M`): the encode publishes a rendition's
  objects **before** it writes the row describing them, so during a transcode a healthy job's uploads are
  indistinguishable from abandoned ones and deleting them would break the job. The queue is re-read at repair
  time, not taken from the scan, so an encode that started in between is still safe. It also keeps (and counts)
  the root segments of a recorded rendition whose video already lives under its project's prefix — what a
  storage move has not copied yet, `--root-derived`'s to move — and it is **refused outright while any storage
  move is `running` or `paused`** (see the storage-move runbook). Both refuse on an instance
  with `video.hls.enabled: false` (503 over HTTP): nothing encodes there, so a dropped row would never be
  produced again.
  **Cost of the streaming check:** with streaming off, or with no video in the catalogue (archived ones counted —
  archiving removes neither rows nor segments), it makes **no query and no store call at all**. Otherwise it adds
  **one `Stat` per recorded rendition** — one per video with the default single-quality plan — plus **one prefix
  listing of `hls/`** for the whole library, and one further `Stat` per orphan object found. A healthy library
  therefore pays one listing and one object check per video, and only a library with rot pays in proportion to
  how much of it there is;
  a no-op without any flag;
  the **retention purge of old audit logs** is separate, only via HTTP/UI, not the CLI — the maintainer calls
  `POST /api/v1/maintenance/audit/purge` `{older_than_days}` (`internal/maintenanceapi`), which deletes audit
  entries older than `now − older_than_days` and **audits itself** (`audit.purge`, so that deleting the trail
  stays traceable); the admin UI has a „Vymazat audit log" card on the Údržba page with presets
  (3/6 months, 1/2 years) or a custom number of days plus a confirmation),
  **`kukatko sidecar`** (metadata sidecars — `internal/sidecarjob`; the terminal entry point into the export
  that makes curation data independent of the database): `sidecar backfill` enqueues a `sidecar` job for
  every photo with a **missing or stale** sidecar, in **every active project** (it is an instance-level trigger, so it
  runs the per-project backfill once per active project and adds the counts up), `--all` forces a full re-run over every
  unarchived photo (this catches up changes outside the photo's own row — album membership, a label).
  It only **enqueues**; the files are written by a running server's worker (the same queue, same handler, same
  dedup as for live edits, so the backfill cannot race the user), which is why it prints the number of
  scheduled jobs. Idempotent — over a library with up-to-date sidecars it schedules zero — so it
  can be run from cron and, above all, **before any risky operation** (a migration, upgrade, restore drill),
  which is exactly the moment a person is at the terminal. When `sidecar.enabled: false`, the command
  **fails** instead of a silent “0 scheduled”. The full format is in [`docs/RESTORE.md`](RESTORE.md),
  the HTTP counterpart is `POST /api/v1/p/<slug>/process/sidecars`.
  `sidecar families` writes **every project's family tree export** (`families.yaml` under each project's storage
  prefix — the root of the storage for `v` — `internal/familyexportjob`) **now**, printing each key written, and
  unlike its sibling it writes the files itself rather than enqueuing: there is one file per project and the work
  is one small read each, so there is nothing to spread over a queue, and the moment this is typed is exactly the
  moment a running server cannot be assumed. The server rewrites a project's file on its own whenever a relation —
  or a subject in one — changes in that project; this is the "before I do something risky" button. It
  fails the same way when `sidecar.enabled: false`, and it has no HTTP counterpart,
  **`kukatko storage`** (operations over the storage of originals — `internal/storagemigrate`):
  `storage migrate-to-r2` (a one-off **resumable** move of the library to R2, see below) and
  `storage rekey <slug>` (the **storage move** of one project from the store root under `projects/<uid>/` —
  `internal/rekey`, stepped by the worker's `storage_rekey` job; `--dry-run`, `--status`, `--pause`,
  `--resume`, `--retry`; reports on stderr, exit nonzero only for a `failed` run; see the runbook below),
  **`kukatko project purge <slug>`** (removes one project outright — its catalogue rows, members, invitations,
  the project row and everything under its `projects/<uid>/` prefix — `internal/reset`; dry run by default,
  `--yes --confirm <slug>` to execute, refuses the bootstrap project without `--allow-bootstrap` and any
  project with an empty storage prefix; see below),
  **`kukatko push generate-keys`** (mints a fresh **VAPID key pair** for Web Push — `internal/push`
  `GenerateKeys`; reads no config, touches no DB, so it runs anywhere the binary does). It prints the two
  environment assignments `KUKATKO_PUSH_VAPID_PUBLIC_KEY=…` and `KUKATKO_PUSH_VAPID_PRIVATE_KEY=…` on
  **stdout** and a reminder on **stderr** — the private key belongs in the environment (1Password → the
  deployment's env), **never in a committed file** — so `kukatko push generate-keys > vapid.env` captures the
  keys and nothing else. Mint a pair **once per instance**: every browser subscribes against the public key,
  so a new pair silently invalidates every stored subscription and everybody has to allow notifications
  again,
  **`kukatko ctl`** (a remote client over the HTTP API of a running instance — `internal/ctl`; the only subcommand
  that **touches neither the DB nor disk**, see below),
  `kukatko version` (version + commit). The persistent `--config <path>` flag selects the YAML config.
  `server.New(addr, server.WithAPI(register))` mounts the route groups under `/api/v1`.

### `kukatko import dir --project <slug> <path>`

Walks a directory on disk (recursively) and uploads every media file into the library of **one project**
**through the same pipeline as a browser upload** (`internal/ingest`): stream + SHA256, metadata, the original into
`<storage prefix>YYYY/MM`, thumbnails, `image_embed`/`face_detect` jobs onto the queue. The source directory is **read only** —
originals are copied, never moved or modified. For ops/cron without a running server (it applies
migrations and opens the DB itself); the run is recorded in `import_runs` as source `folder` **in that project**, so it is
visible in the project's `/import` page and in `GET /p/<slug>/import/runs` alongside the finished migration's runs (those
belong to the bootstrap project `v`). It is the **only** source still written.

**`--project <slug>` is required** — there is no default, because an import into the wrong library is a leak nobody
notices. Everything of the run happens inside that project: the photos are catalogued there, the originals land under
its storage prefix (an empty prefix for `v`, `projects/<uid>/` for a created project), the duplicate check looks only at
its photos (the same bytes in another project are imported again, not reported as a duplicate), and `--album`,
`--labels` and an export's favourites/ratings resolve there. A deactivated project is refused. The import reads only the
foreign sidecars lying beside the media on disk (below); it never reads the exported `sidecars/` tree of the object
store, so no other project's metadata can reach it.

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
| `--project <slug>` | **required** | the project the photos, the run, the album and the labels belong to (an unknown or deactivated slug fails before anything is read) |
| `--album <uid\|name>` | – | adds every photo to the album; a uid is used as-is, a name is looked up and **created if not found** (applies to duplicates too → this fixes a forgotten `--album`) |
| `--labels <a,b,c>` | – | attaches labels (by name; anything that does not exist is created) to every photo in the run |
| `--recursive`, `-r` | `true` | descends into subdirectories too |
| `--no-recursive` | `false` | flat directory only (**mutually exclusive** with `--recursive`) |
| `--dry-run` | `false` | only reports what it would do (new / duplicate / skipped + reason, including the **full sidecar report**) — **writes nothing**, not even `import_runs` |
| `--no-sidecars` | `false` | ignores metadata next to the media (a Takeout export then arrives **without dates or captions**) |
| `--concurrency N` | `3` | how many files are uploaded in parallel; **cap 8** (thumbnailing large photos is memory-hungry and the box has 16 GB shared with everything else) |
| `--uploader <user>` | bootstrap admin | the username of the owner of the imported photos, which must be an approved member of `--project` (its name is shown there; another account fails the run before anything is imported); without it `auth.bootstrap_admin_username`, otherwise the first enabled maintainer — each only if a member of the project, else the photos have no owner |

Output: one line per file (`[12/2000] imported 2014/IMG_0001.JPG (sidecar: IMG_0001.JPG.json)`) and at
the end a summary `imported=… duplicates=… skipped=… failed=…` + a breakdown of the skipped ones by reason,
the **sidecar report** (see above), and the run duration.

### `kukatko storage migrate-to-r2`

A one-off move of ~120 GB of originals (with their metadata sidecars and already-cached thumbnails)
from the local disk to the R2 bucket. It runs for hours and may be killed and restarted at any time.
Object keys = `file_path` from Postgres (and the parallel `sidecars/…` key for each sidecar), so
nothing is re-keyed — the bucket gets the same layout as the disk, **prefix included**: a created project's
`projects/<uid>/…` originals, sidecars and thumbnails land under the same `projects/<uid>/` keys, and the
`families.yaml` of the root layout and of every project's prefix (`Catalogue.StoragePrefixes`) is moved once
per run.

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
`POST /api/v1/p/<slug>/import/*` triggers and `GET /api/v1/p/<slug>/import/verify` are gone with them.

What stays is live data, not leftovers:

- the `import_runs`/`import_failures` history (`GET /api/v1/p/<slug>/import/runs`, `GET /api/v1/p/<slug>/import/failures`,
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
alone. Such a subject cannot be created deliberately: `POST /api/v1/p/<slug>/subjects` rejects a name with no letter or
digit. One in the catalogue was therefore minted by an importer, and it behaves as a **catch-all**: the
find-or-create-by-name paths used to key on `people.Slugify`, which is total and answers `subject` for a
nameless face, so the first unnamed face created one empty-named subject and every unnamed face after it was
*found* by that same key and assigned to it. On production (2026-08-02) one such subject collected **16 532
markers** against 4 635 on all real people, sat first in `/people`, and made face assignment unusable.

The importers now key on `people.NameSlug`, which returns `""` for exactly those names, so a nameless face
stays unassigned. This command is the repair for the data the old behaviour already wrote.

```bash
kukatko maintenance nameless-subjects --project v                                   # dry run: report only
kukatko maintenance nameless-subjects --project v --apply --undo-file /tmp/undo.json   # detach, writing the undo
kukatko maintenance nameless-subjects --project v --undo /tmp/undo.json                # put everything back
```

- **`--project <slug>` is required** (`errProjectRequired`), in every mode, the report included. Each project
  may have a catch-all of its own and the command never guesses which library it is about to change: it
  resolves the slug (an unknown one is an error), lists, detaches and restores inside that project only, and a
  subject of another project is invisible to it. The undo file records the project's uid and slug
  (`project_uid`, `project_slug`), and `--undo` refuses a file of another project
  (`namelessjob.ErrProjectMismatch`) before restoring anything. A file written before projects existed records
  none and belongs to the bootstrap project (`v`).
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
minutes of index maintenance. Watch the job queue on the same page for progress. The page works on the
current project (the switcher's; the routes are `/api/v1/p/<slug>/maintenance/nameless-subjects*`), as the
CLI does with `--project`. Both jobs carry `project_uid` in their payload, and their dedup key is (type, project, subject)
(`idx_jobs_nameless_dedup`, migration 0105), so scheduling the same detach or restore twice while the first
is still queued collapses into it.

### `maintenance scan` — the per-project report

The scan is instance-wide, but it lists the store **once**, classifies every key with the project whose tree
holds it (`internal/storekeys`: the root layout or `projects/<uid>/`) and reconciles it against the catalogue
**per project**. The report (`maintenance.Report`, also `GET /api/v1/maintenance/scan`) carries, next to the
instance-wide findings:

| Field | Meaning |
| --- | --- |
| `projects[]` | One entry per project (deactivated ones included), in listing order: `uid`, `slug`, `name`, `storage_prefix`, `photos` (with a primary file), `files_in_db`, `originals` (in the store, attributed to it), and its share of `missing_originals`, `orphan_files` and `missing_thumbnails`. The shares add up to the instance-wide findings. |
| `orphan_project` | Objects of any kind under `projects/<uid>/` for a uid no project has — e.g. what a purge whose store half failed left behind. Never repaired: a maintainer decides. |
| `root_derived` | Thumbnails and streaming segments at the **root** whose photo already lives under its project's prefix — what a storage move could not move, or what a move before 0115 left behind. Found from the **bucket listing** (the root `thumb/` and `hls/` trees, matched to a moved photo by the content hash in the key — never the local thumbnail cache), only for projects a storage move touched, sampled by root key and **not** counted again under `legacy_layout`. Nothing serves them (a photo's URLs name its prefixed keys), so a tile whose prefixed copy is missing shows blank. `maintenance repair --root-derived` moves them — a key the layout no longer builds (an unregistered thumbnail size, a name the streaming layout cannot hold) is deleted instead, as the storage move's retry does. |
| `legacy_layout` | Root-layout objects once **no** project uses the root layout (the bootstrap project's storage has moved): an original no catalogue row points at, and — once no catalogued file lives at the root any more — every thumbnail, sidecar, segment or `families.yaml` still there. Half-way through the move a root original the catalogue still points at is the project's own, and the root's derived objects are still in use. Never repaired. |

`orphan_files` (and so `repair --import-orphans`) counts only originals of a **real** project's tree; an original
under an orphan project or in the legacy layout is reported by the two findings above instead and never offered
for import — no project would receive it. All three findings make the report non-clean.

### `kukatko maintenance reset` — the guarded library wipe

Empties this instance's library — every catalogue table and every object the configured store owns
(`internal/reset`). It is the only command in the binary that destroys the library on purpose. This
deployment has **no S3 backup**, and the only way back is re-walking the folders the library was
imported from: a misfire is **unrecoverable**. The guards below are the feature, not decoration.

**What it deletes.** The catalogue tables — `photos`, `photo_files`, `albums`, `album_photos`, `labels`,
`photo_labels`, `photo_tasks`, `photo_task_photos`, `subjects`, `markers`, `faces`, `face_clusters`,
`face_detections`, `face_confirmations`,
`embeddings`, `photo_phashes`, `photo_places`, `photo_edits`, `comments`, `photoprism_aliases`,
`import_runs`, `import_failures`, `jobs`, the
per-user curation (`user_favorites`, `user_ratings`, `saved_searches`) and the rejection/dismissal tables
(`face_rejections`, `label_rejections`, `duplicate_dismissals`, `duplicate_marker_dismissals`) — in **one**
`TRUNCATE … RESTART IDENTITY`
(no `CASCADE`: every FK between them is inside the list, so a future table that references one of them and was
not classified makes Postgres refuse the statement instead of silently widening the blast radius). In the store
it deletes the `YYYY/MM` originals, the `thumb/` thumbnails and the `sidecars/` metadata, plus the local
thumbnail cache under `storage.cache_path` — in **both layouts**: at the root and under every `projects/<uid>/`
tree (a catalogued key carries its prefix; `families.yaml` is named at the root and under every project's
prefix; the `--orphan-sweep` classifies `projects/<uid>/…` exactly as the root, an orphan project's tree
included, and a non-uid directory under `projects/` or a `db/` inside a tree stays foreign). To empty **one**
project instead, use `kukatko project purge`.

**What it never touches.** `users`, `sessions`, `api_tokens`, `projects`, `project_members`, `announcements`,
`instance_settings`, `audit_log` and `schema_migrations` — a wipe must not lock you out of the instance you just wiped, nor close
self-service registration and throw away its shared secret, nor erase the record of the wipe.

**The guards** (all on by default):

| Guard | Behaviour |
| --- | --- |
| Dry run is the default | Without `--execute` it prints a row count per table and an object count per prefix, and deletes nothing. |
| Typed confirmation | You must type the target database's name (not `y/N`); a mismatch → `ErrConfirmationMismatch` and nothing is deleted. `--confirm-database <name>` supplies it without a prompt. |
| Typed bucket confirmation | On a bucket-backed store you must type the configured bucket's name too; a mismatch → `ErrBucketConfirmationMismatch`. `--confirm-bucket <name>` supplies it without a prompt. The database and the bucket come from independent config keys and can name independent deployments (a dev database pointed at the production bucket is exactly the accident this refuses), so confirming one says nothing about the other. A name typed against a store that has **no** bucket — the `fs` backend — is refused for the same reason: the operator was aiming at something this run cannot reach. |
| Target check | `current_database()` is read **from the server** and compared with the database in the loaded config; a mismatch → `ErrTargetMismatch`. Host + database are printed before you are asked. |
| Non-interactive refusal | Stdin that is not a terminal (a script, cron, an agent — `/dev/null` included, which is why the check is a terminal ioctl and not "is a character device") is refused unless `--force` is passed too. Checked **before** the config is loaded, so a stray invocation opens nothing. |
| Storage scope | Only the prefixes the store owns — the `YYYY/MM` originals, `thumb/`, `sidecars/` and `hls/` — are ever deleted; anything else in the bucket is counted as `foreign` and left alone. A key the catalogue does not reference is deleted only with `--orphan-sweep`, and the HLS segments are always in that group: their keys are known to the store, not to the catalogue. There is no delete-everything path. |
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
which leaves behind whatever an earlier interrupted import put in the bucket — and every video's HLS segments,
whose keys the catalogue does not hold. On an object store the sweep is
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

### `kukatko project purge <slug>` — remove one project

The per-project wipe (`internal/reset` `Purger`): it removes **one** project outright and leaves every other one
exactly as it was. It deletes, in one transaction with its audit entry, the project's queued/failed jobs
(by `payload.project_uid` and by the photo/album/subject they are about — a **running** job is left to fail on
its own), every catalogue table of `internal/reset`'s list that carries `project_id` (ordered so a table is
deleted before the tables it references — the order is read from the live foreign keys; child rows such as
files, markers, album members and comments go with their parent through `ON DELETE CASCADE`), then
`project_members` and `project_invites`, then the project row (which cascades its settings and announcement).
Only once that has committed does it delete **every object under the prefix** and the project's tree in the
local cache (`storage.cache_path`/`projects/<uid>/`). The accounts stay; the audit trail stays and gains a
`project.purge` entry (target = the project, `project_id` = the project, details: operator, slug, name, prefix,
rows per table, objects, bytes).

| Guard | Behaviour |
| --- | --- |
| Dry run is the default | Without `--yes` it prints **on stderr** the row count per table (in deletion order) and the number and size of the objects under the prefix, and deletes nothing. |
| Typed slug | `--yes` requires `--confirm <slug>` (the slug typed again, exactly); without it the command refuses before it opens anything, a mismatch → `ErrPurgeNotConfirmed`. |
| Bootstrap | The bootstrap project (`v`) is refused (`ErrPurgeBootstrap`) unless `--allow-bootstrap`. |
| Empty prefix | A project whose `storage_prefix` is empty lives in the store root — its "prefix" would be the whole store — and is refused outright (`ErrPurgeRootPrefix`), `--allow-bootstrap` or not. |
| Schema drift | The same table classification as `maintenance reset`: an unclassified table aborts before anything is deleted. |
| Report | After the database half commits, a JSON report (`reset.PurgeResult`: the plan, the time, the store outcome) is written to `--report <path>`, by default `project-purge-<slug>-<UTC time>.json` in the current directory — also when the store half fails. |

```bash
kukatko project purge b-test                              # dry run: what would be deleted (stderr)
kukatko project purge b-test --yes --confirm b-test       # purge, write the report
kukatko project purge v --allow-bootstrap                 # still refused while v lives in the root layout
```

**In the container, pass `--report`.** The image's working directory `/app` is not writable by the
`nobody` user, so the default report path fails *after* the purge has completed (the database and store halves
both done, exit 1, `writing the purge report …: permission denied` — seen in the final safety gate). Use
`--report /var/lib/kukatko/tmp/project-purge-<slug>.json` there.

Deactivate the project first so nobody uploads into it while it goes. If the store half fails (exit nonzero, the
report lists the failures), the database is already gone: the leftover objects are what the next
`maintenance scan` reports as `orphan_project`.

### Upgrade runbook — multi-tenancy

The multi-tenancy migrations (0100 onwards) restructure data that exists only once — the library of
~21k photos. They were rehearsed on a copy of production (verdict and measurements in
[`MIGRATION_SAFETY.md`](MIGRATION_SAFETY.md)); this is how a deploy that ships them is checked.

**`kukatko maintenance inventory --out <dir> [--storage]`** writes a deterministic snapshot of the
configured database: `counts.tsv` (every table → rows), `columns.tsv`, `digests.tsv` (an order-independent
digest per catalogue table over an explicit column allow-list that exists before **and** after the
tenancy migrations), `photos.tsv` (`uid|file_hash|file_path|file_size|archived_at`) and `users.tsv`
(`uid|username|role|disabled`). It runs in one read-only transaction and **does not apply migrations** — so
the *new* binary takes the "before" snapshot of a database still on the old schema (the old release has no
such command). `--storage` also lists the configured store **once** and writes `storage-missing.tsv` — every
original absent or of the wrong size; empty is good. Give it a **read-only** key (R2: an API token with
*Object Read only* on the bucket — it lists and reads, it cannot write). On the production copy it took
4.75 s and 45 MB RSS on the box (expect ~3–4× that on the VPS).

**`kukatko maintenance inventory-diff <dirA> <dirB> [--expect …]`** prints every difference (unexpected first)
and **exits 1** when one is not allowed by an `--expect`: a `path.Match` glob over the difference key
(`table-added:projects`, `column-added:*.project_id`, `count:schema_migrations`, `digest:photos`,
`photo:<uid>`, `user-role:<uid>`, `storage-missing:<path>`, …) or the rule `role-collapse` (an account whose
only change is viewer/curator/editor/admin → `user`). It needs no database. The expectation set for the
0100–0104 chain — every entry is a difference the design makes, nothing else:

```sh
kukatko maintenance inventory-diff A B \
  --expect table-added:projects --expect table-added:project_members \
  --expect 'column-added:*.project_id' --expect column-added:users.last_project_uid \
  --expect column-removed:announcements.id --expect column-removed:instance_settings.id \
  --expect column-removed:sessions.role --expect count:schema_migrations --expect role-collapse
```

**On a live instance** the app keeps writing between A and B, so the live tables drift: `count:jobs`,
`count:audit_log`, `count:sessions`, `count:notifications`, `count:search_history` and `digest:users` (every
visit stamps `last_seen_at`). Allow those only after reading them: `users.tsv` must show no change but the
role collapse, and every catalogue difference (`digest:photos`, `digest:album_photos`, `photo:…`) must match an
`audit_log` entry written in the window (`created_at` after A). An unexplained catalogue difference stops
the rollout.

**Migrations past 0104.** A deploy from a release older than the whole chain also crosses 0105–0114, which
add `table-added:project_invites` and `table-added:storage_rekey_runs` and drop the five per-library
`users` columns the memberships now hold (0110: `column-removed:users.approved_at`, `…subject_uid`,
`…last_seen_at`, `…visit_reference_at`, `…task_digest_at`). 0114 only drops the transitional `DEFAULT` of
every `project_id` column, which the inventory (names and types only) does not see. The exact set for a
given pair of releases is the one the pre-release rehearsal records in
[`MIGRATION_SAFETY.md`](MIGRATION_SAFETY.md); use that, not a guess.

**Never migrate under a running 0.31.0.** The chain 0100–0114 is not backward compatible: 0.31.0 served
against a migrated schema fails every sign-in with 500 (`column "subject_uid" does not exist` — proven on a
production copy, 2026-10-13) while its `/healthz` stays 200. Which new-binary commands may run against the
production database **while the 0.31.0 container is still running**:

- `maintenance inventory` / `inventory-diff` — yes; this is what they are for (they never migrate).
- every other subcommand that opens the database (`maintenance scan`, `import dir`, `sidecar …`,
  `storage …`, `project purge`, `restore …`) — harmless: it refuses with `database is at schema N, this
  binary expects M` and exits 1 without touching the schema. (Releases up to 0.31.0 instead migrated as a
  side effect here.)
- `kukatko migrate` — **no**. It sees 0.31.0's connections (no `application_name`, same user) and refuses;
  do not override that with `--force`.
- `kukatko serve` — only after the old container is stopped (step 5). It would start and migrate even with
  0.31.0 connected, logging every other client at `ERROR` (`other database client`); seeing that line is
  the sign the old container is already broken — stop it at once.

The only safe order is **stop the old container → start the new one** (it migrates on start). The general
advice to pre-migrate to shorten the downtime (CLI section above) holds only for releases whose migrations
are backward compatible with the running binary — and even then `kukatko migrate` refuses while the old
server is connected, so pre-migrating means `--force`, deliberately. From this release on every Kukátko
announces `application_name` `kukatko/<version> <instance>`, so the next upgrade's `migrate` recognises the
running server by name even if it connects as another database user.

The owner's checklist — in this order, and the storage move only at the very end:

1. **Backup.** `kukatko backup` in the running (old) container, and note the dump key. A dump of a schema
   before 0104 does not restore with `restore db` as-is — use the manual procedure in
   [`RESTORE.md`](RESTORE.md) ("Dumps taken before migration 0104") if it is ever needed.
2. **Check for long transactions** (`SELECT pid, xact_start, query FROM pg_stat_activity WHERE xact_start <
   now() - interval '10 seconds'`) and do not deploy while a backup's `pg_dump` runs: 0102 takes
   `ACCESS EXCLUSIVE` on the catalogue tables and would queue behind it, and every request behind 0102.
3. **Inventory A** with the **new** image, against the production database, before switching:
   `docker run --rm --network <db-network> --entrypoint /app/kukatko -e KUKATKO_DATABASE_URL=… -e
   KUKATKO_STORAGE_…(read-only key) -v /root/inventory:/inv <new-image> maintenance inventory --storage --out /inv/A`.
4. **Repoint the API clients.** There is no unprefixed project route any more: `GET /api/v1/photos` and
   every other catalogue path without `/p/<slug>/` is the API's 404. The embedded frontend is shipped with
   the binary, but anything configured by hand must name its project before the switch: an MCP connector
   on `…/api/v1/mcp` becomes `…/api/v1/p/v/mcp` ([`MCP.md`](MCP.md)), a `kukatko ctl` context needs a
   project (`ctl context set <name> --project v`), and a script calling the API gets the `/p/v` prefix.
5. **Deploy: stop the 0.31.0 container, then start the new one.** The new container's start applies the
   migrations (rehearsed: 0.27 s on the box, ~1–2 s expected on the VPS) — the outage is the container restart
   plus that. No `kukatko migrate` beforehand (see above). Its log must show no `other database client`
   line; one means something still holds a session on the database (the old container?) — find it.
6. **Inventory B** the same way (`--out /inv/B`), then **diff** with the expectation set above; it must exit 0
   once the live drift has been read and allowed.
7. **Verify `v`.** The bootstrap project is the whole old library, so check it is whole and reachable:
   `/system/projects` lists `v` (Veselice), active, with the member count of `users.tsv` and the photo count
   of inventory A; `/system/projects/v` shows its storage layout as the store root. Sign in as an ordinary
   member: the chooser (or `/` straight into `/p/v/`) opens the library grid, a search, one album, one
   person, the timeline and the map, and `GET /api/v1/auth/me` lists the membership of `v` with its role.
   `kukatko maintenance scan` reports nothing missing for `v`. Anything off stops here — roll back below.
8. **Start the storage move** of `v` — a separate job, started by hand, only after a **fresh backup** taken
   on the new release: `kukatko storage rekey v --dry-run`, then the „Spustit přesun" button on
   `/system/projects/v` (or `kukatko storage rekey v`). The runbook below says what it does and refuses.
9. **Watch the page.** `/system/projects/v` refreshes every 3 s: photos and bytes moved, the rate and ETA,
   the last key, the errors; `/system/projects` shows „přesun 42 %" beside `v`. Pause, resume or retry from
   the same card. When the run says `done`, run `kukatko maintenance scan` again and review any root-layout
   orphan it reports before deleting anything.

**Rollback** without restoring a backup: [`docs/migrations-rollback/`](migrations-rollback/README.md) holds
the hand-run down scripts (newest first, each `psql -1`), proven to return inventory A exactly; then start
the previous release.

### `kukatko storage rekey <slug>` — the storage move (runbook)

Moves one project's objects from the **store root** (an empty `projects.storage_prefix`) under its own prefix
`projects/<uid>/`: originals, thumbnails, streaming segments, sidecars and `families.yaml`. Only a project
that predates projects needs it — on an existing instance that is the bootstrap project `v`; a project
created since always had its own prefix and is refused (`already_prefixed`). **Nothing ever starts it
automatically.** Design: `internal/rekey` (the safe per-photo move) and `internal/rekeyjob` (the job).

| Invocation | Does |
| --- | --- |
| `kukatko storage rekey v --dry-run` | Changes nothing. Prints how many photos and bytes of originals would move, the target prefix and the first 20 planned moves (`old → new (+N derived keys)`); refuses exactly what a start would refuse. |
| `kukatko storage rekey v` | **Starts** the move: switches the project's storage prefix (new uploads land in the new layout at once), records a `running` run with its totals, queues the `storage_rekey` job — all in one transaction, audited (`project.storage_prefix`, `project.rekey.start`). |
| `kukatko storage rekey v --status` | Prints the latest run: state, photos and bytes moved of the total, timestamps, the error count, `derived failed: N` when moved photos keep thumbnails/segments at the root, and the last 5 errors. |
| `kukatko storage rekey v --pause` / `--resume` | Pauses the running move (the job stops after the photo in hand and re-reads the run once a minute) / resumes it (the waiting job is made runnable at once). Audited. |
| `kukatko storage rekey v --retry` | Starts another pass of a **failed** move over every photo still in the old layout and every recorded derived failure. Audited. |

The same actions over the API: `GET`/`POST /projects/{uid}/rekey`, `POST …/rekey/{pause,resume,retry}`
(maintainer, [`API.md`](API.md)), `kukatko ctl projects rekey <slug> [--status|--pause|--resume|--retry]`,
and **in the app**: the „Úložiště" card of `/system/projects/<slug>` starts the move (behind a confirmation),
shows its live progress (photos and bytes, percent, the rate and ETA, the last key, the errors) and pauses,
resumes and retries it; `/system/projects` marks a project being moved with „přesun 42 %"
([`FRONTEND.md`](FRONTEND.md)).
Every report goes to **stderr**; the command exits **0 unless the run it reports is `failed`** (or the
action was refused — `already_running`, `already_prefixed`, a pause of a run that is not running, …).

**How it moves.** The server's worker runs the `storage_rekey` job in a **one-slot pool**: one pass takes
`storage.rekey.per_second` photos (default 10, about a second of work) and requeues itself, so the slot is
never held for more than a few seconds and a restart loses at most the photo in hand. Per photo, in
`file_path` order: copy the original to its new key (server-side on R2, a hard link on a disk) → **verify**
the copy (size, and ETag or SHA-256 against `photos.file_hash`) → point the row at it (`photos.file_path`,
`photo_files`) in one transaction → copy the thumbnails/segments/sidecar that exist → **only then** delete the
old objects. A crash anywhere leaves both copies or only the verified new one; the next pass re-verifies and
continues. A photo that fails is recorded (last 50 errors kept) and skipped; more than 100 errors fail the run.

**Derived objects that did not move.** A thumbnail or segment copy that fails **after** the row moved (an
R2 429/5xx is a per-photo failure, not a dead store) cannot undo the photo — its original is verified under
the new prefix — so the photo still counts as moved and its old original is deleted, but **none of its root
thumbnails or segments are deleted**. The photo is recorded durably in `storage_rekey_derived_failures`
(migration 0115: photo uid, the kinds still at the root, the last error, attempts) and the run's
**`derived_failed`** counts those rows (`--status`, the API, the „Úložiště" card). Once the pass has no photo
left in the old layout, every further step of the job — and the pass a `--resume` or `--retry` starts —
re-plans the derived objects of the recorded photos: a prefixed copy that exists is kept, a missing one is
copied from the root, the root copies are deleted only once every prefixed one exists, and the record is
cleared. Before closing, the run also **sweeps the bucket**: a moved photo whose thumbnails or segments a
listing still finds at the root (e.g. from a move before 0115) is recorded and retried the same way.

**The sweep and the retry own the same objects.** The sweep records not just the photo but **every root key it
found for it** (`storage_rekey_derived_failures.root_keys`, migration 0116), and the retry handles those keys
besides the ones its own plan names. So an object no plan knows — a thumbnail of a size no longer registered,
a segment of a rendition nobody recorded, a name the streaming layout cannot hold — no longer makes the sweep
find "the same photo again" after every retry (before 0116 the move could go round `pending cleanup` forever
on such a stray). What the retry does with a recorded key: a key the layout's own builder spells for the
photo's hash (a registered thumbnail size, a valid rendition + segment name) **moves** under the prefix like
any other; any other key in the root `thumb/` or `hls/` tree is never served, so it is **deleted** — both trees
are regenerable from the original. Only keys of those two trees named after the photo's own content hash are
ever touched; an original never is. Deletion still waits for every copy of the photo, as above. The sweep
lists the root `thumb/` and `hls/` once per close attempt (not per photo, not per retry pass); a photo is news
only the first time it is recorded, so a stray whose move keeps failing ends the run `failed`
(`derived_failed`) for a `--retry` instead of another round. A root object whose hash names **no** photo of
the project is never matched: it stays at the root for `maintenance scan` to report as an orphan.

**Jobs during a move.** The `thumbnail`, `hls_transcode` and `sidecar` jobs publish a photo's derived objects
under the prefix of its original, and the move snapshots those objects when it steps the photo. So while a
project's move is `running`, a job for one of its photos still in the old layout is **deferred** (requeued
later, no attempt burned — `internal/rekey.Gate`) until the move has taken the photo; it then publishes under
the new prefix. A paused move defers nothing.

**The photo lock.** Each photo the move takes is guarded by a per-photo Postgres advisory lock
(`internal/rekey/lock.go`). The move holds it **exclusively** from just before it commits the row under the
new prefix until the photo's thumbnails, segments and sidecar are copied and the root ones deleted. Every
publisher holds it **shared** while it re-reads the photo's key and uploads: the thumbnailer around each
size's upload (the on-demand rebuild too), the encode around the segment upload and the rendition row, the
sidecar job around the write. So a publish lands either before the move copies (at the root, and the move takes
it along) or after the move is through (under the new prefix) — never in between, where the stale root copy
would overwrite the fresh tile of an edit or the fresh one would be left at a root already emptied. A publisher
never waits for the lock: one that finds the move holding the photo — or finds the photo committed by a move
that crashed before finishing it (`pending_photo_uid`) — is **deferred** (the thumbnail and sidecar jobs, the
encode), or answered **503 with `Retry-After: 5`** (the on-demand `POST /photos/{uid}/regenerate-thumbnail`).
The move does wait for a publish in flight (a thumbnail upload; for a video, its segment upload).

**Deferrals back off.** A move deferral is requeued after as long as the job has waited already — one minute
for a fresh job, then roughly doubling, at most **30 minutes** (`worker.WaitedDelay`). That matters when the
move's own job is dead-lettered while its run still says `running` (below): nothing steps the move, so every
waiting thumbnail, encode and sidecar job keeps being deferred, but at most every half hour rather than every
minute, until `--pause` + `--resume` restarts the move. Expect the thumbnail and encode queues of the project to
wait while the move runs — that is the deferral, not a stuck worker.

**Trash purge during a move.** The purge (a single photo, empty trash, `purge-older`, and the daily
retention) deletes each photo **under the same lock**, shared, **waiting** while the move copies that photo:
it then deletes the objects of the key the row has by then. A move that copied a photo's original but finds
the photo purged before it could commit deletes its copy again, so nothing of a purged photo survives under
the new prefix. A photo a crashed move committed and has yet to finish is not purged (single purge: **503**
with `Retry-After`; a batch counts it as failed and the next run takes it).

**Repairs during a move.** `maintenance repair --delete-orphan-segments` and `--root-derived` delete at the
store root, which is exactly what a move is emptying: a photo it has committed but not finished has its row
under the prefix and its segments still at the root, where the sweep would take them for orphans.
`--import-orphans` is the mirror case: mid-move, a copied-but-uncommitted original has no row and looks like
an orphan, and importing it could bring back a photo the user purged. All three are **refused while any
storage move is `running` or `paused`** — the CLI exits non-zero with
`a storage move is under way: … is refused while the storage move of project <uid> is <state>`, the API
answers **409** — and nothing is deleted or imported. Run them again once the move is `done` (or `failed`:
the root segments of moved photos are then kept by the sweep, and `--root-derived` moves them).

**Another project in the root layout.** Thumbnail and segment keys are named after a content hash, which is
unique only within a project. While any project other than the moved ones is still in the root layout (none
is today: `project.Create` always gives a prefix), the closing sweep, `maintenance scan`'s `root derived`
and `--root-derived` look at nothing at the root, like the final clean-up of sidecars and `families.yaml`.

**Before you start — pre-move checks.** Run these against the project (here `v`) first; each must return
**0 rows**:

```sql
-- every row has a size and a SHA-256 the copy can be verified against
SELECT uid, file_path, file_size, file_hash FROM photos
WHERE project_id = 'pj0000000000000000veselice'
  AND (file_size <= 0 OR file_hash !~ '^[0-9a-f]{64}$');
-- no original over 5 GiB: a server-side copy cannot take it (ErrTooLargeToCopy),
-- so such a photo stays in the old layout and the run can never end done
SELECT uid, file_path, file_size FROM photos
WHERE project_id = 'pj0000000000000000veselice' AND file_size > 5 * 1024^3;
```

A row with no size or a malformed hash fails verification and is left behind; an original over 5 GiB needs
a multipart copy the move does not do — resolve either before starting.

**When to start.** Only after the multi-tenant release is live and verified — step 7 of the upgrade runbook
above: inventory B diffed clean, the UI spot-checked, **a fresh backup** taken. **Keep that pre-move dump**:
once old keys are deleted it is the **only rollback** of the move. Then:

1. `kukatko storage rekey v --dry-run` — the counts must match what the library holds (photos, bytes of
   originals; `kukatko maintenance scan` beforehand should report nothing missing).
2. Start it at a quiet time (`kukatko storage rekey v`, or the `ctl`/API equivalent). It needs the **running
   server's worker** — the command only queues the job.
3. **Watch** it: `/system/projects/v` (the bar and the measured rate — about `per_second` × 60 photos a
   minute — refresh every 3 s), `--status` (photos moved / total should climb by about `per_second` a
   second), the maintainer's `/jobs` (one `storage_rekey` job, queued/running), and the server log. The
   library stays usable throughout; a photo whose row was just moved is served from its new key.
4. If it competes with serving, `--pause` (or „Pozastavit" on the page), lower `storage.rekey.per_second`,
   restart, `--resume` („Pokračovat").

**Old keys have no grace period.** A photo's old objects are deleted the moment it has moved. A browser tab
opened before that still holds signed URLs of the old keys, so its tiles (and a playing video's segments)
may 404 until the page is reloaded — expected, nothing to repair.

**What "done" means.** The run's state is `done` and `finished_at` is set: every photo row of the project
points under `projects/<uid>/`, **no photo is left in `storage_rekey_derived_failures`** (`derived_failed`
is 0 — a run with derived failures ends `failed` instead), the sidecar of every photo and the project's
`families.yaml` have been re-queued for writing under the new prefix, and the root `families.yaml` and root
sidecars that have their replacement listed under the prefix are deleted (nothing at the root is touched
while another project still lives there). Then:

1. **Run `kukatko backup` right away** — the first backup of the new layout; the pre-move dump stays the only
   way back to the old one.
2. Run `kukatko maintenance scan`: `root derived` should be 0 (if not, `maintenance repair --root-derived`);
   anything else still in the root layout is reported as an orphan or legacy object (an object no row named)
   — review it before deleting anything.

**What to do with `failed`.** `--status` lists the last errors (`key: message`) and, when photos keep
derived objects at the root, `derived failed: N`. A run fails when it met more than 100 errors, when its pass
ended with photos it could not move (the error says how many), or when photos still have thumbnails or
segments at the root (`derived_failed` > 0; the card shows the count with „Zkusit znovu"). Typical causes: an
original missing from the store (`maintenance scan` reports it — restore it from backup, or archive and purge
the photo), a copy that does not verify (the store is corrupting writes — stop and investigate), a store that
refuses or throttles requests (credentials, quota, rate). Fix the cause, then `--retry`: the next pass takes
again every photo still in the old layout and every recorded derived failure; nothing already moved is
redone. The old original is **never** deleted before the row points at a verified copy, so a failed run
loses nothing.

**If the job is dead.** A failure that is not about one photo (database or store unreachable) is retried by
the queue with backoff; after its attempts are spent the job is dead-lettered while the run still says
`running` — and nothing steps it. Unstick it with **`--pause` then `--resume`** (or „Pozastavit" /
„Pokračovat"), which queues a fresh job; requeueing it from the maintainer's `/jobs` works too. Until then the
thumbnail, encode and sidecar jobs of photos the move has not taken keep being deferred, backing off to one
attempt every 30 minutes (see *Deferrals back off* above).

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
    project: v                            # the slug the catalogue commands work in (/api/v1/p/v/…)
```

The file is written **atomically and always with mode `0600`** (the directory `0700`); an existing
world-readable file is tightened before writing. **The token is never printed anywhere** — not to the
log, not to an error message, not to `ctl context list`.

The group is `ctl context`; `ctl config` (its old name) is an alias, and so are the old subcommand
names (`set-context`, `use-context`), so existing scripts keep working.

| Command | Meaning |
| --- | --- |
| `ctl context set [<name>] [--server <url>] [--token <t> \| --token-stdin] [--project <slug>] [--current]` (alias `set-context`) | creates/updates a context — the **current** one when no name is given; the first one created becomes current. An omitted field is preserved (changing the URL keeps the token and the project). `--project ""` clears the project; a malformed slug is refused before saving |
| `ctl context list` (alias `get-contexts`) | lists the contexts; for the token only `stored`/`not set`, plus the `PROJECT` column |
| `ctl context use <name>` (alias `use-context`) | switches the current context |
| `ctl context current` | what the next command would use — context, server, token `set`/`not set`, project — **after** `--project` and the `KUKATKO_*` overrides |

`--token` is visible in `ps` to the whole machine — **prefer `--token-stdin`**:
`printf '%s' "$TOKEN" | kukatkoctl context set prod --server https://… --token-stdin --project v`.

**Env overrides the active context, field by field:** `KUKATKO_SERVER`, `KUKATKO_TOKEN` and
`KUKATKO_PROJECT`. So `KUKATKO_TOKEN` alone re-credentials the stored context without touching the file.
With no file and no context, the variables are enough. The `--context <name>` flag selects a context
other than the current one, `--ctl-config <path>` a different file.

#### The project

Every catalogue command works in **one project** and sends its requests under that project's prefix,
`/api/v1/p/<slug>/…` (see [`API.md`](API.md) → *Project prefix*). The project is picked, first match wins:

1. **`--project <slug>`** — a **persistent** flag of `ctl`, so it goes before or after the subcommand
   (`kukatkoctl --project klub photos list` == `kukatkoctl photos list --project klub`). No subcommand may
   declare a local `--project` (or `--output`): it would shadow the persistent one on that command.
2. **`KUKATKO_PROJECT`**.
3. **The context's `project`** — set with `ctl projects use <slug>` or `ctl context set --project <slug>`.

A project-scoped command with none of the three fails **before any request** with exactly
`no project selected: run 'kukatko ctl projects use <slug>' or pass --project`.

**Instance-level commands ignore the project:** `ctl context`, `ctl projects` and `ctl users` talk to the
bare `/api/v1` whatever is selected (they are marked so in `cmd/kukatko/ctl.go`, `markInstance`; every
other top-level command is project-scoped). There is no `ctl auth`/`jobs`/`system`/`backup`/`maintenance`
— those are server-side subcommands — but they would be instance-level too.

**One token, several projects.** An API token belongs to its owner, not to a project: it works in every
project the owner is a member of (a maintainer's in every project). Two contexts may share a server and a
token and differ only in `project`; for a one-off, `--project` is enough. A project the token cannot enter
answers `404`, like a missing one.

| Command | Meaning |
| --- | --- |
| `ctl projects use <slug>` | stores `<slug>` on the current context (or the `--context` one); no request is sent, the slug is checked for its form only. Warns when `KUKATKO_PROJECT` would override it |
| `ctl projects list` | **your** projects: a maintainer gets every project of the instance (`GET /projects`, see below); anybody else their memberships from `GET /auth/me` — slug, name, role, `active`/`pending`/`inactive`, uid (`-o json` prints the `projects` array as the server sent it) |

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
| `ctl photos get <uid>` | detail `GET /photos/{uid}` (+ albums, labels, `ocr_text`, the hand-attached people and, by default, the face roll-call) |
| `ctl photos search <query>` | `GET /search?q=…&mode=…` |
| `ctl photos image <uid>` | saves a rendition to a file and prints the path |
| `ctl photos edit <uid>` | `PATCH /photos/{uid}` — the whole editable metadata surface (`editor`/`admin`) |
| `ctl photos faces <uid>` | `GET /photos/{uid}/faces` — the detections, their markers, and who they might be |
| `ctl photos similar <uid>` | `GET /photos/{uid}/similar` — the visual neighbours with their cosine distance; `--limit` 1…100 |
| `ctl photos upload <path>…` | `POST /upload` — streams files in through the ordinary ingest path (`editor`/`admin`); each part declares its size (`Content-Length`), so a file over a project limit is refused before the server stages a byte of it |
| `ctl photos archive` / `unarchive <uid>` | `POST /photos/{uid}/archive`+`/unarchive` — to the trash and back (reversible) |
| `ctl photos hide` / `unhide <uid>` | `POST /photos/{uid}/hide`+`/unhide` — out of the library grid, nothing deleted |
| `ctl photos renditions <uid>` | `GET /photos/{uid}/renditions` — a video's encoded streaming qualities, with the encode stamp of each; see below |
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
| `--album` / `--label` / `--task` | — | scope to an album/label/task uid |
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
| `ctl photos rebuild video <uid>` | `POST /photos/{uid}/process/hls_transcode` | the streaming encode — every configured quality, **replacing** what was recorded |
| `ctl photos rebuild storyboard <uid>` | `POST /photos/{uid}/regenerate-storyboard` | the scrub-preview sprite; the cached one is **deleted** first |

A re-detection replaces rather than appends, and hands each assignment to the face that comes back in the
same place, so redoing the detection never leaves duplicate faces behind and never un-names anybody. A face
found somewhere new arrives unassigned.

When the embeddings box (or mapy.com) is offline the work is **queued** instead of failing: a forced job goes
into the queue and runs when the service is back. The command says so and exits 0 — a sleeping box is not an
error. Two requests for the same photo collapse into one job (queue dedup is keyed on type + photo uid).

**The two video rebuilds are always queued**, and that is the only way they differ from the four above. A
streaming encode and a storyboard sprite are each a full pass over the clip — minutes of ffmpeg for a long
video, and the encode is the most expensive job this application runs — so neither happens inside the
request. They therefore answer with a **step and its state** rather than with a result:

```
hls_transcode: queued; the worker runs it in the background
hls_transcode: the recorded result is the previous run (2026-09-01 08:30); the one you asked for is queued behind it
hls_transcode: skipped; this step does not apply to this photo, or is switched off here
```

The middle line is the one worth reading twice. The processing report resolves **persisted evidence before
the queue**, so a clip that is already encoded answers `done` with the *old* stamp — the encode this command
just scheduled has not happened yet. It really is queued; the line says so rather than printing a state word
an operator would read as "nothing happened".

`rebuild video` needs no endpoint of its own because `process/hls_transcode` is already the exception to the
trap at the top of this section: that handler re-encodes wholesale instead of skipping a clip it has already
done, so scheduling the step **is** the rebuild. `rebuild storyboard` is the opposite case and needs its own
endpoint precisely because of the trap — the sprite renderer skips one that already exists, so the cached
sprite is deleted first and only then is the render queued. A clip that can never have a preview (a still, a
live photo, a video of unknown length) is **refused** rather than queued, and on an instance that could not
render a replacement at all the stale sprite is kept rather than thrown away.

```bash
kukatkoctl photos rebuild thumbnail pht01h2j3
kukatkoctl photos rebuild faces pht01h2j3       # → "face_detect: rebuilt, 3 faces on the photo"
kukatkoctl photos rebuild embedding pht01h2j3   # → "image_embed: … a forced job is queued …" when the box sleeps
kukatkoctl photos rebuild video pht01h2j3       # → "hls_transcode: queued; the worker runs it in the background"
kukatkoctl photos rebuild storyboard pht01h2j3  # → "storyboard: queued; the worker runs it …"
```

#### `ctl photos renditions` — what the encode actually produced

```
ctl photos renditions <uid>
```

The one command that reads `photo_hls_renditions`. `photos get` says only whether a clip streams at all, the
master playlist names the qualities but not when they were made, and the processing report knows only that
*some* rendition exists — so "is what is stored still current?" (after a changed rendition plan, a re-encode,
a restore that cleared the segments) had no answer short of the database.

```
RENDITION  PICTURE    BITRATE      CODECS                 SEGMENTS  LENGTH  ENCODED
1080p      1920×1080  5.1 Mbit/s   avc1.640028,mp4a.40.2  7         2:05    2026-08-09 10:11

1 rendition (1080p) · oldest encoded 2026-08-09 10:11
```

The footer names the **oldest** encode, which is the one that decides whether the clip needs redoing. The
media playlist is deliberately not reported: it is the largest column of the row and is already served,
rewritten, at `/photos/{uid}/hls/{rendition}/index.m3u8`. `duration_ms` in `-o json` is the rendition's own
measurement rather than the catalogue's, so a container that lied about its length is visible here. Any
authenticated role may read it — it says no more than the playlist they may already play. A still, a video
the encoder has not reached and an instance with streaming switched off all print *"no streaming renditions:
this photo has never been encoded"* and exit 0; an unknown uid is a 404.

#### `ctl process` — the library-wide backfills

```
ctl process hls [--all] [--yes]
```

The streaming encode over the whole library. By default it schedules only the videos with **no rendition at
all** — what a restore leaves behind (the backup carries the database and the originals, never the segments,
see [`docs/RESTORE.md`](RESTORE.md)) and what an import onto an instance whose box was asleep leaves behind.
`--all` schedules every live video instead: a full re-encode, which is how a newly configured quality level
reaches the videos that predate it, since the job replaces a rendition rather than adding to it.

`--all` needs **`--yes`**. It destroys nothing — every rendition it replaces can be produced again — so it is
not one of [the irreversible commands](#the-irreversible-commands-and-their-gate); the gate is there because
a mistyped flag would occupy the only worker slot that encodes for a day.

Either way this only fills the queue. The worker encodes one clip at a time, so the number reported is **jobs
scheduled, not videos finished**; watch them land with `ctl photos renditions <uid>` or the admin dashboard.
It needs the **maintainer** role.

```bash
kukatkoctl process hls                    # → "12 videos were scheduled; the worker drains them …"
kukatkoctl process hls                    # → "nothing to do: no video is waiting for this work"
kukatkoctl process hls --all --yes        # the whole library, hours of background work
```

Only the encode is here. Every other `/process/*` backfill already has a local counterpart on the machine
that runs the instance — `kukatko maintenance repair --embeddings --faces --places --thumbnails …`,
`kukatko sidecar backfill` — and that is where an operator reaches for them. The encode had neither a flag
nor a subcommand: it was reachable only with curl, which is the whole reason this command exists.

#### `ctl photos get` — the whole photo in one request

Beyond the metadata, `get` reports **`ocr_text`** (the text the recogniser read *in* the photo; the table
folds it onto one line, the full reading is in `-o json`/`-o llm`) and **who is on the photo**, in two
rows. **`FACES`** is the face roll-call: the named subjects followed by a count of the detections nobody
has assigned yet. Reading the photo whole is the point of the command, so it is asked for **by default**;
on the server it stays opt-in (`?people=true`) because assembling it costs a face↔marker match a plain
read should not pay for, and **`--people=false`** skips it. When the response carries no roll-call at all
— you turned it off, or the instance has no face backend — the row reads `- (not reported)`, which is
**not** the same as "nobody is on this photo". **`PEOPLE`** is the people somebody attached **by hand** —
no box, no detected face, for what detection cannot see (anybody in a video at all — detection does not run
on footage — a profile, a back of a head). That one is always reported, so an empty row really does mean
nobody was attached. The date and the location are printed beside their provenance (`estimated, year, manual` /
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
| `ctl albums comments <uid>` | `GET /albums/{uid}/comments` — the album's thread, oldest first, rendered as prose like `ctl comments list` |
| `ctl albums comment <uid> <text>` | `POST /albums/{uid}/comments` — plain text under your own account, at most 2000 characters |

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
| `ctl subjects list` | `GET /subjects` — a **bare `{"subjects":[…]}`**; `PHOTOS` = distinct photos, `MARKERS` = faces. A nickname rides inside `NAME` — `Bohumil Nečas („Bohouš")` — rather than taking a column most rows would leave empty |
| `ctl subjects get <uid>` | `GET /subjects/{uid}` |
| `ctl subjects photos <uid>` | `GET /subjects/{uid}/photos`; `--limit`/`--offset` |
| `ctl subjects create <name>` | `POST /subjects`; `--nickname`, `--type`, `--notes`, `--cover`, `--favorite`, `--private`, `--birth-year`, `--death-year` |
| `ctl subjects rename <uid> <name>` | `PATCH /subjects/{uid}` with the stored record read back first; `--nickname` changes what people call them in the same write — **omit it to keep** the stored one, pass `--nickname ""` to clear it |
| `ctl subjects merge <source-uid> <keeper-uid>` | `POST /subjects/{uid}/merge` — **irreversible**, needs `--yes` |
| `ctl subjects delete <uid>` | `DELETE /subjects/{uid}` — **irreversible**, needs `--yes` |

A subject's gallery is the only paginated subject endpoint and returns the **`/photos` envelope**, so it
prints as a photo list. It does not read the catalog filters, so `ctl` does not offer them either.

**There is no `ctl subjects edit`.** `PATCH /subjects/{uid}` rewrites the whole editable record rather than
patching it, so a flag-per-field edit would silently erase everything you did not mention. `rename` is that
edit done safely: it reads the record, changes the name, and sends the rest back untouched — otherwise
renaming a pet would reclassify it as a person and drop its notes, cover and life years on the way.
**`--nickname` is the one exception**, and only because it is the field most often corrected on its own:
it is applied on top of the record that was read back, so everything else still survives the write. The
nickname is searchable like the name (`ctl photos list -q 'person:Bohouš'` finds it) and is **never** part
of the slug, so changing it breaks no link.

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

#### `ctl family`

The genealogy over subjects (`internal/familyapi`) — who is whose parent, whose partner and whose child.
Reading needs any role, writing `editor`/`admin`.

**The family is the node, not the edge:** a family is a couple, a lone parent or a bare sibling group, plus
their children, and parents, siblings, partners and children are all *derived* from it, which is why they
cannot contradict each other. `sibling` is a role all the same, because the family two siblings share may have
to be created: when their parents are not in the library it is a family with **no partners at all** (migration
`0076`).

| Command | Meaning |
| --- | --- |
| `ctl family relations <subject-uid>` (alias `show`) | `GET /subjects/{uid}/relations` — the four derived lists as one table; `FAMILY` is the uid `family edit` takes |
| `ctl family add <subject-uid> <role> [<related-subject-uid>]` | `POST /subjects/{uid}/relations`; role is `parent`, `child`, `partner` or `sibling`. `--name` instead of a uid, `--child-kind birth\|adopted\|step` |
| `ctl family remove <subject-uid> <related-subject-uid>` | `DELETE /subjects/{uid}/relations/{uid2}` — **irreversible**, needs `--yes`, offers `--dry-run` |
| `ctl family edit <family-uid>` | `PATCH /families/{uid}` — the union itself: `--kind marriage\|partnership\|unknown`, `--from-year`, `--to-year`, `--note` |

`relations` prints one table rather than four because the four lists answer one question and `ROLE` is the
column that separates them. `KIND` is what each relation is recorded as — how a child belongs to their family
(`birth`/`adopted`/`step`), or what tied a couple together. A family whose second partner nobody recorded is
listed under the role `lone-parent`, reading `- (no partner recorded)`, and is **left out of the partner
count**: it is a family all the same — it is where that person's children hang, and its uid is what `family
edit` takes — but there is nobody on the other side to call a partner. A **sibling group** names nobody at all,
so it never appears among the partnerships; it shows up only as the `sibling` rows of its children, and `family
edit` prints its partners as `- (sibling group: no parent recorded)`.

**`add <subject> sibling <other>`** makes the other person another child of the family the subject is a child
in — a sibling is still derived, not stored. Where there is no such family, because nobody recorded the parents
and quite possibly nobody can, a family with **no parents at all** is created for the two of them; no
placeholder "unknown parent" subject is invented, and a real parent recorded on any one of the group later
attaches to *that* family, so the whole group becomes their children at once. Two people who are already
children of **different** families are refused with a 409: honouring that would mean taking one of them out of
a parentage somebody recorded on purpose.

**`add` resolves `--name` client-side**, unlike `ctl faces assign --name`, and the difference matters: the
inline half of the endpoint *always* creates, so handing it a name that already exists would quietly split one
person into two. A name is matched case-insensitively against the stored name **or** slug; one match relates to
that person, several are refused naming every candidate (a uid is then the only honest answer), and **a name
that matches nobody creates them** — subject and relation in one transaction, so a refused relation leaves no
orphan person behind. That inline creation is what makes filling a tree bearable: a great-grandmother nobody
photographed is otherwise a trip to another screen and back, once per person, for every generation nobody wrote
down. `--type`, `--birth-year`, `--death-year` and `--notes` describe a person being *created*; beside the uid
of somebody who already exists they are refused rather than ignored. The role is the **other** person's side of
the relation, and the report names both people, because neither a person nor an agent can read a uid.

Nothing is destroyed by recording a relation, so `add` needs no `--yes`. A relation the tree cannot hold — a
cycle (somebody made their own ancestor), a second parentage, a second family for one couple — is refused by
the server with a **409** and changes nothing; the request was well formed, the tree is what stood in its way.

**`remove` reads how the two are related first**, which is also what it reports: `Marie Nečasová (sub02) as the
parent of Anna Nečasová (sub01)`. Which relation goes follows from the rows, not from the command line. Two
people who are not related fail before a request is spent. Two **siblings** can be parted only when the family
they share records no parent — there the link is the whole relation, and the group is deleted once it drops
below two children; with a parent on it they are siblings *because* they are that parent's children, so the
removal is refused naming that parent and the way to part them is to remove it. Removing one parent leaves the
other — the child keeps the parentage it still has, because "X is no
longer Y's father" must not quietly take Y's mother away too. Both people and every photo they are on survive;
only the line between them goes, and nothing records who was whose parent once it is gone, which is why it
needs `--yes`.

**`edit` rewrites the whole record**, exactly as `PATCH /families/{uid}` does: a year or a note not passed is
cleared, and an omitted `--kind` falls back to the server's default `partnership` (what the archive can
honestly claim about most pairs; `unknown` admits nobody knows which it was). Pass everything the family should
end up carrying, not only what changed — and an edit naming no flag at all is **refused rather than run**,
because it would erase rather than do nothing. Each year lies within 1800…this year with `--to-year` not before
`--from-year`, checked locally against the same rule the SQL CHECK enforces. The refreshed family prints with
its partners **named**, which costs one lookup per partner the API's answer does not carry; a lookup that fails
leaves the uid printed as it is rather than failing a write that already happened.

```bash
kukatkoctl family relations sub1a2b3
kukatkoctl family add sub1a2b3 parent --name "Marie Nečasová" --birth-year 1921   # creates her if new
kukatkoctl family add sub1a2b3 child sub9z8y7 --child-kind adopted
kukatkoctl family edit fam1a2b3 --kind marriage --from-year 1948 --note "oddáni v Křtinách"
kukatkoctl family remove sub1a2b3 sub9z8y7 --dry-run
```

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
| `ctl clusters list [--limit N] [--offset N]` | one page of `GET /faces/clusters`; `SUGGESTION` = the nearest named subject + its cosine distance |
| `ctl clusters assign <cluster-uid> [<subject-uid>]` | names **every** face of the group; `--name` instead of a uid |
| `ctl clusters remove-face <cluster-uid> <photo-uid> <face>` | drops one face that does not belong, before naming |

The listing is **paged** (24 groups by default, 100 at most) and shows only the groups the server has
already prepared: a group is listed once its cached summary exists, and the line under the table says how
many are still being prepared in the background and where the next page starts. Asking for the listing is
what schedules that preparation (the `face_cluster` job, at most one pass queued at a time), so a library
whose groups have never been prepared fills in by itself a moment after the first `list`.

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

#### `ctl upload-links` — hand a link out again

Upload links (`internal/uploadlinkapi`), curator token. A link is visible only to its creator or an admin.
Creating, extending and revoking stay in the web UI; the CLI covers handing a link out again.

| Command | Meaning |
| --- | --- |
| `ctl upload-links list` | `GET /upload-links` — `{"links":[…]}`; the table's `URL` column is the absolute `/u/<code>` address on the context's server, or `unknown (restore-code)` for a link created before codes were stored |
| `ctl upload-links restore-code <uid> <code-or-url>` | `POST /upload-links/{uid}/restore-code` — stores the original code only when it matches the link's hash, so the URL never changes; a wrong code is the server's 422 |
| `ctl upload-links new-code <uid>` | `POST /upload-links/{uid}/new-code` — a fresh code; **the old URL stops working at once**, so it sits behind `--yes` (`--dry-run` names the link) |

#### `ctl projects` / `ctl users` — the system administrator

The instance's projects and accounts (`internal/projectapi`, instance-level routes — the selected project
is ignored), **a maintainer's token** — anybody else gets the CLI's 403 (except `projects list` and
`projects use`, which any account may run). A project is named by its **slug** (its uid works too) and an
account by its **username** (or uid); the CLI resolves either through the list first, so a typo is
"no such project/account" before anything is sent. A refusal prints the server's code and rule, e.g.
`invalid_slug: "Bad" must be 1–40 lowercase letters…`.

| Command | Meaning |
| --- | --- |
| `ctl projects list` | for a maintainer `GET /projects` — slug, name, state, members, photos, originals, `MAX FILE`, `QUOTA` (`unlimited` for none), uid (a non-maintainer gets their memberships, see [the project](#the-project)) |
| `ctl projects get <slug>` | `GET /projects/{uid}` — the detail incl. the storage prefix (`(storage root)` when empty) |
| `ctl projects create --slug <slug> --name <name> [--description <text>] [--max-file-bytes N] [--max-total-bytes N]` | `POST /projects`; you become its first admin. The slug is immutable; a limit left out is no limit (an explicit `0` is sent and refused by the server) |
| `ctl projects update <slug> [--name] [--description] [--max-file-bytes N] [--max-total-bytes N] [--clear-max-file-bytes] [--clear-max-total-bytes]` | `PATCH /projects/{uid}` with only the flags given (a clear flag sends `null`); no flag at all is refused before any request |
| `ctl projects deactivate <slug>` / `activate <slug>` | `PATCH {"active":false}` / `{"active":true}` — a deactivated project is entered by nobody but a maintainer; its data stays |
| `ctl projects add-member <slug> <username> --role <role>` | `POST /projects/{uid}/members` — an **approved** membership (`viewer`, `curator`, `editor`, `admin`); your own username enters the project yourself |
| `ctl projects rekey <slug> [--status\|--pause\|--resume\|--retry]` | The storage move ([runbook](#kukatko-storage-rekey-slug--the-storage-move-runbook)): no flag = `POST /projects/{uid}/rekey` (start), `--status` = `GET …/rekey`, the others `POST …/rekey/{pause,resume,retry}`; renders the run (state, photos and bytes moved, the last 5 errors). One flag at most |
| `ctl users list` | `GET /users` — every account: username, name, instance role (`, disabled`), its projects as `slug (role)` (`, pending` while unapproved) |
| `ctl users promote <username>` / `demote <username>` | `PATCH /users/{uid}` `{"instance_role":"maintainer"}` / `"user"`; demoting yourself is the server's `self` refusal |

`ctl users` used to be an alias of `ctl people`; the alias is gone — `ctl people` is the name directory
of the selected project (`GET /p/<slug>/users`).

#### `ctl members` — a project admin

The members of **one project** (`internal/memberapi`, `/api/v1/p/<slug>/members`), with the token of that
project's **admin** (or a maintainer's). The project is the selected one ([the project](#the-project));
there is no default — without one the command fails before any request. A member is named by its **username** (or uid), resolved
through the project's member list first, so a non-member is "no such member of the project" before
anything is sent. Refusals print the server's code: `self` (your own role, or leaving as the only admin),
`last_admin`, `invalid_role`.

| Command | Meaning |
| --- | --- |
| `ctl members list` | `GET /members` — username, name, role, `approved`/`pending`, last login, uid |
| `ctl members add <username-or-email> --role <role>` | `POST /members` `{identifier, role}` — an **exact** username or e-mail (case-insensitive), approved at once. The server answers the same whether the account was added, already was a member or does not exist, so the CLI only says *accepted: if … names an account, it is a member*; check `members list` |
| `ctl members role <username> <role>` | `PATCH /members/{uid}` — `viewer`, `curator`, `editor` or `admin`; never your own |
| `ctl members approve <username>` | `POST /members/{uid}/approve` — lets a waiting member in and mails them the project's link |
| `ctl members remove <username>` | `DELETE /members/{uid}` — the account stays; the project's only admin cannot be removed |

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

#### `ctl tasks` — the work queue

A task is a question about a frozen group of photographs, with the state of play and the conversation
that settles it (`internal/phototaskapi`). Reading a task and writing in its thread are open to every
signed-in role, viewers included; opening, editing, closing and changing membership need `editor`/`admin`.

| Command | Meaning |
| --- | --- |
| `ctl tasks list` | `GET /tasks`; `--state` (comma separated), `--open`, `--answered`, `--waiting`, `--search`, `--photo`, `--participant` / `--mine`, `--limit`, `--offset` |
| `ctl tasks show <uid>` | `GET /tasks/{uid}`; prints `Last activity` ("2026-09-19 17:21 by Tomáš Kozák") and `Waiting on you` (`yes`/`no`); an `Options` line when the task offers answers |
| `ctl tasks create [<photo-uid>…]` | `POST /tasks`; `--title` (required), `--body`/`--body-file`, `--query`, `--state`, `--option` (repeatable, ≤ 5), `--assign <user-uid>` (repeatable); uids from args or stdin, **or none at all** |
| `ctl tasks update <uid>` | `PATCH /tasks/{uid}`; `--title`, `--body`/`--body-file`, `--query`, `--state`, `--resolution`/`--resolution-file`, `--option` (replaces the whole set) or `--clear-options` (mutually exclusive) |
| `ctl tasks delete <uid>` | `DELETE /tasks/{uid}` — needs `--yes`; the photographs stay |
| `ctl tasks add-photos <uid> [<photo-uid>…]` | `POST /tasks/{uid}/photos` |
| `ctl tasks remove-photos <uid> [<photo-uid>…]` | `DELETE /tasks/{uid}/photos` |
| `ctl tasks comments <uid>` | `GET /tasks/{uid}/comments` |
| `ctl tasks ledger <uid>` | the **review ledger**: the task's whole frozen group as one row per photo — `UID`, `TAKEN` (rendered at its precision: `1974-06-14`, `1974-06`, `1974`, `1970s`), `SOURCE`, `LAST COMMENT` (first line, truncated) and `BY` — read from `GET /photos?task={uid}` (which carries `last_comment` under a task scope), paged through the whole group (capped at 1000); `-o llm` gives the compact rows. The agent's own final check before moving a batch to `review` |
| `ctl tasks comment <uid> [<text>]` | `POST /tasks/{uid}/comments`; `--body-file`; `--state <state>` moves the task with the comment in one transaction (a writer's token; closing still needs `tasks update --resolution`) |
| `ctl tasks participants <uid>` | `GET /tasks/{uid}/participants` |
| `ctl tasks assign <uid> <user-uid>` | `POST /tasks/{uid}/participants` — ask a particular person |
| `ctl tasks unassign <uid> <user-uid>` | `DELETE /tasks/{uid}/participants/{user}` |
| `ctl people` | `GET /p/<slug>/users` (the name directory of the selected project) — the uids the assign commands take |

**`ctl tasks list --waiting` is the command the loop turns on.** It lists the tasks whose move is the
token's: open, the account is on them, and somebody else acted last (`waiting_on_me`). A question a person
just opened and handed to the agent is on it; once the agent comments it is on the person; once they reply
it is on the agent again. `--answered` is the coarser signal — somebody **other than the token's account**
has written in the thread since the state last moved; a reply does not clear it, only a state change does
— and it stays truthful even when nobody remembered to advance the state. Both are relative to the
caller, so the agent's own context comment lights neither for the agent. In a table listing `--answered`
is the `NEW` column; `-o llm` carries `waiting_on_me`, `last_activity_at`, `last_activity_by` and
`last_activity_by_name` like every other field.

**Who is on a task mostly looks after itself.** Opening one, editing it, moving its state, changing its
membership or answering in its thread puts the actor on it, so `ctl tasks participants` is a report rather
than a register. `assign` is for the other case — asking a particular person before they have done
anything — and the `HOW` column tells the two apart: `acted`, or `asked by …`. `ctl tasks list --mine`
(the same as `--participant me`) is "what have I been put on?", which is how an agent finds the questions
a person handed it; it narrows whatever state filter is already given, so `--mine --open` is one call.

**Handing work over is one command.** `ctl tasks create --assign <user-uid>` (repeatable, uids from
`ctl people`) puts each person on the task as **asked**, in the opening's own transaction and audited
exactly as `ctl tasks assign` would be — there is no create-then-assign pair to get half-way through.
With `--state working` it reads "somebody should do this"; the default `question` state is "somebody
should answer this".

**A task need not be about photographs.** `ctl tasks create --title 'rename the wf: labels'` with no
arguments opens a task over nothing — agenda about the library rather than about particular pictures —
and photographs can be added later with `add-photos`. Where the uids come from follows the shell: with
arguments, the arguments; with none and a **pipe** on stdin, the piped uids, an empty pipe meaning no
photographs; with none and a **terminal** on stdin, nothing is read, so an interactive `create` opens
the task instead of waiting on a line nobody is going to type. Either way the result prints
`Photos  0 (no photos)` in words, so an accidentally empty pipe is visible rather than silent.

A task's photographs are read through the catalogue, not from a route of its own:
`kukatkoctl photos list --task <uid>` (or `q=task:<uid>`), so the frozen group is browsed with every
ordinary filter, sort and page on top of it.

Closing a task (`--state done` or `--state rejected`) needs a `--resolution`: "rejected" is a real
outcome — deciding a doubt is unfounded, or that a date cannot be established — and what must never exist
is a task that is closed and silent. Reopening one clears the closing marks and keeps the text.

`delete` is behind [the gate](#the-irreversible-commands-and-their-gate) and is the exception rather than
the way work ends. Finished work is **closed**: the frozen list of photographs is the record of which
pictures a batch of edits touched, and deleting the task destroys that along with every answer written
under it.

**Quick answers.** Most questions are a choice, so a task may offer up to **five answer options**
(`--option "1936" --option "1938" --option "nevím"`; each trimmed, non-empty, ≤ 60 characters,
distinct — a broken set is a 400 that names the rule). The web page draws them as large buttons under
the question, and **a tap posts an ordinary comment whose body is the option text verbatim** — there is
no separate answer endpoint, so `ctl tasks comments` reads the answer as before and the agent matches it
against its own options exactly. A task in `review` with no options of its own gets a built-in pair
whose posted text is **fixed regardless of the UI language**: `Schvaluji.` (approve) and
`Vrátit k přepracování.` (send back). Match those two strings; never their localised labels. `tasks
show` prints an `Options` line when set and `-o llm` carries `options` like every other field.

Long text is awkward on a command line, so `--body-file` and `--resolution-file` read it from a file. The
comment is attributed to the **token's own account**, always — an agent answers as itself and never puts
words in a person's mouth, in a thread whose whole value is that it records who remembered what.

```bash
# Whose move is it? — the questions waiting on this account.
kukatkoctl tasks list --waiting -o llm --fields uid,title,state,last_activity_by_name

# What has somebody else answered since the state last moved?
kukatkoctl tasks list --answered -o llm --fields uid,title,state,comment_count

# Open a question over a batch, then send its link to whoever knows.
kukatkoctl tasks create --title "V kterém roce se přestavoval dům čp. 2?" \
  --body-file /tmp/context.md --query 'camera:Olympus dated:no' ph1 ph2 ph3

# A choice: the person taps one of these and the comment is the text, verbatim.
kukatkoctl tasks create --title "Je na nápisu rok 1936, nebo 1938?" \
  --option 1936 --option 1938 --option nevím ph4 ph5

# Write the answer in, then close the task with the reason.
kukatkoctl tasks comment tk7hcm… --body-file /tmp/answer.txt
kukatkoctl tasks update tk7hcm… --state done --resolution "1987 podle kroniky"
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
(`kukatko backup`, `restore`, `migrate`, `maintenance`, …). [`ctl process hls`](#ctl-process--the-library-wide-backfills)
is not a hole in that rule: it schedules queue work rather than doing it, exactly as the admin UI's own
button does, and it is here because it is the one backfill with no local counterpart at all.

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
  video posters, the `storyboard/` scrub-preview sprites and the `avatar/` subject renditions the
  people index draws, the last two of which live **only** here and are never
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
  application's own `/api/v1/p/<slug>/photos/…` routes and the app streams the bytes itself, which is what a
  bucket with no Worker in front of it (development against a local MinIO) needs. Exactly one of the
  pair fails startup, since it means either URLs the Worker will reject or a secret that signs nothing. Neither the secrets nor the access key are ever logged or appear in an error.
  **The Worker itself is not in this repo** — the bucket, its source, bindings, and hostname are defined and deployed by
  Terraform in the infra repo (root module `cloudflare-r2/`). Rotating the signing secret therefore reaches into
  **two repositories** — procedure below.
- **Storage move key (`storage.rekey.*`, `internal/config` + `internal/rekeyjob`):** `per_second`
  (**default 10**, must be positive — `ErrInvalidRekeyRate` at startup otherwise) is how many photos the
  storage move (`kukatko storage rekey`, below) takes per second at most **and** the batch one pass of the
  `storage_rekey` job takes, so the job holds its single worker slot for about a second at a time. Lower it
  when the move competes with serving the library. Env: `KUKATKO_STORAGE_REKEY_PER_SECOND`.
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
  **What is copied is decided by the key, in one place** (`internal/storekeys`): originals and metadata
  sidecars travel; the derived prefixes `thumb/` (the thumbnail cache) and `hls/` (a video's streaming
  segments) are **deliberately omitted**, being reproducible from an original by an ordinary background
  job and therefore not worth paying for twice — on the staging instance the segments alone were 470 MB
  against 1.9 GB of video originals, and they grow with every encode. A foreign object under none of
  Kukátko's prefixes **is** backed up: its value is not this code's to judge. What that costs on a
  restore is one backfill (`POST /process/hls`) — see [`RESTORE.md`](RESTORE.md#what-the-backup-does-not-contain).
  Object versioning **does not exist**, the second bucket is the only protection — see [`RESTORE.md`](RESTORE.md).
- **Thumbnail keys (`thumb.*`, `internal/config`):** `engine` (`go` **default** / `vips`;
  an unknown value → `ErrInvalidThumbEngine` at startup) — `vips` switches JPEG/PNG/WebP thumbnails to a
  shell-out to `vipsthumbnail` (faster/leaner on large images, **still no CGO**),
  pure-Go stays the default and the per-photo fallback; `vips_binary` (the executable on PATH, default
  `vipsthumbnail`, `vips` only); `concurrency` (max sizes JPEG-encoded and published in parallel per photo —
  the resampling itself runs one size at a time since SEC-018,
  `0`=GOMAXPROCS — lower it on a memory-constrained host); `max_pixels` (`int64`, **default
  `200000000`** = 200 MP) — the decode-pipeline cap: a source whose `width×height` exceeds it is
  rejected (`imgconvert.ErrImageTooLarge`) before its RGBA bitmap is allocated, so a decompression
  bomb or an enormous panorama fails its thumbnail/pHash job instead of OOMing a worker on the shared
  box (a 30000×30000 image is ~3.6 GB; 200 MP is ~800 MB peak at ~4 bytes/pixel). The same cap guards
  thumbnail generation, the ingest-time perceptual-hash decode **and** the face detector's upright rotation
  (`facejob` rasterizes an original that still carries an EXIF orientation in order to turn it before sending it
  to the sidecar, which reads no EXIF); `0`/negative disables it. `decode_budget_mb` (`int64`, **default
  `1536`**) — the **process-wide memory budget** of in-process image decodes (`imgconvert.DecodeBudget`, one
  instance per process): the upload pipeline's two decodes, every pure-Go thumbnail render (bitmap + the
  resize cascade's working memory), the pHash re-decode, the face job's rotation, the edited download and the
  profile-picture upload each reserve `width×height×bytes-per-pixel` (**bit depth counts**: a 16-bit PNG is twice
  an 8-bit one) plus their working copies before allocating, **wait** while the budget is spent, and are
  **refused** (`ErrImageTooLarge`, degrading exactly like an image over `max_pixels`: no thumbnail/pHash,
  a warning, the photo still catalogued) when they alone exceed it. It is what stops N concurrent uploads
  from each taking a `max_pixels`' worth of RAM (SEC-018 in `docs/SECURITY_AUDIT.md`, measurements in
  `docs/PERF.md` §2). It bounds *live* bytes; resident memory still runs above it by Go's GC slack (set
  `GOMEMLIMIT` to tighten). `0`/negative disables it.
  `KUKATKO_THUMB_ENGINE`/`_VIPS_BINARY`/`_CONCURRENCY`/`_MAX_PIXELS`/`_DECODE_BUDGET_MB`. `serve` logs the active engine +
  warns when `vips` is missing on PATH. See `docs/PERF.md`.
- **Video keys (`video.*`, `internal/config`):** `transcode` (bool, **default false**) — enables
  on-the-fly transcode of non-web-friendly codecs (HEVC/H.265 …) to H.264/MP4 via ffmpeg for playback
  in the browser. Off = video is streamed as-is (with HTTP Range) and the client offers a download when the
  browser cannot decode it. Transcode is CPU-heavy, runs on every playback (not cached), and a
  transcoded stream cannot be seeked precisely — hence opt-in. `KUKATKO_VIDEO_TRANSCODE`.
- **HLS keys (`video.hls.*`, `internal/config` + `internal/hlsjob`):** the **pre-generated** streaming
  renditions, produced once in the background by the `hls_transcode` job and served from the object store —
  the opposite trade-off to `video.transcode` above, which re-encodes on every playback and caches nothing.
  `enabled` (bool, **default false**) is the master switch: an encode is the most expensive thing Kukátko
  does, so an instance that does not stream its videos should not spend the CPU. With it **on**, every
  subsequently uploaded video enqueues its encode (the enqueue lives in the upload pipeline next to
  `image_embed`/`face_detect`); videos already in the library are **not** encoded retroactively — the
  maintainer-only **`POST /api/v1/p/<slug>/process/hls`** is what catches them up, enqueueing an `hls_transcode` job
  for every non-archived video with no `photo_hls_renditions` row (and, with **`?all=true`**, for every
  non-archived video, which is how a newly enabled quality level or a changed `segment_seconds` reaches the
  library — the job replaces a rendition rather than adding to it). It only enqueues: with the one-slot pool
  below, a library of videos is hours of background work. The same encode is also a per-photo processing step
  (`hls_transcode` in the photo detail's `processing` block), so its state is visible next to the thumbnails
  and the embedding, and a maintainer can re-run it for one video with
  `POST /api/v1/p/<slug>/photos/{uid}/process/hls_transcode`. With the switch off both refuse — `/process/hls` with
  **503**, the per-photo step with **409** — rather than queueing work no handler would claim. With it
  **off** no handler is registered and nothing is enqueued — a job of a type nothing can claim would wait in
  the queue forever — and the renditions already in the store are left exactly as they are.
  `segment_seconds` (**default 6**) is the length a media segment is cut to *and* the interval keyframes are
  forced onto (they must agree, or a segment does not start on a keyframe); shorter starts playback sooner and
  costs more objects, `≤ 0` means 6. `renditions` (**default `[1080p]`**) names the quality levels, best
  first; each must be one the encoder knows (`internal/hls`) — an **unknown name fails startup** rather than
  quietly producing a library with a missing quality level. The job gets its **own one-slot worker pool**
  (`internal/worker`, like `mail_send`) so a batch of uploaded videos serialises instead of taking the whole
  machine, and its ffmpeg deadline **scales with the clip** (5 min + 10× its length; 4 h when the length is
  unknown) rather than being a flat constant. The temp directory it encodes into is `storage.temp_path`, which
  must have room for a whole rendition. Objects land under `hls/<file_hash>/<rendition>/` and the row goes to
  `photo_hls_renditions`; both are removed with the photo (purge cascades the row, the trash sweep the prefix).
  `enabled` is also reported to every signed-in client as **`video_streaming`** on `GET /api/v1/p/<slug>/capabilities`,
  alongside `semantic_search` and `passkeys` — a static deployment fact, read once by the frontend's
  `CapabilitiesProvider`. It is what lets a grid tile mark a clip whose rendition has not been produced yet
  (the listing's `hls:false`) without lying on an instance that will never produce one: with the switch off,
  no video ever gains a rendition, so nothing is marked.
  `KUKATKO_VIDEO_HLS_ENABLED`/`_SEGMENT_SECONDS`.
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
  behind a backlog of thumbnails; **`push_send` gets a two-slot pool of its own** for that second reason (a
  notification is news; each send is one small request to a browser vendor's push service);
  **`hls_transcode` gets one for the opposite reason** — it is the job that
would otherwise take the whole machine, so a batch of uploaded videos serialises instead of starving every
other type; **`storage_rekey` gets a one-slot pool too** — one project's storage move at a time, at its own
pace (`storage.rekey.per_second`), never queued behind thumbnails; values ≤ 0 are ignored and a type
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
  heavy endpoints. Sections `upload`/`bulk`/`comment`/`tiles`/`upload_link`/`upload_link_per_link`/`invite`,
  each `{rate_per_sec, burst}`; defaults 5/30, 2/10, 0.5/10, 50/200, 2/60, 10/300, 2/60; `rate_per_sec ≤ 0` disables the rule (middleware no-op). Env e.g.
  `KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC`. **`comment` (POST `/photos/{uid}/comments`) is keyed by the
  authenticated user**, not by IP (`Limiter.KeyedMiddleware`, mounted *inside* the auth guard so the
  principal is on the context): a household shares one address, and throttling everyone's conversation
  because one person is chatty would be wrong. The same `comment` settings, each in a bucket of its own and
  keyed the same way, also throttle a task's thread and `POST /push/subscriptions` (`internal/notificationapi`).
  Login has its own limiter (`auth.login_rate_*`), the geocode
  proxy too (`maps.*`). **`upload_link`** (default 2/60) throttles the public upload-link routes
  (`GET /u/{code}` and its upload) per client IP, ahead of the code lookup; **`upload_link_per_link`**
  (default 10/300) throttles the uploads through **one link**, keyed by the link whoever sends them — the
  brake on a link that leaked to somebody with many addresses. Both count **requests**, not files: the
  page sends one file per request, a script up to 50 (the upload's per-request cap); the files are bounded
  by `upload_links.max_uploads_per_link`. **`invite`** (default 2/60, env e.g.
  `KUKATKO_RATELIMIT_INVITE_BURST`) throttles the public invitation routes (`GET /invite/{code}` and
  `POST /invite/{code}/accept`, `internal/inviteapi`) per client IP, ahead of the code lookup — the brake
  on guessing invite codes. An invite's URL is built from `mail.base_url` (+ `/invite/<code>`).
- **Upload-link keys (`upload_links.*`, `internal/uploadlinkapi`):** `max_file_size_mb` (default
  **1024**; a cap of its own on one file sent through a link, on top of `upload.max_file_size_mb` and the
  project's `max_file_bytes` — the smallest wins, see *Upload limits* below; 0 = no cap of its own),
  `max_uploads_per_link` (default **2000** files over a link's life,
  new photos and duplicates alike, taken per file in the database together with the link's liveness, so
  concurrent requests cannot overshoot it; 0 = no cap), `default_days` (default **30**, the validity the create
  form offers) and `max_days` (default **365**, the longest validity a link may be created or extended
  with). Env e.g. `KUKATKO_UPLOAD_LINKS_MAX_UPLOADS_PER_LINK`. The anonymous uploader's session cookie
  follows `web.secure_cookies` like the sign-in cookie.
- **Featured-album key (`albums.featured.default_days`, `internal/organizeapi`):** default **61** (two
  months) — how long an album stays on the homepage's "Doporučujeme" strip when the curator picks no end
  (`PUT /albums/{uid}/featured` without `until`), and the date the feature dialog pre-fills. A feature
  always ends: the furthest end the API accepts is **366 days** ahead (fixed, not configurable), and a
  `default_days` outside 1…366 falls back to 61. Env `KUKATKO_ALBUMS_FEATURED_DEFAULT_DAYS`.
- **Login keys (`auth.login_rate_limit`, `auth.login_rate_window`):** default **10 failed attempts per
  (username, client IP) within 15m**, then 429; a successful login clears the count. Every attempt is
  charged to a **second, IP-independent per-username budget** as well — `login_rate_limit × 3` over the same
  window (30/15m by default), not separately configurable. That one is what an attacker cannot walk away
  from by changing address, and it is deliberately the looser of the two: somebody mistyping their own
  password from one machine hits the per-IP limit first and never meets it, while somebody aiming at
  another person's account to lock them out has to spend three times as much to do it. Both are in-memory
  and per process, so a restart clears them. `KUKATKO_AUTH_LOGIN_RATE_LIMIT`/`_LOGIN_RATE_WINDOW`.
- **Passkey keys (`auth.passkey.*`, `internal/auth`):** the WebAuthn relying party this instance is.
  `rp_id` (the registrable domain a credential is bound to, **without scheme or port**),
  `rp_display_name` (what an authenticator shows while it asks; default `Kukátko`) and `origins` (the
  page origins a ceremony may run from). Env: `KUKATKO_AUTH_PASSKEY_RP_ID`/`_RP_DISPLAY_NAME`/
  `_ORIGINS` (comma-separated).
  **All three have a working default derived from `mail.base_url`**, which the operator has already
  declared as this instance's public address: with `mail.base_url: https://kukatko.example.cz` and
  nothing else set, the origin is `https://kukatko.example.cz` and the relying-party ID is
  `kukatko.example.cz`. Set `origins` only when the same instance is genuinely reached at a second
  address (a local dev server, `http://localhost:5173`); an entry that is not an absolute http(s) URL
  with a host fails startup with `ErrInvalidPasskeyOrigin`. An explicit `rp_id` wins over the
  derivation; an `origins` list without `rp_id` takes the host of its first entry.
  **With neither the keys nor `mail.base_url` set, passkeys are simply off:** the six
  `/auth/passkeys/*` endpoints answer `501 passkeys are not available on this instance`, and
  `GET /capabilities` reports `passkeys: false` so the interface offers nothing. That is deliberate
  rather than a guessed default — a credential is bound to the relying-party ID **forever**, so an
  instance that minted keys against a guessed domain would stop recognising every one of them the
  moment the guess changed. For the same reason, **changing `rp_id` after anybody has registered a
  key invalidates every registered key**; there is no migration, they have to be added again. The
  password is never taken away, so nobody is locked out by that — which is also why deleting the
  last passkey is allowed.
  A ceremony is a two-request exchange: the begin endpoint mints a challenge, holds it **in memory**
  for 5 minutes and names it in an HttpOnly `kukatko_passkey_ceremony` cookie, and the finish
  endpoint spends it exactly once. Ceremonies therefore do not survive a restart (cost: one retry of
  a sign-in nobody had finished) and are capped at 4096 in flight. The two public halves are
  rate-limited on the login budget above, on two separate per-address keys, so opening ceremonies
  cannot spend the allowance that guards credential verification.
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
  photos, so a re-run is idempotent. With `stacks.enabled: false` it responds 503. The run is **audited**
  as one `stacks.detect` entry carrying the created count and the new stack uids, written in the same
  transaction that forms them — as are the four manual stack operations (`docs/API.md`).
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
  admin-only `POST /process/sidecars`. **The same switch also governs the library's family tree export**
  (`families.yaml`, `internal/familyexport`): it is the same promise — the meaning of the library survives the
  database — and a second key would let an instance end up half-covered, which is the state hardest to notice
  and worst to discover. When `false` no `family_export` job is enqueued or registered either, and the file
  already in storage stays as it is; write it by hand with `kukatko sidecar families`.
- **MCP keys (`mcp.*`, `internal/config` + `internal/mcpapi`):** the **MCP server** — the library exposed
  to an AI agent (Model Context Protocol) at `POST /api/v1/p/<slug>/mcp`, so it can search, read, and organize within it
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
  **The token for the agent** is minted by the user **for themselves** — a maintainer creates the account
  (`POST /api/v1/admin/users`, no membership) and makes it an `editor` of the project, that user logs in (`POST /api/v1/auth/login`) and mints a token
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
  to faces that has run out of faces says `no_people`, not `no_people_no_labels`, and a player who
  switches the toggle to a kind at zero is told it is **switched off** (`source_off`) rather than
  that the library has none of it. The default
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
- **Push keys (`push.*`, `internal/push`):** Web Push notifications to the browsers people allowed to show
  them (standard Web Push + VAPID, no FCM/Firebase). `enabled` (bool, **default false**) is the master switch,
  exactly like `mail.enabled`: with push off the **no-op sender** is wired, nothing is ever encrypted or
  dialled and **no key is demanded**. `vapid.public_key` and `vapid.private_key` (no default) are the
  instance's VAPID pair, minted with `kukatko push generate-keys` — unpadded base64url of the uncompressed
  P-256 point and of the private scalar; the public key is public (the frontend subscribes with it), the
  **private key is a secret — set it only via `KUKATKO_PUSH_VAPID_PRIVATE_KEY`**. `vapid.subject` (no
  default) is how a push service reaches the operator, a `mailto:` or an `https:` URL (RFC 8292 requires
  one). **Validation at startup:** an *enabled* push section with any of the three empty fails with
  `ErrIncompletePushConfig` naming **every** missing key (names only); keys that do not decode, are not
  P-256, or are **not a matching pair**, and a subject that is neither `mailto:` nor `https:`, fail with
  `ErrInvalidPushConfig` — a sender that looks configured while every notification is lost is worse than
  none. A *disabled* section is never checked. **Never rotate the pair casually:** every stored subscription
  was made for the public key. Delivery always goes **through the job queue** (`push_send`, one job per
  subscribed device, `internal/pushjob`); unlike `mail_send` its handler is registered **even with push
  off**, so a job left over from when push was on completes unsent instead of waiting for a claimant, and
  nothing new is enqueued while it is off. Env: `KUKATKO_PUSH_ENABLED`, `KUKATKO_PUSH_VAPID_PUBLIC_KEY`,
  `KUKATKO_PUSH_VAPID_PRIVATE_KEY`, `KUKATKO_PUSH_VAPID_SUBJECT`. (`push.tags.window` used to live here;
  it is now `notifications.tags.window`, below, and the old name is still read as a deprecated alias.)
- **Notification keys (`notifications.*`, `internal/config` + the producers around
  `internal/notification`):** what the notification kinds share across their channels, as opposed to one
  channel's transport (`mail.*`, `push.*`). `tags.window` (duration, **default `1h`**) is how long a
  **tagging window** stays open: the first time somebody's linked person is tagged, a window opens, every
  photo they are tagged on inside it is counted, and when it closes the account gets **one "you were
  tagged" notification per album** the photos are in (hand-made albums only; a private album only for a
  curator or above; at most **5** albums per window — `tagnotifyjob.MaxAlbumGroups`, a constant, not a
  key: it is a sanity cap on the fan-out, beyond which the remaining albums fold into the "other photos"
  notification) plus one for the photos outside those albums, and **one e-mail** listing them all (the
  `tag_notify` job, `internal/tagnotifyjob`) — never one per photo. It must be positive, checked whether
  any channel is on or off (`ErrInvalidTagsWindow`); nothing is recorded for an account no channel would
  reach. **Deprecated alias:** `push.tags.window` (`KUKATKO_PUSH_TAGS_WINDOW`), the key's name while the
  window was push-only, is still read when `notifications.tags.window` is absent — from the file or the
  environment — so an existing deployment keeps its setting; when both are set the new name wins. Neither
  name has a viper default (that is what lets "set" be told from "defaulted"); the default is applied in
  `Load`. Env: `KUKATKO_NOTIFICATIONS_TAGS_WINDOW`.
- **Tasks digest keys (`tasks.digest.*`, `internal/taskdigestjob`):** the one message the task queue sends
  outside the app — once a day, every person is e-mailed the open tasks whose move is theirs (the listing's
  „Na mně"), up to 20 of them with a link each and „…a dalších M" for the rest, plus a link to
  `/tasks?waiting=1`. `enabled` (bool, **default false**) switches it on; it **rides on `mail.*`** — with
  `mail.enabled` false nothing is scheduled whatever this says, and the links are built on `mail.base_url`.
  `hour` (**default 7**) is the **UTC** hour of the day (0–23; anything else → `ErrInvalidTaskDigestHour` at
  startup, checked even while disabled) at which the `task_digest` job is enqueued; the worker then sends
  **one mail per person and only when something changed** since that person's previous digest
  in that project (`project_members.task_digest_at`, stamped after the mail is scheduled) — a queue that has not moved sends nothing,
  and a `.invalid` placeholder address is skipped, never dialled. A server that was down at the hour sends
  nothing until the next one. Env: `KUKATKO_TASKS_DIGEST_ENABLED`, `KUKATKO_TASKS_DIGEST_HOUR`.

- **Notification channels and retention (no keys of their own):** every notification kind is delivered
  through up to three channels — **in-app** (the inbox behind the navbar's bell; needs no switch), **push**
  (needs `push.enabled`) and **e-mail** (needs `mail.enabled`; links built on `mail.base_url`) — chosen
  per account and kind in *My account → Notifications*. **Every channel of every kind is off until the
  account turns it on** (since 2026-10-11; before, the tagged and registration pushes defaulted on —
  accounts that saved an explicit choice keep it). One delivery path, `internal/notifydeliver`, decides
  for all producers. The records are purged by the **`notification_purge`** job (`internal/notifypurgejob`):
  enqueued at startup and every 24 h, it deletes read notifications older than **30 days** and unread
  ones older than **90 days** (fixed retention, not configurable); at most one is ever queued
  (migration 0096). The job shows in *System → Jobs* like every other.

### Upload limits — how the three per-file caps and the project quota interact

Three caps can bound **one file**, and the **smallest non-zero** of them wins (`ingest.Service.fileCap`):

| Cap | Where it is set | Applies to |
| --- | --- | --- |
| `projects.max_file_bytes` | the system administrator, *System → Projects* (`PATCH /projects/{uid}`, `kukatko ctl projects …`) | every upload into that project: `POST /upload`, `POST /u/{code}/upload`, `kukatko import dir` |
| `upload.max_file_size_mb` | instance config (0 = none) | every upload into every project |
| `upload_links.max_file_size_mb` | instance config (default 1024, 0 = none) | only files sent through an upload link |

So a project capped at 50 MB on an instance with `upload.max_file_size_mb: 2048` takes nothing over 50 MB,
and a link into it takes nothing over 50 MB either, although the link's own cap is 1 GB. NULL / 0 everywhere
= no cap at all. A refused file answers **413** `file_too_large` with `limit_bytes` = the cap that won. It is
refused **before its body is read** when it declares its size (the web client sends a `size` field before
every file; `import dir` declares the size on disk) and otherwise **while it streams** — the copy stops one
byte past the cap, so an oversized upload never fills the temp directory.

The **quota** `projects.max_total_bytes` bounds the sum of the project's originals (`sum(photos.file_size)`,
archived photos included — their originals stay stored until a purge; derived files — thumbnails, HLS,
sidecars — do not count). Before an original is stored the project's usage is read, and a file that would
take it past the quota answers **413** `quota_exceeded` (`limit_bytes`, `used_bytes`; the public link page
gets the code alone). Per file: in a multi-file request the files that fit still go in. A byte-identical
re-upload stores nothing and still answers `duplicate` — unless the quota is already used up, when every
request is refused before its body is read (it cannot know the file is a duplicate). Lowering a quota below
the current usage deletes nothing; the project simply takes no more files until space is freed (archive +
purge) or the quota is raised. There is **no lock**: two uploads checked at the same moment may both pass and
overshoot the quota by up to one file each — accepted, the quota is a guard against unbounded growth, not a
ledger. The usage read is an index-only scan of `idx_photos_project_file_size` (migration 0111, see
`docs/PERF.md`). The maintenance orphan rescue re-catalogues bytes already stored and is not held to the quota.

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

`GET /metrics` (namespace `kukatko`, mounted outside `/api/v1` when `metrics.enabled`) exposes six
groups, plus `kukatko_build_info{version,commit} 1` — the constant-1 gauge naming the running build
(the same `internal/version` values `/healthz` reports), for annotating a deploy or joining onto any
series with `* on() group_left(version) kukatko_build_info`. It is **unauthenticated** — restrict it at the network layer — so it deliberately carries only
instance-wide aggregates: nothing per-user, and no name of a photo, album, label or person ever
becomes a label value.

- **Request and worker instrumentation** (event-driven, recorded as things happen):
  `kukatko_http_requests_total{method,route,status}` + `_request_duration_seconds` + `_inflight_requests`
  (the route label is the chi route *pattern*, never a raw URL), `kukatko_jobs_started_total{type}` /
  `_finished_total{type,outcome}` / `_execution_duration_seconds{type,outcome}` (outcome `success`,
  `error` — the attempt failed and **will be retried** — `deferred` — requeued without a burned
  attempt, the box was offline — or `terminal`: the handler declared the job permanently failed and it
  was parked in `failed` with no retry), `kukatko_jobs_stale_locks_recovered_total` (running jobs
  requeued because their worker stopped heartbeating — a crash or an OOM kill; **any increase is an
  incident**, alert on `increase(...[1h]) > 0`),
  `kukatko_embedding_request_duration_seconds{operation,outcome}` +
  `kukatko_embedding_service_up{target}`, `kukatko_thumbnail_generation_duration_seconds` and
  `kukatko_geocode_credits_spent_total`.
  `service_up` is **reachability**, not the outcome of the last embed call: `target="text"` is the
  host that answers `/embed/text` (`embedding.text_url`, else the box), refreshed **every minute** by
  the semantic-search capability probe; `target="box"` is `embedding.url`, refreshed by the auto-wake
  loop's probe (which only probes while `embedding.wake.enabled` and enough embedding work waits) and
  by every image/face/OCR call. Every call moves the gauge of the host it went to — a transport-level
  "offline" marks it 0, any answer (even an error response) marks it 1. The gauge is **not**
  pre-set: an unprobed target is unknown, not down, and a 0 at startup would fire an outage alert on
  every restart.
  `kukatko_import_run_photos{source,outcome}` — the tally of a run in progress — was removed with the
  migration importers that checkpointed it; the last-run gauges below are unaffected.
- **The streaming encode** (event-driven, recorded where ffmpeg runs):
  `kukatko_video_encode_duration_seconds{rendition,outcome}` (a histogram whose buckets start at 5 s and
  triple to just over three hours, because an encode is nothing like an HTTP request),
  `kukatko_video_encode_output_bytes_total{rendition,outcome}` — how many bytes of segments the encoder
  published; a failed encode's were removed again, which is what the outcome label is for — and
  `kukatko_video_encode_source_seconds_total`, the seconds of source footage that have been through the
  encoder, counted **once per video** and not once per rendition. The observation is per **rendition** on
  purpose: `hls_transcode` already appears in the generic job series above (started, finished, duration,
  queue depth) and **needs no duplicates of them**, but there a job is one row whether it chewed through a
  six-second clip or a forty-minute one, and the qualities it produces cost wildly different amounts. The
  footage is a counter rather than a ready-made ratio, because a ratio can be neither aggregated across
  instances nor rated over a window. Three queries answer what this exists for:

  ```promql
  # How many encodes are running right now (the job has a one-slot pool, so this is 0 or 1)?
  kukatko_jobs_queue_depth_by_type_state{type="hls_transcode",state="running"}

  # How long does encoding one rendition take (median and p95 over the last 6 h)?
  histogram_quantile(0.5,  sum by (le, rendition) (rate(kukatko_video_encode_duration_seconds_bucket[6h])))
  histogram_quantile(0.95, sum by (le, rendition) (rate(kukatko_video_encode_duration_seconds_bucket[6h])))

  # What does a minute of video cost to encode, across every rendition of the plan (seconds of
  # processing per minute of footage)?
  60 * sum(rate(kukatko_video_encode_duration_seconds_sum{outcome="success"}[6h]))
     / rate(kukatko_video_encode_source_seconds_total[6h])
  ```
- **Infrastructure, sampled at scrape time:** `kukatko_db_pool_*` (live pgx pool stats),
  `kukatko_jobs_queue_depth{state}` / `_by_type{type}` / `_by_type_state{type,state}` (all three folded
  from **one** `GROUP BY type, state` — the two one-dimensional families are sums over the third, so
  they can never disagree) and `kukatko_geocode_credits_remaining` / `_limit`.
- **Library content, per project**, memoised for `metrics.library_ttl` (default 1 m): every gauge below
  carries a **`project` label = the project's slug** (never its name — `/metrics` is unauthenticated), one
  series set per **active** project, counted by one grouped SQL statement (`internal/system`'s
  `CountLibraryByProject`: the statistics page's own predicates grouped by project, so the gauges summed
  per label agree with that project's `GET /p/{project}/stats`). **A dashboard or alert that wants the instance total sums
  over the label**: `sum without (project) (kukatko_library_photos)`, `sum(kukatko_library_faces)`; a bare
  selector now returns one series per project:
  `kukatko_library_photos{media_type="image|video|live",project}` (the total is `sum()` over the labels),
  `_photos_archived` (the trash), `_photos_processed{stage="embedding|faces|places"}` and
  `_photos_pending{stage=…}` (the processing coverage and its gap — note the `faces` gap also counts
  photos that genuinely contain no face, which the counts cannot tell apart), `_embeddings`, `_faces`,
  `_markers{state="assigned|unassigned"}`, `_subjects{type}`, `_albums{type}`, `_labels`,
  `_videos_without_streaming` (the encoding backlog — browsable videos with no rendition at all, the same
  number the System page's video card shows, out of the same memoised aggregation; with `video.hls.enabled`
  off it is every video and is the instance working as configured, so alert on it only where streaming is
  on), plus
  `kukatko_library_collect_errors_total` (no project label — a failed aggregation exports **no** library
  gauges of any project for that scrape — a gap, not a stale number — and bumps this counter, so alert on
  it). The pool, queue, import and platform families (including the platform's `kukatko_library_*`
  encode-state, backlog and bytes gauges below) stay **instance-wide**, without a project label.
  RAW originals are deliberately **not** a media type here: RAW-ness is a per-file property judged by
  extension (`internal/stacks`), not a column, so counting it would mean an unindexed scan on every
  refresh; use `media_type`, which is what the catalogue actually models.
- **Import history** (same memoisation): `kukatko_import_last_run_status{source,status}` — 1 for the
  status the last run of that source is in and **0 for every other known status**, so a transition is
  visible instead of a series silently vanishing — plus
  `kukatko_import_last_run_start_timestamp_seconds{source}` and `_finish_timestamp_seconds{source}`
  (absent while a run is still going). Both are Unix seconds: express the age as `time() - <gauge>`
  rather than exporting a pre-computed one.
- **The platform underneath**, read on every scrape from `system.Service.Platform` — the System page's own
  caches (the dashboard aggregation and the disk measurement, each memoised for 30 s and shared with the
  page) and two in-memory states; the collector adds **no cache of its own** and runs no SQL the caches do
  not already memoise:
  - **Backup:** `kukatko_backup_configured` (always present, 0 without a destination), `_running`,
    `_last_run_success` (1/0 for the most recent *finished* run), `_last_run_finish_timestamp_seconds`,
    `_last_success_timestamp_seconds` (the last *good* run — it stays put through a failure) and
    `_last_run_originals{result="uploaded|skipped"}`. The backup state lives in memory: after a restart
    every outcome series is **absent** until the next run finishes, so write alerts that tolerate it.
  - **Disk:** `kukatko_disk_tree_bytes{tree="originals|cache"}` and `kukatko_disk_filesystem_free_bytes` /
    `_size_bytes` of the filesystem holding `storage.originals_path` (on an object-store instance the
    originals tree is empty and the filesystem is the one the path would be on).
  - **Map provider:** `kukatko_maps_configured` (0 without a mapy.com key), `kukatko_maps_up` (0 after a
    rejected key, a 429, an unreachable upstream or another failure) and
    `kukatko_maps_last_check_timestamp_seconds` — both absent until a call has been observed, because the
    tracker records what the proxy saw and never probes (an idle map is unknown, not down).
  - **Streaming encode states:** `kukatko_library_videos_by_encode_state{state="streamable|queued|running|
    failed|not_scheduled"}` (disjoint, summing to every browsable video; the four non-streamable states sum
    to `_videos_without_streaming`), `kukatko_library_video_encode_oldest_queued_timestamp_seconds` (absent
    with nothing queued) and `kukatko_library_video_streaming_enabled`. `not_scheduled` is the one to alert
    on: no queue row records a video that was never offered for encoding.
  - **Backlogs and weight:** `kukatko_library_backlog{kind="faces_unassigned|clusters|
    photos_without_taken_at|photos_without_gps|photos_without_ocr|duplicate_markers"}` and
    `kukatko_library_bytes{set="live|trash|derived"}` (live and trash by the catalogue's own file sizes,
    derived measured in the local cache — absent when the disk measurement failed). The near-duplicate
    scan is deliberately **not** exported: its background refresh is too expensive to be kept warm by a
    scraper.
  - `kukatko_platform_collect_errors_total{source="catalogue|disk"}` — a failed source drops only its own
    series for that scrape (the backup and map series are in memory and cannot fail).

  ```promql
  # Did last night's backup run and succeed? (fires on a failure, and on no success for 26 h)
  kukatko_backup_last_run_success == 0
  time() - kukatko_backup_last_success_timestamp_seconds > 26 * 3600
  # Will the box run out of disk within a week?
  predict_linear(kukatko_disk_filesystem_free_bytes[1d], 7 * 86400) < 0
  # Videos nobody will ever encode, where streaming is on
  kukatko_library_videos_by_encode_state{state="not_scheduled"} > 0
    and on() kukatko_library_video_streaming_enabled == 1
  ```

**Histogram buckets differ per family on purpose**, because each is sized to the work it measures:

- `kukatko_jobs_execution_duration_seconds`, `kukatko_embedding_request_duration_seconds` and
  `kukatko_thumbnail_generation_duration_seconds` use exponential buckets from **10 ms doubling to
  ~655 s** (17 finite bounds). The Prometheus defaults stop at 10 s, while `image_embed`/`face_detect`
  against a waking box, `ocr`, `storyboard`, `metadata` and a RAW/HEIC thumbnail routinely run longer;
  with the defaults they all landed in `+Inf` and no quantile above p50 meant anything.
- `kukatko_http_request_duration_seconds` keeps the **defaults** (5 ms … 10 s, where nearly every
  request lands, so existing dashboards keep their boundaries) plus a short tail of **30, 60, 120 and
  300 s** for the streaming routes — the recognition sweep's NDJSON, a video download.
- `kukatko_video_encode_duration_seconds` starts at 5 s and triples to ~3 h (above).

**Series exist from process start.** A labelled family exports nothing until its first observation,
which made "nothing failed" indistinguishable from "the instrumentation is broken". So at startup
every label combination the instance can produce is created at zero: the job families for **every job
type the worker has a handler for** × every outcome, the embeddings call histogram for every
operation (`image`, `text`, `face`, `ocr`) × `success`/`error`, and — only with `video.hls.enabled` —
the two per-rendition encode families for every configured rendition × outcome. The unlabelled
counters (`stale_locks_recovered_total`, `geocode_credits_spent_total`, …) are at zero anyway.

The gauges carry no `_total` suffix on purpose (it is reserved for counters, so `rate()` over a name
stays meaningful). A queue-query, library-aggregation or platform-source failure drops only its own
families and never fails the whole scrape.

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

