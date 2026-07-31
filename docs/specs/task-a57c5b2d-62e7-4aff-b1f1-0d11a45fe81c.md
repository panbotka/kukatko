Phase 1 of docs/MIGRATION_PLAN.md ("wipe Kukatko's data") has no tooling. Today the only
way is a manual schema drop plus manual R2 deletions — unrepeatable and unguarded. Build the
tool. Context: docs/READINESS_AUDIT.md.

This command destroys data on purpose, and the owner has explicitly waived S3 backups
(READINESS_AUDIT.md §4), so a misfire cannot be undone from a backup — only by re-importing
from PhotoPrism. Treat the guards as the feature, not as decoration.

## What it must delete

- The whole catalogue: photos, photo_files, albums, album_photos, labels, photo_labels,
  subjects, markers, faces, face_detections, embeddings, photo_phashes, photo_places,
  photo_edits, import_runs, import_failures, jobs, and the curation side tables
  (user_favorites, user_ratings, saved_searches, feedback/rejection tables).
- The originals and derived files in the configured store. In R2 the object key is
  `YYYY/MM/<name>` verbatim, plus the `thumb/` and `sidecars/` prefixes — there is no
  Kukatko-specific prefix, so the bucket root IS the namespace.

## What it must NEVER touch

- `users`, `sessions`, `api_tokens`, `schema_migrations`, `announcements`, `audit_log`.
  Wiping the library must not lock the operator out or erase the audit trail.
- PhotoPrism and photo-sorter. They are read-only sources and the only rollback that exists.

## Required guards (all of them)

1. **Dry run is the default.** Without an explicit execute flag it only prints what it would
   delete: a row count per table and an object count per prefix. Deleting must be opt-in.
2. **Typed confirmation.** Require the operator to type the target database name exactly (not
   y/N). Refuse on mismatch.
3. **Target check.** Refuse to run unless the connected database matches the one in the
   loaded config, and print host + database name before asking. A typo must not be able to
   reach a different database.
4. **Refuse a non-interactive run** unless an explicit force flag is present as well, so a
   stray invocation from a script or an agent cannot wipe anything.
5. **Storage scope.** Delete only the prefixes the store owns. Never issue a
   delete-everything against the bucket, and never delete a key the catalogue did not
   reference unless an explicit orphan-sweep flag is given.
6. **Audit.** Write an audit_log entry in the same transaction as the catalogue truncation,
   recording who ran it and the counts removed.
7. Print a before/after count summary so the result is verifiable, not assumed.

## Verification (mandatory)

- Integration tests against the test database: dry run deletes nothing; a wrong typed name
  aborts; a mismatched target aborts; a real run empties the catalogue tables and leaves
  users/sessions/api_tokens/schema_migrations intact.
- A test proving the storage deletion is confined to the owned prefixes.
- `make check` green.
- docs/OPERATIONS.md documents the command; docs/MIGRATION_PLAN.md phase 1 points at it.
