# UX Audit — Kukátko

App-wide usability review focused on **ordinary, non-technical users**. The guiding
question for every screen: _would a person who is not a developer immediately understand
what this screen is for, be able to tap every control comfortably, and never feel
intimidated?_

- **Audience:** non-technical people managing their own photo/video library.
- **Method:** screen-by-screen review of every page in `web/src/App.tsx` routing, plus the
  shared shell (`Layout`) and styling layer (`web/src/styles/app.css`).
- **Lenses:** Clarity · Touch-friendliness · Consistency · Readability · States · Intimidation.
- **Impact / effort legend:** 🔴 high · 🟡 medium · ⚪ low.
- **Status tags:** ✅ **Done** (implemented in this pass) · 📋 **Backlog** (recommended,
  not yet done) · 🚧 **Out of scope** (tracked in a separate ticket — see the end).

> **Companion document.** This audit was written by reading the code. A second study,
> [`UX_RESEARCH.md`](UX_RESEARCH.md), was made the other way round — by using the live
> production instance on desktop and phone. The two do not overlap; where they touch,
> they cross-reference. Read this one for per-screen consistency, touch sizing and
> jargon; read that one for what breaks under 20 906 real photos.
>
> **Verification pass — 2026-08-05.** Every open (📋) item below was re-checked against
> production (app version 0.5.1) with a **viewer** account. Items are now marked
> `— verified still open (2026-08-05)`, `— no longer applies` (with the reason), or
> `— not verified in this pass` when a viewer could not reach the screen or force the
> state. Admin pages (`/import`, `/maintenance`, `/system`, `/trash`, `/duplicates`)
> return 403 to a viewer, so their items are all unverified.

The app is already in good shape on several fronts: **i18n discipline is excellent** (virtually
no hardcoded user-facing strings; cs/en parity enforced by `i18n.test.ts`), **empty/error copy
is uniformly friendly** and never leaks stack traces (except one spot, noted below), and there is
already a `.kukatko-tap-target` convention and a global responsive polish layer. The findings
below are therefore mostly about **consistency, touch sizing, and a few jargon leaks**, not
fundamental breakage.

---

## What this pass changed (implemented quick wins)

These safe, global improvements were applied in the same session as this audit:

1. **App-wide touch-target floor** (`styles/app.css`). A `@media (pointer: coarse)` rule now
   forces every `.btn`, form control, `.nav-link`, `.dropdown-item`, `.list-group-item-action`,
   `.page-link`, the `.navbar-toggler` hamburger, every `.btn-close` dismiss X and checkbox to
   clear the ~44 px finger-friendly minimum on phones/tablets — **without** touching desktop
   layouts or any per-component markup. The close button grows as a `border-box`, so only its
   hit area changes and the glyph stays as small as it looks; the X inside a pill chip
   (`.badge .btn-close`) is exempt, because there the chip is the target and 44 px would burst
   the pill. This is the single highest-leverage fix for the pervasive `size="sm"` touch
   problem found on almost every page. _Extended 2026-08-12:_ a chip that **links** clears the
   floor too (`a.badge`, `.badge:has(> a)`, the link inside stretched by `.badge > a`) — the list
   above is by class, so an album/label chip stayed 12 px tall; see **Photo detail** below.
2. **Friendly landing page.** The landing page previously led with a **backend health check,
   version number and a raw git commit hash** — pure developer jargon as the very first screen.
   It became a welcome with large, labelled cards linking to the main destinations, and the
   technical status was demoted to a small, muted line at the bottom (status + version only, no
   commit hash). _Superseded:_ see the **Home** section below — the card grid is gone and `/`
   now renders the photo library itself.
3. **Primary-action prominence.** The primary "create" call-to-action on the Albums, Labels and
   People index pages was `size="sm"` (visually minor). Those primary buttons are now full size.
4. **Heading consistency.** Photo detail's title was `h1.h4`; every other page uses `h1.h3`. It
   now matches.
5. **Empty-state consistency.** The subject page's "no photos" state was a bare left-aligned
   paragraph; it now uses the same centered friendly block as every other empty state.
6. **Plain-language wording** (cs + en), removing developer jargon from user-facing copy:
   - `home.subtitle` — dropped the internal "replacement for X" comparison.
   - `search.degraded` — removed "the inference service is offline / semantic / full-text"
     jargon → "Search by content is temporarily unavailable… Showing text-based results."
   - `clusters.empty.hint` — dropped "detected and clustered" → "once the app recognizes faces".
   - `photo.location.none` — dropped "geotag" → "Add it on the Info tab."

Larger or riskier recommendations are **documented below and in the backlog rather than
implemented**, per the task's conservative-changes rule.

---

## Shared shell — `Layout` (navbar)

- **Clarity:** Good. Library / Albums / Labels sit top-level (the three ways the library is actually
  browsed); the rest collapses into Browse, and role-gated Tools / Admin groups are hidden from roles
  that cannot use them.
- **Touch-friendliness:** Nav links and dropdown items carry `kukatko-tap-target`; the coarse-pointer
  floor also covers `.nav-link` and the collapsed burger menu. ✅
- **Consistency:** ~~`app.name` brand doubles as the "Home" link — fine, but there is no visible
  "Home"/"Domů" entry in the bar itself.~~ — **no longer applies:** the bar now carries a
  labelled **Knihovna** entry pointing at `/`, which *is* the home destination since the
  library became the landing page. Nothing further to do.
- **Readability / States / Intimidation:** Every entry pairs a bootstrap-icons glyph with an
  action-describing `title`, so daily users recognise entries by shape.
  ~~⚠️ **Correction (2026-08-05):** this section used to claim that searching and saved searches
  "are reached from `/search`". They are not reachable at all from the bar — `/search` itself
  has no nav entry in any role, only the unlabelled magnifier icon, and `/saved` sits one level
  inside `/search`. Meanwhile `Žebříček` holds a top-level slot for a page with one player.~~
  — **fixed (2026-08-07):** **Hledání** is a labelled top-level entry (bar and phone drawer,
  and the phone tab bar's third slot), **Uložená hledání** sit in „Procházet" beside the
  favourites, and `Žebříček` is demoted into the same group. See `UX_RESEARCH.md` **N3**. ✅

---

## Auth & entry screens

### Login (`/login`)
- **Clarity/States/Intimidation:** Exemplary. One centered card, two fields, one full-width
  primary button. Errors are mapped to three friendly messages (invalid / rate-limited / generic);
  raw API errors never surface.
- **Touch:** Full-size, full-width submit (`d-grid`). Good.
- 📋 `autoFocus` on username can force the mobile keyboard open on load — consider dropping on
  touch. ⚪⚪ — **verified still open (2026-08-05)**: on load `document.activeElement` is the
  username input (focused from JS, not via the `autofocus` attribute).

### Home (`/`)
- **Was the biggest single issue.** The landing page's centerpiece was "Backend status /
  Version / Commit `<hash>`" plus a "replacement for X" subtitle — the most intimidating,
  least actionable screen for a non-technical user, with **no primary navigation at all**.
- ✅ **Done (first pass):** rewritten as a welcome with destination cards; technical status
  demoted to a muted footer. 🔴🟡
- ✅ **Done (follow-up):** the card grid is gone too. `/` now renders the **photo library** —
  the thing the app is for — so the photos greet the user instead of a menu of links to them.
  `/library/*` survives as a replacing redirect (query string preserved) for old bookmarks — both
  Kukátko's own and the ones inherited from the previous instance, whose whole UI lived under `/library/…` —
  the navbar's Knihovna entry points at `/`, and the health badge + build version moved to
  `/account`. An empty catalog gets its own empty state pointing at Upload, distinct from the
  "no photos match these filters" one. `App.test.tsx` covers the routing.

### Account (`/account`)
- **Clarity/States:** Clear two-section layout (identity + change password). Thorough inline
  validation and mapped error messages.
- **Consistency:** The submit button is **not** full-width here, unlike Login's `d-grid`. 📋 Align
  the two password forms. ⚪⚪ — **verified still open (2026-08-05)**: "Změnit heslo" measures
  117 px inside a 614 px container, while Login's submit is 430 px in a 430 px `d-grid`.

### Not found (`*`)
- Friendly, clear recovery link. Uses a raw `className="btn btn-primary"` on a `Link` and a
  `display-5` heading — cosmetically off-pattern but harmless. 📋 Use `<Button as={Link}>` and
  `h1.h3` for consistency. ⚪⚪

---

## Library & browse

### Library (`/`)
- **Clarity:** Title + filter bar + virtualized grid; browsing tiles is self-evident. The header
  actions (Slideshow / Save view / Select) are all `outline-secondary size="sm"` — **nothing reads
  as the page's primary action**, though for a browse screen the content _is_ the action.
- **Touch:** Header packs three small buttons at `gap-1` (4 px) — mis-tap risk. The coarse-pointer
  floor now enlarges them. ✅ 📋 Also bump the header `gap-1`→`gap-2`. ⚪⚪
- **Consistency:** Mixes `<Link className="btn…">` and `<Button>` for visually identical controls.
  📋 Standardize on `<Button as={Link}>`. 🟡⚪
- **States:** All present and friendly.
- **Time axis — fixed:** the filter bar used to carry *two* controls meaning "when was this taken":
  a **Rok** `<select>` of 109 single years in the primary row, and a **Pořízeno od / do** pair one
  click deeper in Filtry. Neither was right: for a library reaching back to 1905 the visible control
  could not express a decade ("babiččiny fotky ze šedesátek"), the one that could was hidden, and the
  two never synchronised with a `year:1960-1969` typed into the search box. They are now one
  **Období** control (`PeriodFilter`) in the primary row — decades expandable to years, exact dates
  pinned under them, one `taken_after`/`taken_before` pair in the URL — and it *reads the period out
  of the query* rather than contradicting it. ✅
- **The phone screen — fixed:** on a 393 × 852 phone the first photo used to start at **371 px**
  and the tab bar took 54 more, leaving 53 % of the screen for photographs. Above them: the
  „Knihovna" heading with Slideshow and Uložit pohled, the search field, the sort select, the
  density stepper next to Filtry, a two-line note about the search box, and „Počet fotek: 20637".
  Six rows of chrome for one field and one button's worth of daily use — on the device the library
  is actually browsed on. Now, below `md`: the heading is `visually-hidden` (the tab bar already
  says where you are), the view actions, the sort, the density and the note are in the Filtry
  drawer, and the count rides in the header row under the Filtry button. Measured again on
  production at 393 × 852: the first photo row starts at **150 px**, 76 % of the screen. ✅
- **The phone had no timeline — fixed:** `.kukatko-timeline` was `display: none` below 576 px, so
  the one control that can skip a 369 018 px list to 1965 existed only where it was least needed.
  A phone rail replaces the hiding: a 2.5 rem strip of year labels on the right edge (the grid
  reserves the lane, so it covers no tile and steals no tap), faint at rest and at full strength —
  plated, with the month bubble — for 1.6 s after any scroll or touch. ✅
- **The rail covered the Filtry button — fixed (2026-08-12):** the lane holds for the *tiles*, but
  the rail is `position: fixed` and started at a constant 6 rem below the navbar, which is the height
  of the filter row **and nothing else**. Anything above that row moved it under the rail: on the
  arrival screen the „Co je nového" digest pushed **Filtry** to y=194–242 while the rail began at 148
  — 40 px of overlap, 38 % of the button — and a tap at (378, 218), visually inside it, hit a year
  tick and scrolled the library to 142 192 px (measured on production, 390 × 844, coarse pointer).
  An instance announcement renders into the same slot, which would have made it permanent rather than
  once per visit. The rail now **measures** the grid box it scrubs (`gridWrapRef` → `useGridTop` →
  `--kukatko-timeline-top`) and starts there, so whatever the page puts above the grid moves the rail
  down with it; once the header has scrolled away a CSS `max()` floor keeps it just below the navbar.
  Verified in Chromium at 390 × 844 with the digest, with an announcement, with both and with
  neither: `document.elementFromPoint` over the whole filter row returns the row's own controls. ✅
- **The rail's own targets were 16 px and 5 px tall — fixed (2026-08-12):** having stopped covering
  other people's buttons, the rail was still unusable as its own control. It sized its ticks for a
  *mouse* and handed them to a thumb: measured on production at 390 × 844 with a genuinely coarse
  pointer, **31 year ticks of 39.8 × 16.2 px at a 20.2 px pitch and 62 month ticks of 16.0 × 5.2 px**
  — 93 buttons in a 40 px strip, against a fingertip of 34–45 px. WCAG 2.2 SC 2.5.8 (AA) asks for
  24 × 24, and its spacing exception cannot rescue a 20 px pitch: 24 px circles centred on adjacent
  ticks intersect. On a rail where a mis-tap is a jump of tens of thousands of pixels with no undo
  but scrolling back, that is the app's primary way across time on a phone.
  CSS alone could not fix it — 44 px boxes at a 20 px pitch overlap, and the one *under* the finger
  would not be the one that answers — so the thinning moved into the layout: on a coarse pointer
  (`useCoarsePointer`) `buildRail` spaces its year labels by `TOUCH_TARGET_PX` instead of by a line of
  text, and only those ticks stay controls (`touchTargets` hands each of them the months of the small
  ticks it replaced, so no month becomes unreachable). The ticks in between keep being drawn — the
  rail still reads as a ruler — but as `aria-hidden`, unfocusable, click-through decoration, so a
  press there reaches the rail and scrubs at the month grain a tap has given up. A mouse keeps the
  dense rail it can aim at.
  Verified in Chromium at 390 × 844 with `pointer: coarse` forced over a persistent CDP session
  (`agent-browser set device` does not set it; the overrides are per-client and revert on
  disconnect) and confirmed by `matchMedia('(pointer: coarse)')` in the same evaluation:
  **17 controls, every one 44 × 44 px, none closer than 44.1 px**, each winning
  `document.elementFromPoint` at its own centre; 97 decorative ticks, all `aria-hidden` and all
  falling through to the rail; the strip and the lane the grid leaves for it both 44 px. The same
  page with `pointer: fine` still draws 114 ticks of 16.2 px / 5.2 px — the desktop rail, unchanged. ✅

### Favorites (`/favorites`)
- Simplest, cleanest page. Excellent action-guiding empty state ("Tap the heart on a photo…").
  No issues.

### Search (`/search`)
- **Clarity:** Prominent autofocus search field + mode selector; well-designed.
- **Intimidation:** Previously the **most jargon-heavy copy** — the degraded notice named "the
  inference service is offline" and "semantic/full-text". ✅ **Done:** reworded to plain language.
  📋 The **mode selector labels** ("Hybrid / Full-text / Semantic") remain technical — rename to
  plain terms (e.g. "Smart / By text / By meaning") and/or hide the selector behind an "advanced"
  toggle, defaulting everyone to the smart mode. 🔴🟡 — **verified still open (2026-08-05)**:
  the selector reads "Hybridní / Fulltext / Sémantické". The bigger problem behind it — with the box
  offline every hybrid query blocked for a **30 s** timeout even though `/capabilities` already
  reported `semantic_search: false` — is ✅ **Done (2026-08-07)**: the query now goes out as
  full-text straight away, "Sémantické" is `disabled` with an explanation, and the notice appears
  beside the selector before the search runs. See `UX_RESEARCH.md` **N1**.
- **Touch/States:** Field is full-size (good); header Save-view + retry are `size="sm"` (now
  floored on touch ✅).

### Saved searches (`/saved`)
- **Touch (weakest here):** Each row packs a `flex-grow-1` link + two `size="sm"` buttons at
  `gap-1` — Rename and destructive Delete adjacent with tiny targets. Coarse-pointer floor helps
  ✅, but 📋 widen the gap and separate Delete. 🟡⚪ — **not verified in this pass**: the audit
  account has no saved searches, so only the (good) empty state was observable.
- **Readability:** Row link uses `text-decoration-none` → tappable saved-search names don't look
  tappable. 📋 Add an affordance (icon or hover underline). 🟡⚪ — **not verified in this pass**,
  same reason.
- **Consistency:** Delete used a native `window.confirm` — unstyled vs. the app's own modals.
  ✅ **Done:** now routes through the shared `ConfirmModal` (see cross-cutting item 3).

### Places (`/places`)
- **Clarity/Touch:** Country/city rows are large full-width `ListGroup.Item action` targets —
  good. But the **breadcrumb links use `variant="link" p-0`** → small, tightly packed inline
  targets around "/" separators. 📋 Give breadcrumb links padding / a real breadcrumb component. 🟡⚪
  — **verified still open (2026-08-05)**: the "Místa" crumb is `btn btn-link p-0` measuring
  42 × 26 px.
- **Intimidation:** Empty hint mentions "GPS souřadnice" / "zpracování polohy" — mildly technical.
  📋 Soften. ⚪⚪ — **not verified in this pass** (the production instance has places, so the
  empty state never rendered).
- A grid skeleton is shown while loading what is actually a **list** — minor mismatch. 📋 ⚪⚪
- **New (2026-08-05):** for this library the whole page is *one row* ("Česko — 2 351 fotek") and
  the rows carry no thumbnails at all. See `UX_RESEARCH.md` **N23**.

### Map (`/map`)
- Clean. Loading/empty are overlays on the map (intentional divergence from full-page empties).
- **Readability:** The empty-state hint is `text-secondary` on `bg-dark` — lower contrast than
  elsewhere. 📋 Lighten. ⚪⚪ — **not verified in this pass** (the map has data). The *inactive*
  style tabs ("Turistická", "Letecká") do read as low-contrast, which supports backlog item #10.
- **New (2026-08-05):** the tile layer itself is light inside the dark app, and only 11 % of the
  library is on the map with no explanation. See `UX_RESEARCH.md` **N23**.
- **The phone screen — fixed (2026-08-12):** measured on production at 390 × 853, the controls above
  the map — three mapset tabs, two date pickers, an archive select and a two-line coverage sentence —
  came to **382 px, 44.8 % of the viewport**, leaving the map a minority of the screen. It was
  [N19](UX_RESEARCH.md#n19) again, on the page the library had already been cured of it. Now, below
  `md`: one row holding the title and a badged Filtry button, the controls in the drawer, and the
  coverage in short beside the button (in full, with the editor's link, inside the drawer). The map
  starts at **121 px** and is **597 px, 70 % of the screen**, with no scrolling at all. The heading
  stays visible here — unlike the library's, the map is not a tab-bar destination. ✅

---

## Organize

### Albums index (`/albums`) & Labels index (`/labels`)
- **Consistency:** Two sibling "index" pages render differently — Albums as a **card grid**, Labels
  as a **ListGroup**. Defensible (labels are lightweight) but worth a deliberate decision. 📋 ⚪⚪
  — **verified still open (2026-08-05)**, and at production scale it stops being cosmetic:
  Albums shows **438** cards in one flat unsorted grid, Labels shows **113** full-width rows over
  5 730 px, and neither page has a search box or a sort control. See `UX_RESEARCH.md`
  **N7** and **N10**.
- **Primary action:** Create CTA was `size="sm"`. ✅ **Done:** now full size.
- **Touch:** Label rows pack Rename + Delete `size="sm"` at the right edge (floored on touch ✅).
- States/copy: friendly and complete.

### Album detail (`/albums/:uid`)
- **Wayfinding:** The way out was a **bare arrow** (`←` + the destination's noun, "Alba") — a glyph
  that named neither the action nor where it led. ✅ **Done:** the shared `BackLink` (see Subject)
  renders an `arrow-left` icon plus a self-explanatory label ("Zpět na alba").
- **Touch (worst offender):** Up to **4 controls** (Slideshow, Edit, Select, Delete) in
  one `d-flex gap-1 flex-wrap`, all `size="sm"` (was 5 — the Reorder button left with manual
  album ordering; albums are now always chronological). The coarse-pointer floor enlarges them ✅, but
  📋 consider collapsing the editor actions into an **overflow "⋯" menu** on small screens to cut
  clutter. 🟡🟡
- **Intimidation:** Delete used a native `window.confirm` (reassuring copy, but unstyled).
  ✅ **Done:** now the shared `ConfirmModal`, whose confirm button reads "Smazat album" (see
  cross-cutting item 3).

### Label detail (`/labels/:uid`)
- Minimal and clear. No inline rename/delete (only on the index) — mild inconsistency with album
  detail. 📋 ⚪⚪
- **Wayfinding:** Same bare arrow as album detail. ✅ **Done:** the shared `BackLink` ("Zpět na
  štítky").

### Photo detail (`/photos/:uid`)
- **Consistency:** Title was `h1.h4` (smaller than every other page). ✅ **Done:** now `h1.h3`.
- **Touch:** Prev/next on-image nav (`‹`/`›`) rely on default button padding, positioned at image
  edges — can crowd small screens; rating/flag icons are small (18–22 px). Coarse floor helps the
  buttons ✅. 📋 Enlarge on-image nav hit areas / add `kukatko-tap-target`. 🟡⚪
  — **verified still open (2026-08-05)**: the arrows sit on the image edges and overlap it when
  a side panel is open.
- **Intimidation:** Child panels expose photographic jargon (Aperture/Exposure/Focal length/ISO,
  DMS coordinate help) and previously "Geotag" (✅ reworded). The EXIF terms are legitimate for a
  photo app but could get tooltips. 📋 ⚪⚪ — **verified still open (2026-08-05)**, plus three
  harder leaks found by using the page: `AI_MODEL: gemini-2.5-flash` rendered as part of the
  auto description, literal `Unknown` for camera/lens, and a raw SHA256 + source UID +
  lat/long. See `UX_RESEARCH.md` **N12** and **N26**. ✅ fixed 2026-08-08 for the first two:
  the model trailer is split off the description (and named in Technical details instead) and a
  stored `Unknown` renders no row at all.
- **New (2026-08-05), mobile only:** the faces and info panels are full-viewport opaque sheets on
  a phone, so the photo they describe is completely hidden, and the viewer's close button
  overlaps the panel title. See `UX_RESEARCH.md` **N6**.
- **Keyboard (2026-08-12):** on a photo opened without the info drawer, Tab walked into the *shut*
  drawer (tab 14 of 30, „Zavřít informace") and the browser scrolled the photograph 416 px off
  screen to reveal controls nobody can see, then kept it there for 17 more stops — while the panel
  was `aria-hidden`, i.e. a WCAG 4.1.2 violation too. ✅ **Done (2026-08-12):** the shut drawer is
  `inert` (plus `visibility: hidden` for older browsers), and closing it from inside hands focus
  back to the toggle that reopens it. Same element carries the faces and edit views, so both are
  fixed with it. 🔴 ⚪
- **Touch (2026-08-12):** the album/label chips in the info sheet were the one place the app's 44 px
  floor did not reach. Measured on production at 390 × 844 with a genuinely coarse pointer: the
  chip's `<a>` was **79.1 × 12.0 px** (`padding: 0`, `font-size: 12px`) inside a **111 × 20.9 px**
  pill — both under WCAG 2.2 SC 2.5.8's 24 px, let alone the app's own 44 px — while every other
  control in the viewer measured 44 × 44 or 52 × 52. The floor missed them because it enumerates
  classes (`.btn`, `.nav-link`, …) and a `.badge` is none of them. ✅ **Done (2026-08-12):** the
  chips are the shared `EntityChip`, whose **pill is the link** (the read-only strip's shape), and
  the floor gained `a.badge` / `.badge:has(> a)` — keyed on the tag, so the next chip that links
  inherits the target. Only the box grows: the type, the hue and the inline padding are the desktop
  chip's, because a chip is secondary metadata, not a button competing with the panel's actions.
  Verified in Chromium at 390 × 844 with `pointer: coarse` forced over a persistent CDP session
  (`agent-browser set device` does not set it) and confirmed by `matchMedia` in the same evaluation:
  **every chip 44 px tall** — a viewer's 113.1 × 44 where the pill *is* the link, an editor's 135.1
  × 44 pill whose link stretches the full 44 px beside the remove X — and the link answering
  `elementFromPoint` across the pill (46/50 sampled points, the misses being the rounded corners).
  The same page with `pointer: fine` still draws the 20.9 px pill: the desktop chip, unchanged. 🔴 ⚪

### Upload (`/upload`)
- **The model to copy.** Full-size primary/secondary buttons, proper h1→h2 hierarchy, friendly
  DropZone, `aria-live` progress summary, reassuring near-duplicate wording. No issues.

### Slideshow (`/slideshow`)
- Fullscreen player (no navbar/title — intentional). Friendly loading/empty/error gates with an
  exit. **Readability:** empty hint `text-secondary` on dark bg is low-contrast. 📋 Lighten. ⚪⚪

---

## People

### People index (`/people`)
- **Primary action:** "Review clusters" link was `outline-primary size="sm"` (under-emphasized).
  ✅ **Done:** now full size. Uses the plain-language "skupiny obličejů" (face groups) — good.
  (Not shown to a viewer, who has no access to clusters.)
- **States:** Error alert has **no retry** (must reload the page). 📋 Add retry. 🟡🟡
  — **not verified in this pass**: a read-only account cannot force the error state.
- **New (2026-08-05):** at 105 subjects the page has no search and no sort at all, and each
  152 px face tile is cropped out of a `fit_1920`/`fit_1280` source — 125 Mpx downloaded to
  paint 1,7 Mpx. See `UX_RESEARCH.md` **N8**.

### Subject (`/people/:uid`)
- **Touch / wayfinding:** Back was a **bare arrow + noun** ("← Lidé"), the smallest target on the
  page; Edit + Load-more are `size="sm"`. Coarse floor helps the buttons ✅. ✅ **Done:** every
  detail page (album, label, person, photo) now shares one `BackLink` — `arrow-left` icon (decorative)
  plus a label naming the destination ("Zpět na lidi"), a real `<Link>` with a hover underline, a
  focus ring, and a 44px minimum on coarse pointers.
- **States:** "No photos" was a bare paragraph. ✅ **Done:** now the standard centered block.
  **Set-cover failure is silently swallowed** — no feedback, unlike the visible `actionError`
  elsewhere. 📋 Surface a toast/alert. 🟡🟡 — **not verified in this pass** (write-gated).
- **New (2026-08-05):** the page offers no filter or sort over a person's photos, so "photos of
  X from the sixties" cannot be done from here; and it shows no photo count, unlike the index.

### Clusters (`/people/clusters`)
- Best-explained of the three (title + subtitle). **Intimidation:** empty hint previously said
  "clustered" (ML jargon). ✅ **Done:** reworded. Error state has no retry. 📋 Add retry. 🟡⚪
  — **not verified in this pass**: `GET /api/v1/faces/clusters` returns 403 to a viewer, so the
  whole page (and `/outliers`, `/review`, `/duplicates`) was out of reach.

---

## Admin & tools

These pages are admin-only, so a _technical_ operator is the audience — but the copy still leans
on unexplained jargon that even a non-developer admin will struggle with.

> **Not verified in the 2026-08-05 pass.** Every page in this section returns 403 to the viewer
> account used for the production walkthrough, so all 📋 items below are carried forward
> unchanged and unconfirmed. One data point does transfer: the "embeddings" jargon flagged here
> also appears on **`/stats`**, which is not admin-only — see `UX_RESEARCH.md` **N22**.

### Trash (`/trash`)
- **Touch (weakest admin page):** nearly every control is `size="sm"` — header, selection bar
  (3–5 buttons), per-card Restore / "Delete forever", retry, load-more. Coarse floor helps ✅.
- **States/Intimidation:** Excellent — the only page with a **properly styled confirm Modal**,
  sanitized errors, reassuring destructive-action copy, correct plurals. This is the confirm-flow
  model the other pages should follow.

### Duplicates (`/duplicates`)
- Best-explained page (full subtitle). **Intimidation:** the group card tooltips expose
  **"perceptual hash distance" / "embedding distance"** — meaningless to a lay admin. 📋 Reword to
  "how similar the photos look". 🟡⚪

### Import (`/import`)
- **Intimidation (highest raw-error risk):** the run-history table renders **`run.last_error`
  verbatim** in red — the one place a raw server/stack error reaches the UI, contradicting the
  sanitize-everything approach everywhere else. 📋 Truncate + wrap in a friendly "Import failed —
  details" disclosure. 🔴🟡 Jargon: "dead jobs", "embeddings", "the migration",
  "background processing queue". 📋 Add plain-language explanations / tooltips. 🟡🟡
- **Consistency:** first-run confirmation used a native `window.confirm` with un-localized
  OK/Cancel, vs. Trash's styled modal. ✅ **Done:** now the shared `ConfirmModal`; its confirm
  button carries "Spustit import" (non-destructive, so `primary`, not `danger`).

### Maintenance (`/maintenance`)
- **Most jargon-dense page:** "embeddings", "perceptual hashes", "orphan files/originals", "face
  detection", "missing thumbnails", plus scan `samples` that **dump raw file paths / UIDs inline**.
  📋 Add a plain-language explainer per repair option; collapse raw samples behind a "show details".
  🔴🟡
- **Touch:** repair `Form.Check` checkboxes are the tightest hit areas; the coarse-pointer rule now
  enlarges `.form-check-input`. ✅ 📋 Also enlarge the clickable label. ⚪⚪
- **States:** Well covered; a 503 (orphan import not configured) collapses into the generic error
  with no hint why. 📋 Distinguish. ⚪⚪

### System status (`/system`)
- Cleanest error hygiene (no raw errors surfaced). **Intimidation:** "dead-letter" → "Requeue dead
  jobs", "embeddings (box)", "box offline". The offline hint copy is genuinely good. 📋 A short
  glossary or tooltips for "box" / "dead jobs". 🟡⚪
- **Touch:** many `size="sm"` links-as-buttons (requeue, backup, import/maintenance links) — floored
  on touch ✅.

---

## Cross-cutting patterns

1. **Touch targets — systemic.** Almost every page defaulted to `size="sm"` with no
   `kukatko-tap-target`, well under the app's own 44 px standard. ✅ Addressed globally via the
   coarse-pointer floor; Upload remains the best per-page model (full-size buttons).
2. **`<Link className="btn…">` vs `<Button>`.** Slideshow/back/nav actions are rendered both ways
   for visually identical controls. 📋 Standardize on `<Button as={Link}>`. 🟡⚪
3. **Native `window.confirm` for destructive actions** (Album detail, Labels, Saved searches,
   Import first-run) — unstyled, un-localized OK/Cancel, jarring vs. Trash's styled modal.
   ✅ **Done:** one shared `ConfirmModal` (modelled on Trash) now covers all four call sites; its
   confirm button carries the action ("Smazat album" / "Spustit import"), destructive confirms are
   `danger` and never the default Enter target, and focus/Escape/restore are handled.
4. **Missing retry on error states** (People, Subject, Clusters) — user must reload. 📋 Add a retry
   button (they already have the fetch logic). 🟡🟡 — **not verified in this pass** (could not
   force the failure from a read-only account).
5. **Silent failures** (Subject set-cover) contradict the visible-error pattern used elsewhere.
   📋 Always surface an alert/toast. 🟡⚪ — **not verified in this pass** (write-gated).
6. **Jargon inventory to keep out of user copy:** "backend/commit", "inference service",
   "semantic/full-text", "clustered", "geotag" (✅ all fixed), plus still-present "perceptual
   hash / embedding distance", "dead jobs / dead-letter", "orphan files", "box". 📋
   — **extended 2026-08-05:** the list also has to cover copy that reaches *every* role —
   "Embeddingy" on `/stats`, `AI_MODEL: gemini-2.5-flash` and `Unknown` on photo detail (✅ both
   fixed 2026-08-08, see **N12**), and
   the flag buttons named after their glyph ("Oko", "Palec nahoru", "Označené okem" — ✅ fixed
   2026-08-08: they are named after the act now, "Vybrat"/"Zamítnout"/"Prohlédnout později",
   each with a one-sentence tooltip). See `UX_RESEARCH.md` **N11**, **N12**, **N22**.
7. **Muted-text contrast.** `text-secondary` subtitles/hints on the dark Superhero theme are on the
   low side, especially over `bg-dark` overlays (Map, Slideshow empties). 📋 Audit contrast; consider
   a slightly lighter muted token. 🟡🟡 — **partially verified (2026-08-05)**: the inactive map
   style tabs are hard to read; the two empty-state cases named here have data in production and
   could not be checked. — **the worst case is fixed (2026-08-12)**: `.btn-outline-secondary` (85 call
   sites, the app's quiet action) was Bootswatch's `#4e5d6c` label on the `#4e5d6c` card Bootswatch also
   bakes — contrast **1.00**, an invisible control, reported from the photo editor. `app.css` now re-points
   the label and the border; measured 5.6:1 text / 3.2:1 outline on a card, 15.4:1 / 6.1:1 on the page.
   The **card surface itself is still Superhero navy**, because that `--bs-card-bg` is baked on the `.card`
   selector and the tokens' re-pin cannot reach it — a separate, app-wide change, not done here.
8. **Heading hierarchy** is otherwise consistent (`h1.h3` titles, `h2.h5/h6` sections) after the
   photo-detail fix. ✅
9. **New (2026-08-05) — RBAC is invisible.** Routes a role cannot use redirect silently to the
   library, and controls it cannot use are `disabled` with no explanation. See
   `UX_RESEARCH.md` **N13** and **N14**. 🟡⚪ — ✅ **Done (2026-08-08).** Routes answer with
   `ForbiddenPage` on the address that was asked for (N13), and controls follow **one rule, both
   halves of it**: (a) *a control the reader's role forbids is not rendered at all* — the app
   already did this nearly everywhere (`OrganizePanel`, `PeoplePanel`, `FacesPanel`,
   `PhotoLocation`'s clear, the batch bars, the nav), so a greyed-out control now always means
   "not right now", never "not you", and the two are told apart without pressing anything; (b)
   *whatever is greyed out says why*, in one finished sentence, via the shared `ReasonedButton`
   (see `FRONTEND.md`) — which has no `disabled` prop, so a dead control cannot exist without a
   reason. The one place that keeps its buttons on screen is the user roster's maintainer
   boundary, and that is not a role gate: this administrator may manage users, just not *this*
   one, which is what the printed line beside the row says. Where the absent controls would leave
   a reader puzzled (the faces panel, whose whole point is naming) the panel says once, in words,
   what the missing buttons would have said one by one.
10. **New (2026-08-05) — index pages don't scale.** Albums (438), Labels (113) and People (105)
    all render one flat list with no search and no sort. See `UX_RESEARCH.md` **N7**, **N8**,
    **N10**. 🔴🟡
11. **New (2026-08-05) — every browser tab is called „Kukátko".** `document.title` never changed,
    so browser history — the natural route to "the photo I saw last week" — held fifty
    indistinguishable entries, two open tabs could not be told apart, and a bookmarked view had no
    name. See `UX_RESEARCH.md` **N15**. 🟡⚪ — ✅ **Done (2026-08-08).** Every page names its own tab
    through one shared `useDocumentTitle` hook (`<page> · Kukátko`, i18n'd separator and all), a
    detail page uses the data it already holds (photo, person, album, label, query), and leaving a
    page restores the bare app name so no title is ever stale. See `FRONTEND.md`.
12. **New (2026-08-12) — half the icon buttons said nothing when hovered.** An icon-only control is
    a guess until it is pressed, and roughly half of them already answered the mouse with a native
    `title` while the rest stayed silent — which is which looked random. The quiet ones clustered
    where they were most missed: the photo viewer (back, archive/restore, the faces and edit
    toggles, prev/next, close info) and the slideshow's whole control row, plus the metadata,
    faces, people and edit panels, the filter and period chips, the timeline rail and the decade
    nav. Screen readers were never the problem — no icon-only control was found without an
    `aria-label`; this was purely the sighted mouse user's hover. ✅ **Done (2026-08-12).** Every
    icon-only `<Button>`/`<button>`, link and `Dropdown.Toggle` now carries a `title` saying the
    same thing as its `aria-label`, out of the same `t()` call — state-switching and interpolated
    labels included — while the `aria-label` stays, because a phone shows no tooltip at all.
    Buttons with visible text were deliberately left alone. See `FRONTEND.md` (`Icon`).

---

## Prioritized backlog (follow-up tickets)

Ordered by impact-to-effort. 🔴/🟡/⚪ = impact, then effort. The **Checked** column records the
2026-08-05 production pass: ✔ = still open and confirmed by using the app, — = could not be
reached or reproduced with a viewer account, ✅ = done.

| # | Item | Impact | Effort | Checked | Notes |
|---|------|:---:|:---:|:---:|-------|
| 1 | Plain-language **search modes** ("Smart / By text / By meaning") + hide selector behind "advanced" | 🔴 | 🟡 | ✔ | Search is a core flow; mode names are the last scary copy there. Fix together with `UX_RESEARCH.md` **N1** (30 s timeout on an offline box) — same component. |
| 2 | ✅ **Done** — shared **`ConfirmModal`** replaces all `window.confirm` (Album/Labels/Saved/Import) | 🔴 | 🟡 | ✅ | One dialog modelled on Trash: confirm button carries the action, `danger` by default and not the Enter target, focus/Escape/restore handled, cs+en. |
| 3 | **Import**: stop rendering `run.last_error` verbatim; friendly "details" disclosure | 🔴 | 🟡 | — | Only raw-error leak in the app. Admin-only, 403 for the audit account. May become moot: removing the dead import is already a queued task. |
| 4 | **Maintenance/Import/Duplicates/System**: plain-language explainers/tooltips for jargon (embeddings, hashes, orphans, dead jobs, box) | 🔴 | 🟡 | — | Admin pages, but still meant to be usable. The same jargon leaks onto the all-roles `/stats` — see `UX_RESEARCH.md` **N22**. |
| 5 | Add **retry** to People / Subject / Clusters error states | 🟡 | ⚪ | — | They already have the fetch logic. Error state not forceable read-only. |
| 6 | Surface **Subject set-cover failure** (currently silent) | 🟡 | ⚪ | — | One alert. Write-gated. |
| 7 | Standardize **`<Button as={Link}>`** everywhere (kill `className="btn…"` on `Link`) | 🟡 | 🟡 | — | Removes a whole class of inconsistency. Code-level, not observable by using the app. |
| 8 | **Saved searches** / ✅ **Album detail** — widen action gaps, separate destructive buttons, overflow menu for 5-button headers | 🟡 | 🟡 | — | Reduces mis-taps & clutter on mobile. Album detail is done: the shared `HeaderActions` keeps Promítání inline on a phone and folds Stáhnout/Upravit + (behind a divider) Smazat into a „…" menu; saved searches can take the same component. The audit account has no saved searches, so the row layout was not observable. |
| 9 | **Breadcrumb** affordance on Places (padding / real breadcrumb) | 🟡 | ⚪ | ✔ | Still `btn btn-link p-0`, 42 × 26 px. |
| 10 | **Contrast** pass on muted text over dark/overlay backgrounds (Map, Slideshow, subtitles) | 🟡 | 🟡 | ✔ | Readability across the app; the inactive map style tabs are the clearest live example. |
| 11 | Align **Account** submit to full-width like Login; **NotFound** to `<Button>`/`h1.h3` | ⚪ | ⚪ | ✔ | Account submit 117 px vs. Login's 430 px `d-grid`. |
| 12 | Saved-search **link affordance** (`text-decoration-none` hides that names are tappable) | 🟡 | ⚪ | — | No saved searches on the audit account. |
| 13 | Drop `autoFocus` on Login username for touch | ⚪ | ⚪ | ✔ | Avoids keyboard-on-load; focus is set from JS, not the attribute. |

---

## Out of scope (tracked separately — referenced, not implemented here)

Per the task brief, these larger items are owned by their own tickets and were intentionally **not**
touched in this pass:

- **Navbar structure** — already shipped (top-level Library/Albums/Labels + Browse/Tools/Admin
  dropdowns, icons + action titles); any further restructuring is separate. ⚠️ The shipped
  structure has a gap: nothing links to `/search` or `/saved` — see `UX_RESEARCH.md` **N3**.
- **Library FilterBar redesign** — already shipped (calm default + progressive disclosure); further
  work separate.
- **Fullscreen photo viewer** — already shipped (Lightbox); further work separate. ⚠️ Its mobile
  layout is where `UX_RESEARCH.md` **N6** and **N20** live.
- **Album/label add autocomplete** — already shipped (`AddAutocomplete`); further work separate.
- **Map-based location picker** — already shipped (LeafletMap picker mode); further work separate.

Items #1–#13 in the backlog above are the recommended follow-up scope for *this* document.
The production walkthrough in [`UX_RESEARCH.md`](UX_RESEARCH.md) proposes a separate,
larger list (N1–N26); pick from both, they don't overlap.
