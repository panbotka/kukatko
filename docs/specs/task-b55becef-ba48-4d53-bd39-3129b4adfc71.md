# Review game: wrong reason on the empty labels tab

Verified on the box staging (commit 68e805e, 2026-09-06). The library there has 10 labels with 6–18 photos each, every one with `review` enabled, and embeddings for all 398 photos. Yet the review game's Štítky tab shows the empty state "Zatím tu nejsou štítky — Hra se teď ptá jen na štítky, ale žádný štítek zatím nemá fotky — nebo jsou všechny na stránce Štítky z třídění vypnuté." and `GET /api/v1/review/queue?source=labels` answers `{"questions":[], "reason":"no_labels", …}`. The message blames the wrong thing: labels exist, have photos and are enabled; there simply are no label candidates in the confidence band right now. Screenshot: `/mnt/nas-botka/kukatko-stage-test/38-review-labels.png`.

## Where to look

- `internal/review/source.go` returns `ReasonNoLabels` at ≈ lines 117 and 139; `internal/review/review.go` ≈ lines 324–330 defines the reasons (`no_people_no_labels`, `no_labels`, …) and the doc comment at ≈ line 486 mentions `ReasonNoLabels` or `ReasonNoCandidates`.
- The frontend maps the reason to the text in `web/src/pages/ReviewPage.tsx` (empty state with the "Štítky" and "Ptát se na lidi" buttons).

## Requirements

- Find out which condition in `source.go` turns "labels with photos, review on, no candidates" into `no_labels`. Either the eligibility test is wrong (fix it) or the empty candidate pool is being reported under the wrong reason (report it as the candidates reason instead). Write down which it was in the commit message.
- The API must distinguish at least: no label is enabled for review / no label has photos (`no_labels`) from "labels are fine but nothing sits in the band right now" (`no_candidates` or the existing equivalent). Document the reason values in `docs/API.md`.
- The empty-state text must match the reason: for the candidate case say what is true, e.g. "Pro štítky teď není co posuzovat — žádná fotka nesedí do pásma nejistoty. Zkus to po dalším nahrání, nebo se nech ptát na lidi.", with the same two buttons. Both locales.
- Tests: a unit test in `internal/review` for a library with enabled, populated labels and an empty candidate band asserting the new reason; the existing `no_labels` cases keep passing; Vitest for the message mapping.
- Docs: `docs/API.md` (reason enum), `docs/PACKAGES.md` (`internal/review`) if the rule changes.

## Out of scope

- Do not tune the confidence band or thresholds; that is `docs/THRESHOLDS.md` territory and not this bug.

## Implementation notes

- Definition of Done per `CLAUDE.md`: docs, `make check` (`make check-box` on the build box), commit, push. Skip `make dev`.