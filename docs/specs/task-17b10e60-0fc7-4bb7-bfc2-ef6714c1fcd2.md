# M0 — Auth frontend + URL-state/back foundation

Add the login flow, authenticated app state, protected routes, and the shared
URL-as-state/history foundation that the whole app uses so the browser Back button always works.

## Context
Read `docs/ARCHITECTURE.md` §11 (Auth), §13 (frontend; "Zpět vždy funguje"). Backend auth
endpoints exist under `/api/v1/auth` (`login`, `logout`, `me`, `password`). Frontend is
React + react-bootstrap (Superhero) + react-router + i18next, served embedded.

## Requirements
- **Login page** (Superhero-styled card): username + password, error states (invalid creds,
  rate-limited 429 message), i18n (cs/en). On success redirect to the originally requested route.
- **Auth context/provider**: loads `GET /auth/me` on boot, exposes `user`, `role`, `login`,
  `logout`. Logout button in navbar showing current user.
- **Protected routes**: unauthenticated users are redirected to login; routes/actions gated by
  role (viewer sees read-only UI; write actions hidden/disabled for viewer).
- **URL-state/back foundation**: a reusable hook/utility to read & write view state
  (filters, sort, search query, page) to URL query params via the History API, so navigating and
  Back/Forward restore prior state. Establish the convention and document it (e.g.
  `web/src/lib/urlState.ts`) for later list/library tasks to use.
- **Account page**: change own password (calls `POST /auth/password`).

## Quality gate (mandatory)
- `make check` MUST pass (frontend ESLint + Vitest included).
- Vitest tests: login form validation + error rendering, protected-route redirect, urlState
  hook round-trips state to/from query params (and Back restores it). Mock fetch/router.
- Components small and typed; i18n strings for all user-facing text.