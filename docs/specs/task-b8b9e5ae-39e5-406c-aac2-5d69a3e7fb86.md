## Context
Round 3 review of the multi-tenancy programme (2026-10-15, main 996ae051). Reports: /mnt/nas-botka/kukatko/multitenancy-review-3/round3/fixes.md §Findings 4, 5, 6 and the last bullet of 14 (guard probe in /mnt/nas-botka/kukatko/multitenancy-review-3/round3/fix/ — `guardprobe`), and /mnt/nas-botka/kukatko/multitenancy-review-3/round3/isolation.md §F4. The guard (internal/tenancyguard, CLAUDE.md rule) passes its own tests at 996ae051, but:
1. internal/tenancyguard/clause.go:211,234 — splits on AND before looking at OR: `WHERE p.project_id=$1 AND p.uid=$2 OR p.uid=$3` passes but reads every project (reproduced in a unit probe).
2. internal/tenancyguard/fold.go:140,153 (`mentionsProject`) — any opaque expression whose text contains "project" counts as a scope: `p.project_id IS NOT NULL`, `(p.project_id=$2 OR TRUE)`, `filter(projectUID)`, `COALESCE(NULLIF($1,''), project_id)` all pass (reproduced). With the heuristic off, three live statements are unflagged and unjustified: internal/phototask/digest.go:155 (and :85-86 whose real filter sits under `$2 = '' OR …`), internal/tagnotifyjob/service.go:163, internal/tagnotifyjob/recorder.go:250.
3. clause.go:275 + tables.go:77 — a child table counts as keyed by ANY operator on its parent key (`ph.photo_uid <> $1` passes). Only 9 child tables are judged; ~17–20 others are missing from `KeyedChildTables` (e.g. comments, user_ratings, photo_task_photos, face_rejections, review_skips, notification_photos).
4. Lower-case SQL and conditionally appended predicates are skipped.
## Requirements
- Fix 1–4 without weakening the guard: OR binds looser than AND; the scope test accepts only `<alias>.project_id = <param|scoped alias>` (no name heuristic); only `=`/`IN`-style equality on the parent key keys a child; derive the child-table list from the migrations (FKs to a root table) so new ones cannot be forgotten; handle lower-case.
- Every statement newly flagged is either scoped or allow-listed with a reason (`path:line reason`).
- Tests that FAIL on the current code: unit cases for each bypass above.
- Docs: docs/PACKAGES.md (tenancyguard), CLAUDE.md only if the rule text changes.
## Out of scope
Behavioural changes to the flagged statements beyond scoping them.