# Album and label links in the photo panel are 12 px tall

The one place in the app where the 44 px touch floor does not reach is the chip that answers "which album is this photo from?".

## Evidence (observed on production, 2026-08-12, 390x844 with a genuinely coarse pointer active)

- In the photo info sheet, the album/label chip's `<a>` measures **79.1 x 12.0 px** (`padding: 0`, `font-size: 12px`), inside a pill measuring 111 x 20.9 px.
- Both the link and the pill are under the 24 px WCAG AA floor, and far under the app's own 44 px coarse-pointer floor.
- The reason the floor misses them: the rule targets `.btn`, not `.badge a` — a gap already noted in a comment at `web/src/styles/app.css:626`.
- Every other control in the viewer measured correctly at 44 x 44 or 52 x 52, so this is the single hole, not a pattern.

Sources: `web/src/components/photo/OrganizePanel.tsx:174-193` (albums) and `:210-224` (labels).

## Requirements

- The album and label chips meet the app's coarse-pointer floor.
- The whole pill is the tap target, not a small link inside a larger decorative shape.
- Visual weight stays close to today's — these are secondary metadata, and the fix should not turn them into buttons that compete with the primary controls.
- The gap in the floor rule is closed at its source, so the next component that uses a badge-with-link inherits the fix instead of repeating the bug.
- A test pins the resulting target size under a coarse pointer.

## Implementation Notes

The sibling component `web/src/components/photo/OrganizeBadges.tsx:38-42` already solved exactly this by making the pill itself be the link. Reuse that shape rather than inventing a third one.

Measuring requires genuinely forcing `pointer: coarse` — see the note in the timeline-rail tap-target task; `agent-browser set device` alone reports desktop sizes.

## Constraints

- **No mutable operations against production data.** Read-only browsing of production is allowed for verification; writes of any kind are not.
- `make check` must pass. `make check-box` runs the same gate on the build box and is much faster.
