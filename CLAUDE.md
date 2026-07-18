# CLAUDE.md — Kukátko

Project conventions and hard rules. **Read this and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
before any work.** These rules apply to every task.

This file holds **only rules and a signpost**. Descriptive details (packages, endpoints,
components, config keys) live in `docs/` and you read them only when you need them.

## What it is
Kukátko = a standalone photo/video management app, a replacement for PhotoPrism (combines
PhotoPrism + photo-sorter features, more robust). Full design: `docs/ARCHITECTURE.md`. Phase:
active development via autonomous tasks; PhotoPrism stays **primary** until cutover (import is
read-only, incremental).

## Tech stack (binding)
- **Backend: Go**, a single static binary, **`CGO_ENABLED=0`**. Module `github.com/panbotka/kukatko`.
  Router chi/v5, CLI Cobra, config Viper, DB `pgx`/`pgvector-go`.
- **DB: PostgreSQL + pgvector.** Embeddings are stored **directly in the DB** (`halfvec` + HNSW cosine).
- **Frontend: React + TypeScript + Vite + react-bootstrap + Bootswatch Superhero**, embedded into
  the binary via `//go:embed` (SPA fallback). Icons **only `bootstrap-icons`** via the `Icon`
  component (one set, decorative `aria-hidden`). i18n via i18next: **Czech default**, English.
  Virtualize long grids/lists via **`react-virtuoso`**. Map view via
  **`leaflet`** + **`leaflet.markercluster`** (tiles via a backend proxy, the key stays server-side).
- **Images/videos without CGO:** pure-Go for JPEG/PNG/WebP; **shell-out** to `heif-convert` (HEIC),
  `exiftool`/`dcraw` (RAW preview), `ffmpeg`/`ffprobe` (video poster/metadata/streaming).

## Where to find what
Open **one** document based on what you're touching. Don't read them all preemptively.

| I'm touching… | I read |
| --- | --- |
| A Go package (`internal/*`, `cmd/*`) | [`docs/PACKAGES.md`](docs/PACKAGES.md) |
| An HTTP endpoint under `/api/v1` | [`docs/API.md`](docs/API.md) |
| The MCP server — tools, auth model, what is deliberately not exposed | [`docs/MCP.md`](docs/MCP.md) |
| Frontend (`web/`) — component, hook, page, service | [`docs/FRONTEND.md`](docs/FRONTEND.md) |
| A CLI command, config key, `make` target, CI/packaging | [`docs/OPERATIONS.md`](docs/OPERATIONS.md) |
| Architecture, data model, milestones, testing strategy | [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) |
| Local development, frontend build, embed | [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) |
| Performance (thumbnails, vips, HNSW `ef_search`, indexes) | [`docs/PERF.md`](docs/PERF.md) |
| Restore from backup / disaster recovery | [`docs/RESTORE.md`](docs/RESTORE.md) |
| UX decisions and audit | [`docs/UX_AUDIT.md`](docs/UX_AUDIT.md) |
| The import field mapping (PhotoPrism + photo-sorter) — what migrates, what's dropped | [`docs/MIGRATION_AUDIT.md`](docs/MIGRATION_AUDIT.md) |
| Security audit — findings, severities, attack scenarios | [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md) |

## Package map
One line per package — so you know what exists without opening `docs/PACKAGES.md`.

- `cmd/kukatko` — thin Cobra entrypoint (`serve`/`migrate`/`import`/`backup`/`restore`/`maintenance`/`storage`/`ctl`/`version`) + `buildXxxAPI` wiring
- `web/` — Vite + React 19 + TS frontend, builds into `internal/web/static/dist`
- `internal/audit` — durable audit trail; `Write(ctx, exec, Entry)` runs **in the same transaction** as the mutation
- `internal/auditapi` — admin-only `GET /audit` (read-only listing)
- `internal/auth` — viewer/editor/admin/maintainer roles (strict ladder), bcrypt, sliding sessions, RBAC middleware, API tokens (Bearer)
- `internal/backup` — S3 backup (pg_dump + sync of originals + retention) **and** restore
- `internal/backupapi` — admin-only `GET`/`POST /backup`
- `internal/bulk` — bulk metadata editing, the whole batch in one transaction
- `internal/bulkapi` — `POST /photos/bulk`
- `internal/candidates` — "find a person among untagged photos": per-exemplar kNN over unassigned faces + voting, rejection/negative-exemplar/size filters, action classification; read-only
- `internal/candidatesapi` — `POST /subjects/{uid}/candidates` (RequireWrite)
- `internal/cluster` — auto-clustering of unassigned faces (union-find over HNSW neighbors)
- `internal/clusterapi` — `/faces/clusters` (list, assign, remove-face)
- `internal/config` — typed configuration, Viper, `Load()`
- `internal/ctl` — **client** of the own API for `kukatko ctl`: contexts (kubectl-style), Bearer token, table/JSON output
- `internal/database` — pgxpool wrapper, embedded migration runner, pgvector types
- `internal/dirimport` — `kukatko import dir`: projde adresář na disku a nahraje média přes `internal/ingest`
- `internal/duplicates` — near-dup groups (pHash banded-LSH + embedding HNSW, union-find); read-only
- `internal/duplicatesapi` — `GET /duplicates`, `POST /duplicates/merge`
- `internal/dupmerge` — transactional resolve of a dup group: union albums/labels/people onto the keeper, fill gaps, archive copies
- `internal/embedding` — HTTP client of the inference sidecar on the box; offline-aware typed errors
- `internal/embedjob` — worker handler `image_embed` + backfill
- `internal/exif` — EXIF/GPS extraction (exiftool, pure-Go fallback)
- `internal/expand` — "expand a collection": photos similar to an album/label (per-photo kNN + voting, exclude members, vote rule, label rejections/negative-exemplar); read-only, **never writes**
- `internal/expandapi` — `GET /albums/{uid}/similar`, `GET /labels/{uid}/similar` (RequireWrite)
- `internal/facejob` — worker handler `face_detect` + backfill
- `internal/facematch` — face↔marker IoU matching, identity suggestions, assignment state machine
- `internal/feedback` — persisted opinions: "not this person" / "not this label" / "not duplicates", idempotent, audited, never mutates; bulk exclusion lookups
- `internal/feedbackapi` — `POST`/`DELETE /feedback/{face,label}-rejections` (RequireWrite)
- `internal/geoestimate` — estimate a missing location from photos taken near it in time; refuses unless the neighbours cluster tightly (a wrong location is worse than none), marks every result `estimate`
- `internal/globalsearchapi` — `GET /search/global` (grouped cross-entity)
- `internal/imgconvert` — HEIC/RAW/video → decodable JPEG (shell-out)
- `internal/importapi` — admin-only import triggers + run history
- `internal/importer` — bookkeeping of import/migration runs + high-watermarks
- `internal/ingest` — upload pipeline: stream, SHA256 dedup, metadata, thumbnails, enqueue jobs
- `internal/jobs` — persistent job queue in Postgres (retry, dedup, backoff, `Defer`)
- `internal/jobsapi` — admin-only `/jobs` (stats, list, requeue)
- `internal/maintenance` — library integrity check & repair; **never deletes originals**
- `internal/maintenanceapi` — admin-only `/maintenance` (scan, repair)
- `internal/mapsapi` — tile proxy, geocode (reverse + place search), GeoJSON feed
- `internal/mapy` — server-side mapy.com client; **the key never leaves the server**
- `internal/mcpapi` — MCP server for an AI agent, `POST /mcp` (RequireAuth + per-tool RBAC); off by default, nothing destructive exposed
- `internal/mediaurl` — stamps `thumb_url`/`download_url` into payloads; signed URL, or an own route
- `internal/metajob` — worker handler `metadata` + backfill: re-reads an original into the IPTC/XMP and file-technical columns; gap-filler only
- `internal/metrics` — Prometheus registry + collectors (DB pool, queue depth)
- `internal/obs` — structured logging (JSON slog to stderr)
- `internal/organize` — albums, labels, **per-user** favorites and ratings
- `internal/organizeapi` — `/albums`, `/labels`
- `internal/outlierapi` — `GET /subjects/{uid}/outliers`
- `internal/outliers` — per-person outlier detection of faces (distance from centroid)
- `internal/people` — subjects (people/animals/other) and markers; keeps the `faces` cache consistent
- `internal/peopleapi` — `/subjects` + a subject's photo gallery
- `internal/phash` — perceptual hashes (pHash via DCT, dHash gradient)
- `internal/photoapi` — read/curation API over the catalog: list, search, media, edit, faces, rating
- `internal/photoedit` — applies non-destructive edits (crop/rotate/brightness/contrast), pure-Go
- `internal/photoprism` — read-only HTTP client of a running PhotoPrism
- `internal/photos` — **the photo-catalog core**, `Store` over pgx; dedup on SHA256 `file_hash`
- `internal/photosorter` — read-only client of the photo-sorter PostgreSQL DB
- `internal/places` — cache of reverse-geocoded places (side table `photo_places`)
- `internal/placesapi` — `GET /places` (hierarchy of countries → cities with counts)
- `internal/placesjob` — worker handler `places` (reverse geocode, rate-limited due to credits)
- `internal/ppimport` — incremental **idempotent** import from PhotoPrism
- `internal/processapi` — admin-only `/process/*` backfills (embeddings, faces, clusters, places)
- `internal/psimport` — incremental **idempotent** direct migration from photo-sorter
- `internal/query` — pure parser of the search query language (`q=`): free text + key:value filters → AST; unknown tokens degrade to free text; compiled to SQL in `internal/photos`
- `internal/ratelimit` — per-key token-bucket limiter + HTTP middleware
- `internal/restoreapi` — admin-only **read-only** `/restore/*` (destructive restore only via CLI)
- `internal/review` — the review game: one-question-at-a-time queue of face/label candidates from the uncertainty band; answers reuse existing write paths
- `internal/reviewapi` — `GET /review/queue`, `POST /review/answer` (RequireWrite)
- `internal/savedsearch` — per-user saved searches ("smart albums")
- `internal/savedsearchapi` — `/saved-searches`, everything scoped to the owner (foreign → 404)
- `internal/server` — chi HTTP server, graceful shutdown, `New(addr, WithAPI(...))`
- `internal/sidecar` — čte metadata vedle média (Google Takeout `.json`, Apple `.xmp`), páruje je se soubory a řeší precedenci vůči EXIF
- `internal/sidecarexport` — **writes** the metadata sidecar: the versioned YAML format + its atomic write to storage, so the catalogue survives losing the DB. Not `internal/sidecar` (that reads foreign ones)
- `internal/sidecarjob` — worker handler `sidecar` + backfill: rewrites a photo's sidecar whenever its metadata/curation changes; idempotent, debounced by the queue's dedup
- `internal/stacks` — group RAW+JPEG / edited variants of one shot into a stack (detection rules + manual stack/unstack/set-primary); **grouping, never merging**
- `internal/storage` — storage of originals (`YYYY/MM`, SHA256): local `FS` or Cloudflare `R2` with signed URLs
- `internal/storagemigrate` — resumable move of the library to object store; verify → commit the row → only then delete the original
- `internal/sweep` — recognition sweep: runs the per-subject candidate search across **all** named subjects at a high confidence, bounded worker pool, streams a per-person work list; read-only, **never auto-assigns**
- `internal/sweepapi` — `GET /faces/sweep` (RequireWrite) streaming NDJSON
- `internal/system` — aggregation of instance operational state for the admin dashboard
- `internal/systemapi` — admin-only `GET /system/status`
- `internal/thumb` — thumbnailer (pure-Go default, optional `vips` engine), cache layout
- `internal/thumbjob` — worker handler `thumbnail` (thumbnail regeneration + pHashes)
- `internal/trash` — permanent deletion (purge) of archived photos + scheduled retention
- `internal/vectors` — embeddings and faces directly in Postgres (`halfvec` + HNSW cosine)
- `internal/version` — ldflags-injectable `Version`/`Commit`
- `internal/video` — shell-out to ffprobe/ffmpeg: metadata, poster frame, on-the-fly transcode
- `internal/wake` — optional Wake-on-LAN auto-wake of the box (**default off**, fully inert)
- `internal/web` — SPA fallback handler + `//go:embed` embedded frontend
- `internal/worker` — in-process worker runtime over the job queue (claim/dispatch/complete)

## Hard quality gate (DO NOT SKIP)
- **`make check` MUST pass.** It is the project's verification command — red lint/tests = the task
  ends as `needs_review`. **`check` never changes files** (it only verifies formatting;
  `make fmt` applies it), so after a successful run `git status --short` is empty.
  The race detector lives in `make test-race` (runs in CI), not in the gate.
- **`CLAUDE.md` holds only rules and a signpost.** Descriptive details belong in `docs/`.
  The 300-line limit is enforced by `make docs-budget`. Don't circumvent it — move text to the right document.
- For Go code **use the `golang-developer` skill**.
- **`.golangci.yml` is strict** (inherited from photo-sorter). Don't weaken it. `//nolint` only
  with justification.
- **Tests are mandatory for every change:** unit tests for logic; **integration tests** for DB/HTTP
  against a real test DB. New behavior = new/updated tests. Goal: an extensible app that the next
  iteration won't break. Details in `docs/ARCHITECTURE.md` §19.
- Frontend: **ESLint** (strict) + **Prettier** (`--check`) + **Vitest** must pass (wired into
  `make`). No `any` without a reason.

## Configuration
- **`internal/config`** (`config.Load(path)`): YAML + env override via Viper, **env always
  wins**. Path: `--config` flag → `KUKATKO_CONFIG` env → default `config.yaml`. The file is
  optional (missing = defaults + env only). Required: `database.url`.
- Env: prefix `KUKATKO_`, dot → underscore (`database.url` → `KUKATKO_DATABASE_URL`,
  `backup.s3.bucket` → `KUKATKO_BACKUP_S3_BUCKET`). Exception: `maps.mapy_api_key` ↔ `MAPY_API_KEY`.
- **`config.example.yaml`** documents all keys + defaults; it is committed. The real config
  (`config.yaml`/`config.local.yaml`) and secrets are **not committed**. Add new config keys to
  the `Config` struct, `setDefaults`, `config.example.yaml`, the tests **and `docs/OPERATIONS.md`** at once.
- The catalog of all keys (`thumb.*`, `video.*`, `embedding.wake.*`, `ratelimit.*`, `maps.*`, `log.*`,
  `metrics.*`) is in [`docs/OPERATIONS.md`](docs/OPERATIONS.md).

## Database
- The DB is **already provisioned** in shared Postgres (pgvector 0.8.1 + unaccent).
- Read the DSN from the gitignored **`.secrets/db.env`**: `KUKATKO_DATABASE_URL` (app),
  `KUKATKO_TEST_DATABASE_URL` (integration tests, DB `kukatko_test`, safe to truncate).
  `MAPY_API_KEY` is there too.
- **Never commit secrets.** `.secrets/`, `*.local.yaml`, `.env*` are gitignored.
- Migrations = SQL in `embed.FS` (`internal/database/migrations/NNNN_name.sql`), auto-applied at
  startup in ascending version order, each in its own transaction, idempotently recorded in the
  `schema_migrations` table. Names like `0001_init.sql`. FKs with `ON DELETE CASCADE`/`SET NULL`
  (no orphans).

## Key patterns
- **The embeddings sidecar is NOT built.** Kukátko calls the existing service on the **box** (same
  models as photo-sorter → 1:1 migration) at a configurable `embedding.url`. **The box is often
  offline** → jobs (`image_embed`, `face_detect`) wait in a **persistent queue** in Postgres, upload
  and browsing work without it. External dependencies (sidecar, PhotoPrism API, mapy.com, S3) always
  behind an interface → fake/mock in tests.
- **"Back always works":** view state (filters/sorting/search/page) lives in **URL query params**
  + History API.
- **Import/migration:** store external IDs (`photoprism_uid`, `photoprism_file_hash`,
  `photosorter_uid`). The PhotoPrism file hash is SHA1, Kukátko uses SHA256.
- **Per-user favorites** (not global). **Keep the mapy.com key server-side** (backend proxy).
- Stream large files (upload/download/video) — don't hold them entirely in RAM.

## Definition of Done — at the end of EVERY task
**A task is NOT done until it is committed and pushed.** Completing a task always includes a
commit — never leave uncommitted changes in the working tree, nor "finished" work without a
commit. Always, at the end of every task, in this order:

1. **Write the change into the right document.** Docs must not go stale. Routing:
   - new/changed Go package → `docs/PACKAGES.md` (+ one line into `## Package map` above)
   - new/changed HTTP endpoint → `docs/API.md`
   - new/changed frontend component, hook, page, service → `docs/FRONTEND.md`
   - new config key → `docs/OPERATIONS.md` **and** `config.example.yaml`
   - new CLI subcommand or `make` target → `docs/OPERATIONS.md`
   - large architectural change → `docs/ARCHITECTURE.md`
   - user-visible feature → `README.md`
   - **Touch `CLAUDE.md` only when a _rule_ changed or a package was added/removed.**
     Never write descriptive details into it — that's what `docs/` is for and `make docs-budget` guards it.
2. **`make check`** must pass (docs-budget + fmt-check + lint + typecheck + tests + frontend).
3. **`make dev`** (= `./scripts/dev.sh`) must pass — the dev server starts and answers on
   `/healthz`. It catches what `make check` inherently can't see: a missing migration, broken wiring
   in `cmd/kukatko`, a panic on startup. A failed start (exit 1) = **do not commit**. Details
   in `docs/DEVELOPMENT.md`.
4. **Commit** (in English, concise) and **push** — only this step actually ends the task, see the
   rule above. End the commit message with the line:
   `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`

## Out of scope
- **Photo book** (not carried over from photo-sorter).
- Public sharing / share links are not a priority.

## Language
Code, comments, commits, identifiers **in English**. UI texts via i18n (cs default, en).
