# Help page + "Nápověda" menu item

Add a user-facing Help page that explains, in plain language, how Kukátko works and what its features do, and link to it from the user account menu. This is end-user help (not developer/technical docs). Read `docs/FRONTEND.md` (routing, pages, i18n, Layout/nav) first.

## 1. Menu item

In `web/src/components/Layout.tsx`, the user account menu is a react-bootstrap `NavDropdown` (rendered when `user` is set) containing two items: "Můj účet" (`as={Link} to="/account"`) and "Odhlásit se" (logout `onClick`), split by a `NavDropdown.Divider`.

- Add a new `NavDropdown.Item as={Link} to="/help"` **after the account item and before the divider** (so Help sits with Account, above the logout divider). Mirror the existing item markup: `className="d-flex align-items-center gap-2"`, an `<Icon name="question-circle" />` (already in the `IconName` union — no change to `Icon.tsx` needed), label `{t('nav.help')}`, tooltip `title={t('nav.titles.help')}`.
- Add the i18n keys `nav.help` and `nav.titles.help` to **both** `web/src/i18n/locales/cs/common.json` and `en/common.json` (Czech default): e.g. cs `"Nápověda"` / `"Zobrazit nápovědu"`, en `"Help"` / `"Show help"`.

## 2. The Help page

- New component `web/src/pages/HelpPage.tsx` exporting `HelpPage`. Register it in `web/src/App.tsx` as `<Route path="/help" element={<HelpPage />} />` **inside the `<Route element={<Layout />}>` block, with no extra role guard** — it must be visible to ANY authenticated user (RequireAuth already wraps the shell). Add the import alongside the other page imports.
- Layout/styling: follow the app conventions — do NOT add an outer `Container` (the shell provides it). Use `<h1 className="kk-page-title mb-4">{t('help.title')}</h1>`, a comfortable reading column (`<Row className="justify-content-center"><Col xs={12} lg={9} xl={8}>` or similar), and organise the content into clearly-titled sections. Prefer a **react-bootstrap `Accordion`** (collapsible FAQ-style sections with a short table of contents at the top) for scannability — it's not used elsewhere yet, so introducing it here is fine; alternatively stacked `Card`s with `kk-section-title` headings (the AccountPage pattern). Body text uses `kk-text-body`. Icons only via the `Icon` component (bootstrap-icons). It must look right in the Superhero dark theme like the rest of the app.

## 3. Content (plain, user-friendly — not technical)

Explain what the app does and how each feature behaves from a user's point of view. Base the content on the features that actually exist in the app — cross-check against `docs/` and the real UI; do not document features that aren't present. Cover at least:

- **Browsing photos** — the library/timeline, and choosing how many photos per row (the grid-density control).
- **Search** — the quick filter (title & description) vs. full-text & semantic ("search by content") search, noting that semantic search depends on the AI service being available.
- **Albums** — grouping photos into albums, adding photos, and "expand" (finding more photos similar to an album).
- **Labels (štítky)** — tagging photos and managing labels.
- **Favourites & ratings** — that these are **per-user** (each person has their own favourites/ratings, not shared).
- **People & faces** — face detection, assigning faces to a person, suggestions "this person may also be here" (by similarity), the one-at-a-time **sorting/review** flow (Ano / Ne / Nevím) and the **leaderboard/competition**, and correcting mistakes.
- **Duplicates** — finding near-duplicate photos and merging them.
- **Stacks** — grouping variants of one shot (e.g. RAW+JPEG or edits) together.
- **Map & places** — where photos were taken.
- **Deleting & retention** — the difference between **archiving** (reversible, goes to the trash) and **permanent deletion**, and that archived photos are eventually auto-removed after a retention period. Note that permanent deletion / emptying the trash is restricted (admins).
- **Import** — that the library is imported/migrated (maintainers), kept incremental and non-destructive.
- **User roles / permission levels** — a short section explaining what each level can do (use the app's current role model): **viewer** (view only), **editor** (edit photos & metadata, sorting, manage albums & labels), **admin** (everything an editor can + manage users, audit log, permanently delete), **maintainer** (everything an admin can + imports, maintenance, system, backups). Frame it as "what you can do depends on your role."
- **Your account** — where to change your password / manage your account.

Keep the tone friendly and concise. When a feature is limited to certain roles, say so plainly (e.g. "Only editors can…", "Maintainers can…").

## 4. i18n

- Put all Help text through i18next in a **new top-level namespace** in both `cs` and `en` locale files, Czech default. Note: a `help` key already exists **nested under `search`** (`search.help.*`) — that's fine, a *top-level* `help` namespace does not collide, but if you prefer zero ambiguity use `helpPage` as the namespace name. Keep both locales in sync (same keys). Given the amount of prose, structure it as sections (e.g. `help.sections.<name>.title` / `.body`).

## 5. Tests

- Vitest: the Help page renders its title and the main sections; the route `/help` is reachable for an authenticated user. A Layout/nav test asserting the user menu contains the "Nápověda" item linking to `/help` (if the existing Layout test is a convenient place, extend it).

## Docs (Definition of Done)

Add the new page, route, and menu item to `docs/FRONTEND.md`. `make check` (ESLint + Prettier + Vitest + Go) + dev-server start must pass; commit & push (end with the required `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` line).