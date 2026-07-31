# READINESS_AUDIT — is Kukátko complete enough to replace PhotoPrism?

Measured 2026-07-31 against the live production PhotoPrism (`fotky.kotrzina.cz`,
version `251130` Plus), the live photo-sorter feeds and the running Kukátko dev
instance on `:6480` (commit `ab3c7d5`).

Companion to [`MIGRATION_PLAN.md`](MIGRATION_PLAN.md), which is the *runbook*. This
document is the *measurement*: what works, what does not, and what must be fixed
before the cutover. It changes nothing — every entry is an observation.

**Verdict: NOT ready.** Two defects break the migration itself (§2), and the
point-of-no-return gate (a proven restore) has never been exercised (§4). Feature
parity is otherwise good and is not the blocker.

## Evidence rules

Every row is backed by one of: a `file:line`, an HTTP status from the running
instance, or a SQL count. "It is in the docs" is **not** evidence — documentation is
audited here, not trusted. Rows that could not be verified are marked
`UNVERIFIED` and say why.

## 1. Where the library actually is

| | PhotoPrism (source) | Kukátko (catalogue) | ratio |
| --- | ---: | ---: | ---: |
| photos | 20 670 | 280 | 1.4 % |
| files | 20 870 | 280 | — |
| albums | 164 (`type:album`) | 8 | 5 % |
| labels | 113 | 2 | 2 % |
| people (subjects) | 104 | 10 | 10 % |
| faces | 112 806 (sorter) | 1 298 | 1 % |
| embeddings | 20 092 (sorter) | 280 | 1.4 % |

Source counts: authenticated `GET /api/v1/config` → `count` (PhotoPrism) and
`GET /import/verify` → `vectors` (sorter feeds). Catalogue counts: `SELECT count(*)`
per table. `import_runs` = 4, all scoped test runs.

**Consequence for this audit:** anything that only becomes visible at scale —
thumbnail throughput, HNSW recall and `ef_search`, timeline paging, duplicate
detection, face clustering quality — is **unmeasurable today** and is marked so
below. It is not "passing"; it is untested.

## 2. Blockers — the migration cannot complete

### 2.1 Every photo-listing paging loop stops after the first page

`internal/photoprism/photoprism.go:126` sets `merged=true` on every photo listing so
each photo carries its `Files[]`. PhotoPrism then collapses multiple file rows into
one photo entry, so a page **always** comes back shorter than the requested count.
Measured on the production library (`count=1000`):

| request | rows returned |
| --- | ---: |
| `offset=0` (no `merged`) | 1000 |
| `offset=1000` (no `merged`) | 1000 |
| `offset=0&merged=true` | **914** |
| `offset=1000&merged=true` | **987** |
| `offset=2000&merged=true` | **996** |

Every loop over `ListPhotos` ends with `if len(page) < pageSize { return nil }`,
which therefore reads the **first** page as the end of the library — and returns
success.

| Site | What it drives | Effect |
| --- | --- | --- |
| `internal/ppimport/photos.go:69` | the full photo import | imports ~900 of 20 670 and reports done |
| `internal/ppimport/organize.go:182` | album membership | an album past one page loses the rest |
| `internal/ppimport/organize.go:318` | label membership | same |
| `internal/importverify/importverify.go:250` | source enumeration for the completeness check | reconciles against ~994 instead of 20 670 |

Loops over `ListAlbums` / `ListLabels` / `ListSubjects` (`organize.go:70,111,233,254`,
`people.go:199`) are **not** affected — those endpoints take no `merged` parameter and
return full pages.

The last row is what makes this severe: the tool that guards the point of no return
is blind in exactly the same way as the importer. After a full run it would compare
~900 imported photos against ~994 "source" photos and could report the library
**complete** while 19 700 photos are missing.

Observed today: `GET /import/verify` → `photoprism.source_total: 994`, against a
source holding 20 670.

**Fix direction:** terminate on `len(page) == 0`, not on a short page. A short page
is not evidence of exhaustion for any endpoint that filters or merges server-side.

### 2.2 The verifier expects albums the importer deliberately never imports

- Importer, `internal/ppimport/ppimport.go:89`: a full run maps four album types —
  `album, folder, moment, state`. `month` is excluded on purpose (560 auto-generated
  monthly albums that Kukátko's timeline already covers).
- Verifier, `internal/importverify/importverify.go:145`: defaults to **all five**
  types (`photoprism.AlbumTypes`).

So the completeness report permanently lists ~560 monthly albums as missing.
Observed: `structure.albums.source_count: 758`, `missing_count: 751`.

`MIGRATION_PLAN.md` phase 4 requires resolving "every listed missing item … until the
report is clean". With this mismatch that is unachievable by construction, and real
missing albums are buried in ~560 rows of noise.

**Fix direction:** the verifier should default to the importer's `DefaultAlbumTypes`,
or classify intentionally-skipped types separately from missing ones.

### 2.3 `missing_*_count: 0` reads as "done" when nothing is imported

`GET /import/verify` reports `vectors.missing_embeddings_count: 0` and
`missing_faces_count: 0` while the catalogue holds 50 embeddings against a source of
20 092. The counters are computed over photos **already in the catalogue**, which is
defensible (vectors cannot attach to absent photos) but reads as completeness. Only
the top-level `complete: false` says otherwise.

**Fix direction:** name or scope the field so it cannot be read as source coverage.

## 3. Feature parity with PhotoPrism

Rows are PhotoPrism's own feature areas, taken from the `settings.features` keys its
API serves — its own naming, not a list invented here. (The `true`/`false` values
that endpoint returns are ACL-masked for the token's role and say nothing about what
PhotoPrism supports; only the key set was used.)

Status: **OK** = equivalent exists · **PARTIAL** = exists, narrower · **MISSING** =
no equivalent · **OUT** = deliberately out of scope per `CLAUDE.md`.

| PP area | Kukátko equivalent | Status | Evidence |
| --- | --- | --- | --- |
| account | `/auth/me`, `/auth/password`, `/auth/tokens`, `AccountPage` | OK | probe 200 |
| albums | `/albums`, `AlbumsPage`, `AlbumDetailPage` | OK | probe 200 |
| archive | `/photos/{uid}/archive`, `TrashPage` | OK | probe 405 (POST route) |
| batchEdit | `/photos/bulk` (`internal/bulk`, one transaction) | OK | `bulkapi.go:91` |
| calendar | `/photos/timeline`, `/photos/years` (year/month scrubber) | PARTIAL | probe 200; no calendar view, monthly albums not imported by design |
| delete | `/photos/{uid}/purge`, `/trash/*` | OK | probe 405 |
| download | `/photos/{uid}` download URL, `/photos/download-zip` | OK | `photoapi/http.go:209` |
| edit | `/photos/{uid}/edit` (`internal/photoedit`, non-destructive) | OK | probe 405 |
| estimates | `internal/geoestimate` (refuses unless neighbours cluster tightly) | OK | package present |
| favorites | `/favorites`, `/photos/{uid}/favorite` — **per user**, PP is global | OK+ | probe 200 |
| files | `photo_files` table; no file/folder browser UI | PARTIAL | no `FilesPage` in `web/src/pages` |
| folders | imported as albums (`type: folder`) | PARTIAL | `ppimport.go:89`; no folder-tree UI |
| import | `/import/*` (maintainer-only) | OK | probe 200/405 |
| labels | `/labels`, `LabelsPage` | OK | probe 200 |
| library | `/maintenance/*`, `/process/*` | OK | probe 200/405 |
| logs | `/audit` (admin) + JSON logs to stderr | PARTIAL | probe 200; no log viewer UI |
| moments | imported as albums (`type: moment`) | PARTIAL | `ppimport.go:89`; not auto-generated |
| people | `/subjects` + clusters, candidates, outliers, sweep, review | OK+ | probe 200; richer than PP |
| places | `/places`, `/map/*`, `MapPage`, `PlacesPage` | OK | probe 200 |
| private | `photos.private`, `albums.private` columns; `private:` search filter | OK | `internal/query/query.go` |
| ratings | `/photos/{uid}/rating` — **per user** | OK+ | probe 405 |
| reactions | none | MISSING | no route, no column |
| review | `/review/*` — a face/label uncertainty game, **not** PP's quality-review queue | PARTIAL | different semantics; PP holds 583 photos in its review state |
| search | `/search`, `/search/global` + query language, semantic + hybrid | OK+ | probe 400 (needs `q`) |
| services | none (WebDAV / remote sync) | MISSING | no package |
| settings | config file + `/system/status`; no in-app settings UI | PARTIAL | no `SettingsPage` |
| share | none | OUT | `CLAUDE.md` — "share links are not a priority" |
| upload | `/upload` (`internal/ingest`) | OK | probe 405 |
| videos | `/photos/{uid}/video`, ffmpeg transcode, range requests | OK | `photoapi/http.go:233` |

**Parity summary:** 16 OK (5 of them richer than PhotoPrism), 8 PARTIAL, 2 MISSING,
1 deliberately OUT. **No PARTIAL or MISSING row is a cutover blocker** — `reactions`
and `services` are features the source library does not meaningfully use, and the
PARTIAL rows are narrower presentations of data that is present.

The one worth a decision is **review**: the word means different things in the two
apps. PhotoPrism has 583 photos in *its* review state (low quality / needs
attention); Kukátko's `/review` is a face-and-label confirmation game. Nothing
carries PhotoPrism's review flag over. UNVERIFIED whether that state matters to you.

## 4. Operational readiness

| Gate | State | Evidence |
| --- | --- | --- |
| S3 backup configured | **NO — waived, see below** | no `KUKATKO_BACKUP_*` in the environment |
| Restore rehearsed | **NO — waived, see below** | `GET /restore/dumps` → 503; plan phase 5 never run |
| Default admin password | **YES, still `admin12345` — waived** | `POST /auth/login` → 200 |
| Bind address | `0.0.0.0:6480`, reachable over the tailnet | `ss -ltnp`; log shows requests from `100.108.217.121` |
| Job queue drained | yes (0 pending) | `SELECT count(*) FROM jobs WHERE state IN ('pending','running')` |
| Embeddings sidecar | offline-tolerant by design; box usually off | `internal/reachability`, `/capabilities` |
| Performance at 20k | **UNMEASURABLE** | catalogue holds 280 photos (§1) |
| API drift | none — 102 of 102 documented endpoints exist | probe of every path in `docs/API.md` |

### Executing the full import — prerequisites found while sizing it

Measured against the production library (20 670 photos, 5.16 MB average from the 280
already imported → roughly 104 GB to move; 29 % of the sample carries GPS):

| Finding | Evidence | Effect on the import |
| --- | --- | --- |
| ~~No wipe tooling exists~~ — **resolved 2026-07-31** | `kukatko maintenance reset` (`internal/reset`), documented in [`OPERATIONS.md`](OPERATIONS.md#kukatko-maintenance-reset--the-guarded-library-wipe) | plan phase 1 now has a repeatable, guarded command: dry run by default, typed database name, target check, non-interactive refusal, storage deletion confined to Kukátko's own prefixes, audited in the truncation's transaction |
| Geocoding has a rate limit but no budget | `maps.geocode_rate_per_sec: 5` (`config.go:884`), deferral in `internal/placesjob` | ~6 000 reverse geocodes spend mapy.com credits with no cap and no visibility |
| One global worker slot | `KUKATKO_WORKER_COUNT=1`; `internal/worker` has a single shared concurrency (`worker.go:173,193,249`) | thumbnails, metadata, places and sidecars for 20 670 photos run one at a time on ARM, behind a limit that exists only to protect the GPU |

The last one is worth stating precisely: with the box off during the import — which is
the plan, since the vectors arrive 1:1 from the feeds — `image_embed` and
`face_detect` defer without touching the sidecar, so the constraint that justifies the
single slot does not bind. The CPU work is serialised for nothing.

**Phase order is not cosmetic.** The import enqueues an `image_embed` and a
`face_detect` job per photo (~41 000 for the full library). Both handlers are
idempotent and skip when a vector or a `face_detections` row already exists
(`embedjob.go:7`, `facejob.go:178`, and `vectors/faces.go:82` writes that row on the
feeds path too). So if phase 3 lands first the queue drains as cheap no-ops — but if
the box wakes between phases 2 and 3 it will recompute ~20 000 embeddings the feeds
would have supplied for free.

### Accepted risks (decided 2026-07-31)

Three items above were **waived by an explicit owner decision**, not left open. They
are recorded here so nothing later mistakes them for oversights.

1. **No S3 backup for Kukátko**, and **no restore rehearsal**. `MIGRATION_PLAN.md`
   phase 5 is dropped.
2. **The admin password stays at the bootstrap default**, on a maintainer account
   reachable from the tailnet.

The first has a consequence that turns a precaution into a **hard operating rule**:
with no backup of its own, Kukátko's only safety net is that PhotoPrism and
photo-sorter still hold the library. The rollback is therefore load-bearing.

- PhotoPrism and photo-sorter **stay intact and read-only** — they cannot be retired.
- `storagemigrate DeleteLocal` **stays off**.
- Trash `retention` **stays off**.

Anything that would delete an original from the source libraries requires revisiting
the backup decision first. Until then the "point of no return" is not a restore
rehearsal — it is the moment either source library is touched.

## 5. Incremental import — what a re-run catches

The design supports "flip the library, then keep pulling changes", and the mechanics
are sound:

- **Watermark** (`ppimport.go:538`): the cursor is the newest `UpdatedAt` among
  successfully processed photos, pulled back to the oldest failed one — so a failure
  is retried on the next run instead of being stepped over.
- **Incremental listing**: `q=updated:"<RFC3339>"` (`photoprism.go:134`).
- **Per changed photo the detail is fetched** (`details.go:73`), refreshing albums,
  labels and markers. Unchanged photos cost no request.
- **Vectors and faces** come through their own path (`internal/psfeedsimport`) with a
  keyset cursor and its own watermark, idempotent and without GPU recompute.
- **Album and label catalogues** are walked in full on every run, so renames and new
  albums are picked up.

What it does **not** do:

| Gap | Evidence | Consequence |
| --- | --- | --- |
| Deletions are never reconciled | the only `Delete` calls are rollbacks of half-created rows (`photos.go:249`, `siblings.go:231`) | a photo, album, label or face assignment deleted in PhotoPrism stays in Kukátko forever |
| Sync is one-way | no write path to the source | curation done in Kukátko is invisible to PhotoPrism |
| Marker-only changes may not be seen | UNVERIFIED — depends on whether PhotoPrism bumps a photo's `UpdatedAt` when only a marker/subject changes | if it does not, face (re)assignments made in PhotoPrism after the cutover never reach Kukátko |

The last row is cheap to settle: assign a face in PhotoPrism and check whether that
photo's `UpdatedAt` moves. It was not tested here because the audit's token is a
read-only viewer.

**Why the defect in §2.1 stayed hidden:** an incremental run with fewer than ~900
changed photos fits in a single page, so it terminates correctly by accident. Only a
full run is broken — which is why the four scoped test runs looked healthy.

**Decision this forces:** if PhotoPrism really goes read-only at the cutover, none of
the gaps above matter — the incremental path is only needed for the transition
window. If PhotoPrism keeps being used in parallel, the library has two masters, a
one-way sync and no deletion handling, and the catalogues will silently drift apart.
That is a decision to make, not a feature to build.

## 6. What this audit did not measure

Stated so the gaps are not mistaken for passes:

- **Anything at production scale** — see §1. Thumbnail throughput, HNSW recall,
  timeline paging, duplicate detection and clustering quality are all untested.
- **The frontend by clicking** — this pass verified the API surface and the data
  layer. No screen was exercised in a browser against real data.
- **`importverify.go:473`** — a second paging loop, over a non-photo listing, so the
  §2.1 defect probably does not apply. Not confirmed.
- **Whether PhotoPrism's review state (583 photos) matters** — see §3.

## 7. Order of work

1. Fix §2.1 (paging) — nothing else can be trusted until the importer and the
   verifier can see the whole library.
2. Fix §2.2 and §2.3 so the completeness report can reach a clean state.
3. Run `MIGRATION_PLAN.md` phases 1–4 on the full library.
4. Re-measure §1 and everything marked UNMEASURABLE in §4.

Steps 5 and 6 of an earlier draft — rehearsing backup/restore and rotating the admin
password — were waived; see *Accepted risks* in §4.
