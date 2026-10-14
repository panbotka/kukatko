# Nameless-subjects card on the instance Maintenance page names its project

The instance-level `/maintenance` page hosts a card that calls a project-scoped API through `apiPath`, so it silently targets whichever project the entry rule picked, and throws for a maintainer with no membership.

## Context (verify with rg before starting)
- `web/src/pages/MaintenancePage.tsx` renders `NamelessSubjectsCard`; `web/src/services/maintenance.ts` (~203, 248, 283) calls `apiPath('/maintenance/nameless-subjects…')`.
- The backend route is `/api/v1/p/{project}/maintenance/nameless-subjects` (maintainer-only); the project pages live under `/p/:slug/…`.

## Requirements
- The card moves to a project-scoped page (e.g. the project's settings/admin area under `/p/:slug/…`, maintainer-only) or stays on `/maintenance` with an explicit project picker listing every project; either way the request path always names the chosen project and never relies on `useProject()`'s entry rule. A maintainer with zero memberships can still use it.
- Navigation/tests: `MobileNavDrawer.test` and `navItems.test` updated if a nav entry moves; a Vitest test covers the picker or the new location rendering with the chosen slug in the request URL.
- `docs/FRONTEND.md` updated.

## Out of scope
- Backend changes to the nameless-subjects endpoints.