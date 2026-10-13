# Families are scoped by project

## Context

Kukátko is multi-tenant: `subject_families.project_id` exists; `internal/people` is scoped (convention in `docs/PACKAGES.md`: `projectUID` after `ctx`, never defaulted; foreign = 404 same shape); library-wide jobs carry `project_uid` in their payload and dedup key (field name exactly `project_uid`); each project has `storage_prefix` (`""` for the bootstrap project `v`, `projects/<uid>/` for others). Design: `docs/superpowers/specs/2026-10-11-multi-tenancy-design.md`, sections 2.4, 2.5, 5.2, 6; read `docs/PACKAGES.md` for `internal/family`, `internal/familyexport`, `internal/familyexportjob`, `internal/familyapi`. Step 16 of 40.

## Requirements

- `internal/family`: every method takes `projectUID`; creating a relation verifies all subjects share the project (a foreign subject → `ErrNotFound`); the tree/network walk and the `family:` search filter never leave the project; `internal/familyapi` passes the project; the inline "create a subject" path creates it in the project.
- `internal/familyexport`: the YAML document gains a top-level `project: {uid, slug}` and a bumped format version; the writer stores it at `<storage_prefix>families.yaml` (for `v` with an empty prefix that is today's root key, so nothing moves). `internal/familyexportjob`: the `family_export` job payload carries `project_uid`, dedup per project, triggered by any relation/subject change **in that project**; the rebuild/import side (if any reader of `families.yaml` exists, e.g. in `dirimport` or `restore`) reads the project from the document and refuses a document whose project does not match the target.
- `kukatko ctl family …` commands use the context's project (the ctl step adds the general mechanism; here only make sure the API paths it calls are the project-scoped ones).
- Isolation tests: store — relations/tree for A never include B's subjects; HTTP — relation with a foreign subject → 404; export — two projects produce two documents at two keys with the right `project` block; a job for A does not rewrite B's file.
- Docs: `docs/PACKAGES.md`, `docs/ARCHITECTURE.md` (families.yaml per project), `docs/RESTORE.md` (the file's new location per project).

## Out of scope

The storage move, other packages.
