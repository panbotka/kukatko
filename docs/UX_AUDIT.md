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
   `.page-link` and checkbox to clear the ~44 px finger-friendly minimum on phones/tablets —
   **without** touching desktop layouts or any per-component markup. This is the single
   highest-leverage fix for the pervasive `size="sm"` touch problem found on almost every page.
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
   - `home.subtitle` — dropped the internal "PhotoPrism replacement" comparison.
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
- **Consistency:** `app.name` brand doubles as the "Home" link — fine, but there is no visible
  "Home"/"Domů" entry in the bar itself. Minor.
  📋 Consider a small home/brand affordance hint. ⚪⚪
- **Readability / States / Intimidation:** No issues. Every entry pairs a bootstrap-icons glyph with
  an action-describing `title`, so daily users recognise entries by shape. Searching and saved
  searches left the bar: both are reached from `/search` (search also from the library page). ✅

---

## Auth & entry screens

### Login (`/login`)
- **Clarity/States/Intimidation:** Exemplary. One centered card, two fields, one full-width
  primary button. Errors are mapped to three friendly messages (invalid / rate-limited / generic);
  raw API errors never surface.
- **Touch:** Full-size, full-width submit (`d-grid`). Good.
- 📋 `autoFocus` on username can force the mobile keyboard open on load — consider dropping on
  touch. ⚪⚪

### Home (`/`)
- **Was the biggest single issue.** The landing page's centerpiece was "Backend status /
  Version / Commit `<hash>`" plus a "PhotoPrism replacement" subtitle — the most intimidating,
  least actionable screen for a non-technical user, with **no primary navigation at all**.
- ✅ **Done (first pass):** rewritten as a welcome with destination cards; technical status
  demoted to a muted footer. 🔴🟡
- ✅ **Done (follow-up):** the card grid is gone too. `/` now renders the **photo library** —
  the thing the app is for — so the photos greet the user instead of a menu of links to them.
  `/library` survives as a replacing redirect (query string preserved) for old bookmarks, the
  navbar's Knihovna entry points at `/`, and the health badge + build version moved to
  `/account`. An empty catalog gets its own empty state pointing at Upload, distinct from the
  "no photos match these filters" one. `App.test.tsx` covers the routing.

### Account (`/account`)
- **Clarity/States:** Clear two-section layout (identity + change password). Thorough inline
  validation and mapped error messages.
- **Consistency:** The submit button is **not** full-width here, unlike Login's `d-grid`. 📋 Align
  the two password forms. ⚪⚪

### Not found (`*`)
- Friendly, clear recovery link. Uses a raw `className="btn btn-primary"` on a `Link` and a
  `display-5` heading — cosmetically off-pattern but harmless. 📋 Use `<Button as={Link}>` and
  `h1.h3` for consistency. ⚪⚪

---

## Library & browse

### Library (`/library`)
- **Clarity:** Title + filter bar + virtualized grid; browsing tiles is self-evident. The header
  actions (Slideshow / Save view / Select) are all `outline-secondary size="sm"` — **nothing reads
  as the page's primary action**, though for a browse screen the content _is_ the action.
- **Touch:** Header packs three small buttons at `gap-1` (4 px) — mis-tap risk. The coarse-pointer
  floor now enlarges them. ✅ 📋 Also bump the header `gap-1`→`gap-2`. ⚪⚪
- **Consistency:** Mixes `<Link className="btn…">` and `<Button>` for visually identical controls.
  📋 Standardize on `<Button as={Link}>`. 🟡⚪
- **States:** All present and friendly.

### Favorites (`/favorites`)
- Simplest, cleanest page. Excellent action-guiding empty state ("Tap the heart on a photo…").
  No issues.

### Search (`/search`)
- **Clarity:** Prominent autofocus search field + mode selector; well-designed.
- **Intimidation:** Previously the **most jargon-heavy copy** — the degraded notice named "the
  inference service is offline" and "semantic/full-text". ✅ **Done:** reworded to plain language.
  📋 The **mode selector labels** ("Hybrid / Full-text / Semantic") remain technical — rename to
  plain terms (e.g. "Smart / By text / By meaning") and/or hide the selector behind an "advanced"
  toggle, defaulting everyone to the smart mode. 🔴🟡
- **Touch/States:** Field is full-size (good); header Save-view + retry are `size="sm"` (now
  floored on touch ✅).

### Saved searches (`/saved`)
- **Touch (weakest here):** Each row packs a `flex-grow-1` link + two `size="sm"` buttons at
  `gap-1` — Rename and destructive Delete adjacent with tiny targets. Coarse-pointer floor helps
  ✅, but 📋 widen the gap and separate Delete. 🟡⚪
- **Readability:** Row link uses `text-decoration-none` → tappable saved-search names don't look
  tappable. 📋 Add an affordance (icon or hover underline). 🟡⚪
- **Consistency:** Delete uses a native `window.confirm` — unstyled vs. the app's own modals.
  📋 See cross-cutting item below. 🟡🟡

### Places (`/places`)
- **Clarity/Touch:** Country/city rows are large full-width `ListGroup.Item action` targets —
  good. But the **breadcrumb links use `variant="link" p-0`** → small, tightly packed inline
  targets around "/" separators. 📋 Give breadcrumb links padding / a real breadcrumb component. 🟡⚪
- **Intimidation:** Empty hint mentions "GPS souřadnice" / "zpracování polohy" — mildly technical.
  📋 Soften. ⚪⚪
- A grid skeleton is shown while loading what is actually a **list** — minor mismatch. 📋 ⚪⚪

### Map (`/map`)
- Clean. Loading/empty are overlays on the map (intentional divergence from full-page empties).
- **Readability:** The empty-state hint is `text-secondary` on `bg-dark` — lower contrast than
  elsewhere. 📋 Lighten. ⚪⚪

---

## Organize

### Albums index (`/albums`) & Labels index (`/labels`)
- **Consistency:** Two sibling "index" pages render differently — Albums as a **card grid**, Labels
  as a **ListGroup**. Defensible (labels are lightweight) but worth a deliberate decision. 📋 ⚪⚪
- **Primary action:** Create CTA was `size="sm"`. ✅ **Done:** now full size.
- **Touch:** Label rows pack Rename + Delete `size="sm"` at the right edge (floored on touch ✅).
- States/copy: friendly and complete.

### Album detail (`/albums/:uid`)
- **Touch (worst offender):** Up to **5 controls** (Slideshow, Edit, Select, Reorder, Delete) in
  one `d-flex gap-1 flex-wrap`, all `size="sm"`. The coarse-pointer floor enlarges them ✅, but
  📋 consider collapsing the editor actions into an **overflow "⋯" menu** on small screens to cut
  clutter. 🟡🟡
- **Intimidation:** Delete uses native `window.confirm` (reassuring copy, but unstyled). 📋 See
  cross-cutting item. 🟡🟡

### Label detail (`/labels/:uid`)
- Minimal and clear. No inline rename/delete (only on the index) — mild inconsistency with album
  detail. 📋 ⚪⚪

### Photo detail (`/photos/:uid`)
- **Consistency:** Title was `h1.h4` (smaller than every other page). ✅ **Done:** now `h1.h3`.
- **Touch:** Prev/next on-image nav (`‹`/`›`) rely on default button padding, positioned at image
  edges — can crowd small screens; rating/flag icons are small (18–22 px). Coarse floor helps the
  buttons ✅. 📋 Enlarge on-image nav hit areas / add `kukatko-tap-target`. 🟡⚪
- **Intimidation:** Child panels expose photographic jargon (Aperture/Exposure/Focal length/ISO,
  DMS coordinate help) and previously "Geotag" (✅ reworded). The EXIF terms are legitimate for a
  photo app but could get tooltips. 📋 ⚪⚪

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
- **States:** Error alert has **no retry** (must reload the page). 📋 Add retry. 🟡🟡

### Subject (`/people/:uid`)
- **Touch:** Back is a **bare text link** (smallest target on the page); Edit + Load-more are
  `size="sm"`. Coarse floor helps the buttons ✅. 📋 Make Back a padded control. 🟡⚪
- **States:** "No photos" was a bare paragraph. ✅ **Done:** now the standard centered block.
  **Set-cover failure is silently swallowed** — no feedback, unlike the visible `actionError`
  elsewhere. 📋 Surface a toast/alert. 🟡🟡

### Clusters (`/people/clusters`)
- Best-explained of the three (title + subtitle). **Intimidation:** empty hint previously said
  "clustered" (ML jargon). ✅ **Done:** reworded. Error state has no retry. 📋 Add retry. 🟡⚪

---

## Admin & tools

These pages are admin-only, so a _technical_ operator is the audience — but the copy still leans
on unexplained jargon that even a non-developer admin will struggle with.

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
  details" disclosure. 🔴🟡 Jargon: "dead jobs", "embeddings", "photo-sorter migration",
  "background processing queue". 📋 Add plain-language explanations / tooltips. 🟡🟡
- **Consistency:** first-run confirmation uses a native `window.confirm` with un-localized
  OK/Cancel, vs. Trash's styled modal. 📋 🟡🟡

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
   📋 Introduce **one shared `<ConfirmModal>`** and route all destructive confirms through it. 🔴🟡
4. **Missing retry on error states** (People, Subject, Clusters) — user must reload. 📋 Add a retry
   button (they already have the fetch logic). 🟡🟡
5. **Silent failures** (Subject set-cover) contradict the visible-error pattern used elsewhere.
   📋 Always surface an alert/toast. 🟡⚪
6. **Jargon inventory to keep out of user copy:** "backend/commit", "inference service",
   "semantic/full-text", "clustered", "geotag" (✅ all fixed), plus still-present "perceptual
   hash / embedding distance", "dead jobs / dead-letter", "orphan files", "box". 📋
7. **Muted-text contrast.** `text-secondary` subtitles/hints on the dark Superhero theme are on the
   low side, especially over `bg-dark` overlays (Map, Slideshow empties). 📋 Audit contrast; consider
   a slightly lighter muted token. 🟡🟡
8. **Heading hierarchy** is otherwise consistent (`h1.h3` titles, `h2.h5/h6` sections) after the
   photo-detail fix. ✅

---

## Prioritized backlog (follow-up tickets)

Ordered by impact-to-effort. 🔴/🟡/⚪ = impact, then effort.

| # | Item | Impact | Effort | Notes |
|---|------|:---:|:---:|-------|
| 1 | Plain-language **search modes** ("Smart / By text / By meaning") + hide selector behind "advanced" | 🔴 | 🟡 | Search is a core flow; mode names are the last scary copy there. |
| 2 | Shared **`<ConfirmModal>`**; replace all `window.confirm` (Album/Labels/Saved/Import) | 🔴 | 🟡 | Consistency + localization + polish for destructive actions. |
| 3 | **Import**: stop rendering `run.last_error` verbatim; friendly "details" disclosure | 🔴 | 🟡 | Only raw-error leak in the app. |
| 4 | **Maintenance/Import/Duplicates/System**: plain-language explainers/tooltips for jargon (embeddings, hashes, orphans, dead jobs, box) | 🔴 | 🟡 | Admin pages, but still meant to be usable. |
| 5 | Add **retry** to People / Subject / Clusters error states | 🟡 | ⚪ | They already have the fetch logic. |
| 6 | Surface **Subject set-cover failure** (currently silent) | 🟡 | ⚪ | One alert. |
| 7 | Standardize **`<Button as={Link}>`** everywhere (kill `className="btn…"` on `Link`) | 🟡 | 🟡 | Removes a whole class of inconsistency. |
| 8 | **Saved searches / Album detail**: widen action gaps, separate destructive buttons, overflow menu for 5-button headers | 🟡 | 🟡 | Reduces mis-taps & clutter on mobile. |
| 9 | **Breadcrumb** affordance on Places (padding / real breadcrumb) | 🟡 | ⚪ | Small inline targets today. |
| 10 | **Contrast** pass on muted text over dark/overlay backgrounds (Map, Slideshow, subtitles) | 🟡 | 🟡 | Readability across the app. |
| 11 | Align **Account** submit to full-width like Login; **NotFound** to `<Button>`/`h1.h3` | ⚪ | ⚪ | Tidy-ups. |
| 12 | Saved-search **link affordance** (`text-decoration-none` hides that names are tappable) | 🟡 | ⚪ | |
| 13 | Drop `autoFocus` on Login username for touch | ⚪ | ⚪ | Avoids keyboard-on-load. |

---

## Out of scope (tracked separately — referenced, not implemented here)

Per the task brief, these larger items are owned by their own tickets and were intentionally **not**
touched in this pass:

- **Navbar structure** — already shipped (top-level Library/Albums/Labels + Browse/Tools/Admin
  dropdowns, icons + action titles); any further restructuring is separate.
- **Library FilterBar redesign** — already shipped (calm default + progressive disclosure); further
  work separate.
- **Fullscreen photo viewer** — already shipped (Lightbox); further work separate.
- **Album/label add autocomplete** — already shipped (`AddAutocomplete`); further work separate.
- **Map-based location picker** — already shipped (LeafletMap picker mode); further work separate.

Items #1–#13 in the backlog above are the recommended follow-up scope.
