# M2 — Full-text search

Add Czech-aware full-text search over photo text fields using PostgreSQL tsvector + unaccent.

## Context
Read `docs/ARCHITECTURE.md` §6.2. `unaccent` is enabled. Mirror photo-sorter: dictionary
`simple` (no stemming) + `unaccent` so diacritics-insensitive matching works ("deti" matches
"Děti"). Weight title>description>notes>file_name.

## Requirements
- Migration: add a **generated** `fts tsvector` column on `photos` built from an IMMUTABLE
  unaccent wrapper over title (weight A), description (B), notes (C), normalized file_name (D)
  (file_name normalized by replacing non-alphanumerics with spaces). GIN index on `fts`.
  Provide the IMMUTABLE `immutable_unaccent` wrapper function.
- Repository method + `GET /api/v1/search?q=` (full-text mode): rank by ts_rank using the same
  unaccented query parsing; combine with the existing photo list filters (date/gps/etc.) and
  pagination.
- Ensure metadata updates keep `fts` correct (generated column handles it automatically).

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Integration tests (test DB): diacritics-insensitive match ("tomas" finds "Tomáš"), field
  weighting (title hit ranks above notes hit), file_name token search, combined with a filter,
  pagination, and that updating a title changes results.