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
