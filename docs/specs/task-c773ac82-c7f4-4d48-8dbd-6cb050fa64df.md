# M3 — Subjects & markers schema

Add the tables and repositories for named subjects (people/pets/other) and markers (face/label
regions on photos).

## Context
Read `docs/ARCHITECTURE.md` §5, §7 (people). Markers use normalized [x,y,w,h] (0..1, display
space). Subjects are people/pets/other. The `faces` table (already present) caches marker_uid/
subject_uid for performance.

## Requirements
- Migration creating:
  - `subjects`: uid VARCHAR(32) PK, slug UNIQUE, name, type CHECK(person|pet|other), favorite,
    private, notes, cover_photo_uid (FK photos SET NULL), timestamps.
  - `markers`: uid VARCHAR(32) PK, photo_uid (FK photos CASCADE), subject_uid (FK subjects SET
    NULL), type CHECK(face|label), x,y,w,h DOUBLE PRECISION (0..1), score INT, invalid BOOL,
    reviewed BOOL, timestamps.
- Go `internal/people` package: repositories for subjects (CRUD, by slug/uid, list with counts)
  and markers (create, assign/unassign subject, list by photo, mark invalid/reviewed). Keep the
  `faces` cache columns (marker_uid/subject_uid/subject_name) consistent when markers change
  (update the denormalized cache).
- Slug generation for subjects (unique, from name).

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Integration tests (test DB): subject CRUD + unique slug; marker create/assign/unassign updates
  the `faces` cache columns; cascade delete of markers when a photo is deleted; subject cover
  set-null on photo delete.