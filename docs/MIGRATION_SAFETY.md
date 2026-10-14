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
during a backup would stall the whole app for that long. The runner sets no `lock_timeout` (deliberately: a
timed-out ALTER would fail the start and turn the restart into a loop). Checklist step 2 covers it. Since
2026-10-14 the runner holds one `pg_advisory_lock` for its whole run, so two migrators started at once (a
`serve` start and a CLI subcommand) take turns instead of racing; the second finds the schema up to date.

**Who migrates (round-2 review, 2026-10-14).** The advisory lock only serialises two *new* binaries; it never
noticed the old server merely being connected, and every DB-opening subcommand used to migrate as a side
effect — so an operator running `maintenance scan` with the new image before the switch would have broken
0.31.0 (every sign-in 500, `/healthz` still 200, proven twice). Now only `kukatko migrate` and `kukatko serve`
migrate; every other subcommand refuses a schema that is behind (`database is at schema N, this binary
expects M`), and `maintenance inventory` neither migrates nor refuses. Before migrating, both look for other
sessions on the database (`pg_stat_activity`: any `kukatko/…` `application_name`, which every release from
this one on sets, plus any other session of the same user, which is how 0.31.0 shows up): `migrate` refuses
without `--force`, `serve` logs them at `ERROR` and starts anyway. The deploy order stays **stop old → start
new**; the runbook ([`OPERATIONS.md`](OPERATIONS.md), "Upgrade runbook — multi-tenancy") lists which
commands may run while the old container still serves.

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
| `GET /auth/me` (both accounts) | identical modulo the fields the design adds — `instance_role`, `last_project_slug`, `projects`, `user.instance_role` — plus `download_token` (per session) and `user.last_login_at` (the parity login itself). The viewer's `user.role` is still `viewer` (the transition vocabulary), its `projects[0].role` `viewer` — **true at this step only**: since the account record speaks only the instance role, `user` carries `instance_role` and no `role`, and the per-project role and `subject_uid` live in `projects[]` (round 2, 2026-10-14) |

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

---

## 2026-10-13 — safety gate II (the whole programme, migrations 0100–0114)

**Verdict: GO** for releasing `main` to production over 0.31.0, following the release-day checklist at the
end. No blocking finding: no data difference, no failed migration, no leak between tenants, no object lost by
the storage move. Two small findings were fixed here (a rollback script that dropped a column comment, eight
missing down scripts) and three are written up below as deploy notes.

### What was rehearsed

| | |
| --- | --- |
| Production | `0.31.0`, commit `253fd0b` (`GET /healthz`), schema **0099** |
| Rehearsed | `main` at `a2236428` + this step's commit (rollback scripts and docs only, no code) |
| Chain | all fifteen files `0100_projects` … `0114_project_id_drop_defaults` |
| Data | read-only `pg_dump -Fc` of production, 2026-10-13 18:58 UTC, 426 MB, 1 min 51 s, streamed straight to the box; restored with the manual pre-0104 procedure of [`RESTORE.md`](RESTORE.md) (exit 0, 813 MB) |
| Where | throwaway `pgvector/pgvector:pg17` container on the build box; every database a `TEMPLATE` copy of the restore. The dump, the copies and the container were deleted afterwards |
| Binaries | `v0.31.0` from the tag (old) and `main` (new), `CGO_ENABLED=0`, linux/amd64; inventory A/B/C all with the new binary |
| Walkthrough | the staging image built from `main` (`a2236428`), run as a second container on the box against a migrated copy, with an empty bucket of its own on staging MinIO |
| Storage move | the box staging stack itself (its own library, real objects in MinIO), upgraded from `253fd0b` to `main` |

Library on the copy: 21,151 photos (21,151 files, 21,151 embeddings), 116,920 faces, 22,572 markers, 142
subjects, 443 albums with 42,651 memberships, 137 labels with 16,947 assignments, 82 tasks over 1,256 photos,
3,916 comments, 2,576 places, 14 accounts (3 maintainers), 175,509 jobs, 23,604 audit entries, 30 sessions.

### Per-migration timing and locks

Wall time from the runner's `migration applied` log line of `kukatko migrate` on a fresh copy (a second run —
the walkthrough container migrating its copy on start — agreed within a few ms). Locks are exact: each file
replayed in its own transaction on another copy, `pg_locks` of that backend read just before `COMMIT`; a lock
is held for the file's whole wall time. Only table-level locks of `SHARE` strength and above are listed.

| File | Box | VPS estimate | Strongest locks (held for the whole file) |
| --- | ---: | ---: | --- |
| `0100_projects` | 7 ms | ~30 ms | `ACCESS EXCLUSIVE` on the new `projects` only |
| `0101_project_members` | 8 ms | ~30 ms | `ACCESS EXCLUSIVE` on `users` and the new `project_members`; `SHARE ROW EXCLUSIVE` on `subjects`, `projects` |
| `0102_project_id_root_tables` | 258 ms | **~1 s** | `ACCESS EXCLUSIVE` on the 18 root tables (`photos`, `subjects`, `albums`, `labels`, `faces`, `embeddings`, `face_clusters`, `subject_families`, `photo_tasks`, `upload_links`, `import_runs`, `saved_searches`, `notifications`, `search_history`, `instance_settings`, `announcements`, `photoprism_aliases`, `audit_log`) |
| `0103_users_role_instance` | 4 ms | ~20 ms | `ACCESS EXCLUSIVE` on `users`, `sessions` |
| `0104_immutable_unaccent_qualified` | 4 ms | ~20 ms | none on a table |
| `0105_face_tooling_project_jobs` | 40 ms | ~160 ms | `SHARE` on `jobs`, `face_clusters` (index builds: writes wait, reads do not) |
| `0106_family_export_per_project` | 15 ms | ~60 ms | `ACCESS EXCLUSIVE` on `jobs` (drop + rebuild of a partial index) |
| `0107_task_digest_per_project` | 14 ms | ~60 ms | `ACCESS EXCLUSIVE` on `jobs` |
| `0108_jobs_library_wide_project_dedup` | 37 ms | ~150 ms | `ACCESS EXCLUSIVE` on `jobs` |
| `0109_project_invites` | 5 ms | ~20 ms | `ACCESS EXCLUSIVE` on the new `project_invites`; `SHARE ROW EXCLUSIVE` on `projects`, `users` |
| `0110_users_drop_transitional_columns` | 4 ms | ~20 ms | `ACCESS EXCLUSIVE` on `users`, `subjects` |
| `0111_photos_project_usage_index` | 16 ms | ~70 ms | `SHARE` on `photos` |
| `0112_storage_rekey_runs` | 53 ms | ~220 ms | `SHARE` on `photos` (index build); `ACCESS EXCLUSIVE` on the new `storage_rekey_runs` |
| `0113_jobs_storage_rekey_dedup` | 13 ms | ~60 ms | `SHARE` on `jobs` |
| `0114_project_id_drop_defaults` | 4 ms | ~20 ms | `ACCESS EXCLUSIVE` on the 17 root tables that had a default (catalogue only, no rewrite) |
| **Total** (`kukatko migrate`, process wall) | **0.51 s** | **~2–3 s** | |

The VPS estimate applies gate I's measured 4× (box vs VPS on the same digest query) and rounds up. **No file is
anywhere near 30 s** — the largest is 0102 at ~1 s on the VPS. A 10 ms `pg_locks` sampler running beside the
real `migrate` caught only 0102 and the `jobs` index builds, consistent with the replay.

**Outage window.** The walkthrough container went from `docker run` to a healthy `/healthz` in **0.63 s** on the
box, migrations included. On the VPS expect **the container restart plus ~2–3 s**; during 0102 (~1 s) requests
touching a catalogue table wait, nothing fails. The lock-queueing rule of gate I stands (no deploy during a
`pg_dump`): every `ACCESS EXCLUSIVE` above queues behind a running transaction, and 0106–0108 now add `jobs`
— the worker's hottest table — to that list.

### Inventory diff A → B

`inventory-diff A B` with the expectation set below exits **0**: **43 differences, 0 unexpected**.

- **All 27 content digests are equal**, `photos.tsv` identical (21,151 rows). **No row was lost or altered.**
- **No row count changed** except `schema_migrations` (99 → 114).
- **Memberships cross-checked row by row** against the old `users` (a `dblink` join of the copy before and
  after): 14 memberships for 14 accounts, all 14 with the old role (maintainer → `admin`), the same
  `subject_uid`, the same approval and the same `last_seen_at` / `visit_reference_at` / `task_digest_at` —
  i.e. everything the dropped `users.*` columns held survives in `project_members`.
- **No `project_id` column keeps a default** (`information_schema`: 0).

| Difference | Why |
| --- | --- |
| `table-added:projects` (1 row) | 0100: `pj0000000000000000veselice`, slug `v`, Veselice, `storage_prefix ''` (the root) |
| `table-added:project_members` (14 rows) | 0101: one membership per account — **14 = the number of accounts**, cross-checked above |
| `table-added:project_invites` (0 rows) | 0109 |
| `table-added:storage_rekey_runs` (0 rows) | 0112 — no move has started |
| `column-added:*.project_id` × 18 | 0102; no default since 0114 (the inventory sees names and types only) |
| `column-added:users.last_project_uid` | 0101 |
| `column-removed:instance_settings.id`, `column-removed:announcements.id` | 0102: settings and the banner keyed by project; the existing row became `v`'s |
| `column-removed:sessions.role` | 0103; all 30 sessions survive |
| `column-removed:users.approved_at`, `…subject_uid`, `…last_seen_at`, `…visit_reference_at`, `…task_digest_at` | 0110: the values live on in the memberships (cross-checked above) |
| `user-role:…` × 11 (`role-collapse`) | 0103: 10 × viewer and 1 × editor → `user`; the 3 maintainers unchanged; each old role kept in the membership |
| `count:schema_migrations` 99 → 114 | the fifteen files |

The expectation set for a deploy from 0.31.0 (use it in step 6 of the checklist):

```sh
kukatko maintenance inventory-diff A B \
  --expect table-added:projects --expect table-added:project_members \
  --expect table-added:project_invites --expect table-added:storage_rekey_runs \
  --expect 'column-added:*.project_id' --expect column-added:users.last_project_uid \
  --expect column-removed:announcements.id --expect column-removed:instance_settings.id \
  --expect column-removed:sessions.role --expect 'column-removed:users.*' \
  --expect count:schema_migrations --expect role-collapse
```

The same chain on the **staging** library (upgrade `253fd0b` → `main` by its own `./upgrade.sh`, inventories
with `--storage` against its MinIO): 41 differences of the same kinds, the only one outside the set being
`count:jobs` +1 — the new release's startup scheduler queueing a job, the live drift the runbook expects.
`storage-missing.tsv` empty before and after (457 originals).

### Rollback

Down scripts now exist for **every** file of the chain: this step added `0105`–`0109`, `0111`, `0112` and
`0113_down.sql` (indexes put back to their pre-tenancy keys; `0107_down` and `0113_down` cancel queued jobs the
older key or release cannot hold; `0112_down` refuses once a storage move has committed a photo). On a fresh
copy: `kukatko migrate` (new) → `0114_down` … `0100_down` (each `psql -1`, newest first) → inventory C.

- `inventory-diff A C` with **no** expectations: **0 differences** — inventory A exactly.
- `schema_migrations` back at 99; the **old** binary's `kukatko migrate`: "database is up to date".
- `pg_dump -s` of the original copy vs the rolled-back one: identical except column order (`announcements.id`,
  `instance_settings.id`, `sessions.role`, `users.welcome_seen_at` come back last — no query depends on it)
  and the comment on `users.task_digest_at`, which `0110_down` dropped. **Fixed here**: `0110_down` now puts
  the comment back.

**Follow-up (2026-10-14) — queued jobs of a second project.** `0108_down` (notification_purge) and `0106_down`
(family_export) rebuild a dedup index keyed on the job type alone, while the tenancy chain queues one such job
per project. With a second project's job still queued — e.g. a project deleted before the rollback so that
`0102_down`'s guard passes, its jobs surviving because a payload's `project_uid` is no foreign key — the
rebuild failed with a duplicate key and the hand-run chain stopped half-unwound. **Chosen behaviour: cancel,
not refuse.** Each of the two scripts now first deletes every queued job of its type that is not the bootstrap
project's, the way `0107_down` cancels digests; the bootstrap project's queued job is kept. Nothing the
previous release could use is lost: it serves only the bootstrap library, the purge is re-queued by its
startup and daily scheduler, and a family export by the next relation change (or `kukatko sidecar families`).
Refusing would have stopped the chain in the same half-unwound place. Covered by
`TestRollbackChain_queuedJobsOfAGoneProject` (the whole 0114 → 0100 chain over two projects' queued jobs).

**`0110_down` — an account with no membership comes back approved.** It rebuilds `users.approved_at` as "any
membership approved; a maintainer, or an account in **no** project, always" — the mirror of the forward rule,
where membership approval decides and an account in no project has nothing to be approved in. So an account
that was pending before the deploy and has no membership row at rollback time (never got one, or its
membership was removed) can sign in to the previous release. The script header carries the query that lists
such accounts; run it before rolling back if that matters.

### Product walkthrough (staging image of `main`, migrated production copy)

Signed in as the copied maintainer `panbotka` (password reset on the copy only). The copy had **no originals**
(there is no read-only production bucket key; its own bucket was empty), so every grid tile is a placeholder —
this walk checks routing, data and isolation, not pixels. Screenshots: `/mnt/nas-botka/kukatko/mt-gate-II/`
(`d-*` desktop 1440×900, `m-*` phone 390×844, `i-*` the second tenant).

| | Desktop | Phone |
| --- | :-: | :-: |
| `/` lands in `/p/v` | ✅ | ✅ |
| Library grid (20,421 photos), "what's new" panel on it (7 newly named people, 1 task waiting) | ✅ | ✅ |
| Timeline (`?view=timeline`) | ✅ | ✅ |
| Search with three keys `faces:yes type:image year:2019` | ✅ | ✅ |
| An album (Jihovýchod), a person (Miloslav Novotny), the family tree (Tomáš Kozák) | ✅ | ✅ |
| A task with its comment thread | ✅ | ✅ |
| Favourites, notifications inbox | ✅ | ✅ |
| Upload one photo into `v`, then archive + purge it from the trash (row and object gone) | ✅ | — |
| Members (14), project settings | ✅ | ✅ |
| `/system/projects` (`v`: 14 members, 21,152 photos, 78.4 GB), `/system/users` | ✅ | ✅ |
| `/system/projects/v` storage card: **"v kořeni úložiště"**, „Spustit přesun" shown and **not pressed** | ✅ | ✅ |
| `/projects` chooser | ✅ | ✅ |

**15 old-style deep links** (production's access log holds no SPA deep links — client-side navigation is not
logged and the rest is bot scans — so they were composed from real uids): `/photos/<uid>`, `/albums`,
`/albums/<uid>`, `/labels/<uid>`, `/people`, `/people/<uid>`, `/people/<uid>/tree`, `/tasks/<uid>`,
`/n/<uid>`, `/search?q=faces%3Ayes`, `/?q=year%3A2019`, `/favorites`, `/map`, `/places`,
`/photos/<uid>?panel=info#top` — **15/15 forward to their `/p/v/` twin**, query and hash intact (`/?q=…` lands
on `/p/v?q=…`, the library root; `/map` then appends its own viewport).

### Second tenant — isolation

| Check | Result |
| --- | --- |
| Create `gate-ii` through `/system/projects` | ✅ prefix `projects/pjt5esq2gjdlljtnns83ihjfuk/`, creator = its admin |
| Invite link (editor) from `/p/gate-ii/members`, register `gatetwo` through it | ✅ approved at once, membership of `gate-ii` only |
| As `gatetwo`: `/` → `/p/gate-ii`; chooser lists only Gate II | ✅ |
| `/p/v/`, `/p/v/photos/<uid>`, `/p/v/albums/<uid>` | ✅ "Stránka nenalezena" |
| `/api/v1/p/v/photos` vs `/api/v1/p/<random>/photos` | ✅ both `404 {"error":"project not found"}`, **byte-identical bodies and headers** |
| `v`'s photo / album / subject uid asked through `/p/gate-ii/…` | ✅ 404 "not found", same as a missing uid |
| No name of a `v` member anywhere: 25 display + user names grepped through `gate-ii`'s users directory, subjects, global search, photos, albums, tasks, what's new, `auth/me`, own audit, announcement | ✅ none (the directory shows only Gate Two and Pan Botka, who created `gate-ii` and is its member) |
| `/p/gate-ii/members`, `/p/gate-ii/settings`, `/system/projects` as an editor | ✅ "Sem nemáte přístup" / 403 |
| Same bytes uploaded into `v` and into `gate-ii` | ✅ **two rows, two keys**: `2026/10/gate2-same-bytes.jpg` and `projects/pjt5…/2026/10/gate2-same-bytes.jpg` |
| Archive/purge `v`'s photo through `gate-ii` | ✅ 404, untouched; an editor's purge → 403 |
| Purge the copy from `gate-ii`'s trash (UI, as its admin) | ✅ its row and key gone; `v`'s object still there, sha256 `0817e9e7…` = the uploaded file |
| `kukatko project purge gate-ii` (dry run, then `--yes --confirm gate-ii`) | ✅ dry run: 1 photo, 1 album, 2 memberships, 1 invite, 4 jobs, 2 objects (76 KiB); executed exactly that |
| `v` after the purge | ✅ per-table counts and digests of `v`'s photos, albums, album members, subjects, members and users identical before/after; the instance albums digest back to inventory A's value; root objects of the bucket unchanged; the instance diff shows only gate-ii's rows |

### Storage move rehearsal (staging only)

On the staging library after its upgrade, plus 100 generated JPEGs (`import dir --project v`) to pass 500
photos: **557 photos, 1,310 objects** (originals, 457 sidecars, 295 HLS segments/playlists; staging keeps its
thumbnails in the local cache, not the bucket), 4.3 GiB. `storage rekey v --dry-run` listed the plan and
changed nothing. Then: start → pause after 8 s → resume → **`docker kill` of the server mid-run** → start →
let it finish. At every checkpoint: a full bucket listing, `inventory --storage` and `maintenance scan`, and
"every root object gone since the baseline exists under the prefix with the same size".

| Checkpoint | Rows moved | Root / prefixed objects | `storage-missing` | Deleted root objects without a prefixed twin |
| --- | ---: | ---: | ---: | ---: |
| Baseline | 0 / 557 | 1,310 / 0 | 0 | — |
| Paused | 40 | 1,230 / 80 | 0 | **0** of 80 |
| Killed mid-run (server down) | 70 | 1,170 / 140 | 0 | **0** of 140 |
| Restarted, resumed on its own | 105 | 1,110 / 200 | 0 | **0** of 200 |
| Done | **557 / 557** | **0 / 1,310** | 0 | **0** of 1,310 |

Run `done`, 0 errors; the final scan reports 0 missing originals and 0 orphans. **Throughput: 557 photos in
141 s** including the pause and the kill — **~4 photos/s (~240/min, ~33 MB/s)** against the configured cap of
10/s (`storage.rekey.per_second`). Extrapolated to production's 21,151 photos: **~1.5 h at the box's rate**,
35 min at the cap. Production is slower per photo: R2 over the internet instead of a local MinIO, and ~10
objects per photo (eight thumbnail sizes live in the bucket there) instead of ~2.4 — **plan for 3–9 hours**,
read the real rate from `/system/projects/v` after the first minutes. The move runs online and resumable.

### Findings

1. **Fixed here — down scripts missing for 0105–0109 and 0111–0113**, and `0110_down` dropped the comment of
   `users.task_digest_at`. Written and proven (inventory C = A, see Rollback).
2. **Deploy note — `project purge` in the container needs `--report`.** The default report path is the
   working directory, `/app`, which `nobody` cannot write: the purge itself completed (database and store),
   then the command exited 1 with `writing the purge report …: permission denied`. Runbook updated
   ([`OPERATIONS.md`](OPERATIONS.md)); not used on production by this release.
3. **Deploy note — a mid-move scan can report originals missing that are not.** The scan on the restarted
   checkpoint printed `missing originals: 2` while the move was running; the inventory (one transaction, one
   listing) showed 0 and the final scan 0. The scan reads rows and the listing at different moments, so a
   photo committed in between looks missing. Judge a move by `inventory --storage`, or scan once it is done.
4. **Coverage gap — moving bucket thumbnails was not exercised.** Staging keeps thumbnails in the local cache,
   so the rehearsal moved originals, sidecars and HLS objects only; on staging the thumbnails were re-rendered
   on first view after the move (~0.4 s each, then 5 ms cached) and the scan counts the old local tree as
   "missing thumbnails". Production serves thumbnails from the bucket through the media CDN and keeps no local
   thumbnail cache, so there the move carries them (`internal/rekey` plans every size; covered by
   `TestPlan_listsEveryObjectOfAPhoto` and `TestService_fullRun`). **Owner action:** a few minutes into the
   production move, open the library and check that a moved photo's tile still loads.
5. **Not covered:** pixels (no originals on the copy), push delivery (staging is not a secure context), mail
   (off on staging).

### Release-day checklist (owner)

**Never migrate under the running 0.31.0.** 0.31.0 served against the migrated schema fails every sign-in with
500 (`column "subject_uid" does not exist`, proven on the copy). Until step 5 has stopped the old container,
do **not** run `kukatko migrate` with the new binary against production (it refuses anyway while 0.31.0 is
connected — never `--force` it). The other DB-opening subcommands (`maintenance scan`, `import dir`, `sidecar
…`, `storage …`, `restore …`) no longer migrate as a side effect: they refuse a schema that is behind and exit
1, so running one by mistake is harmless; `maintenance inventory` is the one meant for this moment. The
"pre-migrate to shorten downtime" advice in [`OPERATIONS.md`](OPERATIONS.md) holds only for releases whose
migrations are backward compatible — not this one. The only safe order is stop the old container → start the
new one, which migrates on start.

1. **Backup** — `kukatko backup` in the running 0.31.0 container; note the dump key (a pre-0104 dump restores
   only with the manual procedure in [`RESTORE.md`](RESTORE.md)).
2. **No long transactions, no `pg_dump` running** (`pg_stat_activity`) — 0102, 0106–0108 and 0114 queue behind them.
3. **Inventory A** on production with the **new** image (`--storage` with a read-only R2 token, if one exists by then).
4. **Repoint hand-configured API clients** to `/p/v` (MCP connector, `ctl` contexts, scripts) — the unprefixed
   API is gone.
5. **Deploy: stop the 0.31.0 container, then start the new one** (it migrates on start; no `kukatko migrate`
   beforehand). Expected unavailability: the container restart + ~2–3 s of migrations.
6. **Inventory B**, then **`inventory-diff A B`** with the expectation set above; exit 0 once the live drift
   (`jobs`, `audit_log`, `sessions`, `notifications`, `search_history`, `digest:users`) has been read against
   `audit_log`.
7. **UI spot-check from the walkthrough list:** `/` → `/p/v`, library, timeline, a three-key search, an album, a
   person, the family tree, a task, `/system/projects` (`v`: 14 members, the photo count of inventory A) and
   `/system/projects/v` showing "v kořeni úložiště"; one old bookmark (`/albums/<uid>`) forwards to `/p/v/…`.
8. **Announce** the release to the members.
9. **Only later**, after a **fresh backup on the new release**: `kukatko storage rekey v --dry-run`, then
   „Spustit přesun" on `/system/projects/v`, and **watch it** — rate, ETA, errors; pause/resume from the same
   card. A few minutes in, check that a moved photo's tile loads (finding 4). When it says `done`, run
   `inventory --storage` and `maintenance scan`.

---

## 2026-10-14 — root derived strays before the storage move of `v` (read-only measurement)

**Question:** how many root `thumb/` and `hls/` objects does production hold that a storage move's plan does
not name — the strays that, before migration 0116, made `Finish` go round `pending cleanup` forever (the
final sweep found them, the retry did not touch them)? **Answer: none.** Measured read-only: the R2 bucket
listed with the read-only `kukatko-backup-r2-object-read` token (`aws s3api list-objects-v2 --prefix thumb/`
and `hls/`), production's `photos` and `photo_hls_renditions` read with `SELECT` (schema 0099 then), and every
key compared with what `internal/rekey`'s plan builds — the eight registered thumbnail sizes, and per
recorded rendition `init.mp4`, `00000…(segment_count−1).m4s` and the names its playlist lists.

| Root tree | Objects | Named after a photo's hash | … of those not planned (strays) | Hash of no photo (orphans) | Other shape |
| --- | ---: | ---: | ---: | ---: | ---: |
| `thumb/` | 169,518 | 169,206 | **0** | 312 (39 hashes × 8 sizes) | 0 |
| `hls/` | 125 | 125 (7 renditions = the 7 rows) | **0** | 0 | 0 |

All 21,151 photos have thumbnails (one lacks `fit_2560`/`fit_3840`: a planned key with no object, which the
move skips). The **312 orphans** belong to 39 hashes that no `photos`/`photo_files` row names — most likely
left behind by purges or merges (79 `photo.purge`, 10 `photos.merge` in the audit log; not proven). The sweep
matches root keys by a photo's hash, so they never enter the move: they stay at the root after `v` is `done`,
and `maintenance scan` reports them as orphans — review, then delete by hand if wanted (nothing needs them:
their photos are gone).

So the move of `v` would not have looped on today's bucket; the fix (0116: the sweep records the keys it
found and the retry moves or drops exactly those) still matters for whatever lands at the root between now
and the move — a thumbnail size retired by a later release, an encode published under a rendition the row no
longer records.

---

## 2026-10-14 — round-2 rehearsal (0099→0115)

**Verdict: the code and the migration chain are GO; the operator runbook as it stood was NO-GO** — three
blockers, all documentation/process (B1–B3 below), fixed in the runbook the same day. Rehearsed on `main`
**672357f6** (migrations 0100–0115; 0116 came later and only adds a column to a table this path creates)
against a **fresh production dump**; production was **0.31.0 / 253fd0b, schema 0099**. The only actions on
production were one `pg_dump -Fc`, `/healthz`, `docker ps/inspect/logs`, env *names* and file listings; all
work ran on the box. Full report: `/mnt/nas-botka/kukatko/multitenancy-review-2/migration-review.md`.

### What was rehearsed

- **The dump:** `docker exec pgvector pg_dump -U postgres -Fc kukatko` on the VPS, piped to the box —
  425,634,450 B in 1 min 55 s (started 10:22:54 UTC). Restored into a throwaway `pgvector/pgvector:pg17` with
  the pre-0104 `sed` procedure of [`RESTORE.md`](RESTORE.md): exit 0, **3 min 21 s**, 813 MB — 21,151
  photos, 14 accounts, schema 99. Every working database was a `TEMPLATE` copy of it.
- **Binaries:** `v0.31.0` and `672357f6`, `CGO_ENABLED=0 linux/amd64`; the staging image built from exactly
  672357f6.

### Per-migration timing (runner log `migration applied … duration_ms`, box)

| File | `migrate` (cold) | `serve` start | `serve` (2nd instance) | lock-test copy | VPS est. (×4) |
| --- | ---: | ---: | ---: | ---: | ---: |
| 0100_projects | 11 | 5 | 4 | 5 | ~40 ms |
| 0101_project_members | 12 | 5 | 5 | 5 | ~50 ms |
| 0102_project_id_root_tables | **324** | 236 | 234 | 241 | **~1–1.3 s** |
| 0103_users_role_instance | 9 | 3 | 2 | 2 | ~40 ms |
| 0104_immutable_unaccent_qualified | 10 | 2 | 2 | 2 | ~40 ms |
| 0105_face_tooling_project_jobs | 39 | 37 | 37 | 36 | ~160 ms |
| 0106_family_export_per_project | 17 | 15 | 13 | 13 | ~70 ms |
| 0107_task_digest_per_project | 40 | 13 | 13 | 13 | ~160 ms |
| 0108_jobs_library_wide_project_dedup | 40 | 38 | 35 | 36 | ~160 ms |
| 0109_project_invites | 12 | 3 | 3 | 3 | ~50 ms |
| 0110_users_drop_transitional_columns | 10 | 2 | 2 | 2 | ~40 ms |
| 0111_photos_project_usage_index | 17 | 13 | 13 | 14 | ~70 ms |
| 0112_storage_rekey_runs | 55 | 48 | 51 | 51 | ~220 ms |
| 0113_jobs_storage_rekey_dedup | 17 | 13 | 12 | 13 | ~70 ms |
| 0114_project_id_drop_defaults | 10 | 2 | 2 | 2 | ~40 ms |
| 0115_storage_rekey_derived_failures | 10 | 2 | 3 | 2 | ~40 ms |
| **Total (process wall of `kukatko migrate`)** | **0.65 s** | | | | **~2–3 s** |

**0115's locks** (replayed in `BEGIN … pg_locks … ROLLBACK` on a copy at 0114): `SHARE ROW EXCLUSIVE` on
`photos` and `projects` (its foreign keys), `ACCESS EXCLUSIVE` on `storage_rekey_runs` and its own table, for
~2–10 ms — harmless. Gate II's lock table for 0100–0114 still stands (timings within ±30 %).

### Inventory A → B

Inventory A with the new binary on the unmigrated copy: 4.78 s, 48 MB RSS, and **it did not migrate**
(schema still 99 afterwards).

| Expect list used | Result |
| --- | --- |
| The OPERATIONS.md runbook list of the time (the 0100–0104 set step 6 pointed at) | **exit 1**, 44 differences, **8 unexpected**: `column-removed:users.{approved_at,last_seen_at,subject_uid,task_digest_at,visit_reference_at}`, `table-added:project_invites`, `table-added:storage_rekey_runs`, `table-added:storage_rekey_derived_failures` |
| Gate II's list (its release-day checklist, step 6) | **exit 1**, 44 differences, **1 unexpected**: `table-added:storage_rekey_derived_failures` (0 rows) |
| Gate II's list + `--expect table-added:storage_rekey_derived_failures` | **exit 0**, 44 differences, 0 unexpected |

- From 0099, `storage_rekey_runs.derived_cursor` never shows: the whole table is `table-added` (the staging
  0114 → 0115 symptom `column-added:…derived_cursor` does not occur on production).
- **All content digests equal**, `photos.tsv` identical, no count changed but `schema_migrations` (99 → 115).
- 11 `user-role` collapses: 10 × viewer → user, 1 × editor → user; the 3 maintainers unchanged.
- No `project_id` column keeps a default (`information_schema`: 0).
- **Memberships = accounts:** a `dblink` join of the unmigrated `users` against the migrated
  `project_members`: `accounts_old 14 | memberships 14 | users_new 14 | mismatch 0`, comparing role
  (maintainer → admin), `subject_uid`, `approved_at`, `last_seen_at`, `visit_reference_at`, `task_digest_at`.
  Member roles `admin:3, viewer:10, editor:1`; instance roles `user:11, maintainer:3`.

**Resolved:** the runbook now carries **one** authoritative list for 0099 → newest, the third row with the five
`users` columns named instead of `column-removed:users.*`
([`OPERATIONS.md`](OPERATIONS.md), "The release-day expect list"). It is guarded by two tests:
`TestReleaseExpectList_coversTheNewestMigration` (`make check`: the list's marker must name the newest
migration) and `TestReleaseExpectList_provenOnTheRealPath` (integration: replays 0099 → newest on a seeded
schema, judges it with the documented block — 0 unexpected, and every entry must allow something). The
lists in the sections above are history; use the runbook's.

### Old/new parity

0.31.0 on the 0099 copy and the new binary on a copy it migrated on start, both with fs storage and an
unreachable sidecar; `panbotka` (maintainer) and `renzik` (viewer, the only account with a favourite) reset to a
known password.

- Logins: 200 on both sides for both.
- **Byte-identical** after `jq -S` and normalising `/api/v1/p/v/` → `/api/v1/`: `photos?limit=50&sort=newest`
  (total 20,421), `albums` (443), `subjects` (143), `labels` (137), `photos?q=year:2019` (123), `places`,
  renzik's `photos?favorite=true` (1) and that photo's detail.
- `/auth/me` differs only by the tenancy fields (`instance_role`, `projects[]`, `last_project_slug`); the
  account's `role`/`subject_uid` moved into the membership.
- Unprefixed routes on the new binary: `GET /api/v1/photos` → `404 {"error":"not found"}`; `POST /api/v1/mcp`
  → 404.
- **Session and token survival:** an API token minted on 0.31.0 gives 200 on `/api/v1/p/v/photos` after the
  upgrade; a 0.31.0 session cookie too.

### Rollback 0115 → 0099

The [`migrations-rollback/`](migrations-rollback/README.md) scripts, newest first, each `psql -1`:

- **On the migrated copy:** every script exit 0, 63–128 ms each; `max(version)` = 99; `inventory-diff A C`
  with **no** expectations: **0 differences**. 0.31.0's `migrate`: "database is up to date"; 0.31.0 `serve`:
  login 200 for both users, `photos` byte-identical to before the deploy, renzik's favourites total 1, 0
  `ERROR` log lines.
- **After the new server has run** (it queued `notification_purge` and created 2 sessions): the chain exits 0;
  0.31.0 then accepts a session created *by the new release* (200) and a fresh login (200).
- **A dump taken on 0115** restores with plain `pg_restore --single-transaction --no-owner --no-privileges` (no
  `sed`); the new `migrate` then reports "up to date" and `inventory-diff B R15` gives 0 differences. **A
  0099-era dump** restored into the new binary works with the `RESTORE.md` `sed` procedure + `migrate`.
- **On a 0099 copy (nothing applied)** the 0115/0114/0112/0110/0103/0102 down scripts fail (`0115_down:
  relation "storage_rekey_runs" does not exist`), the rest are no-ops. **Resolved:** the runbook and the
  README start the loop at `SELECT max(version) FROM schema_migrations`.

### Deploy order and the advisory lock

- Two new instances started at once on one 0099 copy: one logged 16 `migration applied` lines, the other 0;
  both served `/healthz` 200 — the migration lock works.
- **The new binary neither detects nor refuses a running 0.31.0 on its own start:** with 0.31.0 serving a
  copy, starting the new binary on it made 0.31.0's next login **500** while its `/healthz` stayed 200. The
  protection is the deploy order.
- **Production's deploy does stop the old container first:** upstream `kozaktomas/vps` deploys on push to
  `main` (Ansible → `docker compose up -d --remove-orphans` in `/root/apps/vps`); the service has no
  healthcheck, `restart: always`, `stop_grace_period: 30s`, Compose v5.6.0. On the box (Compose v5.1.1) a tag
  change + `up -d` gave `create (new)` → `kill/stop/die/destroy (old)` → `rename` → `start (new)`: the old
  container is dead before the new one starts. A failed pull aborts before anything is stopped.

### Storage move of `v` — estimate (not run)

- Pre-move checks on the copy: 0 rows with a bad size or hash, 0 originals over 5 GiB (largest 607 MB);
  21,151 photos, 78 GB, 0 already under `projects/`, 7 videos (7 renditions), 51 archived.
  `storage rekey v --dry-run`: `21151 photo(s), 78.4 GiB … under projects/pj0000000000000000veselice/`, `+9
  derived key(s)` per photo. `--status` before any run exits non-zero (`rekey: no storage move for this
  project`).
- **Duration: plan 6–15 h** (~22 R2 round-trips per photo, sequential; 35 min floor at the 10/s cap) — gate
  II's 3–9 h lower bound is not promised. **Cost under ~$2.** The **CDN Worker** MACs any path, so prefixed keys
  validate with no infra change — but it was never exercised end-to-end; the runbook now has a post-move fetch
  through the CDN.
- **Backups around it:** a DB dump alone cannot undo the move (the root objects are deleted); the rollback
  material is that dump **plus** the S3 noncurrent versions (90 days). The first R2 → S3 sync after the move
  re-copies ~78 GB through the Pi (the initial full sync took 10,617 s), fires `KukatkoBackupSlow` and doubles
  the backup bucket for 90 days (~$5–6 in total). All in the runbook now.

### Findings and what became of them

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| B1 | blocker (runbook) | `kukatko backup` cannot run on production: no `KUKATKO_BACKUP_*` config, no `pg_dump` in the image | Runbook rewritten to the backups that exist: an on-demand `docker exec pgvector pg_dump -Fc` right before the merge (with its restore command), the `backup` container's 8-hourly `.sql.gz` as fallback, the Pi's R2 → S3 sync by hand before the storage move |
| B2 | blocker (runbook) | Documented expect lists exit 1 on 0115 | One authoritative list for 0099 → newest in OPERATIONS.md, guarded by `make check` and an integration proof |
| B3 | blocker (process) | The owner's `/release` BLOCKs this chain by its own rules | Runbook's "Owner override for the tenancy release" lists each check that fires and why it is accepted; the vps PR body links the runbook |
| S1 | should-fix | Overlap guarded only by deploy order | Code change, separate task |
| S2 | should-fix | Step 5 did not say the vps merge is the deploy; dump window | Runbook: steps 1–4 before the merge, avoid the `backup` container's run, watch for 17 `migration applied` lines |
| S3 | should-fix | Rollback did not stop the new container or revert the tag | Runbook: stop → down chain → revert |
| S4 | should-fix | Down scripts fail on never-applied files | Start at `max(version)` (runbook + README) |
| S5 | should-fix | Inventory `docker run` hit `permission denied` (image runs as `nobody`) | Output directory created owned by 65534 |
| S6 | should-fix | Clients that break at the switch were unlisted | Listed: fotky (own Botka task `3a05aa5b`), the Pi's ctl contexts, MCP off, the reload in the announcement |
| S7/S8 | should-fix | Storage-move rollback misdescribed; the big first sync | Runbook's storage-move section rewritten |
| A3 (code review) | nice | `0106`/`0107`/`0108_down` cancelled only queued jobs | They now cancel running ones too (0.31.0's stale-lock recovery would hit the rebuilt index); covered by `TestRollbackChain_queuedJobsOfAGoneProject` |
| D5 (code review) | nice | `0115_down`'s comment claimed the older scan re-finds the rows' objects | Comment and README corrected |

**Cleanup:** the box container, the dump, every copy, the worktrees and the binaries were deleted; nothing on
production was written, migrated or restarted.
