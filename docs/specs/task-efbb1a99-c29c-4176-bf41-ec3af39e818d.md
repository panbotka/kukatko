# The review page never loads on the real library

`GET /api/v1/review/queue` on production (https://kukatko.kotrzina.cz, 105 named
subjects, 113 628 faces, 20 437 photos) answers **200 OK after 250.8 seconds** with a
valid 89 kB payload. The endpoint is not broken — it is four minutes slow, so every
browser gives up first and https://kukatko.kotrzina.cz/review simply never loads.

## Root cause

`internal/review/queue.go:80-93`:

```go
func (s *Service) faceQuestions(ctx context.Context) ([]Question, int, error) {
    params := sweep.Params{Threshold: 1 - s.bandMin}
    err := s.sweeper.Sweep(ctx, params, func(ev sweep.Event) error {
```

Building the queue runs the **entire recognition sweep across every named subject**,
synchronously, inside one HTTP request. That is the same machinery behind
`GET /faces/sweep`, which exists as a *streaming NDJSON* endpoint precisely because
it is long-running (`internal/sweepapi`). Here its full result is awaited before a
single byte of the response is written.

Measured while the request was in flight, from `pg_stat_activity`:

```
pid  | state  | running_for      | query
9186 | active | 00:00:00.057697  | SELECT photo_uid, face_index, embedding <=> $1 AS distance, bbox,
9213 | active | 00:00:00.040471  |   subject_uid, subject_name, marker_uid FROM faces
9209 | active | 00:00:00.040206  | WHERE subject_uid IS NULL AND (embedding <=> $1) <= $2
9308 | active | 00:00:00.033001  |   AND NOT EXISTS ( ...
9309 | active | 00:00:00.023683  |
```

**It is not one slow query — it is thousands of fast ones.** Each per-exemplar kNN
over `faces` costs ~40 ms; the sweep runs them for 105 subjects across 113 628 faces
behind a bounded worker pool. The cost is structural and scales with
`subjects × exemplars × faces`, so it will keep getting worse as the library grows.

A per-user cache already exists (`review.go:25`, "Built queues are cached per user
for CacheTTL"), which is why a *second* load is instant. It does not help: the first
load after any cache expiry exceeds every client timeout, so in practice the page
never opens.

This could not show in development — with a handful of subjects and a few hundred
faces the sweep is milliseconds. It is the same class of defect as the album index
(task `7cdd5581`), and between them two of the app's four main pages are unusable in
production.

## What to build

The fix is a design decision, not a query tweak. Make it explicitly and justify it:

- **Do not build the whole sweep to answer one question.** The review game shows one
  question at a time (`review.go` package doc). Producing a full library-wide work
  list to display a single card is the mismatch at the heart of this. Consider
  bounding the search to what a batch actually needs — `QueueSize` questions — and
  stopping as soon as enough band-qualifying candidates are found.
- **Or move the work off the request path**: precompute the queue in a job and have
  the endpoint serve what is ready, so the page returns immediately even when the
  queue is cold or empty.

Whichever you choose, these must hold:

- the queue's *content* must not change — the same uncertainty band
  (`inBand`), the same exclusions the sweep already applies (assigned faces,
  persisted rejections, negative exemplars, sub-reviewable faces), and
  `ActionAlreadyDone` still dropped (`queue.go:110-113`);
- questions must stay interleaved between face and label sources
  (`queue.go:65`), and the empty-queue reason must still distinguish
  `ReasonNoSources` from `ReasonNoCandidates` (`queue.go:66-71`);
- a cold cache must not be able to hold a request open for minutes. Whatever the
  shape, the endpoint needs a bound.

Also check `internal/candidatesapi`, `internal/expandapi` and `internal/outlierapi`
for the same pattern — a per-exemplar kNN loop awaited inside a request — and report
what you checked, including the ones that turned out fine.

## Verification — a small library proves nothing

- Seed a fixture at production scale: ~100 named subjects, ~100 000 faces, ~20 000
  photos, with the same skew (most faces unassigned).
- Assert the improvement as a **bounded-work property, not a wall-clock number**:
  the number of kNN queries (or faces examined) to answer one `/review/queue` request
  must not scale with the number of named subjects in the library. A timing
  assertion will be flaky on shared CI.
- Prove the queue's content is unchanged: same questions, same order, same reason
  codes as the current implementation on a fixture small enough to enumerate.
- Report the same evidence as above for the new implementation: request time and the
  query count behind it.
- `make check` **and** `make test-integration` must pass.

## Production constraints

Kukátko is live and this endpoint is user-facing and effectively down.

- **Do not touch the production instance, its database or its bucket.** The numbers
  above are already gathered; do not run load against production.
- Migrations run automatically at startup, each in its own transaction, against
  ~20 000 photos and ~113 000 faces. No destructive DDL, no long locks.
  `CREATE INDEX CONCURRENTLY` cannot run inside a transaction and is unavailable.
- **Never write a path that drops or rewrites originals or R2 objects.**
- **Do not run `make dev`** — it binds the port of the developer's own instance.
- The deployed image is pinned (`ghcr.io/panbotka/kukatko:0.1.1`) and there is no
  auto-updater, so merging does not deploy.

## Documentation

`docs/PERF.md` for the finding and the fix, `docs/PACKAGES.md` if the shape of
`internal/review` or `internal/sweep` changes, `docs/API.md` only if the response
changes — which it should not.