# The phone timeline rail must stop covering the Filters button

On the library's arrival screen on a phone, the fixed timeline rail lies across the right 40 px of the "Filtry" button — 38 % of it. A tap aimed at the button jumps the grid by tens of thousands of pixels instead of opening the drawer.

## Evidence (observed on production, 2026-08-12, 390x844 with a genuinely coarse pointer)

- The "Co je noveho" digest renders above the filter row and pushes "Filtry" down to y=194-242.
- The rail is `position: fixed; top: 148px; right: 0; width: 40px; z-index: 1018`, spanning y=148-774.
- Overlap is 40 px horizontally; only 61.5 % of the button's tap surface is reachable.
- Tapping (378, 218) — visually inside "Filtry" — hit `span.kukatko-timeline-mark` for "Prejit na pro 2019 - cvn 2020" and scrolled the library to `scrollY 142192`.
- Causal proof: hiding the digest moves "Filtry" to y=69-117, above the rail, and the overlap disappears; restoring it brings the overlap back.

Root cause: `web/src/styles/app.css:1119-1125` hard-codes the rail's top as `calc(navbar + safe-area + 6rem)`, assuming nothing ever renders between the navbar and the filter row.

**This is a regression against two written claims.** That same CSS comment says the offset exists precisely so that "a tap aimed at the button would jump the grid to 2014 instead" cannot happen, and `docs/UX_AUDIT.md` records the phone rail as fixed, "the grid reserves the lane, so it covers no tile and steals no tap". Both are false in production.

**It is not only the digest.** `web/src/components/Layout.tsx:345-346` renders `<AnnouncementBanner />` into the same slot, so an active instance-wide announcement makes the collision permanent rather than once per visit.

## Requirements

- The rail derives its top edge from the measured height of whatever precedes the grid, rather than from a constant — or the header reserves the lane so the two can never overlap.
- Verified with the digest present, with an announcement banner present, with both, and with neither.
- No tap aimed at any control in the filter row can reach the rail.
- Update the claim in `docs/UX_AUDIT.md` so it describes what the code now actually does.
- Regression test covering the "something rendered above the filter row" case.

## Constraints

- **No mutable operations against production data.** Read-only browsing of production is allowed for verification; writes of any kind are not.
- Related but separate task: the rail's own tap targets are undersized. Do not undo this fix when that one lands.
- `make check` must pass. `make check-box` runs the same gate on the build box and is much faster.
