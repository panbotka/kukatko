# Managing projects as the system admin — backend

## Context

Kukátko is multi-tenant. `internal/project` (projects, `Usage`, members) exists; `users.role = 'maintainer'` is the system admin (`RequireMaintainer`); `project_members` holds per-project roles; `internal/ctl` is the CLI client of the own API (contexts, Bearer token, table/JSON/llm output). Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, sections 4.3 and 5.1; read `docs/API.md` and `docs/OPERATIONS.md` (ctl). Step 22 of 40. The page comes in the next step.

## Requirements

- New package `internal/projectapi`, mounted **instance-level** (not under `/p/`), every route `RequireMaintainer`, every mutation audited with `ProjectUID`:
  - `GET /projects` → `[{uid, slug, name, description, active, storage_prefix, max_file_bytes, max_total_bytes, members_count, photos_count, originals_bytes, created_at}]`.
  - `POST /projects` `{slug, name, description?, max_file_bytes?, max_total_bytes?}` → 201 + the project; 409 `{"error":"slug_taken"}`; 422 `{"error":"invalid_slug", "message": "<the rule>"}` / `invalid_name`. Creating also inserts the project's settings row (registration closed, empty secret, empty welcome) and makes the creator an `admin` member (so somebody can enter).
  - `GET /projects/{uid}`; `PATCH /projects/{uid}` `{name?, description?, active?, max_file_bytes?: number|null, max_total_bytes?: number|null}` — `null` clears a limit; a `slug` in the body → 422 `slug_immutable`.
  - `POST /projects/{uid}/members` `{user_uid, role}` → adds an **approved** membership (maintainer path); `DELETE /projects/{uid}/members/{user_uid}`; both reuse `internal/project` and its last-admin guard (409 `last_admin`).
  - `GET /users` (instance-level, maintainer) → every account with `uid, username, display_name, email, instance_role, disabled, memberships: [{project_slug, role, approved}]`; `PATCH /users/{uid}` `{instance_role}` to promote/demote a maintainer (cannot demote oneself → 409 `self`); keep the existing `disable` endpoint wherever it lives today and make sure it is maintainer-only.
- `internal/ctl`: `ctl projects list|get <slug>|create --slug --name [--description] [--max-file-bytes] [--max-total-bytes]|update <slug> [...]|deactivate <slug>|activate <slug>|add-member <slug> <username> --role`, `ctl users list|promote <username>|demote <username>` — table/JSON/llm output like the rest (`-o` is the persistent format flag: do not shadow it).
- Tests: Go integration for every route (403 for a project admin who is not maintainer; slug taken; limits set/clear; last admin; self-demote), ctl command tests (fake server).
- Docs: `docs/API.md` (new "Projects (maintainer)" section + instance `/users`), `docs/PACKAGES.md`, `docs/OPERATIONS.md` (ctl), `CLAUDE.md` package map line.

## Out of scope

Any UI, per-project member management by project admins (own step), invitations, the storage move endpoints.
