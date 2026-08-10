# One UX for every review tool — design

**Date:** 2026-08-10
**Status:** approved

## Problem

Kukátko has grown a family of tools that all ask the same question — *is this the right
person / the right photo?* — and each answers it with a different set of controls:

| tool | grid density | enlarge | open the photo's page |
| --- | --- | --- | --- |
| `/faces` | ✗ fixed `minmax(16rem, 1fr)` | ✗ | ✗ |
| `/recognition` | ✗ fixed `minmax(16rem, 1fr)` | ✗ | ✗ |
| `/expand` | ✗ fixed `minmax(14rem, 1fr)` | ✗ | keyboard only (`ExpandPage.tsx`) |
| `/outliers` | ✓ own stored key | ✗ | a small text link in the card body |
| `/people/clusters` | ✗ | ✗ | ✗ |
| `/duplicates` | ✗ | ✗ | a link on one member |
| `/duplicate-markers` | ✗ | ✗ | a link |
| `/review` (the game) | — one photo at a time | not needed, the photo is the screen | ✓ click the photo, or `o` |

Two gaps hurt in daily use. **You cannot enlarge a photo when you are not sure** — the one
moment the tool exists for — and **the density is not yours to pick** on six of the seven
grids, although every browsing list in the app lets you set it.

The reported symptom was `/faces` and `/recognition`; `/outliers` was reported as missing the
click-through, which turns out to exist as a small text link under the photo. That it reads as
missing is the same defect in another form: the affordance is not where the hand looks for it,
which is **on the photo**.

## The insight this design rests on

`/review` already solved this. `components/review/ReviewPhoto.tsx` draws a `fit_1280` stage
with the face box padded ~30 % (a tight crop is unrecognisable), and a quiet anchor in the
frame's **top-right corner** leads to the photo's own page in a new tab; the `o` key does the
same from the keyboard.

That the corner anchor is not the whole photo is deliberate, and its docstring says why: the
preview carries the face rectangle, so *a click into the preview must never be ambiguous*.
This design keeps that rule rather than overriding it.

So this is not a design problem, it is a **distribution** problem: one tool has the vocabulary
and the others do not. Nothing new gets invented here — the game's stage is extracted and
handed to everyone else.

## The gesture vocabulary

Two rules, everywhere:

- **The photo is never a link.** Getting to the photo's own page is always the same quiet
  corner control the game already has (`.review-photo__open`, a real anchor so right-click →
  copy address and Ctrl/middle-click work), plus the `o` key. One control, one shape, one
  keystroke, on every tool.
- **A small photo in a grid → click enlarges it** in a lightbox over the page. This is the only
  new gesture, and it is unambiguous precisely because the corner anchor owns "leave". The
  review list, the sweep and every pending decision stay exactly as they were.
- **In the lightbox:** `←` / `→` step to the previous/next item **of the list the opened card
  belongs to** — on `/recognition` that is the one person's block, not the whole sweep — and
  they **stop at the ends** rather than wrapping: a lightbox that silently returns to the first
  item reads as a stuck control. `y` / `n` (and `✓` / `✗`) act on the shown item exactly as on
  the card. `Esc` closes and returns focus to the card it was opened from, as does a click on
  the backdrop outside the photo.

Inside the lightbox the photo is inert: nothing happens if you click it, because both meanings
it could carry are already taken by controls that name themselves.

The rule is memorable because it is size-based, not page-based: *small photo enlarges, large
photo opens.* It also keeps the game's existing behaviour untouched.

## Components

New, in `web/src/components/review/`:

- **`ReviewStage`** — the stage extracted from `ReviewPhoto`: `fit_*` preview, aspect ratio
  measured from the loaded image (never from the catalogue row, which can carry a transposed
  dimension pair), optional padded face box, and the corner anchor out to the photo's page.
  The caller supplies the frame's sizing style, which is the only thing that differs between
  the game (capped against `100cqh`) and the lightbox. `ReviewPhoto` is rewritten to use it, so
  the game and the lightbox draw literally the same code and cannot drift.
- **`ReviewLightbox`** — a full-viewport overlay around `ReviewStage`: a header with close and
  "open the photo", the caller's own action buttons in the footer, and the key handling above.
  It renders the item it is given and reports which action was chosen; it owns no review state.

New hook, `web/src/hooks/useLightbox.ts`: which item of a list is open, `open`/`close`/`next`/
`prev`, and the focus return. Pages keep owning their data; the hook owns only the pointer into
it.

In `web/src/lib/gridDensity.ts`: **`REVIEW_GRID_SCOPE`** (storage key `kukatko.review.density`),
replacing `OUTLIER_GRID_SCOPE`. One stored number for every review tool, separate from the
library's — judging faces and browsing a library are different jobs at different comfortable
densities, which is the argument `OUTLIER_GRID_SCOPE` already makes; what changes is only that
the review tools now share one number instead of `/outliers` having a private one. The old key
is not migrated: one tool's stored preference resets once, on the first visit after the deploy.

## Rollout, in four phases

Each phase is its own commit with a green `make check`, so a regression is bisectable to one
page group and the production instance is never mid-refactor.

1. **Primitives + the candidate-card family.** `ReviewStage`, `ReviewLightbox`, `useLightbox`,
   `REVIEW_GRID_SCOPE`; then `/faces`, `/recognition` and `/expand`, which share
   `CandidateCard` and `people/Candidates.tsx` — three pages for one change.
2. **`/outliers`.** Switch to the shared scope; the photo becomes the enlarge control and the
   card's text link goes, because the lightbox's corner anchor is now the one way out to the
   photo's page — the same one every other tool has.
3. **`/people/clusters`, `/duplicates`, `/duplicate-markers`.** These draw face crops and
   square `tile_224` thumbnails rather than full frames; the lightbox shows the full frame
   (`fit_*`) in every case, because a centre-cropped square is exactly what you cannot judge
   from. Density here means the grid each page actually has: the cluster grid on
   `/people/clusters`, and the **member grid inside a group card** on `/duplicates` and
   `/duplicate-markers` — those pages are a list of groups, and the row of members within a
   group is the thing worth making bigger. That is the one judgement in this design made
   without seeing it, so it is checked in the browser before the phase is committed.
4. **`/review`.** Adopts `ReviewStage`. No behaviour change — this phase exists to prove the
   extraction was faithful.

## Keyboard collisions

`/faces` already binds `←`/`→`/`h`/`l`/`j`/`k` to move the card focus and `y`/`n`/`Enter` to
decide; the game reserves `Escape`, `z`, `o`, `?`. While the lightbox is open it takes the
arrows and `Esc`, and the page's grid-movement bindings must not also fire — `useKeyboardShortcuts`
is given the lightbox's open state through its existing `enabled` option, the same way the pages
already suspend shortcuts during a running "confirm all".

## Testing

- **Unit:** `useLightbox` (open/close, stepping and that it stops at both ends, focus return)
  and `gridDensity` for the new scope, as pure logic.
- **Component:** `ReviewStage` (box placement waits for the measured frame; the `href` is a
  new-tab link) and `ReviewLightbox` (keys, the action callbacks, that `Esc` restores focus).
- **Page:** for each tool — the density stepper is present and changes the rendered
  `gridTemplateColumns`; clicking a photo opens the lightbox; acting from inside the lightbox
  reaches the same write path as the card, asserted through the mocked service.
- **Real browser:** the tools are editor-gated, so verification signs in as the maintainer
  account (`panbotka`, item "Kukatko" in the 1Password vault "Pan Botka") against production,
  driving this tree's frontend through a throwaway Vite proxy — the technique already used to
  verify the `/library/*` redirect. Every phase is checked on the real library before its
  commit, because the thing being fixed is a judgement made by looking at pixels.

## Out of scope

- Changing what any tool *decides* — every confirm, reject, unassign and merge keeps its
  current write path. This is presentation only.
- Virtualizing these grids. They are bounded by a search limit, not by the library size.
- The photo detail page itself, and the duplicate compare screen (`/duplicates/compare`),
  which is already a full-viewport comparison.
