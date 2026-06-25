# Development

How to build, run, and verify Kukátko locally. Read [`CLAUDE.md`](../CLAUDE.md) and
[`ARCHITECTURE.md`](ARCHITECTURE.md) before starting work.

## Prerequisites

- **Go 1.26+**
- **golangci-lint v2** (provides both `golangci-lint run` and `golangci-lint fmt`)
- A C compiler (`gcc`/`cc`) — only needed for `make test`, because the race detector
  requires cgo. The production binary is still built with `CGO_ENABLED=0`.

## Project layout

```
cmd/kukatko/        # CLI entrypoint (Cobra root + serve/version subcommands), kept thin
internal/server/    # chi HTTP server: routing, handlers, graceful shutdown
internal/version/   # build-time version/commit (ldflags-injectable)
.golangci.yml       # strict golangci-lint v2 config (the quality gate)
Makefile            # single source of truth for all tasks
```

## CLI

```bash
make build            # compile to bin/kukatko (CGO_ENABLED=0, version/commit injected)
./bin/kukatko serve   # start the HTTP server on :8080 (Ctrl-C / SIGTERM = graceful shutdown)
./bin/kukatko version # print version and commit
```

`kukatko serve` exposes `GET /healthz`, returning `200` with a JSON body:

```json
{ "status": "ok", "version": { "version": "dev", "commit": "none" } }
```

The listen port is hardcoded to `:8080` for now; configuration (YAML + env via Viper)
arrives in a later milestone.

## Make targets

```bash
make fmt              # gofmt + goimports (via golangci-lint fmt)
make vet              # go vet
make lint             # golangci-lint run (strict config)
make lint-fix         # golangci-lint run --fix
make test             # unit tests with the race detector (no DB needed)
make test-integration # integration tests (requires KUKATKO_TEST_DATABASE_URL)
make check            # fmt + vet + lint + test  ← the quality gate
make build            # compile the static binary into bin/
make clean            # remove build artifacts
make help             # list targets
```

## The quality gate

`make check` MUST pass before every commit (it is the project's verification command — a
red lint or test means the task ends as `needs_review`). The `.golangci.yml` is strict and
must not be weakened; `//nolint` is allowed only with a documented reason.

Unit tests run without external dependencies. Integration tests (DB/HTTP against a real
pgvector Postgres) are kept behind the `integration` build tag and `KUKATKO_TEST_DATABASE_URL`,
so `make check` stays fast and DB-free; they are added alongside the DB layer in a later task.

## Releasing version info

`Version` and `Commit` in `internal/version` are injected at build time. `make build` does
this automatically from git; to set an explicit version:

```bash
make build VERSION=1.2.3
```
