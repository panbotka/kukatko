# "Co je nového" counts the viewer's own actions

Verified on the box staging (commit 68e805e, 2026-09-06): after the admin posted one comment, the admin's own library greeted them with the banner "Co je nového — 1 nový komentář" on the next visit. The digest is meant to tell a returning person what *others* did since their last visit; their own comment, album or upload is not news to them.

## Root cause (verified, do not re-derive)

`internal/whatsnew/store.go`, `countsSQL` (≈ lines 100–112) counts `photo_comments WHERE created_at > $1 AND deleted_at IS NULL`, `albums WHERE created_at > $1 AND type = 'album'`, `photos WHERE created_at > $1 …` and `subjects WHERE created_at > $1 AND name <> ''` with no reference to the viewer. The actor is recorded on the rows: `photo_comments.author_uid` (migration 0052), `albums.created_by` (migration 0011), and photos carry their uploader (`uploaded_by`, exposed as `uploader` in the API). Subjects have no creator column.

## Requirements

- The digest for user U excludes comments authored by U, albums created by U and photos uploaded by U. Rows whose actor was deleted (`NULL`) stay counted.
- Subjects: keep counting them for everyone (there is no creator to exclude) and say so in a comment; do not add a column for this.
- When the exclusion empties every count, the panel is not shown at all (see `whatsnew.go` ≈ line 130, "is what decides that no panel is shown") — the "1 nový komentář" banner in the reported case must disappear.
- The detail panel ("Podrobnosti") lists the same filtered items as the counts; counts and lists must not disagree.
- Tests: integration tests in `internal/whatsnew` with two users where user A's comment/album/upload is news to B and not to A, and a deleted-author comment is news to both. Keep the existing index-backed range predicates — the new `AND author_uid IS DISTINCT FROM $2` style conditions must not turn the count into a scan (check the plan on the test DB).
- Docs: `docs/PACKAGES.md` entry for `internal/whatsnew`.

## Out of scope

- No schema change. No change to the visit bookkeeping (the 6 h gap).

## Implementation notes

- Definition of Done per `CLAUDE.md`: docs, `make check` (`make check-box` on the build box), commit, push. Skip `make dev`; there is no migration or wiring change.