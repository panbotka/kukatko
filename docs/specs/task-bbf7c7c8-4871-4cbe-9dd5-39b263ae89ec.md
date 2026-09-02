# Instant Lightbox

Flipping through photos in the detail view is the most common gesture in the app — make it feel instant.

## Requirements

- Preload: while a photo is open, its immediate neighbors (previous and next in the current browse order) preload in the background so arrow/swipe navigation shows them with no visible wait. One each side is enough; never preload video files.
- Progressive display: on navigation, immediately show the best already-cached smaller rendition (e.g. the grid thumbnail) scaled to fit, then swap in the full-size image when it arrives — no gray flash, no layout jump.
- Touch zoom: pinch-to-zoom and double-tap-to-zoom on the photo, with panning while zoomed; double-tap again resets. Swipe-to-navigate must not fire while zoomed in.
- Desktop keeps its current interactions; wheel/button zoom is optional, only if it drops out naturally.
- Slideshow and existing controls (info drawer, arrows, keyboard shortcuts) keep working.
- Tests where feasible (preload-trigger logic, zoom state machine as pure logic); verify gestures via a real browser probe if unit tests can't cover them.

## Implementation notes

- This is plain image preloading — do not add new paths that hold originals in RAM on the server.
- Update docs/FRONTEND.md.