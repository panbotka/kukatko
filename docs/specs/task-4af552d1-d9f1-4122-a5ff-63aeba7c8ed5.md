# The statistics page must stop reporting three irreconcilable face counts

`/stats` shows four numbers that all call themselves faces, read as a partition, and do not add up — and it measures the same ratio twice with two different denominators, on one screen.

## Evidence (observed on production, 2026-08-12)

| shown as | value | actually |
| --- | --- | --- |
| Nalezenych obliceju | ~115k | `faces` (raw detections) |
| Pojmenovanych obliceju | ~4.7k | `markers_assigned` |
| Nepojmenovanych obliceju | ~16.6k | `markers_unassigned` |
| Oblicejе se jmenem (meter) | 3,8 % | `faces_assigned / faces` = ~4.4k / ~115k |

- ~4.7k + ~16.6k = ~21.3k, not ~115k. Three counts named "oblicej" that invite addition and cannot be added.
- The meter reads 3,8 % while the card immediately above implies ~4.7k of ~21.3k = 22 %. Same concept, two denominators, two inches apart.

Sources: `web/src/components/LibraryStatsCards.tsx:117` (`headline: stats.faces`) and `:139-143` (`markers_assigned` / `markers_unassigned`) versus `web/src/components/stats/CoverageMeters.tsx:45-49` (`done: stats.faces_assigned, total: stats.faces`).

## Requirements

- The page picks one grain for "how much of the face work is done" and uses it in both the card and the meter, so the two agree.
- The ~115k number is either dropped or renamed to something that is not "oblicej" — it counts detections, most of which never became markers. If it stays, the page says plainly how it relates to the other numbers.
- Any number a reader could reasonably try to add to another either adds up, or is visibly a different kind of thing.
- The Czech and English strings both change; no English leaks into the Czech UI.
- Test coverage for the derived percentages.

## Implementation Notes

The distinction between a detection, a marker and an assigned marker is real and worth keeping — the fix is to name the three honestly, not to collapse them. `internal/system` is where the counts come from if a different aggregate is needed.

## Constraints

- **No mutable operations against production data.** Read-only reads of `GET /api/v1/system/stats` are fine for checking numbers; writes are not.
- `make check` must pass. `make check-box` runs the same gate on the build box and is much faster.
