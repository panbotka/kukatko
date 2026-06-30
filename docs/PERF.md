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
`TestListQueryPlan_usesLiveIndexes`): with sequential and bitmap scans disabled
(forcing an ordered scan), the plan for both orderings uses the matching index
and contains no `Sort` node.

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

---

## 4. Frontend (large-library smoothness)

Verified, already optimal — no change required:

- **Grid virtualization**: `PhotoGrid` uses `react-virtuoso` `VirtuosoGrid` with
  `useWindowScroll`, so only on-screen tiles are mounted and the document scrolls
  (`web/src/components/library/PhotoGrid.tsx`).
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
