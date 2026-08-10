# Review tools — shared UX implementation plan

> Executed inline in this session. Spec: `docs/superpowers/specs/2026-08-10-review-tools-shared-ux-design.md`.

**Goal:** every review tool gets the same three things — a photo you can enlarge, a density
stepper, and a click-through to the photo's page — by distributing what `/review` already has.

**Architecture:** extract `/review`'s stage into `ReviewStage`, build `ReviewLightbox` on it,
add one shared `REVIEW_GRID_SCOPE`, then wire the tools group by group.

**Tech Stack:** React 19 + TS, react-bootstrap, vitest + RTL, i18next (cs default, en).

## Global constraints

- `make check` green before every commit; `make dev` before the last one. Never `prettier --write` on `docs/*.md`.
- Every user-visible string goes through i18next in **both** `cs` and `en`.
- Icons only via the `Icon` component (bootstrap-icons), decorative ones `aria-hidden`.
- Full frames come from `fit_*`; a bbox may never be drawn over a centre-cropped `tile_*`.
- No `any`. Tests are mandatory for every change.
- Deviation from the spec, decided here: **phase 4 collapses into phase 1** — the extraction
  rewires `ReviewPhoto` in the same commit, so the game's existing tests guard it. There is no
  separate "adopt the stage" step.

---

## Commit A — primitives (tasks 1–4)

### Task 1: `ReviewStage`

**Files:** create `web/src/components/review/ReviewStage.tsx` + `.test.tsx`; modify
`web/src/components/review/ReviewPhoto.tsx` to render it.

**Produces:**
```ts
export interface ReviewStageProps {
  photoUid: string
  orientation: number          // raw EXIF 1–8
  fileWidth: number
  fileHeight: number
  size: ThumbSize              // 'fit_720' | 'fit_1280'
  bbox?: Bbox                  // normalised [x,y,w,h] in display space
  padBox?: boolean             // pad ~30 % around the face (default true)
  href?: string                // renders the corner anchor out to the photo's page
  frameStyle?: CSSProperties   // caller's sizing; the game caps against 100cqh
  alt: string
}
export function ReviewStage(props: ReviewStageProps): JSX.Element
```
The photo itself is never wrapped in a link — the corner anchor is the only way out, exactly
as the game has it today.

- [ ] Test: box waits for the measured frame (nothing drawn before load), the corner anchor renders `target="_blank" rel="noopener noreferrer"` and is absent without `href`, the `<img>` has no ancestor anchor, `padBox` widens the drawn rect vs the raw bbox.
- [ ] Run `npx vitest run src/components/review/ReviewStage.test.tsx` → fails.
- [ ] Implement by moving the body of `ReviewPhoto` (keep `useImageFrame`, `faceBoxStyle`, `padBbox`).
- [ ] Rewire `ReviewPhoto` to `ReviewStage` with `size={REVIEW_PREVIEW_SIZE}`; its existing tests must pass untouched.
- [ ] Run `npx vitest run src/components/review src/pages/ReviewPage.test.tsx` → passes.

### Task 2: `useLightbox`

**Files:** create `web/src/hooks/useLightbox.ts` + `web/src/hooks/useLightbox.test.ts`.

**Produces:**
```ts
export interface Lightbox<T> {
  item: T | null
  index: number                // -1 when closed
  open: (index: number) => void
  close: () => void
  next: () => void             // stops at the last item
  prev: () => void             // stops at the first
  isOpen: boolean
}
export function useLightbox<T>(items: T[]): Lightbox<T>
```

- [ ] Test: open/close; `next` stops at the end and `prev` at the start (no wrap); a shrinking list closes rather than pointing past the end.
- [ ] Run → fails. Implement. Run → passes.

### Task 3: `ReviewLightbox`

**Files:** create `web/src/components/review/ReviewLightbox.tsx` + `.test.tsx`; i18n keys
`review.lightbox.{close,prev,next}` in `cs` + `en`.

**Produces:**
```ts
export interface ReviewLightboxProps {
  stage: ReviewStageProps      // what to draw, incl. its href for the corner anchor
  title?: ReactNode            // caption line above the photo (e.g. the match percentage)
  onClose: () => void
  onPrev?: () => void
  onNext?: () => void
  children?: ReactNode         // the caller's action buttons (footer)
}
```
Owns: `Esc` → `onClose`, `←`/`→` → `onPrev`/`onNext`, a backdrop click → `onClose`. The way out
to the photo's page is the stage's own corner anchor — the lightbox adds no second one.

- [ ] Test: renders the stage, `Esc` calls `onClose`, arrows call the callbacks, a backdrop click closes while a click on the photo does not, footer children render and their clicks fire.
- [ ] Run → fails. Implement. Run → passes.

### Task 4: `REVIEW_GRID_SCOPE`

**Files:** modify `web/src/lib/gridDensity.ts` (+ `gridDensity.test.ts`), `web/src/pages/OutliersPage.tsx`,
`web/src/components/library/GridDensityControl.tsx` docstring.

- [ ] Replace `OUTLIER_GRID_SCOPE` with `REVIEW_GRID_SCOPE` (`storageKey: 'kukatko.review.density'`,
      same tile/gap numbers). Update the docstring: shared by every review tool, still separate
      from the library.
- [ ] Update `gridDensity.test.ts` — the test named for the outlier key asserts the review key now.
- [ ] `OutliersPage` imports the new scope (no behaviour change beyond the one-time reset).
- [ ] Run `npx vitest run src/lib/gridDensity.test.ts src/pages/OutliersPage.test.tsx` → passes.

- [ ] **Commit A** after `make check` green.

---

## Commit B — the candidate family (`/faces`, `/recognition`, `/expand`)

**Files:** `web/src/components/faces/CandidateCard.tsx`, `.../CandidateResults.tsx`,
`web/src/pages/FacesPage.tsx`, `web/src/components/recognition/PersonSweepCard.tsx`,
`web/src/pages/RecognitionPage.tsx`, `web/src/components/people/Candidates.tsx`,
`web/src/pages/ExpandPage.tsx` + their tests. New i18n key `review.card.enlarge`.

- [ ] `CandidateCard` gains `onEnlarge: () => void`; the photo becomes a `<button>` wrapper
      (keyboard reachable, `aria-label` = `review.card.enlarge`) — **not** a link, since a click
      enlarges here.
- [ ] `CandidateResults` + `PersonSweepCard` + `Candidates` take `density` and render
      `gridTemplateColumns(density)` with `REVIEW_GRID_SCOPE.gapPx` instead of the fixed `minmax`.
- [ ] Each page: `useGridDensity(REVIEW_GRID_SCOPE)`, `<GridDensityControl scope={REVIEW_GRID_SCOPE} />`
      in the results header, `useLightbox` over the same `visible` list the grid renders, and
      `useKeyboardShortcuts(..., { enabled: … && !lightbox.isOpen })` so grid movement does not
      fight the lightbox.
- [ ] The lightbox footer carries that page's own confirm/reject buttons, calling the same
      handlers the card does.
- [ ] Tests per page: the stepper changes `gridTemplateColumns`; clicking a photo opens the
      lightbox; confirming from inside it calls the same mocked service the card does.
- [ ] Browser check (see below), then **Commit B** after `make check` green.

---

## Commit C — `/outliers`

**Files:** `web/src/components/people/OutlierCard.tsx`, `web/src/pages/OutliersPage.tsx` + tests.

- [ ] Photo becomes the enlarge control; the `outliersPage.card.openPhoto` text link is removed
      (the lightbox header carries it now). Keep the i18n key only if still referenced.
- [ ] `OutliersPage` wires `useLightbox` over `items`, footer = unassign/confirm.
- [ ] Tests updated for the removed link and the new opening gesture.
- [ ] Browser check, then **Commit C** after `make check` green.

---

## Commit D — `/people/clusters`, `/duplicates`, `/duplicate-markers`

**Files:** `web/src/components/people/ClusterCard.tsx`, `web/src/pages/ClustersPage.tsx`,
`web/src/components/duplicates/DuplicateGroupCard.tsx`, `web/src/pages/DuplicatesPage.tsx`,
`web/src/components/people/DuplicateMarkerGroupCard.tsx`, `web/src/pages/DuplicateMarkersPage.tsx`
+ tests.

- [ ] Lightbox on every photo; the stage always uses `fit_*`, never the card's `tile_224`.
- [ ] Density: the cluster grid on `/people/clusters`; the **member grid inside a group card**
      on the two duplicate pages. Verify this reads well in the browser before committing —
      the spec flags it as the one judgement made without seeing it.
- [ ] Browser check, then **Commit D** after `make check` + `make dev` green.

---

## Verification ritual (every commit that touches a page)

1. `npx vitest run <touched test files>` while developing (fast loop).
2. Throwaway Vite proxy to production, as used for the `/library/*` fix:
   `web/vite.proxy.mjs` with `changeOrigin: true` at `https://fotky.kotrzina.cz`, port 4390.
3. Sign in as the maintainer: `op read "op://Pan Botka/Kukatko/password"` (user `panbotka`)
   with `OP_SERVICE_ACCOUNT_TOKEN` exported from `/home/pi/.openclaw/workspace/.secrets/op_service_account.env`.
4. Drive `agent-browser`, screenshot each changed tool, confirm: stepper moves the columns,
   photo click opens the lightbox, `Esc` returns, the decision buttons still write.
5. Delete `web/vite.proxy.mjs`, kill the dev server, `make check`, commit.
