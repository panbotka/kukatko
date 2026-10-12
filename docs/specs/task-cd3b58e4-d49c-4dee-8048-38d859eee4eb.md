# One API path helper for the whole frontend

## Context

Kukátko is becoming multi-tenant. The backend already serves every project-scoped endpoint at `/api/v1/p/<slug>/…` and still serves the old unprefixed paths as aliases to the bootstrap project `v`. `GET /auth/me` returns `instance_role`, `projects: [{uid, slug, name, active, role, approved}]` and `last_project_slug`. Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, section 3.3; read `docs/FRONTEND.md`. Step 08 of 40.

Today 53 service modules under `web/src/services/` each declare `const API_BASE = '/api/v1'` (146 usages) and call `fetch`. This step only centralises the base path and switches the project-scoped calls to the prefixed API. **SPA routes do not change in this step** (the next step moves them under `/p/:slug`); the current slug comes from `/auth/me` for now.

## Requirements

- New `web/src/services/api.ts` exporting `instancePath(path: string): string` (→ `/api/v1` + path) and `apiPath(path: string): string` (→ `/api/v1/p/<current slug>` + path; throws a descriptive `Error('no current project')` when no slug is set — a bug, not a state), plus `setCurrentProjectSlug(slug: string | null)` and `getCurrentProjectSlug()` (module-level state so service functions stay plain). Unit tests for all three.
- `AuthProvider` calls `setCurrentProjectSlug(me.last_project_slug ?? me.projects[0]?.slug ?? null)` after `fetchMe()` and `null` on logout. (The route-driven slug replaces this in the next step.)
- Replace **every** `API_BASE` in `web/src/services/*.ts`: instance services use `instancePath` — `auth`, `push`/`notifications` preferences part, `jobs`, `system`, `backup`, `maintenance`, `restore`, `audit`, `userpic`/avatar of users, `settings` **public anonymous read** (keep instance-level until the per-project settings step), the public upload-link page (`/u/{code}`); everything else uses `apiPath`. `rg -n "API_BASE" web/src` must return nothing when done. Where a module mixes both (e.g. notifications: inbox is project-scoped, push subscriptions are instance), split by call, not by module.
- The anonymous pages (`/login`, `/register`, `/password-reset`, `/u/:code`) must not call `apiPath` (no slug yet) — add a Vitest test rendering each with no slug set and asserting no throw.
- Keep the memory-style lint rules in `docs/FRONTEND.md`/`docs/DEVELOPMENT.md` in mind (typed i18n, strict ESLint, Prettier check — do not read the gate through `| tail`).
- Tests: the existing service tests updated to expect the prefixed URLs (`/api/v1/p/v/photos` etc.); `api.ts` unit tests; a test that `AuthProvider` sets the slug from `/auth/me`.
- Docs: `docs/FRONTEND.md` — "API paths" subsection (the two helpers, the rule that a new service never hardcodes `/api/v1`).

## Out of scope

Moving SPA routes under `/p/:slug`, any provider/context for the project, UI for choosing a project, link rewriting.
