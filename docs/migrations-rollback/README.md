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
membership. Run it first when going back to a release older than 0110; 0105–0109 have no down
scripts of their own.
