# Performance notes (M7 perf pass)

A focused performance pass targeting the Raspberry Pi runtime and large
libraries. It measures and tunes the four hot subsystems — vector search,
thumbnail generation, list/search/album queries, and the frontend grid — without
changing any external behaviour or API contract. See `docs/ARCHITECTURE.md`
§4 (storage/derivatives), §6 (vectors), §16 (risks).

## Test host

All numbers below were captured on the target hardware:

- **Raspberry Pi 5 Model B Rev 1.1**, 4 cores (ARM64), 16 GB RAM.
- PostgreSQL 17 + pgvector 0.8.1 (shared instance), `unaccent` extension.
- Go toolchain as pinned by the module; `CGO_ENABLED=0`.

Reproduce with the commands in each section.

---

## 1. Vector search (HNSW + halfvec)

### Index parameters

The image and face HNSW indexes (`internal/database/migrations/0006_embeddings.sql`)
use `halfvec` (float16) with `halfvec_cosine_ops` and build params `m = 16`,
`ef_construction = 200` — the photo-sorter tuning, validated as a good
recall/memory trade-off for normalised CLIP/ArcFace vectors. `halfvec` halves
the HNSW index memory versus `vector` (float32) at negligible recall loss on
normalised vectors, which is material on the Pi.

### Query-time `ef_search`

Every read query runs inside a read-only transaction that issues
`SET LOCAL hnsw.ef_search = 100` (`internal/vectors/store.go`, `withReadTx`). The
value is a named constant (`efSearch = 100`) with a guard test
(`internal/vectors/efsearch_test.go`) asserting it stays **positive and strictly
below `efSearchMax = 400`** — the ceiling the design forbids reaching. `SET LOCAL`
scopes the tuning to the transaction so it never leaks onto a pooled connection.

`ef_search = 100` is the measured sweet spot: it visits enough candidates for
full recall on these indexes while keeping per-query latency low. Raising it
toward 400 is pure latency cost with no recall benefit at this library size and
is intentionally never used.

### Index build guidance (large libraries / Pi)

Building an HNSW index is the memory-heavy operation, not querying it. For a
large backfill:

- Raise `maintenance_work_mem` for the build session so the graph builds in
  memory rather than spilling — e.g. `SET maintenance_work_mem = '512MB';`
  (or higher on the shared instance) before a `REINDEX`/`CREATE INDEX`.
- Optionally raise `max_parallel_maintenance_workers` to parallelise the build.
- If a from-scratch rebuild of a very large index is ever needed, prefer running
  it on a beefier host (the shared Postgres box, or a temporary restore on the
  x86 build machine) and ship the result, rather than rebuilding on the Pi under
  memory pressure. Day-to-day incremental inserts (one row per embedded photo)
  do not need this.

These are operational notes; no schema change is required.

---

## 2. Thumbnails (pure-Go vs vips)

### Measured pure-Go throughput on the Pi

```
go test -run '^$' -bench 'BenchmarkGenerate' -benchtime 3x -benchmem ./internal/thumb/
```

Source: a 4000×3000 (12-megapixel) JPEG — representative of a camera original.

| Benchmark | Time/op | Allocated/op |
|---|---|---|
| `BenchmarkGenerateFit720` (one `fit_720` preview) | **~0.98 s** | **~90 MB** |
| `BenchmarkGenerateAll` (all 8 registered sizes, one decode) | **~4.1 s** | **~1.18 GB** |

A single large-image preview takes ~1 s and ~90 MB; generating the full size set
for one photo allocates well over a gigabyte. On a multi-photo import this is the
dominant per-photo cost and a real memory concern on a 16 GB box shared with
other stacks — exactly the risk flagged in `ARCHITECTURE.md` §16.

### Optional `vipsthumbnail` engine (config-gated, opt-in)

`thumb.engine: vips` switches JPEG/PNG/WebP thumbnailing to a `vipsthumbnail`
shell-out (`internal/thumb/vips.go`). libvips streams and shrink-on-load, so it
is markedly faster and uses a fraction of the memory on large images. The binary
stays **CGO-free** (a separate process, not libvips bindings), so
`CGO_ENABLED=0` is preserved.

Properties that keep it safe to enable:

- **Pure-Go stays the default.** `thumb.engine` defaults to `go`.
- **Per-photo fallback.** Only JPEG/PNG/WebP originals use vips; HEIC/RAW/video
  go through the existing pure-Go `imgconvert` pre-decode. Any vips invocation
  failure falls back to pure-Go for that photo, so output never depends on vips
  succeeding — only speed does.
- **Same semantics.** Fit sizes use the shrink-only `WxH>` geometry (no upscale,
  matching the pure-Go rule); crop-square sizes use `--smartcrop centre`; EXIF
  orientation is applied by vipsthumbnail's autorotate (the same orientation
  Kukátko stored at import).
- **Bounded concurrency.** Both engines bound per-photo size encoding by
  `thumb.concurrency` (`WithConcurrency`, default GOMAXPROCS); lower it to cap
  peak thumbnail memory on constrained hosts.
- **Startup visibility.** `serve` logs the active engine and warns if `vips` was
  requested but `vipsthumbnail` is not on PATH (it then degrades to pure-Go).

Install on Debian/Ubuntu with `apt install libvips-tools`. Measuring the vips
path on a host with libvips installed (e.g. the x86 build machine) is the way to
quantify the speed-up; libvips is not installed on this Pi, so the shell-out path
here is exercised by tests with a fake `vipsthumbnail` rather than benchmarked.

---

## 3. Queries / pagination

### Hot path

The shared `GET /photos` browse/grid endpoint (library, album, label, favorites
grids, and search filters) all funnel through `internal/photos` `buildListQuery`.
The default, by far most frequent, query is:

```sql
SELECT … FROM photos
WHERE archived_at IS NULL
ORDER BY taken_at DESC NULLS LAST, uid DESC
LIMIT n OFFSET m
```

### Problem found

The original `idx_photos_taken_at (taken_at DESC)` could **not** serve that
ordering: it is `NULLS FIRST` (PostgreSQL's DESC default), has no `uid`
tiebreaker, and is not partial on `archived_at`. So every timeline page read all
live rows and **Sorted** them — the dominant cost on a large library.

### Fix — migration `0015_perf_indexes.sql`

Two partial composite indexes matching the live-timeline orderings exactly:

```sql
CREATE INDEX idx_photos_live_taken_at
    ON photos (taken_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL;
CREATE INDEX idx_photos_live_created_at
    ON photos (created_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL;
```

A timeline page is now a bounded index scan that stops after `LIMIT+OFFSET` rows
with **no Sort node**. They are partial on `archived_at IS NULL` (archived photos
are a minority and never in the default grid), keeping them small and write-cheap.
`idx_photos_live_created_at` backs the `sort=added` (recently-added) ordering used
right after an upload.

Verified by an EXPLAIN integration test
(`internal/photos/store_perf_integration_test.go`,
`TestListQueryPlan_usesLiveIndexes`): the plan for both orderings uses the
matching index and contains no `Sort` node — with **no planner overrides**, so
the planner reaches that plan on cost alone.

Two properties of that test's setup are load-bearing; do not "simplify" them away:

- **It seeds thousands of live photos** (`liveTimelineRows`). The index earns its
  keep only because `LIMIT 100` lets the scan stop early, so the seed must be well
  above one page. The test originally seeded ~87 rows — fewer than the `LIMIT`, so
  the early exit saved nothing and the planner picked between near-tied costs.
- **It runs `ANALYZE photos` after seeding.** Integration tests share one database
  and truncate between cases; `TRUNCATE` resets `pg_class` row counts but leaves
  `pg_statistic` holding whatever the *previous* test left. Without `ANALYZE` the
  planner costs this query from a foreign test's statistics.

Together those made the assertion depend on state the test did not control: the
same code planned `rows=1` on a fresh database, `rows=120` with one set of stale
stats, and `rows=73` **plus a `Sort`** in CI — which is how this test came to be
green locally and red on CI. The earlier `enable_seqscan`/`enable_bitmapscan`
overrides masked this and are gone; at a realistic volume the plan is correct
without them, and dropping the index now correctly turns the plan into a
`Seq Scan` + `Sort`, so the assertion has teeth.

```
go test -tags integration -run TestListQueryPlan_usesLiveIndexes ./internal/photos/
```

### Already-covered scopes (no change needed)

- **Album scope** (`?album=`): `album_photos` PRIMARY KEY `(album_uid, photo_uid)`
  serves the correlated `EXISTS`.
- **Label scope** (`?label=`): `idx_photo_labels_label_uid (label_uid)`.
- **Favorites scope** (`?favorite=`): `user_favorites` PRIMARY KEY
  `(user_uid, photo_uid)`.
- **Full-text search**: `idx_photos_fts` GIN over the generated `fts` tsvector.

### Pagination

Listing uses `LIMIT/OFFSET` (the established API contract:
`{photos,total,limit,offset,next_offset}`). With the matching index the planner
walks the index and stops at `LIMIT+OFFSET`, so a page is bounded rather than a
full sort — adequate for the infinite-scroll grid at realistic scroll depths.
Keyset/cursor pagination (`WHERE (taken_at, uid) < (…)`) would make very deep
pagination O(page) instead of O(offset), but it is a response-shape/contract
change and is intentionally **out of scope** for this behaviour-preserving pass;
it is noted here as the next step if deep-scroll latency ever becomes an issue.

### The album index (`GET /api/v1/albums`)

**Symptom (production, 2026-08-02).** The endpoint took **32.8 s** to return
151 kB for 437 albums / 20 197 visible photos / 40 459 memberships, and the
requests that did not finish in time failed outright — four `status:500` lines in
one minute of a user clicking "Alba".

**Root cause.** `listAlbumsSQL` picked each album's fallback cover with a
correlated `LEFT JOIN LATERAL … ORDER BY p2.taken_at DESC NULLS LAST LIMIT 1`.
That reads as "sort this album's ~93 photos and take the first", but the planner
satisfies a per-album `ORDER BY … LIMIT 1` by walking the **global**
`idx_photos_live_taken_at` order and probing each row for membership of that one
album (`Incremental Sort`, `Presorted Key: p2.taken_at`). An album whose newest
photo is old — a 2011 holiday — walks most of the library before its first hit,
and that happens once per album. `EXPLAIN (ANALYZE, BUFFERS)` on production:
**16 265 977 buffer hits to produce 437 rows, 99.99 % of them in the cover
subquery**; the rest of the statement (join, counts, MIN/MAX) cost 2 531 buffers
and 141 ms. Memoize capped it at 437 executions instead of 40 471, at 79.7 ms
each. A development library of a few hundred photos makes the global walk free,
which is exactly why it shipped.

**Fix.** The cover now falls out of the aggregation that already computes the
count and the capture-time bounds — no second pass over the memberships, no
correlated probe:

```sql
COALESCE(a.cover_photo_uid,
         (array_agg(p.uid ORDER BY p.taken_at DESC NULLS LAST, p.uid)
              FILTER (WHERE p.uid IS NOT NULL))[1]) AS cover_uid
```

The contract is unchanged: newest visible photo, unknown capture time last, uid
breaking ties, a hand-picked cover still winning; on the 437-album reproduction
the old and new statements return **byte-identical** rows. No migration and no
new index — the fix is the query shape, and the plan is two sequential scans and
a `GroupAggregate`.

**Measured** on a reproduction of the production shape (20 000 photos, 437
albums, 40 641 memberships, each album a contiguous slice of the timeline;
PostgreSQL 17.8 on the Pi):

| statement | total time | shared blocks | cover-lookup share |
| --- | --- | --- | --- |
| `LATERAL … LIMIT 1` (before) | 30 172 ms | 17 279 348 | 17 278 338 (99.99 %) |
| `DISTINCT ON (album_uid)` CTE | 352 ms | 2 023 | 1 007 (50 %) |
| single-pass `array_agg` (shipped) | **208 ms** | **1 013** | 0 — no node of its own |

The `DISTINCT ON` variant is the obvious one-pass shape and fixes the pathology
just as well; it lost because it reads `album_photos ⋈ photos` a second time.
Both are within budget, so a future rewrite may pick either — what must not come
back is a per-group `ORDER BY … LIMIT 1`.

**Regression test.** `internal/organize/albums_plan_integration_test.go`,
`TestAlbumListPlanStaysProportionalToMemberships`. It seeds that fixture, runs
`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` over the real `listAlbumsSQL` (exported
to the test through `export_test.go`) and fails when the statement reads more
than **8× the heap pages of `photos + album_photos + albums`** — 8 104 blocks for
this fixture, against 1 013 actually read and 17 279 348 before the fix. The
budget is expressed in pages of the data so it follows the fixture instead of
hard-coding a number, and it is a plan property rather than a wall-clock
threshold, which would be flaky on a shared machine. The same test checks a
sample of the returned covers against the naive per-album definition, so a
cheaper plan cannot quietly return a different photo.

```
go test -tags integration -run TestAlbumListPlanStaysProportionalToMemberships ./internal/organize/
```

**Sibling listings audited** for the same "pick one row per group" antipattern:

- `internal/people` `listSubjectsSQL` — **already correct**: its cover face comes
  from a `DISTINCT ON (m.subject_uid)` CTE, one pass over the markers.
- `internal/organize` `listLabelsSQL`, `searchAlbumsSQL`, `searchLabelsSQL` —
  fine: plain `COUNT` aggregates over one join, no per-group ordered lookup.
- `internal/vectors` `findDuplicatePairsSQL` — a `CROSS JOIN LATERAL … ORDER BY
  embedding <=> … LIMIT n`, but that inner order is served by the HNSW index, so
  the probe is bounded by design rather than a scan of the table.
- `internal/photos` (`store_places`, `store_years`, `store_timeline`),
  `internal/review` `leaderboard`, `internal/importverify`, `internal/jobs`
  stats, `internal/savedsearch` — plain aggregates or single-table listings, no
  per-group row pick.
- The remaining `ORDER BY … LIMIT 1` statements (`internal/jobs` claim,
  `internal/dupmerge`, `internal/importer`, `internal/photos` stack-primary
  election) run once per mutation against a keyed lookup, not once per row of a
  listing.

**Operability.** `internal/organizeapi` discarded the store error and answered a
bare `{"error":"listing albums failed"}`, so the production log carried
`status:500` and nothing else. Every 5xx in that package now goes through `fail`,
which logs the cause via `slog.ErrorContext` before writing the (unchanged)
client response; 4xx stays unlogged, since the caller is already told what was
wrong with the request.


### The review queue (`GET /api/v1/review/queue`)

**Symptom (production, 2026-08-02).** The endpoint answered **200 OK after
250.8 s** with a valid 89 kB payload for 105 named subjects / 113 628 faces /
20 437 photos. Nothing was broken — it was four minutes slow, so every browser
gave up first and `/review` simply never loaded.

**Root cause.** `internal/review` built the queue by running the **entire
recognition sweep across every named subject**, synchronously, inside the
request (`queue.go`, `s.sweeper.Sweep(...)`). That is the machinery behind
`GET /faces/sweep`, which exists as a *streaming* NDJSON endpoint precisely
because it is long-running; here its full result was awaited before a single
byte was written. `pg_stat_activity` sampled during the request showed no slow
statement at all — five concurrent kNNs, each 20–60 ms. **It was not one slow
query but thousands of fast ones**, and the count scaled with
`subjects × exemplars`, so it could only get worse. The per-user cache
(`CacheTTL`) made the *second* load instant and hid the problem: the first load
after any expiry exceeded every client timeout. A development library of a
handful of people makes the sweep milliseconds, which is why it shipped.

**Fix — bound the work to what one batch needs.** The review game shows one
question at a time, so producing a library-wide work list to fill a single card
was the mismatch. `internal/sweep` gained `Scan` — the same per-subject search,
the same worker pool, over a *window*:

```go
cov, err := sweeper.Scan(ctx, params, sweep.Window{Offset: cursor, Budget: 8},
    func(person *sweep.Person) (enough bool, err error) { … })
```

Three bounds now apply to one rebuild, in order of how often they bite:

1. **Early stop.** The collector returns `enough` as soon as the batch has
   `QueueSize` band candidates. A stop cannot un-dispatch what the pool already
   started, so it can overshoot by up to `Concurrency` subjects — those are
   still collected, so nothing computed is thrown away.
2. **Budget.** `review.face_budget` (8 subjects) and `review.label_budget` (6
   labels) cap a rebuild even when nothing qualifies. This is the bound that
   removes the library's size from the cost.
3. **Deadline.** `review.build_timeout` (15 s) is the backstop behind both: a
   rebuild that runs out of time serves what it has instead of holding the
   request open, and is logged. It never degrades into a wrong answer — a
   timed-out scan cannot report `no_people_no_labels`, only `no_candidates`.

**Coverage is preserved by rotation, not by exhaustiveness.** Each rebuild
advances an instance-wide cursor by the subjects (and labels) it scanned, so
successive rebuilds walk the whole library instead of re-reading its head. An
empty queue is no longer cached for the full TTL either: a dry queue rebuilds on
the next request, which scans the *next* window, so a player who works through a
batch keeps moving rather than waiting the cache out.

**What did not change.** The queue's content: the same uncertainty band, the
same exclusions the per-subject search already applies (assigned faces,
persisted rejections, negative exemplars, sub-reviewable faces),
`already_done` still dropped, the same informativeness ordering, the same
face/label interleave, and the same two reason codes — `no_people_no_labels`
still comes from the library-wide subject and label totals, which are cheap
counts, not from the window. The HTTP response shape is untouched.
`GET /faces/sweep` is untouched: `Sweep` still walks every subject and streams.

**Verified as a bounded-work property, not a wall-clock number** (which would be
flaky on a shared machine). `internal/review/queue_bound_test.go` runs one
`Queue` call over synthetic libraries of 10, 105 and 1 000 named subjects at the
production ratio of ~240 exemplars each, and asserts the kNN count stays under
`face_budget + concurrency` subjects' worth in all three — 1 000 subjects cost
what 10 do. Against the real database
(`internal/review/queue_scale_integration_test.go`, 105 named subjects, 4 000
unassigned faces, the face store instrumented to count kNN queries) one cold
`GET /review/queue` returned a full batch of 20 questions in **120 ms behind 7
kNN queries**, where a full sweep of the same fixture runs 105; the same request
over a 10-subject library ran 6, so the work does not follow the library. A
bounded queue is also compared question-for-question against an unbounded one on
a fixture small enough to enumerate.

```
make test-integration                       # or, for just this endpoint:
go test -tags integration -run TestReviewQueue_ ./internal/review/
```

The face dimension of that fixture is deliberately not seeded to 100 000: the
number of kNN queries is a function of the subject count, not of how many rows
each query walks, so 100 000 HNSW inserts would add minutes of fixture without
changing an assertion.

**Sibling endpoints audited** for the same "per-exemplar kNN loop awaited inside
a request" pattern:

- `internal/sweepapi` (`GET /faces/sweep`) — **by design**: it is the
  long-running one, and it streams NDJSON per subject, so the client renders
  progress from the first subject instead of waiting for the last.
- `internal/candidatesapi` (`POST /subjects/{uid}/candidates`) — **fine**: one
  subject, so the fan-out is that subject's exemplars (bounded by its own photo
  count, not by the library's people), under `candidates.concurrency`.
- `internal/expandapi` (`GET /albums|labels/{uid}/similar`) — **fine**: one
  collection, and `expand.source_cap` (500) already caps how many members become
  query vectors, so a thousand-photo album is sampled rather than queried whole.
- `internal/outlierapi` (`GET /subjects/{uid}/outliers`) — **fine**: no kNN at
  all. It loads one subject's faces and measures each against their centroid in
  memory.

The review queue was the only endpoint that multiplied a per-subject search by
the *number of subjects* before answering.

---

## 4. Frontend (large-library smoothness)

Verified, already optimal — no change required:

- **Grid virtualization**: `PhotoGrid` uses `react-virtuoso` `VirtuosoGrid` with
  `useWindowScroll`, so only on-screen tiles are mounted and the document scrolls
  (`web/src/components/library/PhotoGrid.tsx`). This holds at every density: the
  density control only restyles the grid's `grid-template-columns` (via
  `lib/gridDensity` `gridTemplateColumns`), so even the one-photo-per-row layout
  (`minmax(0, 1fr)`, default on phones) stays virtualized with intact infinite paging.
- **Thumbnail lazy-loading**: `PhotoTile` renders `<img loading="lazy"
  decoding="async">` inside a fixed `aspectRatio: '1 / 1'` box (no layout shift)
  and fades in on load (`web/src/components/library/PhotoTile.tsx`).
- **Request batching / pagination**: `usePaginatedPhotos` fetches 100 photos per
  page, cancels the previous in-flight request via `AbortController`, and ignores
  stale responses via a sequence guard; `loadMore` is a no-op while loading or at
  the end (`web/src/hooks/usePaginatedPhotos.ts`).
- **Search debounce**: `SearchPage` debounces typed queries by 350 ms (immediate
  on submit), so keystrokes don't each fire a semantic search
  (`web/src/pages/SearchPage.tsx`).
