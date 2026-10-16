# Migration safety gate I (after the schema steps)

## Context

Kukátko is being turned multi-tenant while it runs in production: ~20k photos, tens of GB of originals on Cloudflare R2, about a dozen accounts. The owner's rule: **nothing may be lost — no row, no photo, no album membership, no face, no account — and there must be no outage.** Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md` (sections 1, 2, 7). Step 05 of 40; steps 01–04 (projects, members, `project_id` columns, auth) are in the tree. This step adds **no feature**: it rehearses the migration chain from the version production runs today to the current `main` on a copy of production data, measures it, builds a reusable before/after inventory tool, and writes the verdict into the repository. A twin of this step runs again at the very end of the programme, and the owner re-queues it before any release that ships further tenancy migrations.

Facts: the deployed version is a release tag (read it from `/healthz` of the production instance; `main` is ahead of it). Migrations apply automatically at startup, one transaction per file. Backups are `pg_dump` + bucket sync to S3 (`internal/backup`, restore steps in `docs/RESTORE.md`). A staging stack exists on the build box (`<staging dir>`, `./upgrade.sh` rebuilds from `origin/main`); the box has Postgres with pgvector. Production access: credentials in the password manager (`<secret store>`), SSH to the VPS (`<prod host>`), the prod database is `kukatko` in the database container. **Run nothing but read-only SQL and `pg_dump` against production.** Keep the dump on the box only (it contains personal data) and delete it when done.

## Requirements

### 1. Inventory tool (reusable at deploy time)
- `kukatko maintenance inventory --out <dir>` subcommand (preferred; fall back to `scripts/tenancy-inventory.sh` only if the CLI cannot fit the budget — say which in the report). Given the configured database, it writes deterministic files:
  - `counts.tsv`: every table → row count.
  - `digests.tsv`: for `photos`, `photo_files`, `subjects`, `markers`, `faces`, `embeddings` (digest over `photo_uid|face/embedding id|project-free columns`, not the vector itself if too slow — then digest `count + sum of a stable hash of the vector's text`), `albums`, `album_photos`, `labels`, `photo_labels`, `photo_tasks`, `photo_task_photos`, `comments`, `users`, `api_tokens`, `passkey_credentials`, `user_favorites`, `user_ratings`, `saved_searches`, `upload_links`, `upload_link_photos`, `subject_families`, `subject_family_children`, `photo_hls_renditions`, `photoprism_aliases`, `photo_places`, `photo_edits`: an order-independent digest `md5(string_agg(row_to_json(t)::text, '' ORDER BY <pk>))` computed over an explicit **column allow-list per table that exists in both the old and the new schema** (exclude `project_id`, `last_project_uid`, and any column added by the tenancy migrations), so before/after are comparable.
  - `photos.tsv`: sorted `uid|file_hash|file_path|file_size|deleted_at`; `users.tsv`: sorted `uid|username|role|disabled`.
  - `--storage` mode: for every `photos.file_path`, check the object exists in the configured storage backend with `file_size` bytes (use one streamed listing of the bucket and match in memory, not one HEAD per object), and write `storage-missing.tsv` (empty = good).
- `kukatko maintenance inventory-diff <dirA> <dirB>` prints a human-readable diff and exits 1 on any difference outside an `--expect` list of allowed differences (new tables, new columns, the `users.role` collapse). Unit tests for the diff.
- Document both in `docs/OPERATIONS.md` under "Upgrade runbook — multi-tenancy".

### 2. Rehearsal on a copy of production data
- Fresh `pg_dump` of production (read-only) restored into a throwaway database **on the box** (not the Pi's shared test DB, which is too small/shared for timing). Build the **deployed tag's** binary from git and the **current `main`** binary.
- Inventory A with the old binary. Apply `main`'s migrations (`kukatko migrate`), capturing per-file wall time (add timing to the migration runner's log line if it is not there — that is a legitimate code change in this step). Sample `pg_locks`/`pg_stat_activity` in parallel and record the strongest lock each file took and for how long.
- Inventory B with the new binary. `inventory-diff A B --expect …`: every difference must be explained by the design (the `projects` row, `project_members` = number of users, the new columns, the role collapse). **Any unexplained difference blocks.**
- Functional parity on the same restored copy: run the old binary on a pre-migration copy and the new binary on the migrated copy, sign in as a copied maintainer account (reset its password locally), and compare the JSON of: `GET /photos` pages 1–3, the timeline, a `q=` search with three filter keys, one album, one subject gallery, one task, one user's favourites, `/whats-new`, `/auth/me`. Bodies must be equal modulo an explicit list of fields the design adds. Repeat through the new path once it exists (`/api/v1/p/v/…`; if the prefix is not mounted yet at this step, say so).
- Rollback rehearsal: for each migration in the chain that a plain `DROP COLUMN`/`DROP TABLE` cannot reverse (the UNIQUE rewrites, the `users.role` collapse, the one-row → per-project settings/announcements), write `docs/migrations-rollback/<NNNN>_down.sql` (never applied by the app) and prove on a fresh restore that applying up then down returns inventory A.
- Outage window: total migration time on the copy + a container restart = the expected unavailability; any single file over 30 s is a finding with a proposed split.
- Storage: confirm by reading the code and the migrations that nothing in the chain touches object keys (the storage move is a separate, manually started job). Run `inventory --storage` against the restored copy with **read-only** credentials to the production bucket (or the staging bucket, and say so) and record the result.

### 3. The written verdict
- New committed file `docs/MIGRATION_SAFETY.md`: date, prod version and `main` commit rehearsed, per-migration timings and locks table, the inventory diff with every difference explained, parity results, rollback results, storage check, a **Go / No-go** line, and the owner's pre-deploy checklist (backup → inventory A on prod → deploy → inventory B → diff → UI spot-check → only then consider the storage move). Later reruns **append** a dated section.
- Findings that block (an unexplained data difference, a migration failing on real data, a parity mismatch) are fixed in this step when small and local (e.g. a missing `ON CONFLICT`); otherwise written up with the exact failing statement and the task ends as `needs_review`.

## Out of scope

Any feature work, starting the storage move, touching production beyond read-only queries and `pg_dump`.
