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
  the DB-pool + job-queue-depth collectors, and inserts the request-metrics + access-log middleware via
  `server.WithMiddleware`/`WithMetricsHandler`), `kukatko migrate` (runs pending migrations on their own and exits),
  `kukatko migrate photosorter` (synchronous read-only incremental **data migration from photo-sorter** —
  `psimport`; applies DB migrations, then `Service.Migrate`; needs `import.photosorter.dsn`, otherwise
  `errPSMigrateNotConfigured`; for ops/cron without a running server),
  `kukatko import photoprism` (synchronous read-only incremental import from PhotoPrism — `ppimport`;
  needs `import.photoprism.base_url`, otherwise an error; for ops/cron without a running server;
  a **scoped run** = the library can be migrated in slices: `--album <photoprism-uid>` (an album's photos),
  `--label <slug>` (photos with that label, e.g. `sdh`), `--person <name>` (photos the given
  subject appears in, e.g. `"Aleš Kozák"`), `--year <YYYY>` (photos taken in that year). Flags **combine
  and narrow the run** (the album goes into `s=`, the rest into `q=` as ANDed terms, verified against production:
  `--album X --year 1985`). A scoped run pulls its slice in **full, regardless of photo age**
  (it ignores the watermark) and **transfers each photo complete**: it creates and attaches **all** the albums the
  photo is in, plus **all** its labels (with `source`/`uncertainty` from the source) — including the ones the
  scope did not name, so a photo from three albums imported via `--album` into one ends up in all three
  (this costs 1 extra photo-detail request; a full run does not do this and maps the structure by walking the
  album/label catalog). People seed the face markers of imported photos. The run **does not advance the watermark**, so
  a later full import still sees all photos. An unknown album uid → `ErrAlbumNotFound`, an unknown label
  slug → `ErrLabelNotFound` (verified **before** downloading), a nonsensical year → `ErrInvalidYear`, no
  flag → a full incremental run. It is idempotent — a re-run does not create a second album, label, or membership.
  Used to verify the import against production and to pre-pull part of the library),
  `kukatko import photosorter-feeds` (synchronous **feeds enrichment** — `internal/psfeedsimport`; applies DB
  migrations, then `Service.Import`; needs `import.photosorter.base_url` (and `token`), otherwise
  `errFeedsImportNotConfigured`. This is the **production** photo-sorter migration path: in production
  photo-sorter holds no photos of its own, only vectors/faces keyed by the PhotoPrism UID, so this pages the
  read-only `/api/v1/embeddings` + `/api/v1/faces` feeds (`psat_` bearer token) and attaches photo-sorter's
  **1:1** CLIP embeddings and InsightFace faces — plus the markers and subject assignments the faces feed
  carries — to the already-imported photo whose `photoprism_uid` matches the feed's `photo_uid`, **without any
  GPU recompute**. Idempotent and re-runnable; a feed entry whose photo is not imported yet is **skipped**, not
  an error. The same pass also runs as the background `ps_feeds_import` job triggered from
  `POST /api/v1/import/photosorter-feeds`. The legacy DSN-based `migrate photosorter` path is irrelevant for
  this deployment (photo-sorter has no native photos here)),
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
  `maintenance scan` (read-only integrity report — disk↔DB drift + missing derived data) and
  `maintenance repair` with the flags `--thumbnails`/`--embeddings`/`--faces`/`--phashes`/`--import-orphans`
  (each opt-in; thumbnails/phashes enqueue `thumbnail` jobs drained by a running server's worker,
  embeddings/faces backfill, orphan import synchronously via the upload pipeline; a no-op without any flag;
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
in `/import` and in `GET /import/runs` alongside PhotoPrism and photo-sorter runs.

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

### `kukatko import verify`

Reconciles the import sources against the catalogue and prints whether **the import is complete and nothing
is missing** (`internal/importverify`). Read-only: it opens the DB, applies migrations, then pulls the source
totals — PhotoPrism's photo count, per-type counts (`type:raw`/`type:video`) and each photo's `Files[]`, and
photo-sorter's feeds `GET /api/v1/stats` (`total_photos`/`photos_with_embeddings`/`total_faces`) — and compares
them against Kukátko, listing what is missing: PhotoPrism UIDs not imported, photos missing an original file
(e.g. a dropped RAW sibling), photos missing their photo-sorter embedding/faces, and albums/labels/people not
transferred. The SHA256/SHA1-dedup delta is accounted for separately (`deduplicated`), so the remaining delta
is a real gap. It does **not** record an `import_runs` row (it is a check, not an import). Needs
`import.photoprism.*` configured (and `import.photosorter.*` for the vectors section); exits **nonzero** when
anything is missing, so a script/CI can gate on it. `--json` prints the full report as JSON.

```bash
kukatko import verify            # human-readable reconciliation summary; exit 1 if incomplete
kukatko import verify --json     # the full importverify.Report as JSON
```

The same reconciliation is exposed over HTTP at `GET /api/v1/import/verify` (maintainer-only) and surfaced in
the `/import` admin page's completeness-check section. The individual per-photo/per-file failures a run records
(instead of only logging them) are persisted in `import_failures` and listed at `GET /api/v1/import/failures`.

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
| `ctl photos get <uid>` | detail `GET /photos/{uid}` (+ files, albums, labels) |
| `ctl photos search <query>` | `GET /search?q=…&mode=…` |

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
line says so (`degraded`).

```bash
kukatkoctl photos list --year 2024 --limit 5
kukatkoctl photos list --album alb1a2b3 --sort title -o json | jq '.photos[].uid'
kukatkoctl photos get pht01h2j3
kukatkoctl photos search "západ slunce nad jezerem" --mode semantic
KUKATKO_SERVER=http://localhost:8080 KUKATKO_TOKEN=kkt_… kukatkoctl photos list
```

#### `ctl albums`

Albums and their membership (`internal/organizeapi`). Anyone logged in may list; **create and membership
require `editor`/`admin`**.

| Command | Meaning |
| --- | --- |
| `ctl albums list` | `GET /albums` — a **bare `{"albums":[…]}`, no pagination**, each album with a photo count |
| `ctl albums get <uid>` | `GET /albums/{uid}`; the detail **does not send** `photo_count`, so the column is absent |
| `ctl albums create <title>` | `POST /albums`; `--description`, `--type`, `--order-by`, `--cover`, `--private` |
| `ctl albums add-photos <album-uid> [<photo-uid>…]` | `POST /albums/{uid}/photos` — appends **after** the existing ones |
| `ctl albums remove-photos <album-uid> [<photo-uid>…]` | `DELETE /albums/{uid}/photos`; a non-member = no-op |

`--type` is `album` (default), `folder`, `moment`, `state`, or `month`; only `album` makes sense
manually, the server generates the rest. `add-photos`/`remove-photos` read uids from arguments, or **from
stdin** when there are none (see *Large batches* below), and send them in **one** request.
In a table they print one line (`album alb1a2b3 now holds 12 photos`), `-o json` the whole new order.

#### `ctl labels`

Labels and attaching them to photos (`internal/organizeapi`). Anyone may list; the rest `editor`/`admin`.

| Command | Meaning |
| --- | --- |
| `ctl labels list` | `GET /labels` — a **bare `{"labels":[…]}`**, ordered by priority |
| `ctl labels get <uid>` | `GET /labels/{uid}` |
| `ctl labels create <name>` | `POST /labels`; `--priority` |
| `ctl labels attach <label-uid> <photo-uid>` | `POST /labels/{uid}/photos`; `--source`, `--uncertainty` |
| `ctl labels detach <label-uid> <photo-uid>` | `DELETE /labels/{uid}/photos`; a non-attached one = no-op |

`--source` is `manual` (default), `ai`, or `import`. If omitted it is **not sent** in the body, so the
server fills in its own default.

#### `ctl subjects`

People, animals, and other subjects of the face pipeline (`internal/peopleapi`). **The whole tree is read-only** —
creating and editing subjects belongs in the UI, where the face gallery is visible and a decision can be verified.

| Command | Meaning |
| --- | --- |
| `ctl subjects list` | `GET /subjects` — a **bare `{"subjects":[…]}`**, with a marker count |
| `ctl subjects get <uid>` | `GET /subjects/{uid}` |
| `ctl subjects photos <uid>` | `GET /subjects/{uid}/photos`; `--limit`/`--offset` |

A subject's gallery is the only paginated subject endpoint and returns the **`/photos` envelope**, so it
prints as a photo list. It does not read the catalog filters, so `ctl` does not offer them either.

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

Backups, restore, migrations, maintenance, import, and the job queue are **not offered over the network**. They are destructive or
long-running and belong on the machine where the instance runs — so they remain only as local subcommands
(`kukatko backup`, `restore`, `migrate`, `maintenance`, `import`, …).

## Configuration keys

<!-- BODY CONFIG -->
- **Observability keys:** `log.level` (debug/info/warn/error, default info, invalid → an error at
  startup; `KUKATKO_LOG_LEVEL`) and `metrics.enabled` (bool, default true; disabled → `/metrics` is
  not mounted, the request-metrics middleware is not installed, the access log keeps running; `KUKATKO_METRICS_ENABLED`).
- **Storage keys (`storage.*`, `internal/storage`):** `backend` (`fs` **default** = local disk /
  `r2` = a private Cloudflare R2 bucket; an unknown value → `ErrInvalidStorageBackend` at startup),
  `originals_path` (the originals root, `fs` only), `cache_path` (derived artifacts — thumbnails,
  video posters), `temp_path` (default `/var/lib/kukatko/tmp`; `r2` stages uploads through it
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
  `ErrIncompleteR2Config` validation **at startup**: the `r2` backend requires all keys except
  `url_signing_secret_previous` (the missing ones are listed in the error — names only, never values)
  and a positive `url_ttl`. Neither the secrets nor the access key are ever logged or appear in an error.
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
  both thumbnail generation **and** the ingest-time perceptual-hash decode; `0`/negative disables it.
  `KUKATKO_THUMB_ENGINE`/`_VIPS_BINARY`/`_CONCURRENCY`/`_MAX_PIXELS`. `serve` logs the active engine +
  warns when `vips` is missing on PATH. See `docs/PERF.md`.
- **Video keys (`video.*`, `internal/config`):** `transcode` (bool, **default false**) — enables
  on-the-fly transcode of non-web-friendly codecs (HEVC/H.265 …) to H.264/MP4 via ffmpeg for playback
  in the browser. Off = video is streamed as-is (with HTTP Range) and the client offers a download when the
  browser cannot decode it. Transcode is CPU-heavy, runs on every playback (not cached), and a
  transcoded stream cannot be seeked precisely — hence opt-in. `KUKATKO_VIDEO_TRANSCODE`.
- **Wake-on-LAN keys (`embedding.wake.*`, `internal/wake`):** `enabled` (bool, **default false** —
  the feature is fully inert), `mac` (the box's MAC, **required and parsed during validation** when enabled),
  `broadcast_addr` (the UDP broadcast target, default `255.255.255.255:9`), `interface` (the NIC for the raw
  Ethernet frame; requires CAP_NET_RAW), `min_queue` (the threshold of pending `image_embed`/`face_detect`
  jobs, default 1), `cooldown` (min. spacing between packets, default 5m). `ErrInvalidWake` validation:
  enabled requires a valid MAC + at least one target (`broadcast_addr`/`interface`).
- **Rate-limit keys (`ratelimit.*`, `internal/ratelimit`):** per-client-IP token-bucket limits on
  heavy endpoints. Sections `upload`/`bulk`/`import`/`tiles`, each `{rate_per_sec, burst}`;
  defaults 5/30, 2/10, 1/3, 50/200; `rate_per_sec ≤ 0` disables the rule (middleware no-op). Env e.g.
  `KUKATKO_RATELIMIT_UPLOAD_RATE_PER_SEC`. Login has its own limiter (`auth.login_rate_*`), the geocode
  proxy too (`maps.*`).
- **Maps/geocode keys (`maps.*`, `internal/config`):** `mapy_api_key` (server-side, env
  `MAPY_API_KEY`; empty → the tile/rgeocode proxy 503s, the `places` job is not registered, and `/process/places`
  returns 503), `user_agent` (see below), `base_url` (default `https://api.mapy.com`), and a reverse-geocode
  throttle for the background **`places` job** (which caches a photo's locality): `geocode_rate_per_sec`
  (default 5, ≤ 0 disables) + `geocode_burst` (default 10) — protects the monthly mapy.com credit budget,
  processing slowly is OK. `KUKATKO_MAPS_GEOCODE_RATE_PER_SEC`/`_GEOCODE_BURST`.
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
  `maps.geocode_rate_per_sec` limiter, so a large backfill feeds the geocoder in drips instead of
  swamping it — count on **1 mapy.com credit per estimated photo**. A re-run is idempotent and
  **an estimate deleted by the user never comes back**.
- **Candidates keys (`candidates.*`, `internal/config` + `internal/candidates`):** tunes the search for
  "a person among untagged photos" (`POST /subjects/{uid}/candidates`). `max_distance` (**default
  0.5**) — the default max cosine distance of a candidate from an exemplar when the request does not send it, **and**
  the baseline the vote rule scales against; `search_limit` (**default 1000**) — how many nearest
  unassigned faces the kNN of each exemplar returns before voting (bounds the fan-out per exemplar);
  `min_face_px` (**default 32**) — the minimum face width in **display pixels** for it to be
  reviewable (a tiny face in a crowd cannot be judged; complements the relative floor taken from
  `faces.min_face_size`); `concurrency` (**default 8**) — how many exemplar kNNs run at once, so searching for
  a person with hundreds of photos does not fan out unbounded. A non-positive value for any key falls back to the
  default (for `min_face_px` it disables the absolute floor). Env: `KUKATKO_CANDIDATES_MAX_DISTANCE`,
  `_SEARCH_LIMIT`, `_MIN_FACE_PX`, `_CONCURRENCY`.
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
  `max_distance` (**default 0.30**, the UI shows it as 70 % similarity) — the default max cosine distance
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
  system is unsure about. `band_min` / `band_max` (**default 0.45 / 0.75**) — the **uncertainty band**:
  only a candidate with confidence (= 1 − cosine distance) in `[band_min, band_max)` becomes a question;
  below the band the guess is noise, from `band_max` up it is confirmed in bulk on `/recognition` / via expand.
  An invalid band (outside (0,1), min ≥ max) falls back to the default **pair**. `queue_size` (**default 20**) —
  the default batch size, the UI prefetches; a request may send its own `?limit` (cap 100).
  `cache_ttl` (**default 60s**) — how long a built queue is served from the per-user cache before the
  expensive vector searches run again (answers edit the queue in-place, the session counter is cheap).
  `max_labels` (**default 200**) — a cap on how many labels one rebuild scans. `label_concurrency`
  (**default 2**) — how many label-similarity searches run at once (each already fans out internally; on a
  RAM-constrained box keep it low). The review does not take the face side with its own keys — it runs through
  sweep/candidates and their `sweep.*`/`candidates.*` limits. A non-positive value for any key
  falls back to the default. Env: `KUKATKO_REVIEW_BAND_MIN`, `_BAND_MAX`, `_QUEUE_SIZE`, `_CACHE_TTL`,
  `_MAX_LABELS`, `_LABEL_CONCURRENCY`.

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
  serially; the R2-backend tests additionally want `KUKATKO_TEST_S3_ENDPOINT` — without it they are skipped,
  see `docs/DEVELOPMENT.md`), `check` (the gate = `docs-budget` + `fmt-check` + `lint` +
  `web-typecheck` + `test`; **rewrites nothing**, after a successful run `git status --short` is
  empty), `build` (frontend build + `CGO_ENABLED=0` → `bin/kukatko`), `dev` (smart rebuild + run on
  `:6480` via `scripts/dev.sh`, `DEV_ARGS=--force` for a full rebuild), `clean`, `help`.
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
  via nfpm. `deb/`: `kukatko.service` (systemd, user `kukatko`, `EnvironmentFile`,
  `Restart=always`), `kukatko.env` (dpkg conffile `config|noreplace`),
  `postinstall.sh`/`preremove.sh`/`postremove.sh` (user + `/var/lib/kukatko/{originals,cache}`).
  Apt deps: `libimage-exiftool-perl`, `libheif-examples|libheif-bin`, `dcraw`, `ffmpeg`,
  `postgresql-client`, `ca-certificates`; **no texlive**.

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
     (`/var/lib/kukatko/{originals,cache,tmp}`).
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

