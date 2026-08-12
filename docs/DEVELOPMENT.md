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

### The PWA: icons and re-checking installability

The app installs to a home screen. Its identity is committed, never generated at build time:
`web/public/icons/kukatko.svg` (and the full-bleed `kukatko-maskable.svg`) is the master, and
every PNG plus `favicon.ico` is rendered from it by

```bash
./scripts/icons.sh     # headless Chromium → crop; re-run after editing either SVG, commit the output
```

The service worker is emitted only by a **production** build (`web/build/pwa.ts`), so `npm run
dev` has no worker at all — registration is gated on `import.meta.env.PROD` and the disabled
branch unregisters leftovers. To exercise the real thing, serve a built binary and open it over
`http://localhost` (Chromium treats localhost as a secure origin) or over HTTPS; then, in
DevTools:

- **Application → Manifest** — Chromium lists the parsed manifest and every error it found
  (must be none), plus the icon previews.
- **Application → Service Workers** — `/sw.js` must be *activated and running*, with the page
  listed as a client.
- **Application → Cache Storage** — one `kukatko-shell-<hash>` cache holding `/index.html` and
  the build's hashed assets, and nothing else.
- **Install app** in the address bar (or the ⋮ menu) — its presence *is* Chromium's
  installability verdict; the `beforeinstallprompt` event fires for the same reason.
- **Network → Offline**, then reload: the app must still paint, the navigation's `deliveryType`
  must read `cache-storage`, and any `/api/…` request must fail (the worker deliberately never
  answers those, see `docs/FRONTEND.md`).

The same checks can be run headless over CDP — `Page.getAppManifest` returns Chromium's own
error list, `Page.addScriptToEvaluateOnNewDocument` can install a `beforeinstallprompt`
listener before the document runs, and `Network.emulateNetworkConditions {offline: true}` cuts
the network (note it does **not** flip `navigator.onLine`, so the in-app offline banner has to
be driven by dispatching the `offline` event).

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
make dev                  # smart rebuild + restart (wraps scripts/dev.sh)
make dev DEV_ARGS=--force # rebuild all three stages
./scripts/dev.sh          # same thing directly; --force to rebuild everything
```

It stops any running instance, rebuilds only the stages whose inputs changed, starts the
server in the background on `${KUKATKO_DEV_PORT:-6480}`, and waits up to 30 s for
`GET /healthz`. It exits `0` once the server is healthy, or `1` with the tail of
`kukatko.log` if it never came up — which is why it is a gate before every commit (see the
Definition of Done in [`CLAUDE.md`](../CLAUDE.md)).

Stopping is two-layered on purpose. The anchored `pkill` reaps a server started from this
same binary path; then the script also frees `${KUKATKO_DEV_PORT}` of **any** leftover
kukatko squatting it — e.g. one left running by a `/verify` or worktree build under
`/tmp/verify-*/bin/kukatko`, whose different path the anchored `pkill` never matches. Such an
orphan would otherwise keep the port, answer the health check, and mask every rebuild with a
stale binary. As a backstop the health gate confirms the pid listening on the port is the one
it just started; if a squatter still holds it, the script fails loudly instead of reporting a
fresh server. Only a kukatko is ever killed, so an unrelated service on that port is safe.

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
script runs on the host, outside the Docker network, so it needs the localhost DSN.
Thumbnails are cached in the gitignored `.devdata/cache`.

Dev serves originals over the same **`r2` backend production runs**, pointed at a local MinIO
rather than at Cloudflare — see [Object storage](#object-storage) below for what starts it and
what goes into `.secrets/db.env`. `dev.sh` only adds a writable `KUKATKO_STORAGE_TEMP_PATH`
(`.devdata/tmp`), which the R2 backend uses to stage objects and the FS backend ignores. To run
dev against the local filesystem instead, set `KUKATKO_STORAGE_BACKEND=fs` — but then nothing on
this box exercises the S3 path, which is where the last two production defects were found.

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
make check-box        # the same gate, run on the build box (24 threads) — ~10× faster
make build            # frontend build + compile the static binary into bin/
make dev-storage      # start the local MinIO (dev runtime + S3 integration tests)
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

While iterating, run the gate on the build box instead: **`make check-box`**
(`scripts/check-on-box.sh`) syncs the working tree — uncommitted work included, secrets never —
to `ssh box` and runs the same `make check` there on 24 threads, exiting with the remote exit
code. Measured 2026-08-12: **434 s here, 66 s there.** It wakes the box if it is asleep and
fails loudly if it cannot reach it; the integration suite has no remote equivalent, because its
database is on this Pi. Details and knobs: `docs/OPERATIONS.md`.

### Wall time

Measured end to end on the Raspberry Pi dev box with warm Go/golangci/npm caches:

| `make check` | wall time |
| --- | --- |
| before this change | 173 s |
| after, first run | 133 s |
| after, immediate rerun | 130 s |
| the same gate on 2026-08-12, after the suite grew to 2734 tests | 434 s |

Both "after" runs had an up-to-date stamp, so neither reran `npm ci`; a lockfile change adds
that back as a one-off (~7 s). The last row is why `make check-box` exists: unchanged target,
a suite that outgrew four cores — the box runs the same 434 s in 66 s.

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

## The integration suite

Unit tests run without external dependencies. Integration tests (DB/HTTP against a real
pgvector Postgres) are kept behind the `integration` build tag and `KUKATKO_TEST_DATABASE_URL`,
so `make check` stays fast and DB-free.

```bash
# the DSN lives in the gitignored .secrets/db.env; from the Pi host use the _HOST variant
export KUKATKO_TEST_DATABASE_URL=postgres://kukatko:…@localhost:5432/kukatko_test?sslmode=disable
make test-integration
```

`-p 1` is not optional: every package truncates the same test database, so a second package
running concurrently would wipe the first one's rows mid-test.

### The bcrypt work factor is lower under the `integration` tag

About fifteen packages seed their accounts through the real auth path
(`auth.Service.CreateUser`), so RBAC is exercised end to end instead of faked. Each seeded
account cost a bcrypt hash at the production cost 12, and `-race` multiplies that: password
hashing, not the behaviour under test, was **88 % of the suite's runtime** measured end to end,
and 95–98 % of it in the packages that only seed and assert. `internal/auditapi` — a handful of
assertions about a read-only listing — spent 49 s hashing.

`internal/auth/password_cost_integration.go` is compiled **only** under the `integration` build
tag and lowers the cost `HashPassword` mints at to `bcrypt.MinCost`. Nothing else changes: a
bcrypt hash records the cost it was minted at and `CompareHashAndPassword` reads it from the
hash rather than from a constant, so cheap and production hashes verify side by side through
the same `CheckPassword` (`TestCheckPassword_mixedCosts` pins that property down).

The knob is a **build-tag-selected identifier, not a settable `var`** — a var could be lowered
by anything that imports `internal/auth`, whereas the cheap cost is simply not compiled into a
build that lacks the tag, so nothing a running server can reach can weaken production hashing.
Two tests keep it honest:

- `TestHashPassword_productionCost` (`password_cost_test.go`, `//go:build !integration`) runs in
  `make test` — i.e. in the gate — and fails unless a tagless build mints cost 12.
- `TestBcryptCost_productionUnchanged` asserts the same constant from *inside* the integration
  build, so lowering the suite's cost cannot quietly drift into lowering the production one.

| env var | default | meaning |
| --- | --- | --- |
| `KUKATKO_TEST_BCRYPT_COST` | `4` (`bcrypt.MinCost`) | work factor the integration build mints at; read only by that build |

Set it to `12` to run the suite at the production cost (that is how the "before" column below
was measured). A value outside bcrypt's accepted range panics rather than silently falling back
to a cost nobody intended.

### Wall time, before and after

Measured on the Raspberry Pi dev box on 2026-08-03, same machine, same command
(`CGO_ENABLED=1 go test -race -p 1 -timeout 30m -tags=integration -count=1 ./...`), cold test
cache both times. "Before" is the identical binary run with `KUKATKO_TEST_BCRYPT_COST=12`, so
the only variable is the work factor:

| run | wall clock | sum of package times (97 packages) |
| --- | --- | --- |
| before — cost 12 | **39 min 46 s** | 2298.0 s |
| after — `bcrypt.MinCost` | **6 min 00 s** | 270.9 s |

Both runs are green. The ~90 s that the "after" run spends outside the tests is the cgo race
toolchain compiling the tree; a rerun with a warm test cache (`make test-integration`, no
`-count=1`) took 5 min 26 s.

The ten packages that dominated the old run:

| package | before | after | factor |
| --- | --- | --- | --- |
| `internal/photoapi` | 647.8 s | 12.6 s | 51× |
| `internal/auth` | 641.6 s | 23.1 s | 28× |
| `internal/mcpapi` | 77.7 s | 5.2 s | 15× |
| `internal/announcementapi` | 77.3 s | 1.9 s | 40× |
| `internal/ingest` | 69.8 s | 8.5 s | 8× |
| `internal/bulkapi` | 68.2 s | 2.2 s | 31× |
| `internal/facematch` | 68.0 s | 2.1 s | 32× |
| `internal/organizeapi` | 67.9 s | 2.1 s | 33× |
| `internal/jobsapi` | 58.2 s | 1.7 s | 33× |
| `internal/maintenanceapi` | 58.0 s | 1.6 s | 36× |

Nothing else changed — the suite runs the same tests, seeds the same accounts and logs in
through the same code; it just stops paying cost 12 for accounts it throws away seconds later.
Note that the packages that gained least (`internal/ingest`, `internal/mcpapi`) are the ones
actually doing work beyond seeding, which is what the remaining time should look like.

**Per-package schemas are not worth it yet.** `-p 1` remains the other structural limit, but
with the ten slowest packages now at 23 s and below, giving each package its own schema to
unlock parallelism would be a large, risky change for a handful of minutes. Revisit only if the
suite grows back into the tens of minutes.

## Object storage

Development runs the **same `r2` backend production runs** — against a local MinIO, not against
Cloudflare. One container serves both the dev runtime and the S3 integration tests, so the
storage path that ships is the storage path that gets exercised here. (Until 2026-08-02 dev
pointed at the *production* bucket with production credentials, then at a local-disk stop-gap
that exercised nothing; this replaces both.)

```bash
make dev-storage        # start it and create the buckets (idempotent)
./scripts/dev-storage.sh --env   # print the block .secrets/db.env needs
docker stop kukatko-minio        # stop it; `make dev-storage` brings it back
```

What that gives you: container `kukatko-minio` on the named volume `kukatko-minio-data` (so
`docker rm` does not take the data with it), `--restart unless-stopped`, a 1 GB memory cap, the
S3 API on `127.0.0.1:18100` and the console on `127.0.0.1:18101` — loopback only, and outside
every range this host reserves (5080, 5100–5999, 9000–9999, 12345, 18789), which is why MinIO's
own 9000/9001 are not used. Credentials are `kukatko` / `kukatko-dev-secret`: dev credentials,
in the script on purpose, and **not** the production ones. Buckets: `kukatko-dev` for the
runtime, `kukatko-test`, `kukatko-test-primary` and `kukatko-test-backup` for the tests.

The corresponding `.secrets/db.env` block sets `KUKATKO_STORAGE_BACKEND=r2`, the
`KUKATKO_STORAGE_R2_ENDPOINT`/`_BUCKET`/`_ACCESS_KEY`/`_SECRET_KEY` above, and the
`KUKATKO_TEST_S3_*` mirror of them for the tests. It never reaches the repo.

**`storage.r2.media_base_url` stays unset in dev, on purpose.** With no base URL
`newSignerFor` leaves the signer nil, `R2.URL()` returns the empty string, and `internal/mediaurl`
falls back to the application's own `/api/v1/photos/…` routes, which stream the bytes. That means
no Cloudflare Worker has to be reproduced locally — and it means **the signed-URL path is the one
thing dev does not exercise**. It is covered by the unit tests in `internal/storage` against the
frozen vectors in `testdata/url_signature_vectors.json`; treat a change to signing as
CI-verified, not dev-verified. The pair is validated as a pair: set both `media_base_url` and
`url_signing_secret` or neither, since either alone fails startup.

Never point `KUKATKO_STORAGE_R2_*` on this box at the production bucket. Beyond "don't", the
enforcement is in `kukatko maintenance reset`, which now makes you type the **bucket** name as
well as the database name before it deletes anything — see `docs/OPERATIONS.md`. The trade-off of
putting the guard there and only there: it protects the one command that *deletes*, not the ones
that *write*. A misconfigured `serve` would still upload into a foreign bucket — but those writes
are additive and recoverable, while the wipe is not, and a guard on every write path would be a
confirmation prompt in the upload handler.

### The S3 integration tests

The R2 backend, `internal/backup` and `internal/storagemigrate` have integration tests behind the
`integration` build tag and `KUKATKO_TEST_S3_ENDPOINT` (plus `KUKATKO_TEST_S3_BUCKET`, `_REGION`,
`_ACCESS_KEY`, `_SECRET_KEY`). They **skip** when the endpoint is unset, which is what keeps
`make check` free of any object-storage dependency. Any S3-compatible endpoint works; with
`make dev-storage` running and `.secrets/db.env` sourced, the variables are already there.

Every test bucket is created if absent and **emptied between cases** — point the variables at a
throwaway bucket, never at a real one.

```bash
set -a; source .secrets/db.env; set +a

# the backend itself
go test -tags=integration -run TestR2 ./internal/storage/

# bucket-to-bucket originals backup: needs TWO buckets, derived from
# KUKATKO_TEST_S3_BUCKET by suffix (…-primary and …-backup). No database. It covers
# the server-side copy, an incremental re-run that copies nothing new, the fact that
# an object deleted from the primary survives in the backup, and the loud failure
# when no target is configured.
go test -tags=integration -run TestBucketBackup ./internal/backup/

# the migration wants BOTH the bucket and KUKATKO_TEST_DATABASE_URL: it migrates a
# fixture library out of a real catalogue, kills the run mid-photo, resumes it, and
# asserts every object landed exactly once and that the photo which failed
# verification still has its local original.
go test -tags=integration -run TestMigrateToR2 ./internal/storagemigrate/
```

Running the whole `make test-integration` instead also needs `KUKATKO_TEST_DATABASE_URL`, since
the other integration packages want a database.

## Releasing version info

`Version` and `Commit` in `internal/version` are injected at build time. `make build` does
this automatically from git; to set an explicit version:

```bash
make build VERSION=1.2.3
```
