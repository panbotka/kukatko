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

### The cheapest thumbnail is one that is already in the bucket

The numbers above are what a *cold local cache* used to cost on a publishing
backend: `Generate` decided "already done" from the local disk alone, so a pruned
cache meant re-encoding **and** re-uploading objects that were already in R2.
That is not a hypothetical — eight sizes are ~2.76 MB per photo (~57 GB for the
production library against ~20 GB of free disk), so the import runs with a pruner
and the cache is cold by construction. A full pass over ~20 670 photos at ~4 s
each is around twenty hours and ~57 GB of pointless writes.

`internal/thumb` now asks the store first (`dropPublished`). The shape matters:
the question is about eight keys at once, and they share the sharded prefix
`thumb/<aa>/<bb>/<cc>/<hash>_`, so **one prefix listing** answers all eight.
Measured against the dev MinIO on this Pi
(`go test -tags integration -v -run TestR2KeysWithPrefix_measure ./internal/storage/`):

| Shape | Round trips | Time |
|---|---|---|
| `KeysWithPrefix` over one photo's prefix | 1 | **~2.2 ms** |
| `Head` per size | 8 | ~8.3 ms |

Against a ~4 s encode (plus eight uploads) the check pays for itself by three
orders of magnitude, and the gap widens on a real WAN link to R2, where the
round-trip count is what dominates. Re-run the test above against another
endpoint before changing the shape.

Two properties keep it from costing anything it should not: a **warm cache never
lists at all** (nothing is missing, so there is nothing to ask about), and a
**failed listing falls back to encoding** — slower is a cost, whereas skipping a
size that is not really in the bucket would leave a thumbnail no client can fetch.

The price of the optimisation is a rule the rest of the application has to keep:
**a generated size need not exist on disk.** `Generate` skipping a published size
is the *point*, and the local file it did not write is exactly the file a caller
must not go looking for. Whatever wants a thumbnail's bytes therefore reads them
through `thumb.OpenOrGenerate` — local cache, then the published object, then
encode — and never through `OpenCached`, which sees the disk alone. Breaking that
rule is not a slow path, it is a dead one: reading the cache directly is what made
**every** `image_embed` job on R2 fail its five attempts and dead-letter with
`thumb: thumbnail not cached`, silently leaving each newly imported photo without
an embedding — so no semantic search, no near-duplicate check and no "similar
photos" for anything new. Fetching the object instead costs one GET against ~4 s
of encoding it did not have to do.

### Which `fit_*` a face crop is worth

A crop of a face is the one place where the smallest thumbnail is usually the
wrong answer, and the largest is usually too expensive. Measured on the
production library for one subject (289 assigned faces): the **average box is
4.9 % of the frame wide**, 69 % are narrower than 5 % and 40 % narrower than 3 %.
In a `fit_720` thumbnail that average face is **35 px** across, the smallest
8.4 px — which is why a review card blowing it up ~7× reads as a smear rather
than a person.

`web/src/lib/faceSource.ts` therefore picks the **smallest** registered `fit_*`
that still puts a target number of real pixels across the crop, per face. Two
ceilings keep that from turning into a blanket `fit_3840`:

| Caller | Ceiling | Target | Why |
|---|---|---|---|
| chips/tiles (`FaceCrop`: people grid, clusters, markers) | `fit_1920` | 300 px | dozens on screen at once; past 1920 the pixels are mostly not in the original either |
| `/outliers` review card | `fit_3840` | 154 px across the crop (≈ 96 px across the face) | the card *is* the question being asked; a handful on screen, all `loading="lazy"` |

96 px is roughly the width a face is *rendered* at in the densest grid the review
page offers (ten columns of a ~1400 px page ⇒ ~88 px, since the crop is defined
from the box and the face is always 62.5 % of a tile), so at maximum density the
crop is essentially 1:1 and at the default it is a ~2× upscale. Raising the bar
buys mostly bytes: with that distribution, the target is what decides how much of
the library lands on `fit_3840` (~1.9 MB) rather than `fit_1920` (~0.5 MB).

Two guards on the cost: a rung above the original's own long side is never
chosen (`fit_*` does not upscale, so it would be the same pixels under another
URL and a second cache entry), and the card **degrades** down the ladder on
`onError` — on a publishing backend the thumb route redirects instead of
generating, so a size missing from the bucket 404s, and one failed request lands
on the `fit_720` that has always been there rather than a broken image.

---

## 3. Queries / pagination

### Hot path

The shared `GET /photos` browse/grid endpoint (library, album, label, favorites
grids, and search filters) all funnel through `internal/photos` `buildListQuery`.
The default, by far most frequent, query is:

```sql
SELECT … FROM photos
WHERE archived_at IS NULL
  AND (stack_uid IS NULL OR stack_primary)
  AND NOT hidden_from_library
ORDER BY taken_at DESC NULLS LAST, uid DESC
LIMIT n OFFSET m
```

The two extra predicates are the stack-visibility gate and the
hide-from-library flag (migration `0049`). Neither is in an index and neither
needs to be: both drop a small minority, so they ride along as **filters** on the
index scan below — measured on 25 000 rows (250 hidden, 500 archived), page 1
costs 7 shared buffers and 0.11 ms with or without `NOT hidden_from_library`, and
the plan is the same `Index Scan using idx_photos_live_taken_at`. The EXPLAIN
test below mirrors them, so the day one of them stops being free is the day it
fails.

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

**The candidate covers ride along for free.** The listing also returns
`cover_uids`, the album's `organize.CoverCandidates` (8) newest visible photos,
so the index can draw a tile that differs from its neighbours' rather than the
one photo overlapping albums share. It is the *same* aggregate sliced further —
`[1:$1]` instead of `[1]` — and because both projections spell it identically the
executor computes it once; the plan test measures 210 ms / 1 024 blocks with the
candidates against 208 ms / 1 013 without. Note the statement now takes one
parameter (the bound), which anything running it by hand — `EXPLAIN`, `psql` —
has to supply.

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
  from a `DISTINCT ON (m.subject_uid)` CTE, one pass over the markers. Its
  `COUNT(DISTINCT p.uid)` (the photo count next to the marker count) is an extra
  sort per group, not an extra join — it reads the rows the marker count already
  aggregates.
- `internal/organize` `listLabelsSQL`, `searchAlbumsSQL`, `searchLabelsSQL` —
  fine: plain `COUNT` aggregates over one join, no per-group ordered lookup.
- `internal/vectors` `findDuplicatePairsSQL` — a `CROSS JOIN LATERAL … ORDER BY
  embedding <=> … LIMIT n`, but that inner order is served by the HNSW index, so
  the probe is bounded by design rather than a scan of the table.
- `internal/photos` (`store_places`, `store_years`, `store_timeline`),
  `internal/review` `leaderboard`, `internal/jobs`
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
- `internal/candidatesapi` (`POST /subjects/{uid}/candidates`) — one subject, so
  the fan-out is that subject's exemplars, under `candidates.concurrency`.
  Judged fine at the time on the grounds that this is "bounded by its own photo
  count, not by the library's people" — which is not a bound at all, as the
  memory section below records. It is bounded now.
- `internal/expandapi` (`GET /albums|labels/{uid}/similar`) — **fine**: one
  collection, and `expand.source_cap` (500) already caps how many members become
  query vectors, so a thousand-photo album is sampled rather than queried whole.
- `internal/outlierapi` (`GET /subjects/{uid}/outliers`) — **fine**: no kNN at
  all. It loads one subject's faces and measures each against their centroid in
  memory.

The review queue was the only endpoint that multiplied a per-subject search by
the *number of subjects* before answering.

### The candidate search's memory (`POST /subjects/{uid}/candidates`, `GET /review/queue`)

**Symptom (production, 2026-08-02, 19:36).** The server process grew to
**10.9 GB anon-rss** and the *host* OOM killer took it out:

```
Out of memory: Killed process 4173337 (kukatko)
  total-vm:13049748kB  anon-rss:10919464kB
oom-kill: constraint=CONSTRAINT_NONE ... global_oom
```

`global_oom` on a 15 GB VPS with no swap and no container memory limit means this
was never Kukátko's problem alone — photoprism, mariadb and the embeddings
sidecar were all in the blast radius. A logged-in user clicking through
`/review` could take the whole box down.

**Attribution — measured, not guessed.** The PhotoPrism import (then still in the
binary) was sampled every 15 s for a whole `--full` run and peaked at **33 MB**,
so it was not the importer;
the idle serve process holds 45–60 MB. The log at that minute carries
`review: queue rebuild hit its deadline, serving a partial queue` and a
`GET /api/v1/review/queue` lasting **15 086 ms** — the rebuild deadline firing,
which bounds *time* and says nothing at all about memory.

**Root cause — two unbounded axes in `internal/candidates`, one request wide.**

1. **Exemplars.** `Find` loaded *every* face assigned to the subject, embeddings
   included, and ran one kNN per exemplar, merging every result set. Nothing
   capped that count, so the cost followed how heavily a person was tagged. The
   separate catch-all-subject bug had left one "person" holding **16 532**
   exemplars, which is what lit the fuse — but a genuinely well-tagged person in
   a large library gets there on merit.
2. **Candidates.** Every survivor was hydrated into a full `photos.Photo` —
   **EXIF blob included** — before the request's `Limit` was applied, and that
   record was then copied again into each `Candidate`, again by the sweep, and
   again into each review `Question`. Truncating afterwards bounds the answer,
   not the work. A subject matching tens of thousands of unnamed faces therefore
   cost hundreds of megabytes, and the review scan repeated it per subject for
   the full 15 s of its deadline, four subjects at a time.

**Measured, in `internal/candidates/memory_test.go`** (synthetic library, fakes
allocating what the pgx-backed store allocates, `runtime.MemStats.TotalAlloc`
around one `Find`):

| shape | before | after |
| --- | ---: | ---: |
| 1 000 exemplars | 65 MB | 33.9 MB |
| 20 000 exemplars | **1 247 MB** | 34.6 MB |
| 500 matching unnamed faces | 33.9 MB | 33.9 MB |
| 40 000 matching unnamed faces | **246 MB** | 46.4 MB |

**Fix — three bounds, none of which mentions the library's size.**

1. **`candidates.max_exemplars` (500).** `vectors.SampleFacesBySubject` reads an
   *even-strided sample* of a subject's faces **in SQL**, plus the true face and
   photo totals so the summary stays honest about what it sampled from. Capping
   in Go would still have transferred and decoded a 512-dimension embedding per
   row. Recall barely moves: the vote rule clamps at five agreeing exemplars,
   which hundreds supply as well as thousands.
2. **`candidates.max_candidates` (500).** The voted set is ordered nearest-first
   and cut **before hydration**, on the small vote structs. Past that cut each
   candidate costs a photo row; before it, it costs 80 bytes. The cut is reported
   as `capped` on the response rather than applied silently.
3. **`Request.MinDistance`.** The review game only ever asks about the
   uncertainty band, so it now pushes the band's *far* edge into the search
   (`Threshold = 1 − band_min`, `MinDistance = 1 − band_max`) instead of
   discarding the confident matches after paying to hydrate them. The queue it
   builds is capped too — a `Question` carries a whole photo record and the queue
   is cached per user until the session is pruned.

The residual worst case is a constant: `max_exemplars` kNN queries of the vector
layer's 500-row maximum each, merged into one voted set — a few tens of
megabytes, whatever the library does.

**Noticed while measuring, not fixed here: there is no index on
`faces.subject_uid`.** Every "this subject's faces" read — `ListFacesBySubject`,
the new `SampleFacesBySubject`, outlier detection — therefore seq-scans the whole
`faces` table (113 628 rows in production), and a review rebuild does it once per
subject in its window. `sampleFacesBySubjectSQL` is deliberately written to scan
once rather than twice (`max(dense_rank())` over two query levels instead of a
second subquery for the distinct-photo count), so this change does not make it
worse — but a `CREATE INDEX idx_faces_subject_uid ON faces (subject_uid)` would
turn a dozen seq scans per rebuild into a dozen index scans. That is a latency
fix, not a memory one, so it is recorded here rather than smuggled into a
migration under this heading.

**One memory exposure is knowingly left in place.** `GET /subjects/{uid}/outliers`
(`internal/outliers`) still reads *every* face of a subject via
`ListFacesBySubject`, embeddings included — 2 kB a face, so ~34 MB for a
16 532-face subject and ~200 MB for a hypothetical 100 000-face one. It is two
orders of magnitude below what the candidate search was doing, and it is not the
same kind of bug: outlier detection ranks a person's faces *against their own
centroid*, so a sample would not bound the work, it would change the answer.
Bounding it properly means computing the centroid and the distances in SQL, which
is a different job from this one. Noted rather than silently claimed as fixed.

**The runnable check.** `internal/candidates/memory_test.go` asserts both axes
structurally — 20× the exemplars must not cost more than 1.5× the bytes, an
order of magnitude more matches not more than 2×, and neither may exceed a
96 MiB ceiling. It runs in
`make check` (no build tag). Removing either cap makes it fail by an order of
magnitude, so it is a real regression detector and not a passing decoration:

```
go test -run TestFind_allocationDoesNotScale -v ./internal/candidates/
```

**Still outstanding — the container has no memory limit.** The fix bounds the
application; it does not change what happens if anything else on that box ever
does this again. The `kukatko` service in the `vps` repo's `docker-compose.yml`
runs with `MemoryLimit=0`, which is why a runaway process became a *global* OOM
instead of one dead container. Proposed (not applied — that repo is deployed
separately):

```yaml
  kukatko:
    image: ghcr.io/panbotka/kukatko:0.2.0
    mem_limit: 1g          # idles at 45–60 MB; 1 GB is ~16x headroom
    memswap_limit: 1g      # no swap on this VPS anyway — make that explicit
    restart: always        # already set: a cgroup OOM then restarts one service
```

With that in place the same bug degrades from "the VPS falls over" to "one
container restarts", which is the difference the limit buys. Sizing rests on the
measurements above: bounded requests peak in the tens of megabytes, and the
CPU-bound worker pool (`KUKATKO_WORKER_COUNT: 4`, thumbnail decodes) is the other
consumer.

### The candidate search's latency (`POST /subjects/{uid}/candidates`)

**Symptom (production, 2026-08-03, from the user).**
`/faces?subject=sudh96iqevipv1v2cjfn85a26q&threshold=60&limit=0` "takes forever.
In the sorter it took a few milliseconds." The subject is Tomáš Kozák, 428
markers. The HNSW indexes were all in place, so this was never a missing index.

**Reproduced before anything was changed**, because a fix without a number
before and after is a guess. A synthetic library of the production shape was
loaded into the development database on the Pi: 50 410 faces over 1 000
identities, one of them holding 460 faces of which 428 are assigned to a subject
and 32 are not. Baseline, warm cache, nothing else running:

```
Find(subject, threshold 0.4): 13.7 s, 428 exemplars, 32 candidates
```

**Root cause — the per-exemplar query, not the number of them.** Every exemplar
ran this, under `hnsw.iterative_scan = strict_order`:

```sql
WHERE subject_uid IS NULL AND (embedding <=> $1) <= 0.4
ORDER BY embedding <=> $1 LIMIT 500
```

`EXPLAIN (ANALYZE, BUFFERS)` on one of them:

```
Limit (actual time=5.4..116.8 rows=32)
  -> Index Scan using idx_faces_hnsw on faces (actual rows=32)
       Rows Removed by Filter: 20260
       Buffers: shared hit=38874 read=3957
Execution Time: 117.4 ms
```

`20260` is `hnsw.max_scan_tuples` (20 000) plus the overshoot of the batch that
reached it — the scan gave up on the cap, not on running out of neighbours. Both
filters are invisible to the index scan, so the iterative scan cannot tell "no
more rows are near enough" from "keep looking" and walks the graph until the cap
— **every time, for every exemplar, whatever the LIMIT**. 428 exemplars × 90 ms
of graph walking is the whole 13.7 s. The two filters cost differently, though:

| variant (same query, same 32 rows out) | time |
| --- | ---: |
| iterative scan + distance predicate (**before**) | 90 ms |
| iterative scan, distance cut in Go | 38 ms |
| iterative scan, distance cut in Go, 100 neighbours | 15 ms |
| partial index, distance cut in Go, 100 neighbours (**after**) | 10 ms |

**Fix — three changes, none of which touches what the search returns.**

1. **A partial HNSW index** (`0047_faces_unassigned_hnsw.sql`) whose predicate is
   the search's predicate verbatim: `WHERE subject_uid IS NULL`. In the
   neighbourhood of a well-tagged person almost every near neighbour *is*
   assigned — they are that person's own tagged faces — so on the full index the
   filter threw nearly all of them away and the iterative scan had to keep
   walking. On the partial index there is nothing to filter. Cost of carrying it:
   65 MB per 50 000 faces, and assignment writes maintain both graphs.
2. **The distance cut moved out of SQL into Go**
   (`internal/vectors.FindSimilarUnassignedFaceCandidates`). The rows arrive
   ordered by distance, so stopping at the first one beyond `maxDistance` yields
   exactly the set the SQL predicate did — including when more faces are within
   the threshold than the `LIMIT`, where both keep the nearest `LIMIT` of them —
   without blinding the index scan.
3. **A per-exemplar neighbour cap that follows the source set**
   (`internal/candidates.perExemplarLimit`): a lone exemplar keeps the full
   500-row maximum, a crowd gets `4 × max_candidates` shared between them, floored
   at 100. Every neighbour is paid for once per exemplar, and 428 × 400 unwanted
   neighbours is most of what remained after (1) and (2).

**End to end, same library, same query:**

| | before | after |
| --- | ---: | ---: |
| 428 exemplars, 32 unnamed matches | 13.7 s | **0.80 s** |
| 428 exemplars, 632 unnamed matches | — | 0.66 s |

**Quality is unchanged, and that is measured too.** Both benchmark shapes — the
sparse one above and a dense one where every exemplar has 632 unnamed neighbours
inside the threshold — return the *identical* candidate set in the identical
order with the cap at 100 and at the store's 500-row maximum. The regression net
is `TestFind_perExemplarCapCostsNoMatchesDB` (`internal/candidates`,
`-tags integration`), which plants the shape in which the cap could bite and
requires the bounded search to equal an unbounded one candidate for candidate;
dropping the cap to 8 makes it fail. `TestUnassignedFaceHNSWIndexExists`
(`internal/vectors`) guards the index predicate, because a drift between it and
the query's `WHERE` clause breaks nothing — it just silently costs 9× again.

**Ruled out, with measurements rather than opinion:**

- **`ef_search`.** Recall on this library is complete at `ef_search = 40`; the
  pinned 100 is not the problem and raising it only costs latency (200 → 14 ms,
  400 → 21 ms, 800 → 29 ms per query, all returning the same 32 rows).
- **Batching the round trips.** One transaction per exemplar is five round trips,
  2 140 of them per request. A `CROSS JOIN LATERAL` over `unnest(...) WITH
  ORDINALITY` does use the partial index and collapses them to one — but at
  0.8 s wall for 4.3 s of database CPU the concurrency already hides them, so it
  would buy latency the request does not spend. Not done.
- **One query over a centroid**, which is what photo-sorter is assumed to have
  done. It would be a single query, but it collapses a person's several
  appearances into one point and there is no widening radius that provably covers
  the exemplars' union: for face embeddings the angular triangle inequality gives
  a radius above 90°, i.e. "everything". Rejected on quality.

**The catch-all subject is already bounded.** `candidates.max_exemplars` (500)
caps the source set before any of this, so the 16 532-exemplar "person" runs 500
searches, not 16 532 — the same bound that fixed the memory blow-up above.

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
- **Windowed library grid (jump cost independent of distance)**: the library does
  *not* accumulate pages — `useWindowedPhotos`
  (`web/src/hooks/useWindowedPhotos.ts`) sizes the grid to the result's `total`
  from the first response and loads only the pages under the visible range
  (`ensureRange` from `onRangeChanged`, ±`WINDOW_PREFETCH_PAGES`), aborting
  requests a jump has travelled past and evicting down to `WINDOW_MAX_PAGES`
  (~2 400 photos) so memory stays bounded. Unloaded slots render as placeholder
  tiles. A timeline jump is therefore `scrollToIndex` to the month's `cumulative`
  (counted in SQL by `GET /photos/timeline`) plus one page fetch. Measured in
  Chromium against a seeded **20 889-photo** library, clicking the rail's oldest
  year: **40.3 s / 102 sequential page requests → ~3.1 s**, the same as jumping
  one month back (6.2 s → 3.2 s) — the point being that the two are now equal.
- **Search debounce**: `SearchPage` debounces typed queries by 350 ms (immediate
  on submit), so keystrokes don't each fire a semantic search
  (`web/src/pages/SearchPage.tsx`).
- **Per-face thumbnail choice**: the `/outliers` cards pick their source size per
  face rather than sharing a constant, and the size is deliberately *not* a
  function of the column count — changing the density restyles the grid without
  re-fetching a single image. See "Which `fit_*` a face crop is worth" above.
