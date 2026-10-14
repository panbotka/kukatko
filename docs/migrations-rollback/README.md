# Rollback scripts for the multi-tenancy migrations

Hand-run **down** scripts for the migrations a plain `DROP COLUMN`/`DROP TABLE` cannot
reverse. **The application never applies them** — they live outside
`internal/database/migrations/` on purpose. They exist so a failed deploy can go back to the
previous release (0.31.0, schema 0099) without restoring a backup and losing the writes made
since.

Run them newest first, each in one transaction, then start the previous release:

```sh
for f in 0104 0103 0102 0101 0100; do
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -1 -f docs/migrations-rollback/${f}_down.sql || break
done
```

Each script removes its own `schema_migrations` row, so the previous binary sees exactly the
schema it knows. `0102_down.sql` refuses to run once a project other than the bootstrap
project `v` exists (dropping `project_id` would silently merge two libraries). Restoring
`users.role` takes each account's role from its bootstrap-project membership, so a role an
admin changed after the deploy survives the rollback.

Proven on a copy of production data: up → down → inventory equal to the pre-deploy inventory,
zero differences. See [`docs/MIGRATION_SAFETY.md`](../MIGRATION_SAFETY.md).

**`0110_down.sql`** (later than the chain above) puts back the five per-library `users` columns
migration 0110 dropped — `approved_at`, `subject_uid`, `last_seen_at`, `visit_reference_at`,
`task_digest_at` — filled from the memberships the way the releases that still read them kept them
in step: approved once any membership is (a maintainer, or an account in no project, always), the
rest from the bootstrap-project
membership (and the column comment of `task_digest_at`). An account with **no** membership therefore comes
back approved, even one that was pending; the script header lists them. Run it before 0109 … 0100 when going back to a
release older than 0110.

**`0114_down.sql`** puts back the transitional `DEFAULT 'pj0000000000000000veselice'` (project `v`)
that migration 0114 dropped from every `project_id` column, for a release whose writers may still
insert without naming the project. It leaves NOT NULL and the foreign keys alone. Run it first when
going back to a release older than 0114.

**`0115_down.sql`** drops `storage_rekey_derived_failures` (the storage move's record of photos whose
thumbnails or streaming segments stayed at the root) and the run's `derived_cursor`. It is safe at any
time: an older release neither reads nor retries those rows, and `kukatko maintenance scan` finds the
objects again from a bucket listing. Run it first when going back to a release older than 0115.

**Every file of the chain has a down script** since the final safety gate (2026-10-13). To go back to
0.31.0 (schema 0099):

```sh
for f in 0115 0114 0113 0112 0111 0110 0109 0108 0107 0106 0105 0104 0103 0102 0101 0100; do
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -1 -f docs/migrations-rollback/${f}_down.sql || break
done
```

`0105`–`0108` and `0111`/`0113` only put indexes back to their pre-tenancy keys (`0107_down` cancels queued
`task_digest` jobs, `0113_down` queued/running `storage_rekey` jobs, `0108_down` the queued
`notification_purge` and `0106_down` the queued `family_export` jobs of every project but the bootstrap one —
the older key or release cannot hold them, and a project deleted before the rollback can leave such jobs
behind); `0109_down` drops the invitation links (accepted memberships stay); `0112_down` drops the storage-move
bookkeeping and **refuses once a move has committed a photo** — a moved `file_path` cannot go back to a release
that knows only the root layout. Proven on the production copy: up → all fifteen down → inventory equal to the
pre-deploy inventory, zero differences.
