# Multi-tenancy: several projects in one Kukátko instance

Date: 2026-10-11. Status: approved design, implementation split into Botka tasks.

## 1. Goal

One Kukátko instance hosts several independent **projects** (tenants). Each project is a whole
library of its own: photos, people, albums, labels, tasks, settings. Projects are for **foreign
tenants**: they must never learn anything about each other (names of users, photos, counts).

Non-negotiable: **everything that exists today becomes project #1 with no data loss and no
user-visible change** apart from the URL prefix. The instance is in production; every task
below must be deployable on its own with the live data intact.

Decisions taken with the owner (2026-10-11):

| Topic | Decision |
| --- | --- |
| Purpose | Foreign tenants (SaaS-like isolation) |
| Accounts | One global account per person; membership per project |
| Current project | In the URL: `/p/<slug>/…` (frontend) and `/api/v1/p/<slug>/…` (API) |
| Who adds members | System admin anywhere; a project admin inside their project |
| Instance role | Today's `maintainer` is the system admin; project roles are viewer/curator/editor/admin |
| System admin and content | Implicitly admin in every project (even without membership; audited) |
| Storage | One bucket; per-project key prefix; project #1 starts at the root and is moved later |
| First project | name **Veselice**, slug **`v`** |
| DB isolation | Explicit `project_id` column + predicate in every query (not schemas, not RLS) |
| Inviting members | Invitation links (role + expiry) and exact username/e-mail match; no directory autocomplete |
| Project lifecycle | Create, edit, deactivate in the UI; irreversible purge only via CLI (`--yes`, dry run first) |
| Storage move | Resumable per-photo copy → verify → commit row → delete old; progress visible in the app |

## 2. Data model

### 2.1 `projects`

| Column | Notes |
| --- | --- |
| `uid` | `pj` + random, like every other uid |
| `slug` | `[a-z0-9][a-z0-9-]{0,39}`, UNIQUE, **immutable** (it is the URL) |
| `name`, `description` | editable; `description` may be empty |
| `active` | `false` = nobody can enter; data stays |
| `storage_prefix` | text; `""` for project #1 until the move, `projects/<uid>/` for new projects and after the move |
| `max_file_bytes` | NULL = no limit; largest single original accepted |
| `max_total_bytes` | NULL = no limit; cap on `sum(photos.file_size)` of the project |
| `created_at`, `updated_at` | |

The bootstrap migration inserts `v` / "Veselice" / `storage_prefix = ''` / no limits / active with
the fixed uid **`pj0000000000000000veselice`** (`project.BootstrapUID`); every `project_id` column
carries that value as a transitional `DEFAULT` until the alias-removal step drops it.

### 2.2 `project_members`

PK `(project_uid, user_uid)`, both FKs `ON DELETE CASCADE`.

| Column | Notes |
| --- | --- |
| `role` | `viewer` / `curator` / `editor` / `admin` (strict ladder, as today minus maintainer) |
| `approved_at` | NULL = registered but not yet approved by a project admin (moves here from `users.approved_at`) |
| `subject_uid` | "this is me" in **this** project (moves here from `users.subject_uid`); FK to `subjects` `ON DELETE SET NULL` |
| `last_seen_at`, `visit_reference_at` | "what's new since your last visit", per project (move here from `users`) |
| `task_digest_at` | task digest watermark, per project (moves here from `users`) |
| `created_at` | |

Backfill: every existing user becomes a member of `v` with their current `users.role`; a
`maintainer` becomes a project `admin`. `approved_at`, `subject_uid` and the three timestamps are
copied from `users`. The `users` columns are dropped only in the task that finishes the move of
their readers (never in the same migration that adds the new ones, so a rollback stays possible).

### 2.3 `users.role` becomes the instance role

Allowed values after the change: `user` and `maintainer`. The migration maps every non-maintainer
to `user`. `maintainer` = **system admin**: instance operations (backup, jobs, maintenance,
import, system status, restore) **plus** project management and membership everywhere, and an
implicit `admin` role inside every project. Nothing is renamed in the database or the API; the
word "system admin" is UI copy only.

### 2.4 `project_id` on root tables

`project_id TEXT NOT NULL REFERENCES projects(uid) ON DELETE CASCADE`, backfilled to `v`, indexed
(usually as the leading column of the existing listing indexes):

- `photos`, `subjects`, `albums`, `labels`, `face_clusters`, `subject_families`, `photo_tasks`,
  `upload_links`, `import_runs`, `saved_searches`, `notifications`, `instance_settings`,
  `announcements`
- `search_history`: the PK becomes `(user_uid, project_id, query)`
- `embeddings` and `faces`: **denormalised** copy (FK to projects, kept in sync by the writer),
  so the filtered HNSW kNN queries can filter without a join
- `audit_log.project_id` nullable (instance-level actions have none)

Child tables (`photo_files`, `markers`, `album_photos`, `comments`, `photo_task_photos`,
rejections, …) are scoped through their FK to a root row and get no column.

UNIQUE constraints rewritten:

| Table | Before | After |
| --- | --- | --- |
| `photos` | `file_hash` | `(project_id, file_hash)` |
| `subjects`, `albums`, `labels` | `slug` | `(project_id, slug)` |
| `photoprism_aliases` | PK `photoprism_uid` | PK `(project_id, photoprism_uid)` (column added, backfilled) |
| `instance_settings`, `announcements` | one-row `id BOOLEAN` | one row **per project** (`project_id` PK) |

`import_runs`' latest-watermark index becomes per `(project_id, source)`.

### 2.5 Jobs

Per-photo jobs (`thumbnail`, `image_embed`, `face_detect`, `metadata`, `ocr`, `sidecar`,
`places`, `storyboard`, `hls_transcode`) keep their payload; the handler reads the project from
the photo. Library-wide jobs carry `project_uid` in the payload **and** in their dedup key:
`face_cluster`, `family_export`, `nameless_detach`/`nameless_restore`, `task_digest`,
`notification_purge`. `tag_notify` stays keyed per user but groups by project inside. The new
`storage_rekey` job is keyed per project.

### 2.6 Reset, backup, restore

Backup and restore stay instance-wide (one `pg_dump`, one bucket). `internal/reset` gains the new
tables in its classification (`projects` and `project_members` are **account** tables and are
preserved; everything with `project_id` is catalogue). `kukatko project purge <slug>` is the
per-project wipe: `DELETE … WHERE project_id` over the catalogue tables in dependency order, then
the object-store prefix (refuses an empty prefix), dry run by default, typed slug to confirm.

## 3. Auth and routing

### 3.1 Principal

`authenticateRequest` resolves the account as today (API token first, then the session cookie).
A project-scoped route then resolves the project from the `{project}` URL parameter (slug):

- unknown slug, or `active = false` → 404 for everyone except a maintainer, who gets 404 only
  for unknown slugs;
- membership row → its role; `approved_at IS NULL` → 403 `pending_approval`;
- no membership → 404 (a tenant must not learn the project exists), except a maintainer, who is
  treated as `admin` and the audit entry records `actor_role = maintainer`.

`auth.Principal` gains `Project` (uid, slug, name, storage prefix, limits) and `ProjectRole`.
`RequireAuth`/`RequireCurator`/`RequireWrite`/`RequireAdmin` check `ProjectRole` when the route
is project-scoped; `RequireMaintainer` and `RequireImport` check `users.role`. The helpers
`auth.ProjectFromContext(ctx)` and `ProjectUIDFromContext(ctx)` are **the only way** a store
learns the project; a store method that touches a root table takes `projectUID string` as its
first argument after `ctx` and never defaults it.

API tokens keep belonging to a user and carry no project: a token works in every project its owner
is a member of. The session stores nothing about projects; the "last used project" lives in
`users.last_project_uid` (nullable, FK `SET NULL`) and is updated on every project-scoped
request at most once a minute.

### 3.2 Routes

Project-scoped (everything that reads or writes catalogue data) moves under
`/api/v1/p/{project}/…`: photos, search, albums, labels, subjects/people, families, faces,
clusters, review, sweep, candidates, outliers, duplicates, duplicate-markers, expand, bulk,
trash, stacks, tasks, comments, feedback, saved-searches, search-history, whats-new, places,
maps (tiles/geocode/geojson), upload, upload-links management, notifications inbox, capabilities,
settings, announcement, users directory (`/users` = members), import bookkeeping, process
backfills, **mcp**.

Instance-wide, unchanged paths: `/auth/*`, `/users/{uid}/avatar`, `/push/*`, `/jobs`, `/system`,
`/backup`, `/maintenance`, `/restore`, `/audit` (gains a `project` filter), `/metrics`,
`/healthz`, the public `/u/{code}` (the link resolves its own project) and the new `/projects`.

**Transition aliases.** Until the frontend has moved, every old unprefixed project route stays
mounted and resolves to project `v` (hard-coded bootstrap slug read from the `projects` table,
not from config). The final task removes the aliases. Each intermediate task therefore leaves a
working, deployable instance.

### 3.3 Frontend

- Routes: everything project-scoped lives under `/p/:slug/…`. Old paths redirect client-side to
  `/p/<last project>/…` (bootstrap `v` when the user has only one project).
- `ProjectProvider` + `useProject()` directly under `AuthProvider`: current project (from the
  URL), the user's memberships (`GET /auth/me` gains `projects: [{uid, slug, name, role,
  approved}]` and `last_project_slug`), `switchProject(slug)`. `TaskSummaryProvider` and
  `NotificationInboxProvider` refetch on project change.
- One `apiPath(path)` helper (services/api.ts) replaces the 38 local `API_BASE` constants; it
  prefixes `/api/v1/p/<slug>` for project routes and `/api/v1` for instance routes.
- `/` → the last used project, else the only project, else `/projects` (the chooser: name,
  description, role; a deactivated project is listed greyed with "unavailable"). A user with no
  membership sees an empty chooser with "ask the administrator of a project for an invitation".
- Project switcher in the header (name of the current project; a menu when the user has more
  than one). The switcher keeps the current route when the destination project has it.
- Role predicates in `useAuth()` (`canCurate`, `canWrite`, `isAdmin`) read the **project** role;
  `isMaintainer` reads the instance role.
- The PWA `start_url` stays `/`.

### 3.4 ctl and MCP

`kukatko ctl` contexts gain `project` (slug); `--project` overrides it; a project-scoped command
without a project fails with a clear message. MCP is served at `/api/v1/p/{project}/mcp`; its
tools are unchanged and scoped by the URL.

## 4. Membership, registration, settings

### 4.1 Project admin: members page (`/p/<slug>/members`)

Replaces `/users` inside a project. Members with role, approval state, last login; change role,
approve, remove (not oneself, not the last admin). Add an existing account by **exact**
username or e-mail (no autocomplete; a miss and a hit look identical to the caller: "if that
account exists, it has been added" — the response is the same, the audit says what happened).
Invitation links: role, expiry (default 7 days, max 90), optional max uses, revoke; the link
`/invite/<code>` joins a signed-in user (role capped at the link's role) or opens registration
for an anonymous one, which creates the account **and** the approved membership.

### 4.2 Registration is per project

`instance_settings` becomes per-project (`registration_enabled`, `registration_secret`,
`welcome_markdown`). The registration page is `/p/<slug>/register` (or `/invite/<code>`,
`/u/<code>`); an account created there is a member of that project. Approval is on the
membership. The announcement banner is per project. Upload links file into their own project
and the registration-through-upload-link flow joins that project.

### 4.3 System admin: projects page (`/system/projects`, `/api/v1/projects`)

List (name, slug, active, members, photos, originals bytes, limits), create (slug, name,
description, limits), edit (name, description, limits, active), deactivate/reactivate, open the
members page of any project, add oneself as a member, start/pause the storage move and watch its
progress (§5.3). `/system/users` lists every account of the instance with its instance role and
its memberships. All of it `RequireMaintainer`, all mutations audited with `project_id`.

## 5. Limits and storage

### 5.1 Limits

- Effective per-file cap = the stricter of `projects.max_file_bytes` and the instance
  `upload.max_file_size_mb`; it applies to the upload endpoint, upload links and `import dir`.
- `max_total_bytes` counts **originals only** (`sum(photos.file_size)` of non-purged photos). An
  upload that would cross it is refused with 413 and a translated message naming the quota; nothing
  existing is deleted. Usage (bytes, photos) is shown on the project's settings page (admin) and on
  the system projects page.
- Usage is computed by a query, not cached, until it is measured to be too slow.

### 5.2 Storage prefix

Every key of a project is `projects.storage_prefix + <today's key>`: originals `YYYY/MM/<name>`,
`sidecars/…`, `thumb/…`, `hls/…`, `families.yaml`, `storyboard/…`, `avatar/…` (the last two in
the local cache). The layout of a photo's derived files is **the layout of its own original**:
the prefix is taken from `photos.file_path`, never from the project row, so a half-moved project
is consistent per photo. `storekeys.Classify` accepts both the root layout and the
`projects/<uid>/` layout; maintenance scan, trash purge, backup classification, sidecar rebuild
and reset work inside the project's prefix (an empty prefix means the root layout).

### 5.3 Storage move (`storage_rekey`)

Moves one project from an empty prefix to `projects/<uid>/`. Required for `v`; new projects never
need it.

1. Start: set `projects.storage_prefix = 'projects/<uid>/'`, so **new uploads land in the new
   layout immediately**; existing rows keep their bare keys. Insert a `storage_rekey_runs` row
   (project, state `running`/`paused`/`done`/`failed`, totals, done counts, bytes, last key,
   error list, started/updated at).
2. Per photo, in `file_path` order, idempotently: server-side copy of the original to the new key
   → verify the copy (size, and ETag or SHA-256 against `photos.file_hash`) → update
   `photos.file_path` (and `photo_files`) in one transaction → copy that photo's derived objects
   (thumbnails, HLS segments) under the new prefix, skipping missing ones → delete the old
   original and the old derived objects. **The old object is deleted only after the row points at
   a verified new copy.** A crash anywhere leaves either both copies or the new one; the next run
   re-verifies and continues.
3. After the photos: sidecars (rewrite via the sidecar job rather than copy) and `families.yaml`
   (re-export).
4. Done: the run flips to `done`; a maintenance scan lists any bare-layout leftovers as orphans.

Progress is visible at `/system/projects/<slug>` (processed/total photos, bytes, rate, ETA,
error count with the last errors, pause/resume/retry) and from `kukatko storage rekey <slug>`
(`--dry-run` lists what would move). The job runs in the worker with one slot, rate-limited
by a config key `storage.rekey.per_second` (default 10 photos/s).

## 6. Cross-cutting

- **Mail and push**: deep links carry `/p/<slug>/`; mail subjects/bodies name the project; the
  task digest groups by project in one mail per user.
- **Metrics**: `kukatko_library_*` gain a `project` label (slug). `/metrics` stays unauthenticated,
  so the label is the slug, never the name.
- **Audit**: `project_id` on every project-scoped entry; `GET /audit?project=` filter for the
  maintainer; a project admin's `/p/<slug>/audit` shows only that project.
- **Isolation tests**: `internal/projecttest` seeds two projects with a member each and a
  photo/subject/album in both, and offers `AssertInvisible(t, …)` helpers. Every task that scopes
  a package adds at least one test that project B's rows are invisible from project A through the
  store **and** the HTTP handler.
- **Guard in `make check`**: a Go test scans every non-test `.go` file under `internal/` and
  `cmd/` for SQL literals that reference a root table (`FROM|JOIN|UPDATE|INTO|DELETE FROM
  <table>`) and fails if the statement has no `project_id` (allow-list per file:line with a
  reason, e.g. maintenance scan over all projects).

## 7. Order of work

1. **Schema and model** (deployable, no UI change): `projects` + bootstrap, `project_members` +
   backfill, `project_id` columns + backfill + rewritten UNIQUEs, `internal/project`, the
   principal with a project role, transition aliases.
2. **Routing**: API prefix; frontend prefix, `ProjectProvider`, `apiPath`, chooser, switcher,
   redirects.
3. **Scoping the stores**, one package group per task, each with isolation tests.
4. **Features**: project management, members and invitations, per-project settings and
   registration, limits, storage prefix, the rekey job and its progress page, ctl/MCP, mail/push,
   metrics/audit, the guard test, removal of the aliases, docs.

The programme is split into 40 Botka tasks ("Multi-tenancy NN/40", priorities 1000 → 610). Two of
them are **safety gates** that add no feature: 05 (after the schema steps) and 40 (after everything)
rehearse the migration from the deployed tag to `main` on a copy of production data, build and use
`kukatko maintenance inventory` / `inventory-diff`, and write the verdict into
`docs/MIGRATION_SAFETY.md`. The owner re-queues a gate before any release that ships further
tenancy migrations.

## 8. Out of scope

Subdomain per project, per-project buckets, billing, project deletion from the UI, moving a
photo between projects, cross-project search.
