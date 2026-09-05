# The tile selection checkbox is a 41 px touch target against the project's own 44 px floor

This is a small, marginal finding. Read the reasoning before deciding to act — closing it
as "not worth changing" is an acceptable outcome if the alternative is worse, but say so
explicitly rather than leaving it half-done.

## Measurement

`.kk-tile__check` in the library grid renders 26x26, but a `::before` pseudo-element with
`inset: -4px -13.6px -13.6px -4px` enlarges its hit area. Sweeping outward from the centre
with `elementFromPoint` on a phone viewport with real touch emulation active
(`pointer: coarse` true, `maxTouchPoints` 5) gives an effective hit region of **41x41**.

`web/src/styles/tapTargets.test.ts` sets `TOUCH_FLOOR_PX = 44`, so the control is 3 px under
the standard the project enforces on its other controls.

The expansion is also asymmetric — `-4px` top and left against `-13.6px` right and bottom —
so the touch centre sits below and to the right of the visible control.

## Requirements

- Either bring the effective hit region to at least 44x44 and centre it on the visible
  control, or record a deliberate exception with the reason.
- If you enlarge it, the checkbox must not start stealing taps intended for the photo tile
  itself. The tile opens the photo; the checkbox selects it, and those two targets sit on
  top of one another. Verify the tile is still comfortably tappable across its area after
  the change.
- The visible size of the checkbox should not grow noticeably — the grid is dense and the
  control is deliberately unobtrusive. This is about the hit area, not the artwork.
- Do not change the desktop appearance or behaviour.

## Testing

Extend `web/src/styles/tapTargets.test.ts` to cover this control, or, if you take the
exception route, add the exception there with a comment explaining it so the next person
does not re-open the question.

## Note for verification

`agent-browser set device` gives a mobile viewport and user-agent but leaves
`maxTouchPoints` at 0, so `@media (pointer: coarse)` never matches and any tap-target
measurement taken that way is wrong. Touch emulation has to come from
`Emulation.setTouchEmulationEnabled` held open in the same CDP session as the measurement,
because the override is dropped when the session that set it detaches.
