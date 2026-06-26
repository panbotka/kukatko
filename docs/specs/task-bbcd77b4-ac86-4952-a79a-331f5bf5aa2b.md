# M2 — Worker runtime

Add the in-process background worker that runs queued jobs, with a handler registry and graceful
lifecycle, plus a small status API.

## Context
Read `docs/ARCHITECTURE.md` §8. Depends on the `internal/jobs` queue (Claim/Complete/Fail). Runs
inside `kukatko serve`. Handlers for specific job types (image_embed, face_detect, etc.) are added
by later tasks — this task provides the runtime + registry + one trivial handler for testing.

## Requirements
- `internal/jobs` (or `internal/worker`): a `Worker` that polls `Claim` on an interval with
  bounded concurrency (configurable worker count), dispatches to a registered handler by job
  `type`, and calls `Complete`/`Fail` based on the handler result. Honors `context` cancellation
  and shuts down gracefully (finish/abandon in-flight cleanly; locks recovered by the queue).
- **Handler registry**: `Register(type, HandlerFunc)`; handlers receive (ctx, job payload).
- Start the worker from `kukatko serve`; stop it on shutdown signal.
- **Status API** (admin): `GET /api/v1/jobs/stats` (counts by state/type), `GET /api/v1/jobs`
  (recent/dead-letter list), `POST /api/v1/jobs/{id}/requeue` (requeue a failed/dead job). The
  frontend polls these (no SSE dependency).
- Register one trivial built-in handler (e.g. `noop`) used only for tests/sanity.

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Integration tests (test DB): worker claims and runs enqueued jobs to completion; failing
  handler triggers retry then dead-letter; graceful shutdown stops cleanly; status endpoints
  return correct counts; requeue works. Use a controllable fake handler.