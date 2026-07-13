# Fix the failing query-plan integration test on main

CI on `main` is red: the `Run integration tests (make test-integration)` step fails
(GitHub Actions run 29205401903). The only failing test is
`TestListQueryPlan_usesLiveIndexes/default_taken_at_timeline` in
`internal/photos/store_perf_integration_test.go`. Everything else passes.

The failure reproduces deterministically on a local test DB — it is not a CI flake.

## Observed failure

For the default grid query

    SELECT uid FROM photos WHERE archived_at IS NULL
    ORDER BY taken_at DESC NULLS LAST, uid DESC LIMIT 100 OFFSET 0

the plan the test gets is:

    Limit  (cost=34.62..34.80 rows=73 width=35)
      ->  Sort  (cost=34.62..34.80 rows=73 width=35)
            Sort Key: taken_at DESC NULLS LAST, uid DESC
            ->  Index Scan using idx_photos_live_created_at on photos  (cost=0.14..32.36 rows=73)

so both assertions fail: the plan does not use `idx_photos_live_taken_at`, and it contains a
Sort node.

## What the investigation already established (do not redo it, verify it)

- `idx_photos_live_taken_at` exists in the test DB with exactly the expected definition
  (`btree (taken_at DESC NULLS LAST, uid DESC) WHERE archived_at IS NULL`, migration 0015).
- The index CAN serve the ordering. With `enable_sort = off` the planner picks
  `Index Only Scan using idx_photos_live_taken_at` (cost 0.14..49.24) — no Sort.
- The problem is cost, not capability: the test seeds only ~87 rows and never runs `ANALYZE`,
  so the planner works from stale/default statistics (`rows=73` vs. 82 actually live) and a
  cold visibility map. At that size a full scan of the *other* partial index plus a Sort of
  the whole result (34.80) looks cheaper than the ordered scan (49.24), and the `LIMIT 100`
  early exit — the whole point of the index — buys nothing because the table has fewer than
  100 live rows.
- The sibling subtest `recently added created_at` passes only incidentally (its full index
  scan happens to be the plan the planner already prefers), so it is not evidence that the
  setup is sound.
- Conclusion: this is a test-setup defect, not a production regression. `buildListQuery` and
  migration 0015 look correct. Confirm this yourself before changing anything.

## Requirements

- The test must assert the real optimisation: for the two hot live-grid orderings
  (`taken_at DESC NULLS LAST, uid DESC` and `created_at DESC NULLS LAST, uid DESC`, both with
  `archived_at IS NULL`), a page of the grid is served by the matching partial index from
  migration 0015 with no Sort node in the plan.
- Make the setup realistic enough that the planner reaches that plan on its own merits:
  seed enough live photos that `LIMIT 100` is a genuine early exit (a few thousand rows, well
  above the page size), and run `ANALYZE photos` after seeding so the planner has real
  statistics. Keep the existing mix of archived photos and photos with a NULL `taken_at` so
  the partial predicate and the NULLS LAST tail stay exercised.
- Do NOT make the assertion vacuous. Disabling `enable_sort` (or asserting on a plan with the
  ordering forced) would make the test pass while proving nothing — the current
  `enable_seqscan`/`enable_bitmapscan` guards may stay only if they are still needed at the
  larger volume; drop them if the plan is correct without them.
- Keep the test runtime sane (target under ~30 s for the package). Inserting a few thousand
  rows one-by-one through `Store.Create` will be slow — use a bulk insert helper inside the
  test file if needed.
- If, contrary to the analysis above, you conclude the index genuinely cannot serve the
  ordering, then fix the index/migration instead of the test, and record the reasoning in
  `docs/PERF.md`.

## Verification

- `make check` does NOT run integration tests — it will stay green either way, so it proves
  nothing here. You must run the integration tests explicitly.
- Load the test DSN from the gitignored `.secrets/db.env` and export
  `KUKATKO_TEST_DATABASE_URL="$KUKATKO_TEST_DATABASE_URL_HOST"` when running from the Pi host
  (the non-`_HOST` DSN points at the Docker network and will not resolve).
- Run `go test -tags integration -run TestListQueryPlan_usesLiveIndexes ./internal/photos/ -count=1 -v`
  at least three times in a row — it must pass every time, including on a freshly truncated DB.
- Then run the whole `make test-integration` and paste the result. The `internal/photos`
  package must be `ok`.
- Show the passing plan output (the EXPLAIN text) in the task summary as evidence.

## Docs

Update `docs/PERF.md` if the indexes or the reasoning behind them change. A pure test fix
needs no doc change beyond a comment in the test explaining why the seed size and `ANALYZE`
are load-bearing — so the next person does not "simplify" it back into a flaky assertion.
