## Context
Round 2 of the multi-tenancy review (2026-10-14, main 672357f6) produced 16 fix tasks ("Multi-tenancy review II 01–16/17"). Reports and screenshots: `<backup dir>/multitenancy-review-2/` (migration-review.md has the exact rehearsal commands, walk-report.md the walk table, isolation-review.md the probe list). Method notes: the operator's private notes on the prod rehearsal method, the box staging stack and driving prod. Before you start, check in Botka/git log which of the 16 tasks are done; if any blocker task (01, 02, 05, 07, 08) is not done, stop and finish as needs_review listing what is missing.

## Requirements
1. Box staging: wake the build box and upgrade its staging stack to current origin/main; inventory before/after with the runbook's documented expect list.
2. Rehearsal on a FRESH read-only prod dump (streamed to the box, never the Pi; prod is read-only for you: pg_dump + SELECT + /healthz only): full chain from prod's schema to main, follow the runbook in docs/OPERATIONS.md LITERALLY as the operator would (every command copy-pasted) — any step that fails as written is a blocker; inventory-diff exits 0; memberships = accounts; old/new parity; rollback to prod's schema restores inventory A and the old release serves login; the CLI refuses side-effect migrations while the old binary serves.
3. Storage move rehearsal on staging for a fresh project and, if feasible, on a copy of v with a seeded stray root thumb/hls object: reaches done, nothing lost (`inventory --storage`), CDN-less thumbnails render.
4. Re-run the isolation probes from isolation-review.md (≥60 routes foreign vs missing uid, the leaderboard, task participants, cover photo, audit ?user=, near-dup upload) and the walk table from walk-report.md (desktop + phone; project switch on every list page; upload queue across a switch). Max one browser at a time (16 GB Pi).
5. Append a dated "safety gate III" section to docs/MIGRATION_SAFETY.md with verdict GO / NO-GO; every still-open problem becomes a short list in the commit message and in the section (do not create Botka tasks yourself).
6. Clean up: box throwaway containers/dumps deleted; staging leftovers prefixed and listed.

## Out of scope
Deploying to production; tagging a release (the owner does that via /release).