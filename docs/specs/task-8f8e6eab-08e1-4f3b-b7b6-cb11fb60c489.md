# Finishing the move of per-user catalogue state to the membership

## Context

Kukátko is multi-tenant. `project_members` holds `approved_at`, `subject_uid`, `last_seen_at`, `visit_reference_at`, `task_digest_at`; earlier steps moved the readers and writers of what's new, task digest, `person:me`, approval and registration to it. The same five columns still exist on `users` as a transition. `internal/userpic` serves a user's profile picture instance-wide (`GET /users/{uid}/avatar`) through a chain: uploaded picture → picked library photo → the linked subject's face → nothing. Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, sections 2.2, 4.2. Step 29 of 40. Memory-style warning in `docs/DEVELOPMENT.md`: a dropped column's writers in other packages still compile — grep the whole repository, including tests and seeders, before dropping.

## Requirements

- `rg -n "approved_at|subject_uid|last_seen_at|visit_reference_at|task_digest_at" --type go --type sql` and classify every hit as `users.` or `project_members.`; move every remaining `users.` reader/writer/seeder to the membership. Only then the migration: `ALTER TABLE users DROP COLUMN IF EXISTS …` for the five columns.
- `internal/reset`: remove the drop/re-add handling of the `users.subject_uid` FK and its test; keep the equivalent for `project_members.subject_uid`.
- `internal/userpic` + `userpicapi`: step 3 of the chain uses the requester's **current project** membership `subject_uid` of the pictured user when the request carries a project (`auth.ProjectFromContext`), and skips that step without one. Step 2 (a picked library photo) must not be served to a requester who has no access to that photo's project — refuse exactly as the private/hidden refusal works today (a 404, not a 403). The self-service `PUT /auth/picture` picking a library photo verifies the caller is a member of that photo's project.
- Tests: migration test that the five columns are gone; the full integration suite green; userpic — face step per project; picked photo from B not served to a member of A only; reset with a membership subject.
- Docs: `docs/ARCHITECTURE.md` data model (remove the "transitional" note), `docs/PACKAGES.md` (`internal/userpic`), `docs/API.md`.

## Out of scope

Everything else; this step is deliberately small because a column drop is irreversible — take care.
