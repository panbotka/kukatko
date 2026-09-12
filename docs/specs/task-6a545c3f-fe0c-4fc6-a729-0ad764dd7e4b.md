Task 8 of 8 in the family-tree epic, and the one that may be dropped without making the
rest incoherent. Read docs/superpowers/specs/2026-09-12-family-tree-design.md section 9 and
open question 3 in section 14 FIRST.

If the owner's answer to open question 3 is still "the nightly pg_dump is enough", close this
task as not needed and say so, rather than building it. Do not build it by default.

## If it is wanted

Relationships are subject-level; sidecars are per-photo. They therefore do not belong in a
photo's sidecar, and leaving it there puts a hole in the "yours to keep" promise: lose the
database, lose the tree.

Build a library-level `families.yaml` at the store root:

- a versioned format in internal/sidecarexport's style, with a round-trip test that holds its
  sufficiency the way the photo sidecar's round-trip test does;
- its own `Kind` in internal/storekeys, so backup, storage migration and
  `maintenance reset` cannot forget it — that is precisely what that package exists for;
- rewritten by a job whenever a relation changes, debounced by the queue's dedup, following
  internal/sidecarjob's shape.

Note that a new object-store prefix must also be classified for the wipe path, exactly as a
new table must be classified in internal/reset — check both.

## Done

docs/ARCHITECTURE.md section 8.1 (which today says only the photo sidecar exists and that
reading back is not implemented), docs/PACKAGES.md, CLAUDE.md's package map if a package is
added, and README.md's "Yours to keep" section. `make check-box` green. Do NOT run `make dev`
— it would restart the live instance. Commit and push.