# Restore / disaster recovery runbook

This is the counterpart to the [backup](ARCHITECTURE.md#14-konfigurace-build-a-provoz-s15)
subsystem: how to rebuild a Kukátko instance from an S3-compatible backup. A backup writes a
`pg_dump` (custom/compressed format) to `db/kukatko-<timestamp>.dump` in the bucket and
incrementally syncs the on-disk originals. Restore reverses both, then verifies integrity.

> **The database restore is destructive.** `kukatko restore db` overwrites the entire target
> database. Run it only against an empty or throw-away database during recovery, and only with the
> explicit `--yes` flag. Do **not** run it against a live, in-use database.

All restore commands read the same `backup.s3.*` configuration as backup (so a machine that can
back up can also restore), plus `database.url` (the **target** to restore into) and
`storage.originals_path` (where originals are written). Secrets travel via environment variables and
are never logged or placed on the process argument list.

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

### 5. Restore the originals

```bash
kukatko restore originals
# originals restore complete: downloaded=48213 skipped=0
```

This downloads every original in the bucket that is not already on disk at the same key and size,
writing each atomically (temp file under `.tmp/` then rename). **It is resumable**: if it is
interrupted (network drop, Ctrl-C), just run it again — completed files are skipped and only the
remainder is fetched. Database dumps under `db/` are never downloaded as originals.

### 6. Verify integrity

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

### 7. Start the service

```bash
sudo systemctl start kukatko        # deb install
# or: kukatko serve
```

Thumbnails and other derived cache are **regenerated lazily** on first access, so the cache
directory does not need to be restored. Embeddings and detected faces are part of the database dump
and are restored with it.

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
