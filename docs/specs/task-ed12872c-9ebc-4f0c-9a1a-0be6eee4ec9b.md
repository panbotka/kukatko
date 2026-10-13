# Invitation links — pages

## Context

Kukátko is multi-tenant. Backend: `GET/POST /api/v1/p/{slug}/invites`, `DELETE …/invites/{uid}` (project admin); public `GET /api/v1/invite/{code}` → `{project_name, role, signed_in}` or 404; `POST /api/v1/invite/{code}/accept` (signed in) → `{slug}`; `POST /auth/register` accepts `invite_code`. Frontend: `/p/:slug/members` page exists; `apiPath()`/`instancePath()`; login page supports `returnTo`; the registration page exists; Czech default + English; `Icon` only; palette tokens only. Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, sections 4.1, 4.2; read `docs/FRONTEND.md`. Step 27 of 40.

## Requirements

- Members page: an "Pozvánky" section — create form (role select, validity in days with default 7 and max 90, optional max uses), list (role, expiry, uses/max, created by, revoke with confirm), and a **copy button that works after creation and later** (reuse the upload-link copy pattern: the code stays readable for the manager). The created URL is shown in a read-only input with the copy button.
- Public page `/invite/:code` (instance-level route, outside the project Layout, works signed-out): loads the invite; shows "Pozvánka do projektu **<name>** jako <role>". Signed-in → button "Přijmout pozvánku" → accept → navigate to `/p/<slug>/` with a success toast. Signed-out → two buttons: "Zaregistrovat se" → `/register?invite=<code>` (the registration form sends `invite_code`, hides the secret field, shows the project name) and "Přihlásit se" → `/login?returnTo=/invite/<code>`. Invalid → the generic not-found page (same for every reason).
- Service module `web/src/services/invites.ts`.
- Tests (Vitest): invitations section create/list/revoke/copy; public page signed-in vs signed-out vs invalid; registration form with `invite` param hides the secret and sends the code.
- Docs: `docs/FRONTEND.md`, `README.md` (how to invite someone).

## Out of scope

Backend changes.
