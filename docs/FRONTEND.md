# Frontend

A descriptive reference overview of the frontend (`web/`). **These are not rules** — the rules
live in [`CLAUDE.md`](../CLAUDE.md). Record any new component, hook, page, or service
here.

<!-- BODY BEGIN -->
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` with `components/`
  (`Layout` = navbar shell with a user menu (Můj účet, **Nápověda** `/help`, Odhlásit se) + role-gated
  nav with a **visible hierarchy based on
  how often an ordinary person uses an item**: the everyday loop (browsing, sorting, adding photos) is
  loud and immediate, while admin/power-user tooling is present but quieter. It leads with **Knihovna** `/` (= the home
  page; `NavLink` has `end`, otherwise it would light up on every route), **Alba** `/albums` and **Štítky**
  `/labels` (always visible top-level, the `PRIMARY_ITEMS` registry); the remaining browse targets are gathered by the
  **Procházet** dropdown (`nav.browse`, `BROWSE_GROUP`): **Oblíbené** `/favorites`, **Lidé** `/people`,
  **Místa** `/places`, **Mapa** `/map`; **Třídění** `/review` (`REVIEW_ITEM`, gated on `canWrite`) stays
  top-level, not under „Nástroje" — tidying the library one question at a time is the most-used curatorial
  loop, and a game nobody finds is a game nobody plays; **Žebříček** `/leaderboard`
  (`LEADERBOARD_ITEM`, `trophy` icon) sits right next to Třídění as its scoreboard and has **no
  role gate** — the competitive standing is just an aggregate of counts, so **every logged-in user** sees it (even a viewer),
  not just editors; **Nahrát** `/upload` (gated on
  `canWrite`) is the bar's **single call-to-action** — a filled pill (`kukatko-nav-cta`, prop `cta`
  in `renderLink`) so adding photos stands out. After it a **divider** (`kukatko-nav-divider` — a vertical
  hairline in the inline bar ≥ md, horizontal in the collapsed burger menu; drawn only when a role
  actually has something behind it) separates the quieter power-user/admin cluster: the editor dropdown **Nástroje** (`nav.tools`,
  `TOOLS_GROUP`, entirely gated on `canWrite`) now leads with **Rozšířit** `/expand` (a power-user tool that used to
  shout top-level next to albums/labels) + **Najít osobu** `/faces` + **Rozpoznávání** `/recognition` +
  **Možné chyby** `/outliers` + **Duplikáty** `/duplicates` + **Koš** `/trash`; the operations dropdown
  **Provoz** (`nav.operations`, `OPERATIONS_GROUP`, entirely gated on `isMaintainer`) gathers **Import**
  `/import` (formerly a standalone top-level item; import is now an operational capability — it belongs to the maintainer,
  not out in the open) + **Údržba** `/maintenance` + **Systém** `/system`; the governance dropdown **Správa**
  (`nav.admin`, `GOVERNANCE_GROUP`, entirely gated on `isAdmin` = admin **or** maintainer) gathers
  **Uživatelé** `/users` + **Audit** `/audit`. The role model is a strict ladder
  `viewer < editor < admin < maintainer` (see `services/auth.ts` below).
  **The bar leads with global search** `SearchCommand` (`components/search/`) — a field-as-trigger on the left,
  **outside the collapse** (so on mobile it stays visible when the nav folds into the burger), opening a **command
  palette** reachable from anywhere via `/` or Cmd/Ctrl-K (it doesn't steal typing — see `SearchCommand`
  below). The old full `/search` page and saved searches remain; only the navbar no longer has a standalone
  „Hledat" link or the library's filter field.
  Every item and every dropdown toggle carries an **icon** (`Icon`) and a **`title` describing the action**, not
  the noun („Zobrazit alba", not „Alba"; keys `nav.titles.*`); icons are decorative
  (`aria-hidden`) beside the visible text label. A dropdown is hidden entirely when the user has
  all of its items hidden (Tools/Admin for a viewer); the parent menu has an **active state** (`active`
  prop) when the current route is one of its children (`pathMatches` also honors a detail sub-path like
  `/albums/{uid}`) — it is built from `Dropdown`+`Dropdown.Toggle as={NavLink}` (not `NavDropdown`, which
  consumes the `title` prop for the toggle's content, leaving none for the tooltip); items in the mobile burger
  menu expand inline with tap-targets (`kukatko-tap-target`),
  `Footer` (**global footer** below `<main>` on every page in `Layout` — the fullscreen
  `/slideshow` and the immersive `/photos/:uid` run outside the shell, so they don't have it: „Provozuje SDH Veselice“ + a link to the source code
  <https://github.com/panbotka/kukatko> in a new tab with `rel="noopener noreferrer"` and a decorative
  `github` icon (`aria-hidden`); texts `footer.*` (cs/en). It renders in normal flow — on a
  short page it simply follows the content, overlapping and floating nothing. Inside is a space-between
  flex row: operator + GitHub on the left, `children` fills the right side (today the admin job-queue
  badge); `.kukatko-footer` shares safe-area padding with `.kukatko-main`),
  `JobQueueBadges` (right side of the footer: a compact badge with the job-queue state **for maintainers only**
  — the `/jobs` endpoint is a maintainer-only operational capability; via `useAuth().isMaintainer` +
  `useJobStats` — a non-maintainer renders nothing and **makes no request**.
  One badge per non-empty `queued`/`running`/`failed`/`dead` state from `by_state` (the terminal `done`
  is deliberately omitted), `failed`/`dead` carry `bg="danger"` so they catch the eye; when everything is zero,
  a single quiet `idle` badge. A failed request silently hides the badge — the footer never breaks; texts
  `footer.jobs.*` (cs/en)),
  `AnnouncementBanner` (**instance-wide announcement at the top of the content**: in `Layout` right **before `<Outlet/>`**,
  so every logged-in user sees it on every page **inside the shell**; routes **outside `Layout`**
  (`/photos/:uid`, `/slideshow`, `/review`, `/duplicates/compare`) have no banner — immersive views,
  acceptable. Via `useAnnouncement` (fetch on-mount + **polling ~60 s**, so a freshly published message
  appears without a reload) + a dismissible `<Alert>` with a variant per `level` (`info`→`info-circle`
  icon, `warning`→`exclamation-triangle`, decorative `Icon`). **Per-user dismiss keyed on `updated_at`**
  in localStorage (`lib/announcementDismissal.ts`: `readDismissedAnnouncement`/`writeDismissedAnnouncement`,
  mirrors `faceOverlayPref.ts`) — dismissing hides the current message, but a newly published one (new `updated_at`)
  **shows again** (not a plain boolean); empty message / loading / already dismissed → renders nothing; texts
  `announcement.*` (cs/en)),
  `JobStateLegend` (**shared legend of job-queue states**: a compact `dl` with a bold term + a quiet
  one-sentence explanation of each state, so an admin understands without hovering; both the labels and the explanations come from a
  shared i18n block `jobStates.labels.*`/`jobStates.descriptions.*`, so the wording is identical on
  `MaintenancePage` and `SystemStatusPage`; the `states` prop controls order and selection — Maintenance omits
  `pending`, System adds it. Tests: `JobStateLegend.test.tsx`),
  `Icon` (**the app's single icon set**: a bootstrap-icons glyph as `<i class="bi bi-{name}">`,
  the font is imported globally in `main.tsx`; the `IconName` union holds the dictionary of used icons, so a typo
  is a compile error; always `aria-hidden` beside a visible label),
  `components/toast/` = **app-wide toast** (`ToastContext` holds the context + hook `useToast()` +
  types; `ToastProvider` is the component) — a single provider **in `App` around `AppRoutes`**, hosting
  `ToastContainer` (react-bootstrap, `position="top-center"`, `.kk-toast-stack` `z-index:1100`
  above chrome and the viewer) with auto-dismiss (5 s) + manual close (`toast.close`).
  `useToast().show({message, variant?})` (`success`/`danger`/`info`, an `Icon` glyph by tone);
  **one place for placement, duration, and style** — instead of Bootstrap `bg-*` (solid green/red)
  each toast carries **its own surface from tokens**: `.kk-toast` = `--kk-surface-overlay` + a subtle
  `--kk-surface-border` + `--kk-shadow-3` + `--kk-radius-md`, with a **colored accent bar** on the left
  and a glyph tinted by tone (`.kk-toast--{success,danger,info}` via `--kk-toast-accent` from
  `--bs-success`/`--bs-danger`/`--kk-accent`), text in `--bs-body-color`. **Outside the provider it returns a
  no-op** (default context), so focused unit tests need no wrapper. First user:
  `BatchActionBar` (success/failure of a bulk action). Tests run via `BatchActionBar.test`,
  `BackLink` (**shared way back** from every detail (album, label, person, photo) to the list
  it belongs to: an `arrow-left` arrow via `Icon` (decorative, `aria-hidden`) + **text naming the
  target** („Zpět na alba" / „Zpět na štítky" / „Zpět na lidi"), which is also the link's accessible
  name — a bare arrow told no one where it leads. Props `to` (the target's full href **including query**, so the
  list state — filters/sort/page — survives the return and **Back always works**; `PhotoDetailPage`
  builds it via `backHref(view)`), `label` (already translated by the caller), `className?`. Renders a router
  `<Link>` — keyboard-focusable, focus-ring + underline on hover via `.kk-back-link`
  (the arrow leans toward the target on hover, `prefers-reduced-motion` turns the motion off), a 44px
  tap target on a coarse pointer; also used in the error alert of the same pages. Tests: `BackLink.test.tsx`),
  `LanguageSwitcher` (cs/en button group, `aria-pressed` on the active one; **it does not sit in the navbar** —
  it lives in the Jazyk section on `AccountPage`, because only Czechs use this instance and a permanent
  spot in the bar would be a waste. The i18next language detector persists the choice to localStorage),
  `MultiSelect` (**shared searchable multi-select** for collections that grow without limit —
  albums and labels: typing narrows the offering **case- and diacritic-insensitive** via `lib/text`
  `foldedIncludes`, each choice is **added** (not replaced), the selected item **disappears from the list**
  and appears below the field as a removable chip (`.kk-chip`), so a long list stays short
  and the selection readable without a column of checkmarks. Keyboard Up/Down/Enter (with nothing highlighted it takes the best
  match), **Backspace over an empty query removes the last chip**, Esc closes; combobox/listbox
  ARIA (`aria-multiselectable`), a `MAX_SUGGESTIONS` (50) cap on rendered suggestions, ~44px tap
  targets. The `destructive` prop tints the label and chips into the danger key, so a removal never looks
  like an addition. By default it **creates no items** — it only picks from those it receives (mirrors
  `AddAutocomplete` and `SearchableSelect`); with an optional `onCreate(name)` it appends a
  **„Vytvořit «dotaz»“** row to the list, only when a non-empty trimmed query fold-insensitively (case,
  diacritics, edge whitespace) matches **no** option — selected ones included — so it never
  offers a duplicate; Enter with nothing highlighted creates only when nothing else matches. What creating
  means is up to the caller (typically it registers the name and picks the value for it via
  `options`+`selected`); for a reader without write permission, `onCreate` is simply not passed),
  `photo/PlaceSearch` (**place autocomplete by name** = the third route to a photo's location alongside
  coordinates and a map click — for a scanned photo you know *Veselí nad Moravou*, not the numbers, and hunting
  that point by panning the map is a nuisance. `{id,onPick,disabled?}`, `onPick(place)` receives a `Place` and
  the caller decides where to write the coordinates: `MetadataPanel` writes them into its own coordinate
  field (the marker and the map redraw themselves), `BulkEditModal` into `lat`/`lng` for `set_location`.
  Each row carries **name + place kind (`label`) + `location`** — the distinction is the whole point (Veselí
  is a town, a château, and a village district; three identical-looking rows would be useless). Typing goes through
  `usePlaceSearch` (debounce + cancelling in-flight); the field holds **two** state values — what is visible
  (`query`) and what is being searched (`term`) — so picking a suggestion leaves the name in the field as confirmation but
  does not immediately search for it again. Keyboard Up/Down/Enter (with nothing highlighted it takes the best match)/Esc,
  combobox/listbox ARIA, ~44px tap targets — it is a form control and behaves like one. Unavailable
  search (no key, provider down) = **a single line of text**, the rest of the location editor carries
  on. Tests: `PlaceSearch.test.tsx`),
  `KeyboardShortcutsHelp` (in the navbar: a keyboard icon + **shortcuts help modal** — opens with
  `?` (Shift+/) anywhere or by click, lists all shortcuts grouped by context (Grid / Detail)
  from `lib/shortcuts.ts` `SHORTCUT_GROUPS`, closes with Escape/the close button),
  `EmptyState` (**shared empty-collection placeholder**: an icon in a round pit, a short title,
  a single-line hint and an optional action button, centered in the space the collection would occupy.
  Props `title` (required), `hint?`, `icon?` (default = the outline of an empty frame, `aria-hidden`),
  `action?` (usually the same button the filled view offers), `size?` `'md' | 'sm'`
  (a compact variant for a tile/narrow panel), `className?`. Titles/hints are **translated by the caller**
  (each page has its own i18n key so the copy stays concrete). Replaced the bare one-liner
  „Bez náhledu" and every hand-assembled `text-center py-5` block across
  pages (`LibraryPage`, `SearchPage`, `AlbumsPage`, `AlbumDetailPage`, `LabelsPage`,
  `LabelDetailPage`, `PeoplePage`, `SubjectPage`, `PlacesPage`, `MapPage`, `FavoritesPage`,
  `SavedSearchesPage`, `ClustersPage`, `FacesPage`, `ExpandPage`, `DuplicatesPage`, `TrashPage`, `SlideshowPage` (with a
  „Zpět" action), `ImportPage`) and in components (`AlbumTile`/`SubjectTile` cover placeholder,
  `Outliers`). **Not every emptiness deserves it:** in a dense panel where several short
  lists sit stacked (`OrganizePanel` — albums and labels), the placeholder would outgrow the chips
  it stands in for, and the panel would jump as one list fills while the other stays empty —
  there a muted single-line caption stays (`text-secondary small`). Blocks appear via
  `.kk-appear`, which `prefers-reduced-motion` turns off. Tests: `EmptyState.test.tsx`),
  `ErrorState` (**shared failed-load placeholder** = the error twin of `EmptyState`:
  the same centered column (classes `.kk-empty-state*`), but the medallion is colored `danger`
  (`.kk-empty-state--error`) and carries an `exclamation-triangle` icon via `Icon`, plus `role="alert"`,
  so a failure is never read as a deliberately empty collection and never shows raw error text.
  Props `title` (required, a short message, translated by the caller), `hint?`, `onRetry?` (renders a
  **Zkusit znovu** button — an `arrow-clockwise` icon + the shared key `errors.retry` — that re-runs the
  load), `retryLabel?` (overrides the label), `action?` (an additional/alternative action beside Retry — typically
  `BackLink` on a detail that failed to load its entity), `size?` `'md' | 'sm'`, `className?`. Replaced
  hand-assembled `Alert variant="danger"` (bare and with an inline Retry button) across **all**
  data views: grids (`LibraryPage`, `SearchPage`, `FavoritesPage`, `AlbumDetailPage`,
  `LabelDetailPage`, `SubjectPage`, `PlacesPage`, `TrashPage`, `MapPage`, `SlideshowPage`,
  `ExpandPage`, `DupComparePage`), lists (`AlbumsPage`, `LabelsPage`, `PeoplePage`,
  `SavedSearchesPage`, `ClustersPage`) — those that previously had no retry got it via `useReloadKey`
  —, and admin/power views (`FacesPage`, `OutliersPage`, `ImportPage`, `SystemStatusPage`,
  `AuditPage`, `UsersPage`, `DuplicatesPage`) and the photo detail (`PhotoDetailPage`, Back action).
  Retry calls either `retry` from the pagination hook, or a re-fetch via `useReloadKey`/`load()`/`refresh()`.
  Tests: `ErrorState.test.tsx`),
  `FadeInImage` (**a shared preview `<img>` that fades in and settles slightly after decoding**
  instead of popping: it starts transparent and a hair smaller (`scale(0.98)`, never enlarged, so it doesn't overflow
  the box) over a placeholder surface the container provides (a sunken pit), and the `is-loaded` state (from its own
  `onLoad`, plus a `complete` check for already-cached images) brings it up to full opacity and
  1:1. Everything on motion tokens via the `.kk-media-img` class, so under `prefers-reduced-motion` the
  transition collapses to an instant swap; only `opacity`+`transform` move (GPU). Default `loading="lazy"`
  + `decoding="async"` (overridable), the rest of the attributes (`src`/`alt`/`style`/`onError`/`className`)
  flow through. Replaced the manual `loaded` fade in `PhotoTile`/`TrashCard` and added the fade to covers/previews:
  `AlbumTile`, `SubjectTile`, `SubjectPhotoTile`, `SimilarPhotos`, `StackStrip`,
  `DuplicateGroupCard`, `GlobalSearchSections`, `SearchCommand`. Tests: `FadeInImage.test.tsx`),
  `Skeleton` / `TileGridSkeleton` / `ListSkeleton` (**shared skeleton placeholders** instead of
  full-page spinners on the main data views: `Skeleton` is a single shimmer block
  (`.kk-skeleton`, warm surface-1 + a sweeping sheen, `aria-hidden`, props size/circle/radius);
  `TileGridSkeleton` is a grid of cards (a square cover + 1–2 caption rows) with the same responsive
  `minmax` as the real grid — `AlbumsPage` (minTile 160, 2 rows) and `PeoplePage` (140, 1 row);
  `ListSkeleton` is a stack of rows (`LabelsPage`). The container carries `role="status"` + `aria-busy` and one
  localized message (the existing keys `*.loading`); the shimmer is the only motion → under
  `prefers-reduced-motion` it turns off and stays a static tone. Tests: `Skeleton.test.tsx`),
  `ConfirmModal` (**the single shared confirmation dialog** — replaced the native `window.confirm`
  in four places: `AlbumDetailPage` (deleting an album), `LabelsPage` (deleting a label),
  `SavedSearchesPage` (deleting a saved search) and `ImportPage` (confirming the first import run).
  Following the styled-modal pattern on `TrashPage` — one pattern instead of a grey OS dialog: **the confirm
  button carries the action itself** („Smazat album" / „Spustit import"), never „OK", and reads the same as
  the control that opened the dialog — the action keeps one name across the whole flow. Props `show`, `title`
  (a short question), `children` (the consequence — what happens and to what), `confirmLabel`, `cancelLabel?` (default
  the shared `confirmModal.cancel`), `variant?` `'danger' | 'primary'` (default `danger` colors the confirm
  red; non-destructive `primary`), `busy?` (locks both buttons and the close/backdrop for the duration of the
  action), `onConfirm`, `onCancel`. **The destructive button is not Enter's default**: after opening, focus rests
  on Zrušit, so a stray Enter cancels rather than deletes; Escape/close/backdrop cancel; react-bootstrap returns
  focus to the trigger. Copy is translated by the caller — no hardcoded strings. Tests: `ConfirmModal.test.tsx`);
  `components/upload/` = `DropZone` (a drag-and-drop zone + file input `multiple`
  `accept="image/*,video/*"` → the mobile gallery + a **Vyfotit** button `capture="environment"`),
  `UploadProgressHeader` (**a prominent sticky** header for the whole batch: „done / total", **one**
  overall progress bar weighted even by the partial `progress` of in-flight files — `barLabel` for a11y —,
  a live breakdown of the uploaded/duplicate/failed/remaining counts; on completion it switches to a **completed
  summary** with a link into the library and one-click retry-failed), `UploadItem` (a queue row as
  a standalone `kk-surface` card: name+size, progress bar, status badge, near-duplicate
  warning, remove/retry actions; a failed row has `border-danger`), `UploadList` (**a virtualized**
  `Virtuoso useWindowScroll` list of rows, gaps via `pb-2`, so 100+ files stay snappy on
  mobile), `UploadOrganize` (two searchable `MultiSelect`s for **albums**
  and **labels** that apply to the whole batch, with inline creation of a new item via `onCreate`; empty
  by default, driven by `useUploadOrganize`); `components/library/` = `PhotoTile`
  (a square lazy-load tile → `/photos/{uid}` in the **hero-first** style: no border, no shadow, and
  with a minimal radius `--kk-radius-tile`, so the library is a dense wall of images; **stack badge**
  (the group's member count at top right — an `images` icon + `stack_count`, `library.tile.stackCount`,
  only when `stack_count > 1`), a **play badge + duration** for a video/live photo (`▶` + `formatDuration`,
  **top right** — the date took the lower reading corner; a stack never meets a video), a placeholder with no
  layout shift; a **hover date caption** `.kk-tile__caption` (capture date over the bottom scrim
  `--kk-tile-scrim`, only on hover/focus, `aria-hidden` because the same date is already carried by the image alt,
  not shown on touch — without a date it doesn't render); on hover the **image** zooms in discreetly
  (`scale`, inside `overflow:hidden`, no layout shift); an optional **favorite heart** overlay
  `favoritable` → `FavoriteButton` (star ratings and the pick/reject flag live **only in the photo
  detail**, not on the tile); the heart hides in selection mode; `src` takes **`photo.thumb_url`
  from the payload** via `useThumbSrc` and **never** builds it from the UID),
  `PhotoGrid` (a virtualized **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, `endReached` → next page, footer spinner/retry; the `favoritable` prop
  leaks the heart onto the tiles; an optional `gridRef` (imperative `scrollToIndex`
  handle) + `onRangeChanged` (the visible range) for the timeline; it takes its column template from
  `useGridDensity` → `lib/gridDensity` `gridTemplateColumns`, the DOM carries `data-density` for tests.
  A density change **only restyles** the existing `<div>` — virtuoso re-measures the tiles, scroll and selection
  survive because the grid is neither keyed nor remounted),
  `TimelineScrubber` (**the timeline** — a thin fixed vertical data rail beside the grid: it fetches a monthly
  histogram via `useTimeline(params)` (refetch on filter change), each month = a clickable tick
  placed proportionally by `cumulative/total`, month labels via `lib/format` `formatMonth`;
  a click/drag jumps to a month via `onJump(bucket.cumulative)`, the active month is highlighted per
  `activeIndex` (start of the visible range) by a floating orange bubble (`.kukatko-timeline-current`)
  in its own track **left of the rail**; the rail is wide enough that a year tick (`.kukatko-timeline-year`)
  stays inside, so the bubble and the year labels **never overlap** even at a year boundary (where they fall
  onto one line); the overlay is `position: fixed`, so a loading/empty timeline renders nothing and
  doesn't shift the layout, on small widths it hides via `styles/app.css` `.kukatko-timeline*`; only for
  the default newest sort), `FilterBar`
  (**a redesign for a calm default state + progressive disclosure**: the header holds only a prominent
  search field (the visual anchor, the largest element), sort (incl. **by rating**),
  `GridDensityControl` and a
  **Filtry** button with a badge of the active-filter count; advanced filters (date from/to, location, private,
  camera, archive, **min. rating ≥1…≥5**, **picked/rejected flag**) live in a collapsible
  panel — inline `Collapse` on desktop, `Offcanvas` on mobile per `matchMedia` (the shared hook `useIsNarrowViewport`,
  defensive against jsdom, where `matchMedia` returns `undefined`); each active filter = a removable
  **chip** (`buildChips`, a pill with a cross, clears only that filter — the `q` query has no chip,
  it has its own field; **album and label chips carry the entity color** — `.kk-entity-album`
  vs. `.kk-entity-tag` + a guide icon from `ENTITY_STYLE`, so an album and a label are distinct at a glance
  (see *entity colors* in `tokens.css`); the other filters stay a neutral `text-bg-primary`)
  + one **„zrušit filtry"** + the photo count; **no behavior change** — everything
  runs through `viewToParams`/`useUrlState`/`LibraryView`, the query replaces history, the rest push;
  generic over `LibraryView`+a superset, props `showSearch`/`showSort` hide the query/sort
  on the search page, `showDensity` hides density in the trash (card-based, not a photo grid),
  **`showFavorite`** enables the **Oblíbené** toggle in the panel (a two-state select „Vše"/„Jen oblíbené"
  → `view.favorite` `''`/`'true'`, the backend scopes only to `true`; the library enables it so you can
  combine „oblíbené + album + rok" in the main grid, the Oblíbené page doesn't — it's already scoped)
  (chips/panel/clear keep working); ~44 px tap targets via `styles/app.css`
  `.kukatko-filter-*`;
  **the four facets by which photos are actually searched** (the `facets` prop from `useLibraryFacets`): on
  **desktop** its own always-visible row of four below the header, on **phone** (per
  `useIsNarrowViewport`) it **folds into the same filter `Offcanvas`** as the advanced filters —
  otherwise four columns stacked below one another would push the photos below the first screen; the active facet
  still stays visible as a **chip**, so the filtered set is no mystery even with the drawer closed:
  **Rok** = a plain `<select>`
  („Libovolný rok" + `{{year}} ({{n}})` from `GET /photos/years`, the catalog always has only a handful of years),
  **Album**, **Štítek** and **Osoba** = `SearchableSelect` (all collections grow without limit;
  people from `GET /subjects` with `marker_count`), **multi-select**: each choice is **added** to the current
  set (AND — a photo must be in all selected albums, carry all labels and contain all
  selected people), the select is a pure „add-picker" (it keeps the placeholder „libovolné", drops its selected
  items from its options so they can't be added twice), already-selected albums/labels/people hang as
  removable chips (one per UID) below.
  The inline **„filtrovat dle názvu/popisu"** field (`q`) stays a quick narrowing of the grid; the help text
  „Filtruje název a popis." (describes `q`, unrelated to embeddings) is **always visible**, but
  **the link to `/search`** for fulltext + semantic search shows **only when semantic
  search is available** — `FilterBar` reads `useCapabilities().semantic_search` and hides the link when the embeddings box is offline
  (fulltext keeps working, but its label promises semantics); `searchHref` carries the current `q`,
  the search modes are **not duplicated** here), `SearchableSelect`
  (`components/library/`, a single-select facet you can type into: at rest it shows the choice,
  focus opens the full list, typing narrows it **case- and diacritic-insensitive** via `lib/text`
  `foldedIncludes` (`namesti` finds `Náměstí`, same as the backend `immutable_unaccent`);
  the leading row „libovolné" clears the facet, keyboard Up/Down/Enter/Esc, combobox/listbox ARIA,
  a `MAX_SUGGESTIONS` (50) cap on rendered suggestions; it never creates items —
  mirrors `AddAutocomplete`), `filterChips.ts` (pure `buildChips(view, t, {facets?, includeQuery?})`
  → `FilterChip{key,label,clear,kind?}` for each active filter; **one chip per selected album,
  label and person** (`clear` removes only its own UID from the list, the last chip clears the facet; an album chip has
  `kind:'album'`, a label `kind:'tag'`, a person `kind:'person'` → `FilterBar` takes the color + icon from it via
  `ENTITY_STYLE`; **favorites** = a neutral chip with no `kind`); `facets`
  name the album/label/person by title instead of UID (missing → raw UID, a chip is never empty),
  `includeQuery` enables a chip for `q`
  — the filter bar disables it (it has its own field), **the empty state enables it** (a reader at zero results must
  see all the filters that got them there); the field length = the active-filter count on the badge),
  `SimilarPhotos` (a reusable horizontally scrollable strip
  of similar photos over `GET /photos/{uid}/similar` via `fetchSimilar`, links to the detail,
  empty-friendly + loading/error, refetch on `uid` change),
  `FavoriteButton` (a heart toggle over `useFavorite` — an **optimistic** per-user favorite
  with rollback; no role gate, allowed to any logged-in user; as a tile overlay it is a sibling
  of the link, so a click doesn't navigate), `RatingStars` (pure controlled 0–5 stars; a click on the current
  rating clears it to 0; without `onRate` a read-only display) + `FlagControl` (a pure controlled per-user
  **personal flag** — three neutral states via `Icon` bootstrap-icons: 👁 eye (`text-info`),
  👍 thumbs-up (stored `pick`, `text-success`), 👎 thumbs-down (stored `reject`, `text-danger`);
  a click on the active state clears it to `none`; without `onFlag` read-only; a sibling of the link → a click doesn't navigate),
  `GridSkeleton` (a placeholder photo grid on the first load; it also mirrors the chosen density, so after
  the photos load the layout doesn't jump. The tiles are `Skeleton` blocks (the shared `.kk-skeleton` shimmer, not
  Bootstrap `.placeholder`); the `label?` prop localizes the `role="status"` message (a person's gallery says
  „načítám fotky osoby", the library „načítám fotky"). It is consumed by `LibraryPage`, `FavoritesPage`,
  `AlbumDetailPage`, `LabelDetailPage`, `PlacesPage`, `TrashPage`, `DuplicatesPage`, `SearchPage`
  and `SubjectPage`),
  `GridDensityControl` (a compact zoom stepper **Dlaždic na řádek**: `−` / a middle chip / `+`;
  `−` steps toward **one photo per row** (fewer, larger tiles) down to a floor of 1, `+` pins more
  columns up to 10, the middle chip is **only a read-only indicator** of the current column count (1…10) —
  no „auto" mode and no reset button (`pointer-events: none`, it is not a button); it steps along
  the `stepDensity` ladder within 1…10; icons via `Icon` (`dash-lg`/`grid-3x3-gap-fill`/`plus-lg`),
  `−` is disabled at 1 (one photo per row), `+` at 10; reads/writes `useGridDensity`, i.e.
  localStorage, **not the URL** — it is a device preference, not part of the shared view; it sits in the header of
  `FilterBar` and in the header of `SubjectPage` (a person's gallery), it changes all photo grids in the app
  at once — and because it is only a view preference, **it is not write-gated** (a viewer sees it too);
  `PhotoTile`+`PhotoGrid` support
  **a modern multi-select in the style of photo apps** (props `selectable`/`selectFirst`/`selected`/
  `anySelected`/`onToggleSelect`, or `selection`): each tile carries a **round check
  circle** in the corner (`.kk-tile__check`, a sibling of the link/button like the heart — a click **selects without
  opening the photo**), which appears on hover and **stays visible once something is selected**
  (`kk-tile--checks`); a selected tile gets an **accent ring** (`kk-tile--selected` → inset
  `::after` from `--kk-accent`) and a **dimmed image**, so the selection is unmissable on the dense wall.
  Selection mode is either **explicit** (`selection.active` — tiles are selection targets from the start,
  only the /expand candidate review via `SelectionStart`), or **hover-select** (`selection.hoverSelect`,
  **every photo-list page**: library, album/label detail, favorites, search, places, subject gallery):
  in both modes the tile is **always the same `<Link>` element** — the root **never switches**
  between `<a>` and `<button>` (that would remount the whole grid on a 0↔1 selection transition and trigger the load-in
  fade of all images at once — a flicker of the whole wall). **Only the first selection** makes the whole thing a target
  (`selectFirst`): a click **toggles it instead of navigating** (`role="button"` + `aria-pressed`, navigation suppressed by
  `event.preventDefault()`, which react-router respects; Space handled manually, Enter via the native
  link activation), so a run of tiles can be selected quickly without "entering a mode"; the heart hides in selectFirst.
  **Shift+click selects a contiguous range**: `onToggleSelect` carries
  the `shiftKey` of the click, `PhotoGrid` redirects it with its own photo order to the optional
  `selection.onToggleRange(uid, orderedUids)` (without it a plain toggle remains) — the anchor is held by
  `useSelection`, so a range works in every grid without page wiring; `PhotoTile` has
  an optional **`extras`** slot (or the `PhotoGrid` prop `tileExtras(photo)`) for page overlays —
  a badge/action as a **sibling** of the link/button in a relative wrapper (an interactive extra doesn't navigate,
  doesn't toggle; a badge with `pe-none` doesn't steal the click) — used by `/expand` for the % similarity and ✗;
  the tile **shows no date** — the only one it carries is
  in the `alt` text, and even there an **estimated** date is marked (`cca 1950`), so it can't be read as certain;
  the grid/timeline sort doesn't change, it is still `taken_at`,
  `components/organize/` = `AlbumTile` (an album card: the **effective cover** `cover_uid`
  (manually chosen, otherwise the album's newest photo — computed by the backend) / name / **year range**
  via `formatCaptureRange` (only when the album has dated photos) / count → `/albums/{uid}`;
  `EmptyState` only for an album with no photos),
  `AlbumEditModal` (create/rename an album: name/description/private), `LabelEditModal` (create/rename
  a label: name/priority), `SelectionBar` (a sticky selection toolbar: count +
  actions + clear — used by browse grids outside the library, shown at
  `selection.count > 0` since those grids are hover-select too),
  `BatchActionBar` (**NEW**: the library's floating bottom **bulk action bar** — frosted
  (`--kk-header-bg` + `backdrop-filter: blur(--kk-header-blur))`, `--kk-shadow-3`, `.kk-batch-*`
  in `app.css`) `position: fixed` centered at the bottom, **slides up at ≥ 1 selected photo**, carries a live
  count (`aria-live`), **Vybrat vše** (`onSelectAll`), close (✕ = `selection.clear`) and the actions
  **Přidat do alba** / **Štítky** (add+remove, both via `MultiSelect` in a small `Modal`, options
  lazy from `fetchAlbums`/`fetchLabels` — the effect keys **only on `picker`** (+ a retry counter), never on
  `options.status`, otherwise writing `loading`/`ready` would re-run the effect and **abort its own fetch**;
  "already loaded" is held by `useRef`, a retry after an error bumps the counter, cache per session), **Oblíbené**, **Archivovat**, **Stáhnout**
  (`DownloadZipButton`), **Seskupit** (`StackSelectedControl`) and **Další úpravy** (the whole
  `BulkEditModal`); each metadata action runs **as a single `POST /photos/bulk`** via `bulkUpdatePhotos`,
  success/failure reported by a **toast** (`useToast`): success clears the selection and reloads the grid (`bulk.finish`),
  **a failure keeps the selection** (it can be retried). Driven by `useBulkEdit({hoverSelect:true})`; Esc clears the
  selection via grid keyboard nav. **Editor/admin only** (`bulk.canBulkEdit`), i18n `batch.*`),
  `BulkEditControl` (**a reusable trigger** for bulk editing: a button
  (`selection.edit`) + `BulkEditModal`, driven solely by the result of `useBulkEdit`; **it doesn't render at all
  for a viewer**, and is disabled at an empty selection — just drop it into `SelectionBar`, the page
  holds no dialog state; the optional `prefill` prop flows through into the modal), `SelectionStart` (**the counterpart** to `BulkEditControl`: a button
  `selection.enter` that turns on selection mode; it doesn't render for a viewer or for an already-enabled selection,
  `onEnter` overrides the action for a page that must first leave another mode),
  `DownloadZipButton` (**download the selection or the whole album as a ZIP** of originals: calls
  `downloadPhotosZip`, shows a spinner while it streams and an error on failure — 413 = over the cap
  (`download.zipTooMany`), otherwise generic (`download.zipError`); `photoUids` = the current selection,
  `albumUid` (+ `name` = the album title) = the whole album; **available to a viewer too** (a download is not a write),
  disabled when there is nothing to download. Inserted into the library's `SelectionBar` and into the album header),
  `StackSelectedControl` (**NEW**: a **Seskupit vybrané** button (`selection.stack`) in the library's selection bar
  (`LibraryPage`), **editor/admin only**, disabled until **≥ 2** photos are selected; calls
  `stackPhotos`, on success clears the selection and reloads the grid),
  `BulkEditModal` (**bulk edit** of the selection via `POST /photos/bulk`, the whole batch
  in a single transaction on the backend; the form is split into **four sections** (`.kk-text-eyebrow`
  headings): **Zařazení** (add/remove albums, add/remove labels — four `MultiSelect`s, so one
  apply handles **multiple albums and multiple labels at once**; the add fields additionally offer via `onCreate`
  **„Vytvořit «název»“** for a name that fold-insensitively matches nothing existing — only for
  users with write permission (`useAuth().canWrite`). A new item appears immediately as a chip
  (value `create:<název>`, `CREATE_PREFIX` — the colon doesn't occur in a base32 UID; the shared
  helpers `pendingValue`/`pendingName`/`pendingOptions` live in `lib/pendingCreate` and are also used by
  `useUploadOrganize`) and **is created
  only on Apply**: first `POST /albums`/`POST /labels` (defaults: empty description, non-private;
  priority 0), the fresh UID is swapped into the form and options — so a retry doesn't create a duplicate — and
  only then does the batch go; a canceled dialog creates nothing. A failed creation prints the server's message
  (`bulkEdit.createError`) and doesn't send the batch, the selection stays; when the batch fails only after creation,
  `bulkEdit.createdButApplyFailed` says the items already exist and only the assignment failed),
  **Metadata** (set/clear the description), **Poloha**
  (set/clear coordinates; above the `lat`/`lng` fields on `set` sits **the same `PlaceSearch`** as in the detail
  editor — it fills only those two fields, so the sent batch is the same as if someone typed the coordinates
  by hand) and **Příznaky** (private, archive, favorite); the set/clear pairs remain
  separate modes. **Destructive choices** (removal from an album/label, archiving) are in the danger key
  (`destructive` chips, `text-danger` label, `border-danger` select). Below the form is
  **`PendingChanges`** — a `.kk-surface` panel that says sentence by sentence what apply will do, and **how many
  photos it affects** (destructive rows in red + `visually-hidden` „(destruktivní)"; `aria-live`).
  A selection **over `LARGE_SELECTION` (50) photos** requires **explicit confirmation**: the first Apply only
  opens a danger alert („Ano, použít na N fotek" / „Zpět"), and **any form change revokes the
  confirmation**. Client-side coordinate validation + "at least one change" stays; after applying,
  a **per-photo result summary** from the response. A failed request **prints the server's message**
  (`ApiError.message` — a conflicting operation, too large a batch), otherwise a generic `bulkEdit.applyError`;
  the selection stays untouched so apply can be retried. The optional prop **`prefill`**
  (`BulkEditPrefill{addAlbums?,addLabels?}`, memoized — a new reference would reset the form)
  prefills the add fields on each opening (`/expand` puts the expanded collection there); `onDone` receives
  **`BulkEditOutcome{operations,result}`** — what apply actually sent and per-photo results — so
  the page can edit the list in place instead of refetching),
  `pages/` (`LoginPage`, `AccountPage` = identity/role, **the Jazyk section** (`LanguageSwitcher` +
  a hint, `account.language*`) and changing your own password, **plus the app's technical status**
  (`GET /healthz` badge + version, without the commit hash) in a small muted row at the bottom — status and language
  came here from elsewhere (from the home page and the navbar respectively): they belong where the user looks for them, not
  in front of the photos nor in a prime spot in the bar,
  `HelpPage` = **user help** (route `/help`, **no role gate** — every logged-in user sees it;
  the link is in the user menu under the name, the item „Nápověda" with a `question-circle` icon): a reading column
  with a short **table of contents** at the top and an `Accordion` (collapsible sections, open by default) that in plain
  language explains browsing, search, albums, labels, favorites/rating, people and faces, duplicates,
  shot variants (stacks), the map and places, deletion+trash, import and **roles** (what each role may do). Texts
  in the new top-level namespace `help.*` (cs/en); the first `Accordion` in the app,
  `LibraryPage` = the main photo library **and at the same time the app's home page** (route `/`):
  `FilterBar` above a virtualized infinitely-scrolling
  grid, loading/empty/error states, the whole view (filters+sort) in the URL, hearts
  on the tiles (favoritable; rating and pick/reject are only in the photo detail), **`SlideshowStart`**
  (a Promítání button + a duration estimate, the photo count comes from `total`),
  **two different empty states** — with active filters „Nenalezeny žádné fotky", whose hint
  **lists the active filters** (`buildChips(..., {facets, includeQuery: true})` joined by ` · `,
  album/label by title, not UID) and offers to clear them with one button,
  without filters „Zatím tu nejsou žádné fotky" with a CTA to `/upload` (editor/admin; a viewer gets only
  an explanatory sentence), distinguished via `hasActiveFilters(view)`,
  `LibraryRedirect` = a shim for the retired route `/library`: `<Navigate replace>` to `/` with the
  `search`+`hash` preserved literally (old bookmarks and links work, `replace` prevents a Back bounce),
  plus **the timeline** (`TimelineScrubber`) beside the grid for quick jumps to a month — the grid
  exposes `gridRef`+`onRangeChanged`, the jump runs via `useGridJump` (loads pages when the month
  lies beyond the loaded portion), shown only for the default newest sort and outside selection (`selection.count === 0`),
  plus for editors **a modern multi-select** — `useBulkEdit({hoverSelect:true})`: each tile has
  a corner checkbox (hover; Shift+click a range), **no „Vybrat" button** is needed anymore, and
  once something is selected, **`BatchActionBar`** slides up (a floating bottom bar: album/labels/favorites/
  archive/download/stack/more edits via the bulk API + toasts; on success `reloadKey` = **a background
  refetch, the grid doesn't flash to a skeleton**). Esc clears the selection,
  plus a **Uložit pohled** button (`SaveSearchModal` →
  `createSavedSearch` with the current view object as `params`),
  `SavedSearchesPage` = `/saved` (any logged-in user) „Moje uložená hledání": a list of the current
  user's saved views, each link opens the exactly restored view (`savedSearchHref`), plus
  renaming (`SaveSearchModal`) and **optimistic deletion** + empty state,
  `FavoritesPage` = `/favorites` the current user's favorites: the same grid/filters as the library
  scoped to `favorite=true`, hearts to remove from favorites in place (favoritable),
  the tiles carry the scope in the detail link (`detailQuery` with `favorite=true`) → Esc/Back/prev-next from a photo returns here,
  for editors **selection mode** → `BulkEditControl`; a bulk removal from favorites drops the photo from the list
  (the selection is cleared **before** the refetch, so no photo that vanished from the grid stays in it),
  `AlbumsPage` = `/albums` a grid of album cards + `Nové album` (editor/admin) — the order **from
  the newest album** (by the newest photo, undated/empty at the end) **is enforced by the backend**,
  the page doesn't reorder and has no sort selector; after creating an album it **reloads the list**
  (`useReloadKey`) instead of locally appending to the end — where a new album belongs is known only to the server,
  `AlbumDetailPage` = `/albums/:uid` a header + a **Promítání** button (for everyone) + editor actions
  (edit/delete/select) above
  a photo grid scoped to the album (`useScopedPhotos` + `FilterBar showSort={false}` + URL state) —
  an album is **always chronological** (oldest first, enforced by the backend), so the page has no sort
  selector or manual reordering; selection → set cover / **bulk edit**
  (`BulkEditControl`) / remove from the album (both removal and a successful edit **empty the selection**, so no
  UIDs of photos that vanished from the grid stay in it, and reload the grid via `reloadKey`); the tiles carry the
  album scope in the detail link (`detailQuery` with `album=uid`) → Esc/Back/prev-next from a photo returns to the album;
  the page either browses or selects (`selection.active`),
  `LabelsPage` = `/labels` a list of labels with counts + create/rename/delete (editor/admin),
  `LabelDetailPage` = `/labels/:uid` a photo grid scoped to the label (`useScopedPhotos` + `FilterBar` + URL);
  the tiles carry the label scope in the detail link (`detailQuery` with `label=uid`) → Esc/Back/prev-next from a photo
  returns to the label; + a **Promítání** button + for editors **selection mode** → `BulkEditControl` (refetch on success),
  `SearchPage` = semantic/hybrid/fulltext search: a prominent debounced (350 ms)
  search field + a mode toggle (`q`+`mode` in the URL), the same virtualized grid as the
  library + the shared `FilterBar` (without query/sort), `degraded` → a non-blocking notice
  (sidecar offline), idle/loading/empty/error states (an empty result **repeats the query** —
  `search.empty.hintQuery` „Pro «dotaz» jsme nic nenašli…“ — and advises loosening the narrowing; the error is
  `ErrorState` with Retry); the field speaks **the search language**
  (`q` = free text + `klíč:hodnota` filters, grammar in docs/API.md „Vyhledávací jazyk (q=)“;
  parsed exclusively by the backend): the input is `SearchQueryInput` (`components/search/`) — a combobox
  with **filter-key autocomplete** (suggestions from `lib/queryLanguage.ts` `suggestFilterKeys`/
  `applyFilterKey` + `FILTER_KEYS`; arrows + Enter/Tab accept `klíč:`, Esc closes, values are
  never completed), beside the label `SearchQueryHelp` (a `?` button → a modal with operators and filters
  with examples, rows from `QUERY_HELP_ROWS`/`QUERY_HELP_OPERATORS`, texts `search.help.*` cs+en),
  and `unknown_tokens` from the response (`PhotoListResponse.unknown_tokens` → `usePaginatedPhotos`
  returns `unknownTokens`) → a non-blocking info hint „těmto filtrům nerozumím“ above the grid;
  a pure filter query returns `mode: "filter"` (`EffectiveSearchMode`); the tiles carry the search scope in the detail link
  (`detailQuery` with `q`+`mode`) → Esc/Back from a photo returns to the search (sorted results, not the library with `q`
  as a substring) and prev/next pages the same results, plus above the grid a **cross-entity section**
  (`GlobalSearchSections`) with chips of matching albums/people/labels (grouped `GET /search/global`), so a
  text query surfaces non-photo entities too, plus in the header **`SlideshowStart`** (scope `{mode}`,
  so the slideshow plays **the search results**, not the library filtered by the substring `q`)
  and **the single entry point to saved searches**
  (`SavedSearchesDropdown` — list, open, „Spravovat" → `/saved`) beside the **Uložit pohled** button
  (`SaveSearchModal` — `params` carries `mode` too, so restoration targets `/search`),
  plus for editors **selection mode** over the results → `BulkEditControl` (on success the search
  replays via `reloadKey`); changing `q`/`mode` is a different result set, so it **leaves selection mode**
  (filters that only narrow the same search keep the selection, just as in the library),
  `UploadPage` = multi-upload (drag-and-drop + gallery/camera on mobile, **mobile-first**):
  `DropZone` above a **sticky** `UploadProgressHeader` (the batch's overall progress) and a virtualized
  `UploadList` (`UploadItem` rows), start/clear controls + a **jen neúspěšné** toggle (the filter
  `showErrorsOnly` for failed files); a completed summary + a link to the newly uploaded photos
  (`/?sort=added`, via `LIBRARY_PATH` in the header) and retry-failed are in `UploadProgressHeader`; above the queue
  `UploadOrganize` — before uploading you can pick **albums and labels** for the whole batch, and after all files
  settle **all** recognized photos (new **and** duplicate `resolvedUids`) are assigned
  by a single `POST /photos/bulk` (state „přiřazuji…“, success, or a **retryable** error — the photos are
  uploaded, only the assignment failed); with no selection no call is made,
  `ImportPage` = `/import` (maintainer only) the import/migration console: two sections (PhotoPrism,
  photo-sorter) with a **Spustit import** button (gated on the `sources` flags), the live progress of a running run
  (spinner + imported/updated/skipped/failed counts) and the background queue state (`GET /jobs/stats`),
  plus a **run history** table (`import_runs`: source/start/end/status/counts/error); it polls
  `GET /import/runs` + `GET /jobs/stats` every 3 s, 409 → „už běží", a confirm before the first (large) run of a
  source, self-gated on `canImport` (= maintainer). The history also shows runs of the **`folder`** source (`kukatko import dir`,
  reads a directory on the server's disk → **has no button**, it just appears in the table): in `services/import.ts`
  therefore `RunSource` = `ImportSource | 'folder'` (the launch sections stay `SOURCES` =
  photoprism/photosorter), the label `import.source.folder`,
  `MaintenancePage` = `/maintenance` (maintainer only) the library maintenance console: a **Spustit kontrolu** button
  (`GET /maintenance/scan`) → a summary of totals + a findings table (count + samples per class, or „knihovna
  konzistentní"), repair checkboxes (thumbnails/embeddings/faces/hashes/import of orphans — annotated
  with the remaining count from the last check) → **Spustit opravy** (`POST /maintenance/repair`) with a result
  summary, plus the background queue state (`GET /jobs/stats` polls every 3 s) as progress; **every finding,
  the summary „drift" row and every queue state carries a quiet plain-language explanation** (without hovering) —
  `maintenance.findings.descriptions.*`, `maintenance.scan.summaryHint`, `maintenance.jobs.intro`
  and the shared `JobStateLegend` (total/queued/running/failed/**dead**) — so a maintainer knows what a count
  means and whether action is needed; plus the destructive card **`AuditPurgeCard`** (**Vymazat audit log**)
  with a retention choice (presets 3/6 months, 1/2 years or a custom number of days), a **confirmation step**
  (irreversible deletion) and a result `Alert` with the deleted count (`purgeAuditLog(olderThanDays)` →
  `POST /maintenance/audit/purge`); self-gated on `isMaintainer`,
  `SystemStatusPage` = `/system` (maintainer only) a **system-status dashboard**: auto-refresh (polling 5 s)
  `GET /system/status` → a card grid (DB, embeddings, job queue, backup, imports, storage,
  **maps**, version) with **quick actions** — *requeue dead jobs* (`requeueDeadLetterJobs`: list dead →
  per-job `POST /jobs/{id}/requeue`), *run a backup* (`POST /backup`), links to the import flow
  (`/import`) and the maintenance check (`/maintenance`); **box offline** + pending embeddings → a highlighted
  message „doženou se po návratu"; **the Mapy card** (`MapsCard` over `status.maps`) shows the latest
  mapy.com status — `key_rejected` in red + what to do about it (swap the key in the mapy.com console), degradation
  in yellow, without a key „Nenastaveno"; the job queue card carries the shared `JobStateLegend`
  (total/queued/running/failed/**dead**/**pending** = „Čeká na box") with a plain-language explanation of
  each state (`jobStates.*` + `system.jobs.intro`); it also carries **the Oznámení card** (`AnnouncementCard`,
  gated `isMaintainer`) — a textarea + a level `<select>` (info/warning) + **Zveřejnit**/**Zrušit oznámení**
  over `setAnnouncement`/`clearAnnouncement`, prefill of the current message via `fetchAnnouncement`, feedback via
  the same dismissible `ActionNotice` `<Alert>` pattern; loading/error/notice states, self-gated on `isMaintainer`,
  `UsersPage` = `/users` (admin **or** maintainer, `isAdmin`) **account management**: a user table (username, full name, role,
  status, note, last login, created) over `GET /admin/users`, the dialogs **Nový uživatel**
  (username/password/role/name/note) and **Upravit** (role/name/note; username is `readOnly`
  `plaintext` — the backend cannot change it), **Změnit heslo** for another user (logs them out of all
  devices; the hash is never rendered anywhere) and **Povolit/Zakázat** behind a confirmation dialog
  (`setUserDisabled`); **your own row has a `disabled` toggle** + a short explanation of why
  (`users.selfDisableHint`), **deletion is not offered** — an account is retired by disabling it, so the history
  (photos, ratings, audit) stays whole. **The maintainer boundary** (mirrors the backend
  `authorizeUserManagement`): the **maintainer** role may be granted only by a maintainer — the role
  `<select>` doesn't offer it to a non-maintainer at all (`ROLES.filter`, prop `isMaintainer`) — and a maintainer account may not
  be edited / re-passworded / disabled by a non-maintainer, so its three row actions are `disabled` with the hint
  `users.maintainerManageHint` (`canManage = isMaintainer || role !== 'maintainer'`). API validation errors map to a specific field
  (`fieldErrorFor`: 409 → username, 400 by keyword → password/role/note, otherwise
  a form-level alert), not to a generic banner. States: a **skeleton** (`Placeholder` in the table) while loading,
  an error alert with **Zkusit znovu**, an empty state (`EmptyState`, practically unreachable — the bootstrap
  admin always exists, but must not crash); self-gated on `isAdmin`,
  `AuditPage` = `/audit` (admin **or** maintainer, `isAdmin`) an **audit log**: a read-only table of records from `GET /audit`
  newest first (when/who/action/target/IP), the `details` JSON via an expandable row (`aria-expanded`,
  also shows `user_agent`). If `details` carries a `changes` map (the edit convention of `internal/audit`, see
  `AuditChange`/`AuditChanges` in `services/audit.ts`), it is rendered by `readChanges`+`ChangesTable` as
  a compact table **pole / původní / nová** (`data-testid="audit-changes"`, a cleared field =
  `null`/`""` → a muted dash via `ChangeValue`); records without `changes` (legacy, non-edit actions)
  fall back to the existing `JSON.stringify`. Filters (actor = a `<select>` over the roster via `fetchUsers`, action, entity type+UID,
  date range `od`/`do`) in a **draft** form → **Filtrovat** writes them to the URL and resets
  the page, **Zrušit filtry** clears them; the dates are expanded in `viewToParams` to RFC 3339 day boundaries
  (UTC). prev/next pagination over `offset`/`next_offset` (limit 100) with a `od–do z total` count;
  filters and offset live in the URL (`useUrlState` over `AUDIT_DEFAULTS`), so Back restores the exact view.
  Actor names are fetched from the roster **best-effort** (fallback to UID, or `—` for a system action),
  never blocking the table render. Loading/empty/error (retry via `reloadKey`) states, self-gated on
  `isAdmin`,
  `PhotoDetailPage` = `/photos/:uid` a **full-canvas immersive viewer** (and the route itself;
  **outside `Layout`**, like `/slideshow` — the photo owns the whole viewport, no navbar/footer).
  The photo is centered, `object-fit: contain` at the **largest fit without cropping** over a **warm near-black
  backdrop** (`--kk-viewer-backdrop`), reflecting the saved non-destructive edit (a live draft while the Úpravy
  panel is open) — for a **video** `VideoPlayer` instead of the image, for a **live photo** `LivePhoto` (both have
  their own native fullscreen; the image viewer doesn't open for them). The style is in
  `components/photo/viewer.css`, the `--kk-viewer-*` tokens (backdrop, chrome/panel scrim, z-index) in
  `tokens.css`. **It replaced the old click-opens-lightbox** — `Lightbox` and `lightbox.css` were removed
  and absorbed here.
  **Disappearing chrome:** the top action bar (title + curatorial loop + toggles) and the **‹/› arrows**
  after a short idle **dim away** and return on mouse move / tap / key
  (`useAutoHideChrome` — an idle timer + a global wake, `paused` when the drawer is open, so a control
  under your hand doesn't vanish); the transitions run on duration tokens, so `prefers-reduced-motion`
  turns them off. **The persistent close ✕** (a circle at top left, `photo.back`, **doesn't disappear** with the chrome) and **Esc**
  always work and return **to the exact previous scroll position**: `navigate(-1)` when you arrived here from
  the grid (the browser restores scroll), otherwise (a direct link/refresh — caught by `location.key === 'default'`
  at mount) `backHref(view)` reconstructs the list URL. **Keys:** ←/→ steps through neighbors, `f`
  favorite, `m` faces, `i` drawer, Esc **a step back** (first the selected face, then the drawer, then
  out); rating hotkeys `0`–`5`/`p`/`r`/`v` on document (except while typing into an input).
  **prev/next** = `<Link replace>` `‹`/`›` carrying scope+filters from the URL (`detailQuery`) **and `info`**,
  respecting the source listing's order (`usePhotoNeighbors` over `neighborParams`+`mode` — `GET
  /photos`, or `GET /search` when the detail came from a search; stop at the ends); **touch**:
  `usePinchZoom` (pinch/double-tap zoom + pan + swipe on a plain still) or `useSwipeNavigation`
  (swipe when faces/edit are on, where zoom is off so the transform doesn't shift the boxes/preview);
  neighbor preload (`new Image()` on `fit_1920`). **Paging without a full-page flicker** — only the first
  load shows a big spinner, otherwise the current photo stays mounted (the `<img>`/figure key on the
  **displayed** `photo.uid`, not the route `uid`) and the new one is fetched in the background, then **swapped in
  place** with a fade/scale; a corner spinner glows over the shot (`photo.loadingNext`). While a neighbor is loading
  (`loadingNext = photo.uid !== uid`), faces are suppressed (photo B's boxes aren't drawn over
  A); an abort on a `uid` change cancels the leapfrogged request (the last target wins).
  **Deep-linkable:** the open photo is in the route, **the drawer state in the `info` query param** (outside
  `DetailView`/`DETAIL_DEFAULTS`, so it doesn't leak into the neighbors or into `backHref`), scope in the query — so Back and
  refresh line up. In the header `RatingStars`+`FlagControl` (per-user stars 0–5 + a personal flag
  eye/👍/👎 over `useRating`) and `FavoriteButton` (shares the optimistic toggle with `f`). Beside it is
  **Archivovat/Vrátit z koše** (editor+ only per `canWrite`, as with bulk archiving): `archivePhoto`
  sends the open photo to the trash, `unarchivePhoto` restores it (a photo opened from `/trash` arrives already
  archived); **you stay on the page** — `archived_at` is toggled in place (the icon flips
  `archive` ⇄ `arrow-counterclockwise`, the label Archivovat ⇄ Vrátit z koše) and the result is reported by a toast.
  **The viewer carries
  exactly ONE image of the photo** — faces are a **toggleable overlay** over it (`FaceOverlay` over
  `useFaces`), never a second copy of the shot, and even the **Úpravy** panel edits this one shot.
  **Faces are OFF by default** (`FACE_OVERLAY_DEFAULT = false` in `lib/faceOverlayPref`, the choice
  is remembered in localStorage): the photo is content, the boxes are opt-in. They are turned on by the **Zobrazit/Skrýt
  obličeje** button (only on a still with at least one face, `aria-pressed`) or the **`m`** key (in the shortcut
  registry, so the `?` help shows it too). When localStorage remembers **faces on**, the drawer
  **opens by itself on the faces panel** on load (an effect on the edge of `facesAvailable`, once), so
  the saved choice shows the panel too, not just boxes over a closed drawer; a later manual close is respected
  and the open state continues to travel in the `info` param. The drawer is **one panel with three mutually exclusive
  views** — faces, edits, or metadata („Informace") — driven by `sidePanel: 'faces' | 'edit' |
  null` (`showInfo = !showFaces && !showEdit`): **faces and edits are separate views, metadata
  belongs only to the info view**, so turning on faces/edits **doesn't drag the whole info panel along** (previously
  the metadata was drawn beneath them — a reported bug). The **Informace** button from faces/edits **switches** to
  metadata (discards the lead and the overlay/selection), from already-shown metadata it **closes** the drawer. **Turning off**
  faces/edits **closes** the drawer (it is not "show metadata"). In the faces/edits view the header is carried by
  its own panel (`FacesPanel`/`EditPanel` have a title + close), so the generic header
  „Informace" (`.kk-viewer__panel-head`) glows **only in the info view**. The same `sidePanel` drives the boxes and the faces
  panel, so they can't diverge. **The whole faces UI stands
  (the button and `m`) only when the preview is the identity** (`isIdentityEdit(previewEdit)` in `facesAvailable`):
  the transform of a live or saved edit shifts the rendered pixels under boxes positioned in percentages of the wrapper
  — the frames would miss the faces, so they'd rather not draw and come back once the preview is neutral again.
  **Watch out — a load-bearing invariant:** `FaceOverlay` positions boxes in **percentages** of the `.kk-viewer__figure` wrapper,
  whose box **must sit exactly on the rendered image**. So the figure gets an **inline `aspect-ratio`**
  from the photo's stored dimensions (`displayFrame(file_width, file_height, file_orientation)` — orientation 5–8
  swaps the sides) and `data-framed='true'`: this way it fits into the stage via „contain", but its box is
  **exactly the image** (no letterbox bars into which percentage boxes would drift), in both the width- and
  height-limited fit. If the dimensions were missing (`data-framed` isn't set), it falls back to a bare
  `inline-flex` shrink — a frameless photo carries no face geometry anyway. (jsdom doesn't catch the letterbox
  — verify the geometry visually; previously the figure just shrank to the `<img>` and when the stage was narrowed by the panel
  it stretched, so the **frames drifted apart**.)
  The boxes are colored by state (`lib/faceState`), the selected one is primary + a ring, they carry a **number `#N`** and
  for assigned ones **a name under the box**; hovering a box highlights the row in the panel and vice versa (`hovered`/`onHover`
  held by the page). A click on a box or on a panel row = the same selection (and opens the drawer).
  **The information runs in the drawer** (`.kk-viewer__panel`), which **slides in from the side on demand** (on a phone
  full-width with a scrim, at ≥ md the **stage narrows** on the left so the photo doesn't vanish behind the panel; together
  with the stage the **top bar and the `›` arrow** also move over by the drawer's width (`--kk-viewer-panel-w`), so the panel toggles
  and paging stay **visible beside the drawer, not under it**) —
  the default state is just the photo. Its content is **the same components as before, only in the drawer instead of below
  the photo** (the `OrganizeBadges` „filed under" strip above the photo is gone — albums/labels are in Uspořádání).
  **The drawer's sections**
  (`components/photo/`): **1. Uspořádání** (`sections.organize`) = **the primary block, always
  visible and directly editable** (no "edit mode"): `OrganizePanel` (inline add/remove of albums
  and labels via the organize API) + `PeoplePanel` (people/faces as **person-chips** over the same
  `useFaces` that drives the overlay — answers "who is in the photo" even with faces off; **it assigns nothing
  itself**: an editor's click on a chip calls `onEditFace` → the page turns on faces and selects that
  face in `FacesPanel`, so assignment lives in exactly one place. A viewer sees named
  people read-only; named = a rose chip, an unnamed detection = a neutral chip); albums/labels/people have
  a distinct color via `ENTITY_STYLE` (`components/entityStyle`). Adding runs through
  **`AddAutocomplete`** (a type-to-filter combobox over react-bootstrap primitives,
  **case/accent-insensitive** via `lib/text` `foldedIncludes`, keyboard ↑/↓/Enter/Esc + click,
  a „nic neodpovídá" state, ~44px tap targets, ARIA combobox/listbox; an optional `onCreate` prop adds
  a „Vytvořit «dotaz»" row — `createAndAttachLabel` does `createLabel` + `attachLabel`, matches the name via
  `foldedEquals`, so it just attaches an existing label instead of colliding on the slug; albums are
  not created here — type/cover/privacy belong on the Alba page). **2. Popis a místo**
  (`sections.caption`) = `MetadataPanel` = title/description/ai_note/notes/taken_at/location
  **read-only until the editor clicks a field** — each field is its own inline edit affordance
  (`EditableField` = the whole row is an „Upravit «pole»" button with a pencil icon and a muted „Přidat…"
  placeholder on an empty field), **no hidden global „Upravit"** at the bottom (that was this task's
  fix — discoverability of editing the title/description/AI note). A click on any field opens one
  shared form (title/description/ai_note/notes/taken_at + **an approximate date** +
  **a visual location picker**), Save `updatePhoto` PATCH, Cancel reverts. **Save/Cancel are always at the bottom:**
  the form is long (the map is 260 px), so a quick caption edit would otherwise mean scrolling all the way to the button.
  `MetadataPanel` therefore **portals** the action bar (`.kk-viewer__panel-actions`) (`createPortal`) into
  **the drawer's non-scrolling footer** — `.kk-viewer__panel-foot` (`flex: none`) beside the scrolling
  `.kk-viewer__panel-body`; `PhotoDetailPage` hands it that node via the `footer` prop. The buttons call
  `save`/`setEditing(false)` directly (not a form submit, which the portaled button couldn't reach), so
  they work even outside a `<form>`. **Not `position: sticky`** — that pins only while its own section scrolls,
  so on a tall (4K) monitor where the whole form fits, it never pinned; the footer pins
  always. Without a footer (the panel outside the viewer) the bar drops inline at the end of the form.
  **The approximate („cca") date** — for scanned/inherited photos where nobody knows the exact date:
  in the form a „Datum je odhad" checkbox (`taken_at_estimated`) and **only when it is checked** a text
  field „Poznámka k datování" (`taken_at_note`, `maxLength=500` mirrors the backend cap) — an empty
  note on a date fact makes no sense, so it doesn't clutter the form; both are saved by the same PATCH (no
  separate button). Unchecking the estimate leaves the note in the form (in case they change their mind), but
  only `taken_at_estimated: false` is sent — the server deletes the note. Read-only, the estimated date is
  rendered via `CaptureDate` (in `MetadataPanel.tsx`): a `cca` (cs) / `c.` (en) badge + the date +
  the note in italics, the badge carries a `title` with the note (**not** just color/glyph), so an estimate can't be
  mistaken for a certain date even at a glance or in a screen reader; a photo **without** `taken_at` can
  be an estimate too — then the marker with the note stands on its own. `EditableField` therefore takes an optional
  `display?: ReactNode` (a richer render of the value, plain `value` still decides "filled-ness"). The location picker = **three ways in** in the order a person reaches for them:
  **`PlaceSearch`** (find a place by name), one tolerant coordinate field parsed by
  `lib/coordinates` (`parseCoordinates`/`formatCoordinates`: decimal degrees `49.1234, 16.5678`,
  DMS `49°7'24.2"N 16°34'12.5"E`, degrees-decimal-minutes, hemispheres, axis reorder, range check)
  and the **`LeafletMap` picker mode** (`picker={position,onPick}`: a draggable marker + click-to-place,
  two-way sync text↔marker, clear location = lat/lng null). All three **write the same single
  coordinate field** that save reads — so they have no way to contradict each other about the location. **The PATCH carries only actually-changed
  fields**: an unchanged `taken_at` (the field is `step=1`, holds seconds) would flip `taken_at_source`
  `exif`→`manual`, unchanged coordinates would be rounded to the text field's 6 decimal places —
  both would silently overwrite the catalog. **Invalid coordinate text = an inline error at the field**, not a block of
  the whole form: the other fields save, the location stays unchanged and the form stays open
  (Save is **not disabled**).
  **An estimated location** (`location_source === 'estimate'`, see `internal/geoestimate`) in the read-only
  Poloha row is rendered via `EstimatedLocation` (in `MetadataPanel.tsx`): an `odhad` (cs) /
  `estimate` (en) badge with a `title` „Odhad podle fotek z téhož dne, ne změřená poloha" + a one-line
  explanation of where it came from — **a labeled badge and a sentence, not a subtler shade**: an estimated location that
  looks the same as a real one is a lie the app tells the user, and color alone tells a
  screen reader nothing. Below it the editor gets **two ways out** (a viewer sees only the marker — they too should know
  the pin is a guess): **Potvrdit odhad** sends `{location_source:'manual'}` — just the source, **never** the
  coordinates back (they would be rounded to the 6 decimal places the form rendered, and the pin would
  shift as the price of agreement) — and **Zahodit odhad** sends `{lat:null,lng:null}`, which the backend
  records as a decision (`manual` without coordinates) and **won't offer the same guess again** (the help text says so
  outright, instead of the user finding out by it never coming back). Both are their own one-click
  request (`resolveEstimate`, their own busy/failed state) outside the form's Save — it is an answer to a question
  the app asked, not the edit the user came for; `location_source` is read from `photo`,
  not from form state, because it is a fact about the stored row. A location from EXIF or one with an **unknown** source
  (`''`, older rows) is **not marked** — "we don't know" is not "we guessed".
  **IPTC/XMP credits** (the `credits` sub-section in the same form, **collapsed on first render**,
  a chevron toggle `aria-expanded`/`aria-controls` like `TechnicalDetails`) — belong on scanned/inherited
  photos where neither EXIF nor imports know anything about the author/year: text fields **Předmět** (`subject`),
  **Umělec** (`artist`), **Autorská práva** (`copyright`), **Licence** (`license`), the chip field
  **Klíčová slova** (`keywords`) and a **Sken** (`scan`) checkbox. They are saved by **the same** `updatePhoto` PATCH
  (no second button/form/request); the fields' `maxLength` mirrors the backend `creditLimits`
  (subject/copyright/license 1000, artist 255). Keywords = a single comma-separated string in the DB,
  edited as chips via `KeywordsInput` (the shared `badge rounded-pill` + `ENTITY_STYLE.tag` look,
  but **not** labels — no link to `/labels/:uid`): Enter/comma/pasting „a, b" adds, a click on the cross
  removes, Backspace in an empty field drops the last one, blur commits the half-typed word; the helpers
  `addKeywords`/`joinKeywords`/`sameKeywords`/`splitKeywords` (`lib/photoFacts`) trim,
  de-duplicate and guard a 2000-rune cap on the joined string (rune-count = Go `utf8.RuneCountInString`).
  Credits go into the PATCH **only when they actually changed** (the form normalizes: trim + rejoin, so
  an unchanged field would overwrite the wording from the source file); an emptied field is sent as `""`
  (deletes), a failed PATCH **keeps** the half-typed values and shows the existing `saveError` alert.
  The PATCH's response is the full detail (`albums`/`labels`/`files`), with which the
  page replaces the held photo; the read-only location = `PhotoLocation` (a mini-map over the mapy.com proxy + on-demand
  `reverseGeocode`) **embedded** in this block. **3. Technické údaje** (`TechnicalDetails`,
  **closed on first render** expander `aria-expanded`/`aria-controls`): **everything the app knows about
  the photo**, in **groups** (`MetaGroup` = a heading + `<dl className="row">`, two columns on a wide
  viewport, one on a narrow one; long values wrap, never stretch the page):
  **Fotografie** (camera/lens/aperture/exposure/focal length/ISO, serial number, software, capture
  date source, IPTC/XMP credits `subject`/`artist`/`copyright`/`license`, `keywords` as **chips**
  split on the comma, `projection` + a badge row `private`/`scan`), **Soubor** (name, `original_name`
  only when it differs, format from MIME, size — the exact byte count in `title`, dimensions, **aspect ratio**
  and **Mpx** (computed), EXIF orientation 1–8 as a label, color profile, `image_codec`, a shortened
  SHA256 with the full value in `title` and **copy-to-clipboard**, added/changed), **Poloha**
  (coordinates, `altitude`, + a **cached** `place` from the detail — country/region/city/place; **no
  on-demand geocoding**, only `PhotoLocation` does that on demand), **Video** (only `media_type`
  `video`/`live`: duration `m:ss`, codecs, audio yes/no, fps) and **Původ** (Nahrál/a
  `photo.metadata.uploadedBy` from `photo.uploader.name`, fallback `—` `uploaderUnknown`, +
  `photoprism_uid`/`photosorter_uid`). All **read-only** (editing belongs in `MetadataPanel`);
  **a field with no value doesn't render at all** (`MetaField` returns `null`) and **an empty group also
  doesn't render** — a photo with poor metadata is not a wall of dashes. Numbers/dates via the active locale
  (`i18n.language` → Czech has a decimal comma). **Service actions here** (editor/admin only, `canWrite`): `RegenerateThumbnailButton`
  (`components/photo/`) inside the expanded expander calls `regenerateThumbnail(uid)` (POST
  `/photos/{uid}/regenerate-thumbnail`), shows **pending** (spinner + `disabled`), then success
  or an error (422 = „originál chybí nebo ho nelze dekódovat", otherwise a generic message); on success
  it calls `onThumbnailRegenerated`, which in `PhotoDetailPage` **bumps `thumbVersion`** and appends
  `?v=` to `poster` (the thumb URL is built from the UID, thus stable → a cache-bust forces loading the new
  thumbnail without a hard reload). A viewer doesn't see the button. **Edits are the drawer's lead slot** — they belong
  to the photo they edit, so `EditPanel` (editor/admin, still only) is opened by the **Úpravy** button
  (`aria-pressed`) in the action bar; turning it on **opens the drawer** and mounts the panel at its head (the same
  one `sidePanel` as faces, see above), the header carries a title + a closing **`x-lg`**
  (`photo.edit.closePanel`). Rotation/brightness/contrast/
  crop, `PUT /photos/{uid}/edit` via `saveEdit` — which sends **only the edit itself** (`rotation`/
  `brightness`/`contrast`/`crop_*`): the `PhotoEdit` type also serves as the GET response and additionally carries
  `photo_uid`/`updated_at`, but the PUT body decodes **strictly**, so sending the returned object
  straight back = 400 „malformed JSON body" (this used to crash the save; a missing crop field is
  simply omitted, which the API reads as „without crop"). **It has no `<img>` of its own** — it is a **controlled
  component**: the in-progress edit is held by the page (`editDraft`, `null` = nothing unsaved), the panel reports it
  up via `onChange` — and as **an updater `(prev) => next`, not a finished value**: two
  controls changed in one React batch read the same not-yet-rerendered `edit` prop, so
  composing the next value in the panel = **silently discarding that first change** (the page applies the updater via
  `applyEdit`, the first change builds on `state.edit` because the draft isn't there yet) — and **the preview is that
  ONE original photo up top**
  (`editPreviewStyle(previewEdit)`, `previewEdit = editDraft ?? state.edit`) — so it stays visible the whole
  time and changes live under your hands. Closing or jumping to a neighbor (the `uid` effect) discards the draft
  (the photo returns to the saved state), a successful save swaps it for `state.edit` without a flicker.
  Opening Úpravy also **removes the faces** (one lead slot) and the face selection, but
  **doesn't overwrite the saved overlay choice** — the hiding is a consequence of opening Úpravy, not a decision about
  faces, so it survives to the next photo. A viewer sees everything read-only
  (no Úpravy button, no edit/add/remove actions, no privacy toggle, `FaceOverlay` readOnly
  = sees the boxes but can't click);
  `StackStrip` (`components/photo/`, **NEW**) = **a strip of stack variants** in the viewer's drawer: it lists
  each member (preview, name, dimensions, size), marks the **primary** (`stack.primary`) and links to
  any variant (`stack.viewVariant`); for an editor, per-member buttons **Nastavit jako hlavní**
  (`stack.setPrimary` → `setStackPrimary`) / **Vyjmout ze skupiny** (`stack.unstack` → `unstackMember`)
  and **Zrušit skupinu** (`stack.unstackAll` → `unstackAll`). It is rendered by `PhotoDetailPage` **in the drawer**,
  only when `stack_members` has **≥ 2** items; its actions reload the displayed photo;
  `components/photo/` also carries `MetaField` (one read-only labelled `<dt>`/`<dd>` row inside
  a `<dl className="row">` group, an empty value = nothing; an optional `title` = a tooltip over the shortened
  value and `children` = a rich value (chips/badge/copy button), a row with `children` renders
  always — the caller decides about emptiness); `lib/photoFacts` = pure derived facts about a file
  (`aspectRatio` — a fraction reduced via gcd, decimal fallback `1,50 : 1` when it doesn't reduce to legible
  terms; `megapixels`; `formatMime` → `JPEG`/`MOV`; `orientation`/`takenAtSource` = narrowing to a
  literal union so the `t()` key stays typed; `splitKeywords`; `shortHash`), `lib/format`
  `formatBytes(bytes, locale?)` (locale = decimal comma) and `formatByteCount` (the exact byte count
  for the tooltip); `lib/photoEdit` = pure helpers
  edit→CSS (`editPreviewStyle`/`editFilter`/`editTransform`/`cropClipPath`/`isIdentityEdit`/
  `rotateRight`/`hasCrop`/`NEUTRAL_EDIT`),
  `PeoplePage` = `/people` a people index: a responsive `SubjectTile` grid (image/name/photo
  count), for editors a link to cluster review; the tile shows **that person's face** — what exactly
  is decided by pure `lib/subjectTile.ts` `subjectTileImage` → `{kind:'cover'|'face'|'none'}`:
  an explicit `cover_photo_uid` always wins (a decision overrides a guess), otherwise `cover_face` from the API
  (marker selection see `listSubjectsSQL`) `padBbox(0.3)` + `squareCrop` → `FaceCrop`, and without
  a usable face a placeholder remains (`people.noCover`) — the app doesn't invent a face,
  `SubjectPage` = `/people/:uid` a person's page: a header (name/type + edit via
  `SubjectEditModal` + the shared `GridDensityControl` **Dlaždic na řádek** — a view preference
  open to anyone who sees the page, not just editors; the grid carries `data-density` for tests and
  holds the shared `GRID_GAP_PX` like the other galleries), a paginated gallery (`useSubjectPhotos` +
  `SubjectPhotoTile` with a „set as cover" action for editors — now a **quiet icon-only disk** in the corner
  of the tile: hidden at rest, revealed on hover/focus (on touch, where there is no hover, it stays visible),
  the current cover is marked by a filled accent disk (`.kk-cover-btn`/`--on`, `image`/
  `image-fill`); behavior unchanged — the same `onSetCover` handler and `PATCH /subjects/{uid}`.
  The tile also carries the library's **corner selection checkmark** (`.kk-tile__check`, props
  `selectable`/`selectFirst`/`selected`/`anySelected`) from the outset for an editor, and the
  cover disk only steps aside at `selectFirst` — i.e. once something is picked), and
  two review sections for editors only: `Candidates` („Možná je i zde" — untagged photos where the person
  is present by face resemblance, to confirm/reject; the search is **explicit** via a button, not
  on-load) and below it `Outliers` (suspicious assignments); the tiles carry a **person
  scope** in the detail link (`detailQuery` with `person=uid`, `DETAIL_DEFAULTS` + just that facet) → prev/next
  in the viewer pages this person's photos (`GET /photos?person=uid`), not the whole library; the gallery
  (`GET /subjects/:uid/photos`) and the person facet sort **identically** — `taken_at DESC NULLS LAST, uid DESC`
  (the backend unified the tiebreaker `internal/people/subjects.go`), so the viewer steps exactly in the order of
  the grid even among photos with the same/no date; editors can **select** in the gallery
  → `BulkEditControl` (refetch the gallery on success) — in selection mode a tile is one
  selection target, so „set as cover" steps aside, like the heart/stars on a library tile,
  `ClustersPage` = `/people/clusters` (editor/admin) a review queue of unnamed clusters:
  `ClusterCard` (a representative + samples + removal of a strayed face + one-shot naming
  of the whole cluster) in a `Row`/`Col` grid, optimistic removal after naming,
  `FacesPage` = `/faces` (editor/admin, a link in „Nástrojích") „najdi osobu mezi neotagovanými
  fotkami": the config panel `CandidateSearchForm` (person selection via `AddAutocomplete` with the photo count
  in `hint`, a threshold in **percent** 20–80 % with bookends „Více výsledků"↔„Lepší shody", limit, a
  Hledat button — the search is **explicit**, not live-on-drag), calls `searchCandidates()` (percent→
  distance conversion via `percentToDistance` from `lib/faceThreshold`), `CandidateStats` shows the source photos/
  faces, matches found, done, and the **computed `min_match_count`** with an explanation; `CandidateFilterTabs`
  (Vše/Nové/Přiřadit/Hotovo with counts, also scopes „Potvrdit vše"), `CandidateLegend` + `CandidateCard`
  (`CandidateFaceImage` = a **full `fit_720` preview** with the face as a **colored rectangle** via
  `faceBoxStyle`, not a cropped chip; color/badge/rectangle share one code via the bucket `new`/`assign`/
  `done` in `lib/candidateReview`); ✓ confirms (`assignFace`, `create_marker` vs `assign_person` per the
  candidate's `marker_uid`) **optimistically in place** (the card flips, the grid doesn't reload), ✗
  **permanently rejects** via `rejectFace` (`services/feedback`) and removes the card; **keyboard** (arrows/
  `jkhl` move, `y`/`Enter` confirm, `n` reject, focus jumps to the next actionable card — registered
  in the `?` help via `shortcuts.groups.faceSearch`), „Potvrdit vše (n)" steps through the active tab's actionable cards
  sequentially with a live `current/total`, cancelably, **a partial failure doesn't roll back** and reports
  what failed — the review state is held by `useCandidateReview`; config (person/threshold/limit/tab) in the URL,
  states empty/no-faces/no-embeddings/zero-matches/loading,
  `RecognitionPage` = `/recognition` (editor/admin, a link in „Nástrojích") a **recognition sweep**
  „projdi všechny a najdi shody mezi neoznačenými obličeji": the config panel (a **confidence** slider in
  percent 50–95 %, step 1, **default 75 %** — tight, this page is for easy wins; a per-person limit;
  a Prohledat button) calls the **stream** `streamSweep()` (`services/recognition`, NDJSON via
  `fetch`+`ReadableStream`); during the scan a **live bar** `current/total` + the name being searched right now
  and **cancellation** (`cancel`→`AbortController`), the cards appear **person by person** as they arrive, not
  only at the end; one `PersonSweepCard` per person = a header (name + a to-do count + **„Potvrdit vše
  (n)"**) above the **same** bbox grid as `/faces` (**reuse `CandidateCard`**, no fork); ✓ confirms
  (`assignFace`), ✗ **permanently rejects** (`rejectFace`); **when the last candidate of a person is handled, the whole
  card disappears** (the list shrinking = the reward) — the state is driven by `useSweepReview` (`people` filters to those with
  actionable cards via `hasActionable`); the **keyboard** is the same as `/faces` (arrows/`jkhl` move over
  a flat `focusSequence` across people, `y`/`Enter` confirm, `n` reject — reuse
  `useKeyboardShortcuts` + `shortcuts.groups.faceSearch`); global stats (to handle / already
  assigned / people with matches) from `summary`, a `capped` notice, a **clean empty state** after a scan with no
  matches („všechny obličeje jsou přiřazené"); config (confidence/limit) in the URL; **it never auto-confirms**,
  `ExpandPage` = `/expand` (editor/admin, a top-level link **Rozšířit** by albums/labels) „rozšiř album
  nebo štítek o vizuálně podobné fotky": the config panel `ExpandSearchForm` (an **Album|Štítek** toggle
  (`ToggleButtonGroup`), collection selection via `AddAutocomplete` — options from `lib/expandSearch`
  `expandSources` **sorted by photo count descending, empty collections omitted**, the count in `hint` —,
  a threshold in **percent** 20–80 % step 5 **default 70 %** with bookends „Více výsledků"↔„Lepší shody"
  (range/conversion shared with `lib/faceThreshold`, `expandThresholdDistance` trims float noise for the URL),
  limit 1–200 default 50 (`clampExpandLimit`), a Hledat button — the search is **explicit**, not
  live-on-drag); calls `searchSimilar()` (`services/expand`); results = `ExpandResults`: a summary
  row (source photos / with embedding / min. matches / found) + a **vote-rule explanation**
  („Fotka musí odpovídat alespoň {{n}} zdrojovým fotkám" + „Řazeno podle počtu shod, pak podle
  podobnosti", for `source_capped` also a sample) above the **standard `PhotoGrid`** (no grid fork);
  the tile carries via `tileExtras` a **% similarity** and, at `match_count > 1`, a badge of the **match count**,
  a click opens the photo detail as in the library; **selection = the library model** (`useBulkEdit` +
  `SelectionStart`/`SelectionBar`/„Vybrat vše"/Shift+click range/Esc), `BulkEditControl`
  with **`prefill` = the expanded collection**, so Apply adds right away; on success via
  `BulkEditOutcome` **the added photos leave the grid in place** (without a refetch and scroll jump,
  errored ones stay; a different bulk operation doesn't change the grid) and the summary counts update; ✗ on a tile
  (only **labels** — albums have no rejection model, so it isn't offered) **permanently rejects** via
  `rejectLabel` (`services/feedback`) optimistically with rollback + an alert on failure; the **keyboard**
  like the library (`useGridKeyboardNavigation`: arrows/`hjkl`, Enter opens, `x` selects, Esc clears the
  selection); config (type/collection/threshold/limit) in the URL (Back/refresh restores the search); states
  idle/loading/error/**no-embeddings** (its own message — embeddings are computed once the box is online;
  distinct from zero-matches)/empty-collection/zero-matches (advises lowering the threshold)/all-handled,
  `MapPage` = `/map` a map view: geotagged photos as clustered markers over mapy.com
  tiles (Leaflet), a base-layer toggle + filters (date/archive/private) in `MapFilterBar`,
  state (mapset/viewport/filters) in the URL — panning/zoom writes the viewport without a refetch, a filter change
  fetches the GeoJSON; a click on a marker → the photo detail; loading/empty/error states; a **tile failure**
  (`onTileError` from `LeafletMap`) is diagnosed by `probeTileFailure` and explained with a **dismissible
  warning** (`map.tiles.*`, typically „mapový klíč byl odmítnut") instead of an unexplained grey grid —
  the map stays usable, markers/clusters keep drawing over the empty base; the probe is
  **debounced** (a whole batch of `tileerror` = one query) and switching the mapset resets the warning;
  photos with an **estimated location** (`location_estimated` on the feature) are on the map **by default** — that's what
  the estimate is for — but drawn with a **different pin shape** (`estimatedMarkerIcon` in `LeafletMap`: a hollow
  dashed ring, **not** just a different color — that wouldn't survive a color-blind view or a black-and-white print) plus
  a `title` from the `estimatedTitle` prop, which says the same in words to a screen reader; a pin that looks the same
  as a measured one would let the map claim a precision it doesn't have,
  `PlacesPage` = `/places` browsing the library by locality: a single `fetchPlaces()` fetch pulls
  the countries→cities hierarchy with counts; a **drill in the URL** (`?country=&city=` via `useUrlState` over
  `PlacesView` = `LibraryView`+`country`/`city`, so Back walks the levels) — level 1 a list of countries
  (`ListGroup`), level 2 the cities of the selected country (from nested data, no refetch), level 3 a photo grid
  scoped to `{country,city}` via `useScopedPhotos` (enabled only after a city is picked) + the shared
  `FilterBar` + a Místa/country/city breadcrumb; loading/empty/error states, for editors **selection mode**
  over the grid → `BulkEditControl` (refetch on success, an edit can move a photo out of a place); walking
  the drill **leaves selection mode**, each place is its own list,
  `SlideshowPage` = `/slideshow` a fullscreen slideshow (outside `Layout`, no navbar): reads the scope
  (`?album=`/`?label=`/`?mode=` for search/none) + filters/sort from the URL (the same state as the grid),
  pages via `usePaginatedPhotos` (large sets aren't loaded all at once) — the fetcher is `fetchPhotos`,
  or **`searchPhotos` when the URL carries `mode`** (otherwise `q` would only substring-filter and a
  different set would play), driven by `useSlideshow` +
  `useSlideshowSettings`, `total` from the server is passed to `Slideshow` (the countdown counts the whole show, not just
  the loaded pages), renders loading/empty/error states or `Slideshow`; **its own frame
  preloading**: `preloadWindow(index,length)` → URLs at `SLIDESHOW_PREVIEW_SIZE` → `useImagePreloader`
  (`prime` in an effect), whose `statusOf` goes back into `useSlideshow` as `readiness`, so
  auto-advance waits until the next frame is decoded; exit → `navigate(-1)`
  (fallback to the source view — album/label/`searchHref`/library), so Back works,
  `TrashPage` = `/trash` (editor+ sees the page) the trash: archived photos (a `useScopedPhotos`-style listing via
  `usePaginatedPhotos` scoped `archived=only`) in a `TrashCard` grid with `FilterBar`, **restore**
  (`unarchivePhoto`) is an editor action; **permanent deletion** (`purgePhoto`) individually and in bulk (`useSelection`
  `SelectionBar`), **Vyprázdnit koš** (`emptyTrash`) and **Smazat starší než…** are **admin+ only**
  (the backend guard `RequireAdmin`), so an editor sees only Obnovit on the card and in the bar — the purge controls
  render behind `isAdmin` (the `TrashCard` prop `canPurge`); each permanent action goes through a confirmation `Modal`;
  **Smazat starší než…** is a numeric field in days (default 180, ad-hoc, an integer ≥ 0, saved nowhere)
  + a button → a confirmation modal (`trash.confirm.older` with the specific number of days) → `purgeTrashOlderThan(days)`
  (`POST /trash/purge-older?days=N&confirm=true`), on success `useToast` success with the deleted count
  (`trash.olderThan.success`) + a list reload, on error an error toast (503 → `trash.unavailable`);
  `fetchTrashInfo` fetches the retention for the countdown on the cards,
  `DuplicatesPage` = `/duplicates` (editor/admin) reviewing and **resolving** duplicates: a paginated list of
  groups (`fetchDuplicates`, „načíst další" via `next_offset`) in `DuplicateGroupCard`; per group
  the user picks a keeper and **„Ponechat nejlepší a sloučit"** → `mergeDuplicates(dry_run:true)` computes
  a preview shown in `MergeConfirmModal` („+3 alba, +2 štítky, +1 osoba · 2 kopie budou
  archivovány"); after confirmation `mergeDuplicates()` merges (the keeper inherits albums/labels/people + fills gaps,
  copies to the trash — reversible) → the group disappears + a success alert (`duplicates.merged`), or **rejects** the group
  („není duplikát", hides it locally only); errors via `duplicates.actionError`/503 „nedostupné", loading
  via `GridSkeleton`, an error with retry; each card offers **„Porovnat vedle sebe"** → `DupComparePage`,
  because a 224px tile is enough to recognize a group, not to decide within it,
  `DupComparePage` = `/duplicates/compare?pair=<levá>|<pravá>` (editor/admin, **fullscreen outside
  `Layout`** like `/review` — two photos with a navbar around them are two too-small photos) the decision „kterou
  z těch dvou": from `fetchDuplicates` (one page of groups) it builds a `buildPairQueue` **queue of pairs** —
  a multi-member group is compared **pair by pair against the recommended keeper** (`[K,A,B]` → `(K,A)`,
  `(K,B)`, never `(A,B)`), the page says so in `duplicates.compare.groupNote` („Dvojice 1 z 2 v této
  skupině"), no member is hidden; `useComparePair` loads for the current pair `fetchPhoto` ×2 +
  `fetchFaces` ×2 (people aren't on the photo but on the faces endpoint — and "which copy carries your curatorial
  work" is exactly the question this page exists for); `CompareStage` shows both photos side by side
  (below `md` stacked) with **one shared zoom** (`useSyncZoom` + `lib/compareZoom`): one
  `ZoomView`, both `<img>` render it, so they can't diverge — the wheel zooms toward the cursor, dragging
  pans, a double-click toggles fit ↔ 3×, `?pair=` holds the position across a reload; `DiffTable` (`buildDiffRows`)
  compares dimensions+Mpx, size, format, date, camera, lens, name, place, albums, labels, people
  and **distinguishes only the rows that differ** (a border + bold + `visually-hidden` „liší se" — never just
  color), the toggle `duplicates.compare.diff.onlyDifferences` hides the matching ones; three actions —
  **Nechat levou/pravou** → `mergeDuplicates(dry_run:true)` → `MergeConfirmModal` with a `note`
  (`duplicates.compare.archiveNote`: it archives, doesn't delete) → `mergeDuplicates()` **over that
  pair only** (`member_uids:[keeper,loser]`, not over the whole group — the third member wasn't on screen),
  **Nechat obě** → `dismissDuplicate()` (persistent, `POST /feedback/duplicate-dismissals`);
  after the decision it **goes to the next pair**, not back to the list (pairs of the archived photo drop out
  via `dropPairsTouching`), at the end `EmptyState` `duplicates.compare.done`; keys `←`/`→`/`b`/`Esc`
  (in `SHORTCUT_GROUPS` as `shortcuts.groups.compare`), `KeyboardShortcutsHelp` mounts itself,
  `OutliersPage` = `/outliers` (editor/admin, a **Možné chyby** link in „Nástrojích") „které obličeje
  téhle osoby nejspíš nejsou ona": **the counterpart to the panel on the person's page, which stays** — the panel is
  right when you're looking at the person just now, this page when you want to hunt deliberately (and the panel links to it
  via `/outliers?subject={uid}`, so it arrives with a preselected person); `OutlierControls`
  (a person picker via `AddAutocomplete` with the face count in the hint + a **percentage** threshold slider
  0–100 % step 5 **default 0 = show all**, bookends „Zobrazit vše"↔„Pouze extrémní"; **no
  Hledat button** — the query is a cheap indexed read, so picking a person simply shows) → `fetchOutliers`
  with `{threshold: outlierThresholdDistance(percent), limit: OUTLIER_LIMIT}`; the slider is **live**
  (you watch the list narrow), but the query is **debounced** (`THRESHOLD_DEBOUNCE_MS = 250`) + runs via
  `AbortController`, otherwise one drag would fire a query on every step; config (person/threshold) in the URL,
  only the **committed** value is written to history (a drag doesn't end there); `OutlierStats` (total scored
  / average distance / shown + a one-line sort explanation, a **`no_embedding`
  message** (a face recognized while the box was offline can't be checked and is **not** in the list — say it
  aloud, otherwise an empty list reads as "clean"), a capped message at `OUTLIER_LIMIT`,
  a `meaningful:false` message); a grid of **large** `OutlierCard` (`minmax(20rem, 1fr)`): the **context
  crop** = the bbox enlarged by 30 % on each side via `padBbox` + `cropImageStyle`, inside it
  the face frame via `boxWithinCrop` (all `lib/faceGeometry`, `aspect-ratio` carries the geometry →
  no pixel measurement), a distance badge in **%**, the question „Je to chyba?" and two **opposite**
  answers to it: **✓ „Ano, odebrat"** → `assignFace` `unassign_person`, **✗ „Ne, je to {{name}}"** →
  `confirmFace` (`services/feedback`) — **mind the polarity, it is not `rejectFace`**; both flip
  the card **in place** (the card doesn't disappear → the grid doesn't reshuffle under the cursor); **selection** via
  `useSelection` (Shift+click a range, **Ctrl/Cmd+A** bound separately — the shared hook ignores modifiers
  — and only when the grid owns the page, so it doesn't steal the browser's select-all in a field) +
  `SelectionBar` with a **bulk removal** that goes sequentially and **acknowledges partial failure**
  (progress + an error count, the done ones stay done); the **keyboard** (`shortcuts.groups.outliers`):
  arrows/`hjkl` move, `y`/Enter remove, `n` confirm, `x` select, Esc clears the selection→focus —
  and **focus moves after a verdict to the next undecided card** (`nextActionableIndex`; the focus reset therefore
  hangs on the **answer**, not on the working list that changes with every verdict —
  otherwise the move would be discarded after every decision); states idle („vyber osobu")/loading/error/
  empty („nic podezřelého, sniž práh"); tests `OutliersPage.test.tsx` + `lib/outlierReview.test.ts`,
  `ReviewPage` = `/review` (editor/admin, a top-level link **Třídění** right next to Nahrát) a **sorting
  game**: one question („Je na fotce **Tomáš Kozák**?" / „Sedí k fotce štítek **Ostatky**?")
  across **the whole screen** — the page is **outside `Layout`** (no navbar, like `/slideshow`), because
  nothing but the photo should compete for attention; the order is **the question above the photo** (header/progress →
  question + hint + confidence → photo → actions) and the whole thing **always fits in the viewport**: it doesn't scroll
  vertically or horizontally, on a short display (a phone in landscape) the **photo** shrinks — text and buttons
  win, you never have to scroll to Ne/Nevím/Ano; the state is driven by `useReviewGame`, the photo is drawn by `ReviewPhoto`
  (`REVIEW_PREVIEW_SIZE = fit_1280`, i.e. **the whole shot**, not a square tile — the bbox is relative
  to the full frame; the face frame via `padBbox`+`faceBoxStyle` from `lib/faceGeometry` with **~30 %
  padding**, because you can't recognize a face from a tight crop, + a gentle dimming of the surroundings), the question
  `QuestionText` (`Trans` with `<strong>` around the name/label — an i18n **template**, not string concatenation)
  and the confidence `ConfidenceHint` (a muted % + a bar: context, not the answer); three actions **Ano · Ne ·
  Nevím** are real buttons (large, at the bottom, thumb-reachable on touch), **but the keyboard is the
  primary interface**: `→`/`y` yes, `←`/`n` no, **spacebar**/`↓` don't know, `z` and **Ctrl/Cmd+Z** undo
  (the chord binds outside `useKeyboardShortcuts`, which deliberately ignores modifiers), `Esc` end (leaves
  `Esc` for an open help modal) — all registered in the `?` overlay via
  `shortcuts.groups.review`; the answers are **optimistic** (the UI moves on, the request finishes in the background) and
  the next card is **always already in memory** (`useReviewGame` refills in the background, `useImagePreloader`
  decodes `PRELOAD_AHEAD = 4` photos ahead), so between cards **a spinner never flashes**;
  an unsaved answer isn't lost — it sits in an alert with **Uložit znovu**/**Zahodit**, undo has its own
  alert with retry; the session shows a **counter of answered + remaining** and a thin progress bar
  (no score, streaks or confetti — the reward is a tidy library); states: an **empty library**
  (`no_people_no_labels` → „nejdřív pojmenuj lidi / založ štítky" with links to `/people` and
  `/labels`) is **distinct from an empty queue** (`no_candidates` → „vše posouzeno" + Zkusit znovu),
  plus loading the first batch and **offline/error** with retry; tests `ReviewPage.test.tsx` (a padded
  bbox, the name/label in the question, →/←/spacebar send the right verdict and advance, **no fetch
  between cards within a batch**, undo via the right inverse endpoint, a failed answer doesn't lose
  the place, the two empty states differently),
  `LeaderboardPage` = `/leaderboard` (**any logged-in user** — reading aggregates is not a write, so the
  top-level link **Žebříček** is seen by a viewer too, right next to **Třídění**; **inside `Layout`**, not
  fullscreen) a **competitive sorting leaderboard** over `GET /review/leaderboard` (`fetchLeaderboard(window)`):
  who decided the most in the review game. A sorted table (`react-bootstrap` `Table`) **Pořadí · Hráč · Ano ·
  Ne · Celkem**, the top 3 carry a **medal** (`Icon` `trophy-fill`/`award-fill` + a color class
  `kk-medal--{gold,silver,bronze}` in `app.css`, decorative — the rank number is beside it via
  `visually-hidden`, so a screen reader hears the placing), **the logged-in user's row is highlighted**
  (a match on `useAuth().user.uid`; `kk-leaderboard-row--me` = a `--kk-accent-subtle` tint + a „Vy" badge,
  not just color). **The window toggle** Za celou dobu / Posledních 7 dní / Dnes holds the choice in a **URL query
  param** `window` (`useSearchParams`, replace — „Back always works"), changing the window refetches.
  `ListSkeleton` while loading, `ErrorState` with retry (`useReloadKey`), an **empty state** (`EmptyState`
  „Zatím žádná rozhodnutí" + a CTA to `/review`); if the logged-in user is off the leaderboard, a quiet hint „Zatím
  nejste na žebříčku" with a link to `/review`. The board is small (a row per user), so a **plain
  table without virtualization**. **For an admin (`isAdmin`) a player's name is a link** to their decisions
  overview (`/audit/reviews?user=…`, aria-label `leaderboard.viewDecisions`) → `ReviewDecisionsPage`;
  a non-admin sees only the name without a click-through. i18n `leaderboard.*` (cs/en). Tests: `LeaderboardPage.test.tsx`
  (sorted standings + the Ano/Ne split, highlighting of one's own row, switching the window changes the query param and
  refetches, the empty state with a link to `/review`, top-3 medals, a not-on-board hint, **admin click-through /
  non-admin plain name**),
  `ReviewDecisionsPage` = `/audit/reviews` (admin **or** maintainer, `RequireRole role="admin"`)
  an **overview of one user's review decisions** (reachable by clicking through from the leaderboard): over `GET /audit`
  with `?via=review&user=…` (`fetchAuditLog`). At the top the user's name + their **Ano/Ne/Celkem** tally
  (looked up from `fetchLeaderboard('all')`), below it the **Ano/Ne filter** (`ButtonGroup`, held in the URL
  query `decision`, `viewToAuditParams` maps it to the backend), a table **Fotka · Rozhodnutí · Osoba
  nebo štítek · Kdy**: `thumbUrl(photo_uid,'tile_100')` via `FadeInImage` (fallback an empty well),
  an Ano/Ne `Badge` (`check-lg`/`x-lg`), the subject/label name translated via rosters
  (`fetchSubjects`/`fetchLabels`, best-effort). prev/next pagination over `offset`/`next_offset`
  (limit 60), state in the URL (`user`/`decision`/`offset` — „Back always works"). An empty state when the
  user has no decisions; without a selected user a hint back to the leaderboard; self-gated on `isAdmin`.
  i18n `reviewDecisions.*` (cs/en). Tests: `ReviewDecisionsPage.test.tsx` (the Ano/Ne split + thumbnails,
  the tally from the leaderboard, the filter changes the URL and refetches, the empty state, a non-admin alert),
  `NotFoundPage`),
  `components/savedsearch/` = `SaveSearchModal` (a modal for naming when saving a new view
  or renaming an existing saved search) + `SavedSearchesDropdown` (a dropdown in the header of
  `SearchPage` — **not in the navbar**; lazy fetch on open, items open the saved view via
  `savedSearchHref`, „Spravovat" → `/saved`, loading/empty/error states inside the menu);
  `components/search/` = `GlobalSearchSections` (a compact cross-entity section above the photo grid of the
  search page: via `useGlobalSearch(query)` it pulls the grouped `GET /search/global` and renders
  chips of matching **albums/people/labels** linking to the entity; independent of the photo fulltext/semantic
  search below it, renders nothing until at least one non-photo match arrives — an empty query /
  an in-progress search / a photos-only match adds no chrome) +
  `SearchCommand` (**a global command palette** in the navbar: a field-as-trigger (`kukatko-search-trigger`
  with a key hint) opens via `react-bootstrap` `Modal` a top-anchored console — a live input (a combobox
  with `aria-activedescendant`), grouped **keyboard-operable** results from `useGlobalSearch`
  (rows Fotky/Lidé/Alba/Štítky + always a leading action „Hledat vše" → `/search?q=`) and a footer legend
  of keys. Arrows ↑/↓ move (wrapping), Enter opens the active row, Esc closes, a click opens. It opens
  with `/` (suppressed while typing / with a form-modal open via `isTypingElement`+`isFormModalOpen`) or
  Cmd/Ctrl-K (a chord, works while typing too); **the open/closed state and the query live only in the component, not in the URL**,
  so Back stays untouched. The backend `/search/global` doesn't return `Místa` groups, so the palette
  doesn't show them. Keys `searchCommand.*`, `globalSearch.groups.*`; in the shortcut help the group
  `shortcuts.groups.global`);
  `components/trash/` = `TrashCard` (an archived-photo tile: a preview + a countdown to auto-purge via
  `trashCountdown` + restore/delete actions + selection in selection mode);
  `components/duplicates/` = `DuplicateGroupCard` (a group card: members side by side with a preview/
  dimensions/size/`taken_at`/distances, a radio selection of the keeper (the suggested one by default), a `reason` badge,
  actions **Ponechat nejlepší a sloučit** (`onResolve` → preview) / **Není duplikát**, a busy state) +
  `MergeConfirmModal` (a confirmation dialog: a summary of what moves to the keeper + how many copies are archived,
  an optional `note` below it — `DupComparePage` uses it to say that the copy is archived and not deleted, Potvrdit/Zrušit,
  a busy spinner) + `CompareStage` (two photos side by side, below `md` stacked; both render **the same**
  `SyncZoom.view`, so the zoom is synchronous by construction; the cursor `zoom-in`/`grab`/`grabbing` says
  what the gesture will do; the viewport clips, `object-fit: contain` never crops) + `DiffTable` (a diff
  table: a row that differs is marked with a **border + bold + `visually-hidden` „liší se"** —
  never just color; `onlyDifferences` hides the matching ones, an empty value is „—", all matching → a message
  instead of the table) + `compare.css`;
  `components/expand/` = `ExpandSearchForm` (the `/expand` config panel: an Album|Štítek toggle,
  an `AddAutocomplete` collection picker with the photo count in the hint, a percentage threshold slider with bookends,
  limit, a Hledat submit button — purely controlled, the state is held by the page) + `ExpandResults`
  (a summary row with a vote-rule explanation above `PhotoGrid`; per-tile overlays via `tileExtras`:
  a % similarity badge (`pe-none`), a match-count badge at `match_count > 1`, a ✗ button only when
  the caller supplies `onReject`; after the user empties the grid, a „vše zpracováno" message);
  `components/review/` = `ReviewPhoto` (the sorting game's stage: **the whole frame** of the photo at
  `REVIEW_PREVIEW_SIZE` (`fit_1280`, **exported** — the page preloads exactly this URL) as
  large as **the space left under the question** allows; the frame is **width-driven** via `aspectRatio` +
  `maxWidth: min(100%, calc(100cqh * ratio))`, where `100cqh` is the **actual** height of the stage (it is a
  `container-type: size` container) — so the frame is capped by the real remainder of the column, **not by a guess**,
  it therefore holds the exact ratio and the normalized bbox fits **without pixel measurement**; `displayAspect` computes
  the ratio in **display** (EXIF-oriented) space — orientation 5–8 swap width/height —,
  fallback 3:2 so the stage never collapses; the face frame = `padBbox` (~30 %) → `faceBoxStyle`,
  `pointer-events: none` + `aria-hidden`, the surroundings a gentle dim; a broken preview degrades to an icon, a new
  photo resets the flag) + `review.css` (a fullscreen **flex column** `review-game`: top bar /
  progress / **question** / stage / actions — text **above** the photo; the stage is `flex: 1 1 0` +
  `container-type: size` + `overflow: hidden`, so its height **is** the remainder after the chrome (basis 0 →
  the photo inside can't push anything out) and an overflow of the photo onto the text is **structurally impossible**, however
  the chrome grows — an alert, a wrapped long name, `pointer: coarse` buttons; `@media
  (max-height: 500px)` tightens the paddings on a **phone in landscape** (wide → no width query catches it,
  and yet it has the least room) and `clamp(…, min(3.5vw, 5dvh), …)` on the question holds the same for
  the font; `review-photo__box` frame, a progress bar, `kbd` badges, a touch variant of the actions);
  `components/slideshow/` = `Slideshow` (prezentační fullscreen stage: aktuální fotka v preview
  velikosti `SLIDESHOW_PREVIEW_SIZE` (`fit_1920`, **exportováno** — stránka musí přednačítat přesně
  tuhle URL), ovládání předchozí/play-pause/další/fullscreen/nastavení/zavřít + titulek +
  **postup** (`slideshow.progress` → „snímek 7 ze 40"; počítá se proti `total` ze serveru, ne proti
  načteným stránkám — zbývající čas už tady není); klávesy ←/→ / mezerník / Esc / F
  a dotykový swipe; Fullscreen API feature-detected;
  panel nastavení = výběr efektu + rychlosti a **vedle rychlosti odhad zbývajícího času**
  (`slideshow.remaining` → „zbývá 2 min 45 s"; `slideshowRemainingMs(index, total, intervalMs)` — sleduje
  index i zvolenou rychlost, takže odpočítává a hned reaguje na změnu rychlosti, drží se `total` ze
  serveru (bez blikání při stránkování) a při pauze zamrzne; mizí s koncem promítání);
  efekt **`kenburns`** navíc zapisuje na `<img>` inline
  `--kb-*` custom properties z `lib/kenBurns` (endpointy transformu + `--kb-duration` = interval) —
  aktivuje se **jen pro obrázky**, video snímek a uživatel s `prefers-reduced-motion`
  (`usePrefersReducedMotion`) dostanou statický snímek bez animace) + `slideshow.css` (keyframes
  `slideshow-fade`/`slideshow-slide`/`slideshow-kenburns` (`object-fit: cover`, `var()` se dosadí
  před interpolací, takže se oba transformy interpolují jako shodný `translate() scale()` seznam),
  `@media (prefers-reduced-motion: reduce)` jako druhá pojistka, fullscreen layout)
  + `SlideshowStart` (**sdílené** tlačítko Promítání pro knihovnu / album / štítek / hledání:
  jen `slideshowHref(scope,view)`. **Žádný odhad délky před spuštěním** — přesunul se do přehrávače
  vedle rychlosti, kde sleduje průběh; `count` prop grid pořád posílá (má ho z `total`), ale
  komponenta ho nerenderuje);
  `components/map/` = `LeafletMap` (imperativní Leaflet most: dlaždicová vrstva na **backend
  proxy** `/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}` (klíč server-side, `{r}`→`@2x` na retině),
  **povinné mapy.com prvky** — attribution „© Seznam.cz a.s. a další" → `/copyright` a klikatelné
  **logo** vlevo dole → `mapy.com`; `leaflet.markercluster` shluky (klik přibližuje), markery
  z GeoJSON, popup s náhledem → detail fotky; jednorázový setup, výměna URL dlaždic při změně
  mapsetu, přestavba markerů při změně fotek, fit-bounds na markery; volitelný **`onTileError`**
  prop dostane URL dlaždice, kterou se nepodařilo načíst (Leaflet `tileerror`), aby rodič mohl
  zjistit **proč** — fire per dlaždici, rodič debouncuje), `MapFilterBar` (přepínač
  podkladu basic/outdoor/aerial + datum od/do, archiv, soukromé, počet, zrušit filtry);
  `components/people/` = `SubjectTile`/`SubjectPhotoTile`/`SubjectEditModal`,
  `FaceCrop` (**preferovaný** výřez obličeje: `<img>` s `fit_*` zdrojem z `lib/faceSource.ts`
  `faceSourceSize` (celý rám — `tile_*` je centrovaný čtverec, na kterém by výřez minul obličej;
  velikost se **škáluje podle toho, jak malý obličej je**: pevná by dala 13px šmouhu místo
  člověka u obličeje přes 2 % rámu, žebřík 720/1280/1920 se zastavuje u 1920, protože dál už ty
  pixely v originále nejsou) v `overflow:hidden` kontejneru,
  `cropImageStyle` v %, `aspect-ratio` ze skutečných pixelových proporcí výřezu → **nic se
  nedeformuje**; `size` = pevná šířka v px, jinak vyplní rodiče (`w-100 h-100`); `label=""` =
  dekorativní, když jméno stojí vedle. Potřebuje rozměry rámu),
  `FaceThumb` (**legacy** čtvercový výřez přes `faceCropStyle` — deformuje a čte `tile_*`; zůstává
  jen pro cluster preview, jejichž payload rám nenese),
  `FaceOverlay`+`FacesPanel`+`FaceAssignPanel` (`FaceOverlay` = **čistě prezentační** průhledná vrstva
  klikatelných boxů z normalized bbox přes `faceBoxStyle`, **žádný vlastní obrázek ani fetch** —
  mountuje se jako poslední dítě `position-relative` obalu těsně kolem `<img>`; vrstva je
  click-through, pointer events chytají jen boxy (a při `readOnly` ani ty; číslo a jmenovka boxu mají
  `pointer-events:none`, jinak by ukradly klik a rozbily swipe). Data + stavový automat pojmenování
  drží hook `useFaces`. **`FacesPanel`** = panel v zásuvce prohlížeče, jediné místo, kde se přiřazuje:
  **textové řádky** `Obličej #N` + barevný chip stavu (žádné výřezy — jeden obrázek na stránku),
  klik vybere/odvybere, hover se zrcadlí s boxem; pod vybraným řádkem se rozbalí `FaceAssignPanel`
  (`key={face_index}` → reset stavu při změně výběru). **`FaceAssignPanel`** = top-3 návrhy
  (`{jméno} · {confidence}%`, one-tap) + typeahead nad `useSubjects` (`AddAutocomplete` s `autoFocus`
  a `hint` = počet fotek osoby); u přiřazeného obličeje **Přeřadit** (návrhy, které backend dodává
  i pro přiřazené — vlastní osoba je z nich vyloučená) a **Odebrat**; Esc vyskočí nejdřív z přeřazení,
  pak z výběru), `ClusterCard`, `Candidates` (per-subject verze `/faces` vsazená do stránky osoby:
  tlačítko **Najít návrhy** → `searchCandidates` s defaultním prahem `THRESHOLD_DEFAULT_PERCENT` a
  limitem 60, reuse `useCandidateReview`+`CandidateCard` beze forku; ✓ potvrdí přes `assignFace`
  a `onAssigned` reloadne galerii, ✗ odmítne přes `rejectFace`, obojí optimisticky a potvrzená/
  odmítnutá karta z listu zmizí; `no_faces`/`no_embeddings`/prázdno mají vysvětlení; odkaz
  **Otevřít celý nástroj** na `/faces?subject={uid}`), `Outliers` (žebříček podezřelých obličejů
  s one-tap unassign na stránce osoby + odkaz **Projít všechny** na `/outliers?subject={uid}`, kde
  je plná sweep verze),
  `OutlierCard`/`OutlierControls`/`OutlierStats` (stavební bloky `/outliers`: karta s **kontextovým
  výřezem** (30 % kolem bboxu, `padBbox`+`cropImageStyle`+`boxWithinCrop`), otázkou „Je to chyba?"
  a dvěma opačnými verdikty (✓ odebrat / ✗ potvrdit), výběrovým checkboxem a focus ringem; config
  strip s pickerem osoby a procentním prahem; statistiky včetně **`no_embedding`** hlášky);
  `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  vystavuje `user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin` (admin+)/`isMaintainer`/`canImport`; `ProtectedRoute` =
  `RequireAuth` + `RequireRole` + `RequireImport` route guardy),
  `capabilities/` (`CapabilitiesContext`/`useCapabilities` + `CapabilitiesProvider` = instanční
  feature-flagy `{semantic_search}` z `GET /api/v1/capabilities`; provider je uvnitř `AuthProvider`,
  fetchuje při mountu + po 60 s + na `visibilitychange` (stejný vzor jako `useJobStats`), selhaný
  fetch drží poslední stav; **na rozdíl od `useAuth` hook nehází** — kontext má bezpečný default
  `{semantic_search:false}`, takže komponenta mimo provider jen skryje volitelnou nabídku místo pádu.
  Čte ho `FilterBar` pro odkaz na sémantické hledání), `hooks/` (`usePaginatedPhotos` = sdílený
  paginovaný infinite-scroll loader nad libovolným `PageFetcher`: akumuluje stránky,
  `loadMore`/`retry`, reset+refetch **se skeletonem** při změně dotazu/`key`/`enabled`, ruší
  in-flight requesty a ignoruje stale odpovědi, vystavuje i `mode`/`degraded`; `enabled:false`
  → `idle` stav bez requestu. **`reloadKey` (oddělené od `key`) je _pozadí_ refetch první stránky
  při nezměněném dotazu: aktuální fotky zůstanou připnuté, `status` zůstane `ready` (žádný
  skeleton, žádné znovunačtení náhledů), takže hromadná úprava (favorite/archiv) se projeví
  v místě bez bliknutí mřížky; `reloading` je po dobu refreshe true, neúspěšný refresh je tichý
  (seznam zůstane).** `usePhotoLibrary(params,{reloadKey?})` = tenká obálka nad ním nad
  `fetchPhotos` (`reloadKey` přehraje mřížku na pozadí po mutaci, stejně jako u `useScopedPhotos`);
  `usePhotoSearch(params,mode,{reloadKey?})` = obálka nad `searchPhotos` s injektovaným `mode`
  (jde do `key` → změna módu resetuje se skeletonem), vypnutá při prázdném `q` (idle), `reloadKey`
  přehraje hledání na pozadí po mutaci;
  `useUploadQueue` = fronta uploadu: `addFiles` (dedup jméno+velikost+mtime)/`removeItem`/
  `start`/`retry`/`retryFailed`/`clear`, konkurenční strop `MAX_CONCURRENT_UPLOADS` (3),
  per-file status+progress, souhrn počtů + `progress` (**celková** frakce dávky 0–1 vážená
  částečným progressem běžících souborů, terminální soubory = hotové → plynulý overall bar),
  `createdUids` (jen nové) pro odkaz do knihovny
  a `resolvedUids` (nové **i** duplicitní fotky) pro pouploadové přiřazení; auto-drainuje
  frontu efektem po `start`/retry, ruší běžící uploady při unmountu;
  `useUploadOrganize` = výběr alb/štítků pro celou dávku uploadu + jejich přiřazení: načte katalogy
  alb a štítků (`fetchAlbums`/`fetchLabels`), drží výběr (inline vytvoření jako `create:` marker
  jako v `BulkEditModal`, sdílené helpery `lib/pendingCreate`), `runAssign(uids)` nejdřív založí
  čekající alba/štítky a pak jedním `POST /photos/bulk` (`add_to_albums`+`add_labels`) přiřadí;
  stav `idle`/`assigning`/`done`/`error`, `retryAssign` re-poslání téže dávky, `resetAssign`;
  `useSubjectPhotos(uid,{reloadKey?})` = obálka nad `usePaginatedPhotos` nad
  `GET /subjects/{uid}/photos` (galerie osoby, `uid` jde do `key` → reset se skeletonem při změně
  osoby, `reloadKey` je pozadí refetch po mutaci); `useScopedPhotos` = obálka nad `usePaginatedPhotos`
  nad `GET /photos` scopnutým na album/štítek/**lokalitu** (`PhotoScope` `{album?,label?,country?,city?}`
  + filtry/sort z URL, options `{reloadKey?,enabled?}` — `reloadKey` pro pozadí refetch po mutaci, `enabled:false`
  → idle bez fetche, např. Places před výběrem města); `useMapPhotos` = jednorázový (nestránkovaný) loader
  GeoJSON feedu geotagovaných fotek nad `fetchMapPhotos` (`status` loading/ready/error, `retry`,
  ruší in-flight + ignoruje stale při změně filtrů);
  `useJobStats(enabled)` = poller stavu fronty jobů nad `fetchJobStats` (`GET /jobs/stats`) pro badge
  v patičce: fetchuje **jen když `enabled`** (admin), refetch po ~30 s, **pauzuje při skryté záložce**
  (`visibilitychange`/`document.hidden`) a při návratu hned refreshne; selhání spolkne a vrátí `null`
  (badge se skryje), na unmountu/`enabled→false` ruší timer i in-flight request — nic ho nepřežije;
  `useAnnouncement()` = poller instance-wide oznámení nad `fetchAnnouncement` (`GET /announcement`) pro
  `AnnouncementBanner`: fetch on-mount + refetch po ~60 s, **pauzuje při skryté záložce** a při návratu hned
  refreshne, selhání spolkne a vrátí `null` (banner se skryje), na unmountu ruší timer i in-flight (zrcadlí
  `useJobStats`);

  `useLibraryFacets(params)` = loader nabídek facetů knihovny → `LibraryFacets{years,albums,labels,subjects}`:
  roky přes `fetchPhotoYears` **refetchuje při změně filtrů** (rok drží méně fotek, jakmile přibude
  štítek), ale **`year` z requestu strhává** (backend ho stejně ignoruje — facet nesmí zúžit vlastní
  nabídku — a bez něj zůstane request identický, takže přepínání let nerefetchuje); alba, štítky a
  subjekty (osoby, přes `fetchSubjects`) jsou katalogové, načtou se **jednou**. Neúspěch nechá ten seznam **prázdný** místo chyby (facet,
  který nemá co nabídnout, je degradovaný bar, ne rozbitá stránka — chyby načtení hlásí mřížka);
  in-flight requesty ruší `AbortController` při změně `params`/unmountu, takže pomalá odpověď
  nepřepíše novější (`params` si volající memoizuje z URL stavu); `useTimeline(params)` = jednorázový loader
  měsíčního date-histogramu nad `fetchTimeline` (`buckets`/`total`/`status`, refetch při změně
  filtrů, ruší in-flight + ignoruje stale — podklad `TimelineScrubber`); `useGlobalSearch(query,
  debounceMs?)` = debouncovaný (default 250 ms) grouped global-search loader nad `globalSearch`
  (`status` idle/loading/ready/error + `result`, prázdný dotaz → idle bez requestu, ruší in-flight +
  ignoruje stale — podklad `GlobalSearchSections`); `usePlaceSearch(query,debounceMs?)` =
  debouncovaný (default 300 ms) loader našeptávače míst nad `searchPlaces` (`status`
  idle/loading/ready/**error**/**unavailable** + `places`, ruší in-flight + ignoruje stale —
  podklad `PlaceSearch`); zrcadlí `useGlobalSearch` s dvěma rozdíly, které plynou z toho, že
  lookup **stojí kredit**: dotaz kratší než 2 znaky je `idle` **bez requestu** (jedno písmeno není
  název místa, jen klávesa na cestě k němu) a statusy 424/502/503 dostanou vlastní stav
  `unavailable` (rozbitá je strana poskytovatele, opakovat nemá smysl) proti `error` (zbytek,
  vč. 429 — zkusit znovu dává smysl); `useGridJump({gridRef,
  loadedCount,hasMore,loadingMore,loadMore})` = vrátí `jumpTo(index)`, který skočí mřížkou na foto
  index přes `VirtuosoGridHandle.scrollToIndex` a **nejdřív donačte stránky**, když cíl leží za
  infinite-scroll kurzorem (nebo clampne na poslední načtené, když už další stránky nejsou) —
  podklad skoku časové osy na měsíc před načtenou částí; `useSelection` = multi-výběr fotek v mřížce
  (`active`/`selected`/`count`/`enable`/`disable`/`toggle`/`selectMany` (select-all-in-view)/`clear`);
  poslední `toggle` drží **kotvu** a `toggleRange(uid, orderedUids)` (Shift+klik) vybere souvislý
  rozsah mezi kotvou a `uid` — jen **přidává**, bez kotvy nebo s kotvou mimo pořadí degraduje na
  `toggle`, `clear`/`disable` kotvu shodí;
  `useBulkEdit({onEdited?, hoverSelect?})` = **znovupoužitelná hromadná úprava** libovolného foto-seznamu:
  `useSelection` + role gate (`canBulkEdit` = `canWrite`) + stav dialogu
  (`editing`/`open`/`close`/`finish`), k tomu `photoUids` (**přesně vybrané**, nikdy celý filtrovaný
  výsledek) a `gridSelection` rovnou do `PhotoGrid` (vč. `onToggleRange` → Shift+klik rozsah zdarma
  v každé mřížce). **`hoverSelect:true`** (**všechny foto-seznamy**: knihovna, detail alba/štítku,
  oblíbené, hledání, místa, galerie osoby): `gridSelection` je pro editora **vždy** definované
  s `hoverSelect` (žádný explicitní vstup do režimu — rohové zaškrtávátko na hoveru) a stránka
  ukazuje `SelectionBar` na `selection.count > 0`, ne na `selection.active`; bez něj (jen /expand)
  je `gridSelection` definované až po `enable()`. Viewer dostane vždy `undefined`.
  `finish(outcome?)` = zavřít dialog → `selection.clear()`
  → `onEdited(outcome?)` (refetch; `outcome` = `BulkEditOutcome` pro stránky, které umí seznam
  upravit na místě — `/expand`); režim výběru přežije, takže po úspěchu jde hned vybírat dál a žádné
  zastaralé UID v něm nezůstane. Neúspěšný apply výběr **nechá být**. Stránka wiruje jen
  `gridSelection` (a v explicitním režimu `SelectionStart`), zbytek obstará `BulkEditControl`;
  `useReloadKey()` = `[key, reload]`, string čítač do `reloadKey` foto-seznamu — jedno `reload()`
  přehraje seznam **na pozadí** (refetch první stránky bez blanknutí do skeletonu, fotky zůstanou
  připnuté); `reload` je stabilní, jde rovnou do `useBulkEdit({onEdited})`;
  `useKeyboardShortcuts(handlers,{enabled?})` = sdílené plumbing všech klávesových zkratek: jeden
  document-level `keydown` listener dispatchuje dle normalizovaného `shortcutToken(event.key)` na
  `handlers` (přes refy, bind jednou a vždy vidí aktuální closury), matched key `preventDefault`;
  **nikdy nevystřelí** při držení Ctrl/Meta/Alt, při psaní (`isTypingElement`) ani při otevřeném
  form-modalu (`isFormModalOpen`);
  `useAutoHideChrome({idleMs?,paused?})` → `{visible,wake}` = **mizející chrome** immersivního
  prohlížeče (`PhotoDetailPage`): ovládání startuje viditelné, po `idleMs` (default 2600 ms) bez
  aktivity se ztlumí a vrátí se při další aktivitě. Aktivitu hlídá **globálně** (pointer move/down,
  key, touch), viditelnost drží přes ref a do stavu commituje **jen na skutečnou změnu**, takže
  záplava `pointermove` nepřerenderovává každý frame; `paused` chrome **připne viditelné** a timer
  nespustí (když je zásuvka otevřená). Rozhoduje jen *jestli* se chrome ukáže — *jak* animuje řeší
  CSS přechod na duration tokenech (pod `prefers-reduced-motion` ~0);
  `useGridKeyboardNavigation({count,enabled,resetKey,getColumns,
  scrollToIndex,onOpen,onToggleSelect,onToggleFavorite,hasSelection,onClearSelection})` = navigace
  mřížky nad `useKeyboardShortcuts`: drží `focusedIndex` (zvýraznění), šipky + `j`/`k`/`h`/`l` posouvají
  (vlevo/vpravo o 1, nahoru/dolů o řádek dle živého počtu sloupců) a dorolují dlaždici do view, `Enter`
  otevře, `x` vybere (zapne selection mód), `f` přepne oblíbenou, `Escape` zruší nejdřív výběr, pak
  fokus; fokus se resetuje na `resetKey` (nová filtr/sort/scope);
  `useSwipeNavigation({onSwipe,enabled?,threshold?})` → `{onTouchStart,onTouchMove,onTouchEnd}` =
  horizontální **swipe na dotyku → prev/next** na obrázku detailu; čte jen start/konec doteku a
  **nikdy nedělá `preventDefault`**, takže mostly-vertikální tah propadne nativnímu scrollu (rozhoduje
  `lib/gestures` `swipeAction`: práh + dominantní vodorovná složka). Gesto se zahodí při druhém prstu
  (pinch) a když **začne na interaktivním prvku** (`button`/`a`/form) bez `data-swipe-surface` — takže
  ťuknutí na obličejový box/šipku nelistuje, jen samotný obrázek (jeho tlačítko ten atribut nese). Myš
  na desktopu sem nechodí, gesto je čistě aditivní pro dotyk;
  `useSyncZoom({resetKey})` → `{view,zoomed,dragging,handlers,zoomIn,zoomOut,reset}` = **jeden**
  zoom/pan stav pro **obě** fotky v `DupComparePage`: obě `<img>` renderují týž `ZoomView`, takže
  jsou synchronní **z konstrukce** — není co kopírovat mezi panely, není kde se rozejít. Kolečko
  zoomuje k kurzoru, tažení posouvá (jen když je přiblíženo), dvojklik přepíná fit ↔ 3×, změna
  `resetKey` (id dvojice) vrátí fit, takže další dvojice nezdědí přiblížení. **Není to
  `usePinchZoom`:** ten je touch-only a měří proti `window` (obrázek vyplňuje viewport), tady jde
  o myš ve dvou půlkách obrazovky, takže box se předává dovnitř; čistá matematika je v
  `lib/compareZoom`,
  `useComparePair(pair)` → `{data,loading,error}` = načte obě strany porovnání (`fetchPhoto` ×2 +
  `fetchFaces` ×2, paralelně, `AbortController`); selže-li kterákoli, selže celá dvojice — půlka
  diff tabulky by lhala mlčením,
  `usePinchZoom({onSwipe,resetKey,enabled?})` →
  `{scale,translateX,translateY,isZoomed,gesturing,handlers,reset}` = **pinch/dvojklik zoom** fullscreen
  lightboxu s **pan** při přiblížení a swipe listováním v klidu: dva prsty škálují (`pinchScale`, clamp
  `[1,4]`), **dvojklik** přepíná fit ↔ `DOUBLE_TAP_SCALE` (zoom k místu ťuknutí), tah přiblíženého
  obrázku panuje (clamp `clampPan`, aby nevyjel z obrazovky), tah v klidu rozhodne swipe (`swipeAction`);
  **zoom se resetuje při změně `resetKey`** (zobrazená fotka) a zavřením (lightbox se odmountuje). Povrch
  má `touch-action:none`, takže `preventDefault` není potřeba a prohlížeč gesto nepřebíjí;
  `useFaces(photoUid)` = načte obličeje fotky (`fetchFaces`) a drží stavový automat pojmenování
  (výběr boxu, optimistické přiřazení, refetch smiřující se serverem, `busy`/`actionError`);
  vytažen z `FaceOverlay`, aby detail mohl kreslit boxy nad svým jediným obrázkem a panel
  pojmenování renderovat jinde na stránce. **Po načtení vybere první nepojmenovaný obličej**
  a **po přiřazení posune výběr na další nepojmenovaný** (`firstUnnamed`/`nextUnnamed`, řadí dle
  **pořadí v poli**, ne `face_index`; `facesRef` proti stale closure) — skupinovou fotku tak projedeš
  bez sahání po myši. `unassign` výběr **nechá** (obličej se právě uvolnil a typicky ho hned
  přejmenováváš). Smiřovací refetch po mutaci auto-výběr **nespouští** (`reload(signal, autoSelect)`),
  jinak by pojmenování posledního obličeje odskočilo zpátky nahoru;
  `useSubjects()` = líný seznam všech subjektů pro typeahead (mountuje se až s `FacesPanel`,
  takže prohlížení fotky ho nikdy nezaplatí; chyba = prázdný seznam, pole pak jen zakládá nové);
  `useCandidateReview(subjectUid,candidates)` = stavový stroj review mřížky `/faces`: naseeduje
  pracovní seznam z čerstvého hledání a aplikuje ✓/✗ **optimisticky** (mřížka se nereloadne);
  `confirm` překlopí kartu na `done` a zavolá `assignFace` (chyba → `error` k retry, sousedů se
  nedotkne), `reject` kartu odebere + `rejectFace` (při chybě vrátí zpět), `confirmAll(tab)` projde
  akční karty jedné záložky sekvenčně s `confirmAllState` `{running,current,total,failed}`,
  zrušitelně (`cancelConfirmAll`), částečné selhání neroluje zpět a nahlásí přes `actionError`;
  `useSweepReview()` = orchestrátor `/recognition` sweepu (multi-osoba varianta review): streamuje
  přes `streamSweep`, sbírá jednu `PersonState` na osobu s matchi jak přicházejí (`progress`/`person`/
  `summary`), `confirm`/`reject`/`confirmAllForPerson` aplikuje **optimisticky** stejnými pravidly
  (`buildAssignRequest`/`buildRejection` z `candidateReview`), `people` vrací jen osoby s akčními
  kartami (osoba zmizí, když se vyřídí poslední); `cancel`→`AbortController`, jeden `confirmAll` běží
  naráz; nikdy neautoconfirmuje;
  `useOutlierReview(subjectUid,faces)` = stavový stroj mřížky `/outliers`: naseeduje pracovní seznam
  z čerstvého dotazu a aplikuje oba verdikty **optimisticky a na místě** — karta flipne, kde stojí,
  mřížka se nereloadne a scroll neuteče kurátorovi uprostřed dlouhého seznamu. Verdikty jsou
  **opačné a míří na opačné endpointy**: ✓ `unassign` odpojí osobu přes běžný assign automat,
  ✗ `confirm` zapíše **trvalé potvrzení** (`confirmFace`), které backend z dalších outlier dotazů
  vyloučí — seznam, co dokola nabízí tytéž plané poplachy, je přesně ten problém, co tahle stránka
  řeší. Selhaný zápis označí **vlastní** kartu `error` a sousedů se nedotkne. `unassignMany` jde
  výběrem **sekvenčně** a **přizná částečné selhání** (`bulkState{running,current,total,failed}`,
  cancelovatelné): už odebrané zůstanou odebrané, chyby se spočítají a řeknou, nerollbackují se
  ani nespolknou. Nové `faces` (jiná osoba/práh) resetují vše a opustí běžící run;
  `useReviewGame()` = engine hry na třídění (`/review`): lokální fronta otázek plněná **na pozadí**
  (`fetchReviewQueue`; refill jakmile klesne na `REFILL_AT = 3`, deduplikace proti **všem** už
  viděným id, takže hranice batche je neviditelná), **optimistické** odpovědi (`answer` posune UI
  hned a request doběhne vzadu; selhání spadne do `failed` k explicitnímu retry — nikdy neblokuje
  rytmus ani tiše neztratí verdikt) a **jednokrokové undo**. Fronta má **zdroj pravdy v refu**, ne
  ve stavu: dvě odpovědi se vejdou do jednoho renderu (šipky v rychlosti) a čtení hlavy ze stavu by
  tutéž kartu zodpovědělo dvakrát. `undo` jde přes **inverzní** zápisové cesty (`unassign_person`,
  DELETE feedback-rejection, detach štítku), protože `POST /review/answer` je **idempotentní per
  otázka** — a ze stejného důvodu se **znovu**-odpověď na vrácenou otázku posílá přímými cestami
  (`sendDirect`), jinak by no-opla jako `already_answered`; undo nejdřív **počká na in-flight**
  request, aby inverze nepředběhla odpověď, kterou vrací, a `create_marker`-ano dohledá vzniklý
  marker přes `fetchFaces`, takže případné pozdější re-ano je `assign_person` na **týž** marker,
  ne duplikát;
  `useFavorite(uid,initial)` = **optimistický** per-user favorite toggle nad `favoritePhoto`
  (`PUT`/`DELETE …/favorite`), rollback při chybě, ignoruje souběžný toggle, resync na změnu
  `uid`/server stavu; `useRating(uid,initialRating,initialFlag)` = **optimistické** per-user
  hodnocení (hvězdy) + pick/reject flag nad `ratePhoto` (`PUT …/rating` jen s měněným polem),
  `setRating`/`setFlag` s per-poli rollbackem při chybě, no-op na shodnou hodnotu, `pending` přes
  in-flight counter, resync na změnu `uid`/server stavu (mirror `useFavorite`);
  `useThumbSrc(uid,thumbUrl)` → `{src,failed,onError}` = **odolnost vůči expirované podepsané URL**:
  `thumb_url` v payloadu může být krátkodobě podepsaná adresa media Workeru (default 1 h), takže
  payload držený ve virtualizovaném seznamu nebo přečkaný přes delší nečinnost dá `<img>` adresu,
  kterou Worker odmítne. První `onError` proto **jednou** refetchne fotku (`fetchPhoto`) a zkusí to
  s čerstvě podepsanou URL; druhý pád, selhaný refetch, prázdná nebo **nezměněná** adresa (to dělá
  filesystem backend — jeho URL jsou routy a nestárnou, takže pád = fakt chybějící náhled) → `failed`
  a volající vykreslí placeholder. Nová `thumbUrl` prop (nová stránka výsledků) resetuje retry budget.
  Řeší se to takhle, **ne dlouhým TTL** — krátká životnost je celý smysl podpisu. Používá
  `PhotoTile` a `TrashCard`;
  `useSlideshow({length,hasMore,intervalMs,autoPlay?,onLoadMore?,readiness?,maxHoldMs?})` = řízení
  promítání: vlastní `index`+`playing`+`holding`, `next`/`prev`/`play`/`pause`/`toggle`/`goTo`,
  wrap-around, prefetch `PRELOAD_AHEAD` stránek dopředu
  přes `onLoadMore` (na konci s další stránkou počká místo zacyklení), prázdná sada = no-op, clamp
  indexu při zmenšení sady. **Auto-advance je hlídaný `readiness(index)`**: uplynulý interval
  nepřepne slide, ale spustí *hold* — přepne se v okamžiku, kdy je další snímek `ready` (dekódovaný),
  po `maxHoldMs` (default `MAX_HOLD_MS` = 10 s) přepne tak jako tak, a slide s `error` **přeskočí**
  (rozbitý snímek show neblokuje). Manuální nav a pauza hold zruší (manuál nikdy nečeká, resume
  začne čerstvý interval), interval se dá měnit **během holdu** bez restartu/zdvojení timeru
  (timer se během holdu nearmuje, deadline holdu nezávisí na `intervalMs` ani na `readiness`).
  Sada < 2 snímků ani nedrží, ani nepřepíná. `preloadWindow(index,length)` = indexy k přednačtení
  (`PRELOAD_AHEAD` dopředu, `PRELOAD_BEHIND` dozadu, aktuální první, offsety **wrapují** →
  na konci show jsou první snímky připravené na wrap-around, u malé sady se dedupuje);
  `useImagePreloader()` → `{statusOf(url),prime(urls)}` = přednačítá okno obrázků a hlásí
  `pending`/`ready`/`error`. `prime(urls)` je **celé okno** — cokoli mimo se hned uvolní
  (`removeAttribute('src')` = abort in-flight fetche), poslední okno se uvolní na unmountu, takže
  dlouhá show nekumuluje dekódované bitmapy. Readiness měří **`img.decode()`**, ne `onload`: onload
  znamená „bajty dorazily", dekódování by teprve proběhlo při prvním paintu (přesně ten záblesk
  prázdné plochy, kvůli kterému to celé je); `decode()` je feature-detected (jsdom ho nemá →
  fallback na `onload`/`onerror`). Pozdní `decode()` už uvolněného obrázku se ignoruje. Statusy žijí
  ve stavu → `statusOf` mění identitu při každém dosednutí, takže na něm jde záviset efektem;
  `useSlideshowSettings` = persistentní efekt+rychlost přes
  `lib/slideshowSettings` (read once on mount, setteri zapisují do localStorage, sanitizace);
  `useGridDensity()` → `{density,setDensity}` = hustota foto-mřížky (**vždy konkrétní počet sloupců
  1…10**, žádný `'auto'` režim) přes `useSyncExternalStore` nad `lib/gridDensity`. localStorage je
  **jediný zdroj pravdy** (žádná in-memory kopie): snapshot je primitivum (počet sloupců, nebo `null`
  = nic použitelného uloženo), takže Reactí `Object.is` porovnání nikdy nezacyklí. **Při prvním
  použití** (prázdné úložiště nebo starší `'auto'`/rozbitá hodnota k migraci) se hustota **jednou**
  naseeduje z šířky viewportu (`initialColumns`) a uloží — auto už jen seeduje první hodnotu, pak je
  to natvrdo uživatelova volba a pozdější resize s ní **nehne**. `subscribe` poslouchá i `storage`
  event → všechny záložky na zařízení drží stejný počet sloupců; `setGridDensity` sanitizuje, zapíše
  a překreslí **všechny** mřížky naráz, bez contextu a bez providera (takže i testy stránek fungují
  bez wrapperu);
  `useIsNarrowViewport()` = sdílený hook nad `matchMedia` (`(max-width: 767.98px)`, Bootstrap `md`;
  odebírá `change`, chybějící/rozbité `matchMedia` → „široký"; jeden zdroj pravdy pro offcanvas
  filtrů i výchozí hustotu mřížky);
  `usePrefersReducedMotion()` = sleduje `(prefers-reduced-motion: reduce)` přes `matchMedia`
  (odebírá `change`, chybějící/rozbité `matchMedia` → `false`) — volající dekorativní animaci
  **vynechá**, ne zkrátí)),
  `lib/` (`gestures.ts` = **pure, DOM-free rozhodovací helpery dotykových gest** sdílené
  `useSwipeNavigation`/`usePinchZoom` (a proto **přímo unit-testovatelné** bez jsdom touch sekvencí):
  `swipeAction(dx,dy,{threshold,ratio})` → `'prev'|'next'|null` (vlevo = next, vpravo = prev, práh +
  dominantní vodorovná složka), `touchDistance`/`touchMidpoint`, `pinchScale`/`clampScale`
  (clamp `[MIN_SCALE=1,MAX_SCALE=4]`, `DOUBLE_TAP_SCALE`), `isDoubleTap(dt,dist)` a `clampPan`;
  `compareZoom.ts` = **pure zoom/pan matematika** synchronního plátna v `DupComparePage` (a proto
  unit-testovatelná bez DOM): `ZoomView{scale,x,y}`, `IDENTITY_VIEW`, `MIN_SCALE=1`/`MAX_SCALE=8`/
  `ZOOM_STEP`, `zoomAt(view,factor,px,py,box)` (bod pod kurzorem zůstane pod kurzorem), `zoomCentre`,
  `panBy`, `clampView` (pan se drží v `(scale-1)*box/2`, takže obrázek nejde vytáhnout z panelu),
  `isZoomed`, `viewTransform`; oddělené od `gestures.ts` schválně — ten je touch-only a měří proti
  viewportu;
  `duplicateCompare.ts` = **pure logika porovnání dvojic**: `buildPairQueue(groups)` → `ComparePair[]`
  (vícečlenná skupina **po dvojicích proti keeperovi**, nikdy člen-člen; skupina s keeperem mimo
  members se přeskočí, ne uhodne), `pairId(a,b)` (neuspořádané, jako backend), `pairsInGroup`/
  `pairIndexInGroup` (popisek „dvojice i z n"), `dropPairsTouching(pairs,uid)` (po merge zmizí
  dvojice archivované fotky), `buildDiffRows(left,right,fmt)` → `DiffRow{key,left,right,differs}` —
  `differs` se počítá z **porovnávacího klíče, ne z formátovaného textu** (dva časy ve stejné minutě
  se pořád liší), jména se porovnávají jako **množina** (pořadí z API nic neznamená); `fmt` se
  injektuje, takže testy nezávisí na locale; `countDiffering(rows)`;
  `urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"; `libraryView.ts` = typ `LibraryView` (vč. `min_rating`/`flag`, přepínače `favorite` a facetů
  `year`/`album`/`label`/`person`) + `LIBRARY_DEFAULTS` +
  `LIBRARY_PATH` (= `/`, kanonická routa knihovny — **knihovna je úvodní stránka**; všechny odkazy
  v appce míří sem, `/library` je jen redirect pro staré odkazy) +
  **multi-výběr facetů `album`/`label`/`person`**: každý klíč nese **čárkou spojený seznam UID** (urlState
  ukládá každý klíč jako jeden string, čárka se v UID nevyskytuje) — helpery `parseFilterList`/
  `joinFilterList`/`addToFilterList`/`removeFilterList` (sic `removeFromFilterList`) seznam kódují;
  fotka musí být ve **všech** vybraných albech, nést **všechny** štítky a obsahovat **všechny** vybrané
  osoby (AND). Celý výběr round-tripuje URL query, takže Zpět ho obnoví;
  `viewToParams` (sanitizuje sort/archived/**year** — `toYear` propustí jen čtyřciferný rok, ručně
  psaná/zastaralá URL spadne na „bez filtru" místo backendové 400 —, prosákne `min_rating`/`flag`,
  přepínač `favorite` a čárkou spojené UID facetů `album`/`label`/`person` beze změny — `buildPhotoQuery`
  je rozloží na opakované parametry `?album=a&album=b`, které backend ANDuje; neznámé UID prostě nic
  nenamatchuje; `sort` union navíc `rating`) + `hasActiveFilters` (`{ignoreQuery}` na search stránce,
  neprázdný seznam album/label/person nebo `favorite` = aktivní filtr, zahrnuje rating/flag i facety) —
  mapování URL stavu na API params; `ratingHotkeys.ts` = pure `ratingHotkey(key)` (`0`–`5` →
  rating, `p`/`r`/`v` → osobní označení 👍/👎/👁 (stored pick/reject/eye), jinak null) + `isTypingElement(target)` (input/textarea/select/
  contenteditable → hotkey se přeskočí) — sdíleno detailem fotky i fokusnutou dlaždicí;
  `shortcuts.ts` = registr klávesových zkratek + pure helpery: `shortcutToken(key)` (normalizace
  `KeyboardEvent.key` — single-char lower-case, named keys passthrough, `?` zůstává), `isFormModalOpen`
  (je otevřený `.modal.show` s form controlem? → suppress zkratek za dialogem), `HELP_SHORTCUT_KEY`
  (`?`) a `SHORTCUT_GROUPS` (grouped Grid/Detail zdroj pravdy pro nápovědu, `titleKey`/`descriptionKey`
  typované jako i18next `ParseKeys`, takže neexistující klíč je compile error);
  `searchView.ts` = typ `SearchView` (= `LibraryView` + `mode`)
  + `SEARCH_DEFAULTS` (mode `hybrid`) + `toMode` sanitizér;
  `auditView.ts` = typ `AuditView` (filtry + `offset`, string-only pro URL) + `AUDIT_DEFAULTS`
  + `AUDIT_PAGE_SIZE` (100) + `pickFilters` (view bez offsetu) + `viewToParams` (mapuje na
  `AuditListParams`, `since`/`until` z `YYYY-MM-DD` rozšíří na RFC 3339 hranice dne v UTC) — podklad
  `AuditPage`;
  `reviewDecisions.ts` = view-model pro `ReviewDecisionsPage`: typ `ReviewDecisionsView`
  (`user`/`decision`/`offset`, string-only pro URL) + `REVIEW_DECISIONS_DEFAULTS`
  + `REVIEW_DECISIONS_PAGE_SIZE` (60) + `viewToAuditParams` (vždy `via:'review'` + `decision`)
  + `toReviewDecision(record, subjects, labels)` mapuje audit záznam na `ReviewDecision`
  (`verdict` Ano/Ne z akce, `kind` face/label, `photoUid`/`faceIndex`, cíl přeložený na jméno —
  `subject_name` z details, jinak roster mapa, fallback UID) + `parseDecisionFilter`;
  `savedSearchView.ts` = pure `isSearchParams(params)` (přítomnost `mode` rozlišuje search od library
  pohledu) + `savedSearchHref(params)` (složí `pathname?query` na `LIBRARY_PATH` nebo `/search`, minimálně
  zakóduje uložené params proti defaultům přes `writeUrlState`, ignoruje neznámé/zastaralé klíče) —
  obnova uloženého hledání na přesnou URL;
  `mapView.ts` = typ `MapView` (mapset + viewport `lat`/`lng`/`z` + filtry) + `MAP_DEFAULTS` +
  `mapViewToParams` (sanitizuje archived) + `viewportFromView`/`mapsetFromView`/`hasActiveMapFilters`
  — mapování URL stavu mapy na feed params; `mapPopup.ts` = pure `buildPopupElement` (náhled +
  odkaz na detail fotky jako popup element, plain klik → SPA navigace, modifikovaný klik projde);
  `faceState.ts` = pure `faceState(face)` (`assigned`/`unassigned`/`unmatched` — čte přiřazení, ne
  `face.action`, aby optimistický update držel box i řádek v syncu s právě provedeným klikem)
  + `isNamed`; jeden zdroj pravdy pro barvy v overlayi, `FacesPanel` i `PeoplePanel`;
  `faceGeometry.ts` = pure `faceBoxStyle` (normalized bbox → absolutní `left/top/width/height`
  v %, pro overlay) + `padBbox`/`boxWithinCrop`/`cropImageStyle` + `displayFrame` (uložené
  rozměry + EXIF orientace → **zobrazený** rám; orientace 5–8 prohazuje strany, protože bbox je v
  display space) + `squareCrop` (bbox → výřez **čtvercový v pixelech**, ne v normalized
  jednotkách — to je to, co brání deformaci: „čtverec" v normalized rámu 4000×3000 je v pixelech
  obdélník a ve čtvercové dlaždici by obličej rozmáčkl; roste kratší pixelovou stranu ze středu a
  zasune výřez zpátky do rámu) + `faceCropStyle` (**legacy**, škáluje osy nezávisle → deformuje, a
  čte `tile_*`, což je centrovaný čtverec, ne celý rám; jen pro `FaceThumb`);
  `faceThreshold.ts` = pure převod prahu hledání osoby mezi **procenty** (UI) a **kosinovou
  vzdáleností** (backend): `percentToDistance` (`1 - p/100`)/`distanceToPercent` (inverzní,
  zaokrouhlený — i „match %" na kartě)/`clampThresholdPercent` + konstanty rozsahu (20–80, krok 5,
  default 50); `candidateReview.ts` = pure model review mřížky `/faces`: `ReviewItem`/`CandidateStatus`
  (`pending`/`done`/`error`), bucket `new`/`assign`/`done` (`bucketOf`, sdílený barevný kód přes
  `BUCKET_VARIANT`), `FilterTab`/`FILTER_TABS`/`matchesTab`/`tabCounts`, `isActionable`,
  `buildAssignRequest` (zrcadlí `useFaces`: existující `marker_uid` → `assign_person`, jinak
  `create_marker` s bboxem — nikdy nevyrobí duplicitní marker) a `buildRejection`;
  `recognitionSweep.ts` = pure helpery `/recognition` sweepu: konstanty posuvníku jistoty (50–95,
  krok 1, default 75) + `clampConfidencePercent`, `PersonState`, `personActionableCount`/`hasActionable`
  (karta osoby zmizí, když `hasActionable` je false), a **plochá klávesová fokus sekvence** napříč
  osobami (`FocusEntry`, `focusKey`, `focusSequence` = jen akční karty, `nextFocusKey`);
  `expandSearch.ts` = pure logika `/expand`: default prahu **70 %** (`EXPAND_THRESHOLD_DEFAULT_PERCENT`,
  rozsah/krok sdílí `faceThreshold`) + `clampExpandThresholdPercent`, `expandThresholdDistance`
  (procenta → vzdálenost, `toFixed(4)` řeže float šum pro URL), limit 1–200 default 50
  (`clampExpandLimit`), `ExpandSource` + `expandSources` (picker: bez prázdných sbírek, řazený dle
  počtu fotek sestupně, tiebreak jménem) a `similarityPercent` (podobnost kandidáta → celá %);
  `outlierReview.ts` = pure model `/outliers`: lifecycle `pending`→`removed`/`confirmed`/`error`
  (`OutlierItem`, `outlierKey` = `photo_uid:face_index`, `toOutlierItems`, `isActionable` — errored
  karta se **počítá**, její zápis selhal, takže je pořád nerozhodnutá —, `canUnassign` = má marker,
  jinak není co odpojit) + aritmetika prahu: **UI mluví v procentech, endpoint v kosinové
  vzdálenosti**, `outlierThresholdDistance` (0 % → 0 = „vrať vše", 100 % → `OUTLIER_MAX_DISTANCE`=1,
  protože dva **různí** lidé sedí kolem 1.0 a dál není co schovávat; `toFixed(4)` řeže float šum pro
  URL), `clampOutlierThresholdPercent` (default **0 = zobrazit vše**; nenulový default by tiše
  schovával obličeje), `distancePercent` (schválně **ne** podobnost — na téhle stránce větší číslo
  znamená „dál od člověka", což je ta souzená veličina) a `OUTLIER_LIMIT`=200;
  `coordinates.ts` = pure tolerantní parser souřadnic pro location picker: `parseCoordinates(input)`
  → `{ok:true,value:{lat,lng}}` | `{ok:false,error:'empty'|'format'|'range'}` (desetinné stupně /
  DMS / stupně-desetinné-minuty, komma/mezera oddělovač, ±/hemisféry N/S/E/W, unicode primy/`''`,
  axis reorder dle hemisfér, range check ±90/±180) + `formatCoordinates({lat,lng},precision=6)` →
  kanonický `"49.123400, 16.567800"` (round-tripuje parserem, ale je to **zobrazovací, ztrátový**
  formát — `16.7083583333333` → `16.708358`, proto se nezměněná souřadnice do PATCHe vůbec
  neposílá) — sdílí `MetadataPanel` picker;
  `kenBurns.ts` = pure `kenBurnsMotion(uid,intervalMs)` → endpointy pomalého zoom+pan přes celý
  snímek (`durationMs` = interval, takže animace trvá přesně jeden slide) + `kenBurnsStyle(…)` →
  `--kb-*` custom properties pro `slideshow.css` + `panLimit(scale)`. Parametry (8 směrů × zoom
  in/out × 5 hloubek) se derivují **deterministicky** z FNV-1a hashe `uid`, takže stejné album
  vypadá při každém přehrání stejně. Oba endpointy drží offset do `panLimit` svého scale a scale
  i offset se interpolují lineárně → **obraz nikdy neodkryje okraj** scény;
  `gridDensity.ts` = typ `GridDensity` (**prosté `number`**, počet sloupců) + `GRID_COLUMNS_MIN`
  (**1** = jedna fotka na řádek) / `GRID_COLUMNS_MAX` (**10**) / `GRID_COLUMN_CHOICES` (1…10) /
  `GRID_TILE_MIN_PX` (140, cílová šířka dlaždice **jen pro seed**) / `GRID_GAP_PX` (**3** — hairline
  mezera pro hustou hero-first zeď) / `GRID_DENSITY_DEFAULT` (**5** — konkrétní fallback, když nejde
  změřit šířka viewportu) + pure `readStoredDensity`/`writeDensity`/`sanitizeDensity`/`stepDensity`
  (localStorage `kukatko.grid.density`, holý skalár v JSON; číslo se zaokrouhlí a **oklampuje do
  1…10**; `sanitizeDensity` skládá i starší `'auto'`/nečíselné hodnoty na konkrétní počet seedovaný
  z šířky; `readStoredDensity` vrací `null`, když **není uloženo použitelné číslo** — prázdné/
  nedostupné úložiště, rozbitý JSON nebo starší `'auto'` —, aby volající naseedoval z šířky a hodnotu
  zmigroval) + `initialColumnsForWidth(width)` (kolik ~140px dlaždic se vejde přes šířku, oklampnuto
  1…10; úzký → 1, telefon → 1–2, hodně široko → 10) + `initialColumns()` (seed pro aktuální viewport)
  + pure `gridTemplateColumns(density)` → **vždy `repeat(N, 1fr)`** = přesně N stejných sloupců na
  každém viewportu (žádný `auto-fill` fallback, protože uživatel vždy volí konkrétní číslo); mezeru
  mezi dlaždicemi řeší odděleně `gap` na kontejneru;
  `slideshowSettings.ts` = typ `SlideshowSettings{effect,intervalMs}` + `SlideshowEffect`
  (`fade`/`slide`/`kenburns`/`none`) + nabídky `SLIDESHOW_EFFECTS`/`SLIDESHOW_INTERVALS_MS` (1/2/3/5/10/15/30 s)
  + `SLIDESHOW_DEFAULTS` (`fade`, 5 s)
  + pure `readSettings`/`writeSettings`/`sanitizeSettings` (localStorage `kukatko.slideshow.settings`,
  sanitizace efektu + interval **snapnutý na nejbližší nabízenou hodnotu** — dřív uložený interval,
  který už v nabídce není (7 s), tak nespadne pod stůl ani nevyrenderuje prázdnou položku; při shodné
  vzdálenosti vyhrává kratší; fallback na defaulty při chybě/nedostupném storage);
  `slideshowView.ts` = pure `slideshowHref(scope,view)` (staví `/slideshow?…` z `LibraryView` přes
  `writeUrlState` + scope `album`/`label`/`mode`, default filtry vynechá — launch link promítání;
  `mode` se zapíše i když je roven defaultu, protože `SlideshowPage` čte jeho **přítomnost** jako
  „tohle přišlo z hledání");
  `duration.ts` = pure `splitDuration(ms)` → `{hours,minutes,seconds}` (zaokrouhlí na sekundy,
  záporné/nekonečné → nula) + `formatDuration(ms,t)` → kompaktní jednořádkový zápis přes i18next
  (`45 s` / `3 min 20 s` / `1 h 5 min`; nulová část se vynechá, u hodin se sekundy zahodí)
  + `slideshowDurationMs(count,intervalMs)` (celá show = interval na fotku)
  + `slideshowRemainingMs(index,total,intervalMs)` (fotky, které teprve přijdou — aktuální snímek
  se nepočítá, poslední slide hlásí nulu, index za koncem taky);
  `trashCountdown.ts` = pure `purgeCountdown(archivedAt,retentionDays,now?)` (zbývající dny do
  auto-purge z `archived_at` + retence → `{daysLeft,due}` nebo `null` když odpočet neplatí
  (nearchivovaná / retence ≤ 0 / neparsovatelné), odpočet na kartách koše);
  `format.ts` = pure `formatBytes(bytes)` (byte count → human-readable binární jednotky, např.
  `1536`→`"1.5 KB"`, neplatné→`"0 B"`) pro velikost souboru na duplicate-group kartách +
  `formatDuration(ms)` (ms → `M:SS`/`H:MM:SS`, neplatné→`"0:00"`) pro délku videa na dlaždicích +
  `formatMonth(year,month,locale)` (1-based rok/měsíc → locale-aware krátký měsíc + rok, např.
  `2026,1,'en'`→`"Jan 2026"`, mimo 1–12 → `""`) pro popisky ticků časové osy +
  `formatCaptureRange(from?,to?)` (rozsah `taken_at` alba → nejužší tvar: jeden měsíc
  `"6/2007"`, jeden rok `"2006"`, jinak `"1998–1999"` s en-dash; chybějící/neplatná mez →
  `""`, tj. album bez datovaných fotek nekreslí řádek) pro `AlbumTile` +
  **locale-aware** `formatDate(value,locale)`/`formatDateTime(value,locale)` (ISO/epoch/`Date` →
  `toLocaleDateString`/`toLocaleString` s **aktivním jazykem UI** `i18n.language`, ne výchozím
  jazykem prohlížeče; neparseovatelný vstup → původní string; používá PhotoTile/DuplicateGroupCard/
  MetadataPanel/Import/System pro datumy v cs/en formátu))),
  `services/` (`health.ts`, `capabilities.ts` = `fetchCapabilities(signal)` nad `GET /api/v1/capabilities`
  → `Capabilities{semantic_search}` (posílá session cookie, `credentials:'same-origin'`), `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role` (striktní žebřík `viewer < editor < admin < maintainer`)/`AuthSession`, `ApiError` se
  statusem, `roleAtLeast`, `canWrite` (editor+), `isAdmin` (admin+), `isMaintainer` (maintainer) a
  `canImport` (= maintainer; import je provozní schopnost) — vše přes `ROLE_RANK` zrcadlící backend
  `internal/auth/role.go`; `MIN_PASSWORD_LENGTH`; `photos.ts` = `fetchPhotos(params,signal)` nad `GET /api/v1/photos`
  (filtry/řazení/stránkování → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `searchPhotos(params,mode?,signal)` nad `GET /api/v1/search` (mód
  `fulltext`/`semantic`/`hybrid`, odpověď navíc `mode`+`degraded`),
  `fetchSimilar(uid,limit?,signal)` nad `GET /api/v1/photos/{uid}/similar` → `SimilarPhoto[]`
  (`Photo`+`distance`; empty-friendly), typy `SimilarPhoto`/`SimilarResponse`,
  `fetchTimeline(params,signal)` nad `GET /api/v1/photos/timeline` → `Timeline{buckets,total}`
  (měsíční date-histogram, stejné filtry jako list; sort/stránkování backend ignoruje), typy
  `Timeline`/`TimelineBucket{year,month,count,cumulative}` — podklad `TimelineScrubber`,
  `fetchPhotoYears(params,signal)` nad `GET /api/v1/photos/years` → `YearsResponse{years,total}`
  (rok-histogram, stejné filtry jako list; backend ignoruje `year` sám, sort/stránkování taky),
  typy `YearsResponse`/`YearBucket{year,count}` — podklad year facetu (`useLibraryFacets`);
  `PhotoListParams` navíc `year?: string` (čtyřciferný rok), `buildPhotoQuery` ho serializuje,
  `favoritePhoto(uid,favorite,signal)` nad `PUT`/`DELETE /api/v1/photos/{uid}/favorite` (per-user
  toggle, 204, podklad optimistického `useFavorite`),
  `ratePhoto(uid,{rating?,flag?},signal)` nad `PUT /api/v1/photos/{uid}/rating` +
  `clearRating(uid,signal)` nad `DELETE …/rating` (per-user hvězdy 0–5 + osobní označení
  none|pick|reject|eye, 204, podklad `useRating`), typy `RatingUpdate`/`RatingFlag`,
  `regenerateThumbnail(uid,signal)` nad `POST /api/v1/photos/{uid}/regenerate-thumbnail`
  (editor/admin servisní akce, synchronní, `RegenerateThumbnailResult{status,sizes}`, 422 =
  originál nedekódovatelný; podklad `RegenerateThumbnailButton`),
  **koš** `unarchivePhoto(uid)` (`POST …/unarchive` obnova), `purgePhoto(uid)` (`POST …/purge?confirm=true`
  trvalé mazání), `emptyTrash()` (`POST /trash/empty?confirm=true` → `PurgeResult{purged,failed}`),
  `fetchTrashInfo()` (`GET /trash/info` → `TrashInfo{retention_days}`),
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `videoUrl(uid,token?)` (range stream pro
  `<video>`; při R2 backendu routa **302** redirectne na Workera, `<video>` redirect následuje
  při každém requestu, takže seek jede vždy proti čerstvému podpisu), `GRID_THUMB_SIZE`,
  typy `Photo` (vč. `is_favorite` + per-user `rating`/`flag` + video pole
  `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps` + **`thumb_url`/`download_url`** +
  **`stack_uid`/`stack_count`**)/`PhotoListParams`
  (vč. `album`/`label` scope + **`person` scope** (čárkou spojené UID subjektů → opakované `?person=`, AND)
  + **`country`/`city` place scope** + `favorite` filtr + `min_rating`/`flag` filtry)/`PhotoSort`
  (vč. `rating`)/`RatingFlag`/`ArchivedFilter`/`SearchMode`, `ApiError`.
  **Adresy médií se neskládají z UID.** Grid dlaždice i download odkaz berou `photo.thumb_url` /
  `photo.download_url` z payloadu — jen server umí URL podepsat. `thumbUrl(uid,size)` zůstává pro
  velikost, kterou payload nenese (lightbox, canvas editoru, cover podle UID) a `downloadUrl(uid,…)`
  pro **rendering nedestruktivního editu**, který umí jen aplikace;
  `organize.ts` = Albums/Labels klient: alba `fetchAlbums`/`fetchAlbum`/`createAlbum`/`updateAlbum`/
  `deleteAlbum`/`addAlbumPhotos`/`removeAlbumPhotos`, štítky `fetchLabels`/
  `fetchLabel`/`createLabel`/`updateLabel`/`deleteLabel`/`attachLabel`/`detachLabel`; typy
  `Album`/`AlbumCount`/`AlbumInput`/`AlbumType`/`Label`/`LabelCount`/`LabelInput`;
  `savedSearches.ts` = uložená hledání klient: `fetchSavedSearches`/`createSavedSearch(name,params)`/
  `updateSavedSearch(uid,{name?,params?})`/`deleteSavedSearch(uid)` nad `/api/v1/saved-searches`, typy
  `SavedSearch`/`SavedSearchParams` (= verbatim URL view-state `Record<string,string>`)/
  `SavedSearchUpdate`; `announcement.ts` = instance-wide oznámení klient: `fetchAnnouncement()`/
  `setAnnouncement(message,level)`/`clearAnnouncement()` nad `/api/v1/announcement`, typy `Announcement`
  (`{message, level?, author_uid?, updated_at?}`, prázdný `message` = nic zveřejněno)/`AnnouncementLevel`
  (`'info'|'warning'`); `search.ts` = grouped **global search** klient: `globalSearch(q,signal)` nad
  `GET /api/v1/search/global` → `GlobalSearchResult{query,albums,labels,people,photos}` (top-N per
  skupina, každá vždy pole) + pure helpery `hasEntityMatches`/`isEmptyResult`, typy
  `GlobalSearchAlbum`/`GlobalSearchLabel`/`GlobalSearchPerson`/`GlobalSearchResult`; oddělené od
  photo `searchPhotos` (fulltext/semantic/hybrid), podklad `GlobalSearchSections`; `bulk.ts` =
  `bulkUpdatePhotos(uids,ops)` nad `POST /photos/bulk` (hromadná úprava výběru), typy
  `BulkOperations` (add/remove alba+štítku, set/clear caption+popisu+polohy,
  archive/unarchive, set_favorite per-user)/`BulkLocation`/`BulkResult`; `duplicates.ts` =
  `fetchDuplicates(params,signal)` nad `GET /api/v1/duplicates` (skupiny duplikátů →
  `DuplicatesResponse{groups,total,limit,offset,next_offset}`) + `mergeDuplicates(input,signal)` nad
  `POST /api/v1/duplicates/merge` (řešení skupiny → `MergeResult{albums_added,labels_added,people_added,
  metadata_filled[],archived,dry_run}`; `dry_run:true` = náhled), typy `DuplicateReason`/
  `DuplicateMember`/`DuplicateGroup`/`DuplicatesParams`/`MergeInput`/`MergeResult`; `upload.ts` =
  `uploadFile(file,{onProgress,signal})`
  nad **`XMLHttpRequest`** (jeden soubor/request kvůli upload-progress eventům, FormData se
  streamuje), `isAbortError`, typy `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`; `photos.ts` navíc `fetchPhoto(uid)` (detail `GET /photos/{uid}` →
  `PhotoDetail` = `Photo`+`files`+`albums`+`labels` inline chipy `+ uploader?` `{uid,name}`),
  `updatePhoto(uid,patch)`
  (`PATCH …` částečná editace metadat → `PhotoMetadataUpdate`, null maže nullable),
  `fetchEdit(uid)`/`saveEdit(uid,edit)` (`GET`/`PUT …/edit` nedestruktivní edit → `PhotoEdit`
  crop/rotation/brightness/contrast), `downloadUrl(uid,{original?,token?})` (URL downloadu,
  default honoruje edit, `original:true` pro originál),
  `downloadPhotosZip({photoUids?,albumUid?,name?})` (**hromadné stažení ZIP**: `POST
  …/download-zip`, přečte odpověď jako `Blob` a stáhne ji přes dočasnou object URL — jméno
  archivu skládá klient (`name`.zip nebo `kukatko-photos-<date>.zip`, `date` počítá klient a
  posílá i serveru), hází `ApiError` (413 = přes strop); typ `ZipDownloadRequest`),
  **stacky** `stackPhotos(photoUids,signal)` (`POST …/photos/stack` — ruční seskupení výběru → `PhotoDetail`
  nového primárního), `setStackPrimary(uid,signal)` (`POST …/{uid}/stack/primary`),
  `unstackMember(uid,signal)` (`POST …/{uid}/unstack`) a `unstackAll(uid,signal)`
  (`POST …/{uid}/unstack-all`) — všechny vracejí refreshnutý `PhotoDetail`; typy `PhotoDetail` (navíc
  `stack_members?: StackMember[]` — pruh variant, primary první)/`StackMember`
  `{uid,file_name,media_type,file_mime,file_width,file_height,file_size,is_primary,thumb_url?,download_url?}`/`PhotoAlbumRef`/
  `PhotoLabelRef`/`PhotoUploaderRef`/`PhotoMetadataUpdate`/`PhotoEdit`; `people.ts` = People/face klient: subjekty
  `fetchSubjects`/`fetchSubject`/`createSubject`/`updateSubject`/`deleteSubject`/
  `fetchSubjectPhotos`, obličeje `fetchFaces`/`assignFace`, shluky `fetchClusters`/
  `assignCluster`/`removeClusterFace`, outlier `fetchOutliers`; typy `Subject`/`SubjectCount`/
  `SubjectInput`/`SubjectType`/`Bbox`/`FaceView`/`FacesResponse`/`AssignRequest`/`Suggestion`/
  `ClusterView`/`ExampleFace`/`ClusterAssignRequest`/`RemoveFaceRequest`/`OutlierResult`/
  `OutlierFace`; sdílí `ApiError`+`buildPhotoQuery` z `auth.ts`/`photos.ts`);
  `faces.ts` = klient hledání „najdi osobu mezi neotagovanými fotkami":
  `searchCandidates(subjectUid,{threshold,limit},signal)` nad `POST /subjects/{uid}/candidates`; typy
  `CandidateSearchRequest`/`CandidateResult`/`Candidate`/`FaceBox`/`CandidateCounts`/`CandidateAction`
  (`create_marker`/`assign_person`/`already_done`)/`CandidateReason`; potvrzení jde přes `assignFace`
  z `people.ts`, zamítnutí přes `feedback.ts`; `feedback.ts` = perzistentní zpětná vazba (nemutuje,
  jen drží zamítnutý obličej/fotku mimo příští hledání): `rejectFace(req,signal)`/`unrejectFace(req,signal)`
  nad `POST`/`DELETE /feedback/face-rejections`, typ `FaceRejection` `{photo_uid,face_index,subject_uid}`,
  a `rejectLabel(req,signal)`/`unrejectLabel(req,signal)` nad `POST`/`DELETE /feedback/label-rejections`,
  typ `LabelRejection` `{photo_uid,label_uid}`; **`confirmFace(req,signal)`/`unconfirmFace(req,signal)`**
  nad `POST`/`DELETE /feedback/face-confirmations`, typ `FaceConfirmation`
  `{photo_uid,face_index,subject_uid}` — **opačná polarita než `rejectFace`**: zapisuje „tenhle
  obličej **JE** tahle osoba" (✗ v outlier review = „ne, fakt je to on"), backend pak potvrzený
  obličej z dalších outlier výsledků vyloučí; zaměnit ji za `rejectFace` znamená uložit pravý opak
  toho, co uživatel řekl; **`dismissDuplicate(req,signal)`/`undismissDuplicate(req,signal)`** nad
  `POST`/`DELETE /feedback/duplicate-dismissals`, typ `DuplicateDismissal` `{photo_uid,other_uid}` —
  „tyhle dvě fotky NEJSOU duplikáty" z `DupComparePage` („Nechat obě"); dvojice je **neuspořádaná**
  (backend normalizuje), nic se nearchivuje ani neslučuje, jen se zapíše názor a `GET /duplicates`
  pak tu hranu na každém dalším scanu zahodí (vše idempotentní → jde volat optimisticky);
  `expand.ts` = klient rozšiřování sbírky: `searchSimilar(kind,uid,{threshold,limit},signal)` nad
  `GET /albums/{uid}/similar` / `GET /labels/{uid}/similar` (`threshold` = **kosinová vzdálenost**,
  převod z procent dělá volající přes `lib/expandSearch`), typy `ExpandKind`/`ExpandCandidate`
  (`photo` má `thumb_url` už oražené)/`ExpandResult` (summary počty + `min_match_count` +
  `reason?` `empty_collection`/`no_source_embeddings`)/`ExpandReason`/`ExpandSearchRequest`;
  přidávání jde přes `bulk.ts` (`POST /photos/bulk`), zamítnutí přes `feedback.ts`;
  `recognition.ts` = klient recognition sweepu: `streamSweep(params,onMessage,signal)` nad
  `GET /faces/sweep` **streamuje NDJSON** (`fetch`+`ReadableStream`, řádkuje ručně, `onMessage` dostane
  jen kompletní řádky), typy `SweepParams` `{confidence,limit}` (`confidence` = **procenta**, backend
  si je přeloží na vzdálenost) a `SweepMessage` = `progress`|`person`|`summary` (`SweepPerson` nese
  `candidates`/`counts`/`actionable` ve stejném tvaru jako `faces.ts`); abort přes `signal` = `AbortError`
  (volající ignoruje); potvrzení jde přes `assignFace`, zamítnutí přes `rejectFace`;
  `review.ts` = klient review hry: `fetchReviewQueue(limit?,signal)` nad `GET /review/queue`,
  `answerReview(questionId,answer,signal)` nad `POST /review/answer` (idempotentní; typy
  `ReviewQuestion`/`ReviewQueue`/`ReviewAnswer`; podklad `useReviewGame`), a **žebříček**
  `fetchLeaderboard(window,signal)` nad `GET /review/leaderboard?window=all|7d|today` →
  `Leaderboard{window,caller_uid,entries:LeaderboardEntry[]}` (`LeaderboardEntry` =
  `{user_uid,display_name,yes_count,no_count,total,is_me}`, řazeno backendem podle `total`),
  typ `LeaderboardWindow` = `'all'|'7d'|'today'` + `LEADERBOARD_WINDOWS` (pořadí přepínače);
  podklad `LeaderboardPage`;
  `map.ts` = mapový klient: `fetchMapPhotos(params,signal)` nad `GET /api/v1/map/photos`
  (GeoJSON FeatureCollection geotagovaných fotek + `buildMapQuery`), `tileLayerUrl(mapset)` (Leaflet
  URL template na backend proxy, **bez API klíče**), `reverseGeocode(lat,lng,signal?)` nad
  `GET /api/v1/map/rgeocode` (on-demand reverse geocode pro detail fotky → `GeocodeResult`),
  `searchPlaces(query,limit?,signal?)` nad `GET /api/v1/map/geocode` (**forward** geocode pro
  editor polohy → `Place[]` = `{name,label,type,location,lat,lng}` od nejlepší shody; žádná shoda
  = **prázdné pole**, ne chyba; volající **musí debouncovat** — backend sice cachuje a
  rate-limituje, ale request na klávesu je jak vypálit měsíční kredit za odpoledne),
  **`probeTileFailure(tileUrl,signal?)`** (`<img>` status v JS nevidíš → dlaždice, kterou Leaflet
  nenačetl, se přefetchne a status proxy se přeloží na `TileFailure`: **424 → `key_rejected`**
  (mapy.com odmítá **náš** klíč), 429 → `rate_limited`, 503 → `unavailable`, jinak `error`;
  200/404 → `null`, protože chybějící dlaždice mimo pokrytí je normální odpověď; síťová chyba →
  `'error'`, abort probublá), `toMapset`/`MAPSETS`; typy
  `MapFeature`/`MapFeatureCollection`/`MapFeatureProperties`/`MapPhotoParams`/`Mapset`/
  `TileFailure`/`GeocodeResult`/`RegionalItem`/`Place`);
  `places.ts` = klient hierarchie míst: `fetchPlaces(country?,signal)` nad `GET /api/v1/places`
  → `PlaceCountry[]` (země s počty + nested `cities`, volitelné `country` drillne do měst jedné
  země); typy `PlaceCountry`/`PlaceCity`; procházení fotek lokality jde přes sdílené
  `fetchPhotos({country,city})`;
  `import.ts` = admin import klient: `fetchImportRuns(signal)` nad `GET /api/v1/import/runs`
  (`{runs,limit,offset,sources}`), `fetchJobStats(signal)` nad `GET /api/v1/jobs/stats`,
  `startImport(source,signal)` nad `POST /api/v1/import/{photoprism|photosorter}` (409 → ApiError);
  typy `ImportSource`/`RunStatus`/`ImportCounts`/`ImportRun`/`ImportSources`/`ImportRunsResponse`/
  `StartImportResult`/`JobStats`),
  `maintenance.ts` = admin maintenance klient: `fetchMaintenanceScan(signal)` nad
  `GET /api/v1/maintenance/scan` → `ScanReport`, `runMaintenanceRepair(options,signal)` nad
  `POST /api/v1/maintenance/repair` → `RepairResult`, `purgeAuditLog(olderThanDays,signal)` nad
  `POST /api/v1/maintenance/audit/purge` → `AuditPurgeResult` (`{deleted,older_than_days,cutoff}`);
  typy `Finding`/`ScanReport`/`RepairOptions`/`RepairResult`/`AuditPurgeResult`; sdílí `ApiError`
  z `auth.ts` a `fetchJobStats` z `import.ts` pro progress,
  `system.ts` = admin system-status klient: `fetchSystemStatus(signal)` nad `GET /api/v1/system/status`
  → `SystemStatus`, `triggerBackup(signal)` nad `POST /api/v1/backup` (409/503 → ApiError),
  `requeueDeadLetterJobs(signal)` (vylistuje `GET /jobs?state=dead` → per-job `POST /jobs/{id}/requeue`,
  vrací počet, 404/409 skip); typy `SystemStatus`/`DatabaseStatus`/`EmbeddingsStatus`/`JobsStatus`/
  `BackupStatus`/`ImportsStatus`/`StorageStatus`/`MapsStatus`/`MapsState`/`VersionInfo`; sdílí
  `ApiError` z `auth.ts` a `ImportRun` z `import.ts`,
  `users.ts` = admin klient správy účtů nad `/api/v1/admin/users`: `fetchUsers(signal)` → `AdminUser[]`
  (= `User` + `note`), `createUser(body,signal)` (`POST`, 409 = obsazený username, 400 = slabé heslo /
  neplatná role / dlouhá poznámka), `updateUser(uid,body,signal)` (`PATCH`, **replace** celého
  mutovatelného profilu → posílej i pole, která dialog nenabízí), `setUserDisabled(user,disabled,signal)`
  (zakázat → dedikovaný `POST /{uid}/disable`, který nepotřebuje profil a nepřepíše souběžnou editaci;
  povolit → `PATCH` s `disabled:false`, vlastní endpoint neexistuje) a `resetUserPassword(uid,pwd,signal)`
  (`POST /{uid}/password`, 204, odhlásí všechny session cíle); konstanty `ROLES`
  (`viewer`/`editor`/`admin`/`maintainer`, vzestupně po žebříku)/`MAX_NOTE_LENGTH`,
  typy `AdminUser`/`CreateUserBody`/`UpdateUserBody`; hash hesla nemá kam uniknout — backend ho
  neserializuje a žádný typ pro něj nemá pole,
  `audit.ts` = admin auditní klient nad `GET /api/v1/audit`: `fetchAuditLog(params,signal)` →
  `AuditListResponse{entries,total,limit,offset,next_offset}`, `buildAuditQuery` serializuje filtry
  (prázdné/nulový offset vynechá); typy `AuditRecord` (nullable `actor_uid`/`target_uid`/`ip`/
  `user_agent`/`details`)/`AuditListParams` (vč. `via:'review'` + `decision:'yes'|'no'` pro admin
  přehled review rozhodnutí); sdílí `ApiError` z `auth.ts`. Pozor na názvosloví:
  query params používají jména endpointu (`user`/`entity_type`/`entity_uid`), záznamy sloupce
  (`actor_uid`/`target_type`/`target_uid`),
  `i18n/` (i18next init — options jsou exportované jako `initOptions`, ať si je test může nabootit
  do vlastní instance — + `locales/{cs,en}/common.json`;
  typované klíče přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů;
  **čeština default**, žádné natvrdo zapsané UI texty — vše přes `t()`. Jediný detektor je
  `localStorage` (kam píše `LanguageSwitcher` z `AccountPage`); `navigator`/`htmlTag` **záměrně
  nejsou** v `detection.order`, jinak by anglicky nastavený prohlížeč dostal při první návštěvě
  anglické UI — bez uložené volby rozhoduje `fallbackLng: 'cs'`. **Pluralizace** přes
  i18next CLDR plural sufixy: count-vázané řetězce kde se podstatné jméno shoduje s číslem mají
  formy `key_one/_few/_many/_other` (čeština) a `key_one/_other` (angličtina) — caller jen předá
  `{ count }` (např. `albums.photoCount`, `clusters.size`, `bulkEdit.title`, `duplicates.memberCount`/
  `archived`, `trash.confirm.bulk`); label-tvary s dvojtečkou/závorkou (`library.count`, `selection.count`)
  zůstávají bez plurálu. **Datumy/čísla respektují jazyk** přes `lib/format` `formatDate`/`formatDateTime`
  (`i18n.language`). **Drift-guard testy** `i18n.test.ts` (cs/en mají identické *logické* klíče po
  odstranění plural sufixu, žádné prázdné hodnoty, každý jazyk má všechny své CLDR plural kategorie,
  interpolační `{{var}}` proměnné se shodují napříč jazyky; navíc **default-language testy** nad
  čerstvou instancí z `initOptions`: prázdný localStorage → `cs` i pod anglickým prohlížečem,
  uložená volba vyhrává, změna jazyka se uloží) + `screens.test.tsx` (reprezentativní
  obrazovky — navbar + dlaždice — se vykreslí bez missing-key warningů v cs i en přes
  `cloneInstance({saveMissing})`, plural rendering 1/3/5, language-switch přepíše viditelný text)),
  `styles/tokens.css` (**design token vrstva** — jediný zdroj pravdy pro odstupy, rádiusy, elevaci,
  motion a typografickou škálu; importovaná **jednou** v `main.tsx` hned za Bootswatch CSS a **před**
  `app.css`, které tokeny konzumuje. Bootswatch Superhero zůstává základní téma — tohle je vrstva
  **nad** ním, nepřepisuje `--bs-*` proměnné globálně (jediná výjimka je cílený **theme root**).
  **Theme root:** aplikace běží s `data-bs-theme="dark"` na `<html>` (v `index.html`) — bez něj
  Superhero nechává `--bs-tertiary-bg`, `--bs-secondary-bg(-subtle)` a `--bs-emphasis-color` na
  **světlých** hodnotách na `:root` a do tmy je překlápí až uvnitř `[data-bs-theme=dark]`, takže
  `.bg-body-tertiary` panely (advanced-filtr knihovny, `SelectionBar`, detail řádek auditu) i
  skeletony (`.bg-secondary-subtle`) se malovaly skoro bílé pod skoro bílým `--bs-body-color` =
  neviditelné popisky (white-on-white). Superhero navíc barví celý chrome do syté navy; foto-appka
  musí opak — jediné syté na obrazovce má být fotka. `:root[data-bs-theme='dark']` v `tokens.css`
  proto **re-pinuje hrstku `--bs-*` proměnných na vlastní identitu**: teple-neutrální **near-black**
  ramp (`--bs-body-bg`/`-color`, `--bs-tertiary-`/`secondary-bg`, `--bs-card-bg`, `--bs-border-color`
  a `--bs-dark` pro navbar) a **jeden chladný azurový akcent** (`--bs-primary`, `--bs-link-color`,
  `--bs-navbar-active-color` + `--bs-primary-*-subtle/emphasis`). Každý re-pin míří na `--kk-*` token,
  takže paleta žije na jednom místě. Obsah: **akcent** `--kk-accent` (světlý — text/link/focus na
  tmavých povrchech), `--kk-accent-hover`, `--kk-accent-solid` (tmavší — výplň s bílým textem v AA),
  `--kk-accent-solid-hover`, `--kk-accent-subtle`, `--kk-on-accent` (azura je záměrná volba, ne
  oranžová: tři entitní odstíny jsou obsazené, `danger` je červená, tak zbývá jeden nezabraný hue, a
  chladný akcent na teplém chromu se nepere s fotkami); **povrchy + elevace** — warm-near-black ramp
  `--kk-surface-page`/`-1`/`-raised`/`-overlay` + `--kk-surface-sunken` (jáma) a `--kk-surface-border`
  (vlásková linka); **průsvitná hlavička** `--kk-header-bg` (tón stránky na 72 %), `--kk-header-blur`
  (14px) a `--kk-header-border` — pro slim navbar sedící nad scrollujícím foto-wallem (viz `app.css`,
  s `@supports` fallbackem na plný `--kk-surface-1`); elevace se čte z **úrovně povrchu + vlásková
  linka**, ne z těžkého stínu
  (`--kk-shadow-0..3` jsou proto lehké — jen jemné ukotvení + `inset 0 1px 0` horní highlight; `3` je
  výjimka pro zvednutou dlaždici/overlay); **text** `--kk-text`/`--kk-text-muted` (teplá bílá, muted
  nad Superhero baseline kontrastem); **spacing** `--kk-space-1..7` (4px škála), **rádiusy**
  `--kk-radius-sm/md/lg/pill` (jeden souvislý roh, 8/12/16 rytmus; `md` je kanonický), **motion**
  `--kk-duration-fast/base/slow` + `--kk-ease-standard`, **focus ring** `--kk-focus-ring-*` (barva =
  azurový akcent, jeden viditelný prstenec všude), **typografie** modulární škála (~1.2–1.25 krok)
  `--kk-font-size-display`/`-page-title`/`-section-title`/`-body`/`-caption` + `--kk-line-height-*`/
  `--kk-tracking-*`.
  Sémantické třídy: **typografická škála** `.kk-display` (největší krok — hero číslo/statistika),
  `.kk-page-title` (jedna na route, na `<h1>`), `.kk-section-title` (nadpis panelu/sekce,
  `<h2>`/`<h3>`), `.kk-text-body`, `.kk-text-caption`, `.kk-text-eyebrow` — komponenty **nenastavují
  vlastní `font-size`** (žádné `h3`/`h5`/`fs-5` utility na nadpisech, žádné inline `fontSize`);
  **povrchy** `.card` (elevace přes raised výplň + vlásková linka `--bs-card-border-color:
  var(--kk-surface-border)` a `--kk-shadow-1`; `.border-primary` apod. stále fungují) a `.kk-surface`
  (raised + linka); **dlaždice** `.kk-tile` + `.kk-tile__media` (bez okraje — fotka má vlastní hranu,
  elevace,
  hover/focus lift na `--kk-shadow-3` — používají `AlbumTile`, `SubjectTile`, `PhotoTile`;
  `:focus-within` pokrývá `PhotoTile`, kde je fokusovatelný až vnitřní odkaz).
  **Hero-first foto zeď**: dlaždice **uvnitř `.kukatko-photo-grid`** (jen ty — album/label/people
  tiles si nechávají kartu) shazují stín i lift a rádius zmenší na `--kk-radius-tile` (2px); hover
  místo liftu **přiblíží obrázek** (`scale(1.05)` v `overflow:hidden`, bez layout-shiftu), spodní
  `.kk-tile__caption` odkryje datum nad scrimem `--kk-tile-scrim`, a fokus-ring se kreslí **dovnitř**
  (`outline-offset` záporný), aby na husté zdi nepřetekl přes hairline mezeru k sousedům.
  A `.kk-tile-row`
  (řádková varianta pro seznam štítků — místo liftu se zvýrazní pozadím, protože řádek v sloupci
  nemá kam vyskočit); `.kk-tile__placeholder`; **chip** `.kk-chip` (odebíratelný token nad
  Bootstrap `.badge` — jen to, co badge nemá: box kolem koncového `.btn-close` a strop šířky,
  aby se dlouhý název alba zkrátil místo roztažení řádku; používá `MultiSelect`);
  **barvy entit** — album/tag/osoba dostávají každý svůj odstín, aby se rozlišily na první pohled
  (dřív byly album i štítek stejná primární oranžová = nešly rozeznat). Tokeny
  `--kk-entity-album-bg` (fialová) / `--kk-entity-tag-bg` (tyrkysová) / `--kk-entity-person-bg`
  (růžová) + `--kk-entity-fg` (bílá); modifikátory `.kk-entity-album/-tag/-person` na `.badge`
  (barva má `!important`, aby přebila Bootstrap `.bg-*`/`.text-bg-*`, které jsou taky `!important`,
  takže třída sedí na plain `.badge` i na `<Badge>` i na odkaz-pill). Mapování kind→třída+ikona je
  **jednou** v `components/entityStyle.ts` (`ENTITY_STYLE`) a čte ho každé místo, kde se entita
  zobrazí jako chip: aktivní filtr-chipy knihovny (`FilterBar`), organize panel fota
  (`OrganizePanel`), pruh badgí nad fotkou (`OrganizeBadges`) a `GlobalSearchSections` — barevný
  jazyk je tak konzistentní, ne jednorázový.
  Barva je **jen doplněk**: chip vždy nese i textový popisek a vodicí ikonu (album `collection` /
  tag `tags` / osoba `person-circle`), aby rozlišení přežilo pro barvoslepé; bílý text má na
  near-black pozadí kontrast ≥ 5:1. Neutrální filtry (rok, hodnocení, flag…) zůstávají `text-bg-primary`;
  **appear** `.kk-appear` (jednorázový fade-up).
  **Motion tokeny:** tři durations `--kk-duration-fast/base/slow` (120/200/320 ms) + jedna křivka
  `--kk-ease-standard` (decelerate) nesou všechny hover/focus/open-close mikrointerakce; ruční `ms`
  hodnoty rozházené po komponentách jsou svedené na ně (`PhotoTile`, `TrashCard`, `LivePhoto`,
  `CompareStage`, `PhotoDetailPage` still-zoom, `review.css` progress). Načítání obrázků a skeletonů
  má dvě sdílené třídy: **`.kk-media-img`** (fade + `scale(0.98)` dosednutí po dekódování; sdílí
  `transform` přechod s hover zoomem knihovní zdi, který má vyšší specificitu) a **`.kk-skeleton`**
  (shimmer lesk přejíždějící warm surface-1 blok, perioda `--kk-duration-skeleton` = 1400 ms,
  `linear infinite`). **Focus outline se nikdy neodstraňuje** —
  `.kk-tile:focus-visible`/`.kk-tile__media:focus-visible` kreslí `outline` (přežije `overflow:
  hidden` náhledu). **`prefers-reduced-motion`**: token durations spadnou na `1ms`, takže lift
  (`transform`), `.kk-appear` i `.kk-media-img` prolnutí se stanou okamžité; skeleton shimmer
  (`--kk-duration-skeleton` do kolapsu nepatří) se místo toho vypne přímo a zůstane statický blok;
  spinnery a progress bary animují dál, protože nesou význam),
  `styles/app.css` (**global responzivní polish vrstva** importovaná v `main.tsx` hned za
  `tokens.css` — jen cross-cutting mobil/touch věci, které Bootstrap utility neumí: **safe-area
  insety** přes `env(safe-area-inset-*)` (fungují díky `viewport-fit=cover` v `index.html`) na
  navbaru (`.kukatko-navbar`) a hlavním kontejneru (`.kukatko-main`); guard proti vodorovnému
  scrollu/overscroll bounce (`body overflow-x:hidden`, `html overscroll-behavior-y:none`); sdílený
  **sticky-toolbar offset** `.kukatko-sticky-toolbar` (`top: navbar height + safe-area-inset-top`,
  z-index pod navbarem — in-page sticky bary jako `SelectionBar` dosednou pod navbar, ne pod něj);
  **min. tap-target** `.kukatko-tap-target` (2.75rem/44px) pro icon-only ovládání jako
  `FavoriteButton`; **app-wide touch-target floor** — `@media (pointer: coarse)` blok, který na
  dotykových zařízeních (telefon/tablet) vynutí min. 44px na `.btn`/`.form-control`/`.form-select`/
  `.nav-link`/`.dropdown-item`/`.list-group-item-action`/`.page-link` + větší `.form-check-input`,
  bez zásahu do desktop (fine-pointer) layoutu a bez per-komponentových změn (systémová oprava
  všudypřítomných `size="sm"` ovládání);
  **native form chrome** — Superhero peče `.form-control`/`.form-select` bíle (`#fff`) bez ohledu na
  téma; místo připnutí na světlé schéma jim dáváme reálný tmavý povrch `--kk-surface-sunken` s
  vláskovou linkou a `color-scheme: dark` (výplň i schéma souhlasí, takže nativní glyfy — kalendář
  `type=date`, list selectu — jsou světlé-na-tmavém a viditelné); chevron selectu je světle tažená
  kopie přes `--bs-form-select-bg-img`; **akcent na bake-nutých ovládáních** — Bootswatch peče
  oranžovou výplň napřímo (ne přes `--bs-primary`), tak ji `app.css` přepisuje na azuru:
  `.btn-primary`/`.btn-outline-primary`, `.form-check-input:checked`/indeterminate, `.form-range`
  thumb, `.progress-bar` (+ track jako jáma), `.dropdown-menu` (warm overlay + aktivní položka),
  `.list-group` aktivní řádek a `.navbar.kukatko-navbar` aktivní odkaz;
  **slim průsvitný navbar** `.kukatko-navbar` (sedí NAD scrollujícím obsahem: výplň `--kk-header-bg`
  = tón stránky na 72 % + `backdrop-filter: blur(--kk-header-blur)` frostí, co pod ním scrolluje,
  vlásková spodní linka `--kk-header-border`; `@supports not (backdrop-filter…)` fallback na plný
  `--kk-surface-1`, aby lišta nikdy nebyla průhledná bez blur; ztenčené `padding-block`, na fine
  pointeru se `.nav-link` tap-target uvolní na 2.25rem — proto je `--kukatko-navbar-height` 3.25rem,
  dimenzované na vyšší touch případ); **klidnější nav** — neaktivní `.nav-link` tlumené, aktivní
  route nese jeden akcentový stav = pilulka `--kk-accent-subtle` + akcentový text (mimo CTA);
  **global command paleta** `.kukatko-search-trigger` (pole-jako spouštěč vedoucí bar, na fine
  pointeru slim, na coarse 44px, na mobilu roste) + `.kukatko-search-dialog`/`-panel` (top-anchored
  konzole na `--kk-surface-overlay`) + `.kukatko-search-option` (řádek: náhled/glyf + text + počet,
  aktivní řádek `--kk-accent-subtle` + inset akcentová lišta) + `.kukatko-search-legend` (patičková
  legenda kláves, na telefonu skrytá) — podklad komponenta `SearchCommand`;
  **časová osa** `.kukatko-timeline*` (fixní svislá datová lišta u pravého
  okraje pod navbarem, absolutně umístěné ticky, floating popisek aktivního měsíce, `touch-action:
  none` pro tažení, na šířkách ≤ 575.98px skrytá); **filtr-bar** `.kukatko-filter-*`
  (`.kukatko-filter-search` = search pole roste a plní řádek hlavičky, `.kukatko-filter-sort`
  min. šířka, `.kukatko-filter-panel` = 44px tap-targety na prvcích panelu, `.kukatko-filter-chip`
  = tappable pill chip s křížkem); CSS proměnná `--kukatko-navbar-height`),
  `test/setup.ts` (jsdom **`window.matchMedia` stub** — non-matching default, jednotlivé testy ho
  můžou přepsat pro simulaci telefonu).
  Routing v `App.tsx`: tabulka rout žije v exportované `AppRoutes` (aby ji šlo v testech mountnout
  do `MemoryRouter` a ověřit samotné drátování — `App.test.tsx`), `App` ji jen obalí
  `BrowserRouter`+`AuthProvider`+`CapabilitiesProvider` (kapabilit-provider je uvnitř auth-provideru,
  protože `/capabilities` je za `RequireAuth`). `/login` veřejné, zbytek pod `RequireAuth`; `/slideshow` a
  immersivní `/photos/:uid` jsou pod `RequireAuth` ale **mimo `Layout`** (fullscreen bez navbaru),
  zbytek pod `Layout`
  (**`/` = `LibraryPage`** — knihovna je úvodní stránka; `/library` → `LibraryRedirect`
  (`replace` redirect na `/` se zachovaným query stringem),
  `/favorites`, `/albums`, `/albums/:uid`, `/labels`, `/labels/:uid`, `/search`, `/saved`, `/map`,
  `/places`, `/people`,
  `/people/:uid`, `/account`, `/help`; `/upload`, `/people/clusters`, `/faces`, `/recognition`, `/trash` a
  `/duplicates` navíc pod `RequireRole role="editor"` = write-only (a `/duplicates/compare` tamtéž,
  ale **mimo `Layout`** — fullscreen jako `/review`), `/import` pod `RequireImport` (= maintainer,
  `canImport`), `/maintenance` a `/system` pod `RequireRole role="maintainer"` = provoz (jen
  maintainer), `/users` a `/audit` pod `RequireRole role="admin"` = governance (admin **nebo**
  maintainer)). Konfig:
  `vite.config.ts` (build → `../internal/web/static/dist`, vitest jsdom, dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed), `.prettierrc.json`,
  `tsconfig*.json`.
