# Development

How to build, run, and verify Kukátko locally. Read [`CLAUDE.md`](../CLAUDE.md) and
[`ARCHITECTURE.md`](ARCHITECTURE.md) before starting work.

## Prerequisites

- **Go 1.26+**
- **golangci-lint v2** (provides both `golangci-lint run` and `golangci-lint fmt`)
- **Node.js 22+** (npm) — for the `web/` frontend (Vite build, ESLint, Prettier, Vitest)
- A C compiler (`gcc`/`cc`) — only needed for `make test-race` and `make test-integration`,
  because the race detector requires cgo. `make check` and the production binary are both
  `CGO_ENABLED=0`.

## Project layout

```
cmd/kukatko/        # CLI entrypoint (Cobra root + serve/version subcommands), kept thin
internal/server/    # chi HTTP server: routing, handlers, graceful shutdown
internal/version/   # build-time version/commit (ldflags-injectable)
internal/config/    # typed config: YAML + env override via Viper (config.Load)
internal/web/       # SPA fallback handler + internal/web/static (//go:embed of dist)
web/                # React 19 + TS + Vite frontend (Superhero theme, i18n); builds into
                    #   internal/web/static/dist, which Go embeds into the binary
config.example.yaml # documented example config (committed; real config is gitignored)
.golangci.yml       # strict golangci-lint v2 config (the quality gate)
Makefile            # single source of truth for all tasks
```

## Frontend

The SPA lives in `web/` (React 19 + TypeScript + Vite, react-bootstrap + Bootswatch
Superhero, react-router-dom, i18next with Czech default). `npm run build` outputs to
`internal/web/static/dist`; Go embeds that directory (`//go:embed all:dist/*`) and serves it
with an SPA fallback (unknown non-asset paths → `index.html`; fingerprinted files under
`/assets/` get an immutable cache). A committed `internal/web/static/dist/.gitkeep` keeps the
embed directive valid on a fresh checkout before any build. Dev loop:

```bash
cd web && npm install   # once
npm run dev             # Vite dev server, proxies /healthz and /api to localhost:8080
npm run lint            # ESLint (strict, typed)
npm run format:check    # Prettier
npm run test            # Vitest + React Testing Library
```

## CLI

```bash
make build                              # compile to bin/kukatko (CGO_ENABLED=0, version/commit injected)
export KUKATKO_DATABASE_URL="postgres://…"  # required by serve
./bin/kukatko serve                     # start HTTP server on web.host:web.port (default 0.0.0.0:8080)
./bin/kukatko serve --config config.yaml    # use an explicit config file
./bin/kukatko version                   # print version and commit
```

`kukatko serve` exposes `GET /healthz`, returning `200` with a JSON body:

```json
{ "status": "ok", "version": { "version": "dev", "commit": "none" } }
```

All other paths are served by the embedded SPA (client-side routes fall back to
`index.html`). Build the frontend first (`make build` does this automatically) so the binary
embeds real assets; without a build, only the `.gitkeep` placeholder is embedded.

## Dev server

`scripts/dev.sh` rebuilds and restarts the local dev server in single-binary (embed) mode —
one process serving the API and the embedded SPA on one port, exactly as in production.

```bash
./scripts/dev.sh          # smart rebuild: skips npm ci / Vite / go build when nothing changed
./scripts/dev.sh --force  # rebuild all three stages
```

It stops any running instance, rebuilds only the stages whose inputs changed, starts the
server in the background on `${KUKATKO_DEV_PORT:-6480}`, and waits up to 30 s for
`GET /healthz`. It exits `0` once the server is healthy, or `1` with the tail of
`kukatko.log` if it never came up — which is why it is a gate before every commit (see the
Definition of Done in [`CLAUDE.md`](../CLAUDE.md)).

Each build stage is skipped independently, using `find -newer`: `npm ci` against
`package-lock.json`, the Vite build against `internal/web/static/dist/index.html`, and
`go build` against `bin/kukatko`. The stages cascade — a rebuilt SPA changes the embedded
assets, so it forces a rebuild of the binary. A backend-only change therefore skips the Vite
build entirely: a cached restart takes about 2 s versus roughly 35 s for `--force`.

The script deliberately does not call `make build`: it wants finer-grained staging than
`build → web-build → web-deps` gives it (the make chain always reruns the Vite build and
`go build`). Both now skip `npm ci` when the lockfile is unchanged — the script via
`find -newer`, make via the `web-deps` stamp file.

The DSN comes from `KUKATKO_DATABASE_URL_HOST` in the gitignored `.secrets/db.env`: the
script runs on the host, outside the Docker network, so it needs the localhost DSN. Uploads
and thumbnails land in the gitignored `.devdata/`.

The same script is registered as the project's `dev_command` in Botka (`dev_port` 6480).
Botka runs it, waits for it to **exit**, and then discovers the real server PID by scanning
the port — so the script must background the server and return rather than `exec` into it.

For frontend-only iteration, `cd web && npm run dev` is still faster: the Vite dev server
proxies `/healthz` and `/api` to the backend.

## Configuration

`internal/config` loads a typed `Config` via `config.Load(path)`: built-in defaults are
overlaid with an optional YAML file and then `KUKATKO_`-prefixed environment variables
(env always wins). The file path is resolved from the `--config` flag, then the
`KUKATKO_CONFIG` env var, then the default `config.yaml`; a missing file is not an error.

Env keys map onto nested config keys by replacing dots with underscores
(`database.url` → `KUKATKO_DATABASE_URL`, `web.port` → `KUKATKO_WEB_PORT`). The one
exception is `maps.mapy_api_key`, read from the unprefixed `MAPY_API_KEY`. `database.url`
is required; `web.port`, the connection-pool sizes, and embedding dimensions are validated.
Every key and its default is documented in [`config.example.yaml`](../config.example.yaml).
Copy it to `config.yaml` (or the gitignored `config.local.yaml`) and keep secrets in the
environment.

## Make targets

```bash
make fmt              # golangci-lint fmt + Prettier --write   ← the only mutating target
make fmt-check        # golangci-lint fmt --diff + Prettier --check (read-only)
make vet              # go vet (standalone; `check` gets it via golangci-lint's govet)
make lint             # golangci-lint run + ESLint (strict)
make lint-fix         # golangci-lint run --fix + eslint --fix
make typecheck        # tsc -b --noEmit over the frontend
make test             # Go unit tests (CGO off, no race, no DB) + Vitest
make test-race        # Go unit tests under the race detector (CGO_ENABLED=1)
make test-integration # integration tests (requires KUKATKO_TEST_DATABASE_URL)
make check            # docs-budget + fmt-check + lint + typecheck + test  ← the quality gate
make build            # frontend build + compile the static binary into bin/
make clean            # remove build artifacts (binary, embedded dist, web build)
make help             # list targets

# frontend-only targets (run npm under web/):
make web-deps      # npm ci (stamped)   make web-build     # npm run build → embed dir
make web-lint      # eslint             make web-test      # vitest
make web-fmt       # prettier --write   make web-fmt-check # prettier --check
make web-typecheck # tsc -b --noEmit
```

## The quality gate

`make check` MUST pass before every commit (it is the project's verification command — a
red lint or test means the task ends as `needs_review`). The `.golangci.yml` is strict and
must not be weakened; `//nolint` is allowed only with a documented reason.

`check` **never modifies a file**: a successful run on a clean tree leaves `git status --short`
empty. It verifies formatting (`golangci-lint fmt --diff`, `prettier --check`) rather than
applying it — use `make fmt` for that. It also runs the frontend type check, which `make build`
would otherwise be the first thing to catch.

Two checks deliberately live outside the gate so committing stays fast:

- **`go vet`** — `.golangci.yml` sets `default: standard`, which already enables `govet`, so
  `golangci-lint run ./...` covers it. The `vet` target remains for standalone use.
- **`-race`** — the race detector needs `CGO_ENABLED=1`, whose toolchain cannot share the
  build cache with the `CGO_ENABLED=0` production build and therefore recompiles the tree.
  It moved to `make test-race`, which CI runs on every push (and `make test-integration`
  keeps `-race` too).

`web-deps` is guarded by a stamp file (`web/node_modules/.kukatko-npm-ci-stamp`) that depends
on `web/package-lock.json`, so `npm ci` reruns only when the lockfile changes.

### Wall time

Measured end to end on the Raspberry Pi dev box with warm Go/golangci/npm caches:

| `make check` | wall time |
| --- | --- |
| before this change | 173 s |
| after, first run | 133 s |
| after, immediate rerun | 130 s |

Both "after" runs had an up-to-date stamp, so neither reran `npm ci`; a lockfile change adds
that back as a one-off (~7 s).

Where the ~40 s went: the race detector (`CGO_ENABLED=1 go test -race ./...` takes 48 s against
14 s for the cache-sharing `CGO_ENABLED=0 go test ./...`), `npm ci` (7 s), and the duplicate
`go vet` compile (1 s) — offset by the 16 s newly spent on `tsc`, which used to hide until
`make build`.

Two caveats worth knowing before optimising further:

- **Warm-to-warm, a rerun is not faster.** It skips `npm ci` and recompiles nothing (Go prints
  `(cached)` for every package), but those are only ~7 s here. ESLint (32 s) and Vitest (65 s)
  cache nothing and now dominate the gate. They are the next thing to attack.
- **The real win is on a cold cache** — a fresh CI runner, or a fresh Botka attempt on a box
  that has not built the tree yet. There the cgo race toolchain used to recompile all 73
  packages plus the instrumented standard library, and `npm ci` had to download the world;
  neither happens any more.

Unit tests run without external dependencies. Integration tests (DB/HTTP against a real
pgvector Postgres) are kept behind the `integration` build tag and `KUKATKO_TEST_DATABASE_URL`,
so `make check` stays fast and DB-free; they are added alongside the DB layer in a later task.

The R2 storage backend has its own integration tests, behind the same build tag and
`KUKATKO_TEST_S3_ENDPOINT` (plus `KUKATKO_TEST_S3_BUCKET`, `_REGION`, `_ACCESS_KEY`,
`_SECRET_KEY`). They skip when the endpoint is unset. Any S3-compatible endpoint works; a
throwaway MinIO is the easiest:

```bash
docker run -d --name kukatko-minio -p 127.0.0.1:18100:9000 \
  -e MINIO_ROOT_USER=kukatko -e MINIO_ROOT_PASSWORD=kukatko-secret \
  quay.io/minio/minio:latest server /data

KUKATKO_TEST_S3_ENDPOINT=http://127.0.0.1:18100 \
KUKATKO_TEST_S3_ACCESS_KEY=kukatko KUKATKO_TEST_S3_SECRET_KEY=kukatko-secret \
  go test -tags=integration -run TestR2 ./internal/storage/
```

MinIO binds to loopback on a port outside the ranges this host reserves. Running the whole
`make test-integration` instead also needs `KUKATKO_TEST_DATABASE_URL`, since the other
integration packages want a database.

The test bucket (`kukatko-test` by default) is created if absent and **emptied between
cases** — point the variables at a throwaway bucket, never at a real one.

`internal/backup` has an integration test for the **bucket-to-bucket** originals backup, behind the
same variables. It needs **two** buckets, derived from `KUKATKO_TEST_S3_BUCKET` by suffix
(`kukatko-test-primary` and `kukatko-test-backup`); both are created if absent and emptied between
cases. It covers the server-side copy, an incremental re-run that copies nothing new, the fact that
an object deleted from the primary survives in the backup, and the loud failure when no target is
configured. No database is needed:

```bash
KUKATKO_TEST_S3_ENDPOINT=http://127.0.0.1:18100 \
KUKATKO_TEST_S3_ACCESS_KEY=kukatko KUKATKO_TEST_S3_SECRET_KEY=kukatko-secret \
  go test -tags=integration -run TestBucketBackup ./internal/backup/
```

`internal/storagemigrate` has an integration test that wants **both**: the bucket *and*
`KUKATKO_TEST_DATABASE_URL`, because it migrates a fixture library out of a real catalogue,
kills the run mid-photo, resumes it, and asserts every object landed exactly once and that the
photo which failed verification still has its local original:

```bash
KUKATKO_TEST_S3_ENDPOINT=http://127.0.0.1:18100 \
KUKATKO_TEST_S3_ACCESS_KEY=kukatko KUKATKO_TEST_S3_SECRET_KEY=kukatko-secret \
KUKATKO_TEST_DATABASE_URL=... \
  go test -tags=integration -run TestMigrateToR2 ./internal/storagemigrate/
```

## Releasing version info

`Version` and `Commit` in `internal/version` are injected at build time. `make build` does
this automatically from git; to set an explicit version:

```bash
make build VERSION=1.2.3
```
