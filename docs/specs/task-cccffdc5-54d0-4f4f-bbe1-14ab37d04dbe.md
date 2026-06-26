# M0 — Authentication & authorization (backend)

Implement users, password auth, sessions with sliding expiry, login rate limiting, role-based
access control, and bootstrap admin.

## Context
Read `docs/ARCHITECTURE.md` §5 (users/sessions), §11 (Auth & security). Roles: `admin`,
`editor`, `viewer` (`editor`+`admin` have write access). Improvements over photo-sorter:
**sliding session expiry, login rate limiting, password change invalidates other sessions**.
DB is provisioned (DSN in `.secrets/db.env`). Use the migration runner + DB layer from the
existing codebase.

## Requirements
- Migration: `users` table (uid VARCHAR(32) PK, username UNIQUE, display_name, email,
  password_hash, role CHECK in admin/editor/viewer, disabled, timestamps, last_login_at) and
  `sessions` table (id PK, token, download_token, user_uid, role cached, created_at, expires_at;
  index on expires_at).
- Passwords: **bcrypt cost 12** (`HashPassword`/`CheckPassword`). UID generator (prefix + random,
  VARCHAR(32)).
- Endpoints under `/api/v1`: `POST /auth/login` (username+password → create session, set
  HttpOnly + SameSite=Strict cookie; separate download_token), `POST /auth/logout`,
  `GET /auth/me` (current user), `POST /auth/password` (change own password → invalidate this
  user's other sessions).
- **Sliding expiry**: on authenticated requests, extend session expiry (e.g. rolling window),
  with a maximum lifetime cap. Hourly cleanup of expired sessions.
- **Login rate limiting**: per-username/IP throttle on `/auth/login` (e.g. N attempts / window →
  429) to prevent brute force.
- **RBAC middleware**: inject authenticated user+role into context; helpers `RequireAuth`,
  `RequireWrite` (editor/admin), `RequireAdmin`. Viewer = read-only.
- **Bootstrap admin**: on empty `users` table, if `auth.bootstrap_admin_username/password` set,
  create the first admin; otherwise log a warning.
- Admin user management endpoints (admin only): list/create/update/disable users, reset password
  (reset invalidates all that user's sessions).

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Unit tests: password hashing/verify, UID format, rate-limiter logic, RBAC decisions.
- Integration tests (test DB): login success/failure, session creation + sliding extension +
  expiry cleanup, password change invalidates other sessions, RBAC enforced (viewer blocked from
  writes), bootstrap admin on empty table.