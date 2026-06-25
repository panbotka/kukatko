# M0 — Database connection, migrations, integration test harness

Establish the PostgreSQL connection, an embedded SQL migration runner that auto-applies on
startup, and the integration-test harness used by all later DB tasks.

## Context
Read `docs/ARCHITECTURE.md` §4 (DB), §5 (data model), §19.2 (integration tests). Database is
already provisioned in the shared Postgres: **pgvector 0.8.1 + unaccent enabled**. App DSN is in
the gitignored `.secrets/db.env` as `KUKATKO_DATABASE_URL`; the separate test DB DSN is
`KUKATKO_TEST_DATABASE_URL` (database `kukatko_test`, safe to truncate). Use `jackc/pgx/v5`
(pgxpool) and `pgvector/pgvector-go`.

## Requirements
- `internal/database` package: open a `pgxpool.Pool` from `database.url`, apply pool limits from
  config, expose a typed wrapper with `Ping`, `Close`, and access to the pool.
- **Migration runner**: SQL files embedded via `//go:embed migrations/*.sql`, applied in
  lexicographic order on startup, tracked in a `schema_migrations` table (idempotent; never
  re-applies). Filenames like `0001_init.sql`. Wrap each migration in a transaction where safe.
- **First migration** (`0001_init.sql`): `CREATE EXTENSION IF NOT EXISTS vector;`
  `CREATE EXTENSION IF NOT EXISTS unaccent;` and the `schema_migrations` table if the runner
  doesn't create it itself. Verify `halfvec` type is usable (pgvector ≥ 0.7).
- Wire `kukatko serve` to run migrations before serving; add `kukatko migrate` subcommand to run
  migrations standalone.
- **Integration test harness** in e.g. `internal/database/dbtest`: helper that connects to
  `KUKATKO_TEST_DATABASE_URL`, applies all migrations, and provides per-test isolation
  (truncate-all or transaction+rollback). If the env var is unset, helpers call `t.Skip` so the
  fast `make test` gate does not require a DB. `make test-integration` runs these.

## Quality gate (mandatory)
- Use the **golang-developer** skill. `make check` MUST pass.
- Unit test: migration ordering/parsing logic without a DB. Integration test (skipped without
  test DSN): migrations apply cleanly, `vector` + `unaccent` extensions present, `halfvec(3)`
  column can be created and queried.
- Ensure `make test-integration` is green when `KUKATKO_TEST_DATABASE_URL` is set (it is, in
  `.secrets/db.env`).