# M4 — Slideshow

Build a fullscreen slideshow for albums and labels with a configurable transition effect and
speed.

## Context
Read `docs/ARCHITECTURE.md` §1, §13 (slideshow). Backend: scoped photo listing (album/label) and
thumbnail/preview endpoints. react-bootstrap (Superhero), i18n, responsive/touch.

## Requirements
- Launch a slideshow from an album or label (and optionally from any filtered grid view) using
  the current ordering/filters.
- **Configurable transition effect** (e.g. fade / slide / none) and **speed/interval**; controls
  for play/pause, next/prev, and fullscreen. Persist the user's effect/speed preference locally.
- Preload upcoming images (use an appropriately sized preview) for smooth playback; handle large
  sets without loading everything at once.
- Keyboard (arrows, space, Esc) and touch (swipe) controls; works on mobile/tablet fullscreen.
- Loading/empty/error states; i18n (cs/en). Exiting returns to the prior view (Back works).

## Quality gate (mandatory)
- `make check` MUST pass (frontend ESLint + Vitest).
- Vitest tests: advances on interval (fake timers), play/pause, next/prev, effect+speed settings
  applied and persisted, keyboard handlers, graceful empty set. Mock API/timers.
- Typed components; all text via i18n.