# The review game — the page

Kukátko shows you one photo and asks one question: *"Je tohle Tomáš Kozák?"* or *"Má tahle fotka mít štítek
Ostatky?"* You answer, the next question appears. The point is **rhythm** — it should feel like flicking through
cards, not like filling in a form.

The backend serves the queue (`GET /api/v1/review/queue`) and takes the answers (`POST /api/v1/review/answer`).
**Confirm both exist before starting.**

## The page

New route **`/review`** (editor-only), prominently linked in the nav. Czech label "Třídění", English "Review".

**One question fills the screen.** No sidebar, no grid, no chrome competing for attention:

- The photo, **large** — as much of the viewport as fits. For a face question, the face is marked with a rectangle
  **with generous padding around it** (~30 %), because you cannot recognise a person from a tight crop; reuse the
  existing bbox geometry helpers (`web/src/lib/faceGeometry.ts`) rather than reinventing them.
- The question in plain language, big: **"Je tohle Tomáš Kozák?"** / **"Má tahle fotka mít štítek Ostatky?"** The
  name or label is the emphasised part — it is what the eye needs to land on.
- The system's confidence, shown honestly but quietly (a small percentage or a subtle bar). It is context, not the
  answer.
- Three actions: **Ano · Ne · Nevím**.

## Keyboard is the primary interface

The mouse is the fallback, not the other way round. This is what makes the game a game.

- **← = Ne**, **→ = Ano**, **mezerník / ↓ = Nevím (skip)**. Also accept `y` / `n`.
- **The next question must already be loaded when the current one is answered.** Prefetch the batch; never make the
  user wait between cards. A visible spinner between every question kills the whole feature.
- The answer is sent **optimistically** — the UI advances immediately, the request settles in the background. If it
  fails, surface it (a toast) and offer to retry; do not silently lose an answer and do not block the flow.
- **Undo the last answer** (`z` or Ctrl+Z) — a mis-hit arrow key is inevitable at speed, and an un-undoable
  permanent rejection is a trap. Undo calls the corresponding un-reject / unassign path.
- Register every shortcut in the existing `?` help overlay (`web/src/lib/shortcuts.ts`, `useKeyboardShortcuts`).
  Shortcuts must not fire while a text input is focused.

## Session feedback

Enough to make progress feel real, and no more:

- A counter of how many you have answered this session, and roughly how many candidates remain.
- A subtle progress indication as the batch is worked through.
- No score, no streak, no confetti, no badges. The reward is the library getting tidier — do not decorate it.

## States

- **Empty queue** — "Není se na co ptát", with a hint that naming a few people or creating a label gives the game
  material to work with. Distinguish this from an **empty library** ("nejdřív pojmenuj pár lidí a založ štítky"),
  which is what the backend reports separately. A generic "no results" here would send the user hunting a bug that
  is not there.
- **Batch exhausted** — fetch the next batch seamlessly; the user must not notice the boundary.
- **Loading the first batch** — a clean loading state, since this one wait is unavoidable.
- **Offline / failed fetch** — say so and offer retry; do not show a blank card.

## Accessibility and touch

- The three actions are real buttons with labels — the keyboard flow is an accelerator, not a replacement.
- The rectangle around the face must not be the only signal that carries meaning (state it in text too).
- On touch, the buttons are large and thumb-reachable at the bottom of the screen. Swipe gestures are optional and
  must never be the only way to answer.

## i18n

Every string into **both** `web/src/i18n/locales/cs/common.json` and `.../en/common.json` (namespace `common`,
Czech is the default). The question sentences are interpolated with the person's name / label name — build them as
i18n templates, do not concatenate strings.

## Verification

- Vitest: a face question renders with the padded bbox and the person's name; a label question renders with the
  label name; ← / → / space each send the right answer and advance; the next question is already in memory (assert
  no fetch happens between two answers within a batch); undo reverts the last answer through the right endpoint; a
  failed answer surfaces an error without losing the user's place; the empty-queue and empty-library states render
  distinctly.
- `make check` must pass (ESLint strict, Prettier, Vitest, typecheck).
- Document: `docs/FRONTEND.md` (the page, its components and hooks) and `README.md` — this is the most user-visible
  feature in the batch.

## Out of scope

- Album questions (faces and labels only).
- Any gamification beyond the session counter.
- Changing how faces or labels are assigned — the game answers route through the existing write paths.