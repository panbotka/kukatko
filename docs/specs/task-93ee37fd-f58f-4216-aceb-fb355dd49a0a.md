# Harden the job-queue lifecycle (heartbeat + reclaim safety)

Several related defects let a long-running or reclaimed job be duplicated,
dead-lettered while still running, or clobbered by a stale worker. Fix them together
— they all live in `internal/jobs` + `internal/worker`.

## Issues (all verified)

1. **No heartbeat → long jobs recovered as "stale" while still running.** `Heartbeat`
   exists (`internal/jobs/store.go:238-252`) but is NEVER called; `internal/worker`
   `process` (~worker.go:273-288) passes the root context and never refreshes
   `locked_at`. `RecoverStaleLocks` (`store.go:258-275`) presumes any job whose
   `locked_at` is older than `worker.stale_after` (default 5m,
   `config/config.go:964`) is dead: it requeues it (`attempts+1`) and a second worker
   re-claims the SAME row. A `pp_import`/`ps_migrate` job doing one full pass easily
   exceeds 5m → concurrent duplicate import, then spurious dead-letter while still
   running.
   - Fix: start a heartbeat ticker (interval < staleAfter, e.g. staleAfter/3) in
     `process` that calls `Heartbeat(id, workerID)` while the handler runs; add
     `Heartbeat` to the `Queue` interface. (Alternatively/additionally, bound a single
     import job to one page-batch that re-enqueues itself — but the heartbeat is the
     core fix.)

2. **Complete/Fail/Defer don't check `locked_by`.** `Complete` (store.go:166), `Fail`
   (:199), `Defer` (:226) match `WHERE id=$1 AND state='running'` only (unlike
   `Heartbeat`, which checks `locked_by`). A slow/stale worker A's late `Complete`
   marks a job `done` that worker B has already re-claimed and is processing → B's
   work double-runs or is silently dropped.
   - Fix: add `AND locked_by = $workerID` to Complete/Fail/Defer; thread the worker id
     through `record`. A mismatch means the job was reclaimed → drop the late result.

3. **Stale recovery retries immediately with no backoff.** `RecoverStaleLocks`
   (store.go:267) sets `run_after = now()`, so a job that crashes its process (e.g.
   OOM on a huge original) is re-claimed immediately and can crash again in a tight
   loop. `Fail` applies exponential backoff; recovery bypasses it.
   - Fix: give recovered jobs the same `least(cap, base*2^attempts)` backoff `Fail`
     uses.

4. **A deferral coinciding with shutdown burns a retry attempt.** `process`
   (worker.go:282-285) abandons ANY non-nil result on shutdown, including a
   `RetryAfterError` (which must NOT consume an attempt); the job stays `running` and
   `RecoverStaleLocks` later increments `attempts`. Across restarts while the box is
   offline, an `image_embed` job that should wait forever can exhaust attempts and
   dead-letter.
   - Fix: on shutdown still write `Defer` for a `RetryAfterError` via the
     shutdown-immune bookkeeping context; only abandon genuine handler errors.

5. **(Minor) Claim index order mismatches the ORDER BY.** Index
   `idx_jobs_claim (state, run_after, priority)` (`migrations/0005_jobs.sql:34`) vs the
   claim `ORDER BY priority DESC, run_after ASC, id ASC` (store.go:131) → the planner
   sorts on every claim under a deep backlog.
   - Fix: add a migration for a partial index `(state, priority DESC, run_after, id)
     WHERE state='queued'` matching the claim ordering.

## Requirements
- All five fixed without regressing existing job semantics (dedup, backoff, dead-letter).
- Changes to the `Queue` interface must be reflected in all implementers/mocks.

## Testing
- Integration tests (real test DB): a job that runs longer than `stale_after` with
  heartbeat is NOT recovered; a reclaimed job cannot be completed by the previous
  owner; recovery applies backoff; a deferral at shutdown does not increment attempts.
- `make check` must pass.