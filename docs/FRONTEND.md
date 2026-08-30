# Frontend

A descriptive reference overview of the frontend (`web/`). **These are not rules** — the rules
live in [`CLAUDE.md`](../CLAUDE.md). Record any new component, hook, page, or service
here.

<!-- BODY BEGIN -->
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` with `components/`
  (`Layout` = navbar shell with a user menu (Můj účet, **Statistiky** `/stats`, **Nápověda** `/help`,
  **the build version**, the **„Správa" section**, Odhlásit se; the version is a `NavDropdown.ItemText` with `kk-menu-version`
  — muted caption-sized text closing the account block, deliberately **not** an `Item`: it takes no click and no
  focus while arrowing through the menu. Its value is `formatVersion(useCapabilities().version)`, so it
  costs no request when the menu opens and simply isn't rendered until (or unless) the capabilities call
  answers; the commit belongs to `/help`, the menu is too narrow for it. **„Správa"** is a `NavDropdown.Divider`
  + a `NavDropdown.Header` (`kk-menu-section` — the drawer's uppercase caption label, so both widths name
  the block the same way) + the `adminItems({isAdmin, isMaintainer})` entries, rendered by `renderMenuItem`
  like every other entry of the menu; it sits **between the account block and Odhlásit se**, and the whole
  block — divider and heading included — is skipped when the list comes back empty) + role-gated
  nav with a **visible hierarchy based on
  how often an ordinary person uses an item**: the everyday loop (browsing, sorting, adding photos) is
  loud and immediate, while admin/power-user tooling is present but quieter. It leads with **Knihovna** `/` (= the home
  page; `NavLink` has `end`, otherwise it would light up on every route), **Alba** `/albums`, **Štítky**
  `/labels` and **Hledání** `/search` (always visible top-level, the `PRIMARY_ITEMS` registry — search is a
  *labelled* destination since 2026-08-07, because the magnifier circle below carries no text and states its
  `/`-and-Ctrl+K chords only in a `title` a phone never hovers, which left the app's strongest feature with no
  trace in any menu; the circle stays as the shortcut, see `UX_RESEARCH.md` **N3**); the remaining browse targets are gathered by the
  **Procházet** dropdown (`nav.browse`, `BROWSE_GROUP`): **Oblíbené** `/favorites`, **Uložená hledání**
  `/saved` (smart albums, so they sit next to the favourites instead of a dropdown *inside* `/search`),
  **Lidé** `/people`, **Místa** `/places`, **Mapa** `/map`, and last **Žebříček** `/leaderboard` (`trophy`
  icon, **no role gate** — the competitive standing is just an aggregate of counts, so **every logged-in
  user** sees it, even a viewer). The leaderboard used to hold a top-level slot beside Třídění; on the live
  instance it has one player and 38 answers in total, which does not buy a place next to Knihovna and Alba.
  **Třídění** `/review` (`REVIEW_ITEM`, gated on `canWrite`) does stay
  top-level, not under „Nástroje" — tidying the library one question at a time is the most-used curatorial
  loop, and a game nobody finds is a game nobody plays; **Nahrát** `/upload` (gated on
  `canWrite`) is the bar's **single call-to-action** — a filled pill (`kukatko-nav-cta`, prop `cta`
  in `renderLink`) so adding photos stands out. After it a **divider** (`kukatko-nav-divider` — a vertical
  hairline in the inline bar ≥ md, horizontal in the collapsed burger menu; drawn only when a role
  actually has something behind it) separates the quieter power-user cluster — since 2026-08-14 that is the
  editor dropdown **Nástroje** (`nav.tools`,
  `TOOLS_GROUP`, entirely gated on `canWrite`) and nothing else: it leads with **Rozšířit** `/expand` (a power-user tool that used to
  shout top-level next to albums/labels) + **Najít osobu** `/faces` + **Rozpoznávání** `/recognition` +
  **Možné chyby** `/outliers` + **Duplikáty** `/duplicates` + **Koš** `/trash`. **Administration is no longer
  in the bar at all**: the two dropdowns that used to follow Nástroje — **Provoz** (`nav.operations`,
  `OPERATIONS_GROUP`, `isMaintainer`: import, maintenance, system) and **Správa** (`nav.admin`,
  `GOVERNANCE_GROUP`, `isAdmin`: users, audit) — are merged into **one „Správa" section of the user menu**
  (see `ADMIN_ITEMS` below and the user dropdown further down). They belong to the signed-in identity
  rather than to browsing the library, and the two toggles were spending the row's scarcest resource:
  with them a maintainer's inline bar overran its container below 1400px, which is what the merge fixes.
  **Measured** (Chromium over the real `Layout`, Czech labels, maintainer = the widest role; the two
  toggles reconstructed in place for the before-shot): „Provoz" is **109px** and „Správa" **108px**, so the
  inline row drops from **1003px to 786px — 217px narrower**. At **1280px** (container 1140px) the collapse
  needed 1171px before, ran 31px past the container and scrolled the document 21px sideways; after it is
  1068px and nothing overflows. At **1400px** (container 1320px) the bar fit before and fits after — with
  those same 217px more room to spare. The role model is a strict ladder
  `viewer < editor < admin < maintainer` (see `services/auth.ts` below).
  **The bar carries no brand — no mark, no wordmark, no logo home link.** It used to open with one
  (`Navbar.Brand` + a `binoculars-fill` mark + the „Kukátko" wordmark), but width is this row's scarce
  resource: the wordmark only stayed alive by juggling four display utilities (`d-none d-sm-inline
  d-md-none d-xxl-inline`) because it fit nowhere, and the inline bar ran past its container for an editor
  and a maintainer well into desktop widths. Dropping it — with the `kukatko-navbar-brand*` rules and the
  `nav.brand` / `app.name` / `nav.titles.home` keys — buys the whole leading block back. Measured in a
  static before/after harness (real bootswatch + tokens + `app.css`, the actually rendered bar, Czech
  labels), the compact search below plus the dropped brand cut the overflow past the viewport edge by
  **264px at every desktop width**: an **editor** bar stops scrolling horizontally from ~1160px instead of
  ~1420px, a **maintainer** one from ~1290px instead of ~1890px (at 1200px: editor +135px → fits,
  maintainer +352px → +88px; at 1400px: +14px / +231px → both fit). Below `md` nothing overflowed before or
  after, since the collapse is not rendered there. **The way home does not depend on a logo:** on `md`+ it
  is the bar's first item **Knihovna** `/` (labelled, `end`-matched, `title` „Zobrazit knihovnu fotek"),
  below `md` it is the leading tab of `MobileTabBar`, permanently under the thumb — one tap either way,
  exactly as the mark was. The **hamburger closes the phone row** `[search] [hamburger]` instead of opening
  it (last in the DOM); it is `display: none` on `md`+, so the desktop bar is unmoved by that.
  **The bar leads with global search** `SearchCommand` (`components/search/`) — since the same change a
  **compact icon button** (a 2.25rem magnifier, 2.75rem on `pointer: coarse`, guarded by
  `styles/tapTargets.test.ts`), not the old 16rem field-shaped pill: the control never takes a keystroke, it
  only opens a dialog, so it does not need to look like a place to type. It keeps the sunken well and the
  hairline so it still reads as a control among the quiet nav links, and stays **outside the collapse** (on
  mobile it survives the nav folding into the burger). Below `md` `margin-inline-start: auto` pairs it with
  the hamburger at the trailing edge rather than leaving it alone on the left of the row the brand used to
  open; from `md` the margin flips and it leads the bar ahead of the nav. Nothing on it is visible text, so
  its name and its shortcut are stated outright: `aria-label` (`searchCommand.open`), `aria-keyshortcuts`
  and a `title` tooltip (`searchCommand.triggerTitle` = „Hledat v celé knihovně (klávesa / nebo Ctrl+K)")
  which is what replaces the keycap the field used to draw — both chords are listed in the
  keyboard-shortcuts overlay sitting in the same bar too. It opens a **command palette** reachable from
  anywhere via `/` or Cmd/Ctrl-K (it doesn't steal typing — see `SearchCommand` below). The old full
  `/search` page and saved searches remain, and **both are back in the menus** (top-level **Hledání**,
  „Procházet" → **Uložená hledání**) — the circle is the shortcut, not the only door; what the navbar does
  not bring back is the library's filter field.
  **Overflow, measured again for that swap** (Chromium over the real `Layout`, Czech labels, maintainer =
  the widest role): „Hledání" is **95px** against „Žebříček"'s 102px, so the inline row came out **7px
  narrower** — at a 1200/1280px viewport the container is overrun by 111px instead of 118px, and from 1400px
  (container 1320px) it fits, before and after alike. An editor's row fits at 1200px either way.
  Every item and every dropdown toggle carries an **icon** (`Icon`) and a **`title` describing the action**, not
  the noun („Zobrazit alba", not „Alba"; keys `nav.titles.*`); icons are decorative
  (`aria-hidden`) beside the visible text label. A dropdown is hidden entirely when the user has
  all of its items hidden (Tools for a viewer); the parent menu has an **active state** (`active`
  prop) when the current route is one of its children (`pathMatches` also honors a detail sub-path like
  `/albums/{uid}`) — it is built from `Dropdown`+`Dropdown.Toggle as={NavLink}` (not `NavDropdown`, which
  consumes the `title` prop for the toggle's content, leaving none for the tooltip). **The whole item registry
  lives in `components/navItems.ts`** (`NavEntry`/`NavGroup`, `PRIMARY_ITEMS`, `BROWSE_GROUP`, `TOOLS_GROUP`,
  `ADMIN_ITEMS`/`adminItems()`, `REVIEW_ITEM`, `UPLOAD_ITEM`, `ACCOUNT_ITEM`,
  `STATS_ITEM`, `HELP_ITEM`, `pathMatches`), so the bar and the phone drawer below read **the same list with the same role
  gates** and cannot drift apart. The administration entries are the one registry that is **not** a
  `NavGroup`: `ADMIN_ITEMS` is a flat list of `AdminEntry` (a `NavEntry` + a `gate` of `'maintainer'` or
  `'admin'`) in menu order — Import, Údržba, Systém, Uživatelé, Audit, Nastavení — and `adminItems({isAdmin,
  isMaintainer})` returns just the ones a role clears. **The gate is per item, never on the section**: a
  maintainer gets all six, an admin only Uživatelé + Audit + Nastavení, an editor/viewer an empty list and therefore
  no section at all (no orphan heading, no stray divider). One entry is **built rather than declared**: `myPhotosItem(subjectUid)`
  ("Moje fotky") returns `null` for an account that has not said which person of the library it is, and
  otherwise a `NavEntry` pointing at that person's scoped grid (`/?person=<uid>`). It is offered **only**
  when the link is set — an entry that leads to a grid of nobody is worse than no entry — and it lives in
  the **user menu** (beside `ACCOUNT_ITEM`, and in the drawer's "Můj účet" section) rather than in the bar:
  the inline row is the app's scarcest space and "mine" belongs with the rest of what is mine. On a phone the `Navbar` is **controlled**
  (an `expanded` state + `onToggle`); a `useEffect` on the `useLocation` pathname resets it to closed on
  **every navigation**, so tapping any item — top-level link, group-dropdown item, or user-menu item —
  auto-closes the burger instead of leaving it open over the page. Logout closes it explicitly (a handler,
  not a route change). This replaces react-bootstrap `collapseOnSelect`, which never fired for the bar's
  bare `NavLink`s and raw `Dropdown.Item`s; on `md`+ the collapse is always shown, so the state is inert there.
  Below `md` the `Navbar.Collapse` is **not rendered at all** — `useIsNarrowViewport` swaps it for the
  `MobileNavDrawer`, so a phone never carries two copies of the nav links (and a resize past the breakpoint
  resets `expanded`, or the desktop bar would come back with an invisible open menu behind it),
  `MobileNavDrawer` (**the phone menu, as a real drawer** — `components/MobileNavDrawer.tsx`, rendered by
  `Layout` only below the navbar's `md` breakpoint and opened by the hamburger, whose `aria-controls` points at
  the shared `MOBILE_MENU_ID` = `main-navbar` that the desktop collapse also uses. It replaces the old inline
  collapse: react-bootstrap disables Popper inside a `Navbar`, so every group dropdown used to expand *into*
  the bar — one long nested stack with no grouping, no headings and no room to aim a thumb. It is a
  react-bootstrap `Offcanvas` (`placement="end"`), so sliding, the backdrop (tap outside = dismiss), Escape,
  the focus trap and the body scroll-lock come from Bootstrap; the header adds a labelled close button
  (`nav.closeMenu`) and the title `nav.menu`. The body is **labelled sections**, each a `<section>` +
  `<h2>` heading (so it is a named `region` for assistive tech and for tests): **Hlavní**
  (`nav.sections.main` — Knihovna/Alba/Štítky/Hledání + Třídění and Nahrát when `canWrite`,
  the last keeping the bar's filled CTA look), **Procházet**, the `canWrite` **Nástroje**,
  and **Účet** (`nav.sections.account` — Můj účet, Nápověda, the
  keyboard-shortcuts overlay, **the build version**, the **„Správa" group** and Odhlásit se, i.e. the user dropdown unfolded; the
  version is a plain `<p class="kk-navdrawer__version">` above them — no row, no tap target,
  same `formatVersion(useCapabilities().version)` value the bar shows). **„Správa" is a nested
  `<section>` + `<h3>` inside Účet** (`kk-navdrawer__subsection`, fenced by a hairline above *and* below so
  sign-out stays out of it), holding the same `adminItems()` rows in the same place the desktop user menu
  holds them — between the account rows and sign-out. Promoting it to a top-level section would have put it
  *after* sign-out and taught two different menus at the two widths; the merge of the former Provoz +
  Správa sections happened on 2026-08-14. A closed role gate drops the
  whole section (and an empty `adminItems()` the whole group), exactly as it drops the dropdown in the bar. Rows are 3rem (48px) tap targets with the icon +
  label + `nav.titles.*` tooltip, the same accent-tinted „you are here" pill as the bar and the tab bar
  (`NavLink`, `end`-matched for the library root), and each row also closes the drawer `onClick` — the
  pathname effect covers navigation, the handler additionally covers re-tapping the route you are already on.
  Styles are `.kk-navdrawer*` in `app.css`: `--bs-offcanvas-width: min(20rem, 86vw)` (a strip of backdrop is
  always left to dismiss by), `env(safe-area-inset-{top,right,bottom})` padding on the panel (the left edge
  faces the middle of the screen, so it is deliberately not inset), a hairline + heading between sections, and
  `overscroll-behavior: contain` on the scrolling body),
  `MobileTabBar` (**the phone-only bottom tab bar**, rendered by `Layout` after the `Footer`: on a phone the
  whole primary nav is folded into the burger, so every everyday destination costs an open-then-tap — this
  pins them to the bottom edge where the thumb already is. Four tabs at most (`TABS`), the everyday loop only:
  **Knihovna** `/` (`end`-matched, otherwise the root's prefix match lights it up everywhere), **Alba** `/albums`,
  **Hledat** `/search` (`nav.searchShort` — the imperative, like the „Nahrát" beside it, where the bar and the
  drawer use the page's own noun „Hledání"; it took the slot **Štítky** `/labels` used to hold, which keeps its
  row in the drawer: on a phone searching had no entry at all, while browsing by label is the rarer errand),
  **Nahrát** `/upload` (gated on `canWrite` — a viewer gets three). Browse / Třídění /
  Nástroje / Správa deliberately stay in the burger menu: the bar earns its permanent strip only by
  being short enough to hit blind. Each tab is a `NavLink` with a decorative `Icon` above a short label plus the
  same `nav.titles.*` action tooltip as the navbar, an `active` accent-tinted pill matching the top bar's
  „you are here", and a 2.75rem (44px) touch target; the landmark is labelled `nav.tabBar` (cs/en).
  **Shown only below the navbar's `md` expand breakpoint**, and the decision is made in JS
  (`useIsNarrowViewport`) — it renders `null` on `md`+ rather than hiding via `d-md-none`, so the desktop DOM
  carries no duplicate set of nav links (the class stays only as a guard for the frame between a resize and
  the re-render). It publishes its live rendered height (safe-area padding included) into `--kk-tabbar-height`
  on the document root via a `ResizeObserver`, mirroring `BatchActionBar`'s `--kk-batch-bar-height`; that one
  variable is what keeps everything else off it — `body`'s `padding-bottom` reserves the scroll clearance for
  every page at once, `--kk-bottom-edge` (`max(env(safe-area-inset-bottom), --kk-tabbar-height)`) lifts the
  floating `.kk-batch-dock` and `--kk-batch-clearance` so a selection **stacks above** the tabs instead of
  colliding with them, and `.kukatko-timeline`'s `bottom` stops the scrubber rail short of them. The variable
  is removed on unmount and at the desktop breakpoint, so all of that collapses to `0px` when there is no bar),
  `Footer` (**global footer** below `<main>` on every page in `Layout` — the fullscreen
  `/slideshow` and the immersive `/photos/:uid` run outside the shell, so they don't have it: „Provozuje SDH Veselice“ + a link to the source code
  <https://github.com/panbotka/kukatko> in a new tab with `rel="noopener noreferrer"` and a decorative
  `github` icon (`aria-hidden`); texts `footer.*` (cs/en). It renders in normal flow — on a
  short page it simply follows the content, overlapping and floating nothing. Inside is a space-between
  flex row: operator + GitHub on the left, `children` fills the right side (today the admin job-queue
  badge); `.kukatko-footer` shares safe-area padding with `.kukatko-main` — which carries
  `pt-3 pt-md-4 pb-4`, one step less top padding on a phone, where that gap comes straight out of the
  first screen. Both live inside one **`.kukatko-page`** column (everything `Layout` renders in normal
  flow; the navbar and the tab bar stay outside it), which is what the timeline rail's lane is cut from —
  see `TimelineScrubber` below),
  `JobQueueBadges` (right side of the footer: a compact badge with the job-queue state **for maintainers only**
  — the `/jobs` endpoint is a maintainer-only operational capability; via `useAuth().isMaintainer` +
  `useJobStats` — a non-maintainer renders nothing and **makes no request**.
  One badge per non-empty `queued`/`running`/`failed`/`dead` state from `by_state` (the terminal `done`
  is deliberately omitted), `failed`/`dead` carry `bg="danger"` so they catch the eye; when everything is zero,
  a single quiet `idle` badge. A failed request silently hides the badge — the footer never breaks; texts
  `footer.jobs.*` (cs/en).
  The whole row is **one `<Link to="/system">`** — the queue is a panel of the system-status page, the
  frontend has no `/jobs` route — so „ve frontě 16“ leads where it can be acted on instead of being a
  dead number. One link for the row, not one per badge: a single destination earns a single tab stop, and
  a real anchor is tab-reachable and Enter-activatable without any key handling. `text-decoration-none`
  keeps the badges looking exactly as they did (a badge sets its own colour; the underline was the only
  thing the anchor added). The link carries an explicit `aria-label` = `title` (`footer.jobs.link`,
  e.g. „Fronta úloh: ve frontě 16, běží 1 — otevřít Stav systému“) so its accessible name names the queue,
  its state **and** the destination rather than announcing bare counts. The audience matches the target:
  the badges render for `isMaintainer`, which is the very predicate `/system`'s `RequireRole role="maintainer"`
  guard uses, so nobody is offered a link to a page that would answer them with `ForbiddenPage`),
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
  `WhatsNewPanel` (`components/library/`, **"what's new since your last visit"** — the digest above the
  library grid, rendered by `LibraryPage` **before `FilterBar`** because it is about the library as a whole,
  not about the current view. A shared family library otherwise makes somebody else's evening of uploading
  invisible to the next person who opens the app; this is the one place that says so, and it is deliberately
  four lines rather than a second timeline competing with the real one. Via `useWhatsNew` + a dismissible
  `<Alert variant="info">` with a `clock-history` `Icon`, a `whatsNew.since` sub-line through
  `formatDateTimeMinutes`, then one `<li>` per non-zero group: **`whatsNew.photos` links to `/?sort=added`**
  (recently added, not the capture-time timeline — a scan of 1962 negatives is *new* but not *recent*),
  a **„N nových fotek, na kterých jsi ty"** line follows it when `mine_photos > 0` *and* the reader's
  account names a person (`useAuth().user.subject_uid`), linking to that person's grid in the same
  recently-added order (`/?person=<uid>&sort=added`); zero, or an unlinked account, renders **no line at
  all** — an empty "0 new photos of you" is noise, and for most readers this line simply never exists;
  albums link to `/albums/{uid}` and people to `/people/{uid}` via the shared `DigestLine`, which after the
  server's 6 links appends a plain **non-linked** `whatsNew.more` tail (no page lists "albums created since
  Tuesday"); comments are a count, not a link. **Dismiss is keyed on the digest's `since`** in localStorage
  (`lib/whatsNewDismissal.ts`: `readDismissedWhatsNew`/`writeDismissedWhatsNew`, mirrors
  `announcementDismissal.ts`) — `since` is constant for the length of a visit, so closing the panel closes it
  for **this** visit through every reload and every walk around the app, and the next visit's fresh `since`
  brings it back. Loading / `has_news:false` / already dismissed → renders nothing; **every role sees it**,
  viewers included. Texts `whatsNew.*` (cs/en, with the Czech plural categories `_one/_few/_many/_other`
  so 1 fotka / 2 fotky / 5 fotek all read correctly). Tests: `WhatsNewPanel.test.tsx`),
  `WelcomeModal` (`components/welcome/`, **the first-run welcome, shown once**: mounted by `Layout` after
  `MobileTabBar`, so it lies over whatever page the reader landed on and every routed screen inside the shell
  stays where it is behind it (the immersive routes outside `Layout` never get it). Two steps in one
  `<Modal show centered scrollable fullscreen="sm-down">` — full-bleed on a phone because the welcome is a page,
  not an aside — with `aria-labelledby` pointing at its `Modal.Title`. **Step one** (`WelcomeTextStep`) prints
  the administrator's greeting through the shared `Markdown` component, the very renderer that draws the
  live preview in `SettingsPage`, so preview and reality cannot drift; the wrapper carries `text-break` so a
  pasted URL cannot push the dialog sideways. **Step two** (`WelcomePersonStep`) asks who the reader is: a
  `type="search"` field **at the top of the body** (on a phone the keyboard covers the bottom half, and a search
  box under it cannot be read while it is typed into) filtering `useSubjects()` through `foldedIncludes`, then up
  to `MAX_CANDIDATES` = 6 rows, each a `Button` wrapping a `SubjectSummary` — **name *and* face**, because a
  family archive is full of namesakes and a face settles it faster than a spelling does. Rows are ordered
  **most-photographed first**, which is what makes an empty query a useful shortlist rather than the alphabet;
  the marked row carries `aria-pressed`. **Nothing is written until Confirm**, which calls `setMySubject(uid)`
  then `refresh()` (the menu entry, `person:me` and the comment avatar all read the link off the session).
  An account that **already names a person** is not asked again: it is told who it is linked to
  (`welcome.person.linkedTo`, or `unknownPerson` when the link outlived its subject) and offered
  „Vybrat jinou osobu", which reveals the same picker one click later. **Gating**: a `Phase`
  (`idle`→`loading`→`open`|`done`) latched in state — `done` is final, so the `refresh()` a successful link
  triggers cannot reopen it. It renders nothing and **asks the backend nothing** for an account whose
  `welcome_seen_at` is set (almost every page load); otherwise it fetches `fetchWelcomeMarkdown()` **before** it
  dares appear, an **empty/whitespace greeting opens straight on step two** (an empty first page only teaches
  the reader to click through without reading), and a **failed fetch means the welcome quietly does not happen
  this time** — nothing is recorded, so the next sign-in tries again: the greeting is postponed, not lost.
  However it ends — confirmed, skipped, or closed with the X — `finish()` closes first and fires
  `markWelcomeSeen()` **fire-and-forget with the rejection swallowed**: a request the reader cannot see failing
  is not a reason to keep them in a dialog, and the backend stamp is idempotent. Texts `welcome.*` (cs/en).
  Tests: `WelcomeModal.test.tsx`),
  `JobStateLegend` (**shared legend of job-queue states**: a compact `dl` with a bold term + a quiet
  one-sentence explanation of each state, so an admin understands without hovering; both the labels and the explanations come from a
  shared i18n block `jobStates.labels.*`/`jobStates.descriptions.*`, so the wording is identical on
  `MaintenancePage` and `SystemStatusPage`; the `states` prop controls order and selection — Maintenance omits
  `pending` and `done`, System adds both (its queue table has a `done` column, because without it a row's
  states would not add up to its lifetime total). Tests: `JobStateLegend.test.tsx`),
  `components/system/` = **the admin dashboard's three sections**, all rendered from the single
  `GET /system/status` snapshot the page already polls (no second fetch, no arithmetic of their own):
  `StatTile`/`StatTileGrid` (**the dashboard's number tile** — a big `kk-display` value over its label, 2 per
  row on a phone up to 5 on a wide screen; a tile that has a view behind it makes the **whole card** the click
  target (`stretched-link` over the label, so the accessible name is the label and not "16 585") and a tile
  with nowhere to go stays plain text rather than offering a link that lands somewhere unrelated; a `gap` tile
  is `text-warning` **only while non-zero**, because a cleared backlog is the goal, not a warning),
  `LibraryOverview` (**what is in the library**: ten tiles — fotky, z toho videa, v koši, skryté, soukromé,
  alba, štítky, osoby, obličeje, embeddingy, the first five linking to `/`, `/?q=type%3Avideo`, `/trash`,
  `/?q=hidden%3Ayes`, `/?q=private%3Ayes` — plus two cards: **Nedávno nahráno** (24 h / 7 / 30 / 365 days,
  counted by when a photo *arrived*, which is why the note says so) and **Velikost podle katalogu**
  (knihovna / koš / odvozená data), the one storage figure that is meaningful when the originals live in an
  object store; the header links on to `/stats` for the processing statistics),
  `RemainingWorkPanel` (**what is still to do**: obličeje beze jména → `/review`, shluky → `/people/clusters`,
  bez data, bez souřadnic → `/?q=geo%3Ano`, bez místa, bez rozpoznání textu, duplicitní značky →
  `/duplicate-markers`, skupiny duplicit → `/duplicates`; the duplicates tile is the only one that renders
  **`—` plus "hledá se na pozadí…"** until the backend's background scan has an answer, and "zjištěno <age>"
  once it has — an unavailable scan must not read as "no duplicates"),
  `JobQueuePanel` (**the queue by type × state**: one row per job type with queued/running/failed/dead/done
  columns and its lifetime "celkem kdy" total, rows ordered dead-letter first then busiest then by name so the
  order is stable between two polls; a row with a dead letter carries its **own requeue button**, and the
  whole-dead-letter button sits below with the shared `JobStateLegend`. The lifetime numbers are captioned as
  history — this replaced a badge row where `image_embed: 41 594` against 20 930 photos read as a backlog when
  it was a one-off re-embedding. An empty queue renders a sentence, not an empty table),
  `LibraryStatsCards` (**the shared rendering of the library counts** `GET /system/stats`: six
  `Card`s in a responsive `Row` — photos, vyhledávání podle obsahu, obličeje, lidé a zvířata, značky na
  fotkách, alba a štítky — each with a
  headline number (`kk-display`) over its breakdown (`dl`), every value through `formatCount` for the active
  language; a **coverage gap** row (zbývá zpracovat / bez jména / fotek bez obličeje) turns `text-warning`
  while
  non-zero. The vocabulary is the family's, not the pipeline's: the card that named the embedding now says what
  it buys (**Vyhledávání podle obsahu**, headline = `photos_with_embedding`, row = `photos_without_embedding`).
  **One card, one grain, and the grain adds up.** A face is one detection on one photo, so **Obličeje** leads
  with `faces_assigned` and breaks out `faces_unassigned`; the two make `faces`, which is exactly what the
  faces coverage meter divides by, so card and meter cannot disagree. A marker is a box drawn on a photo, a
  smaller and overlapping set, so `markers` / `markers_assigned` / `markers_unassigned` live on a card of
  their own. Both cards close with an optional `note` (`StatNote` → `StatNoteText`, quiet caption under the
  `dl`, `{{total}}` interpolated through `formatCount`) saying what the numbers add up to and which
  neighbouring count they must not be added to — before this split all three counts wore the word "obličej",
  read as a partition and did not add up (4 671 + 16 586 ≠ 115 461, and the card implied 22 % where the meter
  said 3,8 %).
  **Fotky walks the total down to the count the Knihovna itself reports.** The page said „V knihovně 20 890"
  while the library grid said „Počet fotek: 20 619" and nothing explained the 271: the grid drops the photos
  hidden from the library and the non-primary members of a stack. Both are rows of their own now
  (`photos_hidden`, `photos_stacked`) between **Mimo koš** (`photos_live`) and **Vidíte v Knihovně**
  (`photos_listed`); the three parts are disjoint and add back up to `photos_live`, and the card's `note`
  says why the subtraction happens. Nothing is recomputed in the component — the backend reports all four —
  and `stats.live` reads **Mimo koš** rather than the „V knihovně" that contradicted the grid.
  A count that stands for work to be done, or that a reader may want to check against another screen, is a
  **`Link`** (`text-reset text-decoration-underline`, so it keeps
  the row's colour and gains an underline; `aria-label`/`title` name the destination, since "16 585" names
  nothing): v koši → `/trash`, fotky bez obličeje → the library's own filter `/?q=faces%3A0`, obličeje bez
  jména → `/review` (the review game works over the nameless **detections**, which is why the link hangs off
  `faces_unassigned` and not off a marker count), skrytých → `/?q=hidden%3Ayes` (the documented way back to a
  hidden photo, ungated: hiding is an edit, reading the hidden ones is not) and vidíte v Knihovně → the
  library itself, so the number can be checked where it is disputed. The two write destinations are gated on
  `useAuth().canWrite` and render as plain text
  for a viewer — never a link that would bounce them (N13). The caller still owns loading/errors/retry. It is
  the statistics page's rendering only: `SystemStatusPage` answers a different question ("what is in the
  library?") from the dashboard half of its own snapshot, see `components/system/LibraryOverview`),
  `components/stats/` = **the charts of the statistics dashboard**, plain CSS over the app's tokens
  (`charts.css`), no charting dependency: `ColumnChart` (a column histogram of one measure over a time axis —
  each bar scaled against the chart's tallest with a **2 % floor** so a one-photo year is never drawn as
  empty, an empty bucket drawn as a baseline tick so the axis reads as continuous time, the **whole column**
  as the hit area when the bucket links somewhere, sparse `tick` labels absolutely centred so a decade label
  may overhang its 5px column, and the plot scrolling sideways on a phone instead of shrinking a century into
  hairlines), `BarList` (a horizontal bar list for named things — cameras, media buckets — with the name and
  the value written out beside every bar, which is what keeps identity off the colour; it is also the track
  behind the meters), `CoverageMeters` (the three shares as ARIA `meter` rows over the **counts**, so it needs
  no second request; an untouched whole reads 0 %, never a division by zero. Every row divides two numbers the
  cards already show at the cards' own grain — the faces row is `faces_assigned / faces`, i.e. the faces
  card's headline over the sum of its two face rows), `ChartCard` (the card shell matching
  `LibraryStatsCards`; it takes the height of **its own** content and is never stretched to its row-mate —
  `h-100` beside the ten-row camera list left the shorter panel padded out with hundreds of pixels of nothing),
  `ChartStatement` (**what a card holds when its series has no chart in it**: the figure the chart would have
  been scaled to, on `.kk-display` with proportional figures, over a caption naming the one bucket it fell in;
  plain text, so it carries no `aria-label`) and `LibraryChartsPanel` (the four charts assembled from
  `GET /system/stats/charts`, with every label, tooltip, empty-state sentence and `aria-label` summary from
  `stats.charts.*`). Both primitives take a summary `ariaLabel` carrying the chart's key numbers and render as
  `role="img"`, or as `role="group"` when their marks link somewhere so those links stay reachable),
  `Icon` (**the app's single icon set**: a bootstrap-icons glyph as `<i class="bi bi-{name}">`,
  the font is imported globally in `main.tsx`; the `IconName` union holds the dictionary of used icons, so a typo
  is a compile error; always `aria-hidden` beside a visible label. **Every control whose visible content is
  one of these glyphs and nothing else carries a `title` as well** — a `<Button>`/`<button>`, an icon-only
  `Link`/`<a>`/`Nav.Link`, a `Dropdown.Toggle` — and that `title` is the **same string as the `aria-label`,
  out of the same `t()` call**: the interpolated ones (`{ name }`, `{ decade }`, the timeline's month) and
  the ones that switch on state (play/pause, show/hide faces, archive/restore, enter/exit fullscreen) switch
  in both, bound to a local rather than translated twice. The `aria-label` **stays** — a phone shows no
  tooltip at all, so the accessible name has to keep standing on its own — and a control with visible text
  gets none, since repeating the words already printed on it is noise. Two deliberate exceptions: a natively
  `disabled` button never shows a `title` (Bootstrap's `.btn:disabled` kills its pointer events), which is
  why `ReasonedButton` switches off with `aria-disabled` + `kk-btn-inert` instead; and the viewer's hidden
  toggle keeps its longer `photo.hidden.*Hint` tooltip, which says more than its label does. Swept over all
  of `web/src` on 2026-08-12 — from here the rule rides on review, not on a lint rule),
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
  targets. The suggestion list is **layout-responsive** (via `useIsNarrowViewport`) so a scrollable
  modal never clips it: on desktop it is a **`position: fixed` overlay** measured off the input
  (escaping any `overflow: auto` `.modal-body` — the bulk pickers and `BulkEditModal` both nest it in
  one), sized to its content up to `min(50vh, room-below)` and scrolling only its own options beyond
  that; on a phone it flows **in the modal's own scroll** (`position-static`), keeping the field and
  its options reachable **above the on-screen keyboard**. The desktop overlay also carries
  **`.kk-overlay-menu`**, which lifts it off Bootstrap's `.dropdown-menu` z-index (1000 — under every
  sticky bar the app draws) onto the app's own **layering scale** (the `--kk-*-z` block on `:root` in
  `app.css`: timeline rail 1018 < sticky toolbar 1019 < navbar 1020 < tab bar 1025 < batch dock 1030
  < **overlay menu 1035** < Bootstrap's dialog band 1040+). Without it the sticky bar under `/upload`'s
  album picker (1019 — the progress header of the day, `.kk-upload-rail` now) painted over the open
  album list and swallowed its clicks —
  measured with `elementFromPoint`, the two rows overlapping the header hit the header, not the option.
  The layer deliberately stops **below** 1040, so a dialog opened over the page still covers the menu and
  a menu inside a modal can never outrank the modal backdrop (the modal is its own stacking context in
  any case, which is why the same class changes nothing in `BulkEditModal` or the bulk pickers). Phone
  keeps no layer at all — a `position: static` box does not stack. The `destructive` prop tints the label and chips into the danger key, so a removal never looks
  like an addition. By default it **creates no items** — it only picks from those it receives (mirrors
  `AddAutocomplete` and `SearchableSelect`); with an optional `onCreate(name)` it appends a
  **„Vytvořit «dotaz»“** row to the list, only when a non-empty trimmed query fold-insensitively (case,
  diacritics, edge whitespace) matches **no** option — selected ones included — so it never
  offers a duplicate; Enter with nothing highlighted creates only when nothing else matches. What creating
  means is up to the caller (typically it registers the name and picks the value for it via
  `options`+`selected`); for a reader without write permission, `onCreate` is simply not passed.
  Creating — unlike picking, which keeps the list up so you can take several — **closes the list and
  blurs the field**, which on a phone is what dismisses the on-screen keyboard: the name did not exist,
  so there is nothing left to choose from the list it was typed into. Without that the list stayed up
  over the keyboard with a blank query and the whole thing read as „nothing happened"; the new chip
  below the field is the confirmation),
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
  from `lib/shortcuts.ts` `SHORTCUT_GROUPS`, closes with Escape/the close button. Prop `variant`
  changes only the trigger: `'icon'` (default) is the bar's compact keyboard cap, `'row'` is the
  full-width `.kk-navdrawer__link` row `MobileNavDrawer` puts in its account section — the modal is a
  portal either way, so it stacks above the drawer that opened it),
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
  `EntityChip` (**the shared album/label/person chip**: a pill badge in the entity's `ENTITY_STYLE` hue with its
  leading glyph, linking to that entity's scoped list, plus an optional editor's remove X. Props `kind`, `to`,
  `children` (the name, optionally with a count beside it), `className?` (e.g. `fw-normal`), `remove?` =
  `{label, onRemove}` (the label names what leaves, not merely „odebrat"). **The pill is the link**, not a small
  `<a>` inside a decorative pill: measured on production (390 × 844, coarse pointer) the photo panel's album chip
  was a 79.1 × 12.0px link inside a 111 × 20.9px pill, because the touch floor in `app.css` lists `.btn` and its
  kind and a `.badge` is none of them. The anchor now carries the pill's own classes with the glyph inside it, so
  the floor's `a.badge` rule lifts the **whole chip** to 44px on a coarse pointer (and leaves it compact on a
  mouse). With a remove control the pill cannot *be* the anchor — an `<a>` may not contain a button — so it is a
  span whose link is its direct child and stretches over its full height (`.badge > a`), with the X trimming it at
  the end. One shape for all three call sites: `OrganizePanel` (with the remove for editors), `OrganizeBadges`
  (the read-only strip) and the label hits of `GlobalSearchSections`. Tests: `EntityChip.test.tsx` (the shape the
  floor keys on) + `styles/tapTargets.test.ts` (the rule itself)),
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
  flow through. Optional `skeleton` draws a `Skeleton` **behind** the image until it decodes (absolutely
  positioned, corners inherited — the caller's box has to be `position: relative`, which the tile media
  well is): for a grid whose sources are measured in megapixels, an empty dark square reads as broken
  rather than as loading. It clears on `onError` as well as on `onLoad` — a placeholder that pulses
  forever says „still loading" about an image that has given up — and the caller's own `onError` still
  runs (`OutlierCard` steps down the thumbnail ladder from it). `SubjectTile`/`FaceCrop` ask for it. Replaced the manual `loaded` fade in
  `PhotoTile`/`TrashCard` and added the fade to covers/previews:
  `AlbumTile`, `SubjectTile`, `SubjectPhotoTile`, `SimilarPhotos`, `StackStrip`,
  `DuplicateGroupCard`, `GlobalSearchSections`, `SearchCommand`. Tests: `FadeInImage.test.tsx`),
  `Skeleton` / `TileGridSkeleton` / `ListSkeleton` / `ChipCloudSkeleton` (**shared skeleton placeholders** instead of
  full-page spinners on the main data views: `Skeleton` is a single shimmer block
  (`.kk-skeleton`, warm surface-1 + a sweeping sheen, `aria-hidden`, props size/circle/radius);
  `TileGridSkeleton` is a grid of cards (a square cover + 1–2 caption rows) with the same responsive
  `minmax` as the real grid — `AlbumsPage` (minTile 160, 2 rows) and `PeoplePage` (140, 1 row);
  `ListSkeleton` is a stack of rows (`MyActivityPage`, `ReviewDecisionsPage`, `LeaderboardPage`);
  `ChipCloudSkeleton` is a wrapping row of pills of uneven width (`LabelsPage`) — the widths cycle
  through a fixed list rather than being random, so the placeholder is stable between renders and
  testable. The container carries `role="status"` + `aria-busy` and one
  localized message (the existing keys `*.loading`); the shimmer is the only motion → under
  `prefers-reduced-motion` it turns off and stays a static tone. Tests: `Skeleton.test.tsx`),
  `TileGrid` (**the virtualized card grid** — the same `react-virtuoso` `VirtuosoGrid` treatment the
  photo wall gets, for the card lists: generic over the item (`items` + `itemKey` + `renderItem`),
  window-scroll, geometry via `minTile`/`gap` (the same values as `TileGridSkeleton`, so the grid
  doesn't shift when the data lands). The list element carries the very same
  `repeat(auto-fill, minmax(<minTile>px, 1fr))` track list the plain CSS grid it replaced had — the
  column count keeps following the container width (1 column at 320px, 2 at 360px, 7 at 1280px) —
  and virtuoso's own style is spread **last** so the padding standing in for the unmounted rows wins.
  A buffer of `increaseViewportBy {top: 200, bottom: 400}` starts a row's covers a touch before it
  scrolls in. The class is `.kk-tile-grid`, a **selector hook only**: unlike `.kukatko-photo-grid`,
  which strips the card chrome off what it holds, the tiles keep their `.kk-tile` look. Used by
  `AlbumsPage` and `PeoplePage`. Tests: `TileGrid.test.tsx`),
  `ConfirmModal` (**the single shared confirmation dialog** — replaced the native `window.confirm`
  in four places: `AlbumDetailPage` (deleting an album), `LabelsPage` (deleting a label),
  `SavedSearchesPage` (deleting a saved search).
  Following the styled-modal pattern on `TrashPage` — one pattern instead of a grey OS dialog: **the confirm
  button carries the action itself** („Smazat album" / „Spustit import"), never „OK", and reads the same as
  the control that opened the dialog — the action keeps one name across the whole flow. Props `show`, `title`
  (a short question), `children` (the consequence — what happens and to what), `confirmLabel`, `cancelLabel?` (default
  the shared `confirmModal.cancel`), `variant?` `'danger' | 'primary'` (default `danger` colors the confirm
  red; non-destructive `primary`), `busy?` (locks both buttons and the close/backdrop for the duration of the
  action), `onConfirm`, `onCancel`. **The destructive button is not Enter's default**: after opening, focus rests
  on Zrušit, so a stray Enter cancels rather than deletes; Escape/close/backdrop cancel; react-bootstrap returns
  focus to the trigger. Copy is translated by the caller — no hardcoded strings. The dialog is `scrollable`, so a
  long consequence scrolls inside the body and the two buttons stay pinned instead of sliding off a short
  (keyboard-shrunk) phone viewport; it is deliberately **not** `fullscreen="sm-down"` like the admin form
  dialogs — a confirm has no inputs, so no keyboard ever covers its footer, and a phone-wide sheet for a
  two-line question reads as a page rather than as a question. Tests: `ConfirmModal.test.tsx`),
  `Markdown` (**the app's one Markdown renderer** — `react-markdown` + `rehype-sanitize`, for wherever
  prose somebody typed is shown as formatted text. Its two callers are the administrator's live
  preview of the first-sign-in welcome in `SettingsPage` and the welcome itself (`WelcomeModal`), so
  the preview is not a lookalike of the real thing but literally the same renderer. **Sanitising is not optional**
  even though only an administrator can write the text: `rehype-sanitize` runs on the rendered tree
  with its default (GitHub) schema, so a `javascript:` href, an `onclick`, an `<iframe>` or a `<style>`
  block cannot survive — a taken-over admin account must not become a way to run script in every other
  user's browser, and the welcome is shown to everybody once, unprompted. Raw HTML in the source is
  inert for a second reason too: `rehype-raw` is deliberately **not** installed, so react-markdown
  never parses HTML at all. A link is rendered through a `components.a` override — `target="_blank"` +
  `rel="noopener noreferrer"` — rather than by a rehype plugin, so the attributes are added *after*
  sanitising and cannot be stripped by it. Props: `children` (the source) and `className` (the
  wrapper). Tests: `Markdown.test.tsx`),
  `ReasonedButton` (**the shared „a button that is off always says why" control.** A greyed-out
  control with no explanation reads as a malfunction, so this drop-in for react-bootstrap's `Button`
  has **no `disabled` prop at all**: the only way to switch it off is `disabledReason` — one finished,
  already-translated sentence, specific („Nejdřív vyberte fotky, na které se má úprava použít.") rather
  than generic. Empty/`null`/`undefined` = a live button. **It does not use the native `disabled`
  attribute**, and that is the point: `<button disabled>` leaves the tab order, so a keyboard or
  screen-reader user can never reach the explanation, and Bootstrap's own `.btn:disabled` rule sets
  `pointer-events: none`, which stops the browser ever showing the `title` — a `title` on a natively
  disabled Bootstrap button is therefore invisible to **both** input methods. Instead it sets
  `aria-disabled`, keeps the button focusable and hoverable, swallows the click in JS (so a
  `type="submit"` inside a form cannot submit it either) and takes the greyed look from `.kk-btn-inert`
  in `app.css` — Bootstrap's own `--bs-btn-disabled-*` values plus `cursor: not-allowed`, minus the
  `pointer-events` kill; the hover/active/focus overrides keep an inert button from repainting on the
  way past, while leaving the focus ring alone. The reason reaches the reader three ways: `title`
  (mouse), `aria-describedby` → a `visually-hidden` sibling (keyboard/AT; `visually-hidden` is
  absolutely positioned, so it adds no box to the toolbar), and — where a cluster of buttons shares one
  reason already printed on screen — prop `reasonId`, which points `aria-describedby` at that visible
  line instead of hiding a second copy (one sentence for a screen reader, and a phone, which never gets
  a `title`, can read it at all). Used by `BulkEditControl`, `StackSelectedControl`, `PhotoLocation`
  and `UserActions` in `UsersPage`. Test helpers `expectOff`/`expectLive` live in `test/reasoned.ts` —
  jest-dom's `toBeDisabled()` only looks at the attribute and would report such a button as live.
  Tests: `ReasonedButton.test.tsx`),
  `RecordTable` (**the shared „wide admin table → stacked cards" reflow.** A many-column roster only
  ever survived a phone by scrolling sideways inside `.table-responsive`, which parked the later columns
  and — worse — the per-row actions off-screen. One `columns` definition drives both layouts, so a table
  and its phone form can never drift apart: `RecordColumn<T>` = `key`, `header` (already translated),
  `cell(record)`, plus `cellClassName?`/`cellStyle?` (the desktop `<td>` only — a `text-nowrap` or a
  width cap that would be wrong on a card), `multiline?` (a value typed into a `<textarea>` — the roster's
  note: it adds `.kk-multiline` in **both** layouts, because a note that reads as two lines on the table
  may not collapse into one on the card) and `cardHidden?`. On `md`+ it renders the familiar
  `<Table striped hover responsive>`; below it each record becomes one `Card` in a `<ul>`, every column
  a „label: value" line of a `dl.row` (`col-5`/`col-7`). The breakpoint is decided **in JS**
  (`useIsNarrowViewport`), like `MobileTabBar` — never `d-md-none` — so only one of the two layouts is
  ever in the DOM and assistive tech (or a test) never sees every record twice. Props `records`,
  `columns`, `rowKey`, `cardActions?` (the card's **full-width** action row, `.kk-record-card__actions`;
  its buttons clear the 44px finger floor **unconditionally**, not only on `pointer: coarse` — a narrow
  window on a laptop gets the same card — guarded by `styles/recordCards.test.ts`), `detail?` (an
  expanded block: a `colSpan` row under the table row, a bordered block at the foot of the card; return
  `null` when the record has nothing to expand), `className?`, `size?` (`'sm'` = the compact `table-sm`
  density on the desktop table only) and `hideHeader?` (**drops the desktop header row** for a listing whose
  first column already names the record — the maintenance findings; the cards keep every `header` as their
  `<dt>`, because a card has no header row to read across from). A value that must look the same in both
  layouts (`text-danger small`, `font-monospace`) belongs **in the `cell`, not in `cellClassName`** — the
  latter reaches the `<td>` only. Adopted by `UsersPage`, `AuditPage`, `ImportPage` (run history) and
  `MaintenancePage` (scan result); any other admin table can take it as-is. Tests: `RecordTable.test.tsx`),
  `HeaderActions` (**the shared „page-header actions → „…" overflow" collapse.** A detail header packs its
  actions next to the title, and on a phone a row of four or five ≥44px buttons wrapped into two or three rows
  — with the destructive one sitting inline among the neutral ones, a mis-tap away. Props are ready-made nodes,
  not descriptors, so a page keeps its own RBAC gating and button styling: `primary` (the one or two actions
  that stay inline **at every width**), `secondary` (inline on desktop, in the menu on a phone),
  `destructive` (same, but always last and, inside the menu, **behind a `Dropdown.Divider`**) and `id?` (the
  toggle's DOM id). Each is a `ReactNode[]` — an array, not a fragment, precisely so a conditionally rendered
  `{canWrite && <Button/>}` arrives as `false` and can be told apart from a real action: an all-`false` list
  renders **no toggle at all** rather than an empty menu, and every entry needs a stable `key` as any array of
  children does. The breakpoint is decided **in JS** (`useIsNarrowViewport`), never `d-md-none`, so only one
  layout is ever in the DOM; the menu stacks its actions as full-width buttons (an inner `d-grid`, like
  `BatchActionBar`'s overflow) and **closes itself on any click inside** — a plain button raises no `select`
  event react-bootstrap would act on, and a menu left standing open behind the modal its item just opened reads
  as a stuck page. Styles `.kk-header-overflow-*` in `app.css`; the bare „…" toggle drops Bootstrap's caret and
  squares off to the 44px floor on `pointer: coarse` (guarded by `styles/tapTargets.test.ts`). i18n
  `headerActions.overflow`. Adopted by `AlbumDetailPage`; any other detail header can take it as-is.
  Tests: `HeaderActions.test.tsx`);
  `components/upload/` = `DropZone` (the pick stage's one surface: a drag-and-drop target that is also
  one big tap target opening the picker, `accept` = `lib/mediaFiles` `PICKER_ACCEPT` (`image/*,video/*`
  + every known extension, so a picker does not hide RAW/HEIC) `multiple`, styled `.kk-upload-drop`
  — sunken, dashed hairline, because it is a hole to put photos into rather than a button. Whatever it
  is given uploads at once, so the copy promises that and there is nothing under it to press. A footnote
  names the third way in, **paste** — the page listens for it via `usePasteFiles`, but nothing on screen
  would otherwise say so),
  `PickFilesButton` (a real, thumb-sized button with its `<input type=file>` hidden behind it: the
  gallery, the camera (`camera` → `capture="environment"` + `accept="image/*"`) and **Přidat další
  soubory** are all this one component, so no entry point can forget that picking *is* starting),
  `UploadActionBar` (**the flow's one action area** — `.kk-upload-rail`. Every stage ends in it and it
  carries both what is happening and what to do next; the progress is drawn as the bar's own **top edge
  filling up** (a real `role=progressbar` with `barLabel` + `aria-valuenow`) beside the „7 / 20" count in
  tabular numerals and what is left. `position: sticky` (not `fixed`): it sits at the end of the page's
  own flow while the content is short and pins to the bottom edge — clear of the mobile tab bar via
  `--kk-bottom-edge` — as soon as it is not, so nothing reserves clearance for it and it behaves the same
  on a phone and on a desktop. Children are given **primary last**: lowest (nearest the thumb) stacked on
  a phone, rightmost in a row from `sm` up. Guarded by `styles/uploadRail.test.ts`),
  `UploadStagePick` / `UploadStageUploading` / `UploadStageDone` (the three stages, see `UploadPage`),
  `UploadQueuePanel` (**the per-file list, demoted**: a disclosure closed by default with the
  uploaded/duplicate/failed badges standing in for it, since fifty rows nobody reads are exactly what
  used to push the progress and the picker off a phone screen. It **opens itself the moment a file
  fails** — that is where the reason, the per-file **Opakovat** and the **jen neúspěšné** filter are —
  and wraps `UploadList`), `UploadItem` (a queue row as
  a standalone `kk-surface` card: **a local `UploadThumb` preview**, name+size, progress bar, status badge,
  near-duplicate warning, remove/retry actions; a failed row has `border-danger`),
  `UploadThumb` (the row's preview, painted by the browser from the picked `File` — **no upload, no server**:
  a fixed square (3.5rem, 4.5rem from `sm`) so virtuoso measures a stable row height, the object URL created
  on mount and **revoked on unmount**, so only the rows on screen hold one and clearing the queue hands them
  back; only what an `<img>` paints is loaded (`lib/mediaFiles` `previewKind`) — a video gets a play glyph,
  HEIC/TIFF/RAW a picture glyph, and an `onError` falls back to the same placeholder; the frame is tinted by
  state `.kk-upload-thumb--<status>` with the live % over an in-flight file and a warning glyph over a failed
  one, all `aria-hidden` because the badge says the same in words),
  `UploadList` (**a virtualized**
  `Virtuoso useWindowScroll` list of rows, gaps via `pb-2`, so 100+ files stay snappy on
  mobile. Tests: `UploadList.test.tsx` — rows, previews, placeholders, revocation, state tint), `UploadOrganize`
  (two searchable `MultiSelect`s for **albums**
  and **labels** that apply to the whole batch, with inline creation of a new item via `onCreate`; empty
  by default, driven by `useUploadOrganize`. Shown **inside** the uploading stage (choosing where the
  photos belong is what there is to do while they go up) and again in the done stage; both carry their
  own heading and framing sentence, so this is the two fields and nothing else. Their `row g-3` is the
  **only** width-dependent thing in the flow — side by side from `md`, stacked below it),
  and beside it the component-free `organizeSelection.ts` = `UPLOAD_ALBUMS_FIELD_ID`
  (the album field's id, kept as one literal) + `organizeSelectionNames`
  (selection → human names for the outcome sentence, a `create:` marker reading as the typed name);
  `components/library/` = `PhotoTile`
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
  detail**, not on the tile); the heart hides in selection mode; the optional
  `onFavoriteChange(uid,favorite)` reports every flip (optimistic **and** rolled back) up to the page,
  so a list that also favorites by another route keeps **one** baseline per photo; `src` takes
  **`photo.thumb_url` from the payload** via `useThumbSrc` and **never** builds it from the UID),
  `PhotoGrid` (a virtualized **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, footer spinner/retry. It serves **two loading shapes**: a grid that grows by
  appending pages passes `onEndReached` (→ next page), while a **windowed** grid (the library)
  omits it and instead hands over `photos` as long as the *whole* result with `undefined` where
  a page is not loaded — a hole renders as a tile-shaped `Skeleton` placeholder, `computeItemKey`
  falls back to `slot-<index>`, and a Shift+click range only spans the loaded uids. An index is then
  a photo's **absolute** position, which is what lets the timeline jump anywhere in one scroll; the `favoritable` prop
  leaks the heart onto the tiles (and `onFavoriteChange` its flips back to the page); an optional `gridRef` (imperative `scrollToIndex`
  handle) + `onRangeChanged` (the visible range) for the timeline; it takes its column template from
  `useGridDensity` → `lib/gridDensity` `gridTemplateColumns`, the DOM carries `data-density` for tests.
  **Which** stored count (and gutter) it follows is the optional `scope` prop — the library's by default,
  `REVIEW_GRID_SCOPE` on `/expand` — threaded to the inner `List` through virtuoso's `context`.
  A density change **only restyles** the existing `<div>` — virtuoso re-measures the tiles, scroll and selection
  survive because the grid is neither keyed nor remounted. Where the reader is in it is reported through
  **`onStateChanged(state)`** and put back through **`restoreStateFrom`** (virtuoso's own `GridStateSnapshot`:
  the offset plus the measurements that let the grid lay the tiles out at it before anything is on screen);
  the grid remembers nothing itself, the page does — see `useGridScrollMemory`),
  `TimelineScrubber` (**the timeline** — a thin fixed vertical date rail beside the grid: it fetches a monthly
  histogram via `useTimeline(params)` (refetch on filter change) and lays it out through
  `components/library/timelineRail`. **Every month bucket owns an equal slice of the rail**
  (`fractionForRank`), not a slice proportional to its photo count: positions used to be `cumulative/total`,
  which on the real library (121 years, ~98 % of the photos in the last two decades) squeezed six decades
  into a couple of pixels and stacked 103 year labels on top of each other. `buildRail(buckets, heightPx)`
  then draws **only what the measured height fits** — month ticks collapse until they are at least
  `TICK_MIN_GAP_PX` apart and a year label is printed only where it clears the previous one by
  `LABEL_MIN_GAP_PX`, so **no two labels can overlap** (459 buckets in a 549 px rail → 76 ticks and
  26 labels, 2026 down to 1905). The height comes from a `ResizeObserver` on the rail
  (`FALLBACK_RAIL_HEIGHT_PX` until the first measurement, e.g. in jsdom). The **last tick anchors to the
  library's oldest month**, so the start of the archive is always one click away; a click jumps via
  `onJump({index,month,replace})` — `index` is the bucket's `cumulative`, i.e. the month's **absolute**
  position in the result as counted by the DB, so the grid scrolls straight there and fetches only the page
  that lands under it (`replace` is true for the steps of a drag and for an anchor restore, so one gesture
  leaves one history entry, not one per month crossed). A drag reads the same mapping backwards
  (`rankForFraction`), so the rail and the grid always agree on where a position lands. The `anchor`
  prop (`YYYY-MM`, from the page's `at` URL param) is resolved against the buckets and jumped to **once**,
  which is what makes Back, a reload and a shared link land on the month the reader was on. A press **does not capture the pointer** — capture
  retargets the compatibility `click` onto the capturing element, and pressing a tick would then do
  nothing; capture is taken on the first move past `DRAG_THRESHOLD_PX`, which is also what tells a click
  from a drag. The active month is highlighted per `activeIndex` (start of the visible range) by a floating
  orange bubble (`.kukatko-timeline-current`) in its own track **left of the rail**; the rail is wide enough
  that a year label (`.kukatko-timeline-year`, on a `.has-year` tick) stays inside, so the bubble and the
  year labels **never overlap** even at a year boundary (where they fall onto one line); the overlay is
  `position: fixed`, so a loading/empty timeline renders nothing and
  doesn't shift the layout; on the library only for
  the default newest sort.
  **The page holds a lane open for it** (`.kukatko-page:has(.kukatko-timeline)` in `app.css`, `sm` and up).
  The rail is fixed at the viewport's right edge while the content is a Bootstrap container whose
  `max-width` steps at each breakpoint, so just above every step the container's right edge walked *under*
  the rail — measured at 992–1111, 1200–1291 and 1400–1471 px, worst at 1000 px, where the rail covered
  48 % of the last tile in a row and half of the **Filtry** button and took the clicks meant for them,
  while the bubble lay over **Uložit pohled** at nearly every desktop width. So the `.kukatko-page` column
  reserves `--kk-timeline-lane` — `--kk-timeline-rail-width` (4.25 rem, a four-digit year plus its mark) +
  `--kk-timeline-bubble-width` (5.75 rem, the bubble's own margin included and its `max-width` capped to
  it, above the 72 px of the widest month label any locale prints) — whenever a rail is **mounted**:
  `:has`, not a page-level flag, because the rail itself declines to render for a timeline that is loading,
  empty or too short in time, and a lane held open for a rail that is not there is a page pushed off centre
  for nothing. The container then centres in what is left, which leaves the same gap on its left as between
  its right edge and the lane. Guarded by `styles/timelineLane.test.ts`, which rebuilds the container
  geometry out of the stylesheet, pins it against the overlap measured in the browser before the fix, and
  sweeps every width from 576 to 3840 px — one width proves nothing, the bug lived *between* breakpoints. It **runs whichever way its grid does** — the backend returns the histogram in
  the grid's own order, so an album read oldest-first gets a rail whose top is its first month (nothing
  here assumes newest-first beyond the order the buckets arrive in; only what a collapsed tick is
  *called* is resolved by date). **`minSpanMonths`** renders no rail at all below a given span
  (`spanMonths(buckets)`) — the library passes nothing, the album page 24, because a scale of months
  over one afternoon is a control offering nothing at the cost of a strip of the screen.
  **The phone gets the same rail, narrowed and asleep** — below 576 px it used
  to be `display:none`, which left the device photos are actually browsed on with nothing but scrolling
  to cross a 369 000 px list. There (`styles/app.css` `@media (max-width: 575.98px)`) it is a 2.5 rem
  strip on the right edge showing **only its year labels** (each on its own plate — it lies over
  photographs, not beside them), **starting where the grid starts** so it can't take a tap meant for
  the **Filtry** button: the rail measures the page's grid box (the required **`gridWrapRef`** prop,
  `useGridTop` — on mount in a layout effect, then on scroll/resize and a `ResizeObserver` on the
  body, coalesced into an animation frame) and publishes its viewport top as `--kukatko-timeline-top`,
  which the phone rule consumes as `top: max(navbar-floor, var(--kukatko-timeline-top, …))`. It used
  to be a constant 6 rem below the navbar — the height of the filter row *and nothing else* — so the
  „Co je nového" digest (`WhatsNewPanel`) or an `AnnouncementBanner` pushed **Filtry** down under the
  rail and a tap on it jumped the grid by 142 192 px instead of opening the drawer (2026-08-12,
  390 × 844). The measurement is clamped at 0, so once the header has scrolled away the `max()` floor
  — just below the sticky navbar — takes over and further scrolling re-renders nothing.
  It **sleeps**: at rest a 0.55-opacity scale, and any activity — the grid scrolling
  under it (`activeIndex` moving, no listener of its own) or a finger on it — sets `is-active` for
  `IDLE_MS` (1.6 s), which brings it to full strength, backs it with a blurred plate and reveals the month
  bubble. The state is a class; everything it *means* is CSS, so on desktop it means nothing. `LibraryPage`
  reserves the strip as grid padding (`.kukatko-grid-timeline-lane`, phone widths only, while the scrubber
  is shown) — a phone has no container margin for the rail to hang in, and without the lane it would cover
  the right column's favourite hearts and take their taps.
  **On a coarse pointer the rail is thinned for a thumb, not for an eye** (`useCoarsePointer`, so a tablet
  gets it and a narrow desktop window does not): `buildRail` is called with `TOUCH_TARGET_PX` (44) as its
  label gap instead of `LABEL_MIN_GAP_PX` (20), which puts the year ticks a fingertip apart, and only those
  ticks are controls — `touchTargets(ticks)` keeps the labelled ones and hands each the months of the small
  ticks that follow it, so the controls still **partition** the buckets and no month becomes unreachable
  (the aria-label then names the whole range, „Jump to <from> – <to>"). The rest keep being drawn as the
  scale's texture but render as `<span class="…is-decorative" aria-hidden>`: no role, no name, no focus
  stop, and `pointer-events: none`, so a press there reaches the rail and scrubs at the month grain a tap
  gave up. `app.css` `@media (pointer: coarse)` then gives each control a 2.75 rem box and the phone strip
  + lane grow to 2.75 rem so a 44 px control fits inside them (`right: 0` — a wider one would hang over the
  tiles and take their taps). It used to draw 93 buttons in a 40 px strip — 31 year ticks of 39.8 × 16.2 px
  at a 20.2 px pitch and 62 month ticks of 16.0 × 5.2 px, where a fingertip is 34–45 px and a mis-tap is a
  jump of tens of thousands of pixels (2026-08-12, 390 × 844, `pointer: coarse` forced over CDP); it now
  draws 17 controls of 44 × 44 px no closer than 44.1 px, and a mouse still gets all 114 ticks), `FilterBar`
  (**a redesign for a calm default state + progressive disclosure**: on desktop the header holds a prominent
  search field (the visual anchor, the largest element), sort (incl. **by rating**),
  `GridDensityControl` and a
  **Filtry** button with a badge of the active-filter count; advanced filters (location, private,
  camera, archive, **min. rating ≥1…≥5**, **picked/rejected flag**) live in a collapsible
  panel — inline `Collapse` on desktop, `Offcanvas` on mobile per `matchMedia` (the shared hook `useIsNarrowViewport`,
  defensive against jsdom, where `matchMedia` returns `undefined`).
  **On a phone the header is the search field and the Filtry button, and nothing else.** That desktop
  header alone stacked into three rows there, and with the page heading, the search note and the count line
  above it the first photo started at 371 px of an 852 px screen (measured on production at 393 × 852);
  now it starts at **150 px**. What moved: sort and density into the drawer (`DisplayControls`, its first
  section — they are what was *taken away* from the bar, so they must be the first thing found), the search
  note to the drawer's foot (`SearchNote`, defined once and placed in exactly one of the two), the page's own
  view actions to the drawer's very bottom (**`mobileActions`**, a `ReactNode` the page hands over — rendered
  only when narrow, so the buttons are never in the document twice), and the **result count into the header
  row itself**, wrapping onto a full-width line under the Filtry button (`.kukatko-filter-status`
  + `flex-row-reverse`, with a 2.75 rem right pad so it clears the timeline rail's lane). `.kukatko-filter-search`
  drops to an 8 rem flex-basis below `md` (an 18 rem basis is wider than the screen, which wrapped the button
  onto its own row) and the bar's bottom margin is `.kukatko-filter-bar` (1 rem, 0.5 rem on a phone) rather
  than an `mb-3` utility.
  **The mobile drawer ends in a sticky footer** (`FilterDrawerFooter`, `.offcanvas-footer.kukatko-filter-footer`):
  the drawer covers the screen, so the „Počet fotek: N" line stays behind it and filtering was blind —
  set a period, a person and a rating, scroll ten fields back up to the cross (the only exit) and only there
  learn the combination matches nothing. The footer's **primary button carries the live count**
  („Zobrazit 227 fotek", from the same `total` prop, so it follows every filter change with the drawer
  open) and closes the drawer; at `total === 0` it says „Žádné fotky — zavřít" instead of promising a grid
  that isn't there and **stays usable** (getting out must always be possible), and with `total` undefined
  („nothing searched for yet") it drops the number for „Zavřít filtry". Beside it a secondary
  **„Zrušit filtry"** (only when there is something to clear) that deliberately **does not close** —
  clearing is the recovery from an empty set and the count beside it shows the recovery worked.
  Sticky needs no positioning: `.offcanvas` is already a flex column whose `.offcanvas-body` grows and
  scrolls, so a footer **after** the body is pinned to the bottom edge *and* subtracted from the scroll
  area (the last field scrolls above it, no reserved padding to keep in sync); the footer carries
  `env(safe-area-inset-bottom)` itself, because the drawer covers the `.kk-tabbar` that normally owns that
  edge. Each active filter = a removable
  **chip** (`buildChips`, a pill with a cross, clears only that filter — the `q` query has no chip,
  it has its own field; the photo count below the chips comes from the `total` prop, which is **optional** —
  omit it and the bar states nothing (the live region stays mounted, so the number is announced when it
  arrives), which is how `SearchPage` avoids claiming zero photos before a query is even typed;
  **album and label chips carry the entity color** — `.kk-entity-album`
  vs. `.kk-entity-tag` + a guide icon from `ENTITY_STYLE`, so an album and a label are distinct at a glance
  (see *entity colors* in `tokens.css`); the other filters stay a neutral `text-bg-primary`)
  + one **„zrušit filtry"** + the photo count; **no behavior change** — everything
  runs through `viewToParams`/`useUrlState`/`LibraryView`, the query replaces history, the rest push;
  generic over `LibraryView`+a superset, props `showSearch`/`showSort` hide the query/sort
  on the search page, **`sortOptions`** narrows the sort list to given values **in the given order**
  (the album page passes `ALBUM_SORTS` = oldest/newest, its sort key being pinned server-side; omitted =
  all six), `showDensity` hides density in the trash (card-based, not a photo grid),
  **`uploaders`** (from `useUploaders`) adds a **Kdo nahrál** select to the panel — the contributors to the
  *current* view, largest first, each with its count, „Kdokoli" at rest and **„Importované"** for the photos
  nobody uploaded (`view.uploader` = a user uid or the reserved `UPLOADER_NONE`); it is a **single** choice,
  not an add-picker like album/label/person, because a photo has exactly one uploader. It rides in its own
  prop rather than in `facets` precisely so a grid already scoped to one album — where the entity pickers are
  dropped — still offers it: „several people uploaded into this album, show me one person's share" is the
  case it exists for. Like the primary pickers it admits when `uploader:` in `q` has taken it over,
  **`showFavorite`** enables the **Oblíbené** toggle in the panel (a two-state select „Vše"/„Jen oblíbené"
  → `view.favorite` `''`/`'true'`, the backend scopes only to `true`; the library enables it so you can
  combine „oblíbené + album + období" in the main grid, the Oblíbené page doesn't — it's already scoped);
  beside the GPS one sits **Datum** — a second `TriStateSelect` over `view.dated`, the worklist for the
  photos nobody could date. It is named after the *date*, not after a flag, so its answers read
  „má datum" / „bez data" rather than Ano/Ne: `TriStateSelect` therefore takes optional `yesLabel`/`noLabel`
  (default Ano/Ne, which is what „Poloha" wants) and an optional `fromQuery`, since a `dated:` typed into `q`
  takes the filter over exactly as `album:` takes over its picker
  (chips/panel/clear keep working); ~44 px tap targets via `styles/app.css`
  `.kukatko-filter-*`;
  **the four ways photos are actually searched** (the entity three from the `facets` prop of
  `useLibraryFacets`, the period always): on
  **desktop** its own always-visible row of four below the header, on **phone** (per
  `useIsNarrowViewport`) it **folds into the same filter `Offcanvas`** as the advanced filters —
  otherwise four columns stacked below one another would push the photos below the first screen; the active filter
  still stays visible as a **chip**, so the filtered set is no mystery even with the drawer closed:
  **Období** = `PeriodFilter` (below; rendered even without `facets` — an album- or place-scoped grid can be
  narrowed in time too, it just gets no decade list),
  **Album**, **Štítek** and **Osoba** = `SearchableSelect` (all collections grow without limit;
  people from `GET /subjects` with `photo_count` — the count beside an option promises how many photos
  picking it yields, so a person is counted by photos like an album or a label, not by faces),
  **multi-select**: each choice is **added** to the current
  set (AND — a photo must be in all selected albums, carry all labels and contain all
  selected people), the select is a pure „add-picker" (it keeps the placeholder „libovolné", drops its selected
  items from its options so they can't be added twice), already-selected albums/labels/people hang as
  removable chips (one per UID) below.
  **A filter is not set only by its picker**: `year:`/`taken:`/`before:`/`after:`, `album:`, `label:` and
  `person:`/`subject:` inside `q`
  filter the grid just as hard while the picker knows nothing about them (a query of `year:1960-1969` over a
  „Libovolné období" trigger is the visible state contradicting the results). So `FilterBar` scans `q` with
  `lib/queryLanguage.ts` `queryFilterTokens`/`facetQueryTokens` + `FACET_QUERY_KEYS` — a **scanner, not a
  parser**: it answers only „which known filter keys does this query use", parsing stays exclusively with the
  backend — and every affected control admits it: the resting option/placeholder reads „Určuje dotaz" instead of
  „libovolné", and a `form-text` note (`QueryOverrideNote`, `bi-info-circle`) below the control
  quotes the responsible tokens verbatim as `<code>` (`aria-describedby` on the control). **Období goes one
  step further and shows the period the query sets** (`lib/period` `periodFromQuery` over a lone, plainly
  shaped `year:` token → `1960–1969` on the trigger), so the control and the query can never contradict each
  other; several period tokens AND together, so none of them is shown as *the* period and only the note
  quotes them. An **unknown**
  key (`osoba:`) filters nothing — it degrades to free text server-side — so it is deliberately not reported
  there; the picker keeps working, adding a filter on top of the query narrows further as ANDed filters do
  everywhere else.
  The inline search field (`q`) is **not** merely a substring narrowing: it runs the **whole `klíč:hodnota`
  search language**, exactly as `/search` does — `year:1960-1969` narrows the library grid to the sixties here
  too — with the residual free text matching title/description/notes as a substring. The placeholder and the
  hint therefore name both halves („Hledat — text, nebo filtr jako `year:1965` či `person:Jarmila`"), and
  beside the field sits **the very same `SearchQueryHelp` `?`** the search page opens — one language, one
  explanation, never a second copy to drift (`showSearch={false}` hides field and `?` together, so `/search`
  doesn't end up with two triggers). What `/search` adds on top is **ranking**, which is what
  **the link to `/search`** promises: it shows **only when semantic
  search is available** — `FilterBar` reads `useCapabilities().semantic_search` and hides the link when the embeddings box is offline
  (fulltext keeps working, but its label promises semantics); `searchHref` carries the current `q`,
  the search modes are **not duplicated** here),
  `PeriodFilter` (`components/library/`, **the library's one control on the time axis**, and the fix for
  having had two: a **Rok** `<select>` of 109 single years — out of which no decade could be assembled — in
  the primary row, and a **Pořízeno od / do** pair hidden one click deeper in the panel that could express a
  range but nobody found. The trigger is styled as the selects beside it (`.form-select`, `bi-calendar-range`)
  and **states the period in words** — `1960–1969`, `1965`, „od 1960", „do 1949",
  „1. 6. 2019 – 31. 8. 2019" (`lib/period` `formatPeriod`) — so it needn't be opened to be read; its panel
  (`.kukatko-period-panel`, capped at `50vh` and scrollable like `SearchableSelect`'s listbox) holds
  „Libovolné období", the **decades the library actually holds** with their photo counts
  (`groupYearsIntoDecades` over the `years` facet — the calendar decade, so what it selects never depends on
  which of its years exist today), each **expandable by a chevron to its own years** with counts, and the
  **exact-date** pair for „léto 2019" — `position: sticky` at the bottom of the scroll area
  (`.kukatko-period-exact`), because a date pair that scrolls away under thirteen decades is exactly the
  hidden filter this control replaces. Opening it auto-expands the decade of the current period.
  Everything it writes is the one `Period` = `taken_after`/`taken_before` (+ clearing the legacy `year`),
  so there is no second state to drift; it closes on Escape and on focus leaving the widget, needs no popper),
  `SearchableSelect`
  (`components/library/`, a single-select facet you can type into: at rest it shows the choice,
  focus opens the full list, typing narrows it **case- and diacritic-insensitive** via `lib/text`
  `foldedIncludes` (`namesti` finds `Náměstí`, same as the backend `immutable_unaccent`);
  the leading row „libovolné" clears the facet, keyboard Up/Down/Enter/Esc, combobox/listbox ARIA,
  a `MAX_SUGGESTIONS` (50) cap on rendered suggestions; it never creates items —
  mirrors `AddAutocomplete`), `filterChips.ts` (pure `buildChips(view, t, locale, {facets?, includeQuery?})`
  → `FilterChip{key,label,clear,kind?}` for each active filter; **one chip for the whole time axis**
  („Období: 1960–1969"), worded by the same `formatPeriod` the control uses — a chip and its control must not
  describe one filter differently, which is why `locale` is passed in; **one chip per selected album,
  label and person** (`clear` removes only its own UID from the list, the last chip clears the facet; an album chip has
  `kind:'album'`, a label `kind:'tag'`, a person `kind:'person'` → `FilterBar` takes the color + icon from it via
  `ENTITY_STYLE`; **favorites** and **„Datum: bez data"** = neutral chips with no `kind`); `facets`
  name the album/label/person by title instead of UID (missing → raw UID, a chip is never empty),
  `includeQuery` enables a chip for `q`
  — the filter bar disables it (it has its own field), **the empty state enables it** (a reader at zero results must
  see all the filters that got them there); the field length = the active-filter count on the badge),
  `timelineRail.ts` (pure layout of the timeline rail, no React: `fractionForRank`/`rankForFraction`
  = the position of a month bucket on the rail and its exact inverse for a drag, `anchorOf(bucket)`
  = the bucket's month as the `YYYY-MM` value the library carries in its `at` URL param, `rankForIndex`
  = binary search from a photo index back to its month, `spanMonths(buckets)` = the calendar months
  between the two ends counting both (the album page's threshold for showing a rail at all — it measures
  the *span*, not the buckets, so two photos 116 years apart span 116 years), and
  `buildRail(buckets, heightPx, labelGapPx = LABEL_MIN_GAP_PX)` → `RailTick[]`,
  which collapses month ticks and thins year labels to what the measured height fits; the ticks
  **partition** the buckets, so nothing becomes unreachable and the active month always highlights
  a tick — the invariants are tested directly against a ~460-bucket, 122-year fixture.
  A rail that will be **tapped** passes `TOUCH_TARGET_PX` (44, the app's finger floor) as the label gap —
  there the labels are also the hit areas, so what has to fit between two of them is a fingertip, not a
  line of text — and then `touchTargets(ticks)` reduces it to the ticks that can carry a 44 px box: the
  labelled ones, each extended to own the buckets of the unlabelled ticks after it (so the result still
  partitions, and `oldest`/`newest` name the whole swallowed range by date). It copies rather than
  rewrites, because `buildRail`'s output is memoized by the component),
  `SimilarPhotos` (a reusable horizontally scrollable strip
  of similar photos over `GET /photos/{uid}/similar` via `fetchSimilar`, links to the detail,
  empty-friendly + loading/error, refetch on `uid` change),
  `FavoriteButton` (a heart toggle over `useFavorite` — an **optimistic** per-user favorite
  with rollback; no role gate, allowed to any logged-in user; as a tile overlay it is a sibling
  of the link, so a click doesn't navigate; an optional `onChange(favorite)` reports the flip and the
  rollback to the owning list, which is how the library's `f` shares this button's state instead of
  keeping a second one), `RatingStars` (pure controlled 0–5 stars; a click on the current
  rating clears it to 0; without `onRate` a read-only display) + `FlagControl` (a pure controlled per-user
  **personal flag** — three neutral states via `Icon` bootstrap-icons: 👁 **Prohlédnout později**
  (stored `eye`, `text-info`), 👍 **Vybrat** (stored `pick`, `text-success`), 👎 **Zamítnout**
  (stored `reject`, `text-danger`). Each button is **named after the act, not the glyph** —
  the `aria-label` is the short name (`rating.eye`/`pick`/`reject`) and the `title` is the
  one-sentence explanation (`rating.eyeHint`/`pickHint`/`rejectHint`), which is also what the
  library's **Označení** filter calls its values (`library.flag.eyes` = „K prohlédnutí později"),
  so the filter and the viewer agree; the stored values and the `flag:` query key are untouched.
  A click on the active state clears it to `none`; without `onFlag` read-only; a sibling of the link → a click doesn't navigate),
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
  `−` is disabled at 1 (one photo per row), `+` at the **viewport's ceiling** (10 on a desktop, but 4 below
  768px and 3 below 576px, see `useGridDensity` `maxColumns`) — the chip reads out the count **in effect**,
  so a narrow screen shows what it renders and never offers a step it would refuse; reads/writes `useGridDensity`, i.e.
  localStorage, **not the URL** — it is a device preference, not part of the shared view; it sits in the header of
  `FilterBar` and in the header of `SubjectPage` (a person's gallery), it changes all photo grids in the app
  at once — and because it is only a view preference, **it is not write-gated** (a viewer sees it too);
  the `scope?` prop says **which** count it moves (default `LIBRARY_GRID_SCOPE` = the photo library);
  every review tool (`/faces`, `/recognition`, `/expand`, `/outliers`, `/people/clusters` and the two
  duplicate screens) hands it `REVIEW_GRID_SCOPE`, so they all get the same control on **one** number of
  their own — separate from the library's, shared between themselves, see `lib/gridDensity`;
  `PhotoTile`+`PhotoGrid` support
  **a modern multi-select in the style of photo apps** (props `selectable`/`selectFirst`/`selected`/
  `anySelected`/`onToggleSelect`, or `selection`): each tile carries a **round check
  circle** in the corner (`.kk-tile__check`, a sibling of the link/button like the heart — a click **selects without
  opening the photo**), which appears on hover and **stays visible once something is selected**
  (`kk-tile--checks`). On a **coarse pointer / touch screen** there is no hover to reveal it, and the check
  is the *only* entry point into multi-select (the library has no "Vybrat" button, and in explicit mode
  nothing else marks the grid as a selection surface), so `@media (hover: none), (pointer: coarse)` in
  `tokens.css` **pins it visible at rest** and grows its tap target to the app's **2.75rem (44px)** floor via an
  invisible `::before` — expanded down/right so it stops at the tile's own edge and can't swallow a tap
  meant for the neighbouring tile. This is safe by construction: the control is **mounted only while the tile
  is `selectable`**, so a viewer's grid has nothing to reveal. Guarded by `src/styles/tokens.test.ts`
  (jsdom evaluates no media queries, so the rule is asserted against the stylesheet source).
  A selected tile gets an **accent ring** (`kk-tile--selected` → inset
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
  `components/organize/` = `AlbumTile` (an album card: the cover / name / **description**
  (`.kk-prose-clamp`, two lines then an ellipsis — the detail page shows all of it; a machine-made
  album has none and loses no room to it) / **year range**
  via `formatCaptureRange` (only when the album has dated photos) / count → `/albums/{uid}`;
  the cover is the `cover` prop — what `lib/albumCovers` planned for the whole grid — and without it
  the tile plans **for itself alone** (right for a tile rendered on its own, and why a caller rendering
  many must pass it): a `collage` draws four `tile_224` cells into `.kk-tile__collage` (a 2 × 2 grid
  filling the same square well, so the geometry never differs from a single cover) inside an
  `aria-hidden` wrapper with empty `alt`s — the link is already named after the album, and four
  identical alt texts would only make the page longer to listen to —, a `single` draws one `tile_500`
  image with the title as its `alt`, and `none` falls back to `EmptyState`;
  the name is the **display title**
  (`i18n/albumNames` `albumDisplayTitle(title, i18n.language)`) and the *same* string feeds the link's
  `aria-label`, its `title` and a single cover's `alt`, so none of them can drift back to the raw one),
  `AlbumFilterBar` (the album index's own filter bar: the section strip — Moje alba · Podle měsíce ·
  Momenty · Místa, each with its live count as a `Badge`, `ButtonGroup` + `aria-pressed` like
  `CandidateFilterTabs` — plus a name search, the ordering `Form.Select` and the „I prázdná" switch;
  every control writes straight into the URL via `SetUrlState<AlbumsView>`, **pushing** except the
  live-typed query, which replaces),
  `LabelFilterBar` (the labels index's own filter bar: a name search + the ordering, both writing
  straight into the URL via `SetUrlState<LabelsView>`, **pushing** except the live-typed query. No
  filter beyond the search: a label is a name and a count, and there is nothing else to filter it by),
  `LabelCloud` (the labels index as a **wrapping cloud of pills** — a real `ul`/`li`, so a screen
  reader counts them instead of reading a heap of links. A folded numbered family renders as one
  dashed toggle chip (`aria-expanded` + `aria-controls`) showing the shared prefix and **how many
  labels** hide behind it; expanded, it takes a whole line (`.kk-label-cloud__group`) with its members
  in their own boxed list, so an open `Dum…` cannot be mistaken for the cloud continuing),
  `LabelChip` (one label: a pill (`.kk-label-chip`) carrying **the label's own photo** (`.kk-label-chip__thumb`,
  the `cover_uid` the listing derives, drawn at `tile_100` — the same preview an album tile shows, at chip
  size, because a wall of a hundred identical pills says far less than the pictures behind them; a label with no
  photo, and one whose thumbnail fails to load, simply keeps the plain pill — a chip is not a fixed-height row,
  so there is no gap to fill), the name (truncates — user data),
  the photo count and, for an editor, the "…" actions `Dropdown`. Its `LabelChipActions` prop bundles
  rename/delete/toggle-review + `savingUID`, and its **absence** is what makes the chip read-only for
  a viewer. The link's `aria-label` spells out the bare count and the skip state, which the glyphs say
  nothing about, and the preview stays out of it (`alt=""` + `aria-hidden`, lazy): the link is already named
  after the label. On a coarse pointer the whole pill grows to 44px rather than the menu being exempted
  from the floor — see `styles/tapTargets.test.ts`),
  `AlbumEditModal` (create/rename an album: name/description/private), `LabelEditModal` (create/rename
  a label: name/priority), `SelectionBar` (a sticky selection toolbar: count +
  actions + clear — shown at `selection.count > 0` since those grids are hover-select too;
  since the batch bar took over **every photo list**, it is left only to the **non-photo-list**
  grids: `TrashPage`, `PlacesPage`, `OutliersPage` and the `/expand` candidate review),
  `BatchActionBar` (the floating bottom **bulk action bar** of **every photo list** — frosted
  (`--kk-header-bg` + `backdrop-filter: blur(--kk-header-blur))`, `--kk-shadow-3`, `.kk-batch-*`
  in `app.css`) `position: fixed` centered at the bottom, **slides up at ≥ 1 selected photo**, carries a live
  count (`aria-live`), **Vybrat vše** (`onSelectAll`), close (✕ = `selection.clear`) and the actions
  **Přidat do alba** / **Štítky** (add+remove, both via `MultiSelect` in a `Modal centered fullscreen="sm-down"`
  — deliberately **not `scrollable`**, whose `overflow: auto` body clipped the suggestion overlay; the phone
  gets the whole screen so the field + its options clear the keyboard, options
  lazy from `fetchAlbums`/`fetchLabels` — the effect keys **only on `picker`** (+ a retry counter), never on
  `options.status`, otherwise writing `loading`/`ready` would re-run the effect and **abort its own fetch**;
  "already loaded" is held by `useRef`, a retry after an error bumps the counter, cache per session.
  Both **add** fields also **create**: `onCreate` (only for `useAuth().canWrite` — a viewer just picks)
  appends **„Vytvořit «název»“** for a name no album/label carries, which is where a new album usually
  starts ("these forty photos are Ostatky 2022"). The pick is held as a `create:` marker
  (`lib/pendingCreate`, shared with `BulkEditModal` and `UploadOrganize`) and turned into a real album/label
  **only on Použít**, via `resolvePending` → `POST /albums`/`POST /labels`, whose fresh UID is swapped into
  the field and the cached options before the batch goes — so closing the picker creates nothing and a retry
  after a failed batch never makes a second album of that name. A failed creation names it in a danger toast
  (`batch.createError` / `batch.createErrorUnknown`), skips the batch and **keeps the photo selection**.
  The **remove** labels field never creates — a label that does not exist cannot be taken off a photo),
  **Oblíbené**, **Archivovat**, **Stáhnout**
  (`DownloadZipButton`), **Seskupit** (`StackSelectedControl`) and **Další úpravy** (the whole
  `BulkEditModal`); each metadata action runs **as a single `POST /photos/bulk`** via `bulkUpdatePhotos`,
  success/failure reported by a **toast** (`useToast`): success clears the selection and reloads the grid (`bulk.finish`),
  **a failure keeps the selection** (it can be retried). Driven by `useBulkEdit({hoverSelect:true})`; Esc clears the
  selection via grid keyboard nav. **Editor/admin only** (`bulk.canBulkEdit`), i18n `batch.*`.
  **Responsive:** on a phone (`useIsNarrowViewport`) the ~10 labelled actions can't share one row, so the bar
  **collapses** — clear (✕), the count and the two most-common actions (**Přidat do alba** / **Štítky**, icon-only)
  stay inline, the rest fold into a **`…` overflow `Dropdown`** (`drop="up"`, `batch.overflow`) — keeping it a single
  compact row instead of wrapping into a tall block. The grid's bottom clearance is **measured, not constant**: the bar
  publishes its live rendered height (a `ResizeObserver` sets the `--kk-batch-bar-height` root var) and every list page
  reserves `--kk-batch-clearance` (`app.css`) as bottom padding while selecting, so the last photo row always scrolls
  clear however the bar wraps or collapses; the safe-area inset is folded into that var.
  **`extraActions?: readonly BatchExtraAction[]`** merges a page's own actions onto the *same* bar
  (`{id, icon, label, onClick, disabled?, danger?}`, rendered after **Další úpravy** with the shared
  `BarAction` styling, disabled while a batch request is in flight) — so a page never grows a second
  toolbar: `AlbumDetailPage` passes **Nastavit obálku** (needs exactly 1 selected) + **Odebrat z alba**
  (`danger`), `SubjectPage` passes **Nastavit jako náhled**; the library, favorites, label and search
  pages pass none),
  `BulkEditControl` (**a reusable trigger** for bulk editing: a button
  (`selection.edit`) + `BulkEditModal`, driven solely by the result of `useBulkEdit`; **it doesn't render at all
  for a viewer**, and at an empty selection it is a `ReasonedButton` that says why it is off
  (`selection.editDisabled` — "pick the photos the edit should apply to first") rather than greying out
  in silence — just drop it into `SelectionBar`, the page
  holds no dialog state; the optional `prefill` prop flows through into the modal. The photo lists now
  reach the same dialog through the batch bar's **Další úpravy**, so this is left to the grids that
  still use `SelectionBar`), `SelectionStart` (**the counterpart** to `BulkEditControl`: a button
  `selection.enter` that turns on selection mode; it doesn't render for a viewer or for an already-enabled selection,
  `onEnter` overrides the action for a page that must first leave another mode),
  `DownloadZipButton` (**download the selection or the whole album as a ZIP** of originals: calls
  `downloadPhotosZip`, shows a spinner while it streams and an error on failure — 413 = over the cap
  (`download.zipTooMany`), otherwise generic (`download.zipError`); `photoUids` = the current selection,
  `albumUid` (+ `name` = the album title) = the whole album; **available to a viewer too** (a download is not a write),
  disabled when there is nothing to download. Inserted into the library's `SelectionBar` and into the album header),
  `SharePhotosButton` (**share the selection into the phone's own library**, beside the ZIP: `photoUids` (a
  selection, or the single photo of `PhotoDetailPage`) → the phone's share sheet, where iOS offers "Save Images"
  into Apple Photos and Android offers Google Photos — the answer to "someone else uploaded these and I want mine
  in my phone". It **renders nothing at all** where the browser cannot hand files over (desktop Firefox, Linux, an
  insecure origin — `navigator.canShare({files})` asked with a real probe file), because a control that throws on
  tap is worse than none, and the ZIP download beside it stays the answer there; **available to a viewer too**,
  gated exactly like the ZIP (`RequireDownload`), disabled at an empty selection. All the state is
  `usePhotoShare`; this component only says what it is doing — a spinner and **„Připravuji 12 z 20"** while a batch
  downloads (originals over a mobile connection are not instant), **„Sdílet dávku 2 z 5"** plus a live „Sdíleno 1
  z 5 dávek" between batches (each needs its own tap), and one sentence in `role="alert"` for a photo that would
  not load / an over-cap selection / a sheet that refused. A cancelled sheet says nothing. i18n `sharePhotos.*`,
  tests `SharePhotosButton.test.tsx` + the wiring in `BatchActionBar.test.tsx`),
  `StackSelectedControl` (a **Seskupit vybrané** button (`selection.stack`) on the batch bar — so on
  every photo list, not just the library — **editor/admin only** (absent, never greyed, for a viewer); a
  `ReasonedButton` that stays off until **≥ 2** photos are selected and tells the reader which of the two
  reasons it is: `selection.stackDisabled` (pick another photo — something to act on) or
  `selection.stackBusy` (the request is running — something to wait out); calls
  `stackPhotos`, on success clears the selection and reloads the grid; tests
  `StackSelectedControl.test.tsx`),
  `BulkEditModal` (**bulk edit** of the selection via `POST /photos/bulk`, the whole batch
  in a single transaction on the backend; the form is split into **four sections** (`.kk-text-eyebrow`
  headings): **Zařazení** (add/remove albums, add/remove labels — four `MultiSelect`s, so one
  apply handles **multiple albums and multiple labels at once**; the add fields additionally offer via `onCreate`
  **„Vytvořit «název»“** for a name that fold-insensitively matches nothing existing — only for
  users with write permission (`useAuth().canWrite`). A new item appears immediately as a chip
  (value `create:<název>`, `CREATE_PREFIX` — the colon doesn't occur in a base32 UID; the shared
  helpers `pendingValue`/`pendingName`/`pendingOptions`/`hasPending`/`resolvePending` live in
  `lib/pendingCreate` and are also used by `BatchActionBar` and
  `useUploadOrganize`) and **is created
  only on Apply**: `resolvePending` first does `POST /albums`/`POST /labels` (defaults: empty description,
  non-private; priority 0), the fresh UID is swapped into the form and options — so a retry doesn't create a
  duplicate — and only then does the batch go; a canceled dialog creates nothing. A failed creation prints the server's message
  (`bulkEdit.createError`) and doesn't send the batch, the selection stays; when the batch fails only after creation,
  `bulkEdit.createdButApplyFailed` says the items already exist and only the assignment failed),
  **Metadata** (set/clear the description + **Datum pořízení**, `TakenAtField`), **Poloha**
  (set/clear coordinates; above the `lat`/`lng` fields on `set` sits **the same `PlaceSearch`** as in the detail
  editor — it fills only those two fields, so the sent batch is the same as if someone typed the coordinates
  by hand) and **Příznaky** (private, archive, **Knihovna** = `hide`/`unhide`, favorite); the set/clear
  pairs remain separate modes. The Knihovna select is the bulk half of hide-from-library and the one that
  actually solves the stated problem (fifty document scans at once, not one); it carries a `Form.Text`
  hint naming `hidden:yes`, and it is deliberately **not** toned danger — nothing is deleted and the
  photos stay in their albums and labels. **Destructive choices** (removal from an album/label, archiving)
  are in the danger key (`destructive` chips, `text-danger` label, `border-danger` select). Below the form is
  **`PendingChanges`** — a `.kk-surface` panel that says sentence by sentence what apply will do, and **how many
  photos it affects** (destructive rows in red + `visually-hidden` „(destruktivní)"; `aria-live`).
  `TakenAtField` re-dates the whole selection (`set_taken_at`): first the **grain** (přesné datum /
  měsíc a rok / jen rok / desetiletí), then the narrowest control that can state it (`type="date"`,
  `type="month"`, a bounded `type="number"` year, a decade **select** labelled „1970–1979"
  via `lib/takenDate` `formatDecade`). The grain leads because it is the real decision — a box of scans is
  dated „1974" far oftener than to the day, and a form opening with a date picker would make the honest
  answer the awkward one. The value's shape follows the grain and is validated client-side
  (`bulkEdit.takenAt.invalid`) before the batch goes; a grain change clears the value. The summary row is
  toned **destructive** (the old date is overwritten, and on some of the selection it may have been the real
  one) and a `Form.Text` says the originals and their EXIF are untouched.
  **The last choice of the same select is „Datum je neznámé"** (`TAKEN_AT_UNKNOWN`, sends `clear_taken_at`):
  it hides every value control — there is nothing to type — and swaps the hint for one saying the current
  value is set aside and can be put back from the photo's detail. It is deliberately a mode of the date
  control and **not** a button in `BatchActionBar`: losing a date is destructive enough to belong behind the
  dialog's confirmation, next to the other date choices. Its summary row is toned destructive too — the
  photos lose the date they are filed under, even though the value itself only moves to
  `taken_at_before_unknown`.
  A selection **over `LARGE_SELECTION` (50) photos** requires **explicit confirmation**: the first Apply only
  opens a danger alert („Ano, použít na N fotek" / „Zpět"), and **any form change revokes the
  confirmation**. **A capture date is confirmed at any selection size** and names itself — „Nastavit rok 1974
  pro 36 fotek?" (`bulkEdit.confirm.takenAt`): it overwrites the one fact the whole library is ordered by,
  on photos whose current dates this dialog does not show. Declaring it unknown goes through the same step
  with its own wording (`bulkEdit.confirm.clearTakenAt`), which says how many photos lose their date **and**
  that the value is kept and can be put back. Client-side coordinate validation + "at least one change" stays; after applying,
  a **per-photo result summary** from the response. A failed request **prints the server's message**
  (`ApiError.message` — a conflicting operation, too large a batch), otherwise a generic `bulkEdit.applyError`;
  the selection stays untouched so apply can be retried. The optional prop **`prefill`**
  (`BulkEditPrefill{addAlbums?,addLabels?}`, memoized — a new reference would reset the form)
  prefills the add fields on each opening (`/expand` puts the expanded collection there); `onDone` receives
  **`BulkEditOutcome{operations,result}`** — what apply actually sent and per-photo results — so
  the page can edit the list in place instead of refetching),
  `pages/` (`LoginPage` (username + password in a Superhero card; `errorKeyFor` maps the failure to
  a sentence — 401 → `login.errorInvalid`, **403 → `login.errorPendingApproval`** (the account
  registered and is waiting for an administrator: the password was right, so blaming it would send the
  reader off retyping one that works), 429 → `login.errorRateLimited`, **`NetworkError` →
  `login.errorOffline`**, anything else → `login.errorGeneric`. The network branch is the one that
  matters: a request that never left the device had nothing judge the credentials, and the generic
  "sign in failed, try again" used to land there, so an offline phone told its owner to retype a
  password they had not forgotten. When `useAuth().status` is already `unreachable` the card shows
  `login.offlineNotice` up front (a `role="status"` warning, `data-testid="login-offline-notice"`,
  yielding to a submit error once there is one) **and drops `autoFocus`** — raising the phone
  keyboard for a form that cannot succeed is an invitation to type. **Under the form** sits the way to
  registration — `login.registerPrompt` + a `Link` to `/register`, `data-testid="login-register-link"` —
  rendered **only** while `useRegistrationOpen()` says `open`: a closed instance, and one that could not be
  asked at all, shows nothing there),
  `RegisterPage` (route `/register`, **public like sign-in** — nobody registering has a session yet — and
  laid out as the same Superhero card, one column, so the two read as one flow): username, display name,
  e-mail, password, password again and the **registration word** (the instance's shared registration
  secret, `register.secretHint` explaining in one line that it is the word somebody who already uses
  Kukátko told them). All six are required. The two passwords are compared **here**, before anything is
  sent: the server never sees the second one, so a typo in it is the one rejection this form can word by
  itself. `useRegistrationOpen()` decides what the card holds at all — a spinner while
  `GET /settings/public` is being asked, and on a **closed** instance a plain explanation plus the way back
  to sign-in (`register.closed`, `data-testid="register-closed"`) instead of a form that would refuse every
  submit after it had been filled in; a probe that **failed** still gets the form, because "we could not
  ask" is not "the door is shut" and the server words a refusal better than a failed probe could.
  `rejectionFor` maps a refusal onto **the field that caused it**: 409 → the username is taken, a 403 whose
  message names the secret → „Tohle není správné registrační slovo" **under the secret field** (never a
  server error — the word is the one thing the reader can fix), a 403 without it → the instance is closed,
  400 naming email/password/username → that field, 429 → the per-address budget, `NetworkError` → the
  connection, anything else generic. A field rejection is rendered under its own input (`isInvalid` +
  `aria-invalid` + that group's `Form.Control.Feedback`), a refusal about the attempt as a whole in an
  `Alert` above the form. Success **replaces** the form with the confirmation
  (`data-testid="register-done"`): the account exists, it is waiting for an administrator, a mail went to
  the address — and **nobody is signed in**, because there is no session to hand out until somebody
  approves it, and a sign-in attempt meanwhile is exactly the 403 `LoginPage` words as
  `login.errorPendingApproval`. Texts in `register.*`; `RegisterPage.test.tsx` covers a successful
  submission, mismatched passwords, a wrong secret, a taken username and the closed state,
  `PasswordResetPage` (route **`/password-reset/:token`**, the landing page of the one-time link
  `POST /admin/users/{uid}/password-reset` mints — **public like sign-in and registration**, because
  whoever follows it is locked out of the very account it belongs to, and laid out as the same
  one-column Superhero card): the link is checked **before** there is anything to fill in
  (`fetchPasswordResetStatus`, a spinner while it is asked, `data-testid="password-reset-checking"`) —
  an empty form that every submit would refuse is worse than no form. A **usable** link greets the
  person by the display name the status carries (`passwordReset.greeting`) and asks for the new
  password twice; the two are compared **here**, before anything is sent, since the server never sees
  the second one, and the length rule is stated up front in the same words the account screen uses
  (`passwordReset.passwordHint`, `MIN_PASSWORD_LENGTH`). An **unusable** one — unknown, already used,
  expired or belonging to a blocked account, which the backend deliberately answers alike — gets one
  explanation and the way to sign-in (`passwordReset.invalidTitle/invalidBody`,
  `data-testid="password-reset-invalid"`), and says nothing that would tell a stranger whether the
  account exists. A **404 at submission time** (the link died between opening the page and sending it)
  is turned back into exactly that explanation rather than a rejection of what was typed; 400 lands
  under the password field, 429 and `NetworkError` in an alert above the form. A **failed check** is
  its own state (`data-testid="password-reset-unreachable"`, a retry button re-asking the endpoint):
  "we could not ask" is not "the link is dead", and the wrong one of those sends somebody off
  requesting a replacement they do not need. Success **replaces** the form with the confirmation
  (`data-testid="password-reset-done"`): the password is changed, **every other session of the account
  is gone**, and the next step is to sign in — nobody is signed in automatically, because the call that
  set the password is the same one that ended every session there was. Texts in `passwordReset.*`;
  `PasswordResetPage.test.tsx` covers the check-before-form state, a usable link, a successful change,
  mismatched passwords, a dead link on opening, a link that dies between opening and submitting, a
  server-refused password and an unreachable check,
  `AccountPage` = identity/role, **the Jazyk section** (`LanguageSwitcher` +
  a hint, `account.language*`) and changing your own password, **plus the app's technical status**
  (`GET /healthz` badge + version, without the commit hash) in a small muted row at the bottom — status and language
  came here from elsewhere (from the home page and the navbar respectively): they belong where the user looks for them, not
  in front of the photos nor in a prime spot in the bar; **plus the way to `MyActivityPage`** (the
  `account.activity.*` card linking to `ACTIVITY_PATH`) — "what I did" is a fact about the signed-in user, so its
  entry point lives here rather than as another item in the already crowded nav bar,
  **plus `MySubjectCard`** (`components/account/`, directly under the identity card — the first block says
  which *account* this is, this one says which *person*): the account's link to a subject of the library.
  Nothing is chosen → the app's ordinary subject typeahead (`AddAutocomplete` over `useSubjects`, the same
  shape as naming a face or merging two people, and deliberately **without** `onCreate`: an account belongs
  to somebody the library already knows). Something is chosen → the name, linked to that person's page, and
  an „Zrušit propojení" button. Above the empty field — **before** the decision, not after the save — sits a
  warning that linking **publishes that person's cover photo next to every comment the account has written**,
  and that the „soukromá osoba" flag hides nothing today. Saving goes through `setMySubject`
  (`PUT /auth/subject`) and then `useAuth().refresh()`, because the menu entry and the avatar both read the
  link off the session rather than off this card. A link whose person has since been deleted reads as
  „Propojená osoba už v knihovně není" instead of a blank. Tests: `MySubjectCard.test.tsx`,
  **plus `ApiTokensCard`** (`components/account/`, below the password — the other credential this one user
  owns): the personal `kkt_…` bearer tokens a script, `kukatko ctl` or an agent signs in with, until now
  reachable only by `curl`. It lists the caller's own tokens (name, the public prefix `kkt_<id>_…` so a token
  found in some config can be matched to a row, when it was made, when it was last seen — or „Zatím nepoužit"
  — and an expiry, badged once it has passed), mints one from a name field, and revokes one behind a
  `ConfirmModal` that **names the token**. A **revoked token is not shown at all** (the API keeps the full
  history; a credential that can never authenticate again is not a row worth reading), and revoking removes
  the row optimistically, restoring it with an alert if the request fails. A fresh secret appears **once**, in
  a `warning` alert with a read-only field (select-on-focus + a copy button that flips to „Zkopírováno") and
  the sentence that it will never be shown again; dismissing it is final. The list then refreshes, so the new
  token stands among the others. Errors are told apart: 400 → name it, 429 → the creation rate limit (shared
  with login) explained as "try again in a moment", anything else generic; a load failure gets an `ErrorState`
  with retry. **A 403 — on the listing or on the creation — switches the whole section to a read-only
  explanation** (`account.apiTokens.forbidden`) instead of leaving a form that answers every submit with an
  error; every token endpoint is behind plain `RequireAuth` today, so this is the guard for a backend that
  changes its mind, not a client-side role gate. Texts in `account.apiTokens.*`, the empty state links to the
  help chapter via `API_TOKENS_HELP_HREF` = `/help#help-api-tokens`,
  `HelpPage` = **user help** (route `/help`, **no role gate** — every logged-in user sees it;
  the link is in the user menu under the name, the item „Nápověda" with a `question-circle` icon): a reading column
  with a short **table of contents** at the top and an `Accordion` (collapsible sections, open by default) that in plain
  language explains browsing, search, albums, labels, favorites/rating, people and faces, duplicates,
  shot variants (stacks), the map and places, deletion+trash, import, **roles** (what each role may do), the
  account, and **API tokens** (`help.sections.apiTokens.*`, id `api-tokens` → the anchor `#help-api-tokens`
  the account page's empty state links to: what a token is, that its secret is shown exactly once, that it
  carries the reader's own permissions, and to revoke one that is no longer needed). Texts
  in the new top-level namespace `help.*` (cs/en); the first `Accordion` in the app. The **Search chapter
  teaches the query language** (`SearchQueryChapter`): `help.sections.search.queryTitle`/`queryIntro`, three
  ready-made queries as a `<dl>` (`QUERY_EXAMPLES` — literal query text, the same in both languages, one
  sentence each under `help.sections.search.example.*`), and then **`SearchQueryReference` itself**, not a
  retelling of it — a feature missing from help effectively does not exist for a reader who never presses
  the `?`, and a second hand-written copy of the syntax would drift the day a filter is added. At the foot the
  `BuildInfo` block (`help.version.*`, a named `region`) gives the **build in full** — the version plus the
  commit as a real `<a href>` (`target="_blank" rel="noopener noreferrer"`, as the footer's GitHub link
  does) into the public repository, which is what
  the user menu deliberately leaves out. A development build (`dev`/`none`) shows `dev` **and no link**
  (`commitUrl` refuses anything that is not a hex sha); with no capabilities answer the block is absent
  altogether,
  `LibraryPage` = the main photo library **and at the same time the app's home page** (route `/`):
  `FilterBar` above a virtualized infinitely-scrolling
  grid, loading/empty/error states, the whole view (filters+sort) in the URL, hearts
  on the tiles (favoritable; rating and pick/reject are only in the photo detail) — the heart and the
  `f` shortcut share **one** state per photo: the page holds the optimistic overrides (cleared on every
  refetch) and the tile reports its own flips back through `onFavoriteChange`, so `f` after a heart
  click un-favorites instead of repeating the click, **`SlideshowStart`**
  (a Promítání button + a duration estimate, the photo count comes from `total`),
  **two different empty states** — with active filters „Nenalezeny žádné fotky", whose hint
  **lists the active filters** (`buildChips(..., {facets, includeQuery: true})` joined by ` · `,
  album/label by title, not UID) and offers to clear them with one button,
  without filters „Zatím tu nejsou žádné fotky" with a CTA to `/upload` (editor/admin; a viewer gets only
  an explanatory sentence), distinguished via `hasActiveFilters(view)`,
  `LibraryRedirect` = a shim for the retired route `/library` **and its whole subtree**
  (`/library/*`): `<Navigate replace>` to `/` with the `search`+`hash` preserved literally
  (old bookmarks and links work, `replace` prevents a Back bounce); the subtree catches the addresses
  inherited from the **previous instance** whose domain this one took over — it kept its
  entire UI under `/library/…`, so `/library/login` and friends used to hit the 404 page, and did so
  *after* a successful sign-in (the guard returns the visitor to the address they asked for),
  plus **the timeline** (`TimelineScrubber`) beside the grid for quick jumps to a month — the grid
  is a **window** over the whole result (`useWindowedPhotos`; `gridRef`+`onRangeChanged` drive both the
  highlighted month and `ensureRange`), so a jump is `scrollToIndex({index,align:'start'})` to the month's
  absolute index plus the one page that lands there — **the cost does not depend on how far it jumps**.
  The month rides in the URL as `at=YYYY-MM` (via `useSearchParams`, deliberately **not** a `LibraryView`
  key: it filters nothing, so it must not reach the API nor be stored in a saved search — and `writeUrlState`
  dropping it on any filter change is right, a new filter renumbers every position). Shown only for the
  default newest sort and outside selection (`selection.count === 0`),
  plus for editors **a modern multi-select** — `useBulkEdit({hoverSelect:true})`: each tile has
  a corner checkbox (hover; Shift+click a range), **no „Vybrat" button** is needed anymore, and
  once something is selected, **`BatchActionBar`** slides up (a floating bottom bar: album/labels/favorites/
  archive/download/stack/more edits via the bulk API + toasts; on success `reloadKey` = **a background
  refetch, the grid doesn't flash to a skeleton**). Esc clears the selection,
  plus a **Uložit pohled** button (`SaveSearchModal` →
  `createSavedSearch` with the current view object as `params`) — live for **every** role, a saved search
  being personal like the favourite heart, and since 2026-08-08 carrying the same `savedSearches.saveViewTitle`
  one-liner the search header gives it, so it cannot be mistaken for one of the editor controls it stands next
  to (see `UX_RESEARCH.md` **N14**).
  **On a phone the page spends no row on a heading.** „Knihovna" over the photo wall repeated what the
  bottom tab bar already highlights, and the two view actions beside it (Promítání, Uložit pohled) are
  occasional; together they were a fifth of the first screen. Below `md` (`useIsNarrowViewport`) the
  heading renders `visually-hidden` — the page still opens with an `h1` for a screen reader — and the two
  actions are handed to `FilterBar` as **`mobileActions`**, which hosts them at the foot of the filters
  drawer. Both are built once into a `viewActions` fragment and rendered in exactly one of the two places,
  so neither button can end up in the document twice,
  `SavedSearchesPage` = `/saved` (any logged-in user, reached from the „Procházet" nav group as well as
  from the dropdown on `/search`) „Moje uložená hledání": a list of the current
  user's saved views, each link opens the exactly restored view (`savedSearchHref`), plus
  renaming (`SaveSearchModal`) and **optimistic deletion** + empty state; the row is phone-proof —
  the name truncates (`kk-min-w-0` + `text-truncate`) and rename/delete keep only their glyph below
  `sm` (`aria-label` = the same word, so the accessible name doesn't change with the viewport),
  `FavoritesPage` = `/favorites` the current user's favorites: the same grid/filters as the library
  scoped to `favorite=true`, hearts to remove from favorites in place (favoritable),
  the tiles carry the scope in the detail link (`detailQuery` with `favorite=true`) → Esc/Back/prev-next from a photo returns here,
  for editors **hover-select** → the shared **`BatchActionBar`** (the library's full set of actions,
  `onSelectAll` picks every loaded tile); a bulk removal from favorites drops the photo from the list
  (the selection is cleared **before** the refetch, so no photo that vanished from the grid stays in it),
  `AlbumsPage` = `/albums` a grid of album cards + `Nové album` (editor/admin), **split by `type`**
  through `AlbumFilterBar` + the pure `lib/albumBrowse`: the API returns one flat list in which more than
  half the albums are machine-made (month folders, moments, places), so the page opens on **Moje alba**
  (`album`) and leaves the rest to **Podle měsíce** (`folder` + `month`) · **Momenty** (`moment`) ·
  **Místa** (`state`); albums with `photo_count = 0` are hidden until the „I prázdná" switch asks for
  them, a name search filters over the **stored and the displayed** title, and the sort selector offers
  **Od nejnovějších** (the backend's own ranking — by the newest photo, undated/empty at the end —
  which the page **keeps** rather than reorders, since only the server can compute it) / **Podle názvu** /
  **Podle počtu fotek**. The whole view (`type`/`q`/`sort`/`empty`) lives in the **URL** (`ALBUMS_DEFAULTS`
  are omitted from it), so Back steps through the sections and a link carries the exact view; only the
  live-typed query replaces its history entry. After creating an album it **reloads the list**
  (`useReloadKey`) instead of locally appending to the end — where a new album belongs is known only to the
  server — and resets the view **with empty albums shown**, so a fresh (photo-less) album isn't swallowed by
  the default filter. Filtering everything away shows `albums.noMatches` (a hint pointing at the switch when
  the filters actually dropped something) while the section badges keep saying where the matches are;
  the grid is **virtualized** (`TileGrid`, minTile 160 / gap 12 — the skeleton's geometry), so a large
  collection puts only the visible rows plus a buffer into the DOM and starts only their cover loads,
  while the layout stays exactly the one the plain CSS grid drew,
  `AlbumDetailPage` = `/albums/:uid` a header + a **Promítání** button (for everyone) + editor actions
  (edit/delete/select) above
  a photo grid scoped to the album (`useWindowedPhotos` with `album` in the params + `FilterBar` +
  URL state) — under the heading it renders the album's **`description`** (`.kk-prose-note`: a readable
  measure, `white-space: pre-line`, so the line breaks the writer typed survive), which was stored,
  editable and shown to nobody until now;
  the header row is a **`HeaderActions`** group: **Promítání** stays inline at every width, while
  **Stáhnout ZIP**, **Upravit** and — behind its own divider, in danger styling — **Smazat** fold into
  the „…" overflow menu on a phone, so the header keeps to one row instead of wrapping into two or
  three; on desktop the actions stay inline exactly as before, and either way the RBAC gate is the
  same `canWrite` on the same buttons;
  an album is **always chronological** — the backend pins the sort *key* to capture time — so the only
  choice is the direction: `FilterBar sortOptions={ALBUM_SORTS}` offers **Nejstarší** / **Nejnovější**
  and nothing else, the view rests on `ALBUM_DEFAULTS` (oldest first, so only the reversal shows in the
  URL), and a sort key from elsewhere (a stale link) is read back as the album's own default so the grid
  and the selector cannot disagree. There is no manual reordering. An album spanning **≥ 24 months**
  also gets the library's own **`TimelineScrubber`** beside the grid (`minSpanMonths`, hidden while a
  selection is being gathered), with the position in the same `at=YYYY-MM` URL param the library uses —
  which is why the grid is a *window* over the album rather than a growing prefix of it: a jump to 1936
  in an album of 781 photos costs one scroll and one page fetch;
  selection raises the shared **`BatchActionBar`** with the album's own
  actions merged in as `extraActions` — **Nastavit obálku** (enabled at exactly 1 selected) and
  **Odebrat z alba** (`danger`) — beside the full batch vocabulary (both removal and a successful edit
  **empty the selection**, so no
  UIDs of photos that vanished from the grid stay in it, and reload the grid via `reloadKey`); the tiles carry the
  album scope in the detail link (`detailQuery` with `album=uid`) → Esc/Back/prev-next from a photo returns to the album;
  the album's own header controls **stay visible** during a selection (the bar floats over the bottom edge),
  `LabelsPage` = `/labels` **a wrapping cloud of label chips** with counts + `Nový štítek`
  (editor/admin). It used to be a column of full-width rows: a label is one word and a number, so a
  real library's 113 of them ran a 5 730 px document with no controls at all, in which the alphabet
  alone decided what you met first — and dozens of numbered `DumNN` house-number labels occupied the
  entire start of it. Now the labels are chips (`LabelCloud` + `LabelChip`), the numbered families
  **fold into one expandable chip each**, and the page carries a name search + an ordering
  (`LabelFilterBar` over the pure `lib/labelBrowse`), defaulting to **most photos first** rather than
  to the alphabet. The whole view (`q`/`sort`/`open`) lives in the **URL** (`LABELS_DEFAULTS` are
  omitted from it), so Back steps through it and a link carries the exact one; only the live-typed
  query replaces its history entry. Searching everything away shows `labels.noMatches`; the loading
  placeholder is `ChipCloudSkeleton`, not the old row stack. Creating or renaming a label **expands
  the family its name lands in** (`withFamilyOpen`) — a new `Dum99` disappearing behind a folded chip
  the moment it is saved reads as a failed save. The row's three inline controls became a per-chip
  "…" `Dropdown` (**Přejmenovat** · the review-game action · **Smazat** behind a divider), because
  repeated on a hundred chips they would cost more width than the names — the width the cloud exists
  to save. The **review-game setting** (`label.review_enabled`, editor/admin only) reads there as the
  action it is (`labels.review.enable`/`disable`) instead of as a switch, and a label the game skips
  carries a muted `slash-circle` on its chip plus `labels.review.off` in the link's `aria-label`, so
  the state is still visible without opening the menu. It `PATCH`es the label with its name and
  priority carried across unchanged, flips the chip optimistically and **rolls back plus shows
  `labels.actionError` if the save fails** — a chip left flipped after a failed save would tell the
  operator a label is out of the game when it is not. The setting lives here and deliberately **not
  inside the game**: "don't ask me about this label again" mid-round is a decision about a whole label
  taken at the worst moment, and "not this photo" is already a per-photo rejection,
  `LabelDetailPage` = `/labels/:uid` a photo grid scoped to the label (`useScopedPhotos` + `FilterBar` + URL);
  the tiles carry the label scope in the detail link (`detailQuery` with `label=uid`) → Esc/Back/prev-next from a photo
  returns to the label; + a **Promítání** button + for editors **hover-select** → the shared
  **`BatchActionBar`** (the library's full set of actions, `onSelectAll`; refetch on success),
  `SearchPage` = semantic/hybrid/fulltext search: a prominent debounced (350 ms)
  search field + a mode toggle (`q`+`mode` in the URL), the same virtualized grid as the
  library + the shared `FilterBar` (without query/sort), `degraded` → a non-blocking notice
  (sidecar offline) **beside the mode selector, raised from `useSearchMode().downgraded` before a search
  runs** rather than only from the server's reply: with `semantic_search:false` the request already went out
  as `fulltext`, so there is nothing to wait for and nothing to announce afterwards. The same flag `disabled`s
  the **Semantic** option (`title` = `search.semanticUnavailable`) — hybrid stays, full-text is a fair half of
  what it promises — while the URL keeps the picked mode, so it applies again the minute the box is back.
  The `FilterBar` count is **omitted entirely until a query exists** (`total={hasQuery ? total : undefined}`):
  „Počet fotek: 0“ over „Zadejte hledaný výraz.“ reads as an empty library, not as an unasked question.
  idle/loading/empty/error states (an empty result **repeats the query** —
  `search.empty.hintQuery` „Pro «dotaz» jsme nic nenašli…“ — and advises loosening the narrowing; the error is
  `ErrorState` with Retry); the field speaks **the search language**
  (`q` = free text + `klíč:hodnota` filters, grammar in docs/API.md „Vyhledávací jazyk (q=)“;
  parsed exclusively by the backend): the input is `SearchQueryInput` (`components/search/`) — a combobox
  whose dropdown draws (via the presentational `SearchSuggestions`) **one list mixing up to two kinds of row**:
  **nedávno hledané** whenever the box is focused — the whole history while it is empty, and once something is
  typed those entries that **start with it** (folded via `foldText`, so `snih` finds `sníh`, minus the one that
  *is* it, at most `HISTORY_MATCH_LIMIT`=5), because a reader who searched for `svatba` last week typing `s`
  was previously offered `square:`/`subject:` and nothing of their own (`useSearchHistory`, now active on
  focus rather than only on an empty box, so matching costs no request per keystroke) — followed by
  **filter keys** while the trailing token could still become one (`suggestFilterKeys`/
  `applyFilterKey` + `FILTER_KEYS`), or **filter values** once a completable key is typed
  (`suggestFilterValues`/`matchFilterValues`/`applyFilterValue`, lists from `useFilterValues`).
  History rows come **first** and wear `clock-history`, completions follow wearing `funnel` (keys) or their
  photo count (values) — a query already run is a stronger proposal than a word half-way to a filter, and the
  glyph says which is which; the listbox names itself `search.history.label`/`search.keySuggestions`/
  `search.valueSuggestions` when it holds one kind and `search.suggestions` („Návrhy") when it mixes them.
  „Vymazat historii" (`search.history.clear`) rides only a list that is **nothing but** history — under rows
  that also complete filters it would read as clearing those. **No panel at all** when there is nothing to
  offer — the page autofocuses this box, so a first-time reader would otherwise meet an empty popover over the
  filters.
  Only `album:`, `label:`, `person:`/`subject:` complete values — a number or a date has no list to propose;
  matching is **prefix + diacritics-insensitive** (`lib/text` `foldText`) and ranked by photo count, the picked
  value is inserted **properly quoted** (`quoteFilterValue`) with a trailing space, and a value token with no
  match still opens the dropdown with „Nic neodpovídá" rather than vanishing mid-word. Arrows move,
  Enter/Tab accept, Esc closes — but **Enter with nothing highlighted belongs to the form**, and **no panel
  rests on a row**: a highlight exists only once the reader has arrowed into the list, so `svatba u` + Enter
  searches for those two words instead of completing `u` into `uid:` — Czech words and endings prefix the
  English keys constantly (`po`→`portrait:`, `ta`→`taken:`, `la`→`label:`). **Tab** is the completion key and
  accepts the first **completion** untouched, skipping the recent searches above it (a whole query is not
  something to complete a word into; with only history on offer Tab leaves the field as usual), while a
  highlight the reader put on a history row is honoured. Beside the label sits `SearchQueryHelp`
  (a `?` button → a modal holding
  **`SearchQueryReference`** — the reference lives in its own component because `/help`'s Search chapter
  renders the same tables inline, so the syntax has one source of truth; operators and filters
  with examples, rows from `QUERY_HELP_ROWS`/`QUERY_HELP_OPERATORS`, texts `search.help.*` cs+en; the
  `?` keeps its small glyph but gets a 44px square on touch via `kukatko-tap-target-touch`; the
  modal is `fullscreen="sm-down"` with both tables `responsive`, and a multi-key row renders every key
  as its own `text-nowrap` `<code>` (the cell wraps between keys, never inside one) — whatever still
  doesn't fit scrolls in its own wrapper instead of pushing the dialog past a 320px viewport),
  and `unknown_tokens` from the response (`PhotoListResponse.unknown_tokens` → `usePaginatedPhotos`
  returns `unknownTokens`) → `UnknownFiltersAlert`, a non-blocking info hint „těmto filtrům nerozumím“ above
  the grid — the same component, and therefore the same wording, the library raises under its own filter bar;
  beside it **`QueryNoticesAlert`** (`components/search/`) renders the response's `notices` — the reason an
  understood query was answered with **nothing** rather than with everything. The only code today is
  `person_me_unlinked` (`person:me` from an account that has not said which person it is), shown as a
  sentence plus the one link that fixes it (`/account`); an **unknown code is ignored**, never printed raw,
  so a client older than the server does not show the reader an identifier. Both hooks
  (`usePaginatedPhotos`, `useWindowedPhotos`) carry `notices` alongside `unknownTokens`, so the library grid
  and the search page explain an empty result identically. Tests: `QueryNoticesAlert.test.tsx`;
  a pure filter query returns `mode: "filter"` (`EffectiveSearchMode`); the tiles carry the search scope in the detail link
  (`detailQuery` with `q`+`mode`) → Esc/Back from a photo returns to the search (sorted results, not the library with `q`
  as a substring) and prev/next pages the same results, plus above the grid a **cross-entity section**
  (`GlobalSearchSections`) with chips of matching albums/people/labels (grouped `GET /search/global`), so a
  text query surfaces non-photo entities too — **and a pasted uid turns that section into a „Přejít na" card**
  for the resolved entity (or a plain „nothing matches this id" alert), which is why the grid below it may
  legitimately stay empty; plus in the header **`SlideshowStart`** (scope `{mode}`,
  so the slideshow plays **the search results**, not the library filtered by the substring `q`)
  and **the single entry point to saved searches**
  (`SavedSearchesDropdown` — list, open, „Spravovat" → `/saved`) beside the **Uložit pohled** button
  (`SaveSearchModal` — `params` carries `mode` too, so restoration targets `/search`);
  the page **remembers a query only when it is submitted** — the form's `onSubmit` (Enter) and `onRun`
  (a recent search picked from the box) both call `recordSearch` from `useRecordSearch`, while the searches
  the 350 ms debounce runs along the way are not submissions and are not remembered;
  plus for editors **hover-select** over the results → the shared **`BatchActionBar`** (the library's
  full set of actions, `onSelectAll`; on success the search
  replays via `reloadKey`); changing `q`/`mode` is a different result set, so it **leaves selection mode**
  (filters that only narrow the same search keep the selection, just as in the library),
  `UploadPage` = multi-upload (drag-and-drop + gallery/camera on mobile, **mobile-first**), laid out as
  **one stage on screen at a time**, and the upload **starts by itself**. The stage is not a mode anyone
  sets — it is read off the queue: empty → ① *vybrat*, files not all settled → ② *nahrávám*, everything
  settled → ③ *hotovo*. So nothing can be out of sync: picking files puts bytes on the wire and moves the
  page in one step, adding more mid-flight keeps it where it is, removing the last file (or **Nahrát další**,
  which only empties the queue) walks it back to ①.
  ① `UploadStagePick` = the headline, the `DropZone`, and an `UploadActionBar` with **Vyfotit** +
  **Vybrat fotky**. No album fields, no queue, no empty third step explaining itself.
  ② `UploadStageUploading` = the album/label picker (`UploadOrganize`) as the body — the one useful thing
  to do while the bytes go up — with the demoted `UploadQueuePanel` under it, and the batch's progress in
  the action bar together with **Přidat další soubory**.
  ③ `UploadStageDone` = **one sentence** of outcome („Nahráno 20 fotek, přidáno do: Pouť 2026.“) and one
  primary action, **Zobrazit je v knihovně** (`/?sort=added`, via `LIBRARY_PATH`), with **Nahrát další**
  beside it. The sentence only claims the album once the assignment has actually come back; a batch with
  failures **leads with the failures**, makes **Opakovat neúspěšné** the primary action and brings the
  per-file list back (that is where the reason, the per-file retry and the errors-only filter are). A batch
  that landed in no album says so and offers the picker rather than leaving the photos untagged; with
  *nothing* landed there is no picker and no „everything else is in your library" — neither would be true.
  **Nahrát další keeps the chosen albums and labels**: the next batch is almost always more of the same event.
  This works with no new backend behaviour because `useUploadOrganize` assigns when the batch **settles**
  and re-arms on every change: all recognized photos (new **and** duplicate `resolvedUids`) go in one
  `POST /photos/bulk`, so a pick made *during* the upload — or after it finished — is applied just the same
  (state „přiřazuji…“ and a **retryable** error live in ③, since the photos are uploaded and only the
  assignment failed). With no selection no call is made.
  Leaving mid-upload loses the queue, which lives only in this tab, so `useLeaveGuard` holds an in-app link
  back behind a `ConfirmModal` and lets the browser warn on a tab close.
  Two more ways in besides the picker: **paste** anywhere on the page (`usePasteFiles`) and
  **`?share=<id>`**, the phone's share sheet — the files are already in the browser's cache by then, so
  the page collects them (`pwa/shareTarget` `collectSharedFiles`) into the ordinary queue, which lands it
  **directly in ②, already uploading**, and drops the parameter (`replace`) so a reload cannot ask for a
  share already spent. A `collected` ref
  makes the collection one-shot, since StrictMode invokes the effect twice and collecting **consumes**
  the cache entries. It then says what it took (`share.notice.staged`, a Czech plural), what it left out
  by name (`share.notice.rejected`) or that the share held nothing usable (`share.notice.empty`),
  `ShareTargetPage` = `/share-target`, where the share sheet lands (see the PWA section below): a junction,
  not a destination — an editor with a staged share is `Navigate`d (`replace`) to `/upload?share=<id>`,
  a **viewer** gets a plain explanation that their account may not upload and their files are discarded
  on the spot (`discardSharedFiles`) rather than left to expire, and **no id at all** — a worker that
  could not read the POST, or none installed, so the server answered it with the shell — gets the honest
  „the files did not come through" plus a link to the picker. It sits inside `RequireAuth` but **outside**
  the editor gate, so an unauthenticated sharer goes through login and comes back to this exact URL,
  which is what makes a share survive the round trip,
  `ImportPage` = `/import` (maintainer only) the import console, now **read-only**: the background queue
  state (`GET /jobs/stats`), the recorded per-photo/per-file failures (`GET /import/failures`) and a
  **run history** table (`import_runs`: source/start/end/status/counts/error) — rendered through the
  shared `RecordTable` (`size="sm"`), so on a phone the six columns become **one stacked card per run**
  instead of a sideways scroll; the error keeps its `text-danger small` on the *value* (a `cellClassName`
  would be desktop-only) —, with the imported/updated/skipped/**deduplicated**/failed counts per run (the
  `deduplicated` badge appears only when the run has any, since older runs have no such key: it counts
  source photos whose content was already catalogued under another source photo). It polls every 3 s and
  is self-gated on `canImport` (= maintainer).
  There is **nothing to start from the page**: the only import left is `kukatko import dir`, which reads a
  directory on the server's disk and therefore runs from the CLI, and the one-off import
  closed in August 2026 and was removed together with its start buttons and its completeness-check card.
  Its runs stay in the history as the catalogue's provenance record, so `RunSource` in `services/import.ts`
  is `'folder' | 'photoprism' | 'photosorter' | 'photosorter_feeds'` and each has a label under
  `import.source.*` — a raw i18n key in that table would be worse than useless,
  `MaintenancePage` = `/maintenance` (maintainer only) the library maintenance console: a **Spustit kontrolu** button
  (`GET /maintenance/scan`) → a summary of totals + a findings table (count + samples per class, or „knihovna
  konzistentní") — also through the shared `RecordTable` (`size="sm"` + **`hideHeader`**: the first column names
  the problem, so the desktop table stays headerless), so a phone gets **one stacked card per finding**
  (`maintenance.findings.problem`/`.count`/`.samples` are the card's labels — a card has no header row to read
  the values across from) —, repair checkboxes (thumbnails/embeddings/faces/hashes/import of orphans — annotated
  with the remaining count from the last check) → **Spustit opravy** (`POST /maintenance/repair`) with a result
  summary, plus the background queue state (`GET /jobs/stats` polls every 3 s) as progress; **every finding,
  the summary „drift" row and every queue state carries a quiet plain-language explanation** (without hovering) —
  `maintenance.findings.descriptions.*`, `maintenance.scan.summaryHint`, `maintenance.jobs.intro`
  and the shared `JobStateLegend` (total/queued/running/failed/**dead**) — so a maintainer knows what a count
  means and whether action is needed; plus the destructive card **`AuditPurgeCard`** (**Vymazat audit log**)
  with a retention choice (presets 3/6 months, 1/2 years or a custom number of days), a **confirmation step**
  (irreversible deletion) and a result `Alert` with the deleted count (`purgeAuditLog(olderThanDays)` →
  `POST /maintenance/audit/purge`), and the card **`NamelessSubjectsCard`**
  (`components/maintenance/NamelessSubjectsCard.tsx`, **Osoba bez jména**) — the importer-minted catch-all
  subject that sits first in `/people` owning 96 % of the library's faces, whose repair used to be reachable
  only over SSH: **Zkontrolovat** (`fetchNamelessSubjects` → `GET /maintenance/nameless-subjects`, read-only)
  renders the found subjects through the shared `RecordTable` (uid + „prázdné jméno", markers, faces, created),
  **Odpojit a smazat subjekt** goes through a confirmation that names both the loss and the undo file, and the
  apply (`detachNamelessSubjects` → `POST …/detach`) **saves the undo file to the user's downloads** —
  the response body *is* the file, and the backend schedules the detach only once it has gone out, so a refusal
  (409 „nothing to detach", 503 „not wired", any 5xx) renders as its own message and nothing was changed. The
  undo direction is a file picker (`restoreNamelessSubjects(file)` → `POST …/restore`, 400 → „not a usable undo
  file"); both destructive halves run in the job queue, so their `Alert`s report what was **scheduled** and
  point at the queue card below; self-gated on `isMaintainer`,
  `SystemStatusPage` = `/system` (maintainer only) the **admin dashboard**: auto-refresh (polling 5 s)
  `GET /system/status`, read top to bottom in the order the questions are asked — **Knihovna**
  (`LibraryOverview`), **Zbývá udělat** (`RemainingWorkPanel`), **Fronta úloh** (`JobQueuePanel`) and only
  then **Zdraví systému**, the card grid (DB, embeddings, backup, imports, **disk serveru**, **maps**,
  version). Everything comes from that one snapshot: there is no second fetch and no arithmetic in the page,
  so nothing on it can drift from what the backend counted. **Quick actions** — *requeue the dead letter*
  (`requeueDeadLetterJobs(jobType?)` → one `POST /jobs/requeue-dead`, whole or per type from the queue row),
  *run a backup* (`POST /backup`), a link to the maintenance check (`/maintenance`), and the import history
  (`/import`) from the imports card. The **imports and backup cards** report **how long ago** they last ran
  (`AgeLine`, `formatRelativeTime` with the exact stamp in the `title`) — an age is what a health readout is
  read for; the **imports card** reports the last `kukatko import dir` run (`imports.folder`), the only import
  that can still happen; the **disk card** is titled *Disk serveru* and says so in a caption, because it used
  to read as the library's size while reporting an almost empty originals directory (prod keeps the originals
  in R2 — what the library weighs is in the Knihovna section); **box offline** + pending embeddings → a
  highlighted message „doženou se po návratu"; **the Mapy card** (`MapsCard` over `status.maps`) shows the latest
  mapy.com status — `key_rejected` in red + what to do about it (swap the key in the mapy.com console), degradation
  in yellow, without a key „Nenastaveno" — and beneath it the **geocode credit line** (`GeocodeCredits` over
  `status.geocode`, the same metered mapy.com account): `spent / limit` for the current budget window plus when
  it refills, the numbers in yellow and the label switched to „Rozpočet vyčerpán, obnoví se" once nothing is
  left (the queued `places` jobs then wait for that instant instead of failing); it renders nothing when
  `budget_enabled` is false, i.e. no cap is configured; the queue panel carries the shared `JobStateLegend`
  (queued/running/failed/**dead**/done/**pending** = „Čeká na box") with a plain-language explanation of
  each state (`jobStates.*` + `system.jobs.intro`); **last on the page** comes the Oznámení card
  (`AnnouncementCard`, gated `isMaintainer`) — it used to sit above everything, which put a rare errand
  where the dashboard belongs — a textarea + a level `<select>` (info/warning) + **Zveřejnit**/**Zrušit oznámení**
  over `setAnnouncement`/`clearAnnouncement`, prefill of the current message via `fetchAnnouncement`, feedback via
  the same dismissible `ActionNotice` `<Alert>` pattern; loading/error/notice states, self-gated on `isMaintainer`,
  `UsersPage` = `/users` (admin **or** maintainer, `isAdmin`) **account management**: a user table (username, full name,
  **e-mail**, role, status, **person**, note, last login, created) over `GET /admin/users` — rendered through the shared
  `RecordTable`, so on a phone the ten columns become **one stacked card per account** and the row actions
  a full-width button row on the card instead of a sideways scroll away; the note column is `multiline`
  (it is written in a `<textarea>`, so its line breaks survive both layouts); the actions column is
  `cardHidden` and comes back through `cardActions` (`UserActions`, prop `stacked` = the card's grid items
  vs. the table cell's inline cluster); the **person** column prints the name of the subject the account is
  linked to, resolved through **one** `useSubjects` fetch for the whole roster (`linkedPersonLabel` is a plain
  function of that map, not a component with a hook, so thirty rows still cost one request) and an em dash
  when there is no link. The **e-mail** column goes through **`UserEmail`** (`components/users/`), which prints a real
  address as it stands but renders a **`.invalid` placeholder as „Bez adresy" + the line asking for a real one**
  (`isPlaceholderEmail`, the same last-label test the backend refuses to dial on — see `mailer.invalidTLD`): every
  message the app sends goes to this field, so "there is something in the column" and "this person can be reached"
  have to look different. The **status** column goes through **`UserStateBadges`** (`components/users/`): blocked
  (`danger`) and **waiting for approval** (`warning`, `approved_at === null`) are independent states, never one badge —
  an account can be both, and a waiting one is never painted the reassuring green of an active one, because it cannot
  sign in at all. Above the list sits **`PendingFilter`** (`components/users/`): a switch narrowing the roster to the
  waiting accounts plus **the count of them**, so an administrator who came for something else still notices that
  self-service registration left somebody standing at the door. It filters the **already loaded** list (the backend's
  `?pending=true` stays unused): the count needs every account anyway, so a second request could only make the two
  disagree — and the count is always of the whole roster, never of what is on screen. Both predicates live in
  `components/users/account.ts` (component-free, so the page can count and filter with them and Fast Refresh
  survives). The dialogs are **Nový uživatel**
  (username/password/**e-mail**/role/name/**person**/note) and **Upravit** (**e-mail**/role/name/**person**/note;
  username is `readOnly` `plaintext` — the backend cannot change it), **Změnit heslo** for another user (logs them out of all
  devices; the hash is never rendered anywhere) and **Povolit/Zakázat** behind a confirmation dialog
  (`setUserDisabled`). A waiting row additionally offers **Schválit** → a `ConfirmModal` (`variant="primary"`) →
  `approveUser`, whose answer replaces the row **in place** (`upsert`, no re-fetch) and reports through the page's
  own success alert; a blocked account's Schválit is off with `users.approve.blockedHint` on its **own** hint line
  (the backend refuses it with 409, and letting somebody in who still cannot sign in is half a decision).
  **Změnit heslo gained a sibling, „Odkaz na heslo"** → **`ResetLinkModal`** (`components/users/`): it asks first
  (issuing kills the account's earlier unused link), then `issuePasswordReset`, then shows the whole link in a
  **read-only, select-on-focus field with a Kopírovat button** (the same disclosure shape as `ApiTokensCard`'s
  `CreatedSecret`, so it can be dragged out where the clipboard API is denied), above it the line saying where it
  was mailed and that it lasts `PASSWORD_RESET_TTL_DAYS` days — or, for a `.invalid` address, that **nothing was
  sent and the link has to be passed on by hand** (`isPlaceholderEmail` again; promising a mail the backend refuses
  to send would leave the administrator waiting). The link lives in that component's state only, so closing the
  dialog takes it off the screen for good. **The two form dialogs are `scrollable fullscreen="sm-down"`** — on a phone the long
  form takes the whole screen and only its body scrolls, so Zrušit/Uložit stay pinned above the on-screen
  keyboard instead of under it; on `sm`+ nothing changes (the same centred 500px card, measured identical).
  Because their `<Form>` wraps header+body+footer, it also carries `MODAL_FORM_CLASS`
  (`d-flex flex-column overflow-hidden`): Bootstrap pins the footer by capping `.modal-content`'s height and
  letting `.modal-body` scroll, and a plain `<form>` in between would size to its content and push the footer
  off-screen anyway — the utility classes make it a shrinkable flex column (a scroll container's automatic
  minimum size is 0) and hand the cap through. The **Povolit/Zakázat** question follows `ConfirmModal` instead:
  `scrollable`, but a centred card on every screen — it has no inputs, so no keyboard can reach it.
  **The e-mail field is required in both dialogs** and is the one profile field the edit dialog stopped
  echoing back: the backend refuses an account without a usable address (`auth.ErrInvalidEmail` → 400,
  see `docs/API.md`), and since `PATCH` replaces the whole profile, an update that dropped it would be
  refused too. The dialog therefore prefills the stored address, submits it trimmed, and blocks its own
  submit while it is blank (`emailMissing`, message `users.form.emailRequired`) rather than letting the
  request go out to be rejected.
  Both form dialogs carry **`SubjectField`**: the same subject typeahead as the account page (pick-only, no
  `onCreate`) when nothing is linked, the chosen name with a „Zrušit" beside it once something is, and a hint
  saying that the linked person's cover photo then appears next to the account's comments. It is part of the
  **replaced** profile, so clearing it in the dialog unlinks the account.
  **Your own row has an off toggle** + a short explanation of why
  (`users.selfDisableHint`), **deletion is not offered** — an account is retired by disabling it, so the history
  (photos, ratings, audit) stays whole. **The maintainer boundary** (mirrors the backend
  `authorizeUserManagement`): the **maintainer** role may be granted only by a maintainer — the role
  `<select>` doesn't offer it to a non-maintainer at all (`ROLES.filter`, prop `isMaintainer`) — and a maintainer account may not
  be edited / re-passworded / disabled by a non-maintainer, so its three row actions go off with the hint
  `users.maintainerManageHint` (`canManage = isMaintainer || role !== 'maintainer'`). This is the one place
  the app keeps the buttons on screen instead of hiding them — and rightly so: it is **not** a role gate but a
  per-row boundary (this administrator may manage users, just not *this* one), which is exactly what the
  printed line beside the row says. `UserActions` renders all three via `ReasonedButton` with `reasonId`
  pointing at that visible line, so the sentence exists once and is reachable by hover, by focus and by eye
  on a phone; before, the `title` on a natively disabled Bootstrap button could be read by neither a mouse
  (`pointer-events: none`) nor a keyboard (out of the tab order). API validation errors map to a specific field
  (`fieldErrorFor`: 409 → username *unless* the message names the maintainer, 400 by keyword →
  password/**email**/role/note, otherwise a form-level alert), not to a generic banner. It lives — with the
  `ErrorKey` union and **`actionErrorFor`** — in `components/users/errors.ts`, shared by the page and by
  `ResetLinkModal`. **`actionErrorFor` is the mapping for a row action that carries no form** (approve, issue a
  link), where the statuses mean something else: 409 is never a taken username but always a blocked account
  (`auth.ErrUserDisabled`), 403 is the maintainer boundary and 404 a roster that has gone stale — each with a
  message saying what to do next. **The last-maintainer
  refusal** (409 `auth: cannot remove the last maintainer`, see `docs/API.md`) belongs to no input — the
  role `<select>` is only how it was triggered and the disable button has no form at all — so it renders
  as a form-level alert with `users.errors.lastMaintainer`, which says *why* and *what to do* (promote a
  second maintainer first). The row-action alert therefore holds an `ErrorKey`, not a boolean, so a failed
  **Zakázat** shows that explanation instead of the generic “action could not be completed”. States: a **skeleton** (`Placeholder` in the table) while loading,
  an error alert with **Zkusit znovu**, an empty state (`EmptyState`, practically unreachable — the bootstrap
  admin always exists, but must not crash) and a second one for a **filter that leaves nothing**
  („Nikdo nečeká"), which is a normal answer rather than an empty library; self-gated on `isAdmin`.
  Tests: `UsersPage.test.tsx` plus `components/users/{account,UserEmail,UserStateBadges,PendingFilter,ResetLinkModal}.test.tsx`,
  `SettingsPage` = `/settings` (admin **or** maintainer, `isAdmin`) **the instance's own settings** —
  the three values behind `GET`/`PUT /settings`: whether self-service registration is open, the shared
  secret it asks a newcomer for, and the Markdown greeting a person meets on their first sign-in. It is
  its own route rather than another section of `UsersPage`: that page is about accounts, this one about
  the instance, and it is long enough already. One `Form` over a local `Draft`, so **nothing is written
  until „Uložit nastavení"** — `isDirty(draft, saved)` drives both buttons (save + **Zahodit změny**),
  the „Máš neuložené změny" hint, and `useLeaveGuard(dirty)` + `ConfirmModal`, which holds an in-app
  link click back (and asks the browser to warn on a tab close) before the draft is lost. The secret is
  compared **trimmed** against the stored value, because the backend stores it trimmed: typing a
  trailing space and deleting it again must not leave the page claiming unsaved work forever.
  **The switch refuses to open registration while the secret is blank**, in the API's own words
  (`settings.registration.secretRequired` ↔ `settings.ErrSecretRequired`); `handleSubmit` repeats the
  check, because the field can be emptied after the switch is already on. **The secret is a readable
  text field** (`type="text"`, an eye button toggles it to `password`), never a write-only password
  box: reading it back to tell people what it is is the whole reason it is stored unhashed, and the
  helper line says out loud that this word is what keeps strangers out and that everybody in the
  community is expected to know it. The welcome text is a monospace `textarea` beside a live preview
  (`Row` + two `Col md={6}` — stacked, editor first, below `md`) rendered by the shared `Markdown`
  component — the one the welcome itself will render with — so the two cannot drift apart. A save reports
  `settings.saved`; a rejection repeats the **server's own message** (`ApiError.message`) rather than a
  generic apology, because a refusal on grounds this form does not know about is exactly what needs
  repeating. Loading spinner / `ErrorState` with retry, and a footer line with
  `formatDateTime(updated_at)`. Tests: `SettingsPage.test.tsx`,
  `AuditPage` = `/audit` (admin **or** maintainer, `isAdmin`) an **audit log**: a read-only table of records from `GET /audit`
  newest first (when/who/action/target/IP) — also through the shared `RecordTable`, so a phone gets **one stacked
  card per entry** —, the `details` JSON via an expandable block (`AuditEntryDetails`, `aria-expanded` +
  `aria-controls` → `detailsId(record)`: a `colSpan` row under the table row, the foot of the card on a phone;
  also shows `user_agent`). The raw payload wraps inside its own box (`.kk-audit-payload`:
  `pre-wrap` + `overflow-wrap: anywhere`, `overflow-x: auto` for an unbreakable token) — unwrapped JSON used to
  set the scroll width of the whole responsive table and drag the summary columns sideways with it.
  If `details` carries a `changes` map (the edit convention of `internal/audit`, see
  `AuditChange`/`AuditChanges` in `services/audit.ts`), it is rendered by `readChanges`+`ChangesTable` as
  a compact table **pole / původní / nová** (`data-testid="audit-changes"`, a cleared field =
  `null`/`""` → a muted dash via `ChangeValue`); records without `changes` (legacy, non-edit actions)
  fall back to the existing `JSON.stringify`.
  **The Target column links to the thing that was edited** (`AuditTarget` + `auditTargetHref`): the UID under the
  type is a `<Link>` to `/photos|/albums|/labels/{uid}`, or `/people/{uid}` for a `subjects` target; a `markers`
  entry — a marker UID addresses no page of its own — is routed through its own `details.photo_uid` and lands on
  `/photos/{uid}?person={subject_uid}&info=1` when the payload names a subject. A target with no detail page
  (`users`, `api_tokens`, `announcement`) keeps the plain muted UID, so the row stays scannable either way. The
  expanded block opens with the same links for the UIDs the payload itself names (`auditDetailLinks`,
  `data-testid="audit-links"`, grouped by the `<entity>_uid`/`_uids` key, cut off at `AUDIT_DETAIL_LINK_LIMIT`
  = 25 with `audit.details.moreLinks`) — the target is not always the useful destination (`label.reject` targets
  the label but happened on a photo) and a bulk action names no target at all, listing its photos only in
  `details`. **The audit log outlives what it audits**, so a link to a purged photo or a deleted album is normal,
  not exceptional: the link is offered anyway and the destination says so itself — a 404 (`isNotFound`) puts
  `AlbumDetailPage`/`LabelDetailPage`/`SubjectPage`/`PhotoDetailPage` into a `missing` state
  (*Toto album už neexistuje.* + a hint) instead of the generic “could not be loaded”.
  Filters (actor = a `<select>` over the roster via `fetchUsers`, action, entity type+UID,
  date range `od`/`do`) in a **draft** form → **Filtrovat** writes them to the URL and resets
  the page, **Zrušit filtry** clears them; the dates are expanded in `viewToParams` to RFC 3339 day boundaries
  (UTC). prev/next pagination over `offset`/`next_offset` (limit 100) with a `od–do z total` count;
  filters and offset live in the URL (`useUrlState` over `AUDIT_DEFAULTS`), so Back restores the exact view.
  Actor names are fetched from the roster **best-effort** (fallback to UID, or `—` for a system action),
  never blocking the table render. Loading/empty/error (retry via `reloadKey`) states, self-gated on
  `isAdmin`,
  `MyActivityPage` = `/account/activity` (**no role gate** — every signed-in user, viewer included) **the user's
  own history**: the same audit records as `/audit`, but from `GET /audit/mine`, which the server narrows to the
  caller. It exists for self-repair, not supervision — *"I know I clicked something wrong a minute ago, what was
  it?"* — and everything follows from that. Three columns through the shared `RecordTable` (a phone gets one card
  per entry): **Kdy** (`formatDateTime`), **Co** (the action in words via `activityActionKey`, e.g. `photo.update`
  → *Úprava fotky*; an unknown action falls back to the raw label) and **Kde** — a `<Link>` to the thing that
  changed, named by its kind (*Fotka* / *Album* / *Štítek* / *Osoba*) rather than by UID, resolved by the very
  same `auditTargetHref` the admin log uses. Two fallbacks in the Kde cell: an entry with no target of its own
  (a bulk edit names its photos only in the payload) links the payload's UIDs (`auditDetailLinks`,
  `data-testid="activity-links"`, capped at `ACTIVITY_LINK_LIMIT` = 5 + `activity.moreLinks`), and a target with
  no page at all (`users`, `api_tokens`, `announcement`) is plain text. **No “kdo” column** (the answer is always
  the reader) and **no filter form** (the recent end of a one-user list is where the answer is); prev/next
  pagination over `offset`/`next_offset` with the offset in the URL (`useUrlState` over `ACTIVITY_DEFAULTS`), so
  Back returns to the right page after following a row out. Loading (`ListSkeleton`)/empty/error (retry via
  `useReloadKey`) states, a `BackLink` to `/account`. **It is deliberately absent from the navigation:** the entry
  point is a card on `AccountPage`, because the desktop bar is crowded already and the admin group next door
  (`nav.admin`) is about supervising everybody, which this page is not,
  `PhotoDetailPage` = `/photos/:uid` a **full-canvas immersive viewer** (and the route itself;
  **outside `Layout`**, like `/slideshow` — the photo owns the whole viewport, no navbar/footer).
  The photo is centered, `object-fit: contain` at the **largest fit without cropping** over a **warm near-black
  backdrop** (`--kk-viewer-backdrop`), reflecting the saved non-destructive edit (a live draft while the Úpravy
  panel is open) — for a **video** `VideoPlayer` instead of the image, for a **live photo** `LivePhoto` (both have
  their own fullscreen — the player's own button, the live photo's native one; the image viewer doesn't open for
  them). The style is in
  `components/photo/viewer.css`, the `--kk-viewer-*` tokens (backdrop, chrome/dock scrim, z-index) in
  `tokens.css`. **It replaced the old click-opens-lightbox** — `Lightbox` and `lightbox.css` were removed
  and absorbed here.
  **Disappearing chrome:** the top action bar's **toggles** (plus the curatorial loop on a mouse),
  the **‹/› arrows** and the phone's **bottom curation dock**
  after a short idle **dim away** and return on mouse move / tap / key
  (`useAutoHideChrome` — an idle timer + a global wake, `paused` when the drawer is open, so a control
  under your hand doesn't vanish); the transitions run on duration tokens, so `prefers-reduced-motion`
  turns them off. **Two things in that bar deliberately survive the idle** (N20): the way out and
  **the photo's title** (`.kk-viewer__heading`, dimmed to `opacity: .72` rather than removed) — with a
  mouse any move brings the chrome back, but a phone has no such gesture, so an idle screen used to be
  one photograph plus one unlabelled glyph, with no name for what you were looking at. The bar's
  darkening wash rides `.kk-viewer__chrome::before` exactly so it can thin to `.55` on its own
  (a gradient can't be transitioned, an opacity can) and keep the surviving title legible over a
  blown-out sky; the title's own shadow deepens in that state too. **The hook decides only *whether***
  — one `data-chrome` flag on the viewer root; `viewer.css` decides **what** answers to it.
  **The persistent way out** is `.kk-viewer__back` (a circle at top left, `photo.back`, **never fades**
  with the chrome) — **a back arrow, not a ✕** (N6/N20): the drawer carries its own ✕ and on a phone
  the two sat side by side as identical round crosses, so a tap meant for the panel closed the whole
  photo. Arrow = leave the photo, cross = close what is over it. It and **Esc**
  always work and return **to the exact previous scroll position**: `navigate(-1)` when you arrived here from
  the grid (the browser restores scroll), otherwise (a direct link/refresh — caught by `location.key === 'default'`
  at mount) `backHref(view)` reconstructs the list URL. **Keys:** ←/→ steps through neighbors, `f`
  favorite, `m` faces, `i` drawer, `s` **skrýt** (hide/unhide, editor+ only — the same handler the eye
  button calls, see below), Esc **a step back** (first the selected face, then the drawer, then
  out); rating hotkeys `0`–`5`/`p`/`r`/`v` on document (except while typing into an input).
  **prev/next** = `<Link replace>` `‹`/`›` carrying scope+filters from the URL (`detailQuery`) **and `info`**,
  respecting the source listing's order (`usePhotoNeighbors` over `neighborParams`+`mode` — `GET
  /photos`, or `GET /search` when the detail came from a search, in the mode `useSearchMode` resolves so a
  bookmarked `mode=hybrid` link doesn't spend the sidecar timeout on neighbours; stop at the ends).
  A **directly opened** photo (a link, F5, Back/Forward) must first be *found* in that list — a page walk of
  a second or more on a big library — so `PhotoNeighbors` carries **`pending`**, keeping "not known yet"
  apart from "there is no such neighbour", and an arrow pressed in that window is **remembered**
  (`pendingStep`) and paged the instant the answer lands, re-checked on arrival against the same
  typing/`isFormModalOpen` guard the shortcut hook applies. Without it the keys were silently dropped
  until the walk happened to finish between two presses, which read as *"you have to click into the page
  first"* — while `i`/`m`/`f`, which need no list order, worked straight away. The walked order is **kept
  per list identity** and stepping is answered from it (no refetch; a photo at the scanned tail resumes
  from the last offset instead of re-walking), so the arrows re-point with the photo rather than leaving
  the previous one's pair up — a `›` aimed at the photo already on screen was a second press that went
  nowhere. **touch**:
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
  refresh line up. The **curation loop** is `RatingStars`+`FlagControl` (per-user stars 0–5 + a personal flag
  Prohlédnout později/Vybrat/Zamítnout over `useRating`) and `FavoriteToggle` (shares the optimistic toggle with `f`), followed by
  **Archivovat/Vrátit z koše** (editor+ only per `canWrite`, as with bulk archiving): `archivePhoto`
  sends the open photo to the trash, `unarchivePhoto` restores it (a photo opened from `/trash` arrives already
  archived); **you stay on the page** — `archived_at` is toggled in place (the label flips
  Archivovat ⇄ Vrátit z koše) and the result is reported by a toast.
  Beside it sits **Skrýt z knihovny/Vrátit do knihovny** (editor+ too): `hidePhoto`/`unhidePhoto` toggle
  `hidden_from_library` in place, which takes the photo out of
  the library grid, the timeline, the map, the slideshow and the default search while leaving it in its
  albums, labels and favourites. It is **not** archiving and nothing is deleted;
  the button's `title` and the success toast both name the way back (`hidden:yes` in search) —
  a flag you cannot list is a flag you cannot undo. **The `s` key** (`shortcuts.detail.hide` in the
  `?` help) is the same act without the mouse, for walking a series and dropping the dross: it runs
  `toggleHidden` itself — one implementation, one optimistic flip, one toast — it is **a toggle and
  the view stays on the photo**, so the second press is the undo the toast has no button for, and it
  is bound only when `canWrite` (for a viewer the key is not in the map at all). `hidden`/`toggleHidden`
  therefore sit **above** the loading/error returns, unlike their archive twins: a `const` declared
  after an early return would be in its temporal dead zone on every render that returns early, and
  the key would throw while the photo was still loading. Nothing else moves — `PhotoFlagBadges` below
  already reads the state in words beside the title, so a stray press is visible where you stand.
  (`a` is deliberately left unbound: archive is the obvious next claimant. `h` is not free — it is the
  grid's vim-style move-left.)
  **Both are *flag toggles*, and their glyph shows STATE, never the action** (`flagBtnClass`, a
  comment on the decision at the call site). They used to show the action — a hidden photo got a
  plain `eye` meaning "click to show" — which contradicted the `aria-pressed` beside it and was
  unreadable anyway: an eye and a struck-through eye differ by a hairline at 1rem, so the state was
  legible to a screen reader and to nobody else (a reported bug). Now the glyph says state
  (`eye-slash` = hidden, `eye` = in the library; archive keeps the `archive` box in both states —
  there is no glyph for "not in the trash", and `arrow-counterclockwise` named the action), and
  so do `aria-pressed`, Bootstrap's `active` marking and its tone; only `aria-label`/`title` say
  what a click will do. **The on-state is toned `danger`** (`.kk-viewer__btn--flag.active` in
  `viewer.css`, `color-mix` over `var(--bs-danger)` — the token, never a literal, so the dark
  theme's own doctoring carries through; deepened towards black so the white glyph clears AA on an
  opaque pill, whatever photograph is underneath) — the one place in the viewer where "on" means
  "this photo is held back", not "this view is turned on", which keeps the azure accent. Colour is
  never alone: `active` carries the state where colour is lost (colour blindness, forced colours),
  and **`PhotoFlagBadges`** repeats it in words under the title — a `danger` pill per flag
  (`photo.archive.badge` „V koši", `photo.hidden.badge` „Skrytá z knihovny", `.kk-viewer__flags`),
  nothing for an ordinary photo. That badge is the only place the state shows for a **viewer**, who
  gets no toggles at all. Elsewhere — the album and label galleries, which raise the filter and do
  list hidden photos — nothing marks them yet; that is left for a later task, as marking a tile is a
  grid concern, not this control's.
  **Where that loop sits depends on the reach.** With a mouse it rides the top bar; **below `md`
  (`useIsNarrowViewport`) it moves into a bottom dock** (`.kk-viewer__dock`, `role="group"` /
  `photo.viewer.actions`) along the edge the thumb already rests on — the top-right corner is the
  hardest place to hit one-handed on a tall phone, and rating/flagging/favoriting is the everyday
  loop. The top bar then keeps only the title and the three occasional view toggles (faces / edits /
  info); the persistent ✕ and the ‹/› arrows are untouched (already reachable). The controls are
  **one element tree mounted in one of two places**, never two copies — the decision is made in JS,
  not by a pair of `d-*-none` rules, so nothing renders a hidden twin of every star for assistive
  tech (or a query) to find, and the two layouts cannot drift apart. The dock **fades with the rest
  of the chrome** (same idle timer) so the photo is never permanently boxed in, **stands down while
  the drawer is open** (which owns the whole screen at this width), carries
  `env(safe-area-inset-bottom)` itself, frosts the photo behind it, and lifts the shared (mouse-sized)
  stars/flags/heart to the **44px finger floor**. Ten finger-sized targets do not fit a 360px row, so
  it **wraps** — the flag+heart+archive cluster is grouped (`.kk-viewer__marks`) so the break falls
  between the stars and the marks rather than stranding a lone control. `PhotoDetailPage.test.tsx`
  guards both layouts (DOM) plus the dock's placement/fade/floor by reading `viewer.css` (jsdom
  evaluates no media query and computes no layout).
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
  panel, so they can't diverge. **A crop — and only a crop — stands the whole faces UI down**
  (`!hasCrop(previewEdit)` in `facesAvailable`): it leaves a frame the boxes were never measured against, so every
  frame would miss its face; the UI comes back the moment the crop is off again. A **rotation no longer costs the
  reader their frames**: `FaceOverlay` takes the preview's `rotation` (and the measured frame's `ratio`) and maps
  every bbox through it — `rotateBbox` turns the coordinates, `rotatedFrameStyle` gives the layer the box the turned
  photo actually paints (for a quarter turn the wrapper's height by its width, centred, since the wrapper keeps the
  *unrotated* aspect ratio). Only the coordinates turn, never the layer via a `rotate()`, so a box's number and name
  stay upright — verified pixel-exact for all four rotations against the real figure geometry. Brightness and
  contrast move no pixels and never mattered.
  **Known limit of that trade:** the boxes drawn *over the photo* follow a rotation, but everything that **crops a
  square tile** out of a `fit_*` thumbnail by bbox (`FacesPanel`'s rows, `Clusters`, `Outliers`,
  `DuplicateMarkerCrop`, `Review`) does not — since a saved edit is now baked into the thumbnails, those crops land
  on the wrong part of a *rotated* photo. Detection data is deliberately left alone (`face_detect` is not re-run),
  and only a handful of photos are ever rotated, so the crops were left for a task of their own: each of those
  readers would need the photo's edit, which the faces/cluster payloads do not carry.
  **Watch out — a load-bearing invariant:** `FaceOverlay` positions boxes in **percentages** of the `.kk-viewer__figure` wrapper,
  whose box **must sit exactly on the rendered image**. So the figure gets an **inline `aspect-ratio`**
  from `useImageFrame` — **the loaded image's own `naturalWidth`/`naturalHeight`** (post-orientation), with the
  catalogue row's `displayFrame` only as the estimate that holds the layout still until the preview arrives —
  plus `data-framed='true'`: this way it fits into the stage via „contain", but its box is
  **exactly the image** (no letterbox bars into which percentage boxes would drift), in both the width- and
  height-limited fit. If neither the row nor the image gives a frame (`data-framed` isn't set), it falls back to a bare
  `inline-flex` shrink — a frameless photo carries no face geometry anyway. **No box is drawn until the frame is
  measured** (`FaceOverlay`'s `measured` prop — the layer mounts, empty): drawn against the estimate a box can
  sit off its face and then jump. (jsdom doesn't catch the letterbox
  — verify the geometry visually; previously the figure just shrank to the `<img>` and when the stage was narrowed by the panel
  it stretched, so the **frames drifted apart**.)
  Taking the frame from the image is what makes the viewer immune to the **other** half of that invariant, which
  is on the backend: `file_width`/`file_height` **must be the stored,
  pre-rotation dimensions**, because `displayFrame` is what applies the orientation to them. The source catalogue
  reports its own dimensions with the tag **already applied**, so the import that took them verbatim
  stored a pair the frontend rotated a second time — the figure got the transposed aspect ratio, „contain"
  letterboxed the photo inside it, and every percentage box drifted off the faces (85 photos with a marker,
  orientations 6 and 8; ~8.6 % of the library carries the pair). The rows still want repairing — everything that
  crops a **square tile** out of a photo (`subjectTile`, `Outliers`, `OutlierCard`, `DuplicateMarkerCrop`,
  `useFaces`) scales a background instead of overlaying a box on a rendered image and so cannot measure its way
  out. The importers now de-orient on the way in (`internal/exif` `RawDimensions`) and
  already-imported rows are corrected by `kukatko maintenance repair --dimensions`, whose dry run is
  `maintenance scan` (the `transposed_dimensions` finding). The same repair's other half moves the **face boxes**
  that were normalized against that transposed frame — most of them are a correct box in the raw frame and need
  the quarter turn, not a rescale — but it decides that per row from the photo's own markers and leaves a row it
  cannot place exactly where it is (`transposed_face_boxes` is that half's dry run).
  The boxes are colored by state (`lib/faceState`) — **two colors: green = named, yellow = not named**, deliberately
  no third one (see the `faceState.ts` note below); the selected one is primary + a ring, and every box carries a
  **number `#N`** — in **reading order**, see `useFaces` below. **A name is drawn only on the ACTIVE box**
  (hovered, focused or selected; `zIndex: 1` lifts it over the neighbours it reaches across). Drawing every name at
  once is what the overlay used to do, and with two people side by side one label lay across another and across a
  third box — on a fifteen-person group photo an unreadable pile, and `faces:3 face:new` alone returns 2 937 photos.
  The other names are one hover away in the panel, whose rows carry the crops.
  Hovering a box highlights the row in the panel and vice versa (`hovered`/`onHover`
  held by the page). A click on a box or on a panel row = the same selection (and opens the drawer).
  **On touch** the pair still works: a box is only as big as the face, so on `pointer: coarse` `.kk-face-box`
  grows an **invisible 44px hit box** around it (an `::after` in `app.css`, `min-*: 100%` so a face already
  bigger keeps its own target and the **drawn outline never changes**; `pointer-events` is inherited, so a
  read-only box stays click-through). The pairing highlight is reported from **focus** as well as hover
  **on both sides** (a finger never hovers, but a tap focuses the box — and the keyboard tabs the panel rows),
  and `FacesPanel`
  **scrolls the selected row into view** (`block: 'nearest'`), so a box tapped on the photo doesn't select
  a row somewhere off-screen in the drawer.
  **The information runs in the drawer** (`.kk-viewer__panel`), **one element with two shapes** — the default
  state is just the photo, and whichever shape opens, **the stage yields exactly the space it takes**, so the
  photograph is never the thing that disappears. At **≥ md** it slides in **from the right** and the **stage
  narrows** by `--kk-viewer-panel-w`; together with the stage the **top bar and the `›` arrow** move over by the
  same width, so the panel toggles and paging stay **visible beside the drawer, not under it**.
  **Below `md` it is a bottom sheet** (N6): the `@media (max-width: 767.98px)` block at the foot of
  `viewer.css` re-anchors the same element to the bottom edge over `--kk-viewer-sheet-h` (**46 dvh** — `dvh`, not
  `vh`, so a phone's collapsing address bar can't make the sheet taller than the screen), gives it a grab handle
  (a pseudo-element), rounded top corners and tighter padding, and the stage takes `bottom: var(--kk-viewer-sheet-h)`.
  It used to be the desktop drawer copied unchanged, which at 393 px meant an opaque panel over a photo that was
  still loaded and **not visible by a single pixel** — fatal for the faces panel, whose rows are numbered to match
  boxes drawn *on* that photo. **There is no scrim any more** (`--kk-viewer-panel-scrim` is gone): the photo above
  the sheet has to stay both visible **and** gesture-bearing, and a tap-to-dismiss layer over it is neither.
  The sheet is closed by its own ✕, by the toggle that opened it, or by Esc. The height cap on the lead panels'
  cards (`FacesPanel`, `EditPanel`) is a class (`.kk-viewer__panel-scroll`), not an inline style, precisely so the
  sheet can **lift** it — two nested scroll regions in a ~370 px window is a touch trap, and an inline style would
  outrank the media query that undoes it. The breakpoint is `useIsNarrowViewport`'s, so the sheet and the
  curation dock cannot disagree about what a phone is. Guarded by `PhotoDetailPage.test.tsx` (geometry read out of
  `viewer.css`, glyphs in the DOM) and verified in a real browser at 393 × 852.
  **Shut, the drawer must not hold the keyboard.** It slides out rather than unmounting, so every one of its
  controls stays laid out — and a laid-out control is a tabbable one: on production at 1440 px, Tab left the
  visible chrome and landed on „Zavřít informace" **inside** the shut panel, whereupon the browser scrolled the
  photograph 416 px off screen to reveal it (`.kk-viewer` is a scroll container — `scrollWidth` 1856 >
  `clientWidth` 1440) and kept it there for 17 more stops. The panel was `aria-hidden` the whole time it held that
  focus, which made it a **WCAG 4.1.2** violation as well: a screen reader announces nothing from there. The fix is
  **`inert={!panelOpen}` on the `<aside>`** — the whole subtree leaves the tab order **and** the accessibility
  tree — plus **`visibility: hidden`** in `viewer.css` for a browser too old for `inert`, transitioned at `0s`
  but **delayed by the slide's own duration**, which is what keeps the drawer visible the whole way out. It covers
  the **faces and edit views too**: those are views *of* this one drawer, not panels of their own (it is the only
  element on this screen that closes by transform). Because an inert subtree drops the focus it holds onto
  `<body>` — where the next Tab restarts at the top of the page — **every** close path goes through one
  `closePanel()`, which notes whether focus was inside the drawer first and then hands it to the toggle that
  reopens the view just closed (`focus({ preventScroll: true })`: scrolling to reveal a control is the very jolt
  this is about). Guarded by `PhotoDetailPage.test.tsx` („the shut drawer and the keyboard" — jsdom implements
  neither `inert` nor layout, so the tests read the DOM condition a browser acts on: an `inert` ancestor).
  Its content is **the same components as before, only in the drawer instead of below
  the photo** (the `OrganizeBadges` „filed under" strip above the photo is gone — albums/labels are in Uspořádání).
  **The drawer's sections**
  (`components/photo/`): **1. Uspořádání** (`sections.organize`) = **the primary block, always
  visible and directly editable** (no "edit mode"): `OrganizePanel` (inline add/remove of albums
  and labels via the organize API) + `PeoplePanel` (people/faces as **person-chips** over the same
  `useFaces` that drives the overlay — answers "who is in the photo" even with faces off; **it assigns nothing
  itself**: an editor's click on a chip calls `onEditFace` → the page turns on faces and selects that
  face in `FacesPanel`, so assignment lives in exactly one place. A viewer sees named
  people read-only; named = a rose chip, an unnamed detection = a neutral chip); albums/labels/people have
  a distinct color via `ENTITY_STYLE` (`components/entityStyle`), and an album/label chip is the shared
  `EntityChip` (the pill itself is the link, so the whole chip is one 44px target on a phone — the editor's
  remove X only trims it). Adding runs through
  **`AddAutocomplete`** (a type-to-filter combobox over react-bootstrap primitives,
  **case/accent-insensitive** via `lib/text` `foldedIncludes`, keyboard ↑/↓/Enter/Esc + click,
  a „nic neodpovídá" state, ~44px tap targets, ARIA combobox/listbox; an optional `onCreate` prop adds
  a „Vytvořit «dotaz»" row — `createAndAttachLabel` does `createLabel` + `attachLabel` and
  `createAndAddAlbum` does `createAlbum` + `addAlbumPhotos` (album defaults: empty description, non-private
  — type/cover/privacy stay editable on the Alba page), both matching the name via `foldedEquals` so they
  reuse an existing entry instead of colliding on the slug. **Both fields therefore stay mounted even with an
  empty option list** — that is the only way to get the first album/label. The create row is offered only for
  a name that really does not exist: an entry the photo already carries is out of `options`, so it is handed
  over separately as **`existingNames`**, which suppresses the offer without putting it back in the list).
  **1b. Komentáře** (`CommentsPanel`, `components/photo/`, **NEW**) = **the conversation around the photo**,
  mounted directly under the people because that is what it is usually about („kdo je ten kluk vlevo?").
  It is the social half of the archive: most of what a family knows about an old photograph is not metadata
  anybody will ever type into a form, it comes out when someone recognises something and says so. So joining
  in is deliberately cheap — **every signed-in role may post, viewers included** (backend
  `RequireAuth`, not `RequireWrite`, see [`API.md`](API.md)), and the composer is therefore **never hidden
  from a viewer**; it is the one place in the app where a viewer writes. The panel takes
  `photoUid` + `currentUserUid` + `canModerate` as **props, not from `useAuth`** (like `PeoplePanel`'s
  `canWrite`), so it renders in a test with no auth provider. Structure: an eyebrow heading that becomes the
  **pluralised count** once there is one (`photo.comments.count`, `_one/_few/_many/_other` — Czech needs
  three forms: 1 komentář / 2 komentáře / 5 komentářů), the thread **oldest first** (a conversation reads
  forwards) as a `<ul>` of `CommentItem`, and the composer. **Empty state invites the first remark**
  („Napiš, co o téhle fotce víš…") rather than reporting an absence. **`PersonAvatar`**
  (`components/`) is what a thread actually draws: the **cover photo of the person the author's account is
  linked to** (`author_photo_uid` on the comment → `thumbUrl(uid, 'tile_224')`, no download token — the
  browser sends the session cookie with a same-origin `<img>`), falling back to `InitialAvatar` when there
  is no link, no cover photo, or the image fails to load. The fallback is the **normal** case, not an error
  path: most accounts name no person and most people have no hand-picked cover, so the letter has to look
  like a design rather than a hole. `.kk-avatar--photo` fills the same circle with `object-fit: cover` over
  a neutral surface. Tests: `PersonAvatar.test.tsx`. **`InitialAvatar`**
  (`components/`, **NEW**) draws a person as **the first letter of their name in a coloured disc** — the
  stand-in for a profile picture in a library that stores photographs, not avatars, and never fetches an
  external asset (no gravatar-style lookup leaking who reads what). Both the letter and the colour are pure
  functions of the name (`lib/avatarIdentity`: `avatarInitial` takes the first **grapheme** via
  `Intl.Segmenter` and `toLocaleUpperCase`s it — „šárka" → „Š" — with `'?'` for a blank name;
  `avatarTone` is an FNV-1a hash mod `AVATAR_TONE_COUNT`, case- and whitespace-folded), so one person keeps
  one colour on every photo, in every session, for every reader, with nothing stored anywhere — that is what
  makes it recognition rather than decoration. The eight hues are theme tokens
  (`--kk-avatar-0-bg`…`--kk-avatar-7-bg` + `--kk-avatar-fg` in `styles/tokens.css`, each carrying white
  text at ≥ 5:1, next to the entity hues); the disc is `aria-hidden` because the name is always written out
  beside it. **`CommentItem`** renders
  `PersonAvatar` + author + `formatRelativeTime` (`lib/relativeTime`, `Intl.RelativeTimeFormat`
  `numeric: 'auto'` + `style: 'narrow'` → „před 2 h", „včera"; the absolute stamp survives as the `<time>`'s
  `title`) + an „upraveno" marker when `edited_at` is set + the body with **`white-space: pre-wrap`** — the
  backend parses nothing, so the client renders text, never HTML/markdown. **Edit is in place** (a textarea
  replacing the body; a modal for fixing a typo would hide the conversation the remark belongs to), **only
  on the reader's own comment**; **delete** is the author's *or* an admin's (`canModerate`) and always goes
  through **one** `ConfirmModal` owned by the panel, not one per row. An admin therefore removes but never
  rewrites — the backend answers 403 to an admin's PATCH, and the UI says the same thing by only offering
  „Smazat". A comment whose author's account is gone reads as „Smazaný uživatel" (the row survives
  authorless) and nobody may touch it. Writes go through **`hooks/useComments`** (`status`/`comments`/`count`/
  `busy`/`failure` + `post`/`edit`/`remove`): it applies **the record the server returns**, not an optimistic
  local guess — the server owns the uid, the timestamps and the author name — and classifies a failure into
  `throttled` (429, the per-user `ratelimit.comment` → „komentáře jdou moc rychle po sobě"), `forbidden`
  (403/404) or `failed`; **a rejected post keeps the text in the box**, so nothing anyone typed is thrown
  away. The **count badge** rides the viewer's own info toggle (`.kk-viewer__btn--badged` +
  `.kk-viewer__btn-badge`, capped at „99+"), seeded from `PhotoDetail.comment_count` and thereafter fed by
  the open thread's `onCountChange`, so posting or deleting re-badges **without a second GET of the photo**;
  the number is in the button's **accessible name** too (`photo.viewer.infoWithComments`, pluralised), since
  the little disc is `aria-hidden` and a screen reader would never reach it. The panel **mounts only while
  the drawer is open** (`{panelOpen && …}`, unlike the rest of the info view): paging through a hundred
  photos with the drawer shut would otherwise fetch a hundred threads nobody asked to read, and the empty
  result would overwrite the count the detail payload already gave the badge. **Enter posts, Shift+Enter
  breaks the line** (in the composer and in the in-place editor) — the convention every chat has already
  taught the reader — with the send button kept for touch and for anyone who does not know it.
  On a **phone** the thread lives in the existing bottom sheet, and the sheet **lifts clear of the on-screen
  keyboard**: a phone keyboard does not shrink the layout viewport (on iOS it slides over it), so a sheet at
  `bottom: 0` would put the composer *behind* the keyboard meant to fill it. `hooks/useKeyboardInset`
  measures the gap between the layout and the visual viewport (`visualViewport` `resize`+`scroll`; 0 under an
  80 px noise floor, 0 while pinch-zoomed past 1.05, 0 where there is no `visualViewport` at all — jsdom and
  every desktop browser), `PhotoDetailPage` publishes it as `--kk-keyboard-inset` on the viewer root and the
  phone media query reads it into the sheet's `bottom`. Covered by `CommentsPanel.test.tsx` (thread, post,
  Enter vs Shift+Enter, viewer posts, edit own, delete-with-confirm, admin moderation, empty state, count
  reported upwards, 429, Czech plural), `useKeyboardInset.test.tsx` and four badge tests in
  `PhotoDetailPage.test.tsx`. **2. Popis a místo**
  (`sections.caption`) = `MetadataPanel` = title/description/ai_note/notes/taken_at/location
  **read-only until the editor clicks a field** — each field is its own inline edit affordance
  (`EditableField` = the whole row is an „Upravit «pole»" button with a pencil icon). **An empty field
  renders nothing at all — for every role, the editor included:** the reading state is what the photo says
  about itself, and a photo with no description used to answer with the instructions for writing one
  („Co se na fotce děje a proč stojí za zapamatování"), so the few facts it did carry were read through a
  hedge of prompts. The `*Prompt` strings are not deleted, they moved to where they are acted on — **the edit
  form offers every field, empty ones included, each under its own prompt** as a `Form.Text` wired to the
  input by `aria-describedby` (`photo-title-prompt` … `photo-taken-at-prompt`; the date field is the reason
  it is help text and not a `placeholder` — `datetime-local` never shows one). What counts as empty is
  decided once, by `lib/photoFacts` **`metaValue`** — the same rule `MetaField` applies, so absent, blank and
  the importers' literal `Unknown` are all nothing to read (a description imported as `Unknown` used to
  render here as content, because `EditableField` had its own looser `value.trim() !== ''`).
  Because the rows disappear, the way into the form cannot be one of them: the panel carries **an
  always-present „Upravit informace" button** at its head (outline, pencil + label, `title` = the same
  sentence) so a photo with no metadata whatsoever is as easy to start filling in as one that already has a
  title — previously the only entrance left was the location row's pencil, which promises to edit the
  location and in fact opens the whole form. **A value written on more than one line
  stays on more than one line:** every one of those fields is typed into a `<textarea>` and comes back with
  the breaks its author made, but HTML collapses them into spaces — so the photo book's two-line description
  („Fotokniha 2026 - str. 135 p. 2" and the caption underneath it) rendered as one sentence, and the
  `AI_MODEL:` trailer of an automatic description ended up glued to its last sentence. Both branches of
  `EditableField` (the editor's button and the viewer's `<div>`) therefore add **`.kk-multiline`**
  (`white-space: pre-wrap`) next to `text-break` — `pre-wrap` rather than `pre-line`: the two differ only in
  what they do with a run of spaces, and in text a person typed by hand the second space is their decision.
  **Nothing is turned into HTML** (no `<br>`, no `dangerouslySetInnerHTML`) — it is the user's own text, so
  it is a rendering concern and stays in CSS; `text-break` stays with it so one long unbroken token still
  cannot stretch the panel. The tests in `MetadataPanel.test.tsx` install the real rule out of `app.css`
  through `test/css.ts` `installRule` and measure `getComputedStyle`, on both branches. A click on any field opens one
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
  `display?: ReactNode` (a richer render of the value, plain `value` still decides "filled-ness").
  **A date declared unknown** (`taken_at` gone, `taken_at_before_unknown` set — see migration 0066) renders
  where the capture-date row would be, through **`PreservedDate`** (in `MetadataPanel.tsx`): muted secondary
  text „původně zaznamenáno: …" formatted by `lib/takenDate` `formatTakenLabel` (the set-aside value has a
  precision like any other date; the estimate flag is deliberately **not** passed on — it describes what the
  photo claims now, and a „cca" in front of a disowned date would be the app hedging about a value it is only
  quoting), plus — for an editor — a link putting it back. Restoring is an **ordinary date edit**
  (`PATCH /photos/{uid}` with `taken_at`, no new endpoint), so the backend drops the preserved value in the
  same statement and the row disappears with it. A photo that never carried a date shows nothing extra: the
  guard is 0066's invariant (a photo with a date never has one set aside) made explicit, so the two can never
  show at once. The location picker = **three ways in** in the order a person reaches for them:
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
  `reverseGeocode`) **embedded** in this block. **Zjistit místo is not role-gated** — a geocode is a read
  that costs mapy.com credits, not write access — so a viewer gets the same live button; it goes off only
  for the duration of its own request, via `ReasonedButton` (`photo.location.lookupBusy`), because a grey
  button beside a bare spinner reads as broken rather than busy. **Vymazat polohu** is the editor's, and
  a viewer simply does not get it (the app-wide rule: a role leaves no greyed-out control behind); it too
  states `photo.location.clearBusy` while the write is in flight. Under the mini-map the panel names the
  **place, not the coordinate**: `placeLabel(photo.place)` (`lib/photoPlace`) reads the cached hierarchy as
  one line („Špilberk, Brno, Česko"), a completed lookup **replaces** it (answering a click with the string
  already on screen would read as the button having done nothing), and with neither the line says so
  (`photo.location.unknownPlace`). „49.39322, 16.70869" is not an answer to „kde to je?" for anyone not
  holding a map — the numbers survive as the line's `title` (`formatCoordinates(…, 5)`) and in full in
  Technické údaje. **3. Technické údaje** (`TechnicalDetails`,
  **closed on first render** expander `aria-expanded`/`aria-controls`): **everything the app knows about
  the photo**, in **groups** (`MetaGroup` = a heading + `<dl className="row">`, two columns on a wide
  viewport, one on a narrow one; long values wrap, never stretch the page):
  **Fotografie** (camera/lens/aperture/exposure/focal length/ISO, serial number, software, capture
  date source, IPTC/XMP credits `subject`/`artist`/`copyright`/`license`, `keywords` as **chips**
  split on the comma, `projection` + a badge row `private`/`scan`), **Soubor** (name, `original_name`
  only when it differs, **Formát** = `fileFormat(file_mime, image_codec)` — one row, not two: the codec
  column repeats the MIME type for a plain JPEG („Formát: JPEG" over „Kodek obrazu: jpeg" is the same fact
  twice) and only earns its brackets when it says something new, `HEIC (HEVC)`; size — the exact byte count
  in `title`, dimensions, **aspect ratio** and **Mpx** (computed), EXIF orientation 1–8 as a label,
  color profile, added/changed), **Poloha**
  (coordinates, `altitude`, + a **cached** `place` from the detail — country/region/city/place; **no
  on-demand geocoding**, only `PhotoLocation` does that on demand), **Video** (only `media_type`
  `video`/`live`: duration `m:ss`, codecs, audio yes/no, fps) and **Původ** — a single row **Nahrání**
  (`photo.technical.upload`) stating the upload as **one fact**, who and when:
  `photo.technical.uploadedBy` (`<uploader>{{name}}</uploader> · {{at}}`, rendered through `Trans` so the
  **name is a `Link` to `uploaderHref(uploader.uid)`** — the shortest path from „who brought this" to
  „show me everything they brought", with the moment left plain text; the tag is `<uploader>`, not `<link>`,
  which the HTML parser behind `Trans` treats as a void element and would drop the name inside)
  from `photo.uploader.name` and
  `formatDateTimeMinutes(created_at)`, or `photo.technical.uploadedByImport` („Importováno · …") for a
  photo with no uploader — an imported item arrived from an import, not from nobody, and a bare `—` said
  neither. `created_at` therefore no longer has its own row in **Soubor**: the name and the moment are one
  sentence, not two rows in two groups. Last comes
  **Pro vývojáře** (`DeveloperGroup`, `photo.technical.groups.developer`) — a group that looks like the
  others but whose heading *is* its chevron toggle (`aria-expanded`/`aria-controls`), **closed on first
  render** and skipped entirely when it would be empty: the shortened **SHA256** (full value in `title`,
  **copy-to-clipboard**) and `photoprism_uid`/`photosorter_uid`. They are live data (uid search, every
  metadata sidecar) and stay reachable, but a 64-character hex string is of no use to someone looking at a
  family photograph and a table that opens on one reads as an app written for technicians — nothing is
  hidden, it is only sorted by who it is for. The **Fotka** group also names the **Model AI**
  (`photo.technical.aiModel`) that wrote the automatic description — `splitAiNote(photo.ai_note).model`,
  i.e. the `AI_MODEL:` trailer taken *out* of the description in `MetadataPanel` and shown here, where a
  fact about the machinery belongs. All **read-only** (editing belongs in `MetadataPanel`);
  **a field with no value doesn't render at all** (`MetaField` returns `null`) and **an empty group also
  doesn't render** — a photo with poor metadata is not a wall of dashes. „No value" includes the importers'
  **`Unknown`**: the source catalogue stored the literal word in `camera_model`/`lens_model` for a scan that never
  had a camera, so `MetaField` runs every value through `metaValue` (`lib/photoFacts`) and drops
  `Unknown`/blank alike — one rule, every row, no „Fotoaparát: Unknown" in the middle of a Czech table.
  `TechnicalDetails`'s own `has()` asks `metaValue` the same question about a whole group (so a heading
  never outlives its last row), and the camera falls back `metaValue(camera_model) ?? metaValue(camera_make)`
  so the placeholder can't win over a make that is really there. Numbers/dates via the active locale
  (`i18n.language` → Czech has a decimal comma). **Service actions here** (editor/admin only, `canWrite`): `RegenerateThumbnailButton`
  (`components/photo/`) inside the expanded expander calls `regenerateThumbnail(uid)` (POST
  `/photos/{uid}/regenerate-thumbnail`), shows **pending** (spinner + `disabled`), then success
  or an error (422 = „originál chybí nebo ho nelze dekódovat", otherwise a generic message); on success
  it calls `onThumbnailRegenerated`, which in `PhotoDetailPage` **bumps `thumbVersion`** and appends
  `?v=` to `poster` (the thumb URL is built from the UID, thus stable → a cache-bust forces loading the new
  thumbnail without a hard reload). A viewer doesn't see the button.
  **Zpracování** (`ProcessingPanel`, `components/photo/`) is a **visible block of the drawer**, not a row
  inside the collapsed Technické údaje: „proč se tahle fotka nenajde ve vyhledávání?" deserves an answer
  without an extra click. `PhotoDetailPage` renders it as its own `kk-viewer__section` (eyebrow
  `photo.sections.processing`) right above Technické údaje, only when the detail carries `photo.processing`.
  One row per step (`photo.processing.steps.*`, the backend's fixed order): a state glyph + the step's name
  + a muted line saying where it stands. **`done` reads calmly** — a green `check-lg` and the moment it ran
  (`formatDateTimeMinutes`), because a library where everything has been computed should not look like a
  wall of ticks demanding attention; `queued` (`clock-history`, info), `running` (`arrow-clockwise`,
  primary), `failed` (`exclamation-triangle`, danger, with the job's `error` text under the row), `pending`
  (`dash-lg`) and `skipped` (`slash-circle`) are each visually distinct. A step that ran and **found
  nothing** says so as a result, never as a gap: `face_count` („0 obličejů") and `text_found`
  („žádný text"). **A maintainer** (`isMaintainer`, not `canWrite` — scheduling background work is
  operations) additionally gets a **Spustit** button on every step that is neither `done` nor `skipped`; it
  calls `runProcessingStep(uid, step)` (POST `/photos/{uid}/process/{step}`) and **replaces that one row**
  with the state the response returns, so nothing is re-fetched. A 409 says the step does not apply to this
  photo, anything else a generic message, both in a `role="alert"` line under the list. Nobody but a
  maintainer sees the buttons. **Edits are the drawer's lead slot** — they belong
  to the photo they edit, so `EditPanel` (editor/admin, still only) is opened by the **Úpravy** button
  (`aria-pressed`) in the action bar; turning it on **opens the drawer** and mounts the panel at its head (the same
  one `sidePanel` as faces, see above), the header carries a title + a closing **`x-lg`**
  (`photo.edit.closePanel`). Rotation/brightness/contrast/
  crop — rotation leading the panel as **two icon buttons**, `arrow-counterclockwise` and `arrow-clockwise`
  (`rotateLeft`/`rotateRight`), each labelled by its direction because the `Icon` glyph is decorative
  (`aria-hidden`), with the current angle spelled out beside them. Two directions rather than the single
  „Otočit doprava" it used to be: one wrong turn otherwise costs three more presses. Each carries
  `kukatko-tap-target-touch`, since an icon-only `btn-sm` is ~32 px wide and the app-wide coarse floor lifts a
  `.btn` to 44 px tall but **not** wide — and on a phone this panel is a bottom sheet under the thumb. They are
  `outline-secondary` like the rest of the panel's quiet actions — a variant Bootswatch bakes in the very colour it
  also bakes into `--bs-card-bg`, so its label used to sit at contrast **1.00** on the card and the control was
  literally invisible; `app.css` re-points the variant (measured: 5.6:1 text / 3.2:1 outline on the card,
  15.4:1 / 6.1:1 on the page). `PUT /photos/{uid}/edit` via `saveEdit` — which sends **only the edit itself** (`rotation`/
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
  `VideoPlayer` (`components/photo/`) = **the video stage of the detail page**: a plain `<video>` streamed from the
  range-capable `/photos/{uid}/video` (so the browser seeks) with **Kukátko's own control bar** under it —
  play/pause, **±10 s skips**, the scrubbable `VideoScrubber` timeline, a **speed menu** (0.5/1/1.25/1.5/2×),
  mute and fullscreen. The native `controls` are deliberately **gone**, for exactly one reason: a native timeline
  exposes no hover position, so the scrub preview — the point of the feature — would have nowhere to hang.
  The chosen speed is remembered **for the session** (`sessionStorage`, `lib/videoPlayback`) and re-applied to
  every clip, since `playbackRate` resets with each new element. **Keyboard:** `K` play/pause, `J`/`L` ∓10 s,
  `<`/`>` step the speed — via `useKeyboardShortcuts` with `enabled` = "the player contains the focused element,
  **or** the clip is playing", so those keys stay free for the rest of the page. **The arrow keys are left alone
  on purpose**: on this page they page between photos, and a video that hijacked them would break browsing to
  serve a control that has its own buttons — the focused timeline is where arrows seek. All the buttons carry
  `kukatko-tap-target`, so speed and skips are finger-sized on touch. A decode failure still falls back to the
  download link. `VideoScrubber` (`components/photo/`) = the timeline: a click/drag-to-seek rail exposed as
  `role="slider"` (so ←/→ seek ±5 s and PageUp/Down ±60 s once it has focus, without the player claiming those
  keys globally), plus the **hover preview** — a frame from the clip's storyboard sprite, drawn by offsetting one
  JPEG as a CSS background (`storyboardTileStyle`), so following the cursor costs **no request at all**. The
  preview is **mouse-only** (`pointerType === 'mouse'`): on a touchscreen the finger doing the scrubbing sits
  exactly where the preview would appear, so it would hide the frame it is meant to reveal — touch keeps the full
  drag-to-seek. A clip with no storyboard (pending, or never) shows the time bubble alone: the preview degrades,
  the scrubbing does not. `hooks/useStoryboard(uid, enabled)` fetches it **lazily** — nothing is asked until
  playback starts, because the request is what schedules the render server-side and a grid of videos asking on
  mount would enqueue a full decode each; a `pending` answer is re-polled 4× at 5 s and then given up on (the
  sprite arrives on the next playback), and **every failure is swallowed** so a missing preview is never an error
  on screen. `lib/videoPlayback` holds the pure half — `PLAYBACK_RATES`/`SKIP_SECONDS`, the session-storage
  read/write and `stepPlaybackRate` (clamped, an unknown current rate treated as 1×), `seekTarget` (clamped into
  `[0, duration]`, only below zero while the duration is unknown), `formatPlaybackTime` (`m:ss`, `h:mm:ss` past
  an hour, never `NaN`), `playbackFraction`, `storyboardTileIndex`/`storyboardTileStyle` (mirroring
  `storyboard.Spec.TileIndex` on the server), `previewOffset` (keeps the bubble inside the track) and
  `positionFromPointer`;
  `components/photo/` also carries `MetaField` (one read-only labelled `<dt>`/`<dd>` row inside
  a `<dl className="row">` group, an empty value = nothing; an optional `title` = a tooltip over the shortened
  value and `children` = a rich value (chips/badge/copy button), a row with `children` renders
  always — the caller decides about emptiness); `lib/photoFacts` = pure derived facts about a file
  (`aspectRatio` — a fraction reduced via gcd, decimal fallback `1,50 : 1` when it doesn't reduce to legible
  terms; `megapixels`; `formatMime` → `JPEG`/`MOV`; `fileFormat(mime, codec)` = the two format columns as
  one line, the codec kept only when it is genuinely a second fact (case-insensitive, `jpg`/`tif` forgiven);
  `orientation`/`takenAtSource` = narrowing to a
  literal union so the `t()` key stays typed; `splitKeywords`; `shortHash`; `metaValue` = one stored value
  as the card shows it, `undefined` for blank **and for the importers' `Unknown`**; `splitAiNote` = an
  `ai_note` split into `{ text, model }` by a **trailing** `AI_MODEL:` line — a marker mid-sentence is
  somebody's text and is left alone, and the stored value is never rewritten), `lib/photoPlace` = the
  cached place as text, shared so its two readers cannot drift: `placeName` (the narrowest single level,
  for `photoDisplayTitle`'s heading) and `placeLabel` (narrowest→widest as one line, a level repeating the
  one below it dropped — a village is often its own named place — and the **region left out**: an address
  is not a caption), `lib/format`
  `formatBytes(bytes, locale?)` (locale = decimal comma) and `formatByteCount` (the exact byte count
  for the tooltip); `lib/photoEdit` = pure helpers
  edit→CSS (`editPreviewStyle`/`editFilter`/`editTransform`/`cropClipPath`/`isIdentityEdit`/
  `hasCrop`/`NEUTRAL_EDIT`) and the quarter-turn arithmetic `rotateBy(rotation,quarters)` with its two
  named directions `rotateLeft`/`rotateRight` — it normalises into the 0/90/180/270 the API accepts, which is
  what a counter-clockwise turn needs (a plain `%` would yield `-90` and the save would be rejected),
  `PeoplePage` = `/people` a people index: a responsive, **virtualized** `SubjectTile` grid
  (`TileGrid`, minTile 140 / gap 12 — the skeleton's geometry) with its own **`PeopleFilterBar`** over
  the pure `lib/peopleBrowse`: a name search (folded, so `nemcova` finds `Němcová` — the library's
  „Osoba" facet matches the same way), a **kind** selector (Všichni · Lidé · Zvířata · Ostatní, each
  with its live count) and an ordering (**Podle jména**, re-collated in the reader's language rather
  than the database's / **Podle počtu fotek**, ties broken by name so the grid never reshuffles). The
  whole view (`q`/`type`/`sort`) lives in the **URL** (`PEOPLE_DEFAULTS` are omitted from it), so Back
  steps through it and a link carries the exact one; only the live-typed query replaces its history
  entry. Filtering everything away shows `people.noMatches` (the hint blames the search only when the
  search actually dropped somebody). Virtualizing matters more here than on `/albums`: every tile's
  face crop is cut from a full-frame preview, so mounting all of them at once is what made the page
  fetch a hundred megapixels before it drew anything. The tile shows the image/name/photo
  count — `photo_count`, **not** `marker_count`: the caption says „fotek" and the tile links to the
  subject's gallery, which lists a photo once however many of that person's faces it holds; the face
  tools — `CandidateSearchForm`, `FaceAssignPanel`, `OutlierControls` — keep `marker_count`, and their
  strings say „obličejů"), for editors a link to cluster review; the tile shows **that person's face** — what exactly
  is decided by pure `lib/subjectTile.ts` `subjectTileImage` → `{kind:'cover'|'face'|'none'}`:
  an explicit `cover_photo_uid` always wins (a decision overrides a guess), otherwise `cover_face` from the API
  (marker selection see `listSubjectsSQL`) `padBbox(0.3)` + `squareCrop` → `FaceCrop`, and without
  a usable face a placeholder remains (`people.noCover`) — the app doesn't invent a face,
  `SubjectPage` = `/people/:uid` a person's page: a header (name/**life span**/type + edit via
  `SubjectEditModal` — the page keeps it mounted, so the dialog **re-seeds every field (and clears the
  error) each time it opens**: a cancelled edit is really discarded and a failed save's message doesn't
  greet the next opening; the dialog also carries the two **year** fields, shown **only for
  `type='person'`** (a pet has no life span to read) but always submitted, so a mistaken
  reclassification loses nothing, and pre-validated in place (`lifeYearsValid`, 1800…this year,
  death ≥ birth) so a typo is answered without a round trip — the backend stays the authority —
  + the shared `GridDensityControl` **Dlaždic na řádek** — a view preference
  open to anyone who sees the page, not just editors; the grid carries `data-density` for tests and
  holds the shared `GRID_GAP_PX` like the other galleries), a paginated gallery (`useSubjectPhotos` +
  `SubjectPhotoTile` with a „set as cover" action for editors — now a **quiet icon-only disk** in the corner
  of the tile: hidden at rest, revealed on hover/focus (on touch, where there is no hover, it stays visible),
  the current cover is marked by a filled accent disk (`.kk-cover-btn`/`--on`, `image`/
  `image-fill`); behavior unchanged — the same `onSetCover` handler and `PATCH /subjects/{uid}`.
  The tile also carries the library's **corner selection checkmark** (`.kk-tile__check`, props
  `selectable`/`selectFirst`/`selected`/`anySelected`) from the outset for an editor, and the
  cover disk only steps aside at `selectFirst` — i.e. once something is picked; plus, in the one free
  corner, the **age** plate `age` = `approximateAge(photo.taken_at, subject.birth_year)`
  (`.kk-tile__age`, always visible and never a target — it is read, not pressed, and a fact shown only
  on hover is a fact no phone ever sees)), and
  two review sections for editors only: `Candidates` („Možná je i zde" — untagged photos where the person
  is present by face resemblance, to confirm/reject; the search is **explicit** via a button, not
  on-load) and below it `Outliers` (suspicious assignments); the tiles carry a **person
  scope** in the detail link (`detailQuery` with `person=uid`, `DETAIL_DEFAULTS` + just that facet) → prev/next
  in the viewer pages this person's photos (`GET /photos?person=uid`), not the whole library; the gallery
  (`GET /subjects/:uid/photos`) and the person facet sort **identically** — `taken_at DESC NULLS LAST, uid DESC`
  (the backend unified the tiebreaker `internal/people/subjects.go`), so the viewer steps exactly in the order of
  the grid even among photos with the same/no date; editors can **select** in the gallery
  → the shared **`BatchActionBar`** (the library's full set of actions, `onSelectAll` over the loaded
  gallery; refetch the gallery on success) — in selection mode a tile is one
  selection target, so the tile's „set as cover" steps aside, like the heart/stars on a library tile,
  and the action moves onto the bar as an `extraActions` entry (enabled at exactly 1 selected), so it
  stays reachable; the person's own header controls stay visible during a selection.
  The gallery is also read **by decade** (`lib/photoDecades` `groupPhotosByDecade` → one section per
  decade of `taken_at`, the undated ones last in a section of their own): `SubjectDecadeNav` above it is
  one tick per decade with its count, jumping to that section's heading (`decadeAnchorId`, whose
  `scroll-margin-top` clears the sticky navbar). It wears the **library timeline rail's** vocabulary
  (short mark, dim text, the label at full strength, the current tick emphasised) but not its
  `position: fixed`: this belongs to one section of a page that also has a header and two review panels.
  Both the nav and the headings appear only from **two** decades up — one destination is furniture, not a
  way around — and both cover the photos **loaded so far**, since a decade nobody has paged in is not
  somewhere a reader can be sent. The set-cover PATCH sends `birth_year`/`death_year` back unchanged:
  `PATCH /subjects/{uid}` rewrites the whole record, so omitting them would erase them.
  The page also carries the **two repairs for a mis-catalogued person**, both editors-only (a viewer sees
  neither): in the header **Sloučit s jinou osobou** → `MergeSubjectModal`, and on the batch bar
  **Přesunout k jiné osobě** (a second `extraActions` entry, any selection ≥ 1) → `MoveFacesModal`. Both
  dialogs are mounted **only while open** — unlike `SubjectEditModal` above, each loads the whole people
  list to pick from, and a page that never opens them must not pay for it. After a merge the page toasts
  and `navigate(…, {replace: true})`s to the keeper: its own subject no longer exists, and Back must not
  lead to a page that can never load again. After a move it clears the selection and refetches (the moved
  photos have left this gallery),
  `ClustersPage` = `/people/clusters` (editor/admin) a review queue of unnamed clusters:
  `ClusterCard` (a representative + samples + removal of a strayed face + one-shot naming
  of the whole cluster) in the **review tools' density grid** — `useGridDensity(REVIEW_GRID_SCOPE)` +
  `GridDensityControl` beside the title, `gridTemplateColumns(density)` in place of the responsive
  1/2/3 `Row`/`Col` it had, which survives only as the width the count is *seeded* from; the grid also
  carries **`kk-review-grid`** (`components/review/review.css` → `min-width: 0` on its children),
  without which a card's name field and button would set the `1fr` tracks' automatic minimum and
  push the grid off the side of a phone at the three columns a narrow viewport is capped to —
  the other half of that is inside the card, which now survives **any** track width: the
  representative row and the naming row wrap, the name field is `flex-basis: 0` (its own
  `width: 100%` would otherwise push the submit onto its own line at every width) and the submit
  is `text-break`, so its longest word cannot set a floor under the card either;
  the title row is `flex-md-nowrap`, because a flex line is laid out from each item's *max-content*
  width and the subtitle's is a whole sentence — with wrapping left on the stepper always dropped
  to a second line and `justify-content-between` never applied; optimistic removal after naming;
  the per-sample ✕ is sized by **`components/people/clusters.css`**, not inline — on a fine
  pointer it stays the compact 18px badge in the crop's corner, on `pointer: coarse` it drops
  **out of the corner** into a full-width row under the 48px sample (`position: static`,
  ≥ 2.75rem in both dimensions, the sample becomes a flex column), because the app-wide touch
  floor (`.btn { min-height: 2.75rem }`) caps only the height and turned the inline 18px square
  into a tall red sliver over the very face being judged; pinned by `ClusterCard.test.tsx`
  (behavior + both pointer layouts read out of the stylesheet via `test/css.ts`);
  **every crop in the card enlarges** — the representative and each sample are wrapped in
  `EnlargeButton`, and a click opens the cluster's own faces in `ReviewLightbox` at `fit_1280`
  with the face marked, ←/→ walking representative-then-samples in the order the card draws them
  (48–72px squares are exactly what „is this the same person?" cannot be answered from); the
  overlay's footer repeats the sample's ✕ so a stray face can be detached from the picture that
  revealed it, and carries none for the representative, which the card does not offer either,
  `FacesPage` = `/faces` (editor/admin, a link in „Nástrojích") „najdi osobu mezi neotagovanými
  fotkami": the config panel `CandidateSearchForm` (person selection via `AddAutocomplete` with the photo count
  in `hint`, a threshold in **percent** 20–80 % with bookends „Více výsledků"↔„Lepší shody", limit, a
  Hledat button — the search is **explicit**, not live-on-drag), calls `searchCandidates()` (percent→
  distance conversion via `percentToDistance` from `lib/faceThreshold`), `CandidateStats` shows the source photos/
  faces, matches found, done, and the **computed `min_match_count`** with an explanation; `CandidateFilterTabs`
  (Vše/Nové/Přiřadit/Hotovo with counts, also scopes „Potvrdit vše"), `CandidateLegend` + `CandidateCard`
  (`CandidateFaceImage` = a **full `fit_720` preview** with the face as a **colored rectangle** via
  `faceBoxStyle`, not a cropped chip; the wrapper's `aspect-ratio` — the rectangle's coordinate system — comes
  from `useImageFrame`, i.e. the loaded preview, and the rectangle waits for that measurement (`candidate-frame`
  is the wrapper, and the row is only the estimate keeping the card's height still); color/badge/rectangle share
  one code via the bucket `new`/`assign`/
  `done` in `lib/candidateReview`; the **photo is the way into the lightbox** — it is wrapped in
  `EnlargeButton`, so a click enlarges rather than navigates, and leaving for the photo's page stays the
  stage's corner anchor inside the overlay); ✓ confirms (`assignFace`, `create_marker` vs `assign_person` per the
  candidate's `marker_uid`) **optimistically in place** (the card flips, the grid doesn't reload), ✗
  **permanently rejects** via `rejectFace` (`services/feedback`) and removes the card; **keyboard** (arrows/
  `jkhl` move, `y`/`Enter` confirm, `n` reject — both gated by `isActionable`, so `n` on a **done** card
  (visible under Vše/Hotovo, already assigned to this person) is a no-op instead of persisting the
  contradiction; focus jumps to the next actionable card — registered
  in the `?` help via `shortcuts.groups.faceSearch`), „Potvrdit vše (n)" steps through the active tab's actionable cards
  sequentially with a live `current/total`, cancelably, **a partial failure doesn't roll back** and reports
  what failed — the review state is held by `useCandidateReview`; the grid is the user's own column count on
  **`REVIEW_GRID_SCOPE`** with a `GridDensityControl` in the results header (the old fixed `minmax(16rem, 1fr)`
  survives only as the width that count is *seeded* from), and a click on a photo opens `ReviewLightbox` over
  the **active tab's** list with `CandidateDecisions` (the very ✓/✗ handlers the card calls) in the footer —
  while it is open `useKeyboardShortcuts` is `enabled: false`, so grid movement never fights the overlay's arrows;
  config (person/threshold/limit/tab) in the URL,
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
  matches („všechny obličeje jsou přiřazené"); every person's grid runs on the shared **`REVIEW_GRID_SCOPE`** count
  with one `GridDensityControl` beside the stats (all blocks move together), and a click on a photo opens
  `ReviewLightbox` **scoped to that one person's block** — ←/→ never step from Alice's list into Bob's, because the
  footer's verdict is written for the person the overlay was opened on; config (confidence/limit) in the URL;
  **it never auto-confirms**,
  `ExpandPage` = `/expand` (editor/admin, a top-level link **Rozšířit** by albums/labels) „rozšiř album
  nebo štítek o vizuálně podobné fotky": the config panel `ExpandSearchForm` (an **Album|Štítek** toggle
  (`ToggleButtonGroup`), collection selection via `AddAutocomplete` — options from `lib/expandSearch`
  `expandSources` **sorted by photo count descending, empty collections omitted**, the count in `hint` —,
  a threshold in **percent** 65–90 % step 5 **default 80 %** with bookends „Více výsledků"↔„Lepší shody"
  (the range is `lib/expandSearch`'s own — the photo and face embedding spaces have different scales, see
  `docs/THRESHOLDS.md`; only the conversion and the step come from `lib/faceThreshold`, and
  `expandThresholdDistance` trims float noise for the URL),
  limit 1–200 default 50 (`clampExpandLimit`), a Hledat button — the search is **explicit**, not
  live-on-drag); calls `searchSimilar()` (`services/expand`); results = `ExpandResults`: a summary
  row (source photos / with embedding / min. matches / found) + a **vote-rule explanation**
  („Fotka musí odpovídat alespoň {{n}} zdrojovým fotkám" + „Řazeno podle počtu shod, pak podle
  podobnosti", for `source_capped` also a sample) above the **standard `PhotoGrid`** (no grid fork);
  the tile carries via `tileExtras` a **% similarity** and, at `match_count > 1`, a badge of the **match count**,
  a click opens the photo detail as in the library — and a corner **⤢** enlarges the candidate in `ReviewLightbox`
  (`fit_1280`, the label's ✗ repeated in the footer) instead: these are the library's tiles, where a click already
  means „open this photo", so „look closer" gets its own control rather than a second meaning on the same gesture;
  the grid runs on **`REVIEW_GRID_SCOPE`** (`PhotoGrid`'s `scope` prop) with a `GridDensityControl` in the summary
  row, because this is a judging grid and must not re-densify the library on the way back; **selection = the library model** (`useBulkEdit` +
  `SelectionStart`/`SelectionBar`/„Vybrat vše"/Shift+click range/Esc), `BulkEditControl`
  with **`prefill` = the expanded collection**, so Apply adds right away; on success via
  `BulkEditOutcome` **the added photos leave the grid in place** (without a refetch and scroll jump,
  errored ones stay; a different bulk operation doesn't change the grid) and the summary counts update; ✗ on a tile
  (only **labels** — albums have no rejection model, so it isn't offered) **permanently rejects** via
  `rejectLabel` (`services/feedback`) optimistically with rollback + an alert on failure; the **keyboard**
  like the library (`useGridKeyboardNavigation`: arrows/`hjkl`, Enter opens, `x` selects, Esc clears the
  selection — suspended while the lightbox is open, which owns the arrows then); config (type/collection/threshold/limit) in the URL (Back/refresh restores the search); states
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
  as a measured one would let the map claim a precision it doesn't have; the feed's **`coverage`** member is
  stated in words by `MapFilterBar` (`map.coverage`, counts grouped through `formatCount`) — a map holding
  11 % of a library and saying nothing reads as broken rather than sparse — and an **editor** additionally gets
  `map.coverageAction`, a link to `` `${LIBRARY_PATH}?has_gps=false` `` (= `/?has_gps=false`; an in-app
  link never detours through the retired `/library`), the photos a location can be set on (never a
  button that would run an estimate over the library on their behalf); a viewer is told the same number and
  sent nowhere;
  **on a phone the page spends one row on everything that is not the map** (`useIsNarrowViewport`):
  the three mapset tabs, the two date pickers, the archive select and the coverage sentence stacked to
  382 px of a 853 px screen — 45 % of the viewport gone before the map began — so they fold into
  `MapFilterBar`'s drawer and the page lays the title and that one Filters button out side by side
  (`d-flex justify-content-between`, the pair wrapping onto two lines below ~360 px). The heading
  **stays visible** here, unlike `LibraryPage`'s: the map is not a tab-bar destination, so its `h1` is
  the only thing that says where the reader is. The map keeps its `70dvh`, which on a 390×853 phone
  now starts at y=121 and needs no scroll — **597 px, 70 % of the screen**, up from ~417 px,
  `PlacesPage` = `/places` browsing the library by locality: a single `fetchPlaces()` fetch pulls
  the countries→cities hierarchy with counts and per-place `cover_uid`; a **drill in the URL**
  (`?country=&city=` via `useUrlState` over
  `PlacesView` = `LibraryView`+`country`/`city`, so Back walks the levels) — level 1 a list of countries,
  level 2 the cities of the selected country (from nested data, no refetch), both as **`PlaceRow`** (preview
  photo + name + count; Places is a way through a photo library, so a row without a picture is a gallery
  spelled as a table), level 3 a photo grid
  scoped to `{country,city}` via `useScopedPhotos` (enabled only after a city is picked) + the shared
  `FilterBar` + a Místa/country/city breadcrumb; a level holding **exactly one row is stepped past**
  (`lib/placeDrill.resolvePlaceDrill`) — this library is entirely Czech, so `/places` opened on a single
  „Česko" row that every visit began by clicking through; the skip is resolved from the fetched hierarchy and
  **never written into the URL** (the address keeps saying what the user chose), a city level is only skipped
  when its one city holds **all** of the country's photos (a country also holds photos whose town was never
  resolved and which have no row of their own), and a level that was skipped renders in the breadcrumb as
  plain text rather than a link that lands on the same screen; loading/empty/error states, for editors
  **selection mode**
  over the grid → `BulkEditControl` (refetch on success, an edit can move a photo out of a place); walking
  the drill **leaves selection mode**, each place is its own list,
  `SlideshowPage` = `/slideshow` a fullscreen slideshow (outside `Layout`, no navbar): reads the scope
  (`?album=`/`?label=`/`?mode=` for search/none) + filters/sort from the URL (the same state as the grid),
  pages via `usePaginatedPhotos` (large sets aren't loaded all at once) — the fetcher is `fetchPhotos`,
  or **`searchPhotos` when the URL carries `mode`** (otherwise `q` would only substring-filter and a
  different set would play), driven by `useSlideshow` +
  `useSlideshowSettings`, `total` from the server is passed to `Slideshow` (the countdown counts the whole show, not just
  the loaded pages; the last non-zero total stands in while a reorder reloads, so the position readout
  doesn't blink „5 ze 5"), renders loading/empty/error states (all three through `SlideshowNotice`, so each
  has a way out of a route that has no navbar; the wait **says what it is waiting for out loud**, not only to a
  screen reader) or `Slideshow` — the loading state only
  when there is genuinely nothing to show, since a shuffle toggle reloads the list and blanking the
  running show for that would be a restart in all but name; **shuffle is ordered by the server**:
  `sort: 'random'` + a `seed` generated once per show (`newShuffleSeed`, `useState`), so the whole set
  is reachable and its pages neither overlap nor drop photos, and the **playlist** is
  `playlistOf(carried, photos)` with `seen`/`carried` kept per pass (`extendSeen`, reset when
  `useSlideshow`'s `pass` increments) — that is what makes a mid-show toggle reorder only what is still
  to come; **its own frame
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
  every mutation reloads the list and clears the selection **once the batch settles** (in a `finally`,
  not on the success path only) — a bulk restore/purge that dies on its third uid has already mutated
  the first two server-side, so the grid must not go on rendering them as archived, nor keep them
  selected for a retry that would act on photos that are already gone;
  `fetchTrashInfo` fetches the retention for the countdown on the cards,
  `DuplicatesPage` = `/duplicates` (editor/admin) reviewing and **resolving** duplicates: a paginated list of
  groups (`fetchDuplicates`, „načíst další" via `next_offset`) in `DuplicateGroupCard`; per group
  the user picks a keeper and **„Ponechat nejlepší a sloučit"** → `mergeDuplicates(dry_run:true)` computes
  a preview shown in `MergeConfirmModal` („+3 alba, +2 štítky, +1 osoba · 2 kopie budou
  archivovány"); after confirmation `mergeDuplicates()` merges (the keeper inherits albums/labels/people + fills gaps,
  copies to the trash — reversible) → the group disappears + a success alert (`duplicates.merged`), or **rejects** the group
  („není duplikát", hides it locally only); errors via `duplicates.actionError`/503 „nedostupné", loading
  via `GridSkeleton`, an error with retry; a failed **„načíst další"** (any `offset > 0`) never touches
  `status` — the groups loaded so far stay put and the failure is reported inline under the button
  (`duplicates.moreError`), the button itself being the retry, exactly as `usePaginatedPhotos`/`TrashPage`
  do with `moreError`; a dry-run in flight locks **every** group's actions (a page-level busy, not
  `busyGroupId === group.id`), because until the preview resolves there is no modal or backdrop and a
  click on another group would overwrite `busyGroupId` and discard the merge already under way;
  each card offers **„Porovnat vedle sebe"** → `DupComparePage`,
  because a 224px tile is enough to recognize a group, not to decide within it;
  the **member grid inside a card** is the one the page's `GridDensityControl` sizes
  (`useGridDensity(REVIEW_GRID_SCOPE)` on the page, `density` down to each `DuplicateGroupCard`) —
  the page is a list of groups and the judging happens *within* one, so the row of members is the
  thing worth making bigger, not the list; the title row is `flex-md-nowrap` for the same reason
  as `/people/clusters`,
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
  a `meaningful:false` message); a grid of **large** `OutlierCard` whose **column count the user picks**
  — the shared `GridDensityControl` beside the statistics, on `REVIEW_GRID_SCOPE`, i.e. the review tools'
  **own** localStorage key (`kukatko.review.density`, shared with every other review tool but **not** with
  the library): browsing a wall of photos and judging a 4%-wide face are
  two different jobs, one shared number would re-densify the library on every trip here (the former
  hard-coded `minmax(16rem, 1fr)` survives only as the tile the first count is **seeded** from, so a phone
  starts at one column); ↑/↓ therefore jump a row by the pinned count, without measuring the DOM: the **context
  crop** = the bbox enlarged by 30 % on each side via `padBbox` + `cropImageStyle`, inside it
  the face frame via `faceMarkerStyle` (all `lib/faceGeometry`, `aspect-ratio` carries the geometry →
  no pixel measurement), a distance badge in **%**, the question „Je to chyba?" and two **opposite**
  answers to it: **✓ „Ano, odebrat"** → `assignFace` `unassign_person`, **✗ „Ne, je to {{name}}"** →
  `confirmFace` (`services/feedback`) — **mind the polarity, it is not `rejectFace`**; both flip
  the card **in place** (the card doesn't disappear → the grid doesn't reshuffle under the cursor);
  **a click on the crop enlarges the whole photo** (`fit_1280` with the face marked) in `ReviewLightbox`, the
  same two verdicts repeated in its footer — a crop is what raises the doubt, the frame around it is what
  settles it; that replaced the card's small „Otevřít fotku" text link, whose job the stage's corner anchor
  does now, exactly as on every other tool; **selection** via
  `useSelection` (Shift+click a range, **Ctrl/Cmd+A** bound separately — the shared hook ignores modifiers
  — and only when the grid owns the page, so it doesn't steal the browser's select-all in a field) +
  `SelectionBar` with a **bulk removal** that goes sequentially and **acknowledges partial failure**
  (progress + an error count, the done ones stay done); the **keyboard** (`shortcuts.groups.outliers`):
  arrows/`hjkl` move, `y`/Enter remove, `n` confirm, `x` select, Esc clears the selection→focus — **all of it
  suspended while the lightbox is open**, which then owns the arrows and `Esc` —
  and **focus moves after a verdict to the next undecided card** (`nextActionableIndex`; the focus reset therefore
  hangs on the **answer**, not on the working list that changes with every verdict —
  otherwise the move would be discarded after every decision); states idle („vyber osobu")/loading/error/
  empty („nic podezřelého, sniž práh"); tests `OutliersPage.test.tsx` + `lib/outlierReview.test.ts`,
  `DuplicateMarkersPage` = `/duplicate-markers` (editor/admin, a **Vícenásobné značky** link in „Nástrojích")
  „na téhle fotce je jeden člověk označený vícekrát": the other kind of duplicate — not two photos but one
  person tagged two or three times on the same shot, which is **always** a mistake (on a group photo the
  matcher marched one name across a row of boxes, so the people beside her lost their tag).
  `fetchDuplicateMarkers` (`services/dupmarkers`) pages the findings worst-first into a **virtualized**
  `Virtuoso` list (`useWindowScroll`, keyed by `groupKey`) — a card is tall, and twenty of them mounted at once
  is a lot of images. Each `DuplicateMarkerGroupCard` is built around the picture, because the decision cannot be
  made without one: **the whole photo** (`fit_1280`, never a `tile_*` — a centre-cropped square is not the frame
  the bboxes were normalised to) with every one of that person's boxes outlined and **numbered** — the preview's
  `aspect-ratio` comes from `useImageFrame` and the boxes wait for that measurement, so a transposed catalogue
  row cannot stretch them off the faces — and one
  numbered `DuplicateMarkerCrop` close-up per box below it (the same 30 % context crop as `/outliers`, its source
  size picked per marker by `lib/faceSource`); the numbers are the join between the two halves.
  **Each crop enlarges** — wrapped in `EnlargeButton`, a click opens `ReviewLightbox` on that marker's box over
  the whole `fit_1280` frame, ←/→ walking the finding's own markers in the numbered order, and the footer
  repeating that box's **„Nechat #n"** / **„Není tu obličej"** on the same handlers the card uses (both close the
  overlay, since either answer changes the finding under it). The **row of crops inside a finding** is what the
  page's `GridDensityControl` sizes (`useGridDensity(REVIEW_GRID_SCOPE)`, `density` down to each card) — the page
  is a queue of questions and the judging happens within one; the grid carries `kk-review-grid` so two decision
  buttons under a crop cannot widen the `1fr` tracks past the row, and the title row is `flex-md-nowrap`, both
  exactly as on `/people/clusters`.
  Three decisions, all explicit: **„Nechat #n"** → `keepMarker` (the others are **detached**, not deleted — on a
  group shot the box belongs to whoever stood next to her), **„Není tu obličej"** → `invalidateMarker`, and
  **„Nechat být"** → `dismissDuplicateMarkers` (persistent, `POST /feedback/duplicate-marker-dismissals`, for the
  genuine cases: a mirror, a double exposure, a photo of a photo). The list is settled **locally** rather than
  refetched (`lib/duplicateMarkers`: `groupKey`/`removeGroup`/`dropMarker`) — a refetch would renumber and
  reorder everything under the pointer mid-review; flagging a box drops it from its card and the card leaves the
  queue only once fewer than `MIN_GROUP_SIZE` markers remain, so a three-marker finding shrinks to a
  two-marker one instead of vanishing half-fixed. One page-level `busy` serializes the decisions; a 404 says the
  group changed underneath („načtěte stránku znovu") rather than blaming the user, a 503 says the review is off;
  a failed **„načíst další"** is reported inline exactly as on `/duplicates`; tests
  `DuplicateMarkersPage.test.tsx` (with the repo's standard `react-virtuoso` mock) + `lib/duplicateMarkers.test.ts`,
  `ReviewPage` = `/review` (editor/admin, a top-level link **Třídění** right next to Nahrát) a **sorting
  game played in rounds**: one card across **the whole screen** at a time, ~10 questions to a round with a
  visible position (**4/10**), a **combo** of consecutive answers, roughly one card per round that asks nothing,
  and a small card between rounds offering another. The page names what it is showing (`Stage` =
  `question`/`breather`/`round-summary`/`session-summary`/`loading`/`error`/`empty`) instead of inferring it in
  three places, because each of those states has its own keyboard rules.
  The question comes in **five flavours** — „Je na fotce **Tomáš Kozák**?"
  (face) · „Sedí k fotce štítek **Ostatky**?" (label) · „Byla tahle fotka pořízena v **Brně**?" (place, over a
  location the estimator guessed) · „Je to **stejná fotka**?" (duplicate, the two photos side by side) · „Je na
  téhle fotce opravdu **Tomáš Kozák**?" (outlier, a face already assigned to him but far from his centroid).
  `QUESTION_KEYS`, `questionName`, `QuestionHint` and `QuestionStage` are the four places a kind is branched
  on, and nowhere else: the header, the progress, the three buttons and every keyboard shortcut are
  **identical for all five**, because a game whose controls change under the player is not a game.
  The page is — the page is **outside `Layout`** (no navbar, like `/slideshow`), because
  nothing but the photo should compete for attention; the order is **the question above the photo** (header/progress →
  question + hint + confidence → photo → actions) and the whole thing **always fits in the viewport**: it doesn't scroll
  vertically or horizontally, on a short display (a phone in landscape) the **photo** shrinks — text and buttons
  win, you never have to scroll to Ne/Nevím/Ano; the state is driven by `useReviewGame`, the photo is drawn by `ReviewPhoto`
  (`REVIEW_PREVIEW_SIZE = fit_1280`, i.e. **the whole shot**, not a square tile — the bbox is relative
  to the full frame; the stage's shape comes from `useImageFrame` (the loaded preview; the row is the estimate
  that keeps the stage from resizing under the question, 3:2 when there is none) and the rectangle waits for it —
  being asked „is this Alice?" about the wrong face is worse than being asked a moment later; the face frame via
  `padBbox`+`faceBoxStyle` from `lib/faceGeometry` with **~30 %
  padding**, because you can't recognize a face from a tight crop, + a gentle dimming of the surroundings), the question
  `QuestionText` (`Trans` with `<strong>` around the name/label/place — an i18n **template**, not string
  concatenation; the duplicate question interpolates nothing, the two photos *are* the question)
  and the confidence `ConfidenceHint` (a muted % + a bar: context, not the answer) — **except on a place
  question**, which shows none: the estimator either found neighbours that cluster tightly or refused, so there
  is no score behind the guess and printing one would be a lie. The two new stages:
  **`ReviewDuplicate`** (`components/review/ReviewDuplicate.tsx`) = the pair **side by side** with each copy's
  file name and pixel size under it — side by side because the whole question is a comparison and a pair the
  eye has to scroll between cannot be compared, and the numbers because two exports of one shot often differ in
  nothing a thumbnail can show; both halves are `flex: 1 1 0` (equal, since a bigger frame for one copy reads as
  a verdict on it) at `DUPLICATE_PREVIEW_SIZE = fit_1280` — **the whole frame, never a `tile_*`**, which would
  centre-crop away exactly the edges where a crop or a rotation gives itself away — and each carries the same
  corner anchor out to its own photo. **`ReviewOutlier`** (`ReviewOutlier.tsx`) = the **face crop** big, with the
  whole photo beside it for context: a crop rather than a rectangle on the full frame, because „is this really
  Anna?" over a wedding group where Anna is forty pixels across is unanswerable (the /outliers page settled that
  argument, and this reuses its geometry — `padBbox` 40 % → `cropImageStyle`, the box outlined by
  `faceMarkerStyle`, the source picked per face by `lib/faceSource` at `OUTLIER_TARGET_PX` and the **review**
  limits, degrading down the ladder on a 404). **The crop is cut from a `fit_*` preview and never a `tile_*`**:
  a tile is a centre-cropped square, i.e. a different frame from the one the bbox was normalised against, so
  cropping one lands beside the face on anything but a square photo. The full photo stays beside it because a
  face out of its scene is also how a curator gets it wrong. Below `sm` both stages stack into a column, which
  the stage's `flex: 1 1 0` absorbs. Three actions **Ano · Ne ·
  Nevím** are real buttons (large, at the bottom, thumb-reachable on touch), **but the keyboard is the
  primary interface**: `→`/`y` yes, `←`/`n` no, **spacebar**/`↓` don't know, `z` and **Ctrl/Cmd+Z** undo
  (the chord binds outside `useKeyboardShortcuts`, which deliberately ignores modifiers), `o` open the photo
  under question **in a new tab**, `Esc` end (leaves `Esc` for an open help modal)
  — all registered in the `?` overlay via
  `shortcuts.groups.review`; **the photo leads out to itself**: a small anchor in the frame's top-right corner
  (`ReviewPhoto`'s `href` prop, built by the page's `photoDetailPath` — one string for both routes, so the copied
  link and the opened tab can't disagree) to `/photos/{uid}` with `target="_blank"` +
  `rel="noopener noreferrer"`, and `o` opens the same path via `window.open(…, 'noopener,noreferrer')` — it is a
  **real `href`**, so right-click → copy link address, middle-click and Ctrl/Cmd+click all work; *getting the
  URL* is the point (a photo worth sharing is found mid-game and would otherwise have to be hunted down again
  in the library), and a click handler cannot give that. A new tab, because the queue lives in memory and
  navigating away would drop the run — and neither route answers, skips nor moves the queue;
  the answers are **optimistic** (the UI moves on, the request finishes in the background) and
  the next card is **always already in memory** (`useReviewGame` refills in the background, `useImagePreloader`
  decodes `PRELOAD_AHEAD = 4` photos ahead), so between cards **a spinner never flashes**;
  an unsaved answer isn't lost — it sits in an alert with **Uložit znovu**/**Zahodit**, undo has its own
  alert with retry; **leaving mid-round loses nothing**, because every answer was persisted on its own the
  moment it was given.
  **On touch the three answers are also a swipe** (`useReviewSwipe` on the stage): right Ano, left Ne, down
  Nevím, with the card following the finger (`.review-game__card`, an inline `translate`+`rotate` at
  `DRAG_TILT = 1/24 °/px`) and the verdict **named before the finger lifts**. The wrapper fills the stage
  exactly, so the frame inside still caps itself against `100cqh` and the face geometry is untouched by the
  gesture layer; the buttons and every keyboard shortcut are unchanged — the swipe is purely additive.
  **The header carries the round**, not just a tally: the position `4/10` (with **Dnešní mix** in front of it on
  the day's first round), the ⚡ combo from `COMBO_FLOOR = 2` up (one answer is not a run), the 🔥 day-streak from
  `useReviewStreak`, and the session counter (hidden below `sm`, where the room is not there); the hairline bar
  under the header tracks **the round**, because a session has no end to measure against.
  **The cards that ask nothing** (`components/review/ReviewBreather.tsx`): `BreatherCard` = „jen pro radost", a
  photo somebody favourited or rated highly with its title and year, and `RevealCard` = the payoff of a face just
  confirmed („**Alois Skoták** má teď 27 fotek · nejstarší 1962") with a link into that person's gallery. Both
  render into the game's own three-part body, so the fullscreen rules hold; both are dismissed by **any key** or
  the Pokračovat button (`RESERVED_KEYS` — Esc, `z`, `o`, `?` — keep their own meanings) and **neither moves the
  round position, the combo or any total**. The answer keys reach `answer()` first and no-op there, which is what
  lets → both dismiss a breather and continue into the next round unambiguously.
  **The cards that close something** (`ReviewSummary.tsx`): `RoundSummaryCard` = the round's Ano/Ne/Nevím split
  and one primary action — **Ještě kolo?**, or „Zobrazit souhrn" once the backend is dry — which **Enter/Space/→**
  press; finishing the day's first round adds **Pro dnešek splněno**, a fact about the day and not a locked door
  (the button still offers another round). `SessionSummaryCard` = the totals plus a **mosaic of up to 12 photos
  the player touched**, each a link into the library; it is shown when the queue runs out **and** when the player
  leaves with something to show for the session (Esc/✕ once opens it, again leaves — a player who answered
  nothing is never shown a summary of nothing). `MilestoneBurst` = the 10/25/50 celebration: a small badge over
  the game that clears itself after `MILESTONE_MS = 2600`, `aria-live="polite"`, **no sound**, nothing to
  dismiss and nothing that moves the card underneath;
  **the player chooses what is asked about** — `SourceToggle`, a three-state `ButtonGroup` **Oboje · Lidé ·
  Štítky** in the header (glyph + label, the label hidden below `md`, so on a tablet down it is three glyphs
  with an `aria-label`) whose state lives in the **`?source=` query param** (`parseSource` degrades an unknown
  value to `both`), written with `{replace: true}`: `Esc` leaves via `navigate(-1)`, so pushed toggle entries
  would turn "leave" into "switch back". Below `sm` the toggle takes **a line of its own** in the header
  (`review.css`: `flex-wrap` + `order: 4` + `flex: 1 0 100%`, and the counter's basis is `0` so a wrapping
  header breaks *there* and not under the help button) — three more buttons don't fit next to the counter at
  360 px, and the row of chrome must stay readable; the ~40 px is taken from the photo, which the stage
  absorbs by design. `useReviewGame(source)` rebuilds from the new source rather than filtering the cards in
  hand. The toggle stays **three** buttons: the three new kinds ride with **Oboje** (the default), because
  „Lidé" and „Štítky" are promises about what will be asked and a place question breaks either one, while six
  buttons would spend the game's simplest control on question types that run out quickly.
  **Undo is offered for four kinds of answer and honestly withheld for the fifth**: every other verdict has an
  inverse endpoint that restores the previous state exactly, but nothing can mark a location an *estimate*
  again, so after a place verdict `useReviewGame` holds no undo target and the button stays disabled — writing
  the old coordinates back as a *manual* decision would not be the old state but a different one, silently
  attributed to the user (the fix for a mis-answered place is the photo's own page, where placing a pin *is* the
  decision it claims to be). States (all through `EmptyQueue`): an **empty library** (`no_people_no_labels` → „nejdřív pojmenuj
  lidi / založ štítky" with links to `/people` and `/labels`) is **distinct from an empty chosen source**
  (`no_people`/`no_labels` → „hra se ptá jen na lidi/štítky, ale…" + a link to `/people`/`/labels` **and** a
  button that moves the toggle to the other one) and from an **empty queue** (`no_candidates` → „vše
  posouzeno" + Zkusit znovu; with a restricted source the hint says so and offers **Ptát se na oboje**),
  plus loading the first batch and **offline/error** with retry; tests `ReviewPage.test.tsx` (a padded
  bbox, the name/label in the question, →/←/spacebar send the right verdict and advance, **no fetch
  between cards within a batch**, undo via the right inverse endpoint, a failed answer doesn't lose
  the place, the empty states differently, the URL drives the fetched source, a toggle rebuilds the game and
  lands in the URL, a batch that arrives after the switch is dropped, the photo anchor's `href` on a face **and**
  on a label question plus its `target`/`rel`, a click on it neither answers nor advances, and `o` opens without
  answering while `y`/`n` still answer — the key-collision regression; plus, for the three new kinds: a place
  question named after the guessed place **with no confidence line**, a duplicate question rendering **two**
  halves with different sources and both sizes, an outlier question whose crop's `data-thumb-size` is asserted
  to be a `fit_*` with the box drawn inside it and the context photo beside it, the same →/←/spacebar controls
  answering and **skipping** all three, undo routing a duplicate/outlier confirmation to its own un-confirm
  endpoint, and the undo button staying **disabled** across a place verdict; and for the round game: the
  position counting through the round and the round closing into a summary of what was **played**, Enter opening
  the next round, „Dnešní mix" titling the day's first and „Pro dnešek splněno" not closing the game, the combo
  a skip breaks and an **undo restores**, a breather that moves **no** counter and answers nothing, a reveal card
  linking to the person, the 10-answer celebration, the streak read from the caller's own leaderboard row — and
  the game playing on when that read **fails** — a session left mid-round closing with a summary + a mosaic
  while both answers stayed persisted, no summary for a player who answered nothing, the three swipe directions,
  the hint naming the verdict mid-drag while answering nothing, a short drag and a **second finger** deciding
  nothing, and the whole keyboard — `o`, `y`, `n`, spacebar — surviving the gesture layer) + four `review.css`
  guards (the overlay's corner placement above the veil and its 44 px full-strength target under
  `@media (hover: none)`, which jsdom evaluates for nobody; the drag wrapper being exactly the size of the stage,
  so `100cqh` still measures what the face box was normalised against; and the swipe verdict staying off the
  answer row and `pointer-events: none` — read out of the shipped stylesheet like the `app.css` guards in
  `src/styles/`) + `lib/reviewRounds.test.ts` (breather placement, the reveal's one-slot-per-round rule, the
  milestone edges and the local-day daily flag, including storage that throws) + `lib/gestures.test.ts`,
  `LeaderboardPage` = `/leaderboard` (**any logged-in user** — reading aggregates is not a write, so the
  **Žebříček** link is seen by a viewer too; since 2026-08-07 it is the last entry of the „Procházet"
  dropdown rather than a top-level slot beside **Třídění** — one player and 38 answers on the live instance
  did not earn a place next to Knihovna and Alba. **Inside `Layout`**, not
  fullscreen) a **competitive sorting leaderboard** over `GET /review/leaderboard` (`fetchLeaderboard(window)`):
  who decided the most in the review game. A sorted table (`react-bootstrap` `Table`) **Pořadí · Hráč · Ano ·
  Ne · Celkem · Série**, where **Série** is the 🔥 day-streak the API already computes (`streak_days`, deliberately
  **not** narrowed by the window — a streak is a fact about the habit, not about the slice being shown), so the
  board only **renders** it; nobody on a run gets a dash rather than a `0`, which would read like a score.
  The top 3 carry a **medal** (`Icon` `trophy-fill`/`award-fill` + a color class
  `kk-medal--{gold,silver,bronze}` in `app.css`, decorative — the rank number is beside it via
  `visually-hidden`, so a screen reader hears the placing), **the logged-in user's row is highlighted**
  (a match on `useAuth().user.uid`; `kk-leaderboard-row--me` = a `--kk-accent-subtle` tint + a „Vy" badge,
  not just color). **The window toggle** Za celou dobu / Posledních 7 dní / Dnes holds the choice in a **URL query
  param** `window` (`useSearchParams`, replace — „Back always works"), changing the window refetches.
  `ListSkeleton` while loading, `ErrorState` with retry (`useReloadKey`), an **empty state** (`EmptyState`
  „Zatím žádná rozhodnutí" + a CTA to `/review`); if the logged-in user is off the leaderboard, a quiet hint „Zatím
  nejste na žebříčku" with a link to `/review`. **Both invitations are `canWrite`-only**: `/review` is
  editors-only, so for a viewer the „Začněte třídit" button was the reported *broken button* — pressed,
  it silently produced the library. A viewer now gets neither the button (and a different empty-state
  hint, `leaderboard.empty.hintViewer`) nor the not-on-board line, which they could never act on
  anyway. The board is small (a row per user), so a **plain
  table without virtualization**. **For an admin (`isAdmin`) a player's name is a link** to their decisions
  overview (`/audit/reviews?user=…`, aria-label `leaderboard.viewDecisions`) → `ReviewDecisionsPage`;
  a non-admin sees only the name without a click-through. i18n `leaderboard.*` (cs/en). Tests: `LeaderboardPage.test.tsx`
  (sorted standings + the Ano/Ne split, highlighting of one's own row, switching the window changes the query param and
  refetches, the empty state with a link to `/review`, top-3 medals, a not-on-board hint, **admin click-through /
  non-admin plain name**, the two viewer cases: **no CTA in the empty state** and **no not-on-board line**, and
  the streak column rendering a run and a dash for nobody on one),
  `StatsPage` = `/stats` (**any logged-in user** — read-only aggregate counts, so no role gate, like the
  leaderboard; reachable from the **user menu** and the phone drawer's account section) the **library
  statistics** over `GET /system/stats` (`useLibraryStats`), modelled on the previous system's status page: six
  cards (`LibraryStatsCards`, this page's own rendering) — **Fotky** (celkem, z toho videa, v koši, mimo
  koš, z toho skrytých, z toho variant ve skupinách, vidíte v Knihovně — the walk from the catalogue down to
  the count the library grid shows), **Vyhledávání podle obsahu** (připravených fotek, zbývá zpracovat),
  **Obličeje** (se jménem, bez
  jména, fotek s/bez), **Lidé a zvířata** (subjekty po druhu), **Značky na fotkách** (celkem, se jménem, bez
  jména) and **Alba a štítky**.
  Each card leads with its headline number (`kk-display`) and breaks it down beneath; **every number is grouped
  for the active language** (`formatCount`, cs „20 310" / en „20,310" — never raw JSON), and the **coverage
  gaps** (zbývá zpracovat / obličejů bez jména / fotek bez obličeje) are highlighted while non-zero — that is
  what the page
  is opened for while verifying an import. **No two counts on the page invite an addition that does not
  work:** the faces card's two halves make the detections total (spelled out in the card's closing sentence),
  the markers card's two halves make its headline, and the sentence under the markers says they are a
  different kind of thing from a face and do not add up with one. Because **everyone** reads this page it
  carries **no pipeline
  jargon** (no „embedding" anywhere) and the highlighted numbers **lead somewhere** — `/trash`, the library at
  `?q=faces:0`, `/review` — with the write-gated ones hidden from a viewer (see `LibraryStatsCards`). A failed
  load shows `ErrorState` + retry and **renders no grid of zeroes**, so an unavailable count never reads as an
  empty library.
  Below the cards the page turns into a small **charts dashboard** — "what does our library look like?" —
  drawn from a **second** request, `GET /system/stats/charts` (`useLibraryCharts`), which loads beside the
  counts and **fails apart from them**: a chart outage leaves a fully readable page of numbers with its own
  `ErrorState` + retry under them. `CoverageMeters` (from the counts, so it needs no second request) reports
  the three shares as ARIA `meter` rows — fotky se známým místem, fotky prohledatelné podle obsahu, obličeje se
  jménem — each carrying both numbers behind the percentage, and an untouched whole (no faces detected yet)
  reads as 0 % rather than a division by zero. `LibraryChartsPanel` then draws four charts
  (`ChartCard` shells matching the counts cards): **fotky podle roku pořízení** (the full history, 1905→today,
  full width — clicking a bar opens that year in the library through the period filter,
  `?taken_after=YYYY-01-01&taken_before=YYYY-12-31`; an empty year links nowhere), then two rows **paired by
  the shape they draw, not by the measure**: the time axes **přírůstky knihovny** (the last 12 months, month
  names localised via `formatMonthName`) + **růst knihovny** (the running total by year of addition), and under
  them the lists of named things **nejčastější fotoaparáty** (top 10, each linking to `?camera=<model>`) +
  **velikost podle typu** (image/live/video/RAW). Pairing a 9rem chart with a ten-row list is what used to
  leave a card half empty.
  All of it is **plain CSS over the app's tokens** (`charts.css`) — no charting dependency: `ColumnChart` for
  the time axes, `BarList` for the named things. One measure per chart means **one colour** per chart (the
  accent); what a bar means is written beside it, never carried by a hue. Each chart states its key numbers in
  an `aria-label` (`role="img"`, or `role="group"` when its bars link somewhere so the links stay reachable),
  empty buckets draw a baseline tick so a time axis reads as continuous, a one-photo year keeps a 2 % floor so
  it is never drawn as empty, and a series with nothing in it says so in a sentence instead of drawing an empty
  frame. **A series with nothing to compare is not drawn either** (`ChartStatement`), and the rule is the shape
  of the data alone, so a library that really did grow over five years keeps its chart: growth of a **single
  year** is stated as its size + „Jediný rok, kdy do knihovny něco přibylo…", an arrivals window with **one
  non-zero month** as its count + „Jediný měsíc s přírůstkem…" instead of eleven zero-height bars beside one
  full-height one, and a window with **no** arrivals as a plain sentence. A library filled by one import — the
  ordinary way one starts — has exactly those two shapes, and the lone bar read as broken software.
  On a phone the year plot scrolls sideways rather than shrinking a century into hairlines, and a coarse
  pointer widens the columns.
  i18n `stats.*` incl. `stats.charts.*` (cs/en, localised month names and a pluralised „N fotek"). Tests:
  `StatsPage.test.tsx` (loaded counts with separators + group
  headings, no „embedding" in the grid, the derived gaps, **the faces card and the faces meter answering with
  the same numbers on the same grain**, **the markers standing apart with no count wearing the word "face"**,
  the three links with their hrefs and accessible
  names, **a viewer sees the two write links as plain numbers**, the error state without a zero grid, retry,
  cs grouping, **the four charts' accessible names with their key numbers, the year and camera click-throughs,
  the coverage shares, charts failing alone and retrying alone, the empty-series sentences**),
  `LibraryChartsPanel.test.tsx` (**the degenerate series shapes**: a one-year growth series and a one-month
  arrivals window stated instead of drawn, an empty window said outright, both charts back the moment a second
  year/month carries something, no card wearing `h-100`, and the row pairing),
  `ColumnChart.test.tsx` / `BarList.test.tsx` / `CoverageMeters.test.tsx` (the primitives over fixture data:
  bar scaling and the small-value floor, empty buckets, link vs image roles, the meters' percentages —
  including the faces share dividing by the two halves the card adds up — and their zero whole),
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
  `NotFoundPage`,
  `ForbiddenPage` (**no route of its own** — the route guards render it *in place of* the route the
  current role may not enter, so `/review`, `/duplicates`, `/upload`, `/import`, … no longer bounce a
  viewer to the library without a word. Styled like `NotFoundPage`, its sibling in „this page is not
  for you": a heading, one sentence and a link back to the library. It takes the demanded role
  (`GuardRole` = `Role` without `viewer`, the floor nobody can miss) and prints **its own sentence per
  role** — `forbidden.message.{editor,admin,maintainer}` — rather than interpolating a role name into
  one template: Czech would have to decline it („roli editora" / „roli správce systému") and the way
  to *get* the role differs (an editor asks an admin; an admin asks someone who already is one).
  Because it renders instead of navigating, **the URL stays on the protected route** — a reload
  repeats the explanation and Back goes where the user came from, not one step forward again. On the
  two fullscreen guarded routes (`/review`, `/duplicates/compare`) the guard sits outside `Layout`, so
  it renders bare, without the navbar; the link back to the library is then the only way out, which is
  why it is unconditional. i18n `forbidden.*` (cs/en). Tests: `ForbiddenPage.test.tsx` (the sentence
  names the role actually demanded, the way out points at `/`, the Czech default),
  plus the guard-level checks in `ProtectedRoute.test.tsx` and `App.test.tsx` below),
  `OfflinePage` (**no route of its own** either — `RequireAuth` renders it in place of the route when
  `useAuth().status` is `unreachable`, i.e. the session probe failed at the transport and whether the
  visitor is signed in is simply not known. It replaces a redirect to `/login`, which was the worst
  answer available: the reader had not been signed out, the form could not reach a server to check
  anything, and every attempt came back "invalid username or password" — that is how a lost signal
  came to read as a forgotten password. Same shape as `ForbiddenPage` (a `wifi-off` glyph, a heading,
  two sentences, one control) and the same reason for rendering rather than navigating — the URL
  stays on the route asked for, so a retry lands on the page the reader wanted. The copy is held to
  what the shell cache actually holds: the app opens, the photos and the library do not
  (`offline.message`). `offline.notSignedOut` is worded to be true either way — *nothing checked the
  session, so nothing ended it*, rather than the stronger "you are still signed in", which nobody on
  this screen is in a position to know. The button awaits
  `useAuth().refresh()`, which never rejects — a still-unreachable backend re-files the same status
  and the button comes back. i18n `offline.*` (cs/en). Tests: `OfflinePage.test.tsx` (the wording,
  the retry, the button freeing itself) plus `ProtectedRoute.test.tsx` and `App.coldLaunch.test.tsx`),
  `components/savedsearch/` = `SaveSearchModal` (a modal for naming when saving a new view
  or renaming an existing saved search; a saved search is **personal**, like the favourite heart, so
  nothing here is role-gated and a viewer saves views like anyone else. Both footer buttons are
  `ReasonedButton`s that go off together while the save is in flight and say so
  (`savedSearches.edit.busy`) — two grey buttons and no words is the state a reader takes for a broken
  dialog; tests `SaveSearchModal.test.tsx`) + `SavedSearchesDropdown` (a dropdown in the header of
  `SearchPage` — **not in the navbar**; lazy fetch on open, items open the saved view via
  `savedSearchHref`, „Spravovat" → `/saved`, loading/empty/error states inside the menu);
  `components/search/` = `GlobalSearchSections` (a compact cross-entity section above the photo grid of the
  search page: via `useGlobalSearch(query)` it pulls the grouped `GET /search/global` and renders
  chips of matching **albums/people/labels** linking to the entity (an album/person hit is a pill with a
  `ChipThumb` cover, a label hit is the shared `EntityChip` — the pill *is* the link there, so a phone gets a
  44px target instead of a 12px one); independent of the photo fulltext/semantic
  search below it, renders nothing until at least one non-photo match arrives — an empty query /
  an in-progress search / a photos-only match adds no chrome. **A pasted uid replaces the chips** with
  `DirectHitBanner`: a „Přejít na" card linking straight to the resolved photo/album/label/person, or — when the
  id names nothing — a plain warning alert saying so, because the photo grid staying empty underneath is the
  expected outcome of an id lookup, not a failed search) +
  `UnknownFiltersAlert` (the „těmto filtrům nerozumím (hledám je jako obyčejný text)" info alert listing the
  raw `unknown_tokens` as `<code>`, in input order and repeats included; renders **nothing** for an empty
  list, so a caller needs no condition of its own. Shared by `SearchPage` and `LibraryPage` — a mistyped key
  is one mistake and gets one explanation, wherever it was typed) +
  `SearchCommand` (**a global command palette** in the navbar: a compact icon trigger
  (`kukatko-search-trigger`, named + shortcut-hinted by `aria-label`/`title`, see the navbar above)
  opens via `react-bootstrap` `Modal` a top-anchored console — a live input (a combobox
  with `aria-activedescendant`), grouped **keyboard-operable** results from `useGlobalSearch`
  (rows **Alba/Štítky/Lidé/Fotky — sbírky první, jednotlivá fotka poslední**; hledající „Veselici" chce
  skoro vždycky album nebo štítek, ne jednu z dvou set fotek, které to slovo obsahují, a fotka je nejužší
  možná odpověď, tak se kreslí na konec. Pořadí skupin je zároveň pořadí klávesnice: `buildGroups` je
  vrací v něm a `flat` je v něm zplošťuje, takže šipka dolů jde přesně po tom, co oko čte. Prázdné
  skupiny se dál vůbec nekreslí + always a leading action „Hledat vše" → `/search?q=`) and a footer legend
  of keys. Arrows ↑/↓ move (wrapping), Enter opens the active row, Esc closes, a click opens. It opens
  with `/` (suppressed while typing / with a form-modal open via `isTypingElement`+`isFormModalOpen`) or
  Cmd/Ctrl-K (a chord, works while typing too); **the open/closed state and the query live only in the component, not in the URL**,
  so Back stays untouched. **A pasted uid gets its own „Přejít na" group at the very top** (ahead of the
  „Hledat vše" action, so Enter opens the thing instead of running a text search that could never match an id);
  its second line says what the id was and — via `states` — whether the photo is archived/hidden/private/a stack
  variant, and an id that names nothing is stated in words above the rows instead of being offered as a row.
  **Every row leads with the entity's own picture** (`ResultMedia`): an album, a label and a person all draw the
  cover the endpoint stamped onto the hit (`SearchItem.thumbSrc` = the hit's `thumb_url`, already the medallion
  size and signed where the library sits behind a media Worker), a photo row builds its own from the uid. The
  kind's glyph is the fallback and stays one — an entity with no photo behind it **and** an image that fails to
  load both land on it, in a box of the same size, so a row never changes height and never shows a hole. The
  previews are decoration (`alt=""` + `aria-hidden`, lazy): the row names the entity beside them.
  The backend `/search/global` doesn't return `Místa` groups, so the palette
  doesn't show them. **With the field still empty it offers the reader's own nedávno hledané**
  (`useSearchHistory`, only fetched while the palette is actually open on an empty field) instead of the idle
  hint: ordinary rows navigating to `/search?q=…`, so the palette's contract holds unchanged — the first is
  highlighted and Enter opens it, which makes „otevřít paletu, Enter" repeat the last search — and the group's
  heading (a `SearchGroup.action`, the one part of an eyebrow that is *not* `aria-hidden`, styled
  `kukatko-search-group__action`) carries „Vymazat historii". With no history the idle hint stays.
  The rows that *run a search* — „Hledat vše" and the recent ones — carry `SearchItem.query`, and opening one
  **records it** (`useRecordSearch`): it is the palette's one deliberate submit, and what it hands the search
  page arrives there as a URL, which that page rightly refuses to remember on its own.
  Keys `searchCommand.*`, `search.history.*`, `globalSearch.groups.*`, `globalSearch.direct.*`; in the shortcut
  help the group `shortcuts.groups.global`). Both surfaces share `lib/directHit.ts` — the label maps
  `DIRECT_KIND_LABEL`/`DIRECT_VIA_LABEL`/`DIRECT_STATE_LABEL`, the icon map `DIRECT_TARGET_ICON` and the pure
  `directHitSecondary`/`directHitTitle` — so the palette and the search page never drift apart on what an id
  means, and every i18n key stays a **literal** (a key built from a template would widen to `string` and lose
  type checking);
  `components/trash/` = `TrashCard` (an archived-photo tile: a preview + a countdown to auto-purge via
  `trashCountdown` + restore/delete actions + selection in selection mode);
  `components/duplicates/` = `DuplicateGroupCard` (a group card: members side by side with a preview/
  dimensions/size/`taken_at`/distances, a radio selection of the keeper (the suggested one by default), a `reason` badge,
  actions **Ponechat nejlepší a sloučit** (`onResolve` → preview) / **Není duplikát**, a busy state; the member
  row is a `kk-review-grid` columned by the `density` prop the page passes down, and a **click on a tile enlarges
  instead of navigating** — `EnlargeButton` around the thumbnail, `ReviewLightbox` on the group's members at
  `fit_1280` (never the tile's centre-cropped `tile_224`, which hides the very edges two versions differ at),
  ←/→ within the group, a **„Ponechat tuto"** in the footer setting the same local `keeperUid` the radio does;
  leaving for a photo's own page is the stage's corner anchor, so judging a group no longer costs you the group) +
  `MergeConfirmModal` (a confirmation dialog: a summary of what moves to the keeper + how many copies are archived,
  an optional `note` below it — `DupComparePage` uses it to say that the copy is archived and not deleted, Potvrdit/Zrušit,
  a busy spinner) + `CompareStage` (two photos side by side, below `md` stacked; both render **the same**
  `SyncZoom.view`, so the zoom is synchronous by construction; the cursor `zoom-in`/`grab`/`grabbing` says
  what the gesture will do; the viewport clips, `object-fit: contain` never crops) + `DiffTable` (a diff
  table: a row that differs is marked with a **border + bold + `visually-hidden` „liší se"** —
  never just color; `onlyDifferences` hides the matching ones, an empty value is „—", all matching → a message
  instead of the table) + `compare.css` (a fullscreen `kk-compare` flex column, `height: 100dvh`;
mounted **outside `Layout`**, so it carries its own **safe-area insets**: the side ones once on
`.kk-compare`, `safe-area-inset-top` added to the header's padding and `-bottom` to the footer's,
so Zpět and the Ponechat levou/obě/pravou buttons never hide under a notch or the home-indicator bar);
  `components/expand/` = `ExpandSearchForm` (the `/expand` config panel: an Album|Štítek toggle,
  an `AddAutocomplete` collection picker with the photo count in the hint, a percentage threshold slider with bookends,
  limit, a Hledat submit button — purely controlled, the state is held by the page) + `ExpandResults`
  (a summary row with a vote-rule explanation + the review `GridDensityControl` above a `PhotoGrid` scoped to
  `REVIEW_GRID_SCOPE`; per-tile overlays via `tileExtras`: a % similarity badge (`pe-none`), a match-count badge
  at `match_count > 1`, a corner **⤢** that calls `onEnlarge(index)`, and a ✗ button only when
  the caller supplies `onReject`; after the user empties the grid, a „vše zpracováno" message);
  `components/review/` = `ReviewStage` (the photo stage shared by the sorting game **and the review
  tools' lightbox**, so the two cannot drift; `ReviewPhoto` is the game's thin adapter over it, mapping a
  `Photo` row to the stage's uid + dimensions and pinning
  `REVIEW_PREVIEW_SIZE` (`fit_1280`, **exported** — the page preloads exactly this URL). It imports
  `review.css` itself, so a tool that opens the lightbox cannot end up with an unstyled stage. **The whole
  frame** of the photo as
  large as **the space left under the question** allows; the frame is **width-driven** via `aspectRatio` +
  `maxWidth: min(100%, calc(100cqh * ratio))`, where `100cqh` is the **actual** height of the stage (it is a
  `container-type: size` container) — so the frame is capped by the real remainder of the column, **not by a guess**,
  it therefore holds the exact ratio and the normalized bbox fits **without pixel measurement**; `displayAspect` computes
  the ratio in **display** (EXIF-oriented) space — orientation 5–8 swap width/height —,
  fallback 3:2 so the stage never collapses; the face frame = `padBbox` (~30 %) → `faceBoxStyle`,
  `pointer-events: none` + `aria-hidden`, the surroundings a gentle dim; a broken preview degrades to an icon, a new
  photo resets the flag; the `.review-photo__open` anchor to the `href` prop (the page's `photoDetailPath`, which
  its `o` shortcut opens too) in the frame's **top-right corner**, `target="_blank"` +
  `rel="noopener noreferrer"`: deliberately a small corner target and **not the whole preview**, because the
  preview carries the face rectangle and a click into it must not be ambiguous — the three answers stay the
  easiest thing on the screen (measured: 0 px overlap with the action row, ~620 px away from it on a desktop, ~360
  px on a phone). It is an overlay, so it costs the photo no height; `z-index: 2` puts it above the dimming veil,
  quiet at `opacity: .7` with the `o` hint on a fine pointer and a full 44×44 icon-only target under
  `@media (hover: none)`. The one price of a fixed corner: a face pinned into that same corner has the plate over
  ~1500 px² of its padded box (~0.2 % of the frame, verified in the harness) — accepted, because a control that
  hops between corners per card is worse than a rare, small overlap)
  + `ReviewLightbox` (**the review tools' answer to „I am not sure"**: a fullscreen react-bootstrap `Modal`
  around a `ReviewStage`, opened by clicking a card's photo in a tool's grid, with that card's own
  decision buttons repeated in the footer as `children` so a closer look costs no trip back; `←`/`→`
  step through the list the card came from — disabled at the ends, never wrapping — and the caption
  (`title`) repeats what the card said. Keys go through the shared `useKeyboardShortcuts`, **not** the
  modal's own handling (`keyboard={false}`), so `Esc`, the arrows and `o` live in one map with the rest of
  the app's shortcuts and can be asserted at the document level; it holds no review state, it renders what
  it is handed and calls back. `.review-lightbox__stage` is `container-type: size`, which is what the
  stage's `100cqh` cap measures against — without it a tall photo would overflow the viewport)
  + `EnlargeButton` (**the one gesture into the lightbox**: a photo in a review grid wrapped in a real
  `<button>` — keyboard-reachable, `aria-label` = `review.card.enlarge`, `.review-enlarge` in `review.css`
  strips it to nothing but a `zoom-in` cursor and a focus ring. A button, not a link, deliberately:
  enlarging produces no URL worth copying, and „leave for the photo's page" is already the stage's corner anchor)
  + `candidateStage(candidate, alt)` (one face candidate → `ReviewStageProps`: `fit_1280`, the face bbox,
  `href` to `/photos/{uid}` — shared by `/faces`, `/recognition` and a subject page's `Candidates`, so none of
  the three can hand the stage a square tile or forget the way out)
  + `CandidateDecisions` (the ✓/✗ pair for the lightbox's footer, on the **same handlers the card calls**, with
  words instead of the card's bare icons; a `done` candidate shows the done state rather than a second assign)
  + `ReviewDuplicate` (the duplicate check's stage: the pair **side by side**, halves `flex: 1 1 0` so neither
  copy is implicitly favoured, `DUPLICATE_PREVIEW_SIZE = fit_1280` **never a `tile_*`** — a centre-cropped
  square hides exactly the edges where two exports of one shot differ — each with its file name and pixel size
  under it and the same corner anchor out to its own photo; a broken preview degrades to an icon per half)
  + `ReviewOutlier` (the outlier check's stage: the **face crop** large plus the whole photo beside it, the
  /outliers page's geometry reused — `padBbox` 40 % → `cropImageStyle`, the box outlined by `faceMarkerStyle`,
  the source picked per face by `lib/faceSource` at `OUTLIER_TARGET_PX` with the **review** limits and stepping
  down the ladder on a 404. **`fit_*` only**: the bbox is normalised against the full frame, so cropping a
  centre-cropped `tile_*` lands beside the face. The context photo is not decoration — a face out of its scene
  is how a curator mistakes one wedding for another)
  + `ReviewBreather` (`BreatherCard`/`RevealCard` — the two cards a round carries that **ask nothing**: a photo
  „jen pro radost" with its title and year and the reason it was picked, and the payoff of a face just confirmed
  with a link into that person's gallery. Both render the game's own three-part body, so a pause is still a full
  screen, just one with no verdict on it)
  + `ReviewSummary` (`RoundSummaryCard`/`SessionSummaryCard`/`MilestoneBurst` — what closes a round, what closes
  a session, and the 10/25/50 badge. The tally lines are one shared `Tallies`, so the round card and the session
  card can never drift apart in what Ano/Ne/Nevím look like; the session mosaic is capped at 12 tiles, because
  a souvenir that grows into the library again stops being one)
  + `review.css` (a fullscreen **flex column** `review-game`: top bar /
  progress / **question** / stage / actions — text **above** the photo; the stage is `flex: 1 1 0` +
  `container-type: size` + `overflow: hidden`, so its height **is** the remainder after the chrome (basis 0 →
  the photo inside can't push anything out) and an overflow of the photo onto the text is **structurally impossible**, however
  the chrome grows — an alert, a wrapped long name, `pointer: coarse` buttons; `@media
  (max-height: 500px)` tightens the paddings on a **phone in landscape** (wide → no width query catches it,
  and yet it has the least room) and `clamp(…, min(3.5vw, 5dvh), …)` on the question holds the same for
  the font; `review-photo__box` frame, a progress bar, `kbd` badges, a touch variant of the actions;
  `review-duplicate`/`review-outlier` are rows inside that same stage, so both inherit "the photo takes what is
  left" and both **stack into a column below `sm`**, where a phone has no width for two frames;
  `review-game__card` is the swipe wrapper — exactly the size of the stage, so `100cqh` keeps measuring what the
  face box was normalised against, with the spring-back transition **suppressed mid-drag** so the card tracks the
  finger 1:1 — and `review-game__verdict` is the drag's answer badge, pinned to the **top** of the stage
  (the three buttons own the bottom) and `pointer-events: none`; `review-game__round`/`__combo`/`__streak` are
  the header's counters (tabular figures, so a number does not jitter as it counts) and `review-summary`/
  `review-milestone` the closing cards and the celebration, whose animation `prefers-reduced-motion` drops
  together with the card's;
mounted **outside `Layout`** as well, so it adds its own **safe-area insets**: the side ones once on
`.review-game`, `safe-area-inset-top` added to the header's padding and `-bottom` to the actions row
— and to `review-game__center`, which owns the bottom edge in the loading/error/empty states —
including inside the `max-height: 500px` block, which re-declares exactly those paddings);
  `components/slideshow/` = `Slideshow` (a presentation fullscreen stage: the current photo at preview
  size `SLIDESHOW_PREVIEW_SIZE` (`fit_1920`, **exported** — the page has to preload exactly
  this URL), previous/play-pause/next/fullscreen/settings/close controls + a header bar carrying
  **only the progress** (`slideshow.progress` → „snímek 7 ze 40"; counted against the server's `total`, not against
  the loaded pages — neither the remaining time nor the title lives there any more: the header says
  where you are in the show, the photo says what it is);
  **the chrome is a guest on the photograph**: all of it (close, progress, settings panel, control bar) lives in
  **one `.slideshow__chrome` layer** which `useIdleChrome` fades out after 3 s of nothing — with the mouse
  cursor (`cursor: none`) — and brings back on a mouse move, on **any key**, or on a **tap**; while it is away it is
  `inert` + `visibility: hidden`, so it catches neither a click nor a Tab stop, and it is **held** open whenever
  it is being used (settings panel open, a *mouse* pointer over it, focus inside it — a touch pointer never
  holds it, it would never leave). The caption stays **outside** that layer, deliberately: a show whose
  photos stop saying what they are the moment the mouse goes still is worse than one that never said anything;
  keys ←/→ (and **PageUp/PageDown**, which is what a presentation remote sends) / spacebar (**skipped when a
  button has focus** — one press would otherwise both activate it and toggle playback) / **Esc peeling one
  layer at a time** (the settings panel, then fullscreen, then the show) / F, plus **Tab**, whose first press
  while the chrome is hidden only brings it back (there is nothing to tab *to* otherwise — that is how a
  keyboard reaches controls that can hide);
  touch is **two** gestures: a horizontal swipe past `SWIPE_THRESHOLD` (50 px) changes the photo, a finger that
  stayed within `TAP_SLOP` (10 px) toggles the chrome — the only way a touch screen can ask for controls that
  are not permanently on screen — and the gap between the two is a deliberate dead zone; a second finger
  (pinch) and a touch that starts **on the chrome** are ignored;
  the Fullscreen API is feature-detected **and the control is left out entirely where the browser cannot honour
  it** (iOS Safari fullscreens only a `<video>`; a button that visibly does nothing is worse than no button),
  and a rejected `requestFullscreen()`/`exitFullscreen()` is swallowed rather than left as an unhandled rejection;
  the settings panel renders the shared `SlideshowSettingsForm` and **beside the speed an estimate of the remaining time**
  (`slideshow.remaining` → „zbývá 2 min 45 s"; `slideshowRemainingMs(index, total, intervalMs)` — it follows
  the index as well as the chosen speed, so it counts down and reacts to a speed change at once, sticks to the server's
  `total` (no flicker while paging) and freezes on pause; it disappears when the show ends);
  a change of any setting **resumes rather than restarts** — one `onSettingsChange(patch)` prop, the
  index and the photo on screen are kept;
  the **`kenburns`** effect additionally writes inline
  `--kb-*` custom properties from `lib/kenBurns` onto the `<img>` (transform endpoints + `--kb-duration` = the interval) —
  it activates **only for images**, a video frame and a user with `prefers-reduced-motion`
  (`usePrefersReducedMotion`) get a static frame without animation);
  the entrance animations run for `transitionDurationMs(effect,intervalMs)`, published as
  `--slideshow-transition` on the root, so the 1 s speed is not mostly fade) + `slideshow.css` (keyframes
  `slideshow-fade`/`slideshow-slide`/`slideshow-kenburns` (`object-fit: cover`, `var()` is substituted
  before interpolation, so both transforms interpolate as an identical `translate() scale()` list),
  `@media (prefers-reduced-motion: reduce)` as a second safeguard, fullscreen layout, the chrome's own
  opacity/visibility transition on `--kk-duration-base` (which the token layer already collapses to 1 ms under
  reduced motion), **`env(safe-area-inset-*)` on all four edges** (the route is mounted outside `Layout`, so the
  close button and the progress own their insets too — not just the bottom bar), a `pointer: coarse` floor of
  **2.75 rem `min-width`** on every control (the app-wide floor only gives them their *height*: „previous" was a
  22 × 44 px target, measured on a real coarse pointer) and `.slideshow__play`, which re-colours the play/pause
  button because the theme's `.btn-light` renders it white on pale grey — 2.06 : 1, the least legible control
  in the player; near-black on near-white is 16.18 : 1)
  + `SlideshowCaption` (what the photo **is**, laid over the picture: title, capture date
  (`lib/takenDate` `formatTakenLabel`, so a coarse date reads „1974" and an estimate is marked) and
  description, each shown only when its toggle is on **and** the photo carries the value — an empty
  field renders nothing, not a blank line. Deliberately its own element, outside the header and the
  control bar, so a later fade of the chrome could never take the caption with it; its own scrim +
  text-shadow (white on snow is the case designed for), bounded width and the description clamped to
  three lines, so a long note cannot grow over the photo)
  + `SlideshowNotice` (**the black stage with something other than a photograph on it** — the show loading,
  empty or failed. The route has no navbar and no Back link, so the panel carries the way out itself: the same
  close button in the same corner as the player's, plus **Esc** (the player that listens for it is not mounted
  yet in those states — a slow first page used to be a black screen with a spinner and nothing to press). It
  never hides: with nothing playing there is no picture for it to be in the way of)
  + `SlideshowSettingsForm` (**the one** settings form — transition, speed, repeat, shuffle and the
  three caption toggles — rendered by both the start dialog and the running player, so neither can
  offer a setting the other lacks; `settings` down, a patch back up, `idPrefix` keeps the control ids
  apart, optional `speedNote` for the player's remaining-time readout)
  + `SlideshowStart` (**a shared** Promítání button for the library / album / label / search:
  the ordinary click opens a **settings dialog** (`SlideshowSettingsForm` pre-filled from
  `readSettings()`, plus **how long the show will take** at the chosen speed — `slideshowDurationMs(count,
  intervalMs)`, restated as „jedno kolo trvá…, pak se spustí znovu" with repeat on) and only starts on
  confirmation, which is when the settings are persisted: dismissing it starts nothing and changes
  nothing, because the dialog edits a **draft**. It stays an `<a href>` to `slideshowHref(scope,view)`,
  so a modified click (ctrl/cmd/shift/middle) still opens the player directly with the settings last
  saved; the `count` prop is what the estimate is computed from — an absent count shows none);
  `components/map/` = `LeafletMap` (an imperative Leaflet bridge: a tile layer on the **backend
  proxy** `/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}` (the key stays server-side, `{r}`→`@2x` on retina),
  the **mandatory mapy.com elements** — the attribution „© Seznam.cz a.s. a další" → `/copyright` and a clickable
  **logo** bottom-left → `mapy.com`; `leaflet.markercluster` clusters (a click zooms in), markers
  from GeoJSON, a popup with a preview → the photo detail; a one-off setup, a swap of the tile URL when the
  mapset changes, a rebuild of the markers when the photos change, fit-bounds onto the markers; the optional **`onTileError`**
  prop receives the URL of a tile that failed to load (Leaflet `tileerror`), so the parent can
  find out **why** — it fires per tile, the parent debounces;
  **on touch the map doesn't take over the one-finger swipe**: `prefersTouchGestures()` from `lib/mapGestures`
  decides once on mount and the map is then built with `dragging: false` + `touchZoom: true`, so
  one finger scrolls the page and two fingers move the map — nobody gets stuck on a tall map in the middle of
  scrolling content; the mouse (a fine pointer) keeps drag-to-pan and wheel-zoom unchanged.
  The optional **`twoFingerHint`** prop is the text of a pill shown after a one-finger drag
  (an imperative DOM `.kukatko-map-gesture-hint` inside the Leaflet container, the caption goes into
  `data-label` and CSS draws it with `content: attr(...)`, `aria-hidden`, `pointer-events: none`, it disappears
  by itself after 2 s — so the gesture never repaints the React tree);
  the default height is **`70dvh`** (`DEFAULT_MAP_HEIGHT`, a dynamic viewport unit = what really
  remains on a phone under the disappearing chrome bar; a `vh` fallback in `.kukatko-map`), the detail's mini-map
  and the picker pass a fixed one; a mapset the provider draws **light** (`mapsetNeedsDimming` in
  `services/map` — mapy.com publishes no dark tileset, only `basic`/`outdoor`/`winter`/`aerial`) gets the
  class `kukatko-map--dim-tiles`, whose CSS filter tones the **tile pane** down for the dark page and leaves
  pins, clusters, popups and controls at full colour; a dimming rather than the usual `invert()` trick, which
  turns water orange, and toggled imperatively because Leaflet stamps its own classes onto that element),
  `MapFilterBar` (a basemap toggle
  basic/outdoor/aerial — inactive buttons are `outline-light`, since Superhero's `secondary` navy on this
  page's near-black reads as disabled — + date from/to, archived, the coverage sentence with the
  editor's "fill in locations" link, clear filters. **Two layouts, one set of controls**
  (`MapsetSwitch`/`PhotoFilters` take a `stacked` flag, and exactly one layout is rendered, so no control
  is ever in the document twice): on desktop everything sits above the map as before; on a phone
  (`useIsNarrowViewport`) the bar is **one `Filters` button** — `outline-primary`, `primary` once open or
  filtered, badged with `activeMapFilterCount(view)` (`lib/mapView`: the five photo filters the URL can
  carry, `album`/`label` included, never the mapset or the viewport) — opening an `Offcanvas` that holds
  the basemap group, the dates and the archive select, all at the 44 px touch floor
  (`.kukatko-map-filter-panel`). It closes on the same sticky-footer pattern the library drawer uses
  (`.kukatko-filter-footer`): a primary button carrying the live marker count (`map.filters.apply`, the
  figure grouped by `formatCount` so it matches the coverage line above it; `map.filters.applyEmpty` at
  zero, since "Show 0 photos" reads as a broken promise) plus a non-closing "clear filters" when there is
  something to clear. The **coverage statement survives the fold** — it is the one thing on the page that
  says the map is partial: `CoverageStatement` states it short beside the button (`map.coverageShort`,
  "2 378 z 20 636 na mapě") and in full inside the drawer, where the editor's link lives; only one
  instance per layout is `aria-live`, so a filter change is announced once);
  `components/places/` = `PlaceRow` (one row of the place browse list: a fixed square preview well
  (`.kk-place-row__media`, sunken so a place with nothing to draw is an empty well and not a hole; a map-pin
  glyph stands in), the place name, the photo-count badge; the preview is `alt=""` — the row's own text
  already names the place);
  `components/people/` = `SubjectTile`/`SubjectPhotoTile`/`SubjectEditModal`,
  `SubjectDecadeNav` (a person's gallery in decades — one tick per decade of the loaded photos, with its
  count, jumping to that section; renders **nothing** below two decades. The library rail's look at the
  grain a life is remembered in — see `SubjectPage` above),
  `SubjectSummary` (one person, small: the `subjectTileImage` picture, the name and both counts — built for
  the merge confirmation, where the question "are these two the same person, and which record is the
  substantial one" is answered by the pictures and the photo counts, never by two names),
  `MergeSubjectModal` (**two steps on purpose**: a pick — an `AddAutocomplete` over everyone *else*
  (`useSubjects`, photo count in the hint) and deliberately **without** `onCreate`, since merging into
  somebody who does not exist is a rename with extra steps — then a confirmation putting both
  `SubjectSummary`s side by side with "Bude smazána"/"Zůstane" and a warning that it cannot be undone; a
  **Zpět na výběr** link so a wrong keeper is one click from being changed, `POST /subjects/{uid}/merge` on
  confirm, an inline error keeps the dialog open, and neither the backdrop nor Escape dismisses a merge in
  flight),
  `MoveFacesModal` (the split, opened from the batch bar: pick or **type a new name**, then the run —
  per picked photo `GET /photos/{uid}/faces` → `lib/moveFaces.ts` `moveRequests` → one
  `POST /photos/{uid}/faces/assign` per marker the person holds there. Deliberately **not** a bulk
  endpoint: going through the ordinary assign path keeps the assignment state machine, the faces cache, the
  marker's reviewed flag and the audit trail identical to everywhere else, and a half-finished run leaves
  the photos it reached correctly assigned. Sequential, so a run naming a new person has the first
  assignment create them and the rest find them by slug; a `ProgressBar` while it runs (undismissable), then
  a report counting **photos** — plus what was skipped (no reassignable face there) and what failed, since a
  run that moved fewer photos than were picked has to say so),
  `FaceCrop` (**the preferred** face crop: an `<img>` with a `fit_*` source from `lib/faceSource.ts`
  `faceSourceSize` (the whole frame — `tile_*` is a centred square on which the crop would miss the face;
  the size **scales with how small the face is**: a fixed one would give a 13px smudge instead of
  a person for a face over 2 % of the frame. The ladder is 720/1280/1920/2560/3840, but **the ceiling belongs
  to the caller**: a chip stops at `FACE_SOURCE_TILE_MAX` (1920 — a dense grid of chips isn't worth megabytes),
  a card that exists to be **judged** goes to `FACE_SOURCE_REVIEW_MAX` (3840, `/outliers`). Below the target
  it never picks a rung above the original's own long side — `fit_*` doesn't upscale, so that would be the same
  pixels at another URL. Beside the ceiling there is a **download budget** (`FaceSourceLimits.budgetPx`,
  by default `FACE_SOURCE_TILE_BUDGET_PX` = 1.5 Mpx, measured against the *frame* — what `fit_N` really
  costs for this photo — so a small original is never punished for a 24 Mpx one; the review views pass
  `FACE_SOURCE_REVIEW_BUDGET_PX` = ∞). The budget is the answer to the "no rung reaches the target"
  case: most faces span a few per cent of their frame, so without it every one of them took the ceiling
  and 72 people tiles of 152 px pulled **125 Mpx**. It admits `fit_1280` and refuses `fit_1920` on the
  shape most of the catalogue is — ~102 px instead of ~153 px across the tile, both an upscale on a 2×
  display, at 2.3× the bytes. The real fix is a face crop cut server-side; this is the honest trade
  until then) in an `overflow:hidden` container,
  `cropImageStyle` in %, `aspect-ratio` from the crop's real pixel proportions → **nothing is
  deformed**; `size` = a fixed width in px, otherwise it fills the parent (`w-100 h-100`); `label=""` =
  decorative, when the name stands beside it. It renders through `FadeInImage skeleton`, so a crop that
  hasn't decoded shimmers instead of sitting as a dark square. It needs the frame's dimensions),
  `FaceThumb` (**the legacy** square crop via `faceCropStyle` — it deforms and reads `tile_*`; it stays
  only for cluster previews, whose payload doesn't carry the frame),
  `PeopleFilterBar` (the people index's own filter bar: a name search + the **kind** selector (with each
  option's live count) + the ordering, all `Form.Select`/`InputGroup` in one row. It offers the kinds as
  a select rather than the tab strip `AlbumFilterBar` uses, because one of them holds nearly everybody
  and three buttons reading zero are a strip that only takes space; every control writes straight into
  the URL via `SetUrlState<PeopleView>`, **pushing** except the live-typed query, which replaces),
  `FaceOverlay`+`FacesPanel`+`FaceAssignPanel` (`FaceOverlay` = a **purely presentational** transparent layer
  of clickable boxes from the normalized bbox via `faceBoxStyle`, **no image or fetch of its own** —
  it mounts as the last child of the `position-relative` wrapper tight around the `<img>`. Its `measured` prop
  (default `true`) says whether that wrapper is the **measured** image (`useImageFrame`) or still an estimate;
  while it is `false` the layer renders **empty**, because percentages are only as good as the box they are
  percentages of. Its `rotation`/`frameRatio` props (default upright) follow a **rotated** preview: every bbox goes
  through `rotateBbox` and the layer's own geometry through `rotatedFrameStyle`, which is why the layer carries
  **inline** `left/top/width/height` instead of `w-100 h-100` — the sizing utilities are `!important` and would win
  over it. The layer is
  click-through, pointer events are caught only by the boxes (and with `readOnly` not even by those; the box's number and name tag have
  `pointer-events:none`, otherwise they would steal the click and break the swipe). A box carries `.kk-face-box` = an invisible
  44px hitbox on `pointer: coarse` (see app.css below), so even a small face can be hit on a phone, and it reports
  the pairing with the panel from **focus** too, not only from hover (a finger doesn't hover, but a tap focuses the box).
  **The name tag is drawn only on the active box** (hovered/focused/selected — all three arrive as `hovered`) and that box
  gets `zIndex: 1`; see the viewer section above for why every name at once was unreadable. The data +
  the naming state machine
  are held by the `useFaces` hook. **`FacesPanel`** = the panel in the viewer's drawer, the single place where assignment happens:
  a row per face = **the number badge** (the same mark as on the box, the cross-reference to the photo) + **a round 44px
  `FaceCrop` of that face** (`padBbox(0.25)`+`squareCrop`, `label=""` because the row's own label names the person;
  the generic person icon stands in while `faces.frame` is null, i.e. only during loading) + a colored status chip:
  the person's name (green), or
  **Bez jména** (yellow) — the same two states as the boxes — and, beside it, **how old that person was here**
  (`approximateAge` over the photo's `takenAt` prop and the subject's `birth_year`, looked up in the subject
  list the panel already loads for its typeahead; absent whenever nothing can be said). It rides beside the
  name rather than inside it — the name is what the row is keyed on, the age is what *this* photograph adds —
  and it is folded into the row's `aria-label` too, since that label replaces the button's content.
  The row used to read `Obličej #N` and nothing else,
  which meant naming anybody started with hunting a tiny numeric badge somewhere on the photo — the words are gone,
  the crop answers "who" by being looked at, and the bare number stayed as the tie to the box. A row whose face has **no embedding**
  (`hasEmbedding` = a negative `face_index`, i.e. a marker with no face row behind it) also carries a small muted
  `slash-circle` (`data-embedding="none"`, title/visually-hidden **Bez embeddingu**, and the row's `aria-label` says it too,
  because the label replaces the button's content for a screen reader — which is also why that label now carries the
  person's name: `Vybrat obličej #2: Alice`) — that one *is* worth knowing: it is nameable by
  hand here and nowhere else, no suggestion, similarity search or the review game will ever bring it up.
  A click selects/deselects, hover **and focus** mirror the box (a viewer's inert row reports hover too — with the
  names shown one at a time, that is how a viewer asks which box is whom — but a list of rows that answer no click
  reads as a broken panel, so above it a viewer gets **one line saying why** (`faces.viewerNote`: looking is fine,
  naming needs the editor role). It is the counterweight to the app-wide rule that a role renders no greyed-out
  control: where the missing buttons would leave the reader puzzled, the panel says once what they would have
  said one by one. Not an `Alert` — nothing is wrong — and not shown to an editor or on an empty list);
  the selected row **scrolls itself into view**
  (`block: 'nearest'`), so that a tap on a box in the photo doesn't mark a row off-screen. Its list carries the
  **class** `.kk-viewer__panel-scroll` (shared with `EditPanel`) rather than an inline `maxHeight`, because on a
  phone the drawer is a short bottom sheet that scrolls itself and has to be able to *lift* the cap — an inline
  style would outrank the media query that does it. Under the selected row
  `FaceAssignPanel` expands
  (`key={face_index}` → the state resets when the selection changes). **`FaceAssignPanel`** = the top-3 suggestions
  (`{name} · {confidence}%`, one-tap) + a typeahead over `useSubjects` (`AddAutocomplete` with `autoFocus`
  and `hint` = the person's photo count); a face with no embedding leads with a muted note saying so, which is also
  the honest explanation of its empty suggestion list; for an assigned face **Přeřadit** (suggestions, which the backend supplies
  for assigned faces too — the face's own person is excluded from them) and **Odebrat**; Esc leaves the reassignment first,
  then the selection), `ClusterCard`, `Candidates` (the per-subject version of `/faces` embedded in the person's page:
  a **Najít návrhy** button → `searchCandidates` with the default threshold `THRESHOLD_DEFAULT_PERCENT` and
  limit 60, reusing `useCandidateReview`+`CandidateCard` without a fork; ✓ confirms via `assignFace`
  and `onAssigned` reloads the gallery, ✗ rejects via `rejectFace`, both optimistically, and a confirmed/
  rejected card disappears from the list; the grid is the shared `REVIEW_GRID_SCOPE` count with its
  `GridDensityControl` in the section header, and a click on a photo opens `ReviewLightbox` with
  `CandidateDecisions` in the footer — so the density and the gesture are the same here as in the full
  workspace; `no_faces`/`no_embeddings`/empty have an explanation; an
  **Otevřít celý nástroj** link to `/faces?subject={uid}`), `Outliers` (a ranking of suspicious faces
  with a one-tap unassign on the person's page + a **Projít všechny** link to `/outliers?subject={uid}`, where
  the full sweep version lives; each face is a `FaceCrop` — a `fit_*` source picked per face, padded and
  squared like the people tiles. It used to be a `FaceThumb`, i.e. a centre-cropped `tile_*` treated as
  the whole frame, so on anything but a square photo the crop landed **beside** the face; and being a CSS
  background it also loaded for every face at once, however far down the page the section sits),
  `OutlierCard`/`OutlierControls`/`OutlierStats` (the building blocks of `/outliers`: a card with a **context
  crop** (30 % around the bbox, `padBbox`+`cropImageStyle`+`faceMarkerStyle`) wrapped in `EnlargeButton`
  (stretched over the frame with `position-absolute`, because the picture inside is absolutely positioned and
  a flow wrapper would have no height, and therefore nothing to click), the question „Je to chyba?"
  and two opposite verdicts (✓ remove / ✗ confirm), a selection checkbox and a focus ring; a config
  strip with a person picker and a percentage threshold; statistics including the **`no_embedding`** message).
  Two things the card does **not** hard-code: **which thumbnail the crop is cut from** — `lib/faceSource`
  `faceSourceSize(crop, frame, OUTLIER_TARGET_PX, FACE_SOURCE_REVIEW_MAX)` picks the smallest `fit_*` that
  still puts ~154 real px across the crop (≈ 96 px across the face), because a fixed `fit_720` left a
  5%-wide face 35 px and blew it up ~7× into the card = the „nejde vidět, že je to obličej" complaint;
  a size the bucket lacks steps down the ladder on `onError` (`smallerFaceSource`) instead of a broken image —
  and **the marker's geometry**, which lives in `components/people/outliers.css` (`.kk-face-marker`): JS emits
  only the `--kk-face-*` custom properties (the box's **centre** + size in % of the crop), the CSS does the
  `max()`/`clamp()` against the rendered tile, whose px size the card cannot know (the user picks the columns).
  It guarantees two things: a **minimum apparent size** (`--kk-face-min: 28px`, grown around the centre via
  `translate(-50%,-50%)` and clamped inside the crop, so it never drags off the face nor past the edge) and a ring
  built **only** of the element's own `border` + `inset` shadows (dark/warning/dark), which the card's
  `overflow: hidden` therefore cannot clip; the strokes are absolute px, so they don't thin out at ten columns);
  `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  exposes `status`/`user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin` (admin+)/`isMaintainer`/`canImport`.
  **`AuthStatus` has four values, not three**: `loading` | `authenticated` | `unauthenticated` |
  **`unreachable`** — the last meaning the probe never got an answer, so the session is unknown. Only a
  `NetworkError` (a transport failure, see `services/auth.ts`) files as `unreachable`; anything that
  *did* come from the server (a 5xx, an unparseable body) keeps the older "treat it as signed out so the
  UI can recover" behaviour, because there the difference is not knowable. Collapsing the two was the
  bug: the installed app cold-launches from the shell cache with no network, `GET /auth/me` fails at the
  transport, and every screen downstream then acted as if the session had ended. `refresh()` re-runs the
  same probe and **never rejects** — it re-files a status instead, which is what lets `OfflinePage`
  simply `await` it; `ProtectedRoute` =
  the `RequireAuth` + `RequireRole` + `RequireImport` route guards. `RequireAuth` still **redirects** —
  signing in is the missing step and `/login` is where you take it, with the requested location stashed
  in history state — **except on `unreachable`, which gets `OfflinePage` in place of the route**: there
  is no missing step to take, and the form would only mistranslate the outage into a rejected password.
  The two *authorization* guards do not redirect either: a role that is too low gets `ForbiddenPage`
  **rendered in place of the route** (`RequireRole` passes its own threshold, `RequireImport` passes
  `maintainer`). They used to `<Navigate to="/" replace>`, which dropped the user on a page they had not
  asked for, with no explanation and without the address — a shared link to `/duplicates` opened the
  library and read as a broken app. `RequireRole`'s prop is a `GuardRole`, so `role="viewer"` — a
  threshold nobody could fail — is a compile error),
  `capabilities/` (`CapabilitiesContext`/`useCapabilities` + `CapabilitiesProvider` = what the instance is —
  the feature flags `{semantic_search}` **and the running build `{version?}`** — from
  `GET /api/v1/capabilities`; the provider sits inside `AuthProvider`,
  fetches on mount + after 60 s + on `visibilitychange` (the same pattern as `useJobStats`), a failed
  fetch keeps the last state; **unlike `useAuth` the hook doesn't throw** — the context has a safe default
  `{semantic_search:false, known:false}` (**no `version`**), so a component outside the provider merely hides
  the optional offer — or prints no version — instead of crashing.
  **`known` separates "the instance said no" from "we never got an answer"**: it flips true on the first
  reply and stays true, so a later failure keeps the learned flags rather than reverting the UI to
  "nothing works". Anything that *tells the reader a feature is missing* must key off it — the all-off
  default used to be indistinguishable from a reported outage, which made a capabilities call answered
  401 (an expired session) announce that content search was unavailable while it was running fine.
  `FilterBar` reads it for the semantic-search link, `useSearchMode` to decide the mode a search is
  actually sent as, `Layout`/`MobileNavDrawer`/`HelpPage` for the version:
  the shell holds it once, so no menu open ever costs a request and the number cannot disagree with the
  binary that serves the page — a version baked into this bundle would drift the moment either side is
  rebuilt alone), `hooks/` (`useLightbox(items)` = **which item of a review list is enlarged**, behind
  every tool's `ReviewLightbox`: it holds the **index**, never the item — a review list is rebuilt on
  every decision, so a remembered object would leave the overlay showing a copy the grid behind it has
  already updated — reads `items[index]` per render, closes itself when the list shrinks past the open
  item (rejecting the last candidate), steps with `next`/`prev` that **stop at both ends** (`hasNext`/
  `hasPrev` disable the controls; wrapping would read as a stuck control rather than as "that was the
  last one"), and gives the focus back on close to the element that opened it — the card, so a keyboard
  user is not dropped at the top of the document after every look;
  `usePaginatedPhotos` = a shared
  paginated infinite-scroll loader over an arbitrary `PageFetcher`: it accumulates pages,
  `loadMore`/`retry`, reset+refetch **with a skeleton** when the query/`key`/`enabled` changes, cancels
  in-flight requests and ignores stale responses, and also exposes `mode`/`degraded`; `enabled:false`
  → an `idle` state without a request. **`reloadKey` (separate from `key`) is a _background_ refetch of all pages
  loaded so far with the query unchanged: the current photos stay pinned, `status` stays
  `ready` (no skeleton, no reloading of previews), so a bulk edit (favorite/archive)
  takes effect in place without the grid blinking; `reloading` is true for the duration of the refresh, a failed refresh
  is silent (the list stays, even if it is the second page that fails).** The refetch is not just the first page:
  the hook counts `pages` (the number of successfully loaded pages — not `photos.length / PAGE_SIZE`, a page
  may come back shorter) and walks them in order by the server's `next_offset`, until the range runs out
  or (after a bulk archive) the photos run out. Otherwise a reload would throw a reader nested on page 4
  to the end of a hundred-page list and lose pages 2–4 for them.
  **`initialCount`** makes the *initial* load walk the same way: a list that only ever grew by appending pages
  comes back holding one page, which is a document far too short to restore a deep scroll position into, so a
  page returning a reader to where they were passes the length the list had then. It is rounded up to whole
  pages and capped at `RESTORE_MAX_PAGES` (12 → 1200 photos), which bounds the wait rather than the depth:
  past the cap the grid opens as deep as it got. The walk is read when a query *starts* (a ref, not part of the
  query key — moving it alone refetches nothing), and pages that did arrive before a failure are **kept**
  (`hasMore` still points at the one that failed) rather than thrown away for an error page.
  `usePhotoLibrary(params,{reloadKey?,initialCount?})` = a thin wrapper over it over
  `fetchPhotos` (`reloadKey` replays the grid in the background after a mutation, just like `useScopedPhotos`);
  `usePhotoSearch(params,mode,{reloadKey?})` = a wrapper over `searchPhotos` with an injected `mode`
  (it goes into `key` → a mode change resets with a skeleton), disabled on an empty `q` (idle), `reloadKey`
  replays the search in the background after a mutation; the `mode` it sends is the one
  `useSearchMode` resolves, so **no page backed by this hook can block on the sidecar timeout**;
  `useSearchMode(requested)` → `{mode,semanticAvailable,downgraded}` = the one place that decides what a search
  is really sent as: `effectiveSearchMode` (`lib/searchView`) turns `semantic`/`hybrid` into `fulltext` **only
  once the instance has actually reported the sidecar unreachable** (`known && !semantic_search`); while the
  flags are still unknown the requested mode goes out unchanged, since `downgraded` drives the page's
  "showing text results instead" notice and inventing one on no evidence is the worse error of the two.
  The box is offline most of the time, and asking anyway only buys
  the sidecar timeout (30 s of bare spinner, measured on the live instance) before the backend answers with the
  full-text results it had all along. Idempotent and mode-preserving when semantic search is up, so applying it
  along a chain is safe; the capability refreshes in the background, so a box coming back flips the mode and
  re-runs the search within a minute. Read by `usePhotoSearch`, `usePhotoNeighbors`, `SlideshowPage` and
  `SearchPage` (which additionally stamps the **effective** mode into the tiles' `detailQuery` and the
  slideshow scope, so nothing downstream re-asks for the unavailable mode);
  `usePhotoNeighbors(uid,params,enabled?,mode?)` → `{prev,next,pending}` = the prev/next photo of `uid`
  **in the originating list's order**: it pages `GET /photos` (or `GET /search` in the `useSearchMode`-resolved
  `mode`) from the top, accumulating uids until `uid` and its follower are located, bounded by `MAX_PAGES` (50).
  **`pending` is the third answer** — "still walking", as distinct from a `null` that means "there is no such
  neighbour" — because that walk is the whole latency of a **deep-linked** photo (28 requests / ~3.5 s measured on
  the live library) and `PhotoDetailPage` needs the two apart to know whether an arrow key is worth remembering
  (see the viewer above) or is simply a list end. The scanned stretch is **kept between renders** keyed by list
  identity (`params`+`mode`): stepping to a neighbour is answered from it without a request, and a photo at the
  scanned tail **resumes** from the last offset instead of re-walking. A fresh walk reports `pending` rather than
  leaving the previous photo's pair standing — those uids belong to the photo before, not this one. In-flight
  requests abort on a `uid`/`params` change or unmount; `enabled: false` reports no neighbours without fetching;
  `useUploadQueue` = the upload queue: `addFiles` (dedup on name+size+mtime)/`removeItem`/
  `retry`/`retryFailed`/`clear`, a concurrency ceiling `MAX_CONCURRENT_UPLOADS` (3),
  per-file status+progress, a summary of counts + `progress` (the **overall** fraction of the batch 0–1 weighted by
  the partial progress of running files, terminal files = done → a smooth overall bar),
  `createdUids` (new ones only) for the link into the library
  and `resolvedUids` (new **and** duplicate photos) for the post-upload assignment.
  **The queue starts itself** — there is no `start`: an effect tops the in-flight uploads up to the cap
  whenever the queue changes, so a file is uploading the moment it is added, retried, or freed by
  another one finishing, and adding files to a running batch appends to it rather than restarting it.
  That is what lets `/upload` put the album picker *in* the wait instead of in front of it. Running
  uploads are cancelled on unmount;
  `useLeaveGuard(active)` = holds a navigation back while a page has unsaved browser-only work, and
  asks the browser to warn on a tab close. Two mechanisms, because the browser owns only one of them:
  a `beforeunload` listener (bare `preventDefault()` — the deprecated `returnValue` adds nothing and the
  linter rejects it; the wording is the browser's own) and a **capture-phase `click` listener** that
  catches an anchor before react-router's handler, `preventDefault()`s it (a `Link` bails on an
  already-prevented event) and remembers the target so `confirm()` can navigate to it after the fact.
  The app has no data router, so `useBlocker` is unavailable — and a click listener also catches the
  plain `<a>`s of the footer and the tab bar, which a router-level blocker would not. Returns
  `{asking, confirm, cancel}`, which `/upload` renders as a `ConfirmModal`. Deliberately *not* held:
  a modified click (new tab), an explicit `target`, a `download`, an external origin, a link back to
  the page we are on, and any navigation made in code — none of them destroys the queue. Tests:
  `useLeaveGuard.test.tsx`;
  `usePasteFiles(onFiles)` = hands the page every file pasted into it, listening on `window` so the
  gesture works anywhere (not only over the drop zone) and acting **only** when the clipboard actually
  carries files, so pasting text into the album/label pickers stays ordinary pasting. It is the shortest
  route from an iPhone's camera roll into the library, iOS having no share target at all;
  `useUploadOrganize` = a choice of albums/labels for the whole upload batch + their assignment: it loads the catalogues
  of albums and labels (`fetchAlbums`/`fetchLabels`), holds the selection (inline creation as a `create:` marker
  as in `BulkEditModal`, shared helpers `lib/pendingCreate`), `runAssign(uids)` first creates
  the pending albums/labels and then assigns with a single `POST /photos/bulk` (`add_to_albums`+`add_labels`);
  state `idle`/`assigning`/`done`/`error`, `retryAssign` resends the same batch, `resetAssign`;
  `setAlbums`/`setLabels` are wrappers that return the state to `idle` after a completed assignment (`done`/`error`)
  → a selection made **after** the batch finished really does get applied (it used to be silently dropped
  while the green message reported success); an internal rewrite of a `create:` marker to a real UID during
  a running assignment does **not** return the state to `idle`;
  `useSubjectPhotos(uid,{reloadKey?,initialCount?})` = a wrapper over `usePaginatedPhotos` over
  `GET /subjects/{uid}/photos` (a person's gallery, `uid` goes into `key` → a reset with a skeleton when the
  person changes, `reloadKey` is a background refetch after a mutation); `useScopedPhotos` = a wrapper over `usePaginatedPhotos`
  over `GET /photos` scoped to an album/label/**place** (`PhotoScope` `{album?,label?,country?,city?}`
  + filters/sort from the URL, options `{reloadKey?,enabled?,initialCount?}` — `reloadKey` for a background refetch after a mutation, `enabled:false`
  → idle without a fetch, e.g. Places before a city is picked; `initialCount` restores the list's length, see above); `useMapPhotos` = a one-off (unpaginated) loader
  of the GeoJSON feed of geotagged photos over `fetchMapPhotos` (`status` loading/ready/error, `retry`,
  the feed's `coverage` or `null` while it is unknown, cancels in-flight + ignores stale when the filters
  change);
  `useJobStats(enabled)` = a poller of the job-queue state over `fetchJobStats` (`GET /jobs/stats`) for the badge
  in the footer: it fetches **only when `enabled`** (admin), refetches after ~30 s, **pauses on a hidden tab**
  (`visibilitychange`/`document.hidden`) and refreshes immediately on return; it swallows a failure and returns `null`
  (the badge hides), and on unmount/`enabled→false` it cancels the timer and the in-flight request — nothing outlives it;
  `useAnnouncement()` = a poller of the instance-wide announcement over `fetchAnnouncement` (`GET /announcement`) for
  `AnnouncementBanner`: fetch on mount + refetch after ~60 s, **pauses on a hidden tab** and refreshes immediately
  on return, swallows a failure and returns `null` (the banner hides), on unmount it cancels the timer and the in-flight request (mirrors
  `useJobStats`);
  `useRegistrationOpen()` = the one-shot probe of `fetchPublicSettings` (`GET /settings/public`) behind both
  halves of the sign-in flow → `loading|open|closed|unknown`. It **never polls** (an administrator opening
  or closing registration is rare, and both screens that read it are short-lived) and aborts on unmount —
  an aborted request leaves the state alone, since the component is gone and `unknown` would be a claim
  about the instance rather than about the cancellation. That fourth state is the point: `unknown` means
  the question could not be asked, which `LoginPage` answers by hiding the invitation and `RegisterPage`
  by showing its form anyway;
  `useWhatsNew()` = a loader of the returning-reader digest over `fetchWhatsNew` (`GET /whats-new`) for
  `WhatsNewPanel`: **fetches exactly once per mount and never polls** — not a performance choice but a
  correctness one, because the request is what stamps the visit server-side and a digest that reloaded
  itself would change under the reader's eyes; swallows a failure and stays `null` (the panel hides), aborts
  the in-flight request on unmount;
  `useLibraryStats(enabled=true)` = a loader of the library statistics over `fetchLibraryStats` (`GET /system/stats`)
  → `{state,reload}` with the state `loading|error|ready`: it **surfaces an error explicitly** (never swallows it into zeroes —
  an empty library and an unavailable count must not look the same), an aborted request (unmount/retry) is not an error,
  `reload` repeats the fetch (the retry in `ErrorState`); `StatsPage` is its reader (`SystemStatusPage` used to
  be the second, and now reads the dashboard half of `/system/status` instead);
  `useLibraryCharts(enabled=true)` = its sibling over `fetchLibraryCharts` (`GET /system/stats/charts`) with the
  same shape and the same rules — a **separate** hook, and therefore a separate request, state and retry,
  because the charts are the heavier aggregation and must never hold the counts back or take the page down
  with them (`StatsPage` renders the numbers and shows a chart error under them);

  `useLibraryFacets(params)` = a loader of the library's facet offers → `LibraryFacets{years,albums,labels,subjects}`:
  the years over `fetchPhotoYears` **refetch when the filters change** (a year holds fewer photos as soon as a label
  is added), but it **strips the period (`taken_after`/`taken_before`) from the request** (a facet must not
  narrow its own offer — with the sixties picked every other decade would read zero — and without it the
  request stays identical, so trying one period after another doesn't refetch); albums, labels and
  subjects (people, via `fetchSubjects`) are catalogues, loaded **once**. A failure leaves that list **empty** instead of an error (a facet
  that has nothing to offer is a degraded bar, not a broken page — load errors are reported by the grid);
  in-flight requests are cancelled by `AbortController` when `params` change/on unmount, and the years response is additionally
  checked against the `latestYears` seq ref (an abort is a no-op once the response is already on the wire), so that a
  slow response doesn't overwrite a newer one — otherwise a few wrong counts would flash in the facet after a year change
  (the caller memoizes `params` from the URL state);
  `useUploaders(params)` = the same loader for the **uploader** facet → `UploaderBucket[]` over
  `fetchPhotoUploaders`: the same refetch-on-filter-change, the same abort + `latest` seq ref, the same
  „a failure empties the list" rule, and the same self-narrowing guard — it **strips `uploader` from the
  request**, so whichever contributor is picked the control keeps offering all of them. It is a hook of its
  own rather than a field of `LibraryFacets` because the pages that need it are not only the library:
  `AlbumDetailPage` passes `facets` to nothing and this to `FilterBar`; `useTimeline(params)` = a one-off loader
  of the monthly date histogram over `fetchTimeline` (`buckets`/`total`/`status`, refetch when the filters
  change, cancels in-flight + ignores stale — the basis for `TimelineScrubber`); `useGlobalSearch(query,
  debounceMs?)` = a debounced (default 250 ms) grouped global-search loader over `globalSearch`
  (`status` idle/loading/ready/error + `result`, an empty query → idle without a request, cancels in-flight +
  ignores stale — the basis for `GlobalSearchSections`);
  `useSearchHistory(active)` = the loader of the reader's **nedávno hledané** → `{entries,loading,clear}`:
  `active` means "the dropdown could be on screen right now" (the search box passes plain *focused*, since it
  now matches the list against what is typed too), and the list is (re)fetched on **every**
  activation — the history lives server-side precisely so another device can extend it, and re-reading it each
  time the dropdown opens is what makes that visible; nothing is fetched while it cannot be seen, so a page
  that merely *has* a search box pays nothing. `clear()` empties the list **locally first** and posts behind it
  (the whole point of the action is that the list is gone) and bumps the same `latest` seq ref the loads are
  checked against, so an in-flight load cannot repopulate what was just cleared; a failed load leaves the list
  empty rather than surfacing an error (a box that cannot offer history is a plainer box, and an empty dropdown
  is what a first-time user sees anyway). `useRecordSearch()` = the write side, and it is **imperative**: it
  returns `record(query)`, called at the moments the reader actually **submits** — Enter on the search form,
  picking a recent search, opening a palette row that runs one. Nothing watches the query that is *running*:
  the page runs one as soon as typing pauses, so a four-second hesitation mid-word used to put `sva` in the
  ring beside `svatba`, and a couple of hesitant sessions filled the (20-entry) ring with prefixes of itself —
  no settling delay can tell a pause from an intent, and the caller knows which of its events is a submit. A
  blank query records nothing, the same query is not posted twice **in a row** from one mount (leaning on
  Enter costs one request; re-running an older query still moves it back to the front), the post is **not**
  bound to the caller's lifetime (the palette navigates away the same tick, and an aborted record is no
  record), and failures are swallowed *and forgotten*, so the next submit retries. `useFilterValues(facet,debounceMs?)` = the value lists behind the
  search box's value autocomplete → `FilterValue[]` (`{name,count}`) for `album`/`label`/`person`: nothing is
  fetched until a facet is actually asked for, each facet is fetched **at most once** per mount (the lists are
  catalogue-wide, so matching as the reader types is pure client-side work and **no keystroke costs a
  request**) and the fetch itself is debounced 200 ms on top, so a `key:` typed and deleted again costs nothing
  at all; album titles are taken **raw** rather than through `albumDisplayTitle` (the query matches the stored
  title — a localised name would insert a value that finds nothing) and people are counted by photos, not
  markers; a failure leaves the facet empty and uncached, so returning to the same key retries.
  `usePlaceSearch(query,debounceMs?)` =
  a debounced (default 300 ms) loader of the place typeahead over `searchPlaces` (`status`
  idle/loading/ready/**error**/**unavailable** + `places`, cancels in-flight + ignores stale —
  the basis for `PlaceSearch`); it mirrors `useGlobalSearch` with two differences that follow from the fact that a
  lookup **costs credit**: a query shorter than 2 characters is `idle` **without a request** (a single letter is not a
  place name, just a keystroke on the way to one) and the statuses 424/502/503 get their own state
  `unavailable` (it is the provider's side that is broken, retrying makes no sense) as opposed to `error` (the rest,
  incl. 429 — retrying does make sense); `useWindowedPhotos(params,{reloadKey?})`
  = the library grid as a **window** over the result instead of a growing prefix of it: the first request
  reports `total`, which fixes the item count, and `photos` is that long from then on with `undefined`
  where a page is not loaded; `ensureRange(start,end)` (called from `onRangeChanged`) loads the pages
  covering the visible range plus `WINDOW_PREFETCH_PAGES` either side, **aborts** the requests a jump has
  travelled past, and evicts down to `WINDOW_MAX_PAGES` so memory stays bounded however far the reader goes.
  `useGridScrollMemory({key,count?,track?,restoring?})` = **where the reader was in a photo grid**, remembered
  per view for the browser session over `lib/gridScroll`. Browsing is a constant grid → photo → grid, and
  every step used to land back at the top (measured on the live instance: Back from `scrollY=4000` gave 195),
  which put the older end of a 20 000 photo library out of reach. It returns `restoreFrom` (the snapshot for
  `PhotoGrid`'s `restoreStateFrom`) and `restoreScrollY` (for a grid that renders its own tiles), and takes
  `onStateChanged` back from the grid. `track:'window'` records `window.scrollY` off a passive scroll listener
  instead (the person gallery), and `restoring:true` suppresses every write while the caller is still driving
  the view to its position — the offsets on the way there must not overwrite the one being restored; the
  virtuoso path needs no flag, it simply ignores a reported offset of zero until the grid has been seen away
  from its top. Nothing here re-renders the caller (refs + a 200 ms debounced write, flushed on unmount and on
  `pagehide`, so leaving for a photo always records the position), and an untouched view is never written, so
  opening a photo without scrolling keeps what the last visit left. The restore length (`count`) is read back
  by the page itself (`readGridScroll(key)?.count`) because it feeds the list hook *above* this one.
  Both ways back are the same history pop — the browser's Back button and the viewer's „Zpět na seznam"
  (`PhotoDetailPage` closes with `navigate(-1)`) — so both restore. Wired into `LibraryPage` (windowed, so
  `count` stays 0: the grid is as tall as the whole result from its first response), `AlbumDetailPage`,
  `LabelDetailPage`, `SubjectPage` (`track:'window'`), `FavoritesPage`, `SearchPage` and `PlacesPage`.
  A failed page is retried on the next range change up to `WINDOW_MAX_ATTEMPTS`, then surfaces as `moreError`
  (footer retry); a `reloadKey` bump refetches exactly the loaded pages in the background. It also passes the
  response's `unknown_tokens` through as `unknownTokens` (every page of one query carries the same verdict on
  `q`, and a query change resets it), which is what lets the library raise `UnknownFiltersAlert` for a mistyped
  filter key instead of showing an empty grid that reads as „not in the library".
  This is what makes the timeline's jump cost **independent of distance** — measured on a seeded
  20 889-photo library, clicking the oldest year went from **40.3 s / 102 sequential page requests**
  (the old `useGridJump`, which paged its way to the target) to **~3.1 s**, the same as a jump one month
  back; `useSelection` = multi-selection of photos in the grid
  (`active`/`selected`/`count`/`enable`/`disable`/`toggle`/`selectMany` (select-all-in-view)/`clear`);
  the last `toggle` holds the **anchor** and `toggleRange(uid, orderedUids)` (Shift+click) selects a contiguous
  range between the anchor and `uid` — it only **adds**, without an anchor or with an anchor out of order it degrades to
  `toggle`, `clear`/`disable` drop the anchor;
  `useBulkEdit({onEdited?, hoverSelect?})` = a **reusable bulk edit** of an arbitrary photo list:
  `useSelection` + a role gate (`canBulkEdit` = `canWrite`) + the dialog state
  (`editing`/`open`/`close`/`finish`), plus `photoUids` (**exactly the selected ones**, never the whole filtered
  result) and `gridSelection` straight into `PhotoGrid` (incl. `onToggleRange` → a Shift+click range for free
  in every grid). **`hoverSelect:true`** (**all photo lists**: the library, an album/label detail,
  favorites, search, places, a person's gallery): `gridSelection` is **always** defined for an editor
  with `hoverSelect` (no explicit entry into the mode — a corner checkbox on hover) and the page
  shows `SelectionBar` on `selection.count > 0`, not on `selection.active`; without it (only /expand)
  `gridSelection` is defined only after `enable()`. A viewer always gets `undefined`.
  `finish(outcome?)` = close the dialog → `selection.clear()`
  → `onEdited(outcome?)` (refetch; `outcome` = `BulkEditOutcome` for pages that can edit the list
  in place — `/expand`); the selection mode survives, so after a success you can keep selecting right away and no
  stale UID is left in it. A failed apply **leaves the selection alone**. The page wires only
  `gridSelection` (and `SelectionStart` in the explicit mode), the rest is handled by `BulkEditControl`;
  `usePhotoShare(photoUids)` = **handing photos to the phone's own share sheet** (iOS "Save Images" → Apple
  Photos, Android → Google Photos): `{supported, status, error, busy, share()}`. `share()` asks
  `fetchShareManifest` what the selection is as files, splits it with `splitShareBatches` and fetches **one batch
  at a time** (`fetchShareFile`) into `navigator.share({files})`. The arithmetic and the browser check are pure and
  live in **`lib/photoShare`**: `SHARE_MAX_FILES` = 20 files and `SHARE_MAX_BYTES` ≈ 300 MB per handoff (whichever
  is reached first; a file over the whole budget gets a batch of its own rather than being dropped, so nothing is
  ever truncated), plus `SHARE_PREVIEW_SIZES`, `canSharePhotoFiles` and `isShareAbort` — tests
  `photoShare.test.ts`. `supported` is `canSharePhotoFiles()` probed
  **once per mount** — the component renders nothing when it is false, and `share()` fetches nothing either.
  `status` = `idle` | `manifest` | `fetching {batch,batches,done,total}` | `sharing` | **`waiting
  {batch,batches}`**, and that last one shapes the whole feature: a share sheet may only be opened from a **fresh
  user gesture** (iOS throws otherwise), so a multi-batch selection is never chained — the sequence pauses and the
  button asks for the next tap. Files live only for their own batch, so a 400-photo selection never holds 400
  files at once. `error` is **structured, not a message** (`{kind:'manifest'|'tooMany'|'fetch'|'sheet'}`, `fetch`
  carrying `{name,count}`) so the hook needs no `t`; a cancelled sheet (`isShareAbort`) ends the sequence
  **quietly** and is not an error at all, a photo that will not download is skipped, named, and the remaining
  batches stay shareable, and a sheet that refuses for another reason keeps the sequence **on the same batch** so
  the next tap retries it instead of skipping its photos. A changed selection clears a stale message but never
  disturbs a sequence already under way — those batches were asked for. Tests `usePhotoShare.test.tsx`;
  `useReloadKey()` = `[key, reload]`, a string counter for a photo list's `reloadKey` — a single `reload()`
  replays the list **in the background** (a refetch of the first page without blanking into a skeleton, the photos stay
  pinned); `reload` is stable and goes straight into `useBulkEdit({onEdited})`;
  `useDocumentTitle(title)` = **the browser tab's name**, `<page> · Kukátko` while the caller is
  mounted. Every page calls it (`hooks/useDocumentTitle.ts`) — one hook, not a router-side table,
  because the titles worth having are a photo's name, a person's, an album's, i.e. data only the page
  holds. `null`/`undefined`/blank (a detail still loading) → the bare app name, never a stale or
  invented one; the string is trimmed before it reaches the dependency array, so a page recomputing an
  equal title does not re-run the effect. The **cleanup restores the app name**, which is what keeps a
  photo's name off the library — React runs the outgoing page's cleanup before the incoming page's
  effect, so a navigation between two titled pages still lands on the new one. Both the separator and
  the brand's position are i18n (`documentTitle.page` = `{{title}} · Kukátko`, `documentTitle.app`),
  and a language switch re-titles the tab because `t` changes identity with the language. Static pages
  pass their own `<h1>` key (`t('albums.title')`, …); the dynamic ones pass data — an album via
  `documentTitle.album` under the same `albumDisplayTitle` the heading uses, a label via
  `documentTitle.label`, a person as the bare `subject.name`, a photo as the viewer's own one-string
  name (`photoName`, the `<h1>`'s title-or-facts form), `/search` as `documentTitle.search`
  („{{query}}") off the **URL** query rather than the debounced input, and `/audit/reviews` as the
  player whose decisions it lists. On the pages whose render begins with early returns
  (photo/album/label/subject detail) the call sits **above** them — a hook may not be conditional;
  `useKeyboardShortcuts(handlers,{enabled?})` = the shared plumbing of all keyboard shortcuts: a single
  document-level `keydown` listener dispatches by the normalized `shortcutToken(event.key)` onto
  `handlers` (via refs, bound once and always seeing the current closures), a matched key `preventDefault`s;
  it **never fires** while Ctrl/Meta/Alt is held, while typing (`isTypingElement`) or with a form modal
  open (`isFormModalOpen`);
  `useAutoHideChrome({idleMs?,paused?})` → `{visible,wake}` = the **disappearing chrome** of the immersive
  viewer (`PhotoDetailPage`): the controls start visible, after `idleMs` (default 2600 ms) without
  activity they fade out and return on the next activity. It watches activity **globally** (pointer move/down,
  key, touch), holds visibility in a ref and commits to state **only on a real change**, so a
  flood of `pointermove` doesn't re-render every frame; `paused` **pins the chrome visible** and doesn't start the
  timer (when the drawer is open). It decides only *whether* the chrome shows — *how* it animates is handled by
  a CSS transition on duration tokens (under `prefers-reduced-motion` ~0) — and it doesn't decide **what** hides
  either: it sets one `data-chrome` flag on the viewer root and `viewer.css` picks the surfaces that answer to it.
  Two deliberately don't (N20): the persistent back control and the photo's title, which dim rather than leave, so
  an idle phone screen is never a photograph with no name and no visible way off it;
  `useGridKeyboardNavigation({count,enabled,resetKey,getColumns,
  scrollToIndex,onOpen,onToggleSelect,onToggleFavorite,hasSelection,onClearSelection})` = grid navigation
  over `useKeyboardShortcuts`: it holds `focusedIndex` (the highlight), the arrows + `j`/`k`/`h`/`l` move
  (left/right by 1, up/down by a row based on the live column count) and scroll the tile into view, `Enter`
  opens, `x` selects (turning on selection mode), `f` toggles the favorite, `Escape` clears the selection first, then the
  focus; the focus resets on `resetKey` (a new filter/sort/scope);
  `useSwipeNavigation({onSwipe,enabled?,threshold?})` → `{onTouchStart,onTouchMove,onTouchEnd}` =
  a horizontal **swipe on touch → prev/next** on the detail's image; it reads only the start/end of the touch and
  **never calls `preventDefault`**, so a mostly-vertical drag falls through to the native scroll (`lib/gestures`
  `swipeAction` decides: a threshold + a dominant horizontal component). The gesture is dropped on a second finger
  (pinch) and when it **starts on an interactive element** (`button`/`a`/form) without `data-swipe-surface` — so
  a tap on a face box/arrow doesn't page, only the image itself does (its button carries that attribute). The mouse
  on the desktop never comes here, the gesture is purely additive for touch;
  `useSyncZoom({resetKey})` → `{view,zoomed,dragging,handlers,zoomIn,zoomOut,reset}` = **one**
  zoom/pan state for **both** photos in `DupComparePage`: both `<img>`s render the same `ZoomView`, so they
  are synchronous **by construction** — there is nothing to copy between the panels and nowhere to diverge. The wheel
  zooms toward the cursor, dragging pans (only when zoomed in), a double-click toggles fit ↔ 3×, a change of
  `resetKey` (the pair's id) returns to fit, so the next pair doesn't inherit the zoom. **It is not
  `usePinchZoom`:** that one is touch-only and measures against `window` (the image fills the viewport), here it is
  about a mouse in two halves of the screen, so the box is passed in; the pure maths lives in
  `lib/compareZoom`,
  `useComparePair(pair)` → `{data,loading,error}` = loads both sides of the comparison (`fetchPhoto` ×2 +
  `fetchFaces` ×2, in parallel, `AbortController`); if either fails, the whole pair fails — half of the
  diff table would lie by omission,
  `usePinchZoom({onSwipe,resetKey,enabled?})` →
  `{scale,translateX,translateY,isZoomed,gesturing,handlers,reset}` = the **pinch/double-click zoom** of the fullscreen
  lightbox with **pan** while zoomed in and swipe paging at rest: two fingers scale (`pinchScale`, clamped to
  `[1,4]`), a **double-click** toggles fit ↔ `DOUBLE_TAP_SCALE` (zooming toward the tapped spot), a drag of a zoomed-in
  image pans (clamped by `clampPan`, so it can't leave the screen), a drag at rest decides a swipe (`swipeAction`);
  **the zoom resets when `resetKey` changes** (the displayed photo) and on close (the lightbox unmounts). The surface
  has `touch-action:none`, so `preventDefault` isn't needed and the browser doesn't override the gesture;
  `useFaces(photoUid)` = loads a photo's faces (`fetchFaces`) and holds the naming state machine
  (box selection, an optimistic assignment, a reconciling refetch against the server, `busy`/`actionError`);
  extracted from `FaceOverlay`, so that the detail can draw the boxes over its single image and render the naming
  panel elsewhere on the page. **The detections are put into reading order the moment they land**
  (`readingOrder`, in the one place all the consumers read from): everything numbers and walks faces **by their array
  position** — the boxes, the panel rows, the people chips, `firstUnnamed`/`nextUnnamed` — and detection order is the
  model's own, which on the reported group photo put #1 in the middle, #3 to its left and #5 at the far left.
  **After loading it selects the first unnamed face**
  and **after an assignment it moves the selection to the next unnamed one** (`firstUnnamed`/`nextUnnamed`, ordered by
  **the order in the array**, not by `face_index`; `facesRef` against a stale closure) — so you get through a group
  photo without reaching for the mouse. `unassign` **keeps** the selection (the face has just been freed and you
  typically rename it right away). The reconciling refetch after a mutation does **not** trigger auto-selection (`reload(signal, autoSelect)`),
  otherwise naming the last face would jump back to the top. **The detail pages prev/next without
  remounting the hook**, so every response is checked against the photo it was asked for: `currentUidRef`
  (the uid of the current render) drops the reconciling refetch of a photo the reader has already left — and it does so _before_
  it takes a `latestRequest` id, so as not to kill the in-flight load of the new one; `latestRequest` then handles
  skipping a slow response superseded by a newer one for the same photo. Without it a slow `reload(A)` would repaint
  photo B's faces and the next assignment would send a `marker_uid` from A against B (404). For the same reason
  `busy`/`actionError` are zeroed at the start of every photo and an error/completion of a mutation is written through only
  when the reader is still on the same photo;
  `useSubjects()` = a lazy list of all subjects for the typeahead (it mounts only with `FacesPanel`,
  so merely viewing a photo never pays for it; an error = an empty list, the field then only creates new ones);
  `useCandidateReview(subjectUid,candidates)` = the state machine of the `/faces` review grid: it seeds the
  working list from a fresh search and applies ✓/✗ **optimistically** (the grid doesn't reload);
  `confirm` flips the card to `done` and calls `assignFace` (an error → `error` for a retry, it doesn't touch
  the neighbours), `reject` removes the card + `rejectFace` (on an error it puts it back), `confirmAll(tab)` walks
  one tab's actionable cards sequentially with `confirmAllState` `{running,current,total,failed}`,
  cancellably (`cancelConfirmAll`), a partial failure doesn't roll back and is reported via `actionError`;
  `useSweepReview()` = the orchestrator of the `/recognition` sweep (the multi-person variant of review): it streams
  via `streamSweep`, collects one `PersonState` per person with matches as they arrive (`progress`/`person`/
  `summary`), `confirm`/`reject`/`confirmAllForPerson` apply **optimistically** by the same rules
  (`buildAssignRequest`/`buildRejection` from `candidateReview`), `people` returns only people with actionable
  cards (a person disappears once the last one is dealt with); `cancel`→`AbortController`, one `confirmAll` runs
  at a time; it never auto-confirms;
  `useOutlierReview(subjectUid,faces)` = the state machine of the `/outliers` grid: it seeds the working list
  from a fresh query and applies both verdicts **optimistically and in place** — the card flips where it stands,
  the grid doesn't reload and the scroll doesn't run away from the curator in the middle of a long list. The verdicts are
  **opposite and aim at opposite endpoints**: ✓ `unassign` detaches the person via the ordinary assign machine,
  ✗ `confirm` writes a **permanent confirmation** (`confirmFace`), which the backend then excludes from further outlier
  queries — a list that keeps offering the same false alarms is exactly the problem this page
  solves. A failed write marks **its own** card `error` and doesn't touch the neighbours. `unassignMany` walks
  the selection **sequentially** and **acknowledges partial failure** (`bulkState{running,current,total,failed}`,
  cancellable): the already removed ones stay removed, the errors are counted and reported, not rolled back
  or swallowed. New `faces` (a different person/threshold) reset everything and abandon a running run;
  `useReviewGame()` = the engine of the sorting game (`/review`): **one round at a time**. A fetch is exactly one
  round (`fetchReviewQueue`, deduplication against **all** ids already seen), laid out as **cards** by
  `lib/reviewRounds` — the questions plus the round's breathers, which answer nothing. Consuming the last card
  closes the round into a `RoundSummary` (a snapshot, so the between-rounds card reports what was **played**, not
  what is left) and starts loading the next one **behind** that card, which is what makes „Ještě kolo?" instant
  while the pause is real; the refill first **awaits the in-flight answers**, because the backend mints the next
  round only once the current one is answered and would otherwise hand the same one back. The hook also owns the
  session's own bookkeeping: `round` (index/size/`played`/tallies/`daily`/`last`), `combo` (consecutive answers —
  a skip resets it, an undo restores what it was), `session` (the yes/no/skip split), `touched` (the photos
  decided about, capped at `TOUCHED_LIMIT = 24` for the closing mosaic), `milestone` (10/25/50, via
  `milestoneCrossed`) and the **daily-mix flag** in `localStorage` (`dailyMixDone`/`markDailyMixDone`, keyed on
  the **local** day — a UTC key would flip over mid-evening). A `reveal` on an answer response is spliced into
  the round in flight by `insertReveal`: it takes the next **breather** slot if there is one, otherwise a slot of
  its own right behind the card on screen, and only ever **one per round**. `advance()` moves past a card that
  asks nothing and deliberately touches **no** counter — a pause that scored points would not be a pause.
  Answers stay **optimistic** (`answer` moves the UI
  immediately and the request finishes in the background; a failure falls into `failed` for an explicit retry — it never blocks
  the rhythm nor silently loses a verdict) and a **single-step undo**. The queue has its **source of truth in a ref**, not
  in state: two answers can fit into one render (arrows at speed) and reading the head from state would
  answer the same card twice. `undo` goes through the **inverse** write paths (`unassign_person`,
  DELETE feedback-rejection, detaching a label), because `POST /review/answer` is **idempotent per
  question** — and for the same reason a **re**-answer to a restored question is sent via the direct paths
  (`sendDirect`), otherwise it would no-op as `already_answered`. Both cover all five kinds: a duplicate verdict
  reverses through `unconfirmDuplicate`/`undismissDuplicate`, an outlier yes through `unconfirmFace` and an
  outlier no by **re-assigning** to the marker it detached the person from (the marker survives the detach —
  that is exactly why the outlier "no" goes through `unassign_person` rather than invalidating anything).
  **`isUndoable` withholds undo from a place verdict alone**, and says why: no write path can mark a location an
  *estimate* again, so the only "undo" available would write the old coordinates back as a *manual* decision —
  a different state, silently attributed to the user. The answer is therefore not held as the undo target at
  all, leaving the button disabled instead of offering a lie; undo first **waits for the in-flight**
  request, so the inverse doesn't overtake the answer it is undoing, and a `create_marker`-yes looks up the created
  marker via `fetchFaces`, so a possible later re-yes is an `assign_person` on **the same** marker,
  not a duplicate. `useReviewGame(source)` takes **what the game asks about** (`both`/`people`/`labels`, owned by
  the URL on `ReviewPage`): a change **throws the local queue away** instead of filtering it (those cards are
  exactly what the player turned off) together with the exhausted/error latches and the undo target, while the
  answered counter and the seen-set survive — they are about the session, not the selection. A batch that was
  in flight during the switch is recognised by the `source` the response **echoes** and dropped; because the
  refill effect already ran behind the in-flight latch, dropping it also **bumps a reload key**, otherwise
  nothing would ever fetch the source the player actually chose;
  `useReviewSwipe({onVerdict,enabled})` = the game's **touch input**: right Ano, left Ne, down Nevím (the pure
  decision is `swipeVerdict` in `lib/gestures`, in the directions the arrow keys already use, so the two input
  methods can never disagree). It returns the live `offset` the card is translated by, the `hint` — the verdict
  the drag is heading for, already at `VERDICT_HINT_THRESHOLD = 24 px`, far below the committing
  `VERDICT_THRESHOLD = 64 px`, because a gesture whose outcome only shows **after** it fires is one nobody dares
  use on real photos — and `dragging`. An **upward** drag decides nothing (there is no fourth answer, and up is
  how a phone is scrolled), a gesture that starts on a control (the corner anchor, a button) is ignored unless it
  opts in with `data-swipe-surface`, and a second finger abandons it: two fingers on a photo is a pinch. Only
  touch handlers are bound, so mouse and keyboard are untouched;
  `useReviewStreak()` = the 🔥 day-streak for the game header, read from the caller's own row on
  `GET /review/leaderboard` (`streak_days`) — **render only**, there is no second source of truth. A failure is
  silence (0, which renders as nothing at all): flavour must never be able to stop the game;
  `useFavorite(uid,initial,onChange?)` = an **optimistic** per-user favorite toggle over `favoritePhoto`
  (`PUT`/`DELETE …/favorite`), rollback on an error, ignores a concurrent toggle, resyncs on a change of
  `uid`/the server state; the optional `onChange` reports **every** flip of its own (optimistic and rolled-back,
  the latter even after the tile has unmounted — the list's owner outlives it) upwards, so that a page which favorites the same
  photo by another route as well (the `f` key in the library) holds **one** initial state, not two that have diverged;
  a resync isn't reported, that one comes from the owner; `useRating(uid,initialRating,initialFlag)` = an **optimistic** per-user
  rating (stars) + pick/reject flag over `ratePhoto` (`PUT …/rating` with only the changed field),
  `setRating`/`setFlag` with a per-field rollback on an error, a no-op on an identical value, `pending` via an
  in-flight counter, a resync on a change of `uid`/the server state (mirroring `useFavorite`);
  `useThumbSrc(uid,thumbUrl)` → `{src,failed,onError}` = **resilience against an expired signed URL**:
  the `thumb_url` in the payload may be a short-lived signed address of the media Worker (default 1 h), so a
  payload held in a virtualized list or carried across a longer idle gives the `<img>` an address
  the Worker rejects. The first `onError` therefore refetches the photo **once** (`fetchPhoto`) and tries it
  with a freshly signed URL; a second failure, a failed refetch, an empty or **unchanged** address (which is what the
  filesystem backend does — its URLs are routes and don't age, so a failure = a genuinely missing preview) → `failed`
  and the caller renders a placeholder. A new `thumbUrl` prop (a new page of results) resets the retry budget.
  It is solved this way, **not with a long TTL** — a short lifetime is the whole point of signing. Used by
  `PhotoTile` and `TrashCard`;
  `useSlideshow({length,hasMore,intervalMs,repeat?,autoPlay?,onLoadMore?,readiness?,maxHoldMs?})` = the control of the
  slideshow: it owns `index`+`playing`+`holding`+`pass`, `next`/`prev`/`play`/`pause`/`toggle`/`goTo`,
  wrap-around, prefetch of `PRELOAD_AHEAD` pages ahead
  via `onLoadMore` (at the end with another page it waits instead of looping), an empty set = a no-op, a clamp of the
  index when the set shrinks. **`repeat`** (default `false`) decides the end: with it the show wraps to
  the first photo and `pass` (the round counter — what tells the page its per-pass bookkeeping starts
  over) increments; without it auto-advance **stops** on the last photo, leaving it on screen with
  playback paused. A manual `next` from the end wraps either way — that is the reader steering. **Auto-advance is guarded by `readiness(index)`**: an elapsed interval
  doesn't switch the slide but starts a *hold* — it switches the moment the next frame is `ready` (decoded),
  after `maxHoldMs` (default `MAX_HOLD_MS` = 10 s) it switches anyway, and a slide with `error` is **skipped**
  (a broken frame doesn't block the show). Manual navigation and a pause cancel the hold (manual never waits, a resume
  starts a fresh interval), the interval can be changed **during a hold** without restarting/duplicating the timer
  (the timer isn't armed during a hold, the hold's deadline doesn't depend on `intervalMs` or on `readiness`).
  A set of < 2 frames neither holds nor switches. `preloadWindow(index,length)` = the indices to preload
  (`PRELOAD_AHEAD` ahead, `PRELOAD_BEHIND` behind, the current one first, the offsets **wrap** →
  at the end of the show the first frames are ready for the wrap-around, with a small set it dedupes);
  `useImagePreloader()` → `{statusOf(url),prime(urls)}` = preloads a window of images and reports
  `pending`/`ready`/`error`. `prime(urls)` is **the whole window** — anything outside is released at once
  (`removeAttribute('src')` = aborting the in-flight fetch), the last window is released on unmount, so a
  long show doesn't accumulate decoded bitmaps. Readiness is measured by **`img.decode()`**, not by `onload`: onload
  means „the bytes arrived", the decoding would only happen at the first paint (exactly the flash of
  empty space this whole thing exists for); `decode()` is feature-detected (jsdom doesn't have it →
  a fallback to `onload`/`onerror`). A late `decode()` of an already released image is ignored. The statuses live
  in state → `statusOf` changes identity on every settle, so an effect can depend on it;
  `useSlideshowSettings` = the persistent slideshow preferences over
  `lib/slideshowSettings` (read once on mount, one `update(patch)` writes into localStorage,
  sanitization). The **start dialog does not use it** — it edits a draft and persists on confirmation,
  so dismissing it changes nothing;
  `useGridDensity()` → `{density,setDensity,maxColumns,storedDensity}` = the photo grid's density (**always a
  concrete column count 1…10**, no `'auto'` mode) over `useSyncExternalStore` on top of `lib/gridDensity`. localStorage is
  **the single source of truth** (no in-memory copy): the snapshot is a primitive (a column count, or `null`
  = nothing usable stored), so React's `Object.is` comparison never loops. **On first
  use** (empty storage or an older `'auto'`/broken value to be migrated) the density is seeded **once**
  from the viewport width (`initialColumns`) and stored — auto only ever seeds the first value, after that it is
  hard-coded to the user's choice and a later resize **doesn't move it**. What a resize *does* move is the
  **ceiling**: a second `useSyncExternalStore` (on `resize`/`orientationchange`, snapshotting
  `maxColumnsForViewport()` — the ceiling, not the width, so dragging a window edge only re-renders where the
  grid changes shape) gives `maxColumns`, and `density = min(storedDensity, maxColumns)`. So a phone renders
  at most 3 columns and a small tablet 4 **whatever is stored**, while `storedDensity` (and localStorage) keep
  the user's own number for the wide window it was chosen on. `subscribe` also listens to the `storage`
  event → all tabs on the device hold the same column count; `setGridDensity` sanitizes, writes
  and repaints **all** grids at once, without a context and without a provider (so page tests work
  without a wrapper too). It takes an optional **`GridDensityScope`** (default `LIBRARY_GRID_SCOPE`): the
  key, seed tile and gutter of *one* grid. Every subscriber is woken by every change but each reads its
  own key, so `/outliers` (`OUTLIER_GRID_SCOPE`) and the library never move each other's number; the scope is
  re-`useMemo`d from its fields, not used by identity, so an inline `{…}` at a call site can't thrash the seed;
  `useImageFrame({source,width,height,orientation})` → `{frame,measured,aspectRatio,ratio,imgProps}` =
  **the frame a face box is positioned against, taken from the loaded image**. Wherever a box is drawn over a
  full-frame photo the wrapper *is* the box's coordinate system (the box is percentages of it), so the wrapper
  must sit exactly on the rendered pixels — and the catalogue row cannot be trusted for that: the imported
  import stored already-oriented dimensions that `displayFrame` rotates a **second** time, so ~8.6 % of the
  library carries a transposed pair (`phqale6fftf3a3v5tn17vtfd3d`: stored 3000x4000 + orientation 6 over a
  portrait file → a 4/3 wrapper around a 3/4 image, every box's x/width stretched by 1.78, one of the three
  faces off the right edge entirely). `naturalWidth`/`naturalHeight` **are post-orientation**, so the row
  survives only as the **initial estimate** (no layout jump while the image loads) and the measurement takes
  over on load; `measured` says which one you have. Callers spread `imgProps` onto the image and **draw no box
  until `measured`** — a box against a provisional frame lands off its face and then visibly jumps. `imgProps`
  is `ref`+`onLoad`, not just `onLoad`: the `ref` catches an image already complete before React attached the
  handler, and it demands `img.complete` before believing what it reads, or a `src` swapped to the next photo
  (the element keeps reporting the old image's size until the new one arrives) would stamp the previous frame
  onto it. Used by `PhotoDetailPage`+`FaceOverlay`, `CandidateFaceImage`, `ReviewPhoto` and
  `DuplicateMarkerGroupCard`'s whole-photo preview — one implementation, four call sites. The measurement is a
  **shape** (a `fit_*` thumbnail, not the original), so never read it as the original's pixel count; the
  square-tile croppers keep taking `displayFrame` from the row (see the viewer's invariant above). Tests
  `hooks/useImageFrame.test.tsx` + `test/imageFrame.ts` (`loadImageAs`/`frameRatio` — jsdom fetches nothing, so
  a test about a box has to report the load itself);
  `useIsNarrowViewport()` = a shared hook over `matchMedia` (`(max-width: 767.98px)`, Bootstrap `md`;
  it removes `change`, a missing/broken `matchMedia` → „wide"; the single source of truth for the filter
  offcanvas, the default grid density, the collapse of `BatchActionBar` and `HeaderActions` into the „…" overflow menu on a phone, and the move
  of the viewer's curatorial loop from the top bar to the bottom dock within thumb's reach);
  `useIdleChrome({delayMs,held})` → `{visible,wake,toggle}` = **when a player's controls are on screen**:
  visible on mount, hidden after `CHROME_IDLE_MS` (3 s) of nothing, back on `wake()` (a mouse move, any key) and
  toggled by `toggle()` (a tap — a finger has no „movement" to report, so the tap *is* the request). `held`
  pins them and stops the countdown (a panel open, the pointer resting on them, focus inside them) and
  releasing it starts a **fresh** countdown, so nothing ever times out from under a hand. Only `visible` is
  state — `wake()` on every mouse move sets `true` on an already-`true` state, which React bails out of, and
  the timer lives in a ref;
  `usePrefersReducedMotion()` = follows `(prefers-reduced-motion: reduce)` via `matchMedia`
  (removes `change`, a missing/broken `matchMedia` → `false`) — the caller **omits** a decorative animation,
  it doesn't shorten it);
  `useCoarsePointer()` = follows `(pointer: coarse)` via `matchMedia` (the query itself is the app's single
  spelling of it, `lib/mapGestures.COARSE_POINTER_QUERY`; removes `change`, a missing/broken `matchMedia`
  → „fine"). The **layout-independent** half of the app's two responsive questions: `useIsNarrowViewport`
  answers *how much room is there*, this one *how precisely can it be pointed at* — a tablet is wide and
  still touched, a 500 px desktop window is narrow and still has a mouse. Asked by a control whose **hit
  area** has to grow in a way CSS alone cannot deliver: `TimelineScrubber` thins its rail to 44 px targets
  by it)),
  `lib/` (`gestures.ts` = **pure, DOM-free decision helpers for touch gestures** shared by
  `useSwipeNavigation`/`usePinchZoom` (and therefore **directly unit-testable** without jsdom touch sequences):
  `swipeAction(dx,dy,{threshold,ratio})` → `'prev'|'next'|null` (left = next, right = prev, a threshold +
  a dominant horizontal component), `touchDistance`/`touchMidpoint`, `pinchScale`/`clampScale`
  (clamped to `[MIN_SCALE=1,MAX_SCALE=4]`, `DOUBLE_TAP_SCALE`), `isDoubleTap(dt,dist)` and `clampPan`;
  `compareZoom.ts` = the **pure zoom/pan maths** of the synchronous canvas in `DupComparePage` (and therefore
  unit-testable without a DOM): `ZoomView{scale,x,y}`, `IDENTITY_VIEW`, `MIN_SCALE=1`/`MAX_SCALE=8`/
  `ZOOM_STEP`, `zoomAt(view,factor,px,py,box)` (the point under the cursor stays under the cursor), `zoomCentre`,
  `panBy`, `clampView` (the pan stays within `(scale-1)*box/2`, so the image can't be dragged out of the panel),
  `isZoomed`, `viewTransform`; deliberately separate from `gestures.ts` — that one is touch-only and measures against
  the viewport;
  `duplicateCompare.ts` = the **pure logic of pair comparison**: `buildPairQueue(groups)` → `ComparePair[]`
  (a multi-member group **pair by pair against the keeper**, never member-to-member; a group whose keeper is not among
  the members is skipped, not guessed), `pairId(a,b)` (unordered, like the backend), `pairsInGroup`/
  `pairIndexInGroup` (the caption „dvojice i z n"), `dropPairsTouching(pairs,uid)` (after a merge the pairs of the
  archived photo disappear), `buildDiffRows(left,right,fmt)` → `DiffRow{key,left,right,differs}` —
  `differs` is computed from **the comparison key, not from the formatted text** (two times within the same minute
  still differ), names are compared as a **set** (the order from the API means nothing); `fmt` is
  injected, so the tests don't depend on the locale; `countDiffering(rows)`;
  `version.ts` = the two pure display helpers over the build metadata carried by `Capabilities`:
  `formatVersion(info)` (a semantic version gets the customary `v` prefix, `0.5.1` → `v0.5.1`; anything
  else — notably the `dev` placeholder of an un-stamped binary — is shown as it is; `null` when there is
  nothing to show, so the caller renders nothing rather than an empty line) and `commitUrl(info)`
  (`https://github.com/panbotka/kukatko/commit/<sha>`, or `null` unless the commit is 7–40 hex characters,
  which is what keeps a development build's `none` from becoming a dead link);
  `gridScroll.ts` = the **session store of grid positions** behind `useGridScrollMemory`: one
  `sessionStorage` entry (`kukatko.gridScroll`) holding `{snapshot?,scrollY,count}` per view key, plus
  `gridScrollKey(pathname,search)` — the path and the query that defines the *result set*, sorted for
  stability and with the position-only params (`at`, `info`) dropped, so a timeline jump keys to the same
  view while a changed filter keys to a different one and can never restore an unrelated position. Session
  (not local) storage: a position is worth restoring in the tab that scrolled it and worth forgetting by
  tomorrow. Reads are validated field by field (storage is shared with other builds, and a half-read
  snapshot handed to virtuoso would restore a nonsense layout), the store is an LRU of
  `GRID_SCROLL_MAX_ENTRIES` (16) views, and every failure — disabled storage, a full quota, foreign JSON —
  costs the reader their position and nothing else;
  `urlState.ts` = the `useUrlState` hook +
  the pure `readUrlState`/`writeUrlState`: the view state ↔ the URL query via the History API, „Back always
  works"; `libraryView.ts` = the `LibraryView` type (incl. `min_rating`/`flag`, the `favorite` toggle, the
  capture-date tri-state **`dated`** and the facets
  `album`/`label`/`person`) + `LIBRARY_DEFAULTS` + **`ALBUM_DEFAULTS`/`ALBUM_SORTS`** (the same view
  resting **oldest-first** for a grid scoped to one album, and the two orders such a grid offers — so
  only a deliberate reversal ends up in the URL, and `backHref` measures an album link against these) +
  **the capture period as one filter**: `periodOf(view)` is the single accessor of the time axis
  (`taken_after`/`taken_before`, sanitised to `YYYY-MM-DD` so a hand-typed URL degrades to „open" instead of a
  400; when both are empty a **legacy** `year=1965` — old bookmarks and saved searches — is folded in as that
  year's period) and `periodPatch(period)` the single writer (it clears `year`, so the two can never both be
  set); `viewToParams` sends it as `taken_after` + `takenBeforeParam(to)`, the **end** of the last day
  (`…T23:59:59.999999Z`) — `taken_at <= taken_before` would otherwise drop everything shot that day after
  midnight, and a decade would lose its last New Year's Eve.
  **`dated`** (`''` any / `'true'` has a date / `'false'` none) is the one filter with no list param behind it:
  the backend spells it as the query language's `dated:yes` / `dated:no`, so **`queryWithDated(view)`**
  compiles it into `q` and `viewToParams` sends that instead of `view.q`. Appending is safe on the search
  page too — the backend splits `q` into free text and filters and only the free text is ever embedded —
  and a `dated:` **already typed into the query wins**, nothing is appended (two contradictory tokens would
  match nothing, and `FACET_QUERY_KEYS.dated` is what makes the control admit it) +
  `lib/period.ts` (pure, no React: the `Period{from,to}` pair of inclusive UTC calendar days — one
  representation for a decade, a year, „před 1950" and „léto 2019" — plus `periodForYears`/`yearSpanOf`
  (a whole-year span only when both bounds sit on year boundaries, so „léto 2019" is never reported as
  „2019"), `groupYearsIntoDecades`, `parseYearRange`/`periodFromQuery` (the shapes the backend's number range
  accepts: `1965`, `1960-1969`, `1960-`, `-1949`) and `formatPeriod`. UTC is what makes a picked decade
  return exactly what the year facet counted: EXIF times are read as UTC wall clock and the DB session is
  pinned to UTC, so `date_part('year', taken_at)` and these bounds agree by construction);
  `lib/takenDate.ts` (pure: how much of a capture date may be **shown**. `TAKEN_PRECISIONS`/
  `TakenPrecision` mirror the backend's `taken_at_precision`; `isCoarsePrecision` (an absent or unknown
  grain is an ordinary date, the safe reading); `formatTakenPeriod(takenAt, precision, t, locale)` → „1974",
  „červen 1974", „1970–1979", and `''` for a plain date so the caller falls back to its own date rendering;
  `formatDecade` names a decade as the span „1970–1979" in both languages — the same shape `lib/period`'s
  `formatPeriod` writes for a picked decade, so the filter and the caption cannot disagree;
  `formatTakenLabel(photo, t, locale)` is the **one form anything showing a date to a reader wants** —
  the period when the grain is coarse, otherwise the locale's date, prefixed with the estimate marker
  („cca") when the date is a guess, and `''` when there is no capture time at all, which callers render
  as nothing rather than as a placeholder; the three rules belong together, since applying two of them
  would claim a precision or a certainty the catalogue never had). The anchor is
  read in **UTC**, the zone it was stored in: read locally, a photo dated
  „1974" would be 31 December 1973 west of Greenwich and would contradict the year facet it sits in. Used by
  `MetadataPanel` (the read-only capture-date row + a `Form.Text` in the edit form saying which period the
  instant in the field stands for), `PhotoTile` (the alt text), `SlideshowCaption` and `BulkEditModal`);
  `lib/lifeYears.ts` (pure: `captureYear` (the **UTC** year of an ISO capture time — a local reading would
  move a New Year's Eve photograph into the wrong decade), `approximateAge(takenAt, birthYear)` = the plain
  difference of two years, `null` whenever nothing can be said: no date, no birth year, a photo dated
  **before** the birth, or an age past `MAX_PLAUSIBLE_AGE` (120) — an absurd number beside a face would
  present a data error as a fact — and `formatLifeSpan` → „1923–1998" / „*1923" / „†1998" / `null`;
  the labels themselves are i18n (`subject.age` with a plural count, „~23 let" / „~23 yrs"));
  `lib/photoDecades.ts` (pure, built on `period`'s `decadeOf` so the decade **filter** and this decade
  **grouping** can never disagree: `groupPhotosByDecade` (one section per decade in order of first
  appearance, the undated in their own; never re-sorts, and a decade appearing twice is merged so the
  navigation lists each exactly once), `formatDecade` and `decadeAnchorId`) +
  `LIBRARY_PATH` (= `/`, the library's canonical route — **the library is the home page**; every link
  in the app points here, `/library/*` is only a redirect for old links) +
  `UPLOADER_NONE` (`'none'`, the reserved „no uploader" value — the same word the API param and the query
  language use, so the URL, the request and the search box spell the group one way) + `uploaderHref(uid)`
  (the library filtered to one account's uploads, the target of the detail's **Nahrání** row) +
  the **multi-selection of the `album`/`label`/`person` facets**: each key carries a **comma-joined list of UIDs** (urlState
  stores each key as a single string, a comma doesn't occur in a UID) — the helpers `parseFilterList`/
  `joinFilterList`/`addToFilterList`/`removeFilterList` (sic `removeFromFilterList`) encode the list;
  a photo must be in **all** the selected albums, carry **all** the labels and contain **all** the selected
  people (AND). The whole selection round-trips through the URL query, so Back restores it;
  `viewToParams` (sanitizes sort/archived/**year** — `toYear` lets only a four-digit year through, a hand-written/stale
  URL degrades to „no filter" instead of a backend 400 —, passes `min_rating`/`flag`,
  the `favorite` toggle, the single-valued `uploader` and the comma-joined UIDs of the `album`/`label`/`person` facets through unchanged — `buildPhotoQuery`
  expands them into repeated parameters `?album=a&album=b`, which the backend ANDs; an unknown UID simply matches
  nothing; the `sort` union additionally has `rating`) + `hasActiveFilters` (`{ignoreQuery}` on the search page,
  a non-empty album/label/person list, a picked `uploader` or `favorite` = an active filter, it covers rating/flag and the facets) —
  the mapping of the URL state onto the API params; `ratingHotkeys.ts` = the pure `ratingHotkey(key)` (`0`–`5` →
  a rating, `p`/`r`/`v` → a personal flag 👍/👎/👁 (stored pick/reject/eye), otherwise null) + `isTypingElement(target)` (input/textarea/select/
  contenteditable → the hotkey is skipped) — shared by the photo detail and a focused tile;
  `shortcuts.ts` = the keyboard-shortcut registry + pure helpers: `shortcutToken(key)` (normalization of
  `KeyboardEvent.key` — single-char lower-case, named keys passthrough, `?` stays), `isFormModalOpen`
  (is a `.modal.show` with a form control open? → suppress the shortcuts behind a dialog), `HELP_SHORTCUT_KEY`
  (`?`) and `SHORTCUT_GROUPS` (the grouped Grid/Detail source of truth for the help, `titleKey`/`descriptionKey`
  typed as i18next `ParseKeys`, so a non-existent key is a compile error);
  `albumBrowse.ts` = the album index's view state + the pure browse rules: the `AlbumsView` type
  (`type`/`q`/`sort`/`empty`, string-only for the URL) + `ALBUMS_DEFAULTS` (hand-made albums, the server's
  order, empty ones hidden) + `ALBUMS_SHOW_EMPTY` (`'1'`) + the `toAlbumTab`/`toAlbumSort` sanitizers +
  `albumBrowseOptions(view, language)` + `browseAlbums(albums, options)` → `{visible, counts, filteredOut}`.
  `tabForType` puts **every** album type into one of the four sections (`album` · `folder` **and `month`** ·
  `moment` · `state`; a type this frontend doesn't know yet lands in the default one, so it can never fall
  out of all of them). The search matches the **stored title and the Czech display name** (`leden` finds
  `January 2026`) via `lib/text` `foldedIncludes`; `name`/`count` sort by the **displayed** name
  (`localeCompare`, numeric, base sensitivity — so `květen` really precedes `leden`) while `date`
  **keeps the order the API returned**; the counts are taken **after** the search + empty filter but
  **before** the section split, so a badge answers „where are my matches?" rather than restating the totals;
  `albumCovers.ts` = what each album tile draws, so a grid of cards actually differs card to card: the
  `AlbumCover` union (`none` · `single{photoUid}` · `collage{photoUids}`) + `ALBUM_COLLAGE_TILES` (4) +
  `coverPhotoUids(cover)` + the pure `albumCover(album, taken?)` and `planAlbumCovers(albums)`.
  The cover used to be the album's newest photo, and overlapping albums share exactly that — four albums
  holding one scanned title page all showed it. So an album with at least four candidates
  (`cover_uids`, the backend's `organize.CoverCandidates` newest photos) becomes a **2 × 2 collage**,
  a smaller one degrades to a **single image** (a collage padded with repeats is worse than an honest
  single photo), and either way the photos an earlier tile took are skipped — with a top-up from the
  album's own order when too few are left, because three pictures and a hole shout louder than a repeat.
  A hand-picked `cover_photo_uid` overrules all of it and is shown alone, never as one cell:
  a guess must not overrule a decision. `planAlbumCovers` walks the list in render order, taking each
  cover's photos out of circulation for the tiles after it; the grid is **virtualized**, so the page
  plans the whole `visible` list once in a `useMemo` — a plan recomputed per visible window would deal
  a tile a new cover every time it scrolled back;
  `peopleBrowse.ts` = the same job for the people index: the `PeopleView` type (`q`/`type`/`sort`) +
  `PEOPLE_DEFAULTS` (everybody, alphabetical) + the `toPeopleTab`/`toPeopleSort` sanitizers +
  `peopleBrowseOptions(view, language)` + `browsePeople(subjects, options)` → `{visible, counts,
  filteredOut}`. The search is `foldedIncludes` over the name; `name` sorts by `localeCompare`
  (numeric, base sensitivity — the API orders in the *database's* collation, this one in the reader's),
  `count` by `photo_count` (the figure the tile's caption shows) with the name as the tie-break; a
  subject type this frontend doesn't know yet counts as `other` rather than falling out of every option,
  and the counts are taken **after** the search but **before** the type split,
  `labelBrowse.ts` = the same job for the labels index, plus the **numbered-family folding**: the
  `LabelsView` type (`q`/`sort`/`open`) + `LABELS_DEFAULTS` (everything, **most photos first** — the
  alphabet is precisely the order that puts `DumNN` first) + `toLabelSort` +
  `labelBrowseOptions(view, language)` + `browseLabels(labels, options)` → `{entries, matched,
  filteredOut}`. An entry is either one label or a `LabelFamilyEntry` standing in for several:
  `familyPrefix('Dum 12')` → `Dum` (a trailing number after a prefix that ends in a non-digit, so a
  bare `2024` belongs nowhere), `familyKey` folds that prefix for the URL (`dum`), and a family folds
  only from **`LABEL_FAMILY_MIN` (4) members** up — below it the chip would hide less than it costs.
  A family sorts by its members' **total** count (so folding cannot bury a big prefix) or by its
  prefix, and its members sort by the same rule (numeric collation → `Dum4` before `Dum11`). **A
  search dissolves the families entirely**: typing `dum4` asks for `Dum4` itself, not for a chip to
  open again. `openFamilies`/`toggleFamilyOpen`/`withFamilyOpen` encode the expanded set as the
  comma-separated `open` URL value,
  `searchView.ts` = the `SearchView` type (= `LibraryView` + `mode`)
  + `SEARCH_DEFAULTS` (mode `hybrid`) + the `toMode` sanitizer;
  `auditView.ts` = the `AuditView` type (filters + `offset`, string-only for the URL) + `AUDIT_DEFAULTS`
  + `AUDIT_PAGE_SIZE` (100) + `pickFilters` (the view without the offset) + `viewToParams` (maps onto
  `AuditListParams`, `since`/`until` from `YYYY-MM-DD` are expanded to the RFC 3339 day boundaries in UTC) — the basis for
  `AuditPage`; plus the **one** `target_type -> path` table both its Target column and its details block are
  driven by: `auditTargetHref(record)` (null for a type with no page, or a `markers` entry whose details name no
  photo) and `auditDetailLinks(details)` → `{groups, hidden}` (the `<entity>_uid`/`_uids` keys of a known entity,
  grouped by key, a destination never repeated, capped at `AUDIT_DETAIL_LINK_LIMIT` = 25);
  `activityView.ts` = the view model for `MyActivityPage`: `ACTIVITY_PATH` (`/account/activity` — the one
  place the route is written down, shared by `App.tsx`, the link on `AccountPage` and the tests), the
  `ActivityView` type (**only** `offset` — a one-user listing read newest-first has nothing else to choose)
  + `ACTIVITY_DEFAULTS` + `ACTIVITY_PAGE_SIZE` (50, smaller than the admin log's 100: this page is read to find
  one recent action) + `ACTIVITY_LINK_LIMIT` (5) + `viewToParams`; plus the two word-catalogues that turn a raw
  audit row into a sentence — `activityActionKey(action)` (the `internal/audit` action labels →
  `activity.actions.<domain>.<verb>`) and `activityTargetKey(targetType)` (→ `activity.targets.*`; `markers`
  deliberately maps to the **photo's** label, because that is where its link leads). Both return `undefined` for
  something the catalogue does not know, and the page falls back to the raw label — a missing translation must
  never blank a row. The values are literal `ParseKeys`, not a computed `` `activity.actions.${…}` `` template,
  so a key absent from the catalogue is a compile error,
  `reviewDecisions.ts` = the view model for `ReviewDecisionsPage`: the `ReviewDecisionsView` type
  (`user`/`decision`/`offset`, string-only for the URL) + `REVIEW_DECISIONS_DEFAULTS`
  + `REVIEW_DECISIONS_PAGE_SIZE` (60) + `viewToAuditParams` (always `via:'review'` + `decision`)
  + `toReviewDecision(record, subjects, labels)` maps an audit record onto a `ReviewDecision`
  (`verdict` Ano/Ne from the action, `kind` face/label, `photoUid`/`faceIndex`, the target translated into a name —
  `subject_name` from the details, otherwise the roster map, fallback the UID) + `parseDecisionFilter`;
  `savedSearchView.ts` = the pure `isSearchParams(params)` (the presence of `mode` distinguishes a search from a library
  view) + `savedSearchHref(params)` (assembles `pathname?query` onto `LIBRARY_PATH` or `/search`, encodes the stored
  params minimally against the defaults via `writeUrlState`, ignores unknown/stale keys) —
  the restoration of a saved search to an exact URL;
  `mapView.ts` = the `MapView` type (mapset + the viewport `lat`/`lng`/`z` + filters) + `MAP_DEFAULTS` +
  `mapViewToParams` (sanitizes archived) + `viewportFromView`/`mapsetFromView`/`hasActiveMapFilters`
  — the mapping of the map's URL state onto the feed params; `mapPopup.ts` = the pure `buildPopupElement` (a preview +
  a link to the photo detail as a popup element, a plain click → SPA navigation, a modified click passes through);
  `mapGestures.ts` = **touch gesture handling for the map without a plugin** — `prefersTouchGestures()`
  (`(pointer: coarse)`, an environment without `matchMedia` answers `false`, so the mouse keeps its behavior)
  + `enableTwoFingerPan(map, container, onOneFingerDrag)`, which on a map with **`dragging` turned off**
  enables dragging only for as long as at least two fingers are down; it rests on a detail of the Leaflet
  stylesheet — the container's `touch-action` follows the enabled handlers, so without drag and with pinch it is
  `pan-x pan-y`, i.e. **one finger scrolls THE PAGE** (a tall map in the middle of scrolling
  content stops being a scroll trap) and two fingers both pan the map and zoom it via Leaflet's touch-zoom;
  `onOneFingerDrag` fires **once per gesture** and only once the finger has travelled past a threshold (8px — a tap on a
  marker is not a drag) and **never for a touch that started on `.leaflet-marker-draggable` /
  `.leaflet-control`** (dragging the picker's pin with one finger works, and advising „two fingers" there would be
  bad advice at the worst moment), from which `LeafletMap` shows the „dvěma prsty" hint;
  `placeDrill.ts` = the pure `resolvePlaceDrill(countries, country, city)` → the level `/places` actually
  shows: the URL's choice with any level of **exactly one row** stepped past, the selected country's entry,
  the `…Implied` flags and `canClearCountry`/`canClearCity` (whether stepping back up would show anything
  new, which is what lets the breadcrumb render a skipped level as text). A city level is only skipped when
  its one city holds the country's **whole** count — a country's photos whose town was never resolved have
  no row of their own, so a bare `cities.length === 1` test would hide them;
  `faceState.ts` = the pure `faceState(face)` (**`named`/`unnamed`, two states and two colors** — it reads the assignment, not
  `face.action`, so that an optimistic update keeps the box and the row in sync with the click just made)
  + `isNamed` + `hasEmbedding`; one source of truth for the colors in the overlay, `FacesPanel` and `PeoplePanel`.
  **Why two and not three:** it used to split the unnamed half by whether a marker already covered the face
  (`unassigned` yellow „Bez osoby" / `unmatched` red „Nepojmenovaný" — the labels were also the wrong way round).
  That split is the imported bookkeeping (boxes) meeting Kukátko's detector (vectors): `internal/facematch` handles
  both in one step (`create_marker` creates the marker *and* assigns, `assign_person` assigns to the existing one),
  so naming either one is the same single click and only the verb the backend picks differs — while the marker-less
  half is **82 % of the library** (94 194 of 115 457 faces, 2026-08-03) and was drawn in the color that means
  „something is wrong". **The backend state machine is unchanged** — this is only what the UI shows.
  What replaced it is `hasEmbedding(face)` = `face_index >= 0`: `facematch` appends markers that matched no stored
  face under *descending negative* indexes, so a negative index is exactly „a marker with no vector behind it"
  (144 in production) — the payload already carries the fact, no backend change was needed. That one is **not a
  color**, only a mark on the panel row + a note in `FaceAssignPanel`, because it decides whether automation can
  ever reach the face at all;
  `faceGeometry.ts` = the pure `faceBoxStyle` (a normalized bbox → absolute `left/top/width/height`
  in %, for the overlay) + the rotation pair `rotateBbox(bbox, rotation)` (a bbox measured on the upright photo →
  the same box in the frame of that photo turned N degrees **clockwise**; a quarter turn swaps `w`/`h`, anything but
  90/180/270 passes through unchanged) and `rotatedFrameStyle(rotation, ratio)` (the layer that box is a percentage
  **of**: the wrapper itself for 0°/180°, and for a quarter turn `100/ratio` × `100*ratio` percent centred by a
  translate — the box the rotated image paints over a wrapper that keeps the unrotated aspect ratio; an unusable
  ratio falls back to filling the wrapper, since `NaN` percentages would drop every box off the page)
  + `readingOrder` (a list of anything carrying a `bbox` → the order the eye crosses the
  photo: the top row left to right, then the row below. One greedy pass down the photo assigns faces to **row bands** —
  a face joins the open band when its vertical centre is within **half the taller box** of the band's **topmost**
  member, i.e. the boxes genuinely overlap. Anchoring on the topmost member, not on the face last added, is what
  stops a slow drift down a crowd from chaining everybody into one endless row; merging two rows is the cheap
  mistake — inside a band the order is still left to right — while splitting one real row scatters its numbering,
  which is the whole thing this fixes. It never mutates its input and the sort is stable, so faces sharing a
  position keep their arrival order. Applied once, in `useFaces`) + `padBbox`/`boxWithinCrop`/`cropImageStyle` + `displayFrame` (the stored
  dimensions + the EXIF orientation → the **displayed** frame; orientations 5–8 swap the sides, because the bbox is in
  display space — its input **must** be the pre-rotation pair, see the invariant at the viewer above)
  + `squareCrop` (a bbox → a crop **square in pixels**, not in normalized
  units — that is what prevents the deformation: a „square" in a normalized 4000×3000 frame is a rectangle in pixels
  and would squash the face in a square tile; it grows the shorter pixel side from the centre and
  pushes the crop back inside the frame) + `faceMarkerStyle` (a bbox + a crop → the `--kk-face-*` custom
  properties of `.kk-face-marker`: the box's **centre** and its size in % of the crop. The centre-anchored
  twin of `boxWithinCrop` — a marker growing from its top-left corner would slide off the face when it hits
  the CSS minimum; the `max()`/`clamp()` stay in CSS also because jsdom's CSSOM mangles `clamp()` in `left`
  but passes custom properties through verbatim) + `faceCropStyle` (**legacy**, it scales the axes independently → it deforms, and
  it reads `tile_*`, which is a centred square, not the whole frame; only for `FaceThumb`);
  `faceThreshold.ts` = a pure conversion of the person-search threshold between **percent** (the UI) and the **cosine
  distance** (the backend): `percentToDistance` (`1 - p/100`)/`distanceToPercent` (the inverse,
  rounded — also the „match %" on a card)/`clampThresholdPercent` + the range constants (20–80, step 5,
  default 50); `candidateReview.ts` = the pure model of the `/faces` review grid: `ReviewItem`/`CandidateStatus`
  (`pending`/`done`/`error`), the buckets `new`/`assign`/`done` (`bucketOf`, a shared color code via
  `BUCKET_VARIANT`), `FilterTab`/`FILTER_TABS`/`matchesTab`/`tabCounts`, `isActionable`,
  `buildAssignRequest` (mirrors `useFaces`: an existing `marker_uid` → `assign_person`, otherwise
  `create_marker` with a bbox — it never produces a duplicate marker) and `buildRejection`;
  `recognitionSweep.ts` = the pure helpers of the `/recognition` sweep: the confidence slider constants (50–95,
  step 1, default 75) + `clampConfidencePercent`, `PersonState`, `personActionableCount`/`hasActionable`
  (a person's card disappears when `hasActionable` is false), and a **flat keyboard focus sequence** across
  people (`FocusEntry`, `focusKey`, `focusSequence` = actionable cards only, `nextFocusKey`);
  `expandSearch.ts` = the pure logic of `/expand`: the threshold dial **65–90 %, default 80 %**
  (`EXPAND_THRESHOLD_MIN_PERCENT`/`_MAX_PERCENT`/`_DEFAULT_PERCENT` — its **own** range, read off the photo
  embedding space and matching the backend's `expand.max_distance` of 0.20; only the step and the
  percent↔distance conversion come from `faceThreshold`) + `clampExpandThresholdPercent`, `expandThresholdDistance`
  (percent → distance, `toFixed(4)` trims the float noise for the URL), limit 1–200 default 50
  (`clampExpandLimit`), `ExpandSource` + `expandSources` (the picker: without empty collections, ordered by
  photo count descending, tiebreak by name) and `similarityPercent` (a candidate's similarity → whole %);
  `outlierReview.ts` = the pure model of `/outliers`: the lifecycle `pending`→`removed`/`confirmed`/`error`
  (`OutlierItem`, `outlierKey` = `photo_uid:face_index`, `toOutlierItems`, `isActionable` — an errored
  card **counts**, its write failed, so it is still undecided —, `canUnassign` = it has a marker,
  otherwise there is nothing to detach) + the threshold arithmetic: **the UI speaks in percent, the endpoint in the cosine
  distance**, `outlierThresholdDistance` (0 % → 0 = „return everything", 100 % → `OUTLIER_MAX_DISTANCE`=1,
  because two **different** people sit around 1.0 and beyond that there is nothing left to hide; `toFixed(4)` trims the float noise for
  the URL), `clampOutlierThresholdPercent` (default **0 = show everything**; a non-zero default would silently
  hide faces), `distancePercent` (deliberately **not** similarity — on this page a bigger number
  means „further from the person", which is the quantity being judged) and `OUTLIER_LIMIT`=200;
  `moveFaces.ts` = the pure half of the split (`MoveFacesModal`): `moveRequests(faces, sourceUid, target)`
  → one `assign_person` request per marker the person holds on that photo. Only a face **carrying a
  marker** can move (a bare detection has nothing to reassign), and a marker no detection claimed — which
  the backend hands a **negative** `face_index` — moves without one, since that index names no face row and
  sending it would ask the backend to cache the link onto a slot that does not exist; plus `MoveSummary`
  (moved markers, photos, skipped, failed), `EMPTY_MOVE_SUMMARY` and `movedNothing`;
  `coordinates.ts` = a pure tolerant coordinate parser for the location picker: `parseCoordinates(input)`
  → `{ok:true,value:{lat,lng}}` | `{ok:false,error:'empty'|'format'|'range'}` (decimal degrees /
  DMS / degrees-decimal-minutes, a comma/space separator, ±/the hemispheres N/S/E/W, unicode primes/`''`,
  an axis reorder by the hemispheres, a range check of ±90/±180) + `formatCoordinates({lat,lng},precision=6)` →
  the canonical `"49.123400, 16.567800"` (it round-trips through the parser, but it is a **display, lossy**
  format — `16.7083583333333` → `16.708358`, which is why an unchanged coordinate isn't sent in the PATCH
  at all) — shared with the `MetadataPanel` picker;
  `mediaFiles.ts` = the one place the app decides „this is a photo or a video": `MEDIA_EXTENSIONS` (the
  browser-native images, HEIC/HEIF, the RAW of every usual vendor, the phone/camcorder video containers),
  `PICKER_ACCEPT` (`image/*,video/*` + all of them as `.ext`, the `accept` a picker gets) and the pure
  `isMediaFile({name,type})` (the MIME type when it says image/video, **otherwise the extension** — a file
  manager, a messenger or a cloud drive routinely hands a photo over as `application/octet-stream`, and
  RAW/HEIC usually as nothing at all). Deliberately generous: the backend has the last word and
  drag-and-drop bypasses the list entirely, so a missing entry can only hide a file the user meant to add.
  Shared by `DropZone` (the picker) and `pwa/shareTarget` (the triage of a share);
  `kenBurns.ts` = the pure `kenBurnsMotion(uid,intervalMs)` → the endpoints of a slow zoom+pan across the whole
  frame (`durationMs` = the interval, so the animation lasts exactly one slide) + `kenBurnsStyle(…)` →
  the `--kb-*` custom properties for `slideshow.css` + `panLimit(scale)`. The parameters (8 directions × zoom
  in/out × 5 depths) are derived **deterministically** from an FNV-1a hash of the `uid`, so the same album
  looks the same on every replay. Both endpoints keep the offset within the `panLimit` of their scale and both the scale
  and the offset interpolate linearly → **the image never uncovers the edge** of the scene;
  `gridDensity.ts` = the `GridDensity` type (**a plain `number`**, the column count) + `GRID_COLUMNS_MIN`
  (**1** = one photo per row) / `GRID_COLUMNS_MAX` (**10**) / `GRID_COLUMN_CHOICES` (1…10) /
  `GRID_TILE_MIN_PX` (140, the target tile width **for the seed only**) / `GRID_GAP_PX` (**3** — a hairline
  gap for a dense hero-first wall) / `GRID_DENSITY_DEFAULT` (**5** — a concrete fallback when the viewport width
  can't be measured) + the pure `readStoredDensity`/`writeDensity`/`sanitizeDensity`/`stepDensity`
  (localStorage `kukatko.grid.density`, a bare scalar in JSON; the number is rounded and **clamped into
  1…10**; `sanitizeDensity` also folds older `'auto'`/non-numeric values onto a concrete count seeded
  from the width; `readStoredDensity` returns `null` when **no usable number is stored** — empty/
  unavailable storage, broken JSON or an older `'auto'` —, so that the caller seeds from the width and migrates the
  value) + `initialColumnsForWidth(width)` (how many ~140px tiles fit across the width, clamped
  1…10; narrow → 1, a phone → 1–2, very wide → 10) + `initialColumns()` (the seed for the current viewport)
  + `GRID_COLUMN_CAPS` (`{belowWidthPx,maxColumns}[]` = **at most 3 columns below 576px, 4 below 768px** —
  Bootstrap `sm`/`md`) + `maxColumnsForWidth(width)` / `maxColumnsForViewport()` (that ceiling for a width /
  for the live viewport; an unmeasurable width → **no ceiling**, the user's own choice is the better guess)
  + `clampColumnsToWidth(density,width)` (the sanitized preference lowered to the ceiling). The clamp is
  **display-only** — the stored number is never rewritten, so a wide window restores the chosen density
  verbatim. It exists because *one* number is shared by the laptop that pinned it and the phone that has to
  live with it: eight columns across a 393px screen leaves tiles under 50px, i.e. a mesh of favourite hearts
  over photographs too small to recognize.
  Finally + the pure `gridTemplateColumns(density)` → **always `repeat(N, 1fr)`** = exactly N equal columns on
  every viewport (no `auto-fill` fallback, because the user always picks a concrete number); the gap
  between tiles is handled separately by `gap` on the container. Everything above takes an optional
  **`GridDensityScope`** `{storageKey, tileMinPx, gapPx}` (the whole per-grid contract) and defaults to
  `LIBRARY_GRID_SCOPE` (`kukatko.grid.density`, 140px, 3px). The second scope is `OUTLIER_GRID_SCOPE`
  (`kukatko.outliers.density`, `OUTLIER_TILE_MIN_PX` 256 = the 16rem card, `OUTLIER_GAP_PX` 16 = `gap-3`):
  **a shared control, a separate number**, because a density for browsing photographs is not one for judging
  faces — and a review card seeded from the library's 140px would open at twice the density anyone wants;
  `slideshowSettings.ts` = the `SlideshowSettings{effect,intervalMs,repeat,shuffle,showTitle,
  showDescription,showDate}` type + `SlideshowEffect`
  (`fade`/`slide`/`kenburns`/`none`) + the offers `SLIDESHOW_EFFECTS`/`SLIDESHOW_INTERVALS_MS` (1/2/3/5/10/15/30 s)
  + `SLIDESHOW_DEFAULTS` (`fade`, 5 s, no repeat, no shuffle, **all three captions on** — a family show
  whose photos arrive nameless and undated is what they exist to fix, while repeat and shuffle change
  what the show *is* and are the reader's to ask for)
  + the pure `readSettings`/`writeSettings`/`sanitizeSettings` (localStorage `kukatko.slideshow.settings`,
  sanitization of the effect + the interval **snapped to the nearest offered value** — an interval stored earlier
  that is no longer on offer (7 s) thus neither falls through the cracks nor renders an empty item; on an equal
  distance the shorter one wins; every flag is validated as a **boolean**, so settings written before
  repeat/shuffle/captions existed migrate to the defaults and a tampered `"false"` cannot mean „on";
  a fallback to the defaults on an error/unavailable storage)
  + `transitionDurationMs(effect,intervalMs)` = **how long the entrance into a slide may run at the chosen
  speed**: the comfortable 600 ms (fade / the Ken Burns cross-fade) or 450 ms (slide), capped at a **quarter
  of the interval** — at the 1 s speed the full 600 ms is most of the slide, the show never settles and
  every frame is caught mid-fade, which is what „the transition stutters" actually is; 250 ms at 1 s,
  500 ms at 2 s, unchanged from 3 s up. `none` is always 0. The player publishes it as
  `--slideshow-transition` on the stage's root, where `slideshow.css` reads it;
  `slideshowPlaylist.ts` = the pure order bookkeeping of a running show: `newShuffleSeed()` (a short
  random token, the one impure line, held for the life of the show) + `playlistOf(carried, loaded)`
  (the photos seen so far, in the order seen, followed by every loaded photo still to come — with
  nothing carried it returns `loaded` itself, so the ordinary show pays nothing) + `extendSeen(seen,
  playlist, index)` (grows the seen list to cover the cursor, never shrinks — stepping back revisits a
  photo, it does not un-see it). Together they are what makes **shuffle toggled mid-show** reorder only
  the future: the server reloads the list from page one in the new order, and the seen photos are held
  in front of it and filtered out of it, so the photo on screen stays and nothing repeats;
  `slideshowView.ts` = the pure `slideshowHref(scope,view)` (builds `/slideshow?…` from a `LibraryView` via
  `writeUrlState` + the scope `album`/`label`/`mode`, omitting the default filters — the slideshow's launch link;
  `mode` is written even when it equals the default, because `SlideshowPage` reads its **presence** as
  „this came from a search");
  `duration.ts` = the pure `splitDuration(ms)` → `{hours,minutes,seconds}` (rounds to seconds,
  negative/infinite → zero) + `formatDuration(ms,t)` → a compact one-line rendering via i18next
  (`45 s` / `3 min 20 s` / `1 h 5 min`; a zero part is omitted, with hours the seconds are dropped)
  + `slideshowDurationMs(count,intervalMs)` (the whole show = the interval per photo)
  + `slideshowRemainingMs(index,total,intervalMs)` (the photos still to come — the current frame
  doesn't count, the last slide reports zero, an index past the end too);
  `trashCountdown.ts` = the pure `purgeCountdown(archivedAt,retentionDays,now?)` (the days remaining until the
  auto-purge from `archived_at` + the retention → `{daysLeft,due}` or `null` when the countdown doesn't apply
  (not archived / retention ≤ 0 / unparseable), the countdown on the trash cards);
  `format.ts` = the pure `formatBytes(bytes)` (a byte count → human-readable binary units, e.g.
  `1536`→`"1.5 KB"`, invalid→`"0 B"`) for the file size on the duplicate-group cards +
  `formatCount(value,locale)` (a whole number → **thousands separated in the active language**, `20310` → cs
  `"20 310"` / en `"20,310"`; a fraction is rounded, non-finite → `"0"`) for the counts on `LibraryStatsCards` +
  `formatPercent(ratio,locale)` (a `[0,1]` share → a locale-aware percentage with **at most one decimal**,
  `0.0025` → cs `"0,3 %"` / en `"0.3%"` — the decimal is what keeps a sliver from rounding to `0 %` and
  reading as nothing; out-of-range values are clamped and non-finite → `"0 %"`, so a malformed number never
  shows as full coverage) — its consumer, the import-verify source-coverage column, went with the
  migration in August 2026; the formatter stays as a tested primitive +
  `formatDuration(ms)` (ms → `M:SS`/`H:MM:SS`, invalid→`"0:00"`) for the video length on the tiles +
  `formatMonth(year,month,locale)` (a 1-based year/month → a locale-aware short month + year, e.g.
  `2026,1,'en'`→`"Jan 2026"`, outside 1–12 → `""`) for the timeline tick labels +
  `formatMonthName(year,month,locale)` (its name half alone, `"Jan"` / cs `"led"`) for a chart axis whose
  label already says the year — the arrivals chart on `StatsPage` +
  `formatCaptureRange(from?,to?)` (an album's `taken_at` range → the tightest form: a single month
  `"6/2007"`, a single year `"2006"`, otherwise `"1998–1999"` with an en dash; a missing/invalid bound →
  `""`, i.e. an album without dated photos draws no line) for `AlbumTile` +
  the **locale-aware** `formatDate(value,locale)`/`formatDateTime(value,locale)` (ISO/epoch/`Date` →
  `toLocaleDateString`/`toLocaleString` with the **active UI language** `i18n.language`, not the browser's default
  language; unparseable input → the original string; used by PhotoTile/DuplicateGroupCard/
  MetadataPanel/Import/System for dates in the cs/en format))),
  `services/` (`health.ts`, `capabilities.ts` = `fetchCapabilities(signal)` over `GET /api/v1/capabilities`
  → `Capabilities{semantic_search, version?: VersionInfo{version,commit}}` (it sends the session cookie,
  `credentials:'same-origin'`; `version` is optional on the client because it is absent before the first
  answer and after a failed one, not because the endpoint may omit it),
  `settings.ts` = `fetchPublicSettings(signal)` over `GET /api/v1/settings/public` →
  `PublicSettings{registration_enabled}` — the **one** fact about the instance an anonymous visitor may
  learn, which is why the sign-in and registration screens may ask it before anybody has signed in (the
  full settings record carries the registration secret and stays behind `RequireAdmin`). It sends no
  credentials and **throws** rather than defaulting: "we could not ask" is not "registration is shut", and
  the two callers of `useRegistrationOpen()` answer that difference differently. The admin half lives
  here too: `fetchInstanceSettings(signal)` over `GET /api/v1/settings` → `InstanceSettings`
  (`registration_enabled`, `registration_secret`, `welcome_markdown`, `updated_at`, `updated_by_uid?`)
  and `updateInstanceSettings(update, signal)` over `PUT /api/v1/settings` → the persisted record.
  Both send the session cookie (`credentials: 'same-origin'`) and throw `ApiError` carrying the
  status **and the backend's own `error` message** — a 400 on enabling registration with a blank secret
  is meant to be shown verbatim. All three values travel together because the backend writes them
  together: the flag and the secret guard each other, so a partial update has no meaning.
  `fetchWelcomeMarkdown(signal)` over `GET /api/v1/settings/welcome` → the greeting string alone
  (`WelcomeSettings{welcome_markdown}` unwrapped): the one field of that record any signed-in role may
  read, a viewer having no business with the registration secret that travels with the admin shape. An
  instance where nobody wrote a greeting answers **200 with an empty string, not a 404** — "there is no
  greeting" is an answer — so a rejection here means the question could not be asked, which `WelcomeModal`
  deliberately does not treat as an empty greeting,
  `whatsNew.ts` = `fetchWhatsNew(signal)` over `GET /api/v1/whats-new` → `WhatsNew{has_news, since?,
  photos?, comments?, albums?:WhatsNewAlbum[], album_count?, people?:WhatsNewPerson[], person_count?}`
  (`has_news` is the only flag to branch on — false covers both a first-ever visit and an empty one;
  the request **writes** server-side, stamping the reader's visit, so it is issued once per library-home
  load and **never polled**), `auth.ts` = login/logout/me/changePassword, the types
  `User`/`Role` (the strict ladder `viewer < editor < admin < maintainer`)/`AuthSession`, `ApiError` with a
  status, **`NetworkError`** (the request never reached the backend — no network, DNS/TLS, a dropped
  connection, nothing listening. `fetch` rejects with a bare `TypeError` for all of those, which at the
  call site is indistinguishable from a bug in the call, so **every request in this module goes through
  the private `apiFetch`**, which re-throws it as `NetworkError` with the original as `cause`. An
  `AbortError` passes through untouched — the caller's own cancellation is not an outage. Callers need
  the difference because "we could not ask" is not "the answer was no": `LoginPage` and `AuthProvider`
  both branch on it, and without it an unreachable backend reached the reader as a rejected password and
  as a signed-out session),
  **`register(input, signal)`** over `POST /api/v1/auth/register` → `RegisteredAccount{username,
  display_name, email, pending_approval}` (the self-service way in: it sends the instance's shared
  registration secret alongside the account, carries **no credentials** — there is no session yet — and
  returns no session either, because registering deliberately does not sign anybody in. Its rejections are
  what `RegisterPage` reads: 403 closed/wrong secret told apart by the message, 409 taken, 400 validation,
  429 the per-address budget),
  **`fetchPasswordResetStatus(token, signal)`** over `GET /api/v1/auth/password-reset/{token}` →
  `PasswordResetStatus{valid, display_name?, expires_at?}` and **`resetPassword(token, password,
  signal)`** over `POST` to the same path → 204 (the pair behind `PasswordResetPage`: both are
  anonymous — the reader is locked out by definition — and send **no credentials**. The status call
  answers 200 for every link, so a rejection from it means the question could not be asked at all,
  which is **not** a dead link; `resetPassword` spends the link and ends **every** session of that
  account, and its 404 — unknown, used, expired or blocked, one unspecific answer for all four — is
  what the page turns back into the expired-link explanation, while 400 is a password the length rule
  refused and does not spend the link),
  `isNotFound(err)` (a 404 = "there is no such thing", which the detail pages tell apart from a failed
  load so a link out of the audit log to something deleted explains itself),
  `roleAtLeast`, `canWrite` (editor+), `isAdmin` (admin+), `isMaintainer` (maintainer) and
  `canImport` (= maintainer; import is an operational capability) — all via `ROLE_RANK` mirroring the backend's
  `internal/auth/role.go`; `MIN_PASSWORD_LENGTH`; **`setMySubject(subjectUid|null, signal)`**
  over `PUT /api/v1/auth/subject` → the refreshed `User` (which person of the library the signed-in user is;
  the account written to is always the session's, and `User.subject_uid` is what the "my photos" entry, the
  `person:me` filter and the comment avatar all read); `User` additionally carries **`approved_at`**
  (when an administrator let the account in, `null` while nobody has — **not** the inverse of
  `disabled`, which is an account that *was* let in and then blocked) and **`welcome_seen_at`** (when
  its owner last dismissed the first-run welcome, `null` when never; **`markWelcomeSeen(signal)`** over
  `POST /api/v1/auth/welcome-seen` → the refreshed `User` stamps it once and never moves it, so calling it
  twice is harmless and a caller — `WelcomeModal` fires it and forgets it — need not remember whether it
  already did); **and the personal API tokens**
  (`fetchApiTokens(signal)` / `createApiToken(name,signal)` / `revokeApiToken(id,signal)` over
  `GET`/`POST`/`DELETE /api/v1/auth/tokens[/{id}]`, the types `ApiToken{id,user_uid,name,created_at,
  expires_at?,last_used_at?,revoked_at?}` and `CreatedApiToken{token,secret}` — **`secret` arrives once and
  only once**, the server keeps nothing but its hash — plus `API_TOKEN_NAME_MAX_LENGTH` = 100 mirroring the
  backend's `apiTokenNameMaxLen`; the listing is scoped server-side to the caller and **includes revoked and
  expired tokens**, so hiding the dead ones is the caller's decision);
  `photos.ts` = `fetchPhotos(params,signal)` over `GET /api/v1/photos`
  (filters/sorting/pagination → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `searchPhotos(params,mode?,signal)` over `GET /api/v1/search` (the mode
  `fulltext`/`semantic`/`hybrid`, the response additionally has `mode`+`degraded`),
  `fetchSimilar(uid,limit?,signal)` over `GET /api/v1/photos/{uid}/similar` → `SimilarPhoto[]`
  (`Photo`+`distance`; empty-friendly), the types `SimilarPhoto`/`SimilarResponse`,
  `fetchTimeline(params,signal)` over `GET /api/v1/photos/timeline` → `Timeline{buckets,total}`
  (a monthly date histogram, the same filters as the list; the backend ignores sort/pagination), the types
  `Timeline`/`TimelineBucket{year,month,count,cumulative}` — the basis for `TimelineScrubber`,
  `fetchPhotoYears(params,signal)` over `GET /api/v1/photos/years` → `YearsResponse{years,total}`
  (a year histogram, the same filters as the list; sort/pagination are ignored, and the caller drops the
  period first), the types `YearsResponse`/`YearBucket{year,count}` — the basis for the decades the
  period filter offers (`useLibraryFacets`),
  `fetchPhotoUploaders(params,signal)` over `GET /api/v1/photos/uploaders` → `UploadersResponse{uploaders}`
  (who uploaded into the current view, largest first, the same filters as the list; the caller drops the
  `uploader` scope first), the types `UploadersResponse`/`UploaderBucket{uid,name,count}` — where the
  imported photos are the entry with an **empty `uid` and an empty `name`**, which the client words itself
  („Importované") — the basis for the uploader control (`useUploaders`);
  `PhotoListParams` scopes the time axis **only** through `taken_after`/`taken_before` (the backend's
  single-year `year` param has no caller here — one filter, one representation), `buildPhotoQuery`
  serializes them,
  `favoritePhoto(uid,favorite,signal)` over `PUT`/`DELETE /api/v1/photos/{uid}/favorite` (a per-user
  toggle, 204, the basis for the optimistic `useFavorite`),
  `ratePhoto(uid,{rating?,flag?},signal)` over `PUT /api/v1/photos/{uid}/rating` +
  `clearRating(uid,signal)` over `DELETE …/rating` (per-user stars 0–5 + a personal flag
  none|pick|reject|eye, 204, the basis for `useRating`), the types `RatingUpdate`/`RatingFlag`,
  `regenerateThumbnail(uid,signal)` over `POST /api/v1/photos/{uid}/regenerate-thumbnail`
  (an editor/admin service action, synchronous, `RegenerateThumbnailResult{status,sizes}`, 422 =
  the original is undecodable; the basis for `RegenerateThumbnailButton`),
  `runProcessingStep(uid,step,signal)` over `POST /api/v1/photos/{uid}/process/{step}` (a **maintainer**
  action scheduling one per-photo computation, idempotent, resolves with that step's new
  `PhotoProcessing`, 409 = the step does not apply to this photo; the basis for `ProcessingPanel`), the
  types `ProcessingStep`/`ProcessingState`/`PhotoProcessing` and `PhotoDetail.processing`,
  `hidePhoto(uid)`/`unhidePhoto(uid)` (`POST …/hide`/`…/unhide`, editor/admin, set/clear
  `Photo.hidden_from_library` — library visibility, **not** the trash and not `private`),
  **the trash** `unarchivePhoto(uid)` (`POST …/unarchive` restore), `purgePhoto(uid)` (`POST …/purge?confirm=true`
  permanent deletion), `emptyTrash()` (`POST /trash/empty?confirm=true` → `PurgeResult{purged,failed}`),
  `fetchTrashInfo()` (`GET /trash/info` → `TrashInfo{retention_days}`),
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `videoUrl(uid,token?)` (a range stream for
  `<video>`; with the R2 backend the route **302** redirects to the Worker, `<video>` follows the redirect
  on every request, so a seek always runs against a fresh signature), `GRID_THUMB_SIZE`,
  the types `Photo` (incl. `is_favorite` + the per-user `rating`/`flag` + the video fields
  `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps` + **`thumb_url`/`download_url`** +
  **`stack_uid`/`stack_count`**)/`PhotoListParams`
  (incl. the `album`/`label` scope + the **`person` scope** (comma-joined subject UIDs → repeated `?person=`, AND)
  + the **`country`/`city` place scope** + the `favorite` filter + the `min_rating`/`flag` filters)/`PhotoSort`
  (incl. `rating`)/`RatingFlag`/`ArchivedFilter`/`SearchMode`, `ApiError`.
  **Media addresses are not assembled from a UID.** Both the grid tile and the download link take `photo.thumb_url` /
  `photo.download_url` from the payload — only the server can sign a URL. `thumbUrl(uid,size)` remains for a
  size the payload doesn't carry (the lightbox, the editor's canvas, a cover by UID) and `downloadUrl(uid,…)`
  for **rendering a non-destructive edit**, which only the application can do;
  `comments.ts` (**NEW**) = the per-photo comment client: `fetchComments(photoUid,signal)` over
  `GET /api/v1/photos/{uid}/comments` → `PhotoComment[]` **oldest first** (an empty thread — and a photo
  that does not exist — is an empty array, not a 404), `createComment(photoUid,body,signal)` over `POST`
  (201; **guarded by `RequireAuth`, not `RequireWrite`**, so a viewer may post — the one documented
  exception to the read-only rule — and rate-limited per user, 429), `updateComment(photoUid,uid,body,signal)`
  over `PATCH` (**author only**; an admin gets 403 — moderation removes, it does not rewrite) and
  `deleteComment(photoUid,uid,signal)` over `DELETE` (204, author **or** admin; soft server-side, so a
  second delete is 404). The type `PhotoComment{uid,photo_uid,author_uid,author_name,body,created_at,
  edited_at?}` — `author_uid`/`author_name` are **empty** for a comment whose author's account is gone —
  and `MAX_COMMENT_LENGTH` (2000 characters, mirroring `comments.MaxBodyLen`, which the composer's
  `maxLength` enforces so an over-long body is prevented rather than rejected). **Bodies are plain text**:
  nothing is parsed or sanitised server-side, so the client escapes what it displays (React does — no
  `dangerouslySetInnerHTML` anywhere near a body). `PhotoDetail.comment_count` on the detail payload feeds
  the badge without fetching the thread;
  `organize.ts` = the Albums/Labels client: albums `fetchAlbums`/`fetchAlbum`/`createAlbum`/`updateAlbum`/
  `deleteAlbum`/`addAlbumPhotos`/`removeAlbumPhotos`, labels `fetchLabels`/
  `fetchLabel`/`createLabel`/`updateLabel`/`deleteLabel`/`attachLabel`/`detachLabel`; the types
  `Album`/`AlbumCount`/`AlbumInput`/`AlbumType`/`Label`/`LabelCount`/`LabelInput` (`Label.review_enabled` =
  whether the review game may ask about the label; `LabelInput.review_enabled` is **optional**, because
  omitting it means "leave it as it is" — the rename modal and the labels page's switch each send only what
  they know, so neither can clobber the other's field; `LabelCount.cover_uid` is the photo standing for the
  label — its newest visible one, derived by the backend, absent for a label on no visible photo, and carried
  only by the *listing*: a label out of a global search brings its own cover pair instead);
  `savedSearches.ts` = the saved-searches client: `fetchSavedSearches`/`createSavedSearch(name,params)`/
  `updateSavedSearch(uid,{name?,params?})`/`deleteSavedSearch(uid)` over `/api/v1/saved-searches`, the types
  `SavedSearch`/`SavedSearchParams` (= the verbatim URL view state `Record<string,string>`)/
  `SavedSearchUpdate`;
  `searchHistory.ts` = the search-history client: `fetchSearchHistory()`/`recordSearch(query)`/
  `clearSearchHistory()` over `/api/v1/search-history` + `SEARCH_HISTORY_LIMIT` (20) and the type
  `SearchHistoryEntry{query,searched_at}`; it sends no owner and addresses nothing — every call is about the
  signed-in user's own history; `announcement.ts` = the instance-wide announcement client: `fetchAnnouncement()`/
  `setAnnouncement(message,level)`/`clearAnnouncement()` over `/api/v1/announcement`, the types `Announcement`
  (`{message, level?, author_uid?, updated_at?}`, an empty `message` = nothing published)/`AnnouncementLevel`
  (`'info'|'warning'`); `search.ts` = the grouped **global search** client: `globalSearch(q,signal)` over
  `GET /api/v1/search/global` → `GlobalSearchResult{query,direct?,albums,labels,people,photos}` (top-N per
  group, each always an array) + the pure helpers `hasEntityMatches`/`isEmptyResult`/`directHitRoute`, the types
  `GlobalSearchAlbum`/`GlobalSearchLabel`/`GlobalSearchPerson`/`GlobalSearchResult` +
  `GlobalSearchDirect{uid,kind,found,target_kind?,target_uid?,title?,photo?,cover?,thumb_url?,states?}` with
  `GlobalSearchUidKind`/`GlobalSearchTargetKind`/`GlobalSearchPhotoState`. **`direct` is the answer to a pasted
  id**: present only for a query that names an entity by its uid, and then every group is empty (the backend
  resolves the id instead of fuzzy-searching); `directHitRoute` maps a resolved hit to its page
  (`/photos|/albums|/labels|/people`) and returns `null` when the id named nothing — which is why
  `isEmptyResult` is **false** for an unresolved hit: an unknown id is something to say, not silence. Every
  entity hit (and a direct one) extends `GlobalSearchCover{cover?,thumb_url?}` — the photo standing for it and
  where to fetch that photo's medallion, set **together or not at all**. Prefer `thumb_url`: it is already the
  small square size a row wants, and it is the *signed* address when the library sits behind a media Worker.
  Separate
  from the photo `searchPhotos` (fulltext/semantic/hybrid), the basis for `GlobalSearchSections`; `bulk.ts` =
  `bulkUpdatePhotos(uids,ops)` over `POST /photos/bulk` (a bulk edit of the selection), the types
  `BulkOperations` (add/remove an album+label, set/clear the caption+description+location,
  archive/unarchive, set_favorite per-user)/`BulkLocation`/`BulkResult`; `duplicates.ts` =
  `fetchDuplicates(params,signal)` over `GET /api/v1/duplicates` (duplicate groups →
  `DuplicatesResponse{groups,total,limit,offset,next_offset}`) + `mergeDuplicates(input,signal)` over
  `POST /api/v1/duplicates/merge` (resolving a group → `MergeResult{albums_added,labels_added,people_added,
  metadata_filled[],archived,dry_run}`; `dry_run:true` = a preview), the types `DuplicateReason`/
  `DuplicateMember`/`DuplicateGroup`/`DuplicatesParams`/`MergeInput`/`MergeResult`;
  `dupmarkers.ts` = the repeated-marker client (one **person** marked more than once on one photo, not two
  photos): `fetchDuplicateMarkers(params,signal)` over `GET /api/v1/duplicate-markers` →
  `DuplicateMarkersResponse{groups,total,limit,offset,next_offset}`, `keepMarker(input,signal)` over
  `POST /api/v1/duplicate-markers/keep` → `KeepMarkerResult{…,detached[]}` (the losing markers are **not**
  sent — the server resolves the group from `(photo_uid, subject_uid)`, so a stale list cannot detach a marker
  that was meanwhile re-tagged) and `invalidateMarker(markerUid,signal)` over
  `POST /api/v1/duplicate-markers/invalid` (204); the types `DuplicateMarker`/`DuplicateMarkerGroup`/
  `DuplicateMarkersResponse`/`DuplicateMarkersParams`/`KeepMarkerResult`. The third decision („nechat být") is
  feedback, so it lives in `feedback.ts`; `upload.ts` =
  `uploadFile(file,{onProgress,signal})`
  over **`XMLHttpRequest`** (one file per request because of the upload-progress events, the FormData is
  streamed), `isAbortError`, the types `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`; `onload` is entirely inside `try`/`catch` and a 2xx body without a non-empty `results` array
  is rejected with an `ApiError` — an exception must not escape the XHR callback, the promise would never resolve and the
  upload would hang forever (holding a slot in the concurrency limit); `photos.ts` additionally has `fetchPhoto(uid)` (the detail `GET /photos/{uid}` →
  `PhotoDetail` = `Photo`+`files`+`albums`+`labels` inline chips `+ uploader?` `{uid,name}`),
  `updatePhoto(uid,patch)`
  (`PATCH …` a partial metadata edit → `PhotoMetadataUpdate`, null clears a nullable field),
  `fetchEdit(uid)`/`saveEdit(uid,edit)` (`GET`/`PUT …/edit` a non-destructive edit → `PhotoEdit`
  crop/rotation/brightness/contrast), `downloadUrl(uid,{original?,token?})` (the download URL,
  by default honoring the edit, `original:true` for the original),
  `downloadPhotosZip({photoUids?,albumUid?,name?})` (**a bulk ZIP download**: `POST
  …/download-zip`, it reads the response as a `Blob` and downloads it via a temporary object URL — the archive's
  name is assembled by the client (`name`.zip or `kukatko-photos-<date>.zip`, the client computes the `date` and
  sends it to the server as well), throws `ApiError` (413 = over the ceiling); the type `ZipDownloadRequest`),
  `downloadUrl`/`thumbUrl` additionally take **`{proxy:true}`**, which asks the media route to *stream* instead of
  redirecting to the object store — only for a caller that must **read** the bytes in the page (see `share.ts`); an
  `<a download>` or an `<img>` must never ask for it, it would move every byte through the application for nothing;
  `share.ts` = the client of sharing photos **out** of the library into the phone's own (not to be confused with
  the PWA share target, which shares files *in*): `fetchShareManifest(photoUids,signal)`
  (`POST /photos/share-manifest` → `ShareManifestFile[]` = `{uid,name,mime,size,preview}`, throws `ApiError`,
  413 = over the cap) and `fetchShareFile(file,signal)` → a real **`File`** named and typed as the manifest says,
  which is what makes the photo land in Apple/Google Photos under its own name instead of as an anonymous blob.
  Both fetches ask for `proxy=true`: a **RAW** (`preview`) walks `SHARE_PREVIEW_SIZES` (`fit_3840` → `fit_2560` →
  `fit_1920` → `fit_1280`) and takes the largest that answers, everything else takes
  `download?original=true`; tests `share.test.ts`),
  **stacks** `stackPhotos(photoUids,signal)` (`POST …/photos/stack` — a manual grouping of the selection → the `PhotoDetail`
  of the new primary), `setStackPrimary(uid,signal)` (`POST …/{uid}/stack/primary`),
  `unstackMember(uid,signal)` (`POST …/{uid}/unstack`) and `unstackAll(uid,signal)`
  (`POST …/{uid}/unstack-all`) — all of them return a refreshed `PhotoDetail`; the types `PhotoDetail` (additionally
  `stack_members?: StackMember[]` — the strip of variants, the primary first)/`StackMember`
  `{uid,file_name,media_type,file_mime,file_width,file_height,file_size,is_primary,thumb_url?,download_url?}`/`PhotoAlbumRef`/
  `PhotoLabelRef`/`PhotoUploaderRef`/`PhotoMetadataUpdate`/`PhotoEdit`; `people.ts` = the People/face client: subjects
  `fetchSubjects`/`fetchSubject`/`createSubject`/`updateSubject`/`deleteSubject`/
  `mergeSubject(sourceUid,keeperUid)` (`POST /subjects/{uid}/merge` — the path subject is the one merged
  away and deleted; irreversible, so the caller confirms first)/
  `fetchSubjectPhotos`, faces `fetchFaces`/`assignFace`, clusters `fetchClusters`/
  `assignCluster`/`removeClusterFace`, outliers `fetchOutliers`; the types `Subject`/`SubjectCount`/
  `SubjectInput`/`SubjectType`/`MergeResult`/`Bbox`/`FaceView`/`FacesResponse`/`AssignRequest`/`Suggestion`/
  `ClusterView`/`ExampleFace`/`ClusterAssignRequest`/`RemoveFaceRequest`/`OutlierResult`/
  `OutlierFace`; it shares `ApiError`+`buildPhotoQuery` from `auth.ts`/`photos.ts`);
  `faces.ts` = the client of the „find a person among untagged photos" search:
  `searchCandidates(subjectUid,{threshold,limit},signal)` over `POST /subjects/{uid}/candidates`; the types
  `CandidateSearchRequest`/`CandidateResult`/`Candidate`/`FaceBox`/`CandidateCounts`/`CandidateAction`
  (`create_marker`/`assign_person`/`already_done`)/`CandidateReason`; a confirmation goes through `assignFace`
  from `people.ts`, a rejection through `feedback.ts`; `feedback.ts` = persistent feedback (it doesn't mutate,
  it only keeps a rejected face/photo out of the next search): `rejectFace(req,signal)`/`unrejectFace(req,signal)`
  over `POST`/`DELETE /feedback/face-rejections`, the type `FaceRejection` `{photo_uid,face_index,subject_uid}`,
  and `rejectLabel(req,signal)`/`unrejectLabel(req,signal)` over `POST`/`DELETE /feedback/label-rejections`,
  the type `LabelRejection` `{photo_uid,label_uid}`; **`confirmFace(req,signal)`/`unconfirmFace(req,signal)`**
  over `POST`/`DELETE /feedback/face-confirmations`, the type `FaceConfirmation`
  `{photo_uid,face_index,subject_uid}` — **the opposite polarity to `rejectFace`**: it writes „this
  face **IS** this person" (✗ in the outlier review = „no, it really is them"), the backend then excludes the confirmed
  face from further outlier results; swapping it for `rejectFace` means storing the exact opposite
  of what the user said; **`dismissDuplicate(req,signal)`/`undismissDuplicate(req,signal)`** over
  `POST`/`DELETE /feedback/duplicate-dismissals`, the type `DuplicateDismissal` `{photo_uid,other_uid}` —
  „these two photos are NOT duplicates" from `DupComparePage` („Nechat obě"); the pair is **unordered**
  (the backend normalizes it), nothing is archived or merged, only an opinion is recorded and `GET /duplicates`
  then drops that edge on every subsequent scan;
  **`dismissDuplicateMarkers(req,signal)`/`undismissDuplicateMarkers(req,signal)`** over
  `POST`/`DELETE /feedback/duplicate-marker-dismissals`, the type `DuplicateMarkerDismissal`
  `{photo_uid,subject_uid}` — „this person really IS marked more than once here" from
  `DuplicateMarkersPage` („Nechat být"), for the genuine cases (a mirror, a double exposure, a photo of a
  photo); it keys the (photo, person) pair rather than the markers, whose uids change on re-detection, and it
  detaches or invalidates nothing (all of it idempotent → it can be called optimistically);
  `expand.ts` = the collection-expansion client: `searchSimilar(kind,uid,{threshold,limit},signal)` over
  `GET /albums/{uid}/similar` / `GET /labels/{uid}/similar` (`threshold` = the **cosine distance**,
  the conversion from percent is done by the caller via `lib/expandSearch`), the types `ExpandKind`/`ExpandCandidate`
  (the `photo` already has `thumb_url` stamped)/`ExpandResult` (summary counts + `min_match_count` +
  `reason?` `empty_collection`/`no_source_embeddings`)/`ExpandReason`/`ExpandSearchRequest`;
  adding goes through `bulk.ts` (`POST /photos/bulk`), rejecting through `feedback.ts`;
  `recognition.ts` = the recognition-sweep client: `streamSweep(params,onMessage,signal)` over
  `GET /faces/sweep` **streams NDJSON** (`fetch`+`ReadableStream`, it splits lines by hand, `onMessage` receives
  only complete lines), the types `SweepParams` `{confidence,limit}` (`confidence` = **percent**, the backend
  translates it into a distance) and `SweepMessage` = `progress`|`person`|`summary` (`SweepPerson` carries
  `candidates`/`counts`/`actionable` in the same shape as `faces.ts`); an abort via `signal` = `AbortError`
  (the caller ignores it); a confirmation goes through `assignFace`, a rejection through `rejectFace`;
  `review.ts` = the review-game client: `fetchReviewQueue(source?,limit?,signal)` over `GET /review/queue`
  (`source` = `ReviewSource` `'both'|'people'|'labels'` + `REVIEW_SOURCES` in the toggle's order, default
  `both`; the response echoes the applied `source`, and `REASON_NO_PEOPLE`/`REASON_NO_LABELS` join
  `REASON_NO_SOURCES`/`REASON_NO_CANDIDATES`),
  `answerReview(questionId,answer,signal)` over `POST /review/answer` (idempotent; the types
  `ReviewQuestion`/`ReviewQueue`/`ReviewAnswer` — `ReviewQuestion.tier` = `'sure'|'band'`, which confidence
  tier the question came from; the UI asks the same question either way, it is carried so the mix can be
  observed; the basis for `useReviewGame`).
  **One request is one round**: `ReviewQueue.round` (`ReviewRound` = `{index,size,remaining,kinds?,sure,band,
  entities,last}`) says which round of the session the `questions` array is, how long it was minted and what it
  is made of — the numbers are frozen at mint time, so they are what a between-rounds summary should show, not
  what is left. Re-fetching before answering returns the **same** round, so the dedup-by-id refill in
  `useReviewGame` cannot lose questions — and, for the same reason, the refill for the **next** round waits for
  the in-flight answers first. `ReviewQueue.breathers` (`ReviewBreather[]`, absent when empty) are
  **not questions**: a photo somebody rated or favourited, tagged `BREATHER_KIND` and carrying no id the answer
  endpoint would accept — render them differently and never offer Ano/Ne on one.
  `ReviewAnswerResult.reveal` (`ReviewReveal`) rides back on `result: 'assigned'` only, with the person's photo
  count and year span. Both become cards through `lib/reviewRounds` (`ReviewCard`, `buildRoundCards`,
  `insertReveal`), which is also where the milestone and daily-mix arithmetic lives, DOM-free and unit-tested.
  And **the leaderboard**
  `fetchLeaderboard(window,signal)` over `GET /review/leaderboard?window=all|7d|today` →
  `Leaderboard{window,caller_uid,entries:LeaderboardEntry[]}` (`LeaderboardEntry` =
  `{user_uid,display_name,yes_count,no_count,total,streak_days,is_me}`, ordered by the backend by `total`;
  `streak_days` = the player's current run of consecutive days with a decision, **not** narrowed by the
  window),
  the type `LeaderboardWindow` = `'all'|'7d'|'today'` + `LEADERBOARD_WINDOWS` (the toggle's order);
  the basis for `LeaderboardPage`;
  `map.ts` = the map client: `fetchMapPhotos(params,signal)` over `GET /api/v1/map/photos`
  (a GeoJSON FeatureCollection of geotagged photos + `buildMapQuery`), `tileLayerUrl(mapset)` (a Leaflet
  URL template onto the backend proxy, **without the API key**), `reverseGeocode(lat,lng,signal?)` over
  `GET /api/v1/map/rgeocode` (an on-demand reverse geocode for the photo detail → `GeocodeResult`),
  `searchPlaces(query,limit?,signal?)` over `GET /api/v1/map/geocode` (a **forward** geocode for the
  location editor → `Place[]` = `{name,label,type,location,lat,lng}` best match first; no match
  = an **empty array**, not an error; the caller **must debounce** — the backend does cache and
  rate-limit, but a request per keystroke is like burning a month's credit in an afternoon),
  **`probeTileFailure(tileUrl,signal?)`** (an `<img>`'s status isn't visible from JS → a tile that Leaflet
  failed to load is refetched and the proxy's status is translated into a `TileFailure`: **424 → `key_rejected`**
  (mapy.com is rejecting **our** key), 429 → `rate_limited`, 503 → `unavailable`, otherwise `error`;
  200/404 → `null`, because a missing tile outside the coverage is a normal response; a network error →
  `'error'`, an abort bubbles up), `toMapset`/`MAPSETS`, **`mapsetNeedsDimming(mapset)`** (whether the
  provider draws that mapset light and it therefore has to be toned down for the dark page — mapy.com
  publishes no dark tileset, so `basic`/`outdoor` are true and the aerial imagery is not; the day one ships,
  adding it to `MAPSETS` and leaving it out of the light set is the whole change); the types
  `MapFeature`/`MapFeatureCollection`/`MapFeatureProperties`/`MapPhotoParams`/`MapCoverage`/`Mapset`/
  `TileFailure`/`GeocodeResult`/`RegionalItem`/`Place`);
  `places.ts` = the place-hierarchy client: `fetchPlaces(country?,signal)` over `GET /api/v1/places`
  → `PlaceCountry[]` (countries with counts, a `cover_uid` preview + nested `cities` carrying the same, the
  optional `country` drills into the cities of one
  country); the types `PlaceCountry`/`PlaceCity`; browsing a place's photos goes through the shared
  `fetchPhotos({country,city})`;
  `import.ts` = the **read-only** admin import client: `fetchImportRuns(signal)` over
  `GET /api/v1/import/runs` (`{runs,limit,offset}`), `fetchImportFailures({unresolvedOnly,limit},signal)`
  over `GET /api/v1/import/failures` and `fetchJobStats(signal)` over `GET /api/v1/jobs/stats`;
  the types `RunSource`/`RunStatus`/`ImportCounts`/`ImportRun`/`ImportRunsResponse`/`FailureStage`/
  `FailureSource`/`ImportFailure`/`ImportFailuresResponse`/`JobStats`. There is no trigger: `startImport`
  and `fetchVerifyReport` went with the migration in August 2026),
  `maintenance.ts` = the admin maintenance client: `fetchMaintenanceScan(signal)` over
  `GET /api/v1/maintenance/scan` → `ScanReport`, `runMaintenanceRepair(options,signal)` over
  `POST /api/v1/maintenance/repair` → `RepairResult`, `purgeAuditLog(olderThanDays,signal)` over
  `POST /api/v1/maintenance/audit/purge` → `AuditPurgeResult` (`{deleted,older_than_days,cutoff}`),
  plus the nameless-subject repair: `fetchNamelessSubjects(signal)` over
  `GET /api/v1/maintenance/nameless-subjects` → `NamelessReport`, `detachNamelessSubjects(signal)` over
  `POST …/detach` — the response body *is* the undo file, so it `saveBlob`s it to the user's downloads under the
  server-chosen `Content-Disposition` name and returns `NamelessUndoFile` (`{filename,subjects,markers,faces}`
  read from the `X-Kukatko-Nameless-*` headers) — and `restoreNamelessSubjects(file,signal)` over `POST …/restore`
  (the `File` as the raw body) → `{queued}`;
  the types `Finding`/`ScanReport`/`RepairOptions`/`RepairResult`/`AuditPurgeResult`/`NamelessSubject`/
  `NamelessReport`/`NamelessUndoFile`/`NamelessRestoreResult`; it shares `ApiError`
  from `auth.ts` and `fetchJobStats` from `import.ts` for the progress,
  `system.ts` = the system client: `fetchSystemStatus(signal)` over `GET /api/v1/system/status`
  → `SystemStatus` (maintainer-only), `fetchLibraryStats(signal)` over `GET /api/v1/system/stats`
  → `LibraryStats` (**any logged-in user**; it throws an `ApiError` when the backend can't total the counts — the page then shows
  an error, not zeroes), `fetchLibraryCharts(signal)` over `GET /api/v1/system/stats/charts` → `LibraryCharts`
  (the same readers, deliberately a **second** request so the cheap counts are never held up by the heavier
  series; the arrays come gap-filled and never `null`), `triggerBackup(signal)` over `POST /api/v1/backup`
  (409/503 → ApiError),
  `requeueDeadLetterJobs(jobType?,signal)` (one `POST /jobs/requeue-dead`, optionally `?type=`, → the number
  requeued; it used to list `GET /jobs?state=dead` and `POST` a requeue per row, which a dead letter of
  thousands makes hopeless); the types `SystemStatus`/`LibraryStats`/`LibraryCharts` (+ its
  `YearPhotos`/`MonthPhotos`/`CameraPhotos`/`MediaStorage`/`YearStorage`)/`DatabaseStatus`/
  `EmbeddingsStatus`/`JobsStatus` (whose `by_type_state` is the queue breakdown the dashboard renders, while
  `by_type`/`total` are lifetime tallies)/`LibrarySummary`/`Uploads`/`RemainingWork`/`DuplicateScan`/
  `BackupStatus`/`ImportsStatus`/`StorageStatus`/`MapsStatus`/`MapsState`/`GeocodeStatus`/`VersionInfo`; it shares
  `ApiError` from `auth.ts` and `ImportRun` from `import.ts`,
  `users.ts` = the admin account-management client over `/api/v1/admin/users`: `fetchUsers(signal)` → `AdminUser[]`
  (= `User` + `note`), `createUser(body,signal)` (`POST`, 409 = the username is taken, 400 = a weak password /
  an invalid role / too long a note), `updateUser(uid,body,signal)` (`PATCH`, a **replace** of the whole
  mutable profile → send the fields the dialog doesn't offer as well), `setUserDisabled(user,disabled,signal)`
  (disable → the dedicated `POST /{uid}/disable`, which doesn't need the profile and won't overwrite a concurrent edit;
  enable → a `PATCH` with `disabled:false`, there is no dedicated endpoint) and `resetUserPassword(uid,pwd,signal)`
  (`POST /{uid}/password`, 204, it logs out all of the target's sessions), **`approveUser(uid,signal)`**
  (`POST /{uid}/approve` → the row with `approved_at` stamped; 403 = the maintainer boundary, 409 = a blocked
  account, and approving twice is a 200 with the row unchanged, so a double click cannot fail) and
  **`issuePasswordReset(uid,signal)`** (`POST /{uid}/password-reset` → `IssuedPasswordReset`
  = `{reset_url, expires_at, email}`; it sets **no** password — the person chooses their own behind the link —
  mails the link, kills the account's earlier unused ones, 403/404/409 as above); the constants `ROLES`
  (`viewer`/`editor`/`admin`/`maintainer`, ascending along the ladder)/`MAX_NOTE_LENGTH`/**`PASSWORD_RESET_TTL_DAYS`**
  (7, mirroring `auth.PasswordResetTTL` — what the UI *says* has to match what the backend does),
  the types `AdminUser`/`CreateUserBody`/`UpdateUserBody`/**`IssuedPasswordReset`** — both user bodies carry
  **`subject_uid: string | null`** (which person of the library the account is), and because `PATCH` replaces
  the profile, `null` **clears** the link rather than leaving it alone, which is why `setUserDisabled`'s
  re-enabling `PATCH` echoes the row's own link back; the password hash has nowhere to leak — the backend
  doesn't serialize it and no type has a field for it,
  `audit.ts` = the audit client over `GET /api/v1/audit`: `fetchAuditLog(params,signal)` →
  `AuditListResponse{entries,total,limit,offset,next_offset}`, `buildAuditQuery` serializes the filters
  (it omits empty ones/a zero offset); the types `AuditRecord` (nullable `actor_uid`/`target_uid`/`ip`/
  `user_agent`/`details`)/`AuditListParams` (incl. `via:'review'` + `decision:'yes'|'no'` for the admin
  overview of review decisions); plus `fetchMyActivity(params,signal)` over `GET /api/v1/audit/mine` — the same
  response, the same query builder, but `MyActivityParams` = `AuditListParams` **without `user`**: whose actions
  the listing shows is the server's decision (it takes the actor from the session and answers a request naming
  somebody else with 403), so there is nothing for the client to say about it; it shares `ApiError` from `auth.ts`. Mind the terminology:
  the query params use the endpoint's names (`user`/`entity_type`/`entity_uid`), the records use the columns'
  (`actor_uid`/`target_type`/`target_uid`),
  `i18n/` (the i18next init — the options are exported as `initOptions`, so that a test can boot them
  into its own instance — + `locales/{cs,en}/common.json` + `albumNames.ts`
  (`albumDisplayTitle(title, language)`: the **display-time only** Czech rendering of the machine-made
  English album titles the import left behind — a leading English month with an optional four-digit year
  (`January 2026` → `leden 2026`) and a whole title that is a known English country name (`Czechia` →
  `Česko`, both `Czechia` and `Czech Republic`). The match must be **exact**: `January in Norway` and
  `May Day` stay as they are, and so does everything on a non-Czech UI. Nothing is ever written back —
  renaming albums in the database is explicitly out of scope; tests `albumNames.test.ts`)
  + `countryNames.ts` (the **shared** English→Czech country dictionary and the two ways to read it:
  `countryDisplayName(name, language)` = the exact lookup `albumDisplayTitle` starts from, and
  `localizeCountryNames(text, language)` = the same dictionary applied **inside a composed name** —
  the photo titles (`Jan / Czech Republic / 2026` → `Jan / Česko / 2026`) and place albums
  (`Czech Republic 2026` → `Česko 2026`) the importers wrote. It substitutes only a whole `/`- or
  `,`-separated **segment**, optionally with a trailing four-digit year, which is what keeps
  `New Zealand trip` from becoming `Nový Zéland trip`; an unknown name and a non-Czech UI both fall
  back to the stored string. The dictionary lives in TypeScript rather than in `common.json` because
  it is keyed by *stored data*, not by a UI key. Tests `countryNames.test.ts`);
  typed keys via `types/i18next.d.ts` — add new strings to **both** locale files;
  **Czech is the default**, no hard-coded UI texts — everything through `t()`. The only detector is
  `localStorage` (which `LanguageSwitcher` from `AccountPage` writes to); `navigator`/`htmlTag` are **deliberately
  not** in `detection.order`, otherwise a browser set to English would get an English UI on the first visit —
  without a stored choice it is `fallbackLng: 'cs'` that decides. **Pluralization** via
  i18next CLDR plural suffixes: count-bound strings where the noun agrees with the number have
  the forms `key_one/_few/_many/_other` (Czech) and `key_one/_other` (English) — the caller only passes
  `{ count }` (e.g. `albums.photoCount`, `clusters.size`, `bulkEdit.title`, `duplicates.memberCount`/
  `archived`, `trash.confirm.bulk`); label forms with a colon/parenthesis (`library.count`, `selection.count`)
  stay without a plural. **Dates/numbers respect the language** via `lib/format` `formatDate`/`formatDateTime`
  (`i18n.language`). **Drift-guard tests** `i18n.test.ts` (cs/en have identical *logical* keys after
  the plural suffix is stripped, no empty values, each language has all of its CLDR plural categories,
  the interpolation `{{var}}` variables match across languages; plus **default-language tests** over
  a fresh instance from `initOptions`: an empty localStorage → `cs` even under an English browser,
  a stored choice wins, a language change is stored) + `screens.test.tsx` (representative
  screens — the navbar + tiles — render without missing-key warnings in both cs and en via
  `cloneInstance({saveMissing})`, plural rendering 1/3/5, a language switch rewrites the visible text)),
  `styles/tokens.css` (**the design token layer** — the single source of truth for spacing, radii, elevation,
  motion and the typographic scale; imported **once** in `main.tsx` right after the Bootswatch CSS and **before**
  `app.css`, which consumes the tokens. Bootswatch Superhero remains the base theme — this is a layer
  **on top** of it, it doesn't override the `--bs-*` variables globally (the only exception is a targeted **theme root**).
  **Theme root:** the application runs with `data-bs-theme="dark"` on `<html>` (in `index.html`) — without it
  Superhero leaves `--bs-tertiary-bg`, `--bs-secondary-bg(-subtle)` and `--bs-emphasis-color` at
  **light** values on `:root` and only flips them into the dark inside `[data-bs-theme=dark]`, so
  the `.bg-body-tertiary` panels (the library's advanced filter, `SelectionBar`, an audit row's detail) and
  the skeletons (`.bg-secondary-subtle`) were painted almost white under an almost white `--bs-body-color` =
  invisible labels (white-on-white). Superhero also colors the whole chrome a saturated navy; a photo app
  must do the opposite — the only saturated thing on the screen should be the photo. `:root[data-bs-theme='dark']` in `tokens.css`
  therefore **re-pins a handful of `--bs-*` variables to an identity of our own**: a warm-neutral **near-black**
  ramp (`--bs-body-bg`/`-color`, `--bs-tertiary-`/`secondary-bg`, `--bs-card-bg`, `--bs-border-color`
  and `--bs-dark` for the navbar) and **one cool azure accent** (`--bs-primary`, `--bs-link-color`,
  `--bs-navbar-active-color` + `--bs-primary-*-subtle/emphasis`). Every re-pin points at a `--kk-*` token,
  so the palette lives in one place. Contents: the **accent** `--kk-accent` (light — text/link/focus on
  dark surfaces), `--kk-accent-hover`, `--kk-accent-solid` (darker — a fill with white text at AA),
  `--kk-accent-solid-hover`, `--kk-accent-subtle`, `--kk-on-accent` (the azure is a deliberate choice, not
  orange: the three entity hues are taken, `danger` is red, so one unoccupied hue is left, and
  a cool accent on warm chrome doesn't fight the photos); **surfaces + elevation** — a warm-near-black ramp
  `--kk-surface-page`/`-1`/`-raised`/`-overlay` + `--kk-surface-sunken` (a well) and `--kk-surface-border`
  (a hairline); a **translucent header** `--kk-header-bg` (the page's tone at 72 %), `--kk-header-blur`
  (14px) and `--kk-header-border` — for the slim navbar sitting above a scrolling photo wall (see `app.css`,
  with an `@supports` fallback to the full `--kk-surface-1`); elevation is read from **the surface level + a hairline
  line**, not from a heavy shadow
  (`--kk-shadow-0..3` are therefore light — just a gentle anchoring + an `inset 0 1px 0` top highlight; `3` is
  the exception for a lifted tile/overlay); **text** `--kk-text`/`--kk-text-muted` (a warm white, the muted one
  above the Superhero baseline contrast); **spacing** `--kk-space-1..7` (a 4px scale), **radii**
  `--kk-radius-sm/md/lg/pill` (one continuous corner, an 8/12/16 rhythm; `md` is the canonical one), **motion**
  `--kk-duration-fast/base/slow` + `--kk-ease-standard`, a **focus ring** `--kk-focus-ring-*` (the color =
  the azure accent, one visible ring everywhere), **typography** a modular scale (a ~1.2–1.25 step)
  `--kk-font-size-display`/`-page-title`/`-section-title`/`-body`/`-caption` + `--kk-line-height-*`/
  `--kk-tracking-*`.
  Semantic classes: the **typographic scale** `.kk-display` (the largest step — a hero number/statistic),
  `.kk-page-title` (one per route, on the `<h1>`; it carries `overflow-wrap: anywhere`, because some routes are
  titled with user data — an album/label/person name without spaces wraps inside the header instead of
  overflowing past the viewport's edge, and it also stops holding its flex row stretched),
  `.kk-section-title` (a panel/section heading,
  `<h2>`/`<h3>`), `.kk-text-body`, `.kk-text-caption`, `.kk-text-eyebrow` — components **don't set
  their own `font-size`** (no `h3`/`h5`/`fs-5` utilities on headings, no inline `fontSize`);
  **surfaces** `.card` (elevation via a raised fill + a hairline line `--bs-card-border-color:
  var(--kk-surface-border)` and `--kk-shadow-1`; `.border-primary` and friends still work) and `.kk-surface`
  (raised + a line); **tiles** `.kk-tile` + `.kk-tile__media` (without a border — a photo has an edge of its own,
  elevation,
  a hover/focus lift to `--kk-shadow-3` — used by `AlbumTile`, `SubjectTile`, `PhotoTile`;
  `:focus-within` covers `PhotoTile`, where it is only the inner link that is focusable).
  **A hero-first photo wall**: tiles **inside `.kukatko-photo-grid`** (those only — album/label/people
  tiles keep their card) drop both the shadow and the lift and shrink the radius to `--kk-radius-tile` (2px); instead of a lift, hover
  **zooms the image** (`scale(1.05)` inside `overflow:hidden`, without a layout shift), the bottom
  `.kk-tile__caption` reveals the date above a `--kk-tile-scrim` scrim, and the focus ring is drawn **inwards**
  (a negative `outline-offset`), so that on a dense wall it doesn't overflow across the hairline gap onto the neighbours.
  And `.kk-tile-row`
  (the row variant for the label list — instead of a lift it is highlighted with a background, because a row in a column
  has nowhere to jump); `.kk-tile__placeholder`; a **chip** `.kk-chip` (a removable token on top of the
  Bootstrap `.badge` — only what a badge lacks: a box around the trailing `.btn-close` and a width ceiling,
  so that a long album name is truncated instead of stretching the row; used by `MultiSelect`);
  **entity colors** — an album/tag/person each get their own hue, so that they can be told apart at a glance
  (previously an album and a label were both the same primary orange = indistinguishable). The tokens
  `--kk-entity-album-bg` (violet) / `--kk-entity-tag-bg` (turquoise) / `--kk-entity-person-bg`
  (pink) + `--kk-entity-fg` (white); the modifiers `.kk-entity-album/-tag/-person` on a `.badge`
  (the color has `!important`, so as to beat the Bootstrap `.bg-*`/`.text-bg-*`, which are `!important` too,
  so the class fits a plain `.badge` as well as a `<Badge>` and a link pill). The kind→class+icon mapping is
  **once** in `components/entityStyle.ts` (`ENTITY_STYLE`) and it is read by every place where an entity
  is shown as a chip: the library's active filter chips (`FilterBar`), a photo's organize panel
  (`OrganizePanel`), the strip of badges above a photo (`OrganizeBadges`) and `GlobalSearchSections` — the color
  language is thus consistent, not one-off. A chip that **links** is the shared `EntityChip`, which puts the
  colour and the glyph on the anchor itself, so the whole pill is also the tap target.
  Color is **only a supplement**: a chip always carries a text label and a guiding icon as well (album `collection` /
  tag `tags` / person `person-circle`), so that the distinction survives for the color-blind; white text has a contrast
  of ≥ 5:1 on a near-black background. Neutral filters (year, rating, flag…) stay `text-bg-primary`;
  **appear** `.kk-appear` (a one-off fade-up).
  **Motion tokens:** three durations `--kk-duration-fast/base/slow` (120/200/320 ms) + one curve
  `--kk-ease-standard` (decelerate) carry all the hover/focus/open-close micro-interactions; the manual `ms`
  values scattered across the components are funnelled onto them (`PhotoTile`, `TrashCard`, `LivePhoto`,
  `CompareStage`, the `PhotoDetailPage` still-zoom, the `review.css` progress). The loading of images and skeletons
  has two shared classes: **`.kk-media-img`** (a fade + a `scale(0.98)` settle after decoding; it shares the
  `transform` transition with the library wall's hover zoom, which has a higher specificity) and **`.kk-skeleton`**
  (a shimmer gloss travelling across a warm surface-1 block, the period `--kk-duration-skeleton` = 1400 ms,
  `linear infinite`). **The focus outline is never removed** —
  `.kk-tile:focus-visible`/`.kk-tile__media:focus-visible` draw an `outline` (it survives the preview's `overflow:
  hidden`). **`prefers-reduced-motion`**: the token durations drop to `1ms`, so the lift
  (`transform`), `.kk-appear` and the `.kk-media-img` fade become instant; the skeleton shimmer
  (`--kk-duration-skeleton` doesn't belong in that collapse) is instead switched off directly and stays a static block;
  spinners and progress bars keep animating, because they carry meaning),
  `styles/app.css` (**a global responsive polish layer** imported in `main.tsx` right after
  `tokens.css` — only cross-cutting mobile/touch things that Bootstrap utilities can't do: **safe-area
  insets** via `env(safe-area-inset-*)` (they work thanks to `viewport-fit=cover` in `index.html`) on the
  navbar (`.kukatko-navbar`) and the main container (`.kukatko-main`); a guard against horizontal
  scrolling/overscroll bounce (`body overflow-x:hidden`, `html overscroll-behavior-y:none`); the shared
  **sticky-toolbar offset** `.kukatko-sticky-toolbar` (`top: navbar height + safe-area-inset-top`,
  a z-index below the navbar — in-page sticky bars like `SelectionBar` settle under the navbar, not beneath it);
  the **minimum tap target** `.kukatko-tap-target` (2.75rem/44px) for icon-only controls like
  `FavoriteButton` + its touch-only twin `.kukatko-tap-target-touch` (the same 44px square, but
  only inside `@media (pointer: coarse)` — for controls that should stay visually small next to text with a mouse:
  the `?` by the query field, a row action whose word shrinks to a glyph on a phone); **`.kk-min-w-0`**
  (`min-width: 0` on a flex item — without it the item refuses to shrink below the minimum of its content and
  one long unbreakable name pushes the whole row/header past the viewport; used by the rows of
  `SavedSearchesPage` and the title groups in `AlbumDetailPage`/`LabelDetailPage` — the labels cloud
  carries the same `min-width: 0` inside `.kk-label-chip__link` instead);
  **`.kk-multiline`** (`white-space: pre-wrap` — a value the user typed into a `<textarea>` and that is read
  back as plain text: HTML would collapse its line breaks into spaces, so a two-line photo description came
  back as one line. `pre-wrap`, not `pre-line`, keeps a run of spaces too — in hand-typed text that is the
  author's decision; pair it with `text-break`. Used by `EditableField` in `MetadataPanel` and by
  `RecordTable`'s `multiline` columns);
  an **app-wide touch-target floor** — a `@media (pointer: coarse)` block that on
  touch devices (phone/tablet) enforces a minimum of 44px on `.btn`/`.form-control`/`.form-select`/
  `.nav-link`/`.dropdown-item`/`.list-group-item-action`/`.page-link`/`.navbar-toggler` (the hamburger
  is additionally centred with flex, otherwise the icon would sit on the baseline once the box grew) and on `.btn-close`
  (every closing X — the announcement banner, modal/offcanvas headers, toasts, clearing the query in the palette —
  grows as a `border-box`, so only the hitbox grows and the glyph stays `1em`; the exception is `.badge
  .btn-close`, i.e. the X inside a pill chip, where the target is the chip itself and 44px would tear the pill apart)
  + **a pill chip that links** (`a.badge` and — for a chip that pairs its link with a remove X — `.badge:has(> a)`:
  the list above is by class and a `.badge` is none of those, so the photo panel's album chip stayed 79.1 × 12.0px
  inside a 111 × 20.9px pill on a phone, under even WCAG 2.2 SC 2.5.8's 24px. The pill gets `min-height: 2.75rem`,
  drops its block padding and stretches the link inside it (`.badge > a`), so the whole chip
  is the target while the type, the hue and the inline padding stay the desktop chip's — a chip is secondary
  metadata, not a button competing with the panel's actions. Keyed on the tag rather than on yet another class, so
  the next component that renders a chip that links inherits the target instead of repeating the bug; the shape it
  keys on is `EntityChip`)
  + a bigger `.form-check-input`
  + **`.kk-face-box::after`** (a face frame in `FaceOverlay` is a `<button>` exactly the size of the bbox — it isn't
  a `.btn`, so the floor doesn't catch it, and the box itself can't be enlarged, otherwise the outline would slide off the face; it therefore gets
  an invisible hitbox: an `::after` centred via `top/left: 50%` + `translate(-50%, -50%)`, a `2.75rem` square
  with `min-width/height: 100%`, so that only a small frame grows and a large one keeps a target of its own; nothing is drawn,
  `pointer-events` are inherited, so a `readOnly` frame stays click-through),
  without touching the desktop (fine-pointer) layout and without per-component changes (a systemic fix for the
  ubiquitous `size="sm"` controls; the guards are in `styles/tapTargets.test.ts`, because jsdom doesn't evaluate media
  queries);
  **native form chrome** — Superhero bakes `.form-control`/`.form-select` white (`#fff`) regardless of the
  theme; instead of pinning them to a light scheme we give them a real dark surface `--kk-surface-sunken` with
  a hairline line and `color-scheme: dark` (the fill and the scheme agree, so the native glyphs — the `type=date`
  calendar, a select's list — are light-on-dark and visible); the select's chevron is a light-drawn
  copy via `--bs-form-select-bg-img`; **the accent on baked-in controls** — Bootswatch bakes the
  orange fill in directly (not through `--bs-primary`), so `app.css` overrides it to azure:
  `.btn-primary`/`.btn-outline-primary`, `.form-check-input:checked`/indeterminate, the `.form-range`
  thumb, `.progress-bar` (+ the track as a well), `.dropdown-menu` (a warm overlay + the active item),
  the `.list-group` active row and the `.navbar.kukatko-navbar` active link;
  **the quiet action** `.btn-outline-secondary` (the app's commonest button — rotate, cancel, „zobrazit více",
  85 call sites): Bootswatch bakes its label *and* its border as the theme grey-blue `#4e5d6c`, which it also bakes
  into `--bs-card-bg` **on the `.card` selector itself** — so the tokens' `--bs-card-bg` re-pin never applies, cards
  really are that navy, and inside one the button's label sat at contrast **1.00** on its own background: the
  reported „the text is the same colour as its background". The label is re-pointed at `--bs-body-color` and the
  border at a 60 % tint of `--kk-text`, with a translucent **dark** scrim on hover/active — a *lighter* fill would
  push the white label back under 4.5:1 on the card it lightens. Measured in the browser (blended, incl. alpha):
  5.6:1 text / 3.2:1 outline on the card and 15.4:1 / 6.1:1 on the page, so both clear AA for text and the 3:1 for
  the control's own boundary;
  **a slim translucent navbar** `.kukatko-navbar` (it sits ABOVE the scrolling content: the fill `--kk-header-bg`
  = the page's tone at 72 % + `backdrop-filter: blur(--kk-header-blur)` frosts whatever scrolls beneath it,
  a hairline bottom line `--kk-header-border`; an `@supports not (backdrop-filter…)` fallback to the full
  `--kk-surface-1`, so the bar is never transparent without the blur; a thinned `padding-block`, on a fine
  pointer the `.nav-link` tap target relaxes to 2.25rem — which is why `--kukatko-navbar-height` is 3.25rem,
  dimensioned for the taller touch case); **a calmer nav** — an inactive `.nav-link` is muted, the active
  route carries one accent state = a `--kk-accent-subtle` pill + accent text (apart from the CTA);
  **the global command palette** `.kukatko-search-trigger` (a field-as-trigger leading the bar, slim on a fine
  pointer, 44px on coarse, growing on mobile) + `.kukatko-search-dialog`/`-panel` (a top-anchored
  console on `--kk-surface-overlay`) + `.kukatko-search-option` (a row: a preview/glyph + text + a count,
  the active row `--kk-accent-subtle` + an inset accent bar) + `.kukatko-search-legend` (a footer
  key legend, hidden on a phone) — the basis for the `SearchCommand` component;
  **the map (Leaflet)** `.kukatko-map*` — Leaflet has no theme hooks, so its controls are reached
  through its own classes, but **scoped under `.kukatko-map`** (the element Leaflet itself puts
  `leaflet-container`/`leaflet-touch` on) — this both keeps the override on our maps and
  beats the Leaflet stylesheet that the bundler emits *after* `app.css` (`.leaflet-touch
  .leaflet-bar a` has the same specificity as a single-class override and would win on order);
  `.kukatko-map` = `position: relative` (so that the overlay is positioned from the first paint, not only
  after Leaflet's initialization) + `height: 70vh` as a **fallback** for the inline `70dvh` (an engine that
  rejects the unit drops the inline declaration and the map would have no height at all);
  `.kukatko-map-gesture-hint` = the „dvěma prsty" hint — a frame across the whole map only for the sake of
  centring, what is visible is a **small pill** in `::before` (`content: attr(data-label)`); **not a scrim
  across the whole map**, because a finger crosses a 70dvh-tall map on every scroll of the page, so
  the hint has to be a note, not a curtain; `pointer-events: none` on both — a hint that
  eats the next tap would be worse than none; `.leaflet-marker-draggable` inside the map gets
  `touch-action: none`, because **the picker's draggable pin** has to answer a single finger even on a gesture-handled
  map (the effective `touch-action` is the intersection with the ancestors and `none` is the narrowest);
  `@media (pointer: coarse)` then raises the Leaflet toolbar (26px, 30px with `leaflet-touch`) to 2.75rem
  including the `line-height` and a bigger `font-size` +/- glyphs, gives the mandatory mapy.com logo a 44px hitbox
  and loosens the attribution's line height (that one is text, not a button — a 44px band across the whole bottom edge would
  swallow taps); the guards are in `styles/tapTargets.test.ts`;
  **dark Leaflet chrome** — Leaflet has its light theme hard-wired into the stylesheet (a white popup
  bubble, a white translucent attribution band, white toolbar buttons, a `#ddd` backdrop) and no
  variable to recolor it, so the tokens are pushed in through its classes;
  `.kukatko-map.leaflet-container` = the backdrop `--kk-surface-sunken` (Leaflet's `#ddd` is a light
  flash in the middle of an almost black page — it is visible past the edge of the world and before the tiles load);
  `.leaflet-bar` = a hairline `--kk-surface-border` + `--kk-radius-sm` + `--kk-shadow-2`, the buttons
  `--kk-surface-overlay`/`--kk-text`, hover the same accent tint as the app's other rows,
  `leaflet-disabled` muted and **after** hover (it ties with it on specificity);
  `.leaflet-control-attribution` = `--kk-text-muted` at 88 % of the page's tone, the link `--kk-accent`
  (the header's 72 % can't be taken here — the attribution sits directly on the tiles and above a snow-white
  tile the link would fall below AA); the popup (`.leaflet-popup-content-wrapper` + `-tip`) =
  `--kk-surface-overlay` + a hairline + `--kk-shadow-3` + `--kk-radius-md`, the tip carries the same
  border (it is clipped to two edges, which thus continue the bubble's outline), the closing „×"
  `--kk-text-muted` → `--kk-text` on hover and on **focus**, the links in the content
  `--kk-accent`/`--kk-accent-hover` (the whole card is one link to the detail and the markup has no underline)
  and the preview gets `--kk-radius-sm`; **the tiles, pins and clusters are not recolored** (that is the map's
  content, not chrome) and the Leaflet tooltip/layers/scale aren't styled at all — the app doesn't render them;
  the guards are in `styles/mapChrome.test.ts` (every `.leaflet-*` selector in `app.css` must start with
  `.kukatko-map`, colors only from tokens, compound selectors against Leaflet);
  **the timeline** `.kukatko-timeline*` (a fixed vertical data bar at the right
  edge below the navbar, absolutely positioned ticks, a floating label for the active month, `touch-action:
  none` for dragging, hidden at widths ≤ 575.98px); **the filter bar** `.kukatko-filter-*`
  (`.kukatko-filter-search` = the search field grows and fills the header's row, `.kukatko-filter-sort`
  a minimum width, `.kukatko-filter-panel` = 44px tap targets on the panel's elements, `.kukatko-filter-chip`
  = a tappable pill chip with a cross); **the labels cloud** `.kk-label-*` (`.kk-label-cloud` = the
  wrapping flex cloud, `.kk-label-chip` = the pill that owns the hairline and the hover, split between
  `__link` (`min-width: 0` so a long name truncates, `__name`/`__count`/`__skipped` inside) and the
  caret-less `__menu`; `.kk-label-family` = the dashed toggle of a folded numbered family and
  `.kk-label-cloud__group`/`__members` = the full-width line an expanded one takes, plus its boxed
  member list; on a coarse pointer the whole pill grows to 44px instead of trimming its menu);
  the CSS variable `--kukatko-navbar-height`),
  `test/setup.ts` (a jsdom **`window.matchMedia` stub** — a non-matching default, individual tests can
  override it to simulate a phone — a **`PointerEvent` stub** (a `MouseEvent` subclass; without it jsdom
  dispatches `pointerdown`/`pointermove` as a bare `Event` with no `clientX`/`clientY`, so a drag test
  could only ever read NaN coordinates) plus the stubs for `setPointerCapture`/`scrollIntoView` and RTL's
  `cleanup()`; **mock restoration deliberately does not live here either** — `restoreMocks: true` in
  `vite.config.ts` restores mocks *before* each test, and restoring again in an `afterEach` races with
  that `cleanup()`, see `docs/ARCHITECTURE.md` §19.3),
  `test/batchBar.ts` (fixtures shared by the tests of pages with a photo grid: `BATCH_ACTIONS` = the complete
  dictionary of `BatchActionBar` actions by accessible name — every page asserts it **in full**, so that it
  can't silently regress to a stripped-down toolbar — and `albumOption()` for `fetchAlbums` in the picker),
  `test/timeline.ts` (`realisticTimeline()` = the production library's shape as a fixture: ~460 month
  buckets over 1905–2026, ~10 500 photos, a couple a year before 1950 and thousands a year after 2010 —
  the timeline rail only ever broke on a distribution like this, a development-sized library makes any
  layout look fine, so every test that has to prove the rail stays legible starts from this one),
  `test/css.ts` (reading and mini-parsing stylesheets from tests: `readCss` / `ruleBody` / `declarations`
  / `zIndexOf` / `installRule` — jsdom evaluates neither `env()` nor media queries, so CSS-only rules are
  guarded by reading the file; `zIndexOf(css, prelude)` answers what layer a rule really claims, resolving
  a `var(--kk-*-z)` reference against the sheet's own `:root` scale (jsdom computes no custom properties),
  so two layers can be compared by what the app actually ships — used by `MultiSelect.test.tsx` and
  `MobileTabBar.test.tsx`; `installRule(path, selector)` goes one step further and hands **the real rule body** to the jsdom
  document (returning the function that removes it again), so a component test can assert what an element
  *computes to* — `getComputedStyle(el).whiteSpace` — instead of which class name it carries: it then fails
  both when the element loses the class and when `app.css` stops declaring the rule (`MetadataPanel.test.tsx`,
  `UsersPage.test.tsx` and the multi-line values below); it is used by `styles/tokens.test.ts`, `styles/mapChrome.test.ts` (the Leaflet override —
  the scoping under `.kukatko-map`, colors only from tokens, the compound selectors that beat the Leaflet
  stylesheet), `styles/safeArea.test.ts`, which computes the padding of the
  fullscreen overlays (`review.css`, `compare.css`) against the iPhone's insets and asserts that the
  control rows clear both the notch and the home bar — and that without the insets the spacing stays exactly as before —
  and `components/people/ClusterCard.test.tsx`, which guards both pointer variants of
  `clusters.css` the same way).
  Routing in `App.tsx`: the route table lives in the exported `AppRoutes` (so that a test can mount it
  into a `MemoryRouter` and verify the wiring itself — `App.test.tsx`), `App` merely wraps it in
  `BrowserRouter`+`AuthProvider`+`CapabilitiesProvider` (the capabilities provider sits inside the auth provider,
  because `/capabilities` is behind `RequireAuth`). `/login` is public, the rest is under `RequireAuth`; `/slideshow` and
  the immersive `/photos/:uid` are under `RequireAuth` but **outside `Layout`** (fullscreen without the navbar),
  the rest is under `Layout`
  (**`/` = `LibraryPage`** — the library is the home page; `/library/*` → `LibraryRedirect`
  (a `replace` redirect to `/` with the query string preserved, the splat also covering the
  inherited addresses),
  `/favorites`, `/albums`, `/albums/:uid`, `/labels`, `/labels/:uid`, `/search`, `/saved`, `/map`,
  `/places`, `/people`,
  `/people/:uid`, `/account`, `/help`; `/upload`, `/people/clusters`, `/faces`, `/recognition`, `/trash` and
  `/duplicates` are additionally under `RequireRole role="editor"` = write-only (and `/duplicates/compare` there too,
  but **outside `Layout`** — fullscreen like `/review`), `/import` under `RequireImport` (= maintainer,
  `canImport`), `/maintenance` and `/system` under `RequireRole role="maintainer"` = operations (maintainer
  only), `/users` and `/audit` under `RequireRole role="admin"` = governance (admin **or**
  maintainer)). **A role-gated route a user may not enter renders `ForbiddenPage` on that very URL** —
  no redirect, so the address bar, a reload and Back all keep pointing at what was asked for
  (`App.test.tsx` covers the fullscreen `/review`, the in-shell `/upload` — where the library must *not*
  fetch behind the refusal — and Back off `/duplicates`). **A guarded route with an unreachable backend
  renders `OfflinePage` on that URL** for the same reason. `App.coldLaunch.test.tsx` is the one suite
  that mounts the real `App` (`BrowserRouter` and all) rather than `AppRoutes`, because the offline
  defect lived in how the three providers compose, not in any one of them: it opens the app at `/` and
  at `/login` with every request failing at the transport and asserts the reader gets an explanation
  rather than a login form, that the `PwaStatus` banner does reach `/login`, and that the login
  warning holds even when `navigator.onLine` lies and says `true`. Config:
  `vite.config.ts` (the build → `../internal/web/static/dist`, vitest jsdom, **`restoreMocks: true`** =
  the single place mocks are restored, the dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed, plus a test-file-only
  `no-restricted-syntax` that bans `vi.restoreAllMocks()`), `.prettierrc.json`,
  `tsconfig*.json`.

  **`build/gitkeep.ts` (`gitkeepPlugin`, `apply: 'build'`).** `emptyOutDir: true` wipes
  `internal/web/static/dist` — including the **tracked** `.gitkeep` that keeps `//go:embed
  all:dist/*` compiling on a fresh clone. A deleted tracked file is a modified working tree to Go's
  VCS stamping, which is why every release binary up to v0.8.0 reported `v0.8.0+dirty`. The plugin
  writes the empty placeholder back at `closeBundle` (after Vite has emptied the directory and
  written the bundle), so every build path — `make web-build`, `scripts/dev.sh`, the Dockerfile, the
  goreleaser before-hook — leaves the checkout clean from one place. `build/gitkeep.test.ts` runs a
  real `vite build` over a scratch project (hence `// @vitest-environment node`: esbuild refuses to
  run under jsdom) and asserts the stale output is gone while the placeholder is back.

  **Installable PWA (`web/build/`, `src/pwa/`, `components/pwa/`).** The app installs to a phone
  home screen and opens standalone. Four pieces:
  **(1) Identity.** `public/icons/kukatko.svg` is the master icon — an original door-peephole motif
  (metal ring, dark bezel, azure lens with a glint, the aperture in the middle) painted in the app's
  own tokens: the warm near-black surface ramp and the single cool accent, so the launcher icon and
  the app read as one product. `kukatko-maskable.svg` is the same artwork full-bleed (no rescaling
  needed — the motif's outer edge sits at r=190, inside the 204.8 safe circle a maskable icon must
  respect). Every raster is **generated and committed** by `scripts/icons.sh` (headless Chromium →
  crop; ImageMagick only for the crop and `favicon.ico`): `icons/kukatko-{192,512}.png`,
  `icons/kukatko-maskable-{192,512}.png`, `apple-touch-icon.png` (180, from the maskable master —
  iOS masks it itself and composites onto black), `favicon-{16,32}.png`, `favicon.ico`. Nothing is
  fetched at runtime and `vite.svg` is gone.
  **(2) Manifest.** `public/manifest.webmanifest`: name/short_name „Kukátko", `lang: cs`,
  `display: standalone`, `id`/`start_url`/`scope` `/`, `theme_color` = `background_color` =
  `#15130f` (= `--kk-surface-page`), the five icons above incl. both maskable purposes. `index.html`
  links it together with the favicon set, `apple-touch-icon`, the `theme-color` meta and the iOS
  standalone metas (`apple-mobile-web-app-{capable,status-bar-style,title}`; `black-translucent`
  matches the `viewport-fit=cover` + safe-area insets the layout already assumes).
  **(3) Service worker.** `build/service-worker.js` is the template — plain JS, not bundled;
  `build/pwa.ts` (`pwaPlugin`, `apply: 'build'`) substitutes its two placeholders at
  `generateBundle` and emits `/sw.js` unhashed at the output root. `buildPrecacheManifest` derives
  the shell (index.html + every content-hashed bundle asset) from the finished bundle;
  `cacheNameFor` hashes that manifest into the cache name, so an unchanged shell keeps its cache and
  a changed one gets a fresh one that `activate` swaps to after pruning the old. The fetch handler
  is a **whitelist**: it answers navigations from the precached shell and precached asset paths from
  the cache (refetching an evicted entry), and calls `respondWith` for **nothing else** — non-GET,
  cross-origin (signed R2 media), ranged (video scrubbing) and anything under `/api/`, `/healthz`,
  `/metrics` fall through exactly as with no worker installed. Files copied from `public/` are not
  precached.
  **(4) Registration and update flow.** `src/pwa/register.ts` — `registerServiceWorker({enabled,
  onUpdateReady, reload})` registers `/sw.js` at scope `/` once per load; `enabled: false` (dev)
  actively **unregisters** leftovers so a dev server is never shadowed by a production shell. Install
  never calls `skipWaiting`, so a deployment parks in "waiting" and the page keeps the shell its
  assets match; the module reports it via `onUpdateReady` (only when `controller` exists — a first
  install supersedes nothing), re-checks `registration.update()` whenever the tab returns to the
  foreground, and `applyServiceWorkerUpdate()` posts `SKIP_WAITING` and reloads once on
  `controllerchange`. `src/pwa/usePwaStatus.ts` wraps that plus `online`/`offline` events;
  `components/pwa/PwaStatus.tsx` renders the two ambient alerts (i18n `pwa.offline`,
  `pwa.update.*`) in a fixed bottom stack (`.kk-pwa-status`, z-index 1090 = under the toasts, clears
  `--kk-tabbar-height`). It is mounted in `App` **outside** `AuthProvider` and outside `Layout`, so
  it also reaches the login screen and the shell-less immersive routes — verified, not merely
  intended: `App.coldLaunch.test.tsx` mounts the real `App` at `/login` with the network down and
  asserts the banner is there. **`pwa.offline` is held to what the worker actually caches**, which is
  the app shell and nothing else — it never touches `/api/`, so no photo, album or search result
  survives the connection. It used to read "shows only what it has stored", which promised an offline
  library; every page under the banner then failed to load. It now says the app opens and the library
  needs a connection. One thing to know before debugging this banner: **`Network.emulateNetworkConditions`
  is scoped to the CDP target it was issued on**, so a document created in a *new* target after the
  network was "cut" loads normally and reads `navigator.onLine === true`. Measured on 2026-08-12 with
  `agent-browser`: same tab → `false`, a tab opened afterwards → `true`, page fully loaded. So a report
  of "offline but the banner did not appear" from a devtools-emulated session is very likely the
  harness, not this code — reproduce it with a real radio off. `navigator.onLine` is nevertheless a
  weak signal on its own (a captive portal or a downed server is "online" by definition), which is why
  the auth path never consults it and branches on the failed probe instead (`AuthStatus.unreachable`).
  Tests: `build/pwa.test.ts`
  runs the **real rendered worker** in a `node:vm` scope with fake caches (precache, cache hits,
  every bypass rule, eviction refetch, the activate/skip-waiting handshake),
  `src/pwa/register.test.ts` covers the registration branches, `PwaStatus.test.tsx` the UI.
  **(5) Share target — photos straight from the phone's share sheet (Android/Chromium).** The manifest
  declares `share_target` (`action: /share-target`, `POST`, `multipart/form-data`, a `files` field
  accepting `image/*`+`video/*`), so an **installed** Kukátko appears in the system share sheet for photos
  and videos; a `shortcuts` entry points at `/upload` for a long-press straight to uploading.
  `src/pwa/shareContract.ts` is the contract both halves obey — the paths, the `files` field, the
  `share` query parameter, the `kukatko-share` cache, the `/__kukatko-share__/<id>/<index>` key shape, the
  two headers (`x-kukatko-share-name`, url-escaped, and `x-kukatko-share-modified`), `SHARE_TTL_MS` (24 h)
  and `parseShareEntry`/`shareIdStamp`/`isShareExpired`. It is deliberately **DOM-free**: the worker is
  un-bundled JS and cannot import it, so `build/pwa.test.ts` — typechecked without the DOM lib — imports
  it and drives the **real** worker against it, which is what keeps the halves from drifting.
  The worker's fetch handler answers exactly one non-GET: that POST. It reads the form, writes each file
  into its own entry of the **`kukatko-share` cache** (not the shell cache, whose name prefix `activate`
  prunes — a deployment landing mid-share must not eat the photos), mints the id as
  `<Date.now()>-<sequence>` (the timestamp **is** the expiry bookkeeping) and answers **303** to
  `GET /share-target?share=<id>`; the POST body itself is consumed once and never cached, and a staging
  failure still redirects, just without an id. `ShareTargetPage` then triages (see its entry above) and
  `UploadPage` collects. `src/pwa/shareTarget.ts` is the browser half: `collectSharedFiles(id)` rebuilds
  the `File`s from the staged responses **in share order**, deletes them as it goes (a share is handed
  over exactly once) and sweeps out any *other* share past the TTL on the way — the only place abandoned
  shares are cleaned up; it never rejects (no Cache Storage, no cache, a foreign entry, a failing quota
  all mean „no files"). `partitionSharedFiles` splits the result through `lib/mediaFiles`, so anything
  that is not a photo or a video is **named** on the page instead of queued to fail on the server, and
  `discardSharedFiles(id)` is the viewer's path. The share id rides in the URL, so the login round trip
  keeps it (`LoginPage` returns to `pathname + search`, not the pathname alone) and the files simply wait
  in the cache. With **no worker installed** the POST reaches Go, whose SPA fallback ignores the method
  (`internal/web`, tested) and answers with the shell — the same page, saying the files did not arrive.
  **iOS has no Web Share Target** (verified at implementation time against MDN's browser-compat data and
  webstatus.dev: Chrome/Edge only, WebKit bug 194593 open), and it is not faked: there the honest path is
  the installed app opening instantly from the precached shell with **Nahrát already in the bottom tab
  bar** (`MobileTabBar`, one thumb-reach from wherever it opened), the photo-library picker, and **paste**
  (`usePasteFiles`) — which the Help chapter `help.sections.sharing` spells out in both languages.
  Tests: the share half of `build/pwa.test.ts` (staging, headers, ordering, per-share ids, the ignored
  title/text parts, the failure redirect, cross-origin and other-path POSTs left alone, survival of an
  activate) plus a manifest contract block; `src/pwa/shareTarget.test.ts`, `ShareTargetPage.test.tsx`,
  the share cases in `UploadPage.test.tsx` (including the StrictMode double-invoke) and
  `usePasteFiles.test.tsx`.
