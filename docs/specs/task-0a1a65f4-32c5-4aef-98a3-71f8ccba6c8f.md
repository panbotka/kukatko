# Closing the transition

## Context

Kukátko is multi-tenant. Two safety nets were left in place during the programme: (1) every project-scoped API route is also mounted unprefixed at `/api/v1/…` and `auth.WithProject` resolves an absent `{project}` to the bootstrap project `v` (the frontend, ctl and MCP have since moved to `/api/v1/p/<slug>/…`); (2) every `project_id` column carries `DEFAULT 'pj0000000000000000veselice'` so writers that had not yet been taught the project kept working. Both must go, so a request or an insert without a project fails loudly instead of landing in `v`. Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, sections 2.4, 3.2, 7. Step 39 of 40.

## Requirements

- Remove the alias mount in `cmd/kukatko/projectapi.go` and the bootstrap fallback in `auth.WithProject` (a project-scoped handler reached with no resolved project → 500 with a logged message naming the route — a programming error). Unprefixed `/api/v1/photos` etc. now fall through like any unknown API path (the API 404, not the SPA fallback — check how unknown `/api/v1/*` paths answer today and keep that). `rg` the frontend, `internal/ctl`, `docs/`, the service worker and the PWA manifest for any remaining unprefixed project path and fix them.
- Migration dropping the `DEFAULT` from every `project_id` column (keep NOT NULL and the FK). Run the full integration suite; any insert that relied on the default fails now — fix the writer to pass the project explicitly.
- `rg "TODO multi-tenancy"` must return nothing: resolve and delete each.
- `project.BootstrapUID`/`BootstrapSlug` stay (the bootstrap migration, the rekey runbook and tests use them) but no request path may use them: add a test that no file under `internal/*api` or `cmd/kukatko` (except the migration/bootstrap code) references them.
- Final documentation pass: `docs/ARCHITECTURE.md` gains "Projects (multi-tenancy)" summarising the model (projects, members, instance vs project roles, URL prefix, storage layouts and the move, the tenancy guard); `docs/API.md` loses the transition note; `README.md` describes projects from a user's point of view (choosing, switching, invitations, what a project admin and the system admin do); `docs/OPERATIONS.md` has the complete upgrade runbook (deploy → inventory check → verify `v` → start the storage move → watch the page); `docs/SECURITY_AUDIT.md` gets a tenancy section (foreign-uid probing, directory enumeration, hash collision, derived-object sharing, invite abuse — each with the mitigation).
- Tests: server-level — `GET /api/v1/photos` → 404 while `/api/v1/p/v/photos` works; the bootstrap-reference guard; the migration test for the dropped defaults.

## Out of scope

Any new feature.
