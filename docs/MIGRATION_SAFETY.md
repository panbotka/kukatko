# Migration safety — multi-tenancy

The multi-tenancy programme (`docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`) restructures the
schema of a library that exists once. The rule is that **nothing may be lost** — no row, photo, album
membership, face or account — and that the outage is no more than a restart. This document records how the
migration chain (0100–0119) was checked, what was measured and what the checks changed in the software. It
describes the software, not any one deployment: how to upgrade lives in [`OPERATIONS.md`](OPERATIONS.md)
("Upgrading — migrations, the inventory and rollback"), the down scripts and the `inventory-diff` expect list
in [`migrations-rollback/`](migrations-rollback/README.md).

## Method

Each rehearsal ran on a **copy of a real pre-tenancy library** (schema 0099, release 0.31.0 — about 20k photos,
120k faces, 450 albums, 80 GB of originals) restored from a `pg_dump -Fc` into a throwaway
`pgvector/pgvector:pg17` container on the build box. Every working database was a `CREATE DATABASE … TEMPLATE`
copy of the restore, so each experiment started from the same bytes; the dump and the copies were deleted
afterwards. Both binaries were built `CGO_ENABLED=0` from the 0.31.0 tag (old) and the rehearsed commit (new).

- **Inventory A → B.** `kukatko maintenance inventory` (always the **new** binary — it never migrates, so it
  reads the old schema as it is) before and after the migration, judged by `inventory-diff` with the documented
  expect list.
- **Rollback A → C.** The down chain on the migrated copy, then `inventory-diff A C` with **no** expectations.
- **Parity.** The old binary serving an unmigrated copy and the new binary serving a migrated one, read with
  the same accounts (passwords reset on the copy only): list pages, the timeline, searches, an album, a subject
  gallery, a task, favourites, what's new — compared byte for byte after `jq -S` and normalising
  `/api/v1/p/v/` → `/api/v1/`.
- **Locks.** Each file replayed in its own transaction on a fresh copy, `pg_locks` of that backend read just
  before `COMMIT` (exact, not sampled; a 10 ms sampler beside the real `migrate` caught only the longest files).
- **The storage move** on a staging library with real objects in MinIO, interrupted on purpose.
- **Isolation** with a second project: foreign-vs-missing probes over every route, a walk through the UI.

## Per-migration timing and locks

Wall time from the runner's `migration applied … duration_ms` line on the box (several runs agreed within
±30 %); a slower database host scales them roughly linearly (a 4× slower host measured 4× on the same digest
query). Locks are held for the file's whole wall time; only table locks of `SHARE` strength and above listed.

| File | Box | Strongest locks |
| --- | ---: | --- |
| `0100_projects` | 4–11 ms | `ACCESS EXCLUSIVE` on the new `projects` only |
| `0101_project_members` | 5–12 ms | `ACCESS EXCLUSIVE` on `users` and the new `project_members`; `SHARE ROW EXCLUSIVE` on `subjects`, `projects` |
| `0102_project_id_root_tables` | **230–324 ms** | `ACCESS EXCLUSIVE` on the 18 root tables (`photos`, `subjects`, `albums`, `labels`, `faces`, `embeddings`, `face_clusters`, `subject_families`, `photo_tasks`, `upload_links`, `import_runs`, `saved_searches`, `notifications`, `search_history`, `instance_settings`, `announcements`, `photoprism_aliases`, `audit_log`) |
| `0103_users_role_instance` | 2–9 ms | `ACCESS EXCLUSIVE` on `users`, `sessions` |
| `0104_immutable_unaccent_qualified` | 1–10 ms | none on a table (replaces a function body) |
| `0105_face_tooling_project_jobs` | 34–40 ms | `SHARE` on `jobs`, `face_clusters` (index builds: writes wait, reads do not) |
| `0106_family_export_per_project` | 13–17 ms | `ACCESS EXCLUSIVE` on `jobs` (drop + rebuild of a partial index) |
| `0107_task_digest_per_project` | 13–40 ms | `ACCESS EXCLUSIVE` on `jobs` |
| `0108_jobs_library_wide_project_dedup` | 35–40 ms | `ACCESS EXCLUSIVE` on `jobs` |
| `0109_project_invites` | 3–12 ms | `ACCESS EXCLUSIVE` on the new `project_invites`; `SHARE ROW EXCLUSIVE` on `projects`, `users` |
| `0110_users_drop_transitional_columns` | 1–10 ms | `ACCESS EXCLUSIVE` on `users`, `subjects` |
| `0111_photos_project_usage_index` | 13–17 ms | `SHARE` on `photos` |
| `0112_storage_rekey_runs` | 48–55 ms | `SHARE` on `photos` (index build); `ACCESS EXCLUSIVE` on the new `storage_rekey_runs` |
| `0113_jobs_storage_rekey_dedup` | 12–17 ms | `SHARE` on `jobs` |
| `0114_project_id_drop_defaults` | 2–10 ms | `ACCESS EXCLUSIVE` on the 17 root tables that had a default (catalogue only, no rewrite) |
| `0115_storage_rekey_derived_failures` | 2–10 ms | `SHARE ROW EXCLUSIVE` on `photos`, `projects` (foreign keys) |
| `0116`–`0119` | ≤ 1 ms each | a nullable column each |
| `0120_audit_log_project_backfill` | not part of this rehearsal | `ROW EXCLUSIVE` on `audit_log` (an `UPDATE` of the pre-tenancy library entries, no DDL: reads go on), `ACCESS SHARE` on `projects` |
| **Total** (`kukatko migrate`, process wall) | **~0.65 s** | |

**No file is anywhere near 30 s, so none is split.** A server started on the copy answered `/healthz` 0.63 s
after `docker run`, migrations included: the outage is the restart plus the migrations, and during 0102
requests touching a catalogue table wait, nothing fails. Because every `ACCESS EXCLUSIVE` queues behind a
running transaction — a `pg_dump` holds `ACCESS SHARE` on everything for minutes — an upgrade must not overlap
a database backup (OPERATIONS.md, "Lock queueing"); 0106–0108 add `jobs`, the worker's hottest table, to that.

## What the checks proved

- **Inventory A → B** with the documented expect list: exit 0, 0 unexpected. **Every content digest equal**
  (27 catalogue tables — photos, files, subjects, markers, faces, embeddings, albums and their photos, labels,
  tasks, comments, users but `role`, tokens, passkeys, favourites, ratings, saved searches, upload links,
  families, renditions, aliases, places, edits), `photos.tsv` identical, no row count changed but
  `schema_migrations`. **No row was lost or altered.**
- **Memberships = accounts.** A `dblink` join of the unmigrated `users` against the migrated `project_members`:
  one membership per account, each with the old role (maintainer → `admin`), `subject_uid`, `approved_at`,
  `last_seen_at`, `visit_reference_at` and `task_digest_at` — everything the dropped `users` columns held. No
  `project_id` column keeps a default.
- **Parity.** Every compared read byte-identical; `/auth/me` differs only by the tenancy fields
  (`instance_role`, `projects[]`, `last_project_slug`). Unprefixed catalogue routes answer
  `404 {"error":"not found"}`. An API token and a session cookie issued by 0.31.0 keep working after the upgrade.
- **Rollback.** The whole down chain (≈ 2 s) gives `inventory-diff A C` = **0 differences** with no
  expectations, also after the new server had run (then only live drift: jobs, sessions, `last_seen_at`,
  `last_used_at`); 0.31.0's `migrate` says "database is up to date", and 0.31.0 accepts sessions the new release
  created. `pg_dump -s` before vs after differs only in column order (no query depends on it).
- **Backups across the chain.** A dump taken after 0104 restores with plain `pg_restore --single-transaction
  --no-owner --no-privileges`; a 0099-era dump restores with the `RESTORE.md` `search_path` procedure and then
  migrates. Restoring a 0099 dump over a migrated database (rename the failed one, `createdb`, restore) brings
  0.31.0 back serving identically.
- **Two migrators at once** (two new servers started together on one copy): one applied every file, the other
  none — the advisory lock works.
- **The old server during the switch.** With 0.31.0 serving a copy, starting the new binary on it made 0.31.0's
  next sign-in **500** while its `/healthz` stayed 200: the protection is the order stop old → start new. A
  compose `up -d` with a changed tag removes the old container before it starts the new one (event order
  checked), and a failed pull aborts before anything is stopped.

## What the checks changed in the software

1. **Backups could not be restored (fixed by 0104).** `immutable_unaccent` (0007) called
   `unaccent('unaccent', $1)` unqualified; `pg_restore` empties `search_path`, the planner inlines the function
   while creating `photos.fts`, and the one-transaction `kukatko restore db` rolled back completely. 0104 pins
   the function and dictionary to the extension's schema. Older dumps need the manual procedure in
   [`RESTORE.md`](RESTORE.md).
2. **Only `migrate` and `serve` migrate.** Every DB-opening subcommand used to migrate as a side effect, so a
   `maintenance scan` with the new binary before the switch would have broken the old server. Now the others
   refuse a schema that is behind, `maintenance inventory` neither migrates nor refuses, and both migrators
   look for other sessions first (`migrate` refuses without `--force`, `serve` logs them at `ERROR`). Every
   release since announces `application_name` `kukatko/<version> <instance>`.
3. **Down scripts for every file**, starting at `max(version)` (a never-applied file's script fails), and
   `0110_down` puts the column comment of `users.task_digest_at` back.
4. **Queued jobs of a second project.** `0106_down` and `0108_down` rebuild a dedup index keyed on the job type
   alone; a second project's queued job made the rebuild fail half-unwound. They — and `0107_down`,
   `0113_down` — now **cancel** the queued and running jobs the older key cannot hold (refusing would stop the
   chain in the same place); nothing the previous release needs is lost. Covered by
   `TestRollbackChain_queuedJobsOfAGoneProject`.
5. **`0110_down` approves an account with no membership** (the mirror of the forward rule); the script header
   lists such accounts.
6. **One authoritative expect list** for 0099 → newest, guarded by `make check` and an integration proof
   ([`migrations-rollback/README.md`](migrations-rollback/README.md)) — earlier hand-kept lists had gone stale.
7. **The storage move's cleanup loop** (fixed by 0116): a root object no plan named made the closing sweep find
   "the same photo again" after every retry. The sweep now records the keys it found and the retry moves or
   drops exactly those. On a real bucket the measured number of such strays was zero; objects named after a
   hash no photo holds (left by purges and merges) exist and stay at the root as orphans for `maintenance scan`.
8. **Smaller notes.** `project purge` in a container needs `--report` (the default path is the read-only
   working directory). A `maintenance scan` during a move can report originals missing that are not (rows and
   listing are read at different moments) — judge a move by `inventory --storage`, or scan once it is done.
   Right after a move the scan counts every moved photo as missing a thumbnail: it reads only the local cache,
   whose entries sit under the old path, and rebuilds each on first view.

## The storage move, rehearsed

On a staging library (~560 photos, 1,310 objects: originals, sidecars, HLS segments, 4.3 GiB) the move was
started, paused, resumed, **killed mid-run** (`docker kill` of the server), restarted and finished. At every
checkpoint a full bucket listing, `inventory --storage` and `maintenance scan`: **no root object was ever
deleted without its prefixed twin**, `storage-missing` stayed empty, the run ended `done` with 0 errors.
Throughput against a local MinIO ~4 photos/s; against a remote R2 expect ~1–2.5 s per photo (OPERATIONS.md,
"How long, what it costs"). A second rehearsal on a root-layout copy with seeded strays (an unregistered
thumbnail size, a bad streaming name, an unrecorded segment, a tile of no photo) ended `done` with no cleanup
loop: the first two deleted, the segment moved, the tile left at the root as the scan's one orphan. A project
created after the tenancy release is refused (`already has its own storage prefix`). Not exercised end-to-end:
a media CDN in front of prefixed keys — OPERATIONS.md has the check to run a few minutes into a move.

## Isolation

With a second project and its own accounts: 182 foreign-vs-missing probes over 139 routes answered
identically (a foreign uid is a missing one, bodies and headers byte-identical), foreign uids in write bodies
refused like missing ones, the same bytes uploaded into two projects became two rows and two keys, purging one
project left the other's rows, digests and objects untouched, and no name of the other project's members
appeared anywhere. A UI walk (desktop and phone) switched projects 66 times across every list page with
Back/Forward and found no request to the previous project after a switch; an upload interrupted by a switch
asks first and puts nothing into the other project.

## Open at the last gate

Should-fix, not blockers:

1. **Pre-tenancy audit history has no project.** 0102 adds `audit_log.project_id` nullable and nothing
   backfills it, so after the upgrade the bootstrap project's audit page and review leaderboard start empty and
   the history shows only in the instance audit. Not a leak, nothing lost; a migration can set the project of
   the `NULL` rows of project-scoped actions at any time.
2. ~~**An uploaded profile picture is served across projects** (`GET /users/{uid}/avatar` answers any signed-in
   account; SEC-025) — an existence oracle. A library photo picked as the picture does not leak.~~
   **Closed 2026-10-15** (multi-tenancy review III): an account's picture is served only to itself, a
   maintainer, or a viewer it shares an active project with; everybody else gets the made-up uid's 404.
3. **Browser Back during an upload stops the batch silently** (the leave guard catches link clicks and tab
   close, not a history pop); no file reaches a wrong project.
