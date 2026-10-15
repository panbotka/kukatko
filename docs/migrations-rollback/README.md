# Rollback scripts for the multi-tenancy migrations

Hand-run **down** scripts for every migration of the multi-tenancy chain (0100 onwards). **The application
never applies them** — they live outside `internal/database/migrations/` on purpose. They exist so a failed
upgrade can go back to the last single-library release (0.31.0, schema 0099) — or to any release in between —
without restoring a backup and losing the writes made since. How they fit into an upgrade:
[`OPERATIONS.md`](../OPERATIONS.md), "Upgrading — migrations, the inventory and rollback".

## Running them

**Stop the new server first** — and keep it stopped: it would re-apply the migrations on its next (re)start.
Then run the scripts **newest first, each in one transaction**, starting at the version actually applied:

```sh
N=$(psql "$DATABASE_URL" -Atc 'SELECT max(version) FROM schema_migrations')
for f in $(seq -f '%04g' "$N" -1 100); do
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -1 -f docs/migrations-rollback/${f}_down.sql || break
done
```

Going back from the newest schema to 0099 runs the whole list: `0120`, `0119`, `0118`, `0117`, `0116`,
`0115`, `0114`, `0113`, `0112`, `0111`, `0110`, `0109`, `0108`, `0107`, `0106`, `0105`, `0104`, `0103`,
`0102`, `0101`, `0100`. To go back to an intermediate release instead, replace the loop's lower bound `100` by that release's
newest migration + 1.

The down script of a file that was never applied fails (e.g. `0115_down` on a 0114 database: `relation
"storage_rekey_runs" does not exist`) and `|| break` stops the loop there — hence the start at
`max(version)`. Do not paper over that with `IF EXISTS` everywhere: `0102_down`'s and `0112_down`'s guards
are meant to stop the chain.

Each script removes its own `schema_migrations` row, so the previous binary sees exactly the schema it knows.
Afterwards `SELECT max(version) FROM schema_migrations` must print the target (99 for 0.31.0); start the
previous release and check a sign-in.

**The chain refuses** in two places, and then the way back is the pre-upgrade dump, not the scripts:

- **`0102_down.sql`** refuses once a project other than the bootstrap project `v` exists (dropping
  `project_id` would silently merge two libraries).
- **`0112_down.sql`** refuses once a storage move has committed a photo — a moved `file_path` cannot go back
  to a release that knows only the root layout.

Proven on a copy of a real library: up → the whole down chain → an inventory equal to the pre-upgrade one with
no expectations (zero differences), also after the new server had run and created sessions, which the old
release then accepts. See [`docs/MIGRATION_SAFETY.md`](../MIGRATION_SAFETY.md).

## What each script does

- **`0100`–`0104`** undo the schema steps: the `projects` table, the memberships (`users.role` comes back from
  each account's bootstrap-project membership, so a role an admin changed after the upgrade survives the
  rollback), the per-project keys and the one-row settings and banner, the role collapse, the pinned
  `unaccent` function.
- **`0105`–`0108`, `0111`, `0113`** only put indexes back to their pre-tenancy keys. `0107_down` cancels queued
  and running `task_digest` jobs, `0113_down` queued/running `storage_rekey` jobs, `0108_down` the queued and
  running `notification_purge` and `0106_down` the queued and running `family_export` jobs of every project but
  the bootstrap one — the older key or release cannot hold them, and a project deleted before the rollback can
  leave such jobs behind; a job the stopped server left `running` would otherwise be put back to `queued` by
  the previous release's stale-lock recovery and fail its one bulk `UPDATE` on the rebuilt index every tick.
- **`0109_down.sql`** drops the invitation links (accepted memberships stay).
- **`0110_down.sql`** puts back the five per-library `users` columns migration 0110 dropped — `approved_at`,
  `subject_uid`, `last_seen_at`, `visit_reference_at`, `task_digest_at` — filled from the memberships the way
  the releases that still read them kept them in step: approved once any membership is (a maintainer, or an
  account in no project, always), the rest from the bootstrap-project membership (and the column comment of
  `task_digest_at`). An account with **no** membership therefore comes back approved, even one that was
  pending; the script header lists them.
- **`0112_down.sql`** drops the storage-move bookkeeping and refuses once a move has committed a photo (above).
- **`0114_down.sql`** puts back the transitional `DEFAULT 'pj0000000000000000veselice'` (project `v`) that
  migration 0114 dropped from every `project_id` column, for a release whose writers may still insert without
  naming the project. It leaves NOT NULL and the foreign keys alone.
- **`0115_down.sql`** drops `storage_rekey_derived_failures` (the storage move's record of photos whose
  thumbnails or streaming segments stayed at the root) and the run's `derived_cursor`. The schema change is
  safe at any time, but the rows are forgotten: a release older than 0115 has no record, retry or `root
  derived` scan of them, so their root objects then belong to nothing it knows (its `maintenance scan` sees
  them at most as legacy layout or orphans). Rows exist only once a move has committed photos — and then
  `0112_down` refuses to go further — so this matters only when going back to a tenancy release between 0112
  and 0114: list the rows first and clean their root keys up by hand.
- **`0116_down.sql`** drops `storage_rekey_derived_failures.root_keys` (the root thumbnail and streaming keys a
  storage move's final sweep found for a photo, which the retry moves or drops). Safe at any time: an older
  release neither reads nor writes the column, and its sweep finds the objects again from a bucket listing.
- **`0117_down.sql`** drops `project_members.identity_withheld` (an exact-match add whose account has not
  entered the project yet). Safe at any time: the memberships stay approved — an older release simply shows
  those members' names again.
- **`0118_down.sql`** drops `instance_settings.footer_text` (the line every page of a project ends with). Safe
  at any time: an older release draws its own hard-coded footer again; the lines admins wrote are lost, so read
  them first if they are to come back.
- **`0119_down.sql`** drops `notifications.params` (the facts a notification's text was rendered from). Safe at
  any time: an older release shows every notification's stored text, as it always did.
- **`0120_down.sql`** takes the pre-tenancy audit entries of project-scoped actions back out of the bootstrap
  project (same predicate as 0120: older than its `projects` row, filed under it). Safe at any time: an older
  release reads them as it always did — missing from `v`'s own trail, leaderboard and streaks.

## Schema changes since 0099 — the `inventory-diff` expect list

Every difference `kukatko maintenance inventory-diff` reports between an inventory taken on schema 0099
(0.31.0) and one taken after migrating to the newest file, and nothing else — the generic description of what
the chain changes, usable on any instance (see [`OPERATIONS.md`](../OPERATIONS.md), "The inventory"). On a
live instance add the live drift that document lists, after reading it. The marker names the range the list
is proven for: **adding a migration fails `make check`** (`TestReleaseExpectList_coversTheNewestMigration`)
until the new file is classified here (a table, a column, nothing), the marker is moved and the integration
test `TestReleaseExpectList_provenOnTheRealPath` — which replays 0099 → newest on a seeded schema and judges
it with exactly this block — passes again.

<!-- release-expect: 0099..0120 -->
```sh
kukatko maintenance inventory-diff <inventory A> <inventory B> \
  --expect table-added:projects --expect table-added:project_members \
  --expect table-added:project_invites --expect table-added:storage_rekey_runs \
  --expect table-added:storage_rekey_derived_failures \
  --expect 'column-added:*.project_id' --expect column-added:users.last_project_uid \
  --expect column-removed:announcements.id --expect column-removed:instance_settings.id \
  --expect column-removed:sessions.role \
  --expect column-removed:users.approved_at --expect column-removed:users.subject_uid \
  --expect column-removed:users.last_seen_at --expect column-removed:users.visit_reference_at \
  --expect column-removed:users.task_digest_at \
  --expect column-added:instance_settings.footer_text --expect column-added:notifications.params \
  --expect count:schema_migrations --expect role-collapse
```

What each entry is:

| Migration | Difference |
| --- | --- |
| 0100 | `table-added:projects` — one row, the bootstrap project `v`, `storage_prefix ''` (the store root) |
| 0101 | `table-added:project_members` — one membership per account, holding its old role (maintainer → `admin`), `subject_uid` and approval; `column-added:users.last_project_uid` |
| 0102 | `column-added:*.project_id` — the 18 root tables; `column-removed:instance_settings.id`, `column-removed:announcements.id` — the one-row tables become one row per project (the existing row becomes `v`'s) |
| 0103 | `column-removed:sessions.role` (every session survives); `role-collapse` — every viewer/curator/editor/admin account becomes `user`, maintainers unchanged, each old role kept in the membership |
| 0104–0108 | nothing the inventory sees (a function body, indexes) |
| 0109 | `table-added:project_invites` |
| 0110 | the five per-library `users` columns the memberships now hold, named one by one (`column-removed:users.*` would also hide an unintended drop of, say, `users.password_hash`) |
| 0111 | nothing (an index) |
| 0112 | `table-added:storage_rekey_runs` |
| 0113, 0114 | nothing (an index; dropped `project_id` defaults, which the inventory — names and types — does not see) |
| 0115 | `table-added:storage_rekey_derived_failures` |
| 0116, 0117 | nothing from 0099: each adds a column to a table this path creates |
| 0118 | `column-added:instance_settings.footer_text` (its backfill writes the one `v` row, which no digest covers) |
| 0119 | `column-added:notifications.params` (nullable, no backfill: older notifications keep only their stored text) |
| 0120 | nothing (a backfill of `audit_log.project_id`, a column this path adds; no digest covers `audit_log`) |
| all | `count:schema_migrations` |

No content digest, `photos.tsv` row or other row count may change. **Never allow anything else from this
list blindly.**

## Adding a migration to the chain

It means three things in the same change: its down script here (and its version in `rollbackChain`,
`internal/database/rollback_chain_integration_test.go`), its line in "What each script does", and its
classification in the expect list above — `make check` fails until the `release-expect` marker names the new
file.
