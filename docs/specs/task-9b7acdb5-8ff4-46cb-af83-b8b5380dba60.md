Task 7 of 8 in the family-tree epic. Read CLAUDE.md,
docs/superpowers/specs/2026-09-12-family-tree-design.md section 6, and docs/OPERATIONS.md.
Depends on task 2.

Add a `family` subcommand group to internal/ctl, matching the existing `subjects` and `faces`
groups: read a subject's relations, add and remove them, and edit a family's kind, years and
note.

House rules for ctl, all of which already have precedent in the package:

- table / json / llm output through the PERSISTENT `-o` flag. Do NOT define a local
  `--output` flag on a subcommand: it shadows the persistent one and breaks it. This has
  already happened once in this repo.
- Destructive operations behind `--yes`, with a `--dry-run` that reports what would change —
  the way `subjects delete` and `duplicates merge` do. Removing a relation is destructive;
  adding one is not.
- The client talks to /api/v1 with a Bearer token from the named context. It must work
  against the `boxstage` context, which is how it should be exercised by hand before the
  task is called done.

Adding a relation should accept a person by name as well as by uid, resolving the name the
way the other ctl subcommands do, and should report the resolved names in its output rather
than bare uids — the output is read by humans and by agents, and neither can read a uid.

## Tests

Unit tests over the command wiring and the output shapes, following the existing ctl tests.

## Done

docs/OPERATIONS.md documents every new subcommand and flag. The internal/ctl line in
docs/PACKAGES.md and in CLAUDE.md's package map is updated to mention the family surface.
`make check-box` green. Do NOT run `make dev` — it would restart the live instance. Commit
and push.