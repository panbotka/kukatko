# Migration safety — multi-tenancy

The multi-tenancy programme (`docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`) changes the
schema of a production library that exists once: ~21k photos, 78 GB of originals on R2, 14 accounts. The
owner's rule is that **nothing may be lost** — no row, photo, album membership, face or account — and that
there is **no outage**. Each section below is one rehearsal of the migration chain on a copy of production
data; **later reruns append a dated section** and never rewrite an earlier one. The tools and the deploy
checklist live in [`OPERATIONS.md`](OPERATIONS.md) ("Upgrade runbook — multi-tenancy"); the rollback
scripts in [`migrations-rollback/`](migrations-rollback/README.md).

---

## 2026-10-12 — safety gate I (after the schema steps, migrations 0100–0104)

**Verdict: GO** for deploying migrations 0100–0104, following the checklist at the end. One blocking
finding (backups were not restorable) was found and fixed in this step by migration 0104.

### What was rehearsed

| | |
| --- | --- |
| Production | `0.31.0`, commit `253fd0b` (`GET /healthz`), schema **0099**, PostgreSQL 17.7, DB 1,068 MB |
| Rehearsed | `main` at `066c6bbb` + this step's commit (adds `0104_immutable_unaccent_qualified`) |
| Chain | `0100_projects`, `0101_project_members`, `0102_project_id_root_tables`, `0103_users_role_instance`, `0104_immutable_unaccent_qualified` |
| Data | read-only `pg_dump -Fc` of production, 2026-10-12 13:39 UTC, 426 MB, streamed straight to the box |
| Where | throwaway `pgvector/pgvector:pg17` (17.11) container on the build box; every database a `CREATE DATABASE … TEMPLATE` copy of the restored dump; the dump and all copies were deleted afterwards |
| Binaries | `v0.31.0` built from the tag (old) and the working tree (new), `CGO_ENABLED=0`, linux/amd64 |

Library on the copy: 21,151 photos (21,151 files, 21,151 embeddings), 116,920 faces, 22,572 markers,
142 subjects, 443 albums with 42,651 memberships, 137 labels with 16,947 assignments, 82 tasks over
1,256 photos, 3,916 comments, 2,576 places, 14 accounts (3 maintainers), 175,508 jobs, 23,598 audit entries.

The inventory (`kukatko maintenance inventory`) runs on the **new** binary for both A and B: the old
release has no such command, and the command never migrates, so it reads the old schema as it is.

### Per-migration timing and locks

Wall time is the runner's own `migration applied file=… duration_ms=…` log line (added in this step).
The locks are exact, not sampled: each file was replayed in its own transaction on a fresh copy and
`pg_locks` read for that backend just before `COMMIT` (a 10 ms `pg_locks` sampler running alongside the
real `kukatko migrate` caught only 0102 — every other file finished between two samples). Locks are held
until the file commits, so "held for" equals the file's wall time.

| File | Box | VPS estimate | Strongest lock (held for the whole file) |
| --- | ---: | ---: | --- |
| `0100_projects` | 4 ms | ~20 ms | `ACCESS EXCLUSIVE` on the new `projects` only |
| `0101_project_members` | 5 ms | ~20 ms | `ACCESS EXCLUSIVE` on `users` (ADD COLUMN `last_project_uid`) and the new `project_members`; `SHARE ROW EXCLUSIVE` on `subjects`, `projects` (FKs) |
| `0102_project_id_root_tables` | 239 ms | **~1 s** | `ACCESS EXCLUSIVE` on `photos`, `subjects`, `albums`, `labels`, `faces`, `embeddings`, `face_clusters`, `subject_families`, `photo_tasks`, `upload_links`, `import_runs`, `saved_searches`, `notifications`, `search_history`, `instance_settings`, `announcements`, `photoprism_aliases`, `audit_log` |
| `0103_users_role_instance` | 2 ms | ~10 ms | `ACCESS EXCLUSIVE` on `users`, `sessions` |
| `0104_immutable_unaccent_qualified` | 2 ms | ~10 ms | none on a table (replaces a function body) |
| **Total** (`kukatko migrate`) | **0.27 s** | **~1–2 s** | |

**VPS estimate.** The same read-only query ran on production and on the copy: digesting every face
embedding took 6.22 s vs 2.08 s (3.0×), digesting every photo row 3.82 s vs 0.95 s (4.0×). The estimate
applies 4× and rounds up.

**Outage window** = container restart + ~1–2 s of migrations. During 0102 (~1 s) every request that reads a
catalogue table waits; nothing fails. **No file is anywhere near 30 s, so no split is proposed.**

**Lock queueing (not a finding against the chain, a deploy rule).** `ACCESS EXCLUSIVE` waits for every
transaction already holding a lock on those tables, and every later request waits behind it. A running
`pg_dump` (the nightly backup) holds `ACCESS SHARE` on all of them for its whole run (~2 min), so a deploy
during a backup would stall the whole app for that long. The runner sets no `lock_timeout`. Checklist step 2
covers it.

### Inventory diff A → B

`inventory-diff A B` with the expectation set from the runbook exits **0**: 36 differences, 0 unexpected.

- **All 27 content digests are equal** — `photos`, `photo_files`, `subjects`, `markers`, `faces`,
  `embeddings`, `albums`, `album_photos`, `labels`, `photo_labels`, `photo_tasks`, `photo_task_photos`,
  `comments`, `users` (all columns but `role`), `api_tokens`, `passkey_credentials`, `user_favorites`,
  `user_ratings`, `saved_searches`, `upload_links`, `upload_link_photos`, `subject_families`,
  `subject_family_children`, `photo_hls_renditions`, `photoprism_aliases`, `photo_places`, `photo_edits`.
  `photos.tsv` is identical (21,151 rows: uid, hash, path, size, archive date). **No row was lost or altered.**
- **No row count changed** except `schema_migrations` (99 → 104).

Every difference, each explained by the design:

| Difference | Why |
| --- | --- |
| `table-added:projects` (1 row) | 0100: the bootstrap project `pj0000000000000000veselice`, slug `v`, name Veselice, `storage_prefix ''` |
| `table-added:project_members` (14 rows) | 0101: one membership per account — **14 = number of accounts**. Cross-checked row by row against the old `users`: role equal to the old role (maintainer → `admin`), same `subject_uid`, same approval, `last_project_uid` = the bootstrap project |
| `column-added:*.project_id` (18) | 0102: `albums`, `announcements`, `audit_log`, `embeddings`, `face_clusters`, `faces`, `import_runs`, `instance_settings`, `labels`, `notifications`, `photo_tasks`, `photoprism_aliases`, `photos`, `saved_searches`, `search_history`, `subject_families`, `subjects`, `upload_links` |
| `column-added:users.last_project_uid` | 0101 |
| `column-removed:instance_settings.id`, `column-removed:announcements.id` | 0102: the one-row tables become one row per project (the existing row became `v`'s) |
| `column-removed:sessions.role` | 0103: the cached role is no longer read; all 31 sessions survive |
| `user-role:…` × 11 (`role-collapse`) | 0103: 10 × viewer → user, 1 × editor → user; the 3 maintainers unchanged; each old role is preserved in the membership above |
| `count:schema_migrations` 99 → 104 | the five files |

### Functional parity

The old binary served an unmigrated copy, the new binary a copy it migrated on start; both with an
unreachable embeddings sidecar and an empty local originals root (so no background job could change the
two copies differently). Signed in as a copied **maintainer** (`panbotka`) and as a copied **viewer whose
role collapsed** (`renzik`, the only account with a favourite), passwords reset locally. 16 requests per
instance, all `200`:

| Request | Result |
| --- | --- |
| `GET /photos` pages 1–3 (`limit=100`, offsets 0/100/200), the timeline, `q=faces:yes type:image dated:yes`, album `GET /albums/{uid}` + its photos (2,733-photo album), a subject gallery (1,048 markers), a task + its photos, `/whats-new` (maintainer and viewer), the viewer's favourites and its `GET /photos` | **byte-identical** (`jq -S`) |
| `GET /auth/me` (both accounts) | identical modulo the fields the design adds — `instance_role`, `last_project_slug`, `projects`, `user.instance_role` — plus `download_token` (per session) and `user.last_login_at` (the parity login itself). The viewer's `user.role` is still `viewer` (the transition vocabulary), its `projects[0].role` `viewer` |

**New path:** `/api/v1/p/v/…` is **not mounted yet** at this step (it answers the SPA's HTML); parity
through it is for the final safety gate.

### Rollback

The migrations a plain `DROP` cannot reverse got hand-run down scripts in
[`migrations-rollback/`](migrations-rollback/README.md) (the UNIQUE/PK rewrites, the one-row → per-project
settings and banner, the `users.role` collapse); the trivial ones got one too, so the chain unwinds
completely. On a fresh copy: `kukatko migrate` (new) → `0104…0100_down.sql` (each `psql -1`) → inventory C.

- `inventory-diff A C` with **no** expectations: **0 differences** — inventory A exactly, roles included
  (restored from the memberships).
- The old binary's `kukatko migrate` on the rolled-back copy: "database is up to date".
- `pg_dump -s` of the original copy vs the rolled-back one: identical except `sessions.role`, which comes
  back as the last column instead of the fifth (no query depends on column order).
- `0102_down.sql` refuses to run once a second project exists.

### Storage

- **By reading:** none of 0100–0104 references `file_path`, an object key or the store; 0100 records the
  bootstrap project's `storage_prefix` as `''` (the current root layout). Between `253fd0b` and the
  rehearsed tree, `internal/storage`, `internal/storagemigrate`, `internal/ingest`, `internal/thumb`,
  `internal/hls`, `internal/hlsjob` and `internal/sidecarexport` changed only in two ingest test files. The
  storage move is a separate job started by hand.
- **By listing:** there is **no read-only credential for the production bucket** (only the app's
  read-write key), so per the spec the check ran against the **staging bucket** — staging runs the same
  `253fd0b` — with a throwaway MinIO user limited to `ListBucket`/`GetObject` (a write was refused):
  457 originals, **`storage-missing.tsv` empty**, one listing in 0.21 s. The user and its policy were
  removed afterwards. **Owner action:** create an R2 API token with *Object Read only* on the production
  bucket and pass it to `inventory --storage` in checklist step 3.

### Findings

1. **Blocking, fixed here — production backups could not be restored.** `immutable_unaccent` (0007) called
   `unaccent('unaccent', $1)` unqualified. `pg_restore` empties `search_path`, the planner inlines the
   function while creating `photos.fts`, and `CREATE TABLE photos` fails with
   `function unaccent(unknown, text) does not exist`; `kukatko restore db` restores in one transaction, so
   **the whole restore rolled back** (reproduced with the app's exact flags on today's dump: exit 1, zero
   tables). Migration **0104** pins the function and dictionary to the extension's schema (same value, no
   `photos.fts` rewrite, still inlinable). Proven: a dump of the migrated copy restores with the app's flags
   (exit 0, 21,151 photos) and its inventory equals B with 0 differences. Dumps taken **before** 0104 —
   including the backup step 1 takes before this very deploy — need the manual procedure now in
   [`RESTORE.md`](RESTORE.md), which this rehearsal used to load the copy (all digests equal to production).
2. **Deploy rule — no long transactions during the deploy** (see "Lock queueing").
3. **Live drift** between inventory A and B on the running instance is expected in the live tables; the
   runbook says which ones and how to read them.

### Pre-deploy checklist (owner)

1. **Backup** (`kukatko backup` in the running container); note the dump key — restore it with the manual
   procedure in `RESTORE.md` if ever needed.
2. **No long transactions**, no backup running (`pg_stat_activity`).
3. **Inventory A** on production with the **new** image (`--storage` with a read-only R2 token).
4. **Deploy** (expected unavailability: the container restart + ~1–2 s).
5. **Inventory B**, then **`inventory-diff A B`** with the runbook's expectation set — exit 0 after the live
   drift has been read against `audit_log`.
6. **UI spot-check**: sign in, library grid, a search, an album, a person, the timeline.
7. **Only then** consider the storage move.
